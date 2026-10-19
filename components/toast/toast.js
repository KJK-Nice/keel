(function () {
  "use strict";

  // Vanilla port of shadcn's base/toast (Base UI Toast): the class strings,
  // CSS variables and stacking behavior mirror components/ui/toast.tsx.

  var ENTER_EXIT_MS = 500;
  var SWIPE_THRESHOLD = 45;
  var GAP = 12; // --gap: 0.75rem

  var TOAST_CLASS = [
    "cn-toast group/toast pointer-events-auto absolute right-0 bottom-0 z-[calc(1000-var(--toast-index))] w-full origin-bottom rounded-lg border bg-popover text-popover-foreground shadow-lg will-change-transform outline-none select-none focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/50",
    "[--gap:0.75rem] [--height:var(--toast-frontmost-height,var(--toast-height))] [--offset-y:calc(var(--toast-offset-y)*-1+calc(var(--toast-index)*var(--gap)*-1)+var(--toast-swipe-movement-y))] [--peek:0.75rem] [--scale:calc(max(0,1-(var(--toast-index)*0.1)))] [--shrink:calc(1-var(--scale))]",
    "h-(--height) [transform:translateX(var(--toast-swipe-movement-x))_translateY(calc(var(--toast-swipe-movement-y)-(var(--toast-index)*var(--peek))-(var(--shrink)*var(--height))))_scale(var(--scale))] [transition:transform_500ms_cubic-bezier(0.22,1,0.36,1),opacity_500ms,height_150ms]",
    "after:absolute after:top-full after:left-0 after:h-[calc(var(--gap)+1px)] after:w-full after:content-['']",
    "data-expanded:h-(--toast-height) data-expanded:[transform:translateX(var(--toast-swipe-movement-x))_translateY(var(--offset-y))]",
    "data-limited:opacity-0 data-starting-style:[transform:translateY(150%)]",
    "[&[data-ending-style]:not([data-limited]):not([data-swipe-direction])]:[transform:translateY(150%)]",
    "data-ending-style:data-[swipe-direction=down]:[transform:translateY(calc(var(--toast-swipe-movement-y)+150%))]",
    "data-ending-style:data-[swipe-direction=left]:[transform:translateX(calc(var(--toast-swipe-movement-x)-150%))_translateY(var(--offset-y))]",
    "data-ending-style:data-[swipe-direction=right]:[transform:translateX(calc(var(--toast-swipe-movement-x)+150%))_translateY(var(--offset-y))]",
    "data-ending-style:data-[swipe-direction=up]:[transform:translateY(calc(var(--toast-swipe-movement-y)-150%))]",
    "data-expanded:data-ending-style:data-[swipe-direction=down]:[transform:translateY(calc(var(--toast-swipe-movement-y)+150%))]",
    "data-expanded:data-ending-style:data-[swipe-direction=left]:[transform:translateX(calc(var(--toast-swipe-movement-x)-150%))_translateY(var(--offset-y))]",
    "data-expanded:data-ending-style:data-[swipe-direction=right]:[transform:translateX(calc(var(--toast-swipe-movement-x)+150%))_translateY(var(--offset-y))]",
    "data-expanded:data-ending-style:data-[swipe-direction=up]:[transform:translateY(calc(var(--toast-swipe-movement-y)-150%))]",
  ].join(" ");

  var CONTENT_CLASS =
    "flex w-full items-center gap-3 overflow-hidden p-4 transition-opacity duration-250 ease-[cubic-bezier(0.22,1,0.36,1)] data-behind:opacity-0 data-expanded:opacity-100";
  var TITLE_CLASS = "text-sm font-medium leading-snug";
  var DESCRIPTION_CLASS = "text-sm text-muted-foreground leading-snug";
  var ICON_CLASS =
    "shrink-0 [&_svg]:pointer-events-none [&_svg:not([class*='size-'])]:size-4";

  var BUTTON_BASE =
    "inline-flex shrink-0 items-center justify-center whitespace-nowrap rounded-lg border border-transparent text-sm font-medium transition-all outline-none select-none disabled:pointer-events-none disabled:opacity-50 [&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-4";
  var ACTION_CLASS =
    BUTTON_BASE +
    " border-border bg-background hover:bg-muted hover:text-foreground dark:bg-input/30 dark:border-input dark:hover:bg-input/50 h-7 gap-1 px-2.5 text-[0.8rem]";
  var CLOSE_CLASS =
    BUTTON_BASE +
    " size-7 rounded-[min(var(--radius-md),12px)] text-muted-foreground hover:bg-muted hover:text-foreground dark:hover:bg-muted/50 relative after:absolute after:-inset-2 after:content-['']";

  var SVG =
    '<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"';
  var ICONS = {
    success:
      SVG +
      ' aria-hidden="true"><path d="M22 12C22 6.47715 17.5228 2 12 2C6.47715 2 2 6.47715 2 12C2 17.5228 6.47715 22 12 22C17.5228 22 22 17.5228 22 12Z" /><path d="M8 12.5L10.5 15L16 9" /></svg>',
    info:
      SVG +
      ' aria-hidden="true"><circle cx="12" cy="12" r="10" /><path d="M12 16V12" /><path d="M12.125 8.25H12M12.25 8.25C12.25 8.11193 12.1381 8 12 8C11.8619 8 11.75 8.11193 11.75 8.25C11.75 8.38807 11.8619 8.5 12 8.5C12.1381 8.5 12.25 8.38807 12.25 8.25Z" /></svg>',
    warning:
      SVG +
      ' aria-hidden="true"><path d="M13.9248 21H10.0752C5.44476 21 3.12955 21 2.27636 19.4939C1.42317 17.9879 2.60736 15.9914 4.97574 11.9985L6.90057 8.75333C9.17559 4.91778 10.3131 3 12 3C13.6869 3 14.8244 4.91777 17.0994 8.75332L19.0243 11.9985C21.3926 15.9914 22.5768 17.9879 21.7236 19.4939C20.8704 21 18.5552 21 13.9248 21Z" /><path d="M12 9V13" /><path d="M12.125 16.75H12M12.25 16.75C12.25 16.8881 12.1381 17 12 17C11.8619 17 11.75 16.8881 11.75 16.75C11.75 16.6119 11.8619 16.5 12 16.5C12.1381 16.5 12.25 16.6119 12.25 16.75Z" /></svg>',
    error:
      SVG +
      ' class="text-destructive" aria-hidden="true"><path d="M5.92089 5.92089C8.15836 3.68342 9.2771 2.56468 10.5857 2.19562C11.5105 1.93479 12.4895 1.93479 13.4143 2.19562C14.7229 2.56468 15.8416 3.68342 18.0791 5.92089C20.3166 8.15836 21.4353 9.2771 21.8044 10.5857C22.0652 11.5105 22.0652 12.4895 21.8044 13.4143C21.4353 14.7229 20.3166 15.8416 18.0791 18.0791C15.8416 20.3166 14.7229 21.4353 13.4143 21.8044C12.4895 22.0652 11.5105 22.0652 10.5857 21.8044C9.2771 21.4353 8.15836 20.3166 5.92089 18.0791C3.68342 15.8416 2.56468 14.7229 2.19562 13.4143C1.93479 12.4895 1.93479 11.5105 2.19562 10.5857C2.56468 9.2771 3.68342 8.15836 5.92089 5.92089Z" /><path d="M12 8V12" /><path d="M12.125 15.75H12M12.25 15.75C12.25 15.8881 12.1381 16 12 16C11.8619 16 11.75 15.8881 11.75 15.75C11.75 15.6119 11.8619 15.5 12 15.5C12.1381 15.5 12.25 15.6119 12.25 15.75Z" /></svg>',
    loading:
      SVG +
      ' class="animate-spin" aria-hidden="true"><path d="M12 3V6" /><path d="M12 18V21" /><path d="M21 12L18 12" /><path d="M6 12L3 12" /><path d="M18.3635 5.63672L16.2422 7.75804" /><path d="M7.75804 16.2422L5.63672 18.3635" /><path d="M18.3635 18.3635L16.2422 16.2422" /><path d="M7.75804 7.75804L5.63672 5.63672" /></svg>',
    close:
      SVG +
      ' aria-hidden="true"><path d="M18 6L6.00081 17.9992M17.9992 18L6 6.00085" /></svg>',
  };

  function viewportOf(el) {
    return el ? el.closest("[data-tui-toaster]") : document.querySelector("[data-tui-toaster]");
  }

  function toastsOf(vp) {
    // Newest first, like Base UI's toast list; leaving toasts keep animating
    // but no longer take part in the layout.
    return Array.from(vp.querySelectorAll('[data-slot="toast"]:not([data-ending-style])')).reverse();
  }

  // ----- layout: the Base UI stacking variables -----------------------------

  function layout(vp) {
    var list = toastsOf(vp);
    var limit = parseInt(vp.getAttribute("data-tui-toaster-limit"), 10) || 3;
    var expanded = vp.hasAttribute("data-expanded");

    // Natural heights first: with the per-toast vars cleared, h-(--height)
    // resolves to auto.
    list.forEach(function (t) {
      t.style.removeProperty("--toast-height");
      t.style.removeProperty("--toast-frontmost-height");
    });
    var heights = list.map(function (t) {
      return t.offsetHeight;
    });

    var offset = 0;
    list.forEach(function (t, i) {
      t.style.setProperty("--toast-index", String(i));
      t.style.setProperty("--toast-height", heights[i] + "px");
      if (i > 0) {
        t.style.setProperty("--toast-frontmost-height", heights[0] + "px");
      }
      // --toast-offset-y carries only the summed heights, the class formula
      // adds index*gap on top.
      t.style.setProperty("--toast-offset-y", offset + "px");
      offset += heights[i];
      t.toggleAttribute("data-limited", i >= limit);
      t.toggleAttribute("data-expanded", expanded);
      var content = t.querySelector('[data-slot="toast-content"]');
      if (content) {
        content.toggleAttribute("data-behind", i > 0 && !expanded);
        content.toggleAttribute("data-expanded", expanded || i === 0);
      }
    });
  }

  // ----- timers -------------------------------------------------------------

  function startTimer(t) {
    if (t.getAttribute("data-type") === "loading") return;
    var vp = viewportOf(t);
    var timeout = parseInt(t.getAttribute("data-tui-toast-timeout"), 10);
    if (!timeout) {
      timeout = parseInt(vp.getAttribute("data-tui-toaster-timeout"), 10) || 5000;
    }
    var remaining = t._tuiRemaining != null ? t._tuiRemaining : timeout;
    t._tuiDeadline = Date.now() + remaining;
    t._tuiTimer = window.setTimeout(function () {
      dismiss(t);
    }, remaining);
  }

  function stopTimer(t) {
    if (t._tuiTimer) {
      window.clearTimeout(t._tuiTimer);
      t._tuiTimer = null;
      t._tuiRemaining = Math.max(0, (t._tuiDeadline || 0) - Date.now());
    }
  }

  // ----- create / dismiss ---------------------------------------------------

  var seq = 0;

  function build(opts) {
    var t = document.createElement("div");
    t.className = TOAST_CLASS;
    t.setAttribute("data-slot", "toast");
    t.setAttribute("role", "status");
    t.setAttribute("aria-atomic", "true");
    t.id = opts.id || "tui-toast-" + ++seq;
    if (opts.type) t.setAttribute("data-type", opts.type);
    if (opts.timeout) t.setAttribute("data-tui-toast-timeout", String(opts.timeout));
    t.style.setProperty("--toast-swipe-movement-x", "0px");
    t.style.setProperty("--toast-swipe-movement-y", "0px");

    var content = document.createElement("div");
    content.className = CONTENT_CLASS;
    content.setAttribute("data-slot", "toast-content");

    var iconEl = document.createElement("span");
    iconEl.className = ICON_CLASS;
    iconEl.setAttribute("data-slot", "toast-icon");
    var showIcon = opts.type === "error" || opts.type === "warning" || opts.type === "info" || opts.type === "loading";
    if (showIcon && ICONS[opts.type]) {
      iconEl.innerHTML = ICONS[opts.type];
    } else {
      iconEl.hidden = true;
    }

    var textWrap = document.createElement("div");
    textWrap.className = "flex min-w-0 flex-1 flex-col gap-1";
    var titleEl = document.createElement("div");
    titleEl.className = TITLE_CLASS;
    titleEl.setAttribute("data-slot", "toast-title");
    if (opts.title) titleEl.textContent = opts.title;
    else titleEl.hidden = true;
    var descEl = document.createElement("div");
    descEl.className = DESCRIPTION_CLASS;
    descEl.setAttribute("data-slot", "toast-description");
    if (opts.description) descEl.textContent = opts.description;
    else descEl.hidden = true;
    textWrap.appendChild(titleEl);
    textWrap.appendChild(descEl);

    var actionsWrap = document.createElement("div");
    actionsWrap.className = "flex shrink-0 items-center gap-1";

    if (opts.action) {
      var actionEl = document.createElement("button");
      actionEl.type = "button";
      actionEl.className = ACTION_CLASS;
      actionEl.setAttribute("data-slot", "toast-action");
      actionEl.textContent = opts.action.label || "";
      if (typeof opts.action.onClick === "function") {
        actionEl.addEventListener("click", opts.action.onClick);
      }
      actionsWrap.appendChild(actionEl);
    }

    var closeEl = document.createElement("button");
    closeEl.type = "button";
    closeEl.className = CLOSE_CLASS;
    closeEl.setAttribute("data-slot", "toast-close");
    closeEl.setAttribute("aria-label", "Close toast");
    closeEl.innerHTML = ICONS.close;
    actionsWrap.appendChild(closeEl);

    if (!iconEl.hidden) content.appendChild(iconEl);
    content.appendChild(textWrap);
    content.appendChild(actionsWrap);

    t.appendChild(content);
    return t;
  }

  function createToast(opts) {
    var vp = viewportOf(null);
    if (!vp) return null;
    var t = build(opts);
    t.setAttribute("data-starting-style", "");
    vp.appendChild(t);
    layout(vp);
    // Enter: flush the starting transform, then transition into place
    // (setTimeout instead of rAF so background tabs still settle).
    void t.offsetHeight;
    window.setTimeout(function () {
      t.removeAttribute("data-starting-style");
    }, 20);
    startTimer(t);
    return t;
  }

  function dismiss(t, direction) {
    if (!t || t.hasAttribute("data-ending-style")) return;
    stopTimer(t);
    var vp = viewportOf(t);
    t.setAttribute("data-ending-style", "");
    if (direction) t.setAttribute("data-swipe-direction", direction);
    window.setTimeout(function () {
      t.remove();
      if (vp) layout(vp);
    }, ENTER_EXIT_MS);
    if (vp) layout(vp);
  }

  // ----- expand on hover ----------------------------------------------------

  function setExpanded(vp, expanded) {
    if (vp.hasAttribute("data-expanded") === expanded) return;
    vp.toggleAttribute("data-expanded", expanded);
    toastsOf(vp).forEach(expanded ? stopTimer : startTimer);
    layout(vp);
  }

  document.addEventListener("pointerover", function (e) {
    if (!(e.target instanceof Element)) return;
    var vp = e.target.closest("[data-tui-toaster]");
    if (vp) setExpanded(vp, true);
  });

  document.addEventListener("pointerout", function (e) {
    if (!(e.target instanceof Element)) return;
    var vp = e.target.closest("[data-tui-toaster]");
    if (!vp) return;
    if (e.relatedTarget instanceof Element && e.relatedTarget.closest("[data-tui-toaster]") === vp) return;
    setExpanded(vp, false);
  });

  // ----- close button -------------------------------------------------------

  document.addEventListener("click", function (e) {
    if (!(e.target instanceof Element)) return;
    var close = e.target.closest('[data-slot="toast-close"]');
    if (close) dismiss(close.closest('[data-slot="toast"]'));
  });

  // ----- swipe to dismiss (down and right, the bottom-right defaults) -------

  document.addEventListener("pointerdown", function (e) {
    if (!(e.target instanceof Element)) return;
    var t = e.target.closest('[data-slot="toast"]');
    if (!t || e.target.closest("button")) return;
    t._tuiSwipe = { x: e.clientX, y: e.clientY };
  });

  document.addEventListener("pointermove", function (e) {
    if (!(e.target instanceof Element)) return;
    var t = e.target.closest('[data-slot="toast"]');
    if (!t || !t._tuiSwipe) return;
    var dx = Math.max(0, e.clientX - t._tuiSwipe.x);
    var dy = Math.max(0, e.clientY - t._tuiSwipe.y);
    t.style.setProperty("--toast-swipe-movement-x", dx + "px");
    t.style.setProperty("--toast-swipe-movement-y", dy + "px");
  });

  document.addEventListener("pointerup", function (e) {
    if (!(e.target instanceof Element)) return;
    var t = e.target.closest('[data-slot="toast"]');
    if (!t || !t._tuiSwipe) return;
    var dx = Math.max(0, e.clientX - t._tuiSwipe.x);
    var dy = Math.max(0, e.clientY - t._tuiSwipe.y);
    t._tuiSwipe = null;
    if (dy >= SWIPE_THRESHOLD && dy >= dx) {
      dismiss(t, "down");
    } else if (dx >= SWIPE_THRESHOLD) {
      dismiss(t, "right");
    } else {
      t.style.setProperty("--toast-swipe-movement-x", "0px");
      t.style.setProperty("--toast-swipe-movement-y", "0px");
    }
  });

  // ----- the toast manager pendant: add, close, promise ---------------------

  function normalize(opts) {
    opts = Object.assign({}, opts);
    if (opts.actionProps) {
      opts.action = {
        label: opts.actionProps.children,
        onClick: opts.actionProps.onClick,
      };
    }
    return opts;
  }

  var api = {
    add: function (opts) {
      var t = createToast(normalize(opts || {}));
      return t ? t.id : null;
    },
    close: function (id) {
      if (id === undefined) {
        document.querySelectorAll('[data-slot="toast"]').forEach(function (t) {
          dismiss(t);
        });
        return;
      }
      var el = typeof id === "string" ? document.getElementById(id) : id;
      if (el) dismiss(el);
    },
    // toast.promise: a loading toast that morphs with the promise.
    promise: function (promise, opts) {
      opts = opts || {};
      var t = createToast(Object.assign({}, normalize(opts), { type: "loading", title: opts.loading || "Loading..." }));
      if (!t) return null;
      var p = typeof promise === "function" ? promise() : promise;
      function morph(type, title) {
        if (!t.isConnected) return;
        t.setAttribute("data-type", type);
        var iconEl = t.querySelector('[data-slot="toast-icon"]');
        if (iconEl) {
          iconEl.hidden = false;
          iconEl.innerHTML = ICONS[type] || "";
        }
        var titleEl = t.querySelector('[data-slot="toast-title"]');
        if (titleEl) {
          titleEl.hidden = false;
          titleEl.textContent = title;
        }
        var vp = viewportOf(t);
        if (vp) layout(vp);
        t._tuiRemaining = null;
        startTimer(t);
      }
      Promise.resolve(p)
        .then(function (data) {
          morph("success", typeof opts.success === "function" ? opts.success(data) : opts.success || "Done");
        })
        .catch(function (err) {
          morph("error", typeof opts.error === "function" ? opts.error(err) : opts.error || "Error");
        });
      return t.id;
    },
  };

  window.tui = window.tui || {};
  window.tui.toast = api;

  // ----- SSR/htmx adoption --------------------------------------------------

  function init() {
    document.querySelectorAll("[data-tui-toast-ssr]").forEach(function (stub) {
      var opts = {
        id: stub.id || undefined,
        title: stub.getAttribute("data-tui-toast-title") || "",
        description: stub.getAttribute("data-tui-toast-description") || "",
        type: stub.getAttribute("data-type") || "",
        timeout: parseInt(stub.getAttribute("data-tui-toast-timeout"), 10) || 0,
      };
      stub.remove();
      createToast(opts);
    });
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", init);
  } else {
    init();
  }
  // Re-init on any childList mutation, directly (never rAF-deferred: rAF
  // does not fire in hidden tabs or throttled iframes): swapped-in markup
  // wires itself.
  // Skip mutations that cannot hold component markup: xterm's own DOM
  // (rows, measure elements, helpers), which changes on every frame of
  // terminal output, the notebook's ProseMirror document (.notebook-prose),
  // which changes on every keystroke, and nodes already detached. Each of these used to
  // re-run init(), which queries the whole document.
  function touchesComponents(records) {
    for (const r of records) {
      const t = r.target.nodeType === 1 ? r.target : r.target.parentElement;
      if (t && t.isConnected && !t.closest(".xterm, .notebook-prose")) return true;
    }
    return false;
  }
  new MutationObserver((records) => {
    if (touchesComponents(records)) init();
  }).observe(document.body, { childList: true, subtree: true });
})();
