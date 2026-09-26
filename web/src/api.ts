// The page's side of the loopback API. The same operations exist on the
// CLI, so the app holds no behaviour the outside lacks. URLs are relative:
// the page is served under a per-run token path.

export type Doc = { rel: string; size: number; modTime: string };
export type Commit = { hash: string; message: string; when: string; author: string };
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
  rename: (from: string, to: string) => j<{ path: string }>("api/rename", send("POST", { from, to })),
  remove: (path: string) => j<{ committed: boolean }>("api/delete", send("POST", { path })),
  search: (text: string) => j<Hit[]>("api/search?q=" + q(text)),
  gutter: (path: string) => j<GitChange[]>("api/gutter?path=" + q(path)),
  check: (path: string, text: string) => j<Diagnostic[]>("api/check", send("POST", { path, text })),
  types: () => j<RecordType[]>("api/types"),
  capture: (type: string, section: string, text: string, date?: string) =>
    j<Saved>("api/capture", send("POST", { type, section, text, date })),
  status: () => j<string[]>("api/status"),
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
