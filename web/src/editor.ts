// The CodeMirror 6 editor: Markdown in the Jus material, line numbers,
// active line, folding, search, and a git gutter that marks the working
// file against HEAD. The theme reads the shared CSS variables, so day and
// night need no rebuild.

import { Annotation, EditorState, EditorSelection, StateEffect, StateField, RangeSet, RangeSetBuilder, Compartment, Transaction, type Extension } from "@codemirror/state";
import {
  EditorView,
  Decoration,
  ViewPlugin,
  GutterMarker,
  gutter,
  type DecorationSet,
  type ViewUpdate,
  lineNumbers,
  highlightActiveLine,
  highlightActiveLineGutter,
  highlightSpecialChars,
  drawSelection,
  dropCursor,
  rectangularSelection,
  crosshairCursor,
  keymap,
} from "@codemirror/view";
import { defaultKeymap, history, historyKeymap, indentWithTab } from "@codemirror/commands";
import { search, searchKeymap, highlightSelectionMatches } from "@codemirror/search";
import { markdown, markdownKeymap, markdownLanguage } from "@codemirror/lang-markdown";
import {
  syntaxHighlighting,
  syntaxTree,
  HighlightStyle,
  foldGutter,
  foldKeymap,
  indentOnInput,
  bracketMatching,
} from "@codemirror/language";
import { autocompletion, closeBrackets, closeBracketsKeymap, completionKeymap } from "@codemirror/autocomplete";
import { lintKeymap, linter, forceLinting, type Diagnostic as LintDiagnostic } from "@codemirror/lint";
import { tags } from "@lezer/highlight";
import { api, type GitChange } from "./api.ts";
import { L, prefs } from "./i18n.ts";
import { wikiMarks, wikiCompletion, wikiAt } from "./wikilinks.ts";

// Markdown marks (#, **, `, >, list bullets, link brackets) are faint, so
// the text reads first; headings are larger and bold; code is monospace.
const jusHighlight = HighlightStyle.define([
  { tag: tags.heading1, fontWeight: "700", fontSize: "1.45em" },
  { tag: tags.heading2, fontWeight: "700", fontSize: "1.25em" },
  { tag: tags.heading3, fontWeight: "700", fontSize: "1.1em" },
  { tag: [tags.heading4, tags.heading5, tags.heading6], fontWeight: "700" },
  { tag: tags.processingInstruction, color: "var(--faint)", fontWeight: "400" },
  { tag: tags.strong, fontWeight: "700" },
  { tag: tags.emphasis, fontStyle: "italic" },
  { tag: tags.strikethrough, textDecoration: "line-through", color: "var(--muted)" },
  { tag: tags.link, color: "var(--accent)" },
  { tag: tags.url, color: "var(--muted)", textDecoration: "underline", textDecorationColor: "var(--line)" },
  { tag: tags.monospace, fontFamily: "var(--font-mono)", color: "var(--code)" },
  { tag: tags.quote, color: "var(--muted)" },
  { tag: tags.comment, color: "var(--faint)" },
  { tag: tags.contentSeparator, color: "var(--faint)" },
  { tag: tags.labelName, color: "var(--muted)" },
]);

// --- git gutter -----------------------------------------------------------

class GitMarker extends GutterMarker {
  constructor(readonly kind: GitChange["kind"]) {
    super();
  }
  eq(other: GitMarker) {
    return other.kind === this.kind;
  }
  toDOM() {
    const d = document.createElement("div");
    d.className = "cm-git cm-git-" + this.kind;
    return d;
  }
}
const markers = { add: new GitMarker("add"), modify: new GitMarker("modify"), delete: new GitMarker("delete") };

const setGit = StateEffect.define<GitChange[]>();

const gitField = StateField.define<RangeSet<GutterMarker>>({
  create: () => new RangeSetBuilder<GutterMarker>().finish(),
  update(set, tr) {
    set = set.map(tr.changes);
    for (const e of tr.effects) {
      if (!e.is(setGit)) continue;
      const byLine = new Map<number, GitChange["kind"]>();
      for (const c of e.value) byLine.set(c.line, c.kind);
      const b = new RangeSetBuilder<GutterMarker>();
      for (const [line, kind] of [...byLine].sort((x, y) => x[0] - y[0])) {
        if (line < 1 || line > tr.state.doc.lines) continue;
        const at = tr.state.doc.line(line).from;
        b.add(at, at, markers[kind]);
      }
      set = b.finish();
    }
    return set;
  },
});

const gitGutter = [
  gitField,
  gutter({ class: "cm-git-gutter", markers: (v) => v.state.field(gitField), initialSpacer: () => markers.add }),
];

// --- code blocks ------------------------------------------------------------

// Fenced code gets a quiet band and a monospace face, so a block reads as
// one thing even in the sans-serif text face.
const codeLine = Decoration.line({ class: "cm-codeblock" });
function buildCode(view: EditorView): DecorationSet {
  const b = new RangeSetBuilder<Decoration>();
  const doc = view.state.doc;
  for (const { from, to } of view.visibleRanges) {
    syntaxTree(view.state).iterate({
      from,
      to,
      enter(node) {
        if (node.name !== "FencedCode" && node.name !== "CodeBlock") return;
        const first = doc.lineAt(node.from).number;
        const last = doc.lineAt(node.to).number;
        for (let n = first; n <= last; n++) b.add(doc.line(n).from, doc.line(n).from, codeLine);
        return false;
      },
    });
  }
  return b.finish();
}
const codePlugin = ViewPlugin.fromClass(
  class {
    decorations: DecorationSet;
    constructor(view: EditorView) {
      this.decorations = buildCode(view);
    }
    update(u: ViewUpdate) {
      if (u.docChanged || u.viewportChanged || syntaxTree(u.startState) !== syntaxTree(u.state)) this.decorations = buildCode(u.view);
    }
  },
  { decorations: (v) => v.decorations },
);

// --- record-type checker ------------------------------------------------------

// The checker runs against the current buffer, so a badly shaped entry is
// underlined while it is being written; saving is never blocked.
let lintPath = "";

export function setLintPath(path: string): void {
  lintPath = path;
}

export function forceLint(view: EditorView): void {
  forceLinting(view);
}

const lintExt = linter(
  async (view): Promise<LintDiagnostic[]> => {
    if (!lintPath) return [];
    try {
      const ds = await api.check(lintPath, view.state.doc.toString());
      return ds.map((d) => {
        const n = Math.min(Math.max(1, d.line), view.state.doc.lines);
        const line = view.state.doc.line(n);
        const severity = d.severity === "error" ? "error" : "warning";
        return { from: line.from, to: line.to, severity, message: d.message } as LintDiagnostic;
      });
    } catch {
      return [];
    }
  },
  { delay: 500 },
);

// --- rainbow indent guides ------------------------------------------------------

// A 1px line per indent level, coloured by level (indent-rainbow). The
// positions use ch units, so they follow the font size; the per-depth rules
// are generated once.
const INDENT_SIZE = 4;
const MAX_DEPTH = 24;
const GUIDE_COLORS = ["#e06c75", "#d19a66", "#e5c07b", "#98c379", "#56b6c2", "#61afef", "#c678dd"];

function installGuideStyles(): void {
  let css = "";
  for (let d = 1; d <= MAX_DEPTH; d++) {
    const imgs: string[] = [];
    const sizes: string[] = [];
    const pos: string[] = [];
    for (let k = 1; k <= d; k++) {
      const c = GUIDE_COLORS[(k - 1) % GUIDE_COLORS.length];
      imgs.push(`linear-gradient(${c}66,${c}66)`);
      sizes.push("1px 100%");
      pos.push(`calc(${(k - 1) * INDENT_SIZE}ch + 6px) 0`);
    }
    css += `.cm-indent-d${d}{background-image:${imgs.join(",")};background-size:${sizes.join(",")};background-position:${pos.join(",")};background-repeat:no-repeat;}\n`;
  }
  const style = document.createElement("style");
  style.textContent = css;
  document.head.append(style);
}
installGuideStyles();

function indentDepth(text: string): number {
  let cols = 0;
  for (const ch of text) {
    if (ch === " ") cols++;
    else if (ch === "\t") cols += INDENT_SIZE - (cols % INDENT_SIZE);
    else break;
  }
  return Math.min(Math.floor(cols / INDENT_SIZE), MAX_DEPTH);
}

function buildGuides(view: EditorView): DecorationSet {
  const doc = view.state.doc;
  const builder = new RangeSetBuilder<Decoration>();
  for (const { from, to } of view.visibleRanges) {
    const first = doc.lineAt(from).number;
    const last = doc.lineAt(Math.min(to, doc.length)).number;
    for (let n = first; n <= last; n++) {
      const line = doc.line(n);
      const depth = indentDepth(line.text);
      if (depth > 0) builder.add(line.from, line.from, Decoration.line({ class: `cm-indent-d${depth}` }));
    }
  }
  return builder.finish();
}

const guidePlugin = ViewPlugin.fromClass(
  class {
    decorations: DecorationSet;
    constructor(view: EditorView) {
      this.decorations = buildGuides(view);
    }
    update(u: ViewUpdate) {
      if (u.docChanged || u.viewportChanged) this.decorations = buildGuides(u.view);
    }
  },
  { decorations: (v) => v.decorations },
);

// --- Markdown commands -----------------------------------------------------------

/** Wraps the selection in mark (or unwraps it when already wrapped). */
function toggleWrap(mark: string) {
  return (view: EditorView): boolean => {
    view.dispatch(
      view.state.changeByRange((range) => {
        const doc = view.state.doc;
        const n = mark.length;
        const before = doc.sliceString(range.from - n, range.from);
        const after = doc.sliceString(range.to, range.to + n);
        if (before === mark && after === mark) {
          return {
            changes: [
              { from: range.from - n, to: range.from },
              { from: range.to, to: range.to + n },
            ],
            range: EditorSelection.range(range.from - n, range.to - n),
          };
        }
        return {
          changes: [
            { from: range.from, insert: mark },
            { from: range.to, insert: mark },
          ],
          range: EditorSelection.range(range.from + n, range.to + n),
        };
      }),
    );
    return true;
  };
}


// --- links -------------------------------------------------------------------------

let onLink: (href: string) => void = () => undefined;
export function setLinkHandler(fn: (href: string) => void): void {
  onLink = fn;
}

const LINK_RE = /(jus:\/\/[^\s)>\]]+|https?:\/\/[^\s)>\]]+|mailto:[^\s)>\]]+)/g;

/** The link under pos, if any: a wiki link ("wiki:" + target), a bare URL, or a Markdown link's target. */
function linkAt(state: EditorState, pos: number): string | null {
  const line = state.doc.lineAt(pos);
  const col = pos - line.from;
  const wiki = wikiAt(line.text, col);
  if (wiki) return "wiki:" + wiki;
  for (const m of line.text.matchAll(LINK_RE)) {
    if (m.index <= col && col <= m.index + m[0].length) return m[0];
  }
  for (const m of line.text.matchAll(/\[[^\]]*\]\(([^)\s]+)[^)]*\)/g)) {
    if (m.index <= col && col <= m.index + m[0].length) return m[1];
  }
  return null;
}

// Pasted or dropped files (images) are handed to the app, which stores them
// next to the note and gets back the Markdown to insert.
let onFiles: (files: File[]) => Promise<string | null> = async () => null;
export function setFileHandler(fn: (files: File[]) => Promise<string | null>): void {
  onFiles = fn;
}

function insertFiles(view: EditorView, files: File[], pos: number) {
  void onFiles(files).then((md) => {
    if (md) view.dispatch({ changes: { from: pos, insert: md }, selection: { anchor: pos + md.length } });
  });
}

const fileDrop = EditorView.domEventHandlers({
  paste(e, view) {
    const files = [...(e.clipboardData?.files ?? [])];
    if (!files.length) return false;
    e.preventDefault();
    insertFiles(view, files, view.state.selection.main.head);
    return true;
  },
  drop(e, view) {
    const files = [...(e.dataTransfer?.files ?? [])];
    if (!files.length) return false;
    e.preventDefault();
    const pos = view.posAtCoords({ x: e.clientX, y: e.clientY }) ?? view.state.selection.main.head;
    insertFiles(view, files, pos);
    return true;
  },
});

// Ctrl+click follows a link, as in code editors.
const linkClick = EditorView.domEventHandlers({
  mousedown(e, view) {
    if (!(e.ctrlKey || e.metaKey) || e.button !== 0) return false;
    const pos = view.posAtCoords({ x: e.clientX, y: e.clientY });
    if (pos == null) return false;
    const href = linkAt(view.state, pos);
    if (!href) return false;
    e.preventDefault();
    onLink(href);
    return true;
  },
});

// --- localisation ----------------------------------------------------------------

function phrases(): Record<string, string> {
  return {
    Find: L("查找", "検索"),
    Replace: L("替换", "置換"),
    next: L("下一个", "次へ"),
    previous: L("上一个", "前へ"),
    all: L("全部", "すべて"),
    "match case": L("区分大小写", "大文字と小文字を区別"),
    "by word": L("全词", "単語単位"),
    regexp: L("正则", "正規表現"),
    replace: L("替换", "置換"),
    "replace all": L("全部替换", "すべて置換"),
    close: L("关闭", "閉じる"),
    "current match": L("当前匹配", "現在の一致"),
    "replaced $ matches": L("已替换 $ 处", "$ 件を置換しました"),
    "replaced match on line $": L("已替换第 $ 行的匹配", "$ 行目の一致を置換しました"),
    "on line": L("在第", "行"),
    "Go to line": L("跳到行", "行へ移動"),
    go: L("跳转", "移動"),
    "Folded lines": L("已折叠", "折りたたみ"),
    "Unfolded lines": L("已展开", "展開"),
    to: L("至", "〜"),
    "folded code": L("折叠的内容", "折りたたまれた内容"),
    unfold: L("展开", "展開"),
    "Fold line": L("折叠", "折りたたむ"),
    "Unfold line": L("展开", "展開"),
    "Selection deleted": L("已删除选中内容", "選択範囲を削除しました"),
    Diagnostics: L("提示", "診断"),
    "No diagnostics": L("没有提示", "診断なし"),
    "Control character": L("控制字符", "制御文字"),
  };
}

// --- assembly ------------------------------------------------------------------------

const wrapCompartment = new Compartment();
const guideCompartment = new Compartment();
const numberCompartment = new Compartment();
const phraseCompartment = new Compartment();

const numbers = (on: boolean): Extension => (on ? [lineNumbers(), highlightActiveLineGutter()] : []);

/**
 * What kind of file the editor shows: a Markdown note has all of it
 * (highlighting, links, completion, record-type checks); another text file
 * is plain text with the general editing (numbers, search, multiple
 * cursors, git marks, history); a read-only one cannot be changed.
 */
export interface DocMode {
  note: boolean;
  readOnly: boolean;
}

let mode: DocMode = { note: true, readOnly: false };

function extensions(onUpdate: (u: ViewUpdate) => void): Extension[] {
  const note = mode.note;
  return [
    numberCompartment.of(numbers(prefs.numbers)),
    gitGutter,
    foldGutter({
      markerDOM: (open) => {
        const s = document.createElement("span");
        s.className = "cm-fold " + (open ? "open" : "closed");
        s.innerHTML = '<svg viewBox="0 0 20 20" aria-hidden="true"><path d="M7 5l5 5-5 5"/></svg>';
        return s;
      },
    }),
    highlightSpecialChars(),
    history(),
    drawSelection(),
    dropCursor(),
    EditorState.allowMultipleSelections.of(true),
    indentOnInput(),
    syntaxHighlighting(jusHighlight),
    bracketMatching(),
    closeBrackets(),
    rectangularSelection(),
    crosshairCursor(),
    highlightActiveLine(),
    highlightSelectionMatches(),
    search({ top: true }),
    note ? autocompletion({ override: [wikiCompletion], icons: false, activateOnTyping: true }) : [],
    keymap.of([
      ...(note ? completionKeymap : []),
      ...(note ? [
        { key: "Mod-b", run: toggleWrap("**") },
        { key: "Mod-i", run: toggleWrap("*") },
        { key: "Mod-e", run: toggleWrap("`") },
        ...markdownKeymap,
      ] : []),
      ...closeBracketsKeymap,
      ...defaultKeymap,
      ...searchKeymap,
      ...historyKeymap,
      ...foldKeymap,
      ...lintKeymap,
      indentWithTab,
    ]),
    note ? markdown({ base: markdownLanguage }) : [],
    EditorView.theme({ "&": { height: "100%" }, ".cm-scroller": { overflow: "auto" } }),
    note ? [codePlugin, wikiMarks, lintExt, linkClick, fileDrop] : [],
    mode.readOnly ? [EditorState.readOnly.of(true), EditorView.editable.of(false)] : [],
    EditorView.contentAttributes.of({ spellcheck: "false", autocorrect: "off", autocapitalize: "off" }),
    wrapCompartment.of(prefs.wrap ? EditorView.lineWrapping : []),
    guideCompartment.of(prefs.guides ? guidePlugin : []),
    phraseCompartment.of(EditorState.phrases.of(phrases())),
    EditorView.updateListener.of(onUpdate),
  ];
}

/** Marks a change the app made (loading a file), which is not an edit. */
export const loaded = Annotation.define<boolean>();

/** Whether an update is only the app loading text, not the user editing. */
export function isLoad(u: ViewUpdate): boolean {
  return u.transactions.some((t) => t.annotation(loaded));
}

let updateFn: (u: ViewUpdate) => void = () => undefined;

export function createEditor(parent: HTMLElement, onUpdate: (u: ViewUpdate) => void): EditorView {
  updateFn = onUpdate;
  const state = EditorState.create({ doc: "", extensions: extensions(onUpdate) });
  return new EditorView({ state, parent });
}

/**
 * Shows another note: a fresh editor state, so undo history, folds and
 * search state never cross from one note into the next. The cursor goes to
 * pos (clamped), scrolled into view.
 */
export function setDoc(view: EditorView, text: string, pos = 0, m: DocMode = { note: true, readOnly: false }): void {
  mode = m;
  const anchor = Math.min(Math.max(0, pos), text.length);
  view.setState(EditorState.create({ doc: text, selection: { anchor }, extensions: extensions(updateFn) }));
  view.dispatch({ effects: EditorView.scrollIntoView(anchor, { y: "center" }), annotations: loaded.of(true) });
}

/** Replaces the text keeping the cursor and scroll position as far as possible (an outside edit reloaded). */
export function replaceDoc(view: EditorView, text: string): void {
  const head = view.state.selection.main.head;
  const top = view.scrollDOM.scrollTop;
  view.dispatch({
    changes: { from: 0, to: view.state.doc.length, insert: text },
    selection: { anchor: Math.min(head, text.length) },
    annotations: [loaded.of(true), Transaction.addToHistory.of(false)],
  });
  view.scrollDOM.scrollTop = top;
}

export function docText(view: EditorView): string {
  return view.state.doc.toString();
}

export function setGitChanges(view: EditorView, changes: GitChange[]): void {
  view.dispatch({ effects: setGit.of(changes) });
}

/** Re-applies the preferences that live inside the editor. */
export function applyEditorPrefs(view: EditorView): void {
  view.dispatch({
    effects: [
      wrapCompartment.reconfigure(prefs.wrap ? EditorView.lineWrapping : []),
      guideCompartment.reconfigure(prefs.guides ? guidePlugin : []),
      numberCompartment.reconfigure(numbers(prefs.numbers)),
      phraseCompartment.reconfigure(EditorState.phrases.of(phrases())),
    ],
  });
  view.requestMeasure();
}
