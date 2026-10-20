// Document-first notebook editor (ProseMirror + CommonMark).
// Rebuild the vendor bundle with `make notebook-editor` (runs
// notebook-editor.build.sh: pinned deps in a temp dir, then
//   esbuild entry.mjs --bundle --format=iife --global-name=KeelMarkdownEditor \
//     --outfile=cmd/keel/web/vendor/notebook/editor.js --minify).
// Never hand-edit vendor/notebook/editor.js; commit the .mjs and the bundle together.
import { EditorState } from "prosemirror-state";
import { EditorView } from "prosemirror-view";
import { defaultMarkdownParser, defaultMarkdownSerializer, schema } from "prosemirror-markdown";
import { keymap } from "prosemirror-keymap";
import { baseKeymap, toggleMark, setBlockType, chainCommands, exitCode } from "prosemirror-commands";
import { history, undo, redo } from "prosemirror-history";
import { dropCursor } from "prosemirror-dropcursor";
import { gapCursor } from "prosemirror-gapcursor";
import {
  inputRules,
  wrappingInputRule,
  textblockTypeInputRule,
  smartQuotes,
  emDash,
  ellipsis,
} from "prosemirror-inputrules";

const FENCE_LANGS = ["", "sh", "bash", "shell", "js", "ts", "python", "go", "json", "yaml", "toml", "sql", "html", "css", "md"];

function langOf(params) {
  return String(params || "")
    .trim()
    .split(/[\s{]/)[0]
    .toLowerCase();
}

function emptyDoc() {
  return schema.nodes.doc.createAndFill() || schema.nodes.doc.create(null, schema.nodes.paragraph.create());
}

function parseMarkdown(md) {
  const src = String(md || "").replace(/^\uFEFF/, "");
  try {
    const doc = defaultMarkdownParser.parse(src || " ");
    return doc.childCount ? doc : emptyDoc();
  } catch {
    return emptyDoc();
  }
}

function markdownInputRules() {
  return inputRules({
    rules: [
      ...smartQuotes,
      ellipsis,
      emDash,
      wrappingInputRule(/^\s*>\s$/, schema.nodes.blockquote),
      wrappingInputRule(/^\s*([-+*])\s$/, schema.nodes.bullet_list),
      wrappingInputRule(/^(\d+)\.\s$/, schema.nodes.ordered_list),
      textblockTypeInputRule(/^```(sh|bash|shell|js|ts|python|go|json|yaml|toml|sql|html|css|md)\s$/, schema.nodes.code_block, (match) => ({
        params: match[1],
      })),
      textblockTypeInputRule(/^```$/, schema.nodes.code_block),
      textblockTypeInputRule(/^(#{1,6})\s$/, schema.nodes.heading, (match) => ({ level: match[1].length })),
    ],
  });
}

function editorKeymap() {
  return keymap({
    "Mod-b": toggleMark(schema.marks.strong),
    "Mod-i": toggleMark(schema.marks.em),
    "Mod-`": toggleMark(schema.marks.code),
    "Shift-Ctrl-\\": setBlockType(schema.nodes.code_block),
    "Mod-z": undo,
    "Mod-y": redo,
    "Mod-Shift-z": redo,
    "Mod-Enter": chainCommands(exitCode, (state, dispatch) => {
      if (dispatch) dispatch(state.tr.replaceSelectionWith(schema.nodes.hard_break.create()).scrollIntoView());
      return true;
    }),
  });
}

function fillLangSelect(select, lang) {
  const current = lang || "";
  const langs = FENCE_LANGS.includes(current) ? FENCE_LANGS : [current, ...FENCE_LANGS];
  select.replaceChildren();
  for (const value of langs) {
    const opt = document.createElement("option");
    opt.value = value;
    opt.textContent = value || "plain";
    select.appendChild(opt);
  }
  select.value = current;
}

// Only shell fences get the Type action; every fence gets Copy.
const SHELL_LANGS = new Set(["sh", "bash", "shell"]);

// Icons (hugeicons stroke set, as in term.js), static markup only.
function icon(paths) {
  return `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${paths}</svg>`;
}
const ICON_COPY = icon(
  '<path d="M7.5 14.5C7.5 11.2 7.5 9.55 8.53 8.53C9.55 7.5 11.2 7.5 14.5 7.5C17.8 7.5 19.45 7.5 20.47 8.53C21.5 9.55 21.5 11.2 21.5 14.5C21.5 17.8 21.5 19.45 20.47 20.47C19.45 21.5 17.8 21.5 14.5 21.5C11.2 21.5 9.55 21.5 8.53 20.47C7.5 19.45 7.5 17.8 7.5 14.5Z"/><path d="M7.5 16.5C6.1 16.5 5.41 16.5 4.84 16.3C3.84 15.95 3.05 15.16 2.7 14.16C2.5 13.59 2.5 12.9 2.5 11.5V9.5C2.5 6.2 2.5 4.55 3.53 3.53C4.55 2.5 6.2 2.5 9.5 2.5H11.5C12.9 2.5 13.59 2.5 14.16 2.7C15.16 3.05 15.95 3.84 16.3 4.84C16.5 5.41 16.5 6.1 16.5 7.5"/>',
);
const ICON_TERMINAL = icon(
  '<path d="M7.5 7.5L8.73 8.56C9.24 9 9.5 9.22 9.5 9.5C9.5 9.78 9.24 10 8.73 10.44L7.5 11.5"/><path d="M11.5 12.5H15.5"/><path d="M12 21C15.75 21 17.62 21 18.94 20.05C19.36 19.74 19.74 19.36 20.05 18.94C21 17.62 21 15.75 21 12C21 8.25 21 6.38 20.05 5.06C19.74 4.64 19.36 4.26 18.94 3.95C17.62 3 15.75 3 12 3C8.25 3 6.38 3 5.06 3.95C4.64 4.26 4.26 4.64 3.95 5.06C3 6.38 3 8.25 3 12C3 15.75 3 17.62 3.95 18.94C4.26 19.36 4.64 19.74 5.06 20.05C6.38 21 8.25 21 12 21Z"/>',
);
const ICON_INFO = icon(
  '<path d="M22 12C22 6.48 17.52 2 12 2C6.48 2 2 6.48 2 12C2 17.52 6.48 22 12 22C17.52 22 22 17.52 22 12Z"/><path d="M12 16.5V11.5"/><path d="M12 8.01V8" stroke-width="2.2"/>',
);

// runLabel is the Type button's text: it names the pane the command goes to.
function runLabel(target) {
  return target ? `Type into ${target}` : "Type into pane";
}

function runTitle(target) {
  return `Shows the command for ${target || "the terminal in this tab"} first. Nothing is typed until you confirm.`;
}

// The fence: one box on the stage with a quiet 32px bar inside it (language,
// Copy, Type into <pane>); the code never wraps and scrolls sideways.
class CodeBlockView {
  constructor(node, view, getPos, opts) {
    this.opts = opts;
    this.view = view;
    this.getPos = getPos;
    this.node = node;
    this.dom = document.createElement("div");
    this.dom.className = "notebook-fence";
    this.bar = document.createElement("div");
    this.bar.className = "notebook-fence-bar";
    this.bar.contentEditable = "false";
    this.select = document.createElement("select");
    this.select.className = "notebook-fence-lang";
    this.select.setAttribute("aria-label", "Code language");
    this.select.addEventListener("mousedown", (ev) => ev.stopPropagation());
    this.select.addEventListener("change", (ev) => {
      ev.preventDefault();
      ev.stopPropagation();
      const pos = typeof this.getPos === "function" ? this.getPos() : null;
      if (typeof pos !== "number") return;
      const params = this.select.value;
      this.view.dispatch(this.view.state.tr.setNodeMarkup(pos, null, { params }));
    });
    const gap = document.createElement("span");
    gap.className = "notebook-fence-gap";
    this.copyBtn = document.createElement("button");
    this.copyBtn.type = "button";
    this.copyBtn.className = "k-btn k-btn--ghost k-btn--sm k-btn--icon notebook-fence-copy";
    this.copyBtn.dataset.notebookCopy = "1";
    this.copyBtn.innerHTML = ICON_COPY;
    this.copyBtn.setAttribute("aria-label", "Copy code");
    this.copyBtn.title = "Copy";
    this.copyBtn.addEventListener("mousedown", (ev) => ev.preventDefault());
    this.copyBtn.addEventListener("click", (ev) => {
      ev.preventDefault();
      ev.stopPropagation();
      opts.onCopyFence?.(this.nodeText());
    });
    this.runBtn = document.createElement("button");
    this.runBtn.type = "button";
    this.runBtn.className = "k-btn k-btn--ghost k-btn--sm notebook-fence-run";
    this.runBtn.dataset.notebookFence = "1";
    this.runBtn.innerHTML = ICON_TERMINAL;
    this.runText = document.createElement("span");
    this.runText.dataset.notebookTarget = "1";
    this.runBtn.appendChild(this.runText);
    this.setTarget(opts.target);
    this.runBtn.addEventListener("mousedown", (ev) => ev.preventDefault());
    this.runBtn.addEventListener("click", (ev) => {
      ev.preventDefault();
      ev.stopPropagation();
      opts.onRunFence?.(this.nodeText());
    });
    this.bar.append(this.select, gap, this.copyBtn, this.runBtn);
    this.pre = document.createElement("pre");
    this.contentDOM = document.createElement("code");
    this.pre.appendChild(this.contentDOM);
    this.dom.append(this.bar, this.pre);
    this.sync(node);
  }

  nodeText() {
    return this.node?.textContent || this.contentDOM.textContent || "";
  }

  setTarget(target) {
    if (!this.runText) return;
    this.runText.textContent = runLabel(target);
    this.runBtn.title = runTitle(target);
  }

  sync(node) {
    this.node = node;
    const lang = langOf(node.attrs.params);
    const shell = SHELL_LANGS.has(lang);
    this.dom.classList.toggle("is-shell", shell);
    this.runBtn.hidden = !shell;
    this.contentDOM.className = lang ? `language-${lang}` : "";
    if (this.select.value !== lang) fillLangSelect(this.select, lang);
  }

  update(node) {
    if (node.type !== schema.nodes.code_block) return false;
    this.sync(node);
    return true;
  }

  ignoreMutation(rec) {
    return rec.type === "attributes" || this.bar.contains(rec.target);
  }

  destroy() {
    this.opts.views?.delete(this);
    this.select = null;
    this.copyBtn = null;
    this.runBtn = null;
    this.runText = null;
    this.view = null;
    this.contentDOM = null;
    this.dom = null;
  }
}

// A blockquote renders as a callout: a box with an icon, not a side bar.
class CalloutView {
  constructor() {
    this.dom = document.createElement("blockquote");
    this.dom.className = "notebook-callout";
    this.dom.setAttribute("role", "note");
    const mark = document.createElement("span");
    mark.className = "k-i notebook-callout-icon";
    mark.contentEditable = "false";
    mark.innerHTML = ICON_INFO;
    this.icon = mark;
    this.contentDOM = document.createElement("div");
    this.contentDOM.className = "notebook-callout-body";
    this.dom.append(mark, this.contentDOM);
  }

  ignoreMutation(rec) {
    return rec.type !== "selection" && this.icon.contains(rec.target);
  }

  destroy() {
    this.icon = null;
    this.contentDOM = null;
    this.dom = null;
  }
}

export function mount(el, opts = {}) {
  if (!el) throw new Error("KeelMarkdownEditor.mount requires an element");
  const onChange = typeof opts.onChange === "function" ? opts.onChange : () => {};
  const onRunFence = typeof opts.onRunFence === "function" ? opts.onRunFence : () => {};
  const onCopyFence = typeof opts.onCopyFence === "function" ? opts.onCopyFence : () => {};
  // target names the pane a shell fence types into ("" when the tab has
  // no terminal); setTarget renames every fence's button when it changes.
  const views = new Set();
  const fenceOpts = {
    target: String(opts.target || ""),
    views,
    onRunFence,
    onCopyFence,
  };
  const state = EditorState.create({
    doc: parseMarkdown(opts.markdown),
    plugins: [history(), markdownInputRules(), editorKeymap(), keymap(baseKeymap), dropCursor(), gapCursor()],
  });
  const view = new EditorView(el, {
    state,
    attributes: {
      class: "notebook-prose",
      // Prose keeps spellcheck; autocapitalize and autocorrect stay off in
      // the native WKWebView (code fences and commands share the body).
      spellcheck: "true",
      autocapitalize: "off",
      autocorrect: "off",
      role: "textbox",
      "aria-label": opts.ariaLabel || "Notebook markdown",
      "data-notebook-editor": "1",
    },
    nodeViews: {
      code_block(node, editorView, getPos) {
        const v = new CodeBlockView(node, editorView, getPos, fenceOpts);
        views.add(v);
        return v;
      },
      blockquote() {
        return new CalloutView();
      },
    },
    dispatchTransaction(tr) {
      const next = view.state.apply(tr);
      view.updateState(next);
      if (tr.docChanged) onChange(defaultMarkdownSerializer.serialize(next.doc));
    },
  });
  return {
    view,
    getMarkdown() {
      return defaultMarkdownSerializer.serialize(view.state.doc);
    },
    setTarget(target) {
      const next = String(target || "");
      if (next === fenceOpts.target) return;
      fenceOpts.target = next;
      for (const v of views) v.setTarget(next);
    },
    setMarkdown(md) {
      const doc = parseMarkdown(md);
      const next = EditorState.create({ doc, plugins: view.state.plugins });
      view.updateState(next);
    },
    destroy() {
      try {
        view.destroy();
      } catch {
        /* already gone */
      }
      el.replaceChildren();
    },
  };
}
