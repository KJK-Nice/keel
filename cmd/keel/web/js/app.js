(() => {
  const FitAddonCtor = window.FitAddon?.FitAddon || window.FitAddon;
  const params = new URLSearchParams(location.search);
  const state = {
    hosts: [],
    snippets: [],
    herd: [],
    sessions: new Map(),
    activeId: null,
    query: "",
    name: params.get("name") || localStorage.getItem("keel-name") || "",
    token: params.get("token") || localStorage.getItem("keel-token") || "",
    pendingSession: params.get("session") || params.get("id") || "",
    herdWs: null,
  };

  const TERM_FONT = "JetBrains Mono, ui-monospace, SFMono-Regular, Menlo, Consolas, monospace";
  const TERM_THEMES = {
    dark: {
      background: "#0a0a0a",
      foreground: "#e4e4e4",
      cursor: "#82a7ff",
      cursorAccent: "#0a0a0a",
      selectionBackground: "#2a3344",
      black: "#1a1a1a",
      red: "#f87171",
      green: "#4ade80",
      yellow: "#e8b849",
      blue: "#82a7ff",
      magenta: "#c084fc",
      cyan: "#67e8f9",
      white: "#ececec",
      brightBlack: "#737373",
    },
    light: {
      background: "#fbfbfa",
      foreground: "#1a1a1a",
      cursor: "#3b6cff",
      cursorAccent: "#fbfbfa",
      selectionBackground: "#dbe4ff",
      black: "#1a1a1a",
      red: "#dc2626",
      green: "#15803d",
      yellow: "#b45309",
      blue: "#3b6cff",
      magenta: "#7c3aed",
      cyan: "#0e7490",
      white: "#f5f5f4",
      brightBlack: "#737373",
    },
  };

  function resolvedTheme() {
    const choice = document.documentElement.dataset.theme || "system";
    if (choice === "light" || choice === "dark") return choice;
    return window.matchMedia("(prefers-color-scheme: light)").matches ? "light" : "dark";
  }

  function xtermTheme() {
    return TERM_THEMES[resolvedTheme()];
  }

  function applyTerminalThemes() {
    const theme = xtermTheme();
    for (const s of state.sessions.values()) {
      s.term.options.theme = theme;
      s.term.options.fontFamily = TERM_FONT;
    }
  }

  function syncThemeSwitch() {
    const choice = document.documentElement.dataset.theme || "system";
    document.querySelectorAll("[data-theme-choice]").forEach((btn) => {
      const on = btn.dataset.themeChoice === choice;
      btn.classList.toggle("is-active", on);
      btn.setAttribute("aria-checked", on ? "true" : "false");
    });
  }

  function setTheme(choice) {
    if (choice !== "light" && choice !== "dark" && choice !== "system") choice = "system";
    document.documentElement.dataset.theme = choice;
    localStorage.setItem("keel-theme", choice);
    syncThemeSwitch();
    applyTerminalThemes();
    refreshActiveTerminal();
  }

  function refreshActiveTerminal() {
    const active = state.sessions.get(state.activeId);
    if (!active?.term) return;
    const run = () => {
      try {
        active.fit.fit();
        active.term.refresh(0, Math.max(0, (active.term.rows || 1) - 1));
        sendResize(active);
      } catch {
        /* ignore */
      }
    };
    requestAnimationFrame(() => requestAnimationFrame(run));
  }

  const els = {
    hostList: document.getElementById("host-list"),
    herdList: document.getElementById("herd-list"),
    herdEmpty: document.getElementById("herd-empty"),
    snippetList: document.getElementById("snippet-list"),
    tabs: document.getElementById("tabs"),
    terminals: document.getElementById("terminals"),
    welcome: document.getElementById("welcome"),
    barMeta: document.getElementById("bar-meta"),
    barStatus: document.getElementById("bar-status"),
    controlWidget: document.getElementById("control-widget"),
    btnShare: document.getElementById("btn-share"),
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
    gate: document.getElementById("gate"),
    gateForm: document.getElementById("gate-form"),
  };

  function authHeaders() {
    const headers = { "Content-Type": "application/json" };
    if (state.token) headers.Authorization = `Bearer ${state.token}`;
    return headers;
  }

  function withToken(url) {
    if (!state.token) return url;
    const u = new URL(url, location.origin);
    u.searchParams.set("token", state.token);
    return u.pathname + u.search;
  }

  async function api(path, opts = {}) {
    const res = await fetch(path, {
      headers: { ...authHeaders(), ...(opts.headers || {}) },
      ...opts,
    });
    if (res.status === 204) return null;
    const data = await res.json().catch(() => ({}));
    if (res.status === 401) {
      const err = new Error(data.error || "unauthorized");
      err.code = 401;
      throw err;
    }
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

  function persistIdentity() {
    if (state.name) localStorage.setItem("keel-name", state.name);
    if (state.token) localStorage.setItem("keel-token", state.token);
    else localStorage.removeItem("keel-token");
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

  function driverName(s) {
    const d = (s.attached || []).find((c) => c.role === "driver" || c.id === s.driverId);
    return d?.name || "";
  }

  function peopleLabel(s) {
    const names = (s.attached || []).map((c) => (c.role === "driver" || c.id === s.driverId ? `${c.name}*` : c.name));
    if (names.length) return names.join(", ");
    if (s.clients) return `${s.clients} attached`;
    return "empty";
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
        const driver = driverName(s);
        return `
          <div class="host ${open ? "is-open" : ""}" data-herd="${s.id}">
            <span class="dot" style="--dot:${s.color || "#5ee0b5"};background:${statusColor(s.status)}"></span>
            <div class="meta" role="button" tabindex="0" data-attach="${s.id}">
              <b>${escapeHtml(s.title)}</b>
              <small>${escapeHtml(s.status)}${driver ? ` · ${escapeHtml(driver)} driving` : ""} · ${escapeHtml(peopleLabel(s))}</small>
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

  function isDriving(session) {
    return session?.you?.role === "driver";
  }

  function isOwner(session) {
    return session?.you?.id && session.you.id === session.ownerId;
  }

  function renderControl(session) {
    if (!session || !els.controlWidget) return;
    const attached = session.attached || [];
    if (!attached.length && !session.you) {
      els.controlWidget.hidden = true;
      els.btnShare.hidden = !session;
      return;
    }
    els.controlWidget.hidden = false;
    els.btnShare.hidden = false;
    const canGrant = isDriving(session) || isOwner(session);
    const people = attached
      .map((c) => {
        const driving = c.role === "driver" || c.id === session.driverId;
        const grant = canGrant && !driving ? `data-grant="${c.id}"` : "";
        return `<button type="button" class="chip-person ${driving ? "is-driver" : ""} ${grant ? "is-grantable" : ""}" ${grant} title="${driving ? "Driving" : canGrant ? "Pass control" : ""}">${escapeHtml(c.name)}${driving ? " *" : ""}</button>`;
      })
      .join("");
    let action = "";
    if (!isDriving(session) && isOwner(session)) {
      action = `<button class="tiny" data-take="1">Take control</button>`;
    } else if (!isDriving(session)) {
      action = `<button class="tiny" data-request="1">Request control</button>`;
    }
    els.controlWidget.innerHTML = `<div class="people">${people}</div>${action}`;
    session.wrap?.classList.toggle("is-follow", !isDriving(session));
    if (session.term) session.term.options.disableStdin = !isDriving(session);
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
        ${escapeHtml(s.title)}${isDriving(s) ? "" : s.you ? " · follow" : ""}
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
    if (els.barStatus) {
      if (!active) els.barStatus.textContent = "No session";
      else if (isDriving(active)) els.barStatus.textContent = `${active.subtitle} · driving`;
      else els.barStatus.textContent = `${active.subtitle} · following`;
    }
    renderControl(active);
  }

  function escapeHtml(value) {
    return String(value ?? "")
      .replaceAll("&", "&amp;")
      .replaceAll("<", "&lt;")
      .replaceAll(">", "&gt;")
      .replaceAll('"', "&quot;");
  }

  function applyHerdEvent(ev) {
    if (!ev || !ev.type) return;
    if (ev.type === "snapshot" && Array.isArray(ev.sessions)) {
      state.herd = ev.sessions;
      renderHerd();
      return;
    }
    if (ev.type === "spawn" && ev.session) {
      if (!state.herd.some((s) => s.id === ev.session.id)) state.herd.push(ev.session);
      else state.herd = state.herd.map((s) => (s.id === ev.session.id ? ev.session : s));
      renderHerd();
      return;
    }
    if (ev.type === "kill") {
      const id = ev.id || ev.session?.id;
      state.herd = state.herd.filter((s) => s.id !== id);
      renderHerd();
      return;
    }
    if ((ev.type === "update" || ev.type === "presence" || ev.type === "control") && ev.session) {
      const i = state.herd.findIndex((s) => s.id === ev.session.id);
      if (i >= 0) state.herd[i] = { ...state.herd[i], ...ev.session };
      else state.herd.push(ev.session);
      renderHerd();
    }
  }

  function connectHerd() {
    if (state.herdWs) {
      try {
        state.herdWs.close();
      } catch {
        /* ignore */
      }
    }
    const proto = location.protocol === "https:" ? "wss" : "ws";
    const ws = new WebSocket(`${proto}://${location.host}${withToken("/ws/herd")}`);
    state.herdWs = ws;
    ws.addEventListener("message", (ev) => {
      try {
        applyHerdEvent(JSON.parse(ev.data));
      } catch {
        /* ignore */
      }
    });
    ws.addEventListener("close", () => {
      if (state.herdWs === ws) {
        state.herdWs = null;
        setTimeout(connectHerd, 2000);
      }
    });
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
    const probe = new Terminal({ fontSize: 13, fontFamily: TERM_FONT, theme: xtermTheme() });
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
    const banner = document.createElement("div");
    banner.className = "follow-banner";
    banner.hidden = true;
    banner.textContent = "Following — request control to type";
    wrap.appendChild(banner);
    els.terminals.appendChild(wrap);

    const term = new Terminal({
      cursorBlink: true,
      fontFamily: TERM_FONT,
      fontSize: 13,
      lineHeight: 1.3,
      theme: xtermTheme(),
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
      banner,
      ws: null,
      you: null,
      attached: info.attached || [],
      driverId: info.driverId,
      ownerId: info.ownerId,
    };
    state.sessions.set(id, session);
    state.activeId = id;
    renderTabs();
    renderHerd();

    const proto = location.protocol === "https:" ? "wss" : "ws";
    const q = new URLSearchParams({
      id: info.id,
      cols: String(term.cols || 80),
      rows: String(term.rows || 24),
      name: state.name || "guest",
      kind: "web",
    });
    if (state.token) q.set("token", state.token);
    const ws = new WebSocket(`${proto}://${location.host}/ws/session?${q}`);
    ws.binaryType = "arraybuffer";
    session.ws = ws;

    const applyPresence = (msg) => {
      if (msg.you) session.you = msg.you;
      if (msg.attached) session.attached = msg.attached;
      if (msg.driverId != null) session.driverId = msg.driverId;
      if (msg.ownerId != null) session.ownerId = msg.ownerId;
      if (msg.title) session.title = msg.title;
      banner.hidden = isDriving(session);
      const herd = state.herd.find((h) => h.id === session.runtimeId);
      if (herd) {
        herd.attached = session.attached;
        herd.driverId = session.driverId;
        herd.ownerId = session.ownerId;
        herd.clients = session.attached.length;
      }
      renderTabs();
      renderHerd();
    };

    ws.addEventListener("message", (ev) => {
      if (typeof ev.data === "string") {
        const msg = JSON.parse(ev.data);
        if (msg.type === "status") {
          if (msg.status === "ready") session.status = "is-ready";
          if (msg.status === "error" || msg.status === "exited") session.status = "is-error";
          applyPresence(msg);
        } else if (msg.type === "ready" || msg.type === "presence" || msg.type === "control") {
          session.status = "is-ready";
          applyPresence(msg);
          if (msg.type === "control" && msg.message) toast(msg.message);
        } else if (msg.type === "error") {
          session.status = "is-error";
          term.writeln(`\r\n\x1b[38;2;248;113;113m${msg.message}\x1b[0m`);
          renderTabs();
          toast(msg.message, true);
        }
        return;
      }
      term.write(new Uint8Array(ev.data));
    });
    ws.addEventListener("open", () => {
      term.onData((data) => {
        if (!isDriving(session)) return;
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
    if (!isDriving(session)) return;
    session.ws.send(JSON.stringify({ type: "resize", cols: session.term.cols, rows: session.term.rows }));
  }

  function sendControl(type, extra = {}) {
    const session = state.sessions.get(state.activeId);
    if (!session?.ws || session.ws.readyState !== WebSocket.OPEN) return;
    session.ws.send(JSON.stringify({ type, ...extra }));
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
    if (!isDriving(session)) {
      toast("You are following — take or request control to type");
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

  function shareActive() {
    const session = state.sessions.get(state.activeId);
    if (!session) return;
    const u = new URL(location.origin);
    u.searchParams.set("session", session.runtimeId);
    if (state.name) u.searchParams.set("name", state.name);
    if (state.token) u.searchParams.set("token", state.token);
    navigator.clipboard.writeText(u.toString()).then(
      () => toast("Share link copied — joiners follow until you pass control"),
      () => toast(u.toString())
    );
  }

  function showGate() {
    els.gate.hidden = false;
    els.gateForm.name.value = state.name;
    els.gateForm.token.value = state.token;
    els.gateForm.name.focus();
  }

  async function boot() {
    persistIdentity();
    try {
      await loadAll();
    } catch (err) {
      if (err.code === 401) {
        showGate();
        toast("This runtime requires a share token", true);
        return;
      }
      toast(err.message, true);
      return;
    }
    els.gate.hidden = true;
    connectHerd();
    if (state.pendingSession) {
      const info = state.herd.find((s) => s.id === state.pendingSession);
      if (info) attachRuntime(info);
      state.pendingSession = "";
    }
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
    const grant = ev.target.closest("[data-grant]");
    if (grant) sendControl("grant-control", { targetId: Number(grant.dataset.grant) });
    if (ev.target.closest("[data-request]")) sendControl("request-control");
    if (ev.target.closest("[data-take]")) sendControl("take-control");
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
  [els.hostModal, els.quickModal, els.snippetModal].forEach((modal) => {
    modal.addEventListener("close", refreshActiveTerminal);
  });
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
  els.btnShare.addEventListener("click", shareActive);
  document.querySelector(".theme-switch")?.addEventListener("click", (ev) => {
    const btn = ev.target.closest("[data-theme-choice]");
    if (btn) setTheme(btn.dataset.themeChoice);
  });
  window.matchMedia("(prefers-color-scheme: light)").addEventListener("change", () => {
    if ((document.documentElement.dataset.theme || "system") === "system") applyTerminalThemes();
  });
  syncThemeSwitch();
  els.gateForm.addEventListener("submit", (ev) => {
    ev.preventDefault();
    state.name = els.gateForm.name.value.trim() || "guest";
    state.token = els.gateForm.token.value.trim();
    boot();
  });

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

  if (!state.name) showGate();
  else boot();
})();
