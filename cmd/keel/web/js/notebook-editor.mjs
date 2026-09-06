// Document-first notebook editor (ProseMirror + CommonMark).
// Rebuild vendor bundle (no app bundler): copy this file to a temp dir that
// has node_modules, then:
//   esbuild entry.mjs --bundle --format=iife --global-name=KeelMarkdownEditor \
//     --outfile=cmd/keel/web/vendor/notebook/editor.js --minify
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
    this.copyBtn = document.createElement("button");
    this.copyBtn.type = "button";
    this.copyBtn.className = "tiny notebook-fence-copy";
    this.copyBtn.dataset.notebookCopy = "1";
    this.copyBtn.textContent = "Copy";
    this.copyBtn.title = "Copy fence to clipboard";
    this.copyBtn.addEventListener("mousedown", (ev) => ev.preventDefault());
    this.copyBtn.addEventListener("click", (ev) => {
      ev.preventDefault();
      ev.stopPropagation();
      opts.onCopyFence?.(this.nodeText());
    });
    this.runBtn = document.createElement("button");
    this.runBtn.type = "button";
    this.runBtn.className = "tiny notebook-fence-run";
    this.runBtn.dataset.notebookFence = "1";
    this.runBtn.textContent = opts.buttonLabel || "Run in terminal";
    this.runBtn.title = "Fill CLI input — nothing is typed until you confirm";
    this.runBtn.addEventListener("mousedown", (ev) => ev.preventDefault());
    this.runBtn.addEventListener("click", (ev) => {
      ev.preventDefault();
      ev.stopPropagation();
      opts.onRunFence?.(this.nodeText());
    });
    this.bar.append(this.select, this.copyBtn, this.runBtn);
    this.pre = document.createElement("pre");
    this.contentDOM = document.createElement("code");
    this.pre.appendChild(this.contentDOM);
    this.dom.append(this.bar, this.pre);
    this.sync(node);
  }

  nodeText() {
    return this.node?.textContent || this.contentDOM.textContent || "";
  }

  sync(node) {
    this.node = node;
    const lang = langOf(node.attrs.params);
    this.dom.classList.toggle("is-shell", lang === "sh" || lang === "bash" || lang === "shell");
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
}

export function mount(el, opts = {}) {
  if (!el) throw new Error("KeelMarkdownEditor.mount requires an element");
  const onChange = typeof opts.onChange === "function" ? opts.onChange : () => {};
  const onRunFence = typeof opts.onRunFence === "function" ? opts.onRunFence : () => {};
  const onCopyFence = typeof opts.onCopyFence === "function" ? opts.onCopyFence : () => {};
  const fenceOpts = {
    buttonLabel: opts.buttonLabel || "Run in terminal",
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
      spellcheck: "true",
      role: "textbox",
      "aria-label": opts.ariaLabel || "Notebook markdown",
      "data-notebook-editor": "1",
    },
    nodeViews: {
      code_block(node, editorView, getPos) {
        return new CodeBlockView(node, editorView, getPos, fenceOpts);
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
    setMarkdown(md) {
      const doc = parseMarkdown(md);
      const next = EditorState.create({ doc, plugins: view.state.plugins });
      view.updateState(next);
    },
    destroy() {
      view.destroy();
    },
  };
}
