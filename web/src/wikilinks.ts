// Wiki links in the editor: [[name]] is marked (a missing target looks
// different), typing [[ offers the notebook's notes, and the resolution
// rules match the server's (internal/links): a name with "/" is a path,
// otherwise a file name, ignoring case.

import { RangeSetBuilder } from "@codemirror/state";
import { Decoration, ViewPlugin, type DecorationSet, type EditorView, type ViewUpdate } from "@codemirror/view";
import type { CompletionContext, CompletionResult, Completion } from "@codemirror/autocomplete";
import { openWikiLink } from "./text.ts";

let notes: string[] = [];
let modified = new Map<string, number>();
let current = "";
let names = new Map<string, string[]>(); // lower-case stem -> paths
let paths = new Set<string>(); // lower-case path without extension
let version = 0;

const stem = (p: string) => p.replace(/^.*\//, "").replace(/\.(md|markdown)$/i, "");
const noExt = (p: string) => p.replace(/\.(md|markdown)$/i, "");

/** The note being edited: it is not offered as a link to itself, and its folder comes first. */
export function setCurrentNote(path: string): void {
  current = path;
}

/** The notebook's notes, for marking and completing links. */
export function setNotes(docs: { rel: string; modTime: string }[]): void {
  const list = docs.map((d) => d.rel);
  modified = new Map(docs.map((d) => [d.rel, Date.parse(d.modTime) || 0]));
  notes = list;
  names = new Map();
  paths = new Set();
  for (const p of list) {
    const k = stem(p).toLowerCase();
    names.set(k, [...(names.get(k) ?? []), p]);
    paths.add(noExt(p).toLowerCase());
  }
  version++;
}

export function exists(target: string): boolean {
  const t = target.trim().toLowerCase();
  if (t.includes("/")) return paths.has(noExt(t.replace(/^\/+/, "")));
  return names.has(t.replace(/\.md$/, ""));
}

const WIKI = /\[\[([^[\]|#\n]+)(#[^[\]|\n]*)?(\|[^[\]\n]*)?\]\]/g;
const ok = Decoration.mark({ class: "cm-wikilink" });
const missing = Decoration.mark({ class: "cm-wikilink cm-wikilink-missing" });

function build(view: EditorView): DecorationSet {
  const b = new RangeSetBuilder<Decoration>();
  for (const { from, to } of view.visibleRanges) {
    const text = view.state.sliceDoc(from, to);
    for (const m of text.matchAll(WIKI)) {
      const at = from + m.index;
      b.add(at, at + m[0].length, exists(m[1]) ? ok : missing);
    }
  }
  return b.finish();
}

export const wikiMarks = ViewPlugin.fromClass(
  class {
    decorations: DecorationSet;
    seen = version;
    constructor(view: EditorView) {
      this.decorations = build(view);
    }
    update(u: ViewUpdate) {
      if (u.docChanged || u.viewportChanged || this.seen !== version) {
        this.seen = version;
        this.decorations = build(u.view);
      }
    }
  },
  { decorations: (v) => v.decorations },
);

/** Completes note names after [[. A name shared by several notes is inserted as a path. */
export function wikiCompletion(ctx: CompletionContext): CompletionResult | null {
  const line = ctx.state.doc.lineAt(ctx.pos);
  const typed = openWikiLink(line.text.slice(0, ctx.pos - line.from));
  if (typed === null) return null;
  const from = ctx.pos - typed.length;
  const after = ctx.state.sliceDoc(ctx.pos, ctx.pos + 2);
  const close = after === "]]" ? "" : after.startsWith("]") ? "]" : "]]";
  // The same folder first, then the most recently changed; never the note itself.
  const dirOf = (p: string) => (p.includes("/") ? p.slice(0, p.lastIndexOf("/")) : "");
  const here = dirOf(current);
  const ordered = notes
    .filter((p) => p !== current)
    .sort((a, b) => Number(dirOf(b) === here) - Number(dirOf(a) === here) || (modified.get(b) ?? 0) - (modified.get(a) ?? 0));
  const options: Completion[] = ordered.map((p, i) => {
    const s = stem(p);
    const unique = (names.get(s.toLowerCase()) ?? []).length === 1;
    const insert = unique ? s : noExt(p);
    const dir = p.includes("/") ? p.slice(0, p.lastIndexOf("/")) : "";
    return {
      label: insert,
      detail: unique ? dir : undefined,
      boost: 99 - Math.min(i, 99), // keep this order while the typed text is still empty
      apply: (view: EditorView, _c: Completion, a: number, b: number) => {
        view.dispatch({ changes: { from: a, to: b, insert: insert + close }, selection: { anchor: a + insert.length + 2 } }); // after the "]]", whichever part was already there
      },
    };
  });
  return { from, options, validFor: /^[^[\]|#\n]*$/, filter: true };
}

/** The wiki link target under pos in the document, if any. */
export function wikiAt(text: string, col: number): string | null {
  for (const m of text.matchAll(WIKI)) {
    if (m.index <= col && col <= m.index + m[0].length) return m[1].trim();
  }
  return null;
}
