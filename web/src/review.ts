// The history drawer, in two modes that share one diff view:
//
// - review: the uncommitted files — an agent's, another editor's — each
//   with its diff against the last commit, to accept (commit) or discard;
// - history: one note's versions, each with its diff to the note now, to
//   restore.
//
// Who made a change is shown when it is known (Jus-* trailers, or the
// source an agent recorded with -no-commit); otherwise nothing is claimed.

import { api, type Change, type Commit, type FileDiff, type Source } from "./api.ts";
import { L } from "./i18n.ts";
import { ask } from "./dialog.ts";
import { changedSpan } from "./text.ts";

export type Host = {
  current: () => string;
  open: (path: string, line?: number) => Promise<void>;
  /** The open note changed on disk because of us (discard, restore): reload it. */
  reload: () => Promise<void>;
  refresh: () => Promise<void>;
  leave: () => Promise<boolean>;
  toast: (text: string, error?: boolean) => void;
  /** Gives the keyboard back to the editor when the drawer closes. */
  focus: () => void;
};

const el = document.createElement("aside");
el.id = "review";
el.className = "panel layer";
el.dataset.cap = "status diff commit discard log show restore"; // review and history
el.innerHTML = '<header><h2></h2><span class="grow"></span><button class="text" data-act="all"></button></header><ul class="rv-list" role="listbox"></ul><div class="rv-diff"></div><footer class="rv-actions"></footer>';
document.body.append(el);
const titleEl = el.querySelector("h2")!;
const allBtn = el.querySelector<HTMLButtonElement>("[data-act=all]")!;
const listEl = el.querySelector<HTMLUListElement>(".rv-list")!;
const diffEl = el.querySelector<HTMLDivElement>(".rv-diff")!;
const actionsEl = el.querySelector<HTMLElement>(".rv-actions")!;

let host: Host;
let mode: "review" | "history" = "review";
let changes: Change[] = [];
let commits: Commit[] = [];
let selected = "";
let historyPath = "";

export function initReview(h: Host): void {
  host = h;
  allBtn.addEventListener("click", () => void acceptAll());
  // A click outside closes it (dialogs it opened, and its own triggers, do not).
  document.addEventListener("mousedown", (e) => {
    const t = e.target as Element;
    if (isOpen() && !el.contains(t) && !t.closest("#dialog, #st-git, [data-review-toggle]")) close();
  });
}

export function isOpen(): boolean {
  return el.classList.contains("open");
}

export function close(): void {
  if (!isOpen()) return;
  el.classList.remove("open");
  host?.focus();
}

/** Opens the review, or closes it when it is already showing the review. */
export async function toggleReview(): Promise<void> {
  if (isOpen() && mode === "review") close();
  else await openReview();
}

/** Opens a note's history, or closes it when it is already showing that. */
export async function toggleHistory(path: string): Promise<void> {
  if (isOpen() && mode === "history" && historyPath === path) close();
  else await openHistory(path);
}

export function sourceLabel(s: Source | undefined): string {
  if (!s || !s.author) return "";
  if (s.author === "agent") return "agent" + (s.model ? " · " + s.model : "");
  return L("本人", "本人");
}

/** Opens the review of uncommitted changes, on path when given. */
export async function openReview(path = ""): Promise<void> {
  mode = "review";
  el.classList.add("open");
  titleEl.textContent = L("审阅改动", "変更のレビュー");
  await loadChanges(path);
}

async function loadChanges(prefer = "", afterAction = false) {
  changes = (await api.changes().catch(() => [])).filter((c) => !c.mine);
  allBtn.hidden = changes.length < 2;
  allBtn.textContent = L("全部接受", "すべて承認");
  if (!changes.length && afterAction) {
    // The last one is handled: nothing left to look at here.
    close();
    host.toast(L("改动都处理完了", "すべての変更を処理しました"));
    return;
  }
  if (!changes.length) {
    listEl.replaceChildren(empty(L("没有要审阅的改动", "レビューする変更はありません")));
    diffEl.replaceChildren();
    actionsEl.replaceChildren();
    selected = "";
    return;
  }
  if (!changes.some((c) => c.path === selected)) selected = "";
  selected = (changes.find((c) => c.path === prefer) ?? changes.find((c) => c.path === selected) ?? changes[0]).path;
  paintChanges();
  await showChange();
}

const kindLabel = () => ({ new: L("新增", "追加"), modified: L("修改", "変更"), deleted: L("删除", "削除") });

function paintChanges() {
  const kinds = kindLabel();
  listEl.replaceChildren(
    ...changes.map((c) => {
      const li = document.createElement("li");
      li.className = "click" + (c.path === selected ? " sel" : "");
      li.setAttribute("role", "option");
      const tag = document.createElement("span");
      tag.className = "tag " + c.kind;
      tag.textContent = kinds[c.kind];
      const name = document.createElement("span");
      name.className = "name";
      name.textContent = c.path;
      li.append(tag, name);
      const src = sourceLabel(c.source);
      if (src) li.append(badge(src));
      li.addEventListener("click", () => {
        selected = c.path;
        paintChanges();
        void showChange();
      });
      return li;
    }),
  );
}

async function showChange() {
  const c = changes.find((x) => x.path === selected);
  if (!c) return;
  const d = await api.diff(c.path).catch(() => ({ hunks: [], eolOnly: false }) as FileDiff);
  const hunks = d.hunks;
  renderDiff(d, c.kind === "deleted" ? L("文件已删除", "ファイルは削除されました") : "");
  const buttons: HTMLButtonElement[] = [];
  if (/\.(md|markdown)$/i.test(c.path) && c.kind !== "deleted" && !c.path.startsWith(".")) {
    buttons.push(button(L("打开", "開く"), "quiet", () => void host.open(c.path, hunks[0]?.newStart)));
  }
  buttons.push(button(L("丢弃", "破棄"), "quiet danger", () => void discard(c)));
  buttons.push(button(L("接受", "承認"), "primary", () => void accept([c.path])));
  actionsEl.replaceChildren(...buttons);
}

async function accept(paths: string[]) {
  try {
    const r = await api.commit(paths);
    if (!r.committed) host.toast(L("没有要提交的", "コミットするものはありません"));
    await host.refresh();
    await loadChanges("", true);
  } catch (e) {
    host.toast(L("提交失败：", "コミットできません：") + (e as Error).message, true);
  }
}

async function acceptAll() {
  const { value } = await ask({
    title: L(`接受全部 ${changes.length} 个改动？`, `${changes.length} 件の変更をすべて承認しますか？`),
    body: changes.map((c) => c.path).slice(0, 12).join("\n") + (changes.length > 12 ? "\n…" : ""),
    choices: [
      { label: L("取消", "キャンセル"), value: false },
      { label: L("全部接受", "すべて承認"), value: true, primary: true },
    ],
    cancel: false,
  });
  if (value) await accept(changes.map((c) => c.path));
}

async function discard(c: Change) {
  const { value } = await ask({
    title: L("丢弃这个改动？", "この変更を破棄しますか？"),
    body:
      c.path +
      "\n" +
      (c.kind === "new"
        ? L("这个新文件会被移除（留一份在 .jusnote/backup）。", "この新しいファイルは削除されます（.jusnote/backup に残ります）。")
        : L("文件会回到上次提交时的样子（现在的内容留一份在 .jusnote/backup）。", "ファイルは前回のコミットの状態に戻ります（今の内容は .jusnote/backup に残ります）。")),
    choices: [
      { label: L("取消", "キャンセル"), value: false },
      { label: L("丢弃", "破棄"), value: true, primary: true },
    ],
    cancel: false,
  });
  if (!value) return;
  try {
    await api.discard(c.path);
    if (c.path === host.current()) await host.reload();
    await host.refresh();
    await loadChanges("", true);
  } catch (e) {
    host.toast(L("无法丢弃：", "破棄できません：") + (e as Error).message, true);
  }
}

/** Opens a note's history. */
export async function openHistory(path: string): Promise<void> {
  if (!path) return;
  mode = "history";
  historyPath = path;
  el.classList.add("open");
  allBtn.hidden = true;
  titleEl.textContent = L("历史 · ", "履歴 · ") + path.replace(/\.(md|markdown)$/i, "");
  commits = await api.fileLog(path).catch(() => []);
  if (!commits.length) {
    listEl.replaceChildren(empty(L("还没有提交过", "まだコミットされていません")));
    diffEl.replaceChildren();
    actionsEl.replaceChildren();
    return;
  }
  selected = commits.length > 1 ? commits[1].hash : commits[0].hash;
  paintCommits();
  await showCommit();
}

function paintCommits() {
  listEl.replaceChildren(
    ...commits.map((c, i) => {
      const li = document.createElement("li");
      li.className = "click" + (c.hash === selected ? " sel" : "");
      li.setAttribute("role", "option");
      const when = document.createElement("span");
      when.className = "tag";
      when.textContent = formatWhen(c.when);
      const name = document.createElement("span");
      name.className = "name";
      name.textContent = c.message + (i === 0 ? L("（最新）", "（最新）") : "");
      li.append(when, name);
      const src = sourceLabel(c.source);
      if (src) li.append(badge(src));
      li.title = c.hash.slice(0, 7) + " · " + c.author;
      li.addEventListener("click", () => {
        selected = c.hash;
        paintCommits();
        void showCommit();
      });
      return li;
    }),
  );
}

async function showCommit() {
  const c = commits.find((x) => x.hash === selected);
  if (!c) return;
  const d = await api.diff(historyPath, c.hash).catch(() => ({ hunks: [], eolOnly: false }) as FileDiff);
  renderDiff(d, L("和现在一样", "現在と同じです"), L("这一版 → 现在", "この版 → 現在"));
  actionsEl.replaceChildren(
    button(L("恢复这一版", "この版に戻す"), "primary", () => void restore(c)),
  );
  actionsEl.querySelector("button")!.disabled = d.hunks.length === 0 && !d.eolOnly;
}

async function restore(c: Commit) {
  const { value } = await ask({
    title: L("恢复到这一版？", "この版に戻しますか？"),
    body: `${formatWhen(c.when)} · ${c.message}\n` + L("恢复会作为一次新的提交记下，历史不会被改写。", "復元は新しいコミットとして記録され、履歴は書き換えられません。"),
    choices: [
      { label: L("取消", "キャンセル"), value: false },
      { label: L("恢复", "戻す"), value: true, primary: true },
    ],
    cancel: false,
  });
  if (!value) return;
  if (historyPath === host.current() && !(await host.leave())) return;
  try {
    await api.restore(c.hash, historyPath);
    if (historyPath === host.current()) await host.reload();
    host.toast(L("已恢复并提交", "復元してコミットしました"));
    await host.refresh();
    await openHistory(historyPath);
  } catch (e) {
    host.toast(L("无法恢复：", "復元できません：") + (e as Error).message, true);
  }
}

// --- the diff view -----------------------------------------------------------------

function renderDiff(d: FileDiff, note = "", caption = "") {
  const hunks = d.hunks;
  const parts: HTMLElement[] = [];
  if (caption) {
    const c = document.createElement("div");
    c.className = "rv-caption";
    c.textContent = caption;
    parts.push(c);
  }
  if (d.eolOnly) {
    const n = document.createElement("div");
    n.className = "rv-note";
    n.textContent = L("只改了换行符（CRLF / LF），内容没有变。", "改行コード（CRLF / LF）だけが変わり、内容は同じです。");
    parts.push(n);
  }
  if (!hunks.length) {
    if (!d.eolOnly) parts.push(empty(note || L("没有差别", "差分はありません"), "div"));
    diffEl.replaceChildren(...parts);
    return;
  }
  for (const h of hunks) {
    const box = document.createElement("div");
    box.className = "hunk";
    const head = document.createElement("div");
    head.className = "hunk-head";
    head.textContent = L(`第 ${h.newStart} 行`, `${h.newStart} 行目`);
    box.append(head);
    let o = h.oldStart;
    let n = h.newStart;
    // Pair a run of removed lines with the added run right after it, so each
    // pair can show which characters changed.
    const partner = new Map<number, number>();
    for (let i = 0; i < h.lines.length; ) {
      if (h.lines[i].op !== "-") {
        i++;
        continue;
      }
      let d = i;
      while (d < h.lines.length && h.lines[d].op === "-") d++;
      let a = d;
      while (a < h.lines.length && h.lines[a].op === "+") a++;
      for (let k = 0; k < Math.min(d - i, a - d); k++) {
        partner.set(i + k, d + k);
        partner.set(d + k, i + k);
      }
      i = a;
    }
    h.lines.forEach((l, idx) => {
      const row = document.createElement("div");
      row.className = "dl " + (l.op === "+" ? "add" : l.op === "-" ? "del" : "ctx");
      const a = document.createElement("span");
      a.className = "ln";
      a.textContent = l.op === "+" ? "" : String(o++);
      const b = document.createElement("span");
      b.className = "ln";
      b.textContent = l.op === "-" ? "" : String(n++);
      const op = document.createElement("span");
      op.className = "op";
      op.textContent = l.op === " " ? "" : l.op === "+" ? "+" : "−";
      const t = document.createElement("span");
      t.className = "tx";
      const p = partner.get(idx);
      if (p !== undefined && l.text && h.lines[p].text) {
        const [oldT, newT] = l.op === "-" ? [l.text, h.lines[p].text] : [h.lines[p].text, l.text];
        const [s, eo, en] = changedSpan(oldT, newT);
        const end = l.op === "-" ? eo : en;
        const m = document.createElement("mark");
        m.textContent = l.text.slice(s, end);
        t.append(l.text.slice(0, s), m, l.text.slice(end));
      } else t.textContent = l.text || " ";
      row.append(a, b, op, t);
      box.append(row);
    });
    parts.push(box);
  }
  diffEl.replaceChildren(...parts);
  diffEl.scrollTop = 0;
}

// --- bits ---------------------------------------------------------------------------

function empty(text: string, tag: "li" | "div" = "li"): HTMLElement {
  const e = document.createElement(tag);
  e.className = "empty";
  e.textContent = text;
  return e;
}

function badge(text: string): HTMLSpanElement {
  const b = document.createElement("span");
  b.className = "badge";
  b.textContent = text;
  return b;
}

function button(label: string, cls: string, run: () => void): HTMLButtonElement {
  const b = document.createElement("button");
  b.className = cls;
  b.textContent = label;
  b.addEventListener("click", run);
  return b;
}

function formatWhen(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  const p = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`;
}

/** Re-reads what is shown, after an outside change. */
export async function refreshOpen(): Promise<void> {
  if (!isOpen()) return;
  if (mode === "review") await loadChanges(selected, true);
}
