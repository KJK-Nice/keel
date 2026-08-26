(() => {
  const FitAddonCtor = window.FitAddon?.FitAddon || window.FitAddon;
  const state = {
    hosts: [],
    snippets: [],
    herd: [],
    sessions: new Map(),
    activeId: null,
    query: "",
  };

  const els = {
    hostList: document.getElementById("host-list"),
    herdList: document.getElementById("herd-list"),
    herdEmpty: document.getElementById("herd-empty"),
    snippetList: document.getElementById("snippet-list"),
    tabs: document.getElementById("tabs"),
    terminals: document.getElementById("terminals"),
    welcome: document.getElementById("welcome"),
    barMeta: document.getElementById("bar-meta"),
    search: document.getElementById("search"),
    sidebar: document.getElementById("sidebar"),
    backdrop: document.getElementById("backdrop"),
    hostModal: document.getElementById("host-modal"),
    hostForm: document.getElementById("host-form"),
    quickModal: document.getElementById("quick-modal"),
    quickForm: document.getElementById("quick-form"),
    snippetModal: document.getElementById("snippet-modal"),
    snippetForm: document.getElementById("snippet-form"),
    toast: document.getElementById("toast"),
    activeSnippets: document.getElementById("active-snippets"),
  };

  async function api(path, opts = {}) {
    const res = await fetch(path, {
      headers: { "Content-Type": "application/json", ...(opts.headers || {}) },
      ...opts,
    });
    if (res.status === 204) return null;
    const data = await res.json().catch(() => ({}));
    if (!res.ok) throw new Error(data.error || res.statusText);
    return data;
  }

  function toast(message, isError = false) {
    els.toast.hidden = false;
    els.toast.textContent = message;
    els.toast.classList.toggle("is-error", isError);
    clearTimeout(toast._t);
    toast._t = setTimeout(() => {
      els.toast.hidden = true;
    }, 3600);
  }

  function groupHosts(hosts) {
    const q = state.query.trim().toLowerCase();
    const filtered = hosts.filter((h) => {
      const hay = `${h.name} ${h.group} ${h.address} ${h.username}`.toLowerCase();
      return !q || hay.includes(q);
    });
    const groups = new Map();
    for (const h of filtered) {
      const key = h.group || "Ungrouped";
      if (!groups.has(key)) groups.set(key, []);
      groups.get(key).push(h);
    }
    return groups;
  }

  function hostSubtitle(h) {
    if (h.authType === "local") return "this machine";
    const port = h.port && h.port !== 22 ? `:${h.port}` : "";
    return `${h.username}@${h.address}${port}`;
  }

  function renderHerd() {
    if (!state.herd.length) {
      els.herdList.innerHTML = "";
      els.herdEmpty.hidden = false;
      return;
    }
    els.herdEmpty.hidden = true;
    els.herdList.innerHTML = state.herd
      .map((s) => {
        const open = [...state.sessions.values()].some((tab) => tab.runtimeId === s.id);
        return `
          <div class="host ${open ? "is-open" : ""}" data-herd="${s.id}">
            <span class="dot" style="--dot:${s.color || "#5ee0b5"};background:${statusColor(s.status)}"></span>
            <div class="meta" role="button" tabindex="0" data-attach="${s.id}">
              <b>${escapeHtml(s.title)}</b>
              <small>${escapeHtml(s.status)} · ${s.clients} attached</small>
            </div>
            <div class="host-tools">
              <button class="tiny" data-kill="${s.id}" title="Kill session">kill</button>
            </div>
          </div>`;
      })
      .join("");
  }

  function statusColor(status) {
    if (status === "ready") return "#5ee0b5";
    if (status === "connecting") return "#f5c15d";
    return "#ff6b7a";
  }

  function renderHosts() {
    const groups = groupHosts(state.hosts);
    if (!groups.size) {
      els.hostList.innerHTML = `<div class="empty">No hosts match. Add a server or clear the search.</div>`;
      return;
    }
    const html = [];
    for (const [group, hosts] of groups) {
      html.push(`<div class="group-label">${escapeHtml(group)}</div>`);
      for (const h of hosts) {
        html.push(`
          <div class="host" data-id="${h.id}">
            <span class="dot" style="--dot:${h.color};background:${h.color}"></span>
            <div class="meta" role="button" tabindex="0" data-connect="${h.id}">
              <b>${escapeHtml(h.name)}</b>
              <small>${escapeHtml(hostSubtitle(h))}</small>
            </div>
            <div class="host-tools">
              <button class="tiny" data-edit="${h.id}" title="Edit">Edit</button>
              <button class="tiny" data-delete="${h.id}" title="Remove">✕</button>
            </div>
          </div>`);
      }
    }
    els.hostList.innerHTML = html.join("");
  }

  function renderSnippets() {
    if (!state.snippets.length) {
      els.snippetList.innerHTML = `<div class="empty">Save commands you type all the time. Click one to send it into the active tab.</div>`;
    } else {
      els.snippetList.innerHTML = state.snippets
        .map(
          (s) => `
        <div class="snippet-row">
          <div class="meta" role="button" tabindex="0" data-run="${s.id}">
            <b>${escapeHtml(s.name)}</b>
            <small>${escapeHtml(s.command.trim())}</small>
          </div>
          <div>
            <button class="tiny" data-edit-snippet="${s.id}">Edit</button>
            <button class="tiny" data-delete-snippet="${s.id}">✕</button>
          </div>
        </div>`
        )
        .join("");
    }
    els.activeSnippets.innerHTML = state.snippets
      .map((s) => `<button class="chip" data-run="${s.id}">${escapeHtml(s.name)}</button>`)
      .join("");
  }

  function renderTabs() {
    const sessions = [...state.sessions.values()];
    els.welcome.style.display = sessions.length ? "none" : "block";
    els.terminals.style.display = sessions.length ? "block" : "none";
    els.tabs.innerHTML = sessions
      .map(
        (s) => `
      <button class="tab ${s.id === state.activeId ? "is-active" : ""} ${s.status}" data-tab="${s.id}">
        <span class="status"></span>
        ${escapeHtml(s.title)}
        <span class="close" data-close="${s.id}" title="Close">×</span>
      </button>`
      )
      .join("");
    for (const s of sessions) {
      s.wrap.classList.toggle("is-active", s.id === state.activeId);
      if (s.id === state.activeId) {
        s.fit.fit();
        s.term.focus();
        sendResize(s);
      }
    }
    const active = state.sessions.get(state.activeId);
    els.barMeta.textContent = active ? active.subtitle : "No session";
  }

  function escapeHtml(value) {
    return String(value ?? "")
      .replaceAll("&", "&amp;")
      .replaceAll("<", "&lt;")
      .replaceAll(">", "&gt;")
      .replaceAll('"', "&quot;");
  }

  async function loadAll() {
    const [hosts, snippets, herd] = await Promise.all([
      api("/api/hosts"),
      api("/api/snippets"),
      api("/api/sessions"),
    ]);
    state.hosts = hosts;
    state.snippets = snippets;
    state.herd = herd;
    renderHosts();
    renderSnippets();
    renderHerd();
  }

  function localHost() {
    return state.hosts.find((h) => h.authType === "local");
  }

  async function openSession(host) {
    closeSidebar();
    const termProbe = document.createElement("div");
    termProbe.style.cssText = "position:absolute;visibility:hidden;width:800px;height:400px";
    document.body.appendChild(termProbe);
    const probe = new Terminal({ fontSize: 13, fontFamily: "monospace" });
    probe.open(termProbe);
    const cols = probe.cols || 80;
    const rows = probe.rows || 24;
    probe.dispose();
    termProbe.remove();
    const info = await api("/api/sessions", {
      method: "POST",
      body: JSON.stringify({ hostId: host.id, cols, rows }),
    });
    attachRuntime(info);
    loadAll().catch(() => {});
  }

  function attachRuntime(info) {
    const existing = [...state.sessions.values()].find((s) => s.runtimeId === info.id);
    if (existing) {
      state.activeId = existing.id;
      renderTabs();
      existing.term.focus();
      return;
    }
    closeSidebar();
    const id = info.id;
    const wrap = document.createElement("div");
    wrap.className = "term-wrap";
    els.terminals.appendChild(wrap);

    const term = new Terminal({
      cursorBlink: true,
      fontFamily: "IBM Plex Mono, ui-monospace, SFMono-Regular, Menlo, monospace",
      fontSize: 13,
      lineHeight: 1.3,
      theme: {
        background: "#0b0f16",
        foreground: "#d7deea",
        cursor: "#5ee0b5",
        cursorAccent: "#0b0f16",
        selectionBackground: "#243044",
        black: "#1b2230",
        red: "#ff6b7a",
        green: "#5ee0b5",
        yellow: "#f5c15d",
        blue: "#6ea8ff",
        magenta: "#c084fc",
        cyan: "#67e8f9",
        white: "#e8edf7",
        brightBlack: "#6b7385",
      },
    });
    const fit = new FitAddonCtor();
    term.loadAddon(fit);
    term.open(wrap);
    fit.fit();

    const session = {
      id,
      runtimeId: info.id,
      hostId: info.hostId,
      title: info.title,
      subtitle: info.subtitle,
      status: info.status === "ready" ? "is-ready" : "",
      term,
      fit,
      wrap,
      ws: null,
    };
    state.sessions.set(id, session);
    state.activeId = id;
    renderTabs();
    renderHerd();

    const proto = location.protocol === "https:" ? "wss" : "ws";
    const ws = new WebSocket(
      `${proto}://${location.host}/ws/session?id=${encodeURIComponent(info.id)}&cols=${term.cols}&rows=${term.rows}`
    );
    ws.binaryType = "arraybuffer";
    session.ws = ws;

    ws.addEventListener("message", (ev) => {
      if (typeof ev.data === "string") {
        const msg = JSON.parse(ev.data);
        if (msg.type === "status") {
          if (msg.status === "ready") session.status = "is-ready";
          if (msg.status === "error" || msg.status === "exited") session.status = "is-error";
          renderTabs();
          renderHerd();
        } else if (msg.type === "ready") {
          session.status = "is-ready";
          renderTabs();
        } else if (msg.type === "error") {
          session.status = "is-error";
          term.writeln(`\r\n\x1b[38;2;255;107;122m${msg.message}\x1b[0m`);
          renderTabs();
          toast(msg.message, true);
        }
        return;
      }
      term.write(new Uint8Array(ev.data));
    });
    ws.addEventListener("open", () => {
      term.onData((data) => {
        if (ws.readyState === WebSocket.OPEN) ws.send(new TextEncoder().encode(data));
      });
    });
    ws.addEventListener("close", () => {
      if (session.status !== "is-error") session.status = "";
      term.writeln("\r\n\x1b[38;2;139;149;168mDetached — still in the herd until you kill it.\x1b[0m");
      renderTabs();
      renderHerd();
    });
    ws.addEventListener("error", () => {
      session.status = "is-error";
      toast("WebSocket error", true);
      renderTabs();
    });
  }

  function sendResize(session) {
    if (!session?.ws || session.ws.readyState !== WebSocket.OPEN) return;
    session.ws.send(JSON.stringify({ type: "resize", cols: session.term.cols, rows: session.term.rows }));
  }

  function closeSession(id) {
    const session = state.sessions.get(id);
    if (!session) return;
    session.ws?.close();
    session.term.dispose();
    session.wrap.remove();
    state.sessions.delete(id);
    if (state.activeId === id) {
      state.activeId = state.sessions.size ? [...state.sessions.keys()].at(-1) : null;
    }
    renderTabs();
    renderHerd();
  }

  function sendSnippet(id) {
    const sn = state.snippets.find((s) => s.id === id);
    const session = state.sessions.get(state.activeId);
    if (!sn) return;
    if (!session || session.ws?.readyState !== WebSocket.OPEN) {
      toast("Open a session first");
      return;
    }
    const command = sn.command.endsWith("\n") ? sn.command : sn.command + "\n";
    session.ws.send(new TextEncoder().encode(command));
    session.term.focus();
  }

  function fillHostForm(host) {
    const form = els.hostForm;
    form.reset();
    form.id.value = host?.id || "";
    form.name.value = host?.name || "";
    form.group.value = host?.group || "";
    form.address.value = host?.address && host.address !== "localhost" ? host.address : host?.address || "";
    form.port.value = host?.port || 22;
    form.username.value = host?.username || "";
    form.authType.value = host?.authType || "password";
    form.password.value = "";
    form.privateKey.value = "";
    form.passphrase.value = "";
    const color = host?.color || "#5ee0b5";
    const radio = form.querySelector(`input[name="color"][value="${color}"]`);
    if (radio) radio.checked = true;
    document.getElementById("host-modal-title").textContent = host?.id ? "Edit host" : "New host";
    toggleAuthFields();
  }

  function toggleAuthFields() {
    const auth = els.hostForm.authType.value;
    els.hostForm.querySelectorAll("[data-auth]").forEach((el) => {
      el.style.display = el.dataset.auth === auth ? "grid" : "none";
    });
  }

  function secretPayload(value) {
    if (!value || value === "__saved__") return "";
    return value;
  }

  async function saveHost(event) {
    event.preventDefault();
    const form = els.hostForm;
    const payload = {
      name: form.name.value.trim(),
      group: form.group.value.trim(),
      address: form.address.value.trim(),
      port: Number(form.port.value || 22),
      username: form.username.value.trim(),
      authType: form.authType.value,
      password: secretPayload(form.password.value),
      privateKey: secretPayload(form.privateKey.value),
      passphrase: secretPayload(form.passphrase.value),
      color: form.color.value,
    };
    if (form.id.value) {
      await api(`/api/hosts/${form.id.value}`, { method: "PUT", body: JSON.stringify({ ...payload, id: form.id.value }) });
    } else {
      await api("/api/hosts", { method: "POST", body: JSON.stringify(payload) });
    }
    els.hostModal.close();
    await loadAll();
    toast("Host saved");
  }

  async function saveQuick(event) {
    event.preventDefault();
    const target = els.quickForm.target.value.trim();
    const password = els.quickForm.password.value;
    const parsed = parseTarget(target);
    if (!parsed) {
      toast("Could not parse user@host", true);
      return;
    }
    const host = await api("/api/hosts", {
      method: "POST",
      body: JSON.stringify({
        name: `${parsed.user}@${parsed.host}`,
        group: "Quick connect",
        address: parsed.host,
        port: parsed.port,
        username: parsed.user,
        authType: "password",
        password,
        color: "#6ea8ff",
      }),
    });
    els.quickModal.close();
    await loadAll();
    openSession(host);
  }

  function parseTarget(value) {
    const match = value.match(/^(?:ssh\s+)?([^@\s]+)@(\[?[A-Za-z0-9._:-]+\]?)(?::(\d+))?$/);
    if (!match) return null;
    return { user: match[1], host: match[2].replace(/^\[|\]$/g, ""), port: Number(match[3] || 22) };
  }

  async function saveSnippet(event) {
    event.preventDefault();
    const form = els.snippetForm;
    let command = form.command.value;
    if (!command.endsWith("\n")) command += "\n";
    const payload = { name: form.name.value.trim(), command };
    if (form.id.value) {
      await api(`/api/snippets/${form.id.value}`, { method: "PUT", body: JSON.stringify({ ...payload, id: form.id.value }) });
    } else {
      await api("/api/snippets", { method: "POST", body: JSON.stringify(payload) });
    }
    els.snippetModal.close();
    await loadAll();
    toast("Snippet saved");
  }

  function closeSidebar() {
    els.sidebar.classList.remove("is-open");
    els.backdrop.classList.remove("is-open");
  }

  els.hostList.addEventListener("click", async (ev) => {
    const connect = ev.target.closest("[data-connect]");
    const edit = ev.target.closest("[data-edit]");
    const del = ev.target.closest("[data-delete]");
    if (connect) {
      const host = state.hosts.find((h) => h.id === connect.dataset.connect);
      if (host) openSession(host).catch((err) => toast(err.message, true));
    } else if (edit) {
      const host = state.hosts.find((h) => h.id === edit.dataset.edit);
      fillHostForm(host);
      els.hostModal.showModal();
    } else if (del) {
      const host = state.hosts.find((h) => h.id === del.dataset.delete);
      if (!host) return;
      if (host.authType === "local") {
        toast("Local Shell is part of the workspace and cannot be removed");
        return;
      }
      if (!confirm(`Remove ${host.name}?`)) return;
      await api(`/api/hosts/${host.id}`, { method: "DELETE" });
      await loadAll();
    }
  });

  els.herdList.addEventListener("click", async (ev) => {
    const attach = ev.target.closest("[data-attach]");
    const kill = ev.target.closest("[data-kill]");
    if (attach) {
      const info = state.herd.find((s) => s.id === attach.dataset.attach);
      if (info) attachRuntime(info);
    } else if (kill) {
      try {
        await api(`/api/sessions/${kill.dataset.kill}`, { method: "DELETE" });
        const tab = [...state.sessions.values()].find((s) => s.runtimeId === kill.dataset.kill);
        if (tab) closeSession(tab.id);
        await loadAll();
      } catch (err) {
        toast(err.message, true);
      }
    }
  });

  document.body.addEventListener("click", (ev) => {
    const run = ev.target.closest("[data-run]");
    if (run) sendSnippet(run.dataset.run);
    const editSn = ev.target.closest("[data-edit-snippet]");
    if (editSn) {
      const sn = state.snippets.find((s) => s.id === editSn.dataset.editSnippet);
      els.snippetForm.reset();
      els.snippetForm.id.value = sn.id;
      els.snippetForm.name.value = sn.name;
      els.snippetForm.command.value = sn.command;
      document.getElementById("snippet-modal-title").textContent = "Edit snippet";
      els.snippetModal.showModal();
    }
    const delSn = ev.target.closest("[data-delete-snippet]");
    if (delSn) {
      const sn = state.snippets.find((s) => s.id === delSn.dataset.deleteSnippet);
      if (!sn || !confirm(`Delete snippet “${sn.name}”?`)) return;
      api(`/api/snippets/${sn.id}`, { method: "DELETE" }).then(loadAll);
    }
  });

  els.tabs.addEventListener("click", (ev) => {
    const close = ev.target.closest("[data-close]");
    if (close) {
      ev.preventDefault();
      closeSession(close.dataset.close);
      return;
    }
    const tab = ev.target.closest("[data-tab]");
    if (tab) {
      state.activeId = tab.dataset.tab;
      renderTabs();
    }
  });

  document.querySelectorAll(".side-tab").forEach((btn) => {
    btn.addEventListener("click", () => {
      document.querySelectorAll(".side-tab").forEach((b) => b.classList.toggle("is-active", b === btn));
      document.getElementById("herd-panel").classList.toggle("is-active", btn.dataset.side === "herd");
      document.getElementById("hosts-panel").classList.toggle("is-active", btn.dataset.side === "hosts");
      document.getElementById("snippets-panel").classList.toggle("is-active", btn.dataset.side === "snippets");
    });
  });

  document.getElementById("btn-new-host").addEventListener("click", () => {
    fillHostForm(null);
    els.hostModal.showModal();
  });
  document.getElementById("btn-welcome-host").addEventListener("click", () => {
    fillHostForm(null);
    els.hostModal.showModal();
  });
  document.getElementById("btn-quick").addEventListener("click", () => els.quickModal.showModal());
  document.getElementById("btn-new-snippet").addEventListener("click", () => {
    els.snippetForm.reset();
    document.getElementById("snippet-modal-title").textContent = "New snippet";
    els.snippetModal.showModal();
  });
  document.getElementById("btn-welcome-local").addEventListener("click", () => {
    const host = localHost();
    if (host) openSession(host).catch((err) => toast(err.message, true));
  });
  document.getElementById("host-cancel").addEventListener("click", () => els.hostModal.close());
  document.getElementById("quick-cancel").addEventListener("click", () => els.quickModal.close());
  document.getElementById("snippet-cancel").addEventListener("click", () => els.snippetModal.close());
  els.hostForm.authType.addEventListener("change", toggleAuthFields);
  els.hostForm.addEventListener("submit", (ev) => saveHost(ev).catch((err) => toast(err.message, true)));
  els.quickForm.addEventListener("submit", (ev) => saveQuick(ev).catch((err) => toast(err.message, true)));
  els.snippetForm.addEventListener("submit", (ev) => saveSnippet(ev).catch((err) => toast(err.message, true)));
  els.search.addEventListener("input", () => {
    state.query = els.search.value;
    renderHosts();
  });
  document.getElementById("btn-menu").addEventListener("click", () => {
    els.sidebar.classList.toggle("is-open");
    els.backdrop.classList.toggle("is-open");
  });
  els.backdrop.addEventListener("click", closeSidebar);

  window.addEventListener("resize", () => {
    const active = state.sessions.get(state.activeId);
    if (!active) return;
    active.fit.fit();
    sendResize(active);
  });
  window.addEventListener("keydown", (ev) => {
    if ((ev.metaKey || ev.ctrlKey) && ev.key.toLowerCase() === "k") {
      ev.preventDefault();
      els.search.focus();
    }
  });

  loadAll().catch((err) => toast(err.message, true));
  setInterval(() => {
    api("/api/sessions")
      .then((herd) => {
        state.herd = herd;
        renderHerd();
      })
      .catch(() => {});
  }, 1500);
})();
