// The page's side of the loopback API. The same operations exist on the
// CLI, so the app holds no behaviour the outside lacks. URLs are relative:
// the page is served under a per-run token path.

/** A file of the list: a note, or another text file (note false); readOnly says why one only shows. */
export type Doc = { rel: string; size: number; modTime: string; note?: boolean; readOnly?: "encoding" | "size" };
export type Source = { author?: string; model?: string; run?: string };
export type Commit = { hash: string; message: string; when: string; author: string; source: Source };
export type Change = { path: string; kind: "new" | "modified" | "deleted"; source: Source; tracked: boolean; mine: boolean };
export type Hunk = { oldStart: number; oldLines: number; newStart: number; newLines: number; lines: { op: " " | "-" | "+"; text: string }[] };
export type FileDiff = { hunks: Hunk[]; eolOnly: boolean };
export type Link = { line: number; kind: "wiki" | "md"; raw: string; target: string };
export type Backlink = { from: string; line: number; text: string };
export type GitChange = { line: number; kind: "add" | "modify" | "delete" };
export type Diagnostic = { line: number; message: string; severity: string };
export type Hit = { rel: string; line: number; text: string };
export type Info = {
  open: boolean;
  root?: string;
  vault?: string;
  notes?: number;
  git?: boolean;
  changed?: number;
};
export type Recent = { path: string; name: string };
export type Note = { path: string; text: string; version: string; tracked: boolean };
export type Saved = { path: string; version: string; committed: boolean; hash: string };
export type Session = { file: string; cursors: Record<string, number> };
export type RecordType = { id: string; name: string; sections: { name: string }[] };

/** The file changed on disk since the editor loaded it; nothing was written. */
export class ConflictError extends Error {}

async function j<T>(url: string, init?: RequestInit): Promise<T> {
  const r = await fetch(url, init);
  const text = await r.text();
  if (!r.ok) {
    let msg = r.statusText;
    try {
      msg = (JSON.parse(text) as { error?: string }).error ?? msg;
    } catch {
      /* keep statusText */
    }
    if (r.status === 409) throw new ConflictError(msg);
    throw new Error(msg);
  }
  return (text ? JSON.parse(text) : undefined) as T;
}

const send = (method: string, body: unknown): RequestInit => ({
  method,
  headers: { "Content-Type": "application/json" },
  body: JSON.stringify(body),
});

const q = encodeURIComponent;

export const api = {
  info: () => j<Info>("api/info"),
  open: (folder: string) => j<{ root: string }>("api/open", send("POST", { folder })),
  recent: () => j<Recent[]>("api/recent"),
  list: () => j<Doc[]>("api/list"),
  read: (path: string) => j<Note>("api/note?path=" + q(path)),
  version: (path: string) => j<{ version: string }>("api/version?path=" + q(path)),
  /** base: the version the text was edited from ("" for a new file; null to overwrite). */
  write: (path: string, text: string, base: string | null, commit = false) =>
    j<Saved>("api/note", send("PUT", base === null ? { path, text, commit } : { path, text, base, commit })),
  /** all: every change in the tree; mine: only what the app itself wrote to paths. */
  commit: (paths: string[], opt: { all?: boolean; mine?: boolean } = {}) =>
    j<{ committed: boolean; hash: string }>("api/commit", send("POST", { paths, ...opt })),
  rename: (from: string, to: string, links = true) =>
    j<{ path: string; updated: string[]; committed: boolean }>("api/rename", send("POST", { from, to, links })),
  remove: (path: string) => j<{ committed: boolean }>("api/delete", send("POST", { path })),
  search: (text: string) => j<Hit[]>("api/search?q=" + q(text)),
  gutter: (path: string) => j<GitChange[]>("api/gutter?path=" + q(path)),
  check: (path: string, text: string) => j<Diagnostic[]>("api/check", send("POST", { path, text })),
  types: () => j<RecordType[]>("api/types"),
  capture: (type: string, section: string, text: string, date?: string) =>
    j<Saved>("api/capture", send("POST", { type, section, text, date })),
  status: () => j<string[]>("api/status"),
  changes: () => j<Change[]>("api/changes"),
  /** Against HEAD; with hash, that commit's version against the file now. */
  diff: (path: string, hash = "") => j<FileDiff>("api/diff?path=" + q(path) + (hash ? "&hash=" + q(hash) : "")),
  discard: (path: string) => j<unknown>("api/discard", send("POST", { path })),
  fileLog: (path: string, limit = 50) => j<Commit[]>("api/history?limit=" + limit + "&path=" + q(path)),
  show: (hash: string, path: string) => j<{ text: string }>("api/show?hash=" + q(hash) + "&path=" + q(path)),
  restore: (hash: string, path: string) => j<{ path: string; version: string; committed: boolean; hash: string }>("api/restore", send("POST", { hash, path })),
  links: (path: string, text?: string) => j<{ out: Link[]; back: Backlink[] }>("api/links", send("POST", { path, text })),
  resolve: (target: string, from: string) => j<{ path: string }>("api/resolve?target=" + q(target) + "&from=" + q(from)),
  attach: (note: string, name: string, data: string) => j<{ path: string; link: string }>("api/attach", send("POST", { note, name, data })),
  selftest: (report: unknown) => j<unknown>("api/selftest", send("POST", report)),
  history: (limit = 20) => j<Commit[]>("api/history?limit=" + limit),
  session: {
    get: () => j<Session>("api/session"),
    set: (s: Session) => j<unknown>("api/session", send("POST", s)),
  },
  log: {
    get: async () => (await fetch("api/log")).text(),
    add: (message: string) => j<unknown>("api/log", send("POST", { message })).catch(() => undefined),
  },
};
