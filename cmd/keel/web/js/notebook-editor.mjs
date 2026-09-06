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

const SHELL_LANGS = new Set(["sh", "bash", "shell"]);

function isShellLang(params) {
  const lang = String(params || "")
    .trim()
    .split(/[\s{]/)[0]
    .toLowerCase();
  return SHELL_LANGS.has(lang);
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
      textblockTypeInputRule(/^```(sh|bash|shell)\s$/, schema.nodes.code_block, (match) => ({ params: match[1] })),
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

class CodeBlockView {
  constructor(node, opts) {
    this.opts = opts;
    this.node = node;
    this.dom = document.createElement("div");
    this.dom.className = "notebook-fence";
    this.pre = document.createElement("pre");
    this.contentDOM = document.createElement("code");
    this.pre.appendChild(this.contentDOM);
    this.dom.appendChild(this.pre);
    this.btn = document.createElement("button");
    this.btn.type = "button";
    this.btn.className = "tiny notebook-fence-run";
    this.btn.dataset.notebookFence = "1";
    this.btn.textContent = opts.buttonLabel || "Insert into input";
    this.btn.title = "Fill CLI input — nothing is typed until you confirm";
    this.btn.addEventListener("mousedown", (ev) => ev.preventDefault());
    this.btn.addEventListener("click", (ev) => {
      ev.preventDefault();
      ev.stopPropagation();
      opts.onRunFence?.(this.nodeText());
    });
    this.sync(node);
  }

  nodeText() {
    return this.node?.textContent || this.contentDOM.textContent || "";
  }

  sync(node) {
    this.node = node;
    const shell = isShellLang(node.attrs.params);
    this.dom.classList.toggle("is-shell", shell);
    const lang = String(node.attrs.params || "").trim().split(/\s+/)[0];
    this.contentDOM.className = lang ? `language-${lang}` : "";
    if (shell) {
      if (!this.btn.isConnected) this.dom.appendChild(this.btn);
    } else {
      this.btn.remove();
    }
  }

  update(node) {
    if (node.type !== schema.nodes.code_block) return false;
    this.sync(node);
    return true;
  }

  ignoreMutation(rec) {
    return rec.type === "attributes" || this.btn.contains(rec.target);
  }
}

export function mount(el, opts = {}) {
  if (!el) throw new Error("KeelMarkdownEditor.mount requires an element");
  const onChange = typeof opts.onChange === "function" ? opts.onChange : () => {};
  const onRunFence = typeof opts.onRunFence === "function" ? opts.onRunFence : () => {};
  const fenceOpts = {
    buttonLabel: opts.buttonLabel || "Insert into input",
    onRunFence,
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
      code_block(node) {
        return new CodeBlockView(node, fenceOpts);
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
