import * as live from "./live.ts";
import { cap, CapError } from "./cap.ts";
import type { EditorView, ViewUpdate } from "@codemirror/view";
import { openSearchPanel } from "@codemirror/search";
import { api, ConflictError, type Backlink, type Change, type Doc, type GitChange, type Hit, type Session } from "./api.ts";
import {
  createEditor,
  setDoc,
  replaceDoc,
  docText,
  setGitChanges,
  setLintPath,
  forceLint,
  applyEditorPrefs,
  setLinkHandler,
  setFileHandler,
  isLoad,
} from "./editor.ts";
import { L, prefs, setPref, onLang, onPref, applyStatic, SIZE_MIN, SIZE_MAX, SIZE_DEFAULT, type StaticText } from "./i18n.ts";
import { ask, isOpen as dialogOpen } from "./dialog.ts";
import { wordCount, fuzzyScore, buildTree, notePath, toggleTask, type TreeNode } from "./text.ts";
import { renderInto } from "./markdown.ts";
import { setNotes, setCurrentNote } from "./wikilinks.ts";
import * as review from "./review.ts";
import { initTips } from "./tip.ts";
import { runSelftest } from "./selftest.ts";

/** A Markdown note (links, preview, record types); the other files are plain text. */
const isNotePath = (rel: string) => /\.(md|markdown)$/i.test(rel);
const isNote = (d: Doc) => d.note ?? isNotePath(d.rel);

declare global {
  interface Window {
    kpTitle?: (t: string) => void;
    kpReveal?: (path: string) => void;
    kpTerminal?: (dir: string) => Promise<void>;
    kpPickFolder?: (title: string) => Promise<string> | string;
    kpOpenExternal?: (url: string) => void;
    kpOpenJus?: (url: string) => Promise<string>;
    kpFront?: () => Promise<void>;
    jusVersion?: string;
  }
}

const $ = <T extends HTMLElement = HTMLElement>(id: string) => document.getElementById(id) as T;

const nbName = $("nb-name");
const fileName = $("file-name");
const stPath = $("st-path");
const stPos = $("st-pos");
const stWords = $("st-words");
const stGit = $("st-git");
const stSave = $("st-save");
const outlineEl = $<HTMLUListElement>("outline");
const changesEl = $<HTMLUListElement>("changes");
const uncommittedEl = $<HTMLUListElement>("uncommitted");
const recentEl = $<HTMLUListElement>("recent");
const rail = $("rail");
const filesPanel = $("files-panel");
const filesEl = $<HTMLUListElement>("files");
const menu = $("menu");
const overlay = $("overlay");
const paletteInput = $<HTMLInputElement>("palette-input");
const paletteList = $<HTMLUListElement>("palette-list");
const welcome = $("welcome");
const empty = $("empty");
const nbPopover = $("nb-popover");
const nbRecent = $<HTMLUListElement>("nb-recent");
const settings = $("settings");
const preview = $("preview");
const help = $("help");
const toastEl = $("toast");

// --- state -------------------------------------------------------------

let opened = false;
let root = "";
let docs: Doc[] = [];
let changedFiles: string[] = [];
/** Uncommitted changes the editor did not make itself: what the review is for. */
let toReview = new Map<string, Change>();
/** Attachments pasted into the open note: committed together with it. */
let attachedHere: string[] = [];
let gitChanges: GitChange[] = [];

let currentPath = "";
let version = ""; // the file's version the buffer is based on
let tracked = false;
let dirty = false; // buffer differs from what was last written
let conflict = false; // a save found the file changed outside; autosave waits
let saving: Promise<void> | null = null;
let rendered = false;
let session: Session = { file: "", cursors: {} };
const collapsed = new Set<string>();

const AUTOSAVE_MS = 700;
const WATCH_MS = 2500;

const msg = (e: unknown) => (e instanceof Error ? e.message : String(e));

// --- toast and save state ------------------------------------------------

let toastTimer = 0;
function toast(text: string, error = false) {
  toastEl.textContent = text;
  toastEl.classList.toggle("error", error);
  toastEl.classList.add("show");
  window.clearTimeout(toastTimer);
  toastTimer = window.setTimeout(() => toastEl.classList.remove("show"), error ? 5000 : 2200);
}

type SaveState = "saved" | "dirty" | "saving" | "conflict" | "committed";
let saveState: SaveState = "saved";
function showSave(s: SaveState, _hash = "") {
  saveState = s;
  paintSave();
}
function paintSave() {
  stSave.className = saveState;
  stSave.textContent = !currentPath
    ? ""
    : {
        saved: L("已保存", "保存済み"),
        dirty: L("编辑中", "編集中"),
        saving: L("保存中…", "保存中…"),
        conflict: L("有冲突", "競合あり"),
        committed: L("已保存", "保存済み"),
      }[saveState];
}

// --- editor ----------------------------------------------------------------

const editor: EditorView = createEditor($("editor"), onUpdate);

let autosaveTimer = 0;
let sessionTimer = 0;
function onUpdate(u: ViewUpdate) {
  if (u.docChanged) {
    scheduleOutline();
    scheduleWords();
    if (rendered) renderPreview(docText(editor));
  }
  if (u.docChanged && !isLoad(u)) {
    dirty = true;
    if (!conflict) showSave("dirty");
    window.clearTimeout(autosaveTimer);
    autosaveTimer = window.setTimeout(() => void autosave(), AUTOSAVE_MS);
  }
  if (u.docChanged || u.selectionSet) {
    updatePos();
    window.clearTimeout(sessionTimer);
    sessionTimer = window.setTimeout(saveSession, 800);
  }
}

function updatePos() {
  const s = editor.state;
  const head = s.selection.main.head;
  const line = s.doc.lineAt(head);
  stPos.textContent = currentPath ? `${line.number}:${head - line.from + 1}` : "";
  markOutline(line.number);
  const sel = s.selection.main;
  if (!sel.empty) stWords.textContent = `${wordCount(s.sliceDoc(sel.from, sel.to))} / ${words} ${L("字", "字")}`;
  else paintWords();
}

let words = 0;
let wordsTimer = 0;
function scheduleWords() {
  window.clearTimeout(wordsTimer);
  wordsTimer = window.setTimeout(() => {
    words = wordCount(docText(editor));
    paintWords();
  }, 250);
}
function paintWords() {
  stWords.textContent = currentPath ? `${words} ${L("字", "字")}` : "";
}

function gotoLine(n: number, select?: string) {
  const total = editor.state.doc.lines;
  const line = editor.state.doc.line(Math.min(Math.max(1, n), total));
  let anchor = line.from;
  let head = line.from;
  if (select) {
    const at = line.text.toLowerCase().indexOf(select.toLowerCase());
    if (at >= 0) {
      anchor = line.from + at;
      head = anchor + select.length;
    }
  }
  if (rendered) setRendered(false);
  editor.dispatch({ selection: { anchor, head }, scrollIntoView: true });
  editor.focus();
}

// --- saving ------------------------------------------------------------------

async function autosave() {
  if (!dirty || conflict || !currentPath) return;
  await write(false);
}

/** Writes the buffer; commit also commits it (Ctrl+S). Resolves after any conflict dialog. */
async function write(commit: boolean): Promise<void> {
  if (saving) await saving;
  if (!currentPath || (!dirty && !commit)) return;
  const path = currentPath;
  const text = docText(editor);
  showSave("saving");
  const run = (async () => {
    try {
      const r = await api.write(path, text, version, commit);
      if (path !== currentPath) return;
      version = r.version;
      if (docText(editor) === text) dirty = false;
      if (r.committed) tracked = true;
      showSave(dirty ? "dirty" : r.committed ? "committed" : "saved", r.hash);
      if (commit && r.committed) toast(L("已提交 ", "コミットしました ") + r.hash.slice(0, 7));
      void refreshGit();
    } catch (e) {
      if (e instanceof ConflictError) {
        conflict = true;
        showSave("conflict");
      } else {
        showSave("dirty");
        toast(L("保存失败：", "保存できません：") + msg(e), true);
      }
    }
  })();
  saving = run;
  await run;
  saving = null;
  if (conflict && path === currentPath) await resolveConflict(commit);
}

async function resolveConflict(commit = false) {
  const disk = await api.read(currentPath).catch(() => null);
  const { value } = await ask({
    title: disk ? L("这篇笔记在别处被改过", "このノートは別の場所で変更されました") : L("这篇笔记已在别处被删除", "このノートは別の場所で削除されました"),
    body: disk
      ? L("磁盘上的内容和你正在编辑的不同。要用哪一份？", "ディスク上の内容が編集中のものと違います。どちらを使いますか？")
      : L("要按你正在编辑的内容重新保存吗？", "編集中の内容で保存し直しますか？"),
    choices: disk
      ? [
          { label: L("稍后", "あとで"), value: "later" as const },
          { label: L("用我的覆盖", "自分のもので上書き"), value: "mine" as const, danger: true },
          { label: L("载入磁盘上的", "ディスクの内容を読み込む"), value: "theirs" as const, primary: true },
        ]
      : [
          { label: L("稍后", "あとで"), value: "later" as const },
          { label: L("重新保存", "保存し直す"), value: "mine" as const, primary: true },
        ],
    cancel: "later" as const,
  });
  if (value === "later") return;
  conflict = false;
  if (value === "theirs" && disk) {
    version = disk.version;
    replaceDoc(editor, disk.text);
    dirty = false;
    showSave("saved");
    void refreshGutter();
    return;
  }
  version = disk ? disk.version : "";
  dirty = true;
  await write(commit);
}

async function saveAndCommit() {
  if (!opened || !currentPath) {
    toast(L("先打开或新建一篇笔记", "まずノートを開くか作成してください"));
    return;
  }
  if (conflict) {
    await resolveConflict(true);
    return;
  }
  window.clearTimeout(autosaveTimer);
  if (dirty) await write(false);
  if (conflict) return;
  try {
    // The note and the attachments pasted into it go in one commit.
    const r = await api.commit([currentPath, ...attachedHere]);
    attachedHere = [];
    showSave(r.committed ? "committed" : "saved", r.hash);
    toast(r.committed ? L("已提交 ", "コミットしました ") + r.hash.slice(0, 7) : L("没有要提交的改动", "コミットする変更はありません"));
    if (r.committed) tracked = true;
    void refreshGit();
  } catch (e) {
    toast(L("提交失败：", "コミットできません：") + msg(e), true);
  }
}

type Skill = { name: string; description?: string; run?: string[]; trusted: boolean; problem?: string; out: string };
type SkillResult = { skill: string; exit: number; ms: number; out: string; files: string[]; tail: string; timedOut?: boolean };

/** Runs one of the notebook's skills (.jusnote/skills); a new or changed one is shown and asked about first. */
async function runSkill() {
  const all = await cap<Skill[]>("skills.list").catch((e) => (toast(msg(e), true), null));
  if (!all) return;
  const runnable = all.filter((s) => s.run && !s.problem);
  if (!runnable.length) {
    toast(L("没有可运行的技能（.jusnote/skills）", "実行できるスキルがありません（.jusnote/skills）"));
    return;
  }
  const pick = await ask({
    title: L("运行技能", "スキルを実行"),
    options: runnable.map((s) => ({ value: s.name, label: s.description ? `${s.name} — ${s.description}` : s.name })),
    choices: [{ label: L("运行", "実行"), value: true, primary: true }, { label: L("取消", "キャンセル"), value: false }],
    cancel: false,
  });
  if (!pick.value) return;
  const name = pick.text;
  let r: SkillResult;
  try {
    r = await cap<SkillResult>("skills.run", { name });
  } catch (e) {
    if (!(e instanceof CapError) || e.kind !== "confirm") return toast(msg(e), true);
    const plan = e.plan as { command: string[] };
    const ok = await ask({
      title: L("运行这个程序？", "このプログラムを実行しますか？"),
      body: L("技能随笔记本而来，改动后会再问。", "スキルはノートブックに付属。変更されたら再確認します。") + "\n\n" + plan.command.map((a) => (root && a.toLowerCase().startsWith(root.toLowerCase()) ? "." + a.slice(root.replace(/[\\/]+$/, "").length) : a)).join(" "),
      choices: [{ label: L("运行", "実行"), value: true, primary: true }, { label: L("取消", "キャンセル"), value: false }],
      cancel: false,
    });
    if (!ok.value) return;
    try {
      r = await cap<SkillResult>("skills.run", { name, yes: true });
    } catch (e2) {
      return toast(msg(e2), true);
    }
  }
  toast(L("完成：", "完了：") + `${r.skill} · ${r.files.length} ${L("个文件", "ファイル")}`); // a failed run is an error, shown above
  if (r.files.length) window.kpReveal?.(r.out.replace(/[\\/]+$/, "") + "\\" + r.files[0].replace(/\//g, "\\"));
  void refreshGit();
}

/** The notebook's commit setting (kept in it): manual leaves committing to Ctrl+S. */
async function toggleAutoCommit() {
  try {
    const cur = await cap<{ commit: string }>("config.get");
    const next = cur.commit === "manual" ? "auto" : "manual";
    await cap("config.set", { commit: next });
    toast(next === "auto" ? L("自动提交：开", "自動コミット：オン") : L("自动提交：关，按 Ctrl+S 提交", "自動コミット：オフ、Ctrl+S でコミット"));
    void refreshGit();
  } catch (e) {
    toast(msg(e), true);
  }
}

/** Before leaving a note: write what is unsaved and commit what the app wrote to it. */
async function leaveNote(): Promise<boolean> {
  if (!currentPath) return true;
  window.clearTimeout(autosaveTimer);
  if (dirty && !conflict) await write(false);
  if (conflict) {
    await resolveConflict();
    if (conflict) return false;
  }
  await api.commit([currentPath, ...attachedHere], { mine: true }).catch(() => undefined);
  attachedHere = [];
  return true;
}

/** Re-reads the open note from disk (after a discard or restore), keeping the cursor. */
async function reloadCurrent() {
  if (!currentPath) return;
  try {
    const note = await api.read(currentPath);
    version = note.version;
    tracked = note.tracked;
    replaceDoc(editor, note.text);
    dirty = false;
    conflict = false;
    showSave("saved");
    await refreshGutter();
  } catch {
    // Gone (a discarded new file): show another note.
    closeFile();
    await refreshFiles();
    if (docs.length) await openFile(docs[0].rel);
  }
}

// Outside edits: while the buffer is clean, a changed file is reloaded in
// place; while it is being edited, the next save asks.
async function watch() {
  if (!opened || !currentPath || document.hidden || saving || conflict) return;
  try {
    const { version: v } = await api.version(currentPath);
    if (v === version || saving) return;
    if (v === "") {
      if (!dirty) {
        conflict = true;
        showSave("conflict");
        await resolveConflict();
      }
      return;
    }
    if (!dirty) {
      const note = await api.read(currentPath);
      if (dirty) return;
      version = note.version;
      replaceDoc(editor, note.text);
      dirty = false;
      showSave("saved");
      toast(L("已载入别处的修改", "外部の変更を読み込みました"));
      void refreshGit();
    }
  } catch {
    /* the next tick tries again */
  }
}

// --- files -----------------------------------------------------------------------

function renderFiles() {
  const items: HTMLLIElement[] = [];
  const walk = (nodes: TreeNode[], depth: number) => {
    for (const n of nodes) {
      const li = document.createElement("li");
      li.setAttribute("role", "treeitem");
      li.style.setProperty("--depth", String(depth));
      li.dataset.path = n.path;
      if (n.dir) {
        const open = !collapsed.has(n.path);
        li.className = "dir" + (open ? " open" : "");
        li.setAttribute("aria-expanded", String(open));
        li.innerHTML = '<svg viewBox="0 0 20 20" aria-hidden="true"><path d="M7 5l5 5-5 5"/></svg>';
        li.append(n.name);
        li.addEventListener("click", () => {
          if (collapsed.has(n.path)) collapsed.delete(n.path);
          else collapsed.add(n.path);
          renderFiles();
        });
        items.push(li);
        if (open) walk(n.children, depth + 1);
      } else {
        const rv = toReview.get(n.path);
        li.className = "file" + (isNotePath(n.path) ? "" : " text") + (n.path === currentPath ? " active" : "") + (rv ? " review" + (rv.source?.author === "agent" ? " agent" : "") : changedFiles.includes(n.path) ? " changed" : "");
        li.textContent = n.name.replace(/\.(md|markdown)$/i, "");
        li.title = rv ? `${n.path} · ${rv.source?.author === "agent" ? L("agent 改过，待审阅", "agent が変更・レビュー待ち") : L("别处改过，待审阅", "外部で変更・レビュー待ち")}` : n.path;
        li.addEventListener("click", () => {
          void openFile(n.path);
          if (innerWidth < 900) toggleFiles(false);
        });
        items.push(li);
      }
      li.addEventListener("contextmenu", (e) => {
        e.preventDefault();
        showMenu(e.clientX, e.clientY, n);
      });
    }
  };
  walk(buildTree(docs.filter((d) => prefs.files === "all" || isNote(d)).map((d) => d.rel)), 0);
  if (!items.length) items.push(emptyItem(opened ? L("还没有笔记", "ノートはまだありません") : L("未打开笔记本", "ノートブックが開かれていません")));
  filesEl.replaceChildren(...items);
}

async function refreshFiles() {
  docs = opened ? await api.list().catch(() => []) : [];
  setNotes(docs.filter(isNote)); // links and completion are the notes
  renderFiles();
  showEmpty(opened && docs.length === 0);
}

function emptyItem(text: string): HTMLLIElement {
  const li = document.createElement("li");
  li.className = "empty";
  li.textContent = text;
  return li;
}

// The context menu of the file tree.
function showMenu(x: number, y: number, n: TreeNode) {
  // [label, capability (coverage.ts), action]
  const entries: [string, string, () => void][] = n.dir
    ? [
        [L("在这里新建笔记…", "ここに新規ノート…"), "write", () => void newNote(n.path + "/")],
        [L("在终端中打开", "ターミナルで開く"), "ui:shell", () => openTerminal(n.path)],
      ]
    : [
        [L("打开", "開く"), "editor.open", () => void openFile(n.path)],
        [L("重命名…", "名前を変更…"), "rename", () => void renameNote(n.path)],
        [L("复制 jus:// 链接", "jus:// リンクをコピー"), "link", () => void copyLink(n.path)],
        [L("在资源管理器中显示", "エクスプローラーで表示"), "ui:shell", () => reveal(n.path)],
        [L("删除…", "削除…"), "delete", () => void deleteNote(n.path)],
      ];
  menu.replaceChildren(
    ...entries.map(([label, capName, run], i) => {
      const b = document.createElement("button");
      b.dataset.cap = capName;
      b.className = "wide" + (!n.dir && i === entries.length - 1 ? " danger" : "");
      b.textContent = label;
      b.addEventListener("click", () => {
        closeMenu();
        run();
      });
      return b;
    }),
  );
  menu.classList.add("open");
  const r = menu.getBoundingClientRect();
  menu.style.left = Math.min(x, innerWidth - r.width - 8) + "px";
  menu.style.top = Math.min(y, innerHeight - r.height - 8) + "px";
}
function closeMenu() {
  menu.classList.remove("open");
}

// Back / forward through the notes you opened (Alt+← / Alt+→, mouse side buttons).
const back: { path: string; pos: number }[] = [];
const forward: { path: string; pos: number }[] = [];
let travelling = false;

async function travel(from: typeof back, to: typeof back) {
  const where = from.pop();
  if (!where) return;
  if (currentPath) to.push({ path: currentPath, pos: editor.state.selection.main.head });
  travelling = true;
  try {
    await openFile(where.path, where.pos);
  } finally {
    travelling = false;
  }
}
const goBack = () => travel(back, forward);
const goForward = () => travel(forward, back);

async function openFile(path: string, pos?: number) {
  if (path !== currentPath && !(await leaveNote())) return;
  if (!travelling && currentPath && path !== currentPath) {
    back.push({ path: currentPath, pos: editor.state.selection.main.head });
    if (back.length > 100) back.shift();
    forward.length = 0;
  }
  try {
    const note = await api.read(path);
    currentPath = path;
    setCurrentNote(path);
    version = note.version;
    tracked = note.tracked;
    dirty = false;
    conflict = false;
    // A note, or another text file: plain text, read-only when it cannot be written back as it was.
    const info = docs.find((d) => d.rel === path);
    const asNote = isNotePath(path);
    const readOnly = info?.readOnly;
    setDoc(editor, note.text, pos ?? session.cursors[path] ?? 0, { note: asNote, readOnly: !!readOnly });
    if (!asNote && rendered) setRendered(false);
    const enc = (note as { encoding?: string }).encoding ?? "";
    if (readOnly) toast(readOnly === "encoding" ? L(`只读：${enc.toUpperCase()} 编码`, `読み取り専用：${enc.toUpperCase()}`) : L("只读：文件过大", "読み取り専用：ファイルが大きすぎます"));
    setLintPath(path);
    forceLint(editor);
    words = wordCount(note.text);
    showSave("saved");
    paintTitle();
    renderFiles();
    renderOutline();
    if (rendered) renderPreview(note.text);
    updatePos();
    showEmpty(false);
    saveSession();
    scheduleBacklinks();
    await refreshGit(); // leaving the last note may just have committed it
  } catch (e) {
    toast(L("打不开：", "開けません：") + msg(e), true);
  }
}

function paintTitle() {
  const name = currentPath.replace(/^.*\//, "").replace(/\.(md|markdown)$/i, "");
  fileName.textContent = name;
  fileName.hidden = !name;
  stPath.textContent = currentPath;
  const t = (name ? name + " — " : "") + "Jusnote";
  document.title = t;
  window.kpTitle?.(t);
}

function closeFile() {
  currentPath = "";
  version = "";
  dirty = false;
  conflict = false;
  setDoc(editor, "");
  setLintPath("");
  gitChanges = [];
  setGitChanges(editor, []);
  paintTitle();
  paintSave();
  updatePos();
  renderFiles();
  renderOutline();
  renderChanges();
}

function showEmpty(show: boolean) {
  empty.classList.toggle("open", show);
}

async function newNote(dir = "") {
  if (!opened) {
    void chooseNotebook();
    return;
  }
  const { value, text } = await ask({
    title: L("新建笔记", "新規ノート"),
    input: { value: dir, placeholder: L("名称，可含文件夹，如 读书/某书", "名前。フォルダーも可：読書/本の名前"), select: [dir.length, dir.length] },
    choices: [
      { label: L("取消", "キャンセル"), value: false },
      { label: L("新建", "作成"), value: true, primary: true },
    ],
    cancel: false,
  });
  if (value) await createNote(notePath(text));
}

/** Creates a note with its title as the first line and opens it (or opens it if it exists). */
async function createNote(path: string) {
  if (!path || path.endsWith("/.md")) return;
  const hit = docs.find((d) => d.rel.toLowerCase() === path.toLowerCase());
  if (hit) {
    await openFile(hit.rel);
    return;
  }
  const title = path.replace(/^.*\//, "").replace(/\.(md|markdown)$/i, "");
  try {
    await api.write(path, "# " + title + "\n\n", "");
    await refreshFiles();
    await openFile(path, ("# " + title + "\n\n").length);
    editor.focus();
  } catch (e) {
    toast(L("无法新建：", "作成できません：") + msg(e), true);
  }
}

async function renameNote(path = currentPath) {
  if (!path) return;
  const { value, text, checked } = await ask({
    title: L("重命名", "名前を変更"),
    input: { value: path.replace(/\.(md|markdown)$/i, ""), select: [path.lastIndexOf("/") + 1, path.replace(/\.(md|markdown)$/i, "").length] },
    check: { label: L("同时更新指向它的链接", "このノートへのリンクも更新する"), value: true },
    choices: [
      { label: L("取消", "キャンセル"), value: false },
      { label: L("重命名", "変更"), value: true, primary: true },
    ],
    cancel: false,
  });
  const to = notePath(text);
  if (!value || !to || to === path) return;
  if (path === currentPath && !(await leaveNote())) return;
  try {
    const r = await api.rename(path, to, checked);
    if (session.cursors[path] !== undefined) session.cursors[r.path] = session.cursors[path];
    await refreshFiles();
    if (path === currentPath) {
      currentPath = "";
      await openFile(r.path, editor.state.selection.main.head);
    }
    if (r.updated.length) toast(L(`已更新 ${r.updated.length} 篇笔记里的链接`, `${r.updated.length} 件のノートのリンクを更新しました`));
    void refreshGit();
  } catch (e) {
    toast(L("无法重命名：", "名前を変更できません：") + msg(e), true);
  }
}

async function deleteNote(path = currentPath) {
  if (!path) return;
  const { value } = await ask({
    title: L("删除这篇笔记？", "このノートを削除しますか？"),
    body: path + "\n" + L("已提交的版本仍在历史里，上一版也留在 .jusnote/backup。", "コミット済みの版は履歴に残り、直前の版も .jusnote/backup に残ります。"),
    choices: [
      { label: L("取消", "キャンセル"), value: false },
      { label: L("删除", "削除"), value: true, primary: true },
    ],
    cancel: false,
  });
  if (!value) return;
  try {
    if (path === currentPath) {
      window.clearTimeout(autosaveTimer);
      closeFile();
    }
    const wasOpen = !currentPath;
    await api.remove(path);
    delete session.cursors[path];
    await refreshFiles();
    if (wasOpen && docs.length) await openFile((docs.find((d) => /^readme\.md$/i.test(d.rel)) ?? docs[0]).rel);
    void refreshGit();
    toast(L("已删除 ", "削除しました：") + path);
  } catch (e) {
    toast(L("无法删除：", "削除できません：") + msg(e), true);
  }
}

/** A terminal in the notebook's folder rel ("" : the notebook itself). */
function openTerminal(rel = "") {
  if (!root) return;
  const dir = root.replace(/[\\/]+$/, "") + (rel ? "\\" + rel.replace(/\//g, "\\") : "");
  window.kpTerminal?.(dir).catch((e) => toast(msg(e), true));
}

/** Shows a note in Explorer (the shell wants an absolute path). */
function reveal(rel: string) {
  if (rel && root) window.kpReveal?.(root.replace(/[\\/]+$/, "") + "\\" + rel.replace(/\//g, "\\"));
}

async function copyLink(path = currentPath) {
  if (!path) return;
  const line = path === currentPath ? editor.state.doc.lineAt(editor.state.selection.main.head).number : 0;
  const { link } = await cap<{ link: string }>("link", { path, line });
  try {
    await navigator.clipboard.writeText(link);
    toast(L("已复制 ", "コピーしました：") + link);
  } catch {
    toast(link);
  }
}

// --- rail: outline, changes, history -------------------------------------------------

let headingLines: number[] = [];
const OUTLINE_MAX = 300;
let markedHeading = -1;
function renderOutline() {
  const doc = editor.state.doc;
  const items: HTMLLIElement[] = [];
  headingLines = [];
  let inFence = false;
  for (let i = 1; i <= doc.lines; i++) {
    const text = doc.line(i).text;
    if (/^\s*(```|~~~)/.test(text)) inFence = !inFence;
    if (inFence) continue;
    const m = /^(#{1,6})\s+(.+?)\s*#*\s*$/.exec(text);
    if (!m) continue;
    headingLines.push(i);
    if (items.length >= OUTLINE_MAX) continue; // a note with thousands of headings: list the first ones
    const li = document.createElement("li");
    li.className = "lv" + m[1].length + " click";
    li.textContent = m[2];
    li.addEventListener("click", () => gotoLine(i));
    items.push(li);
  }
  if (headingLines.length > OUTLINE_MAX) items.push(emptyItem(L(`…还有 ${headingLines.length - OUTLINE_MAX} 个标题`, `…ほか ${headingLines.length - OUTLINE_MAX} 件の見出し`)));
  if (!items.length) items.push(emptyItem(currentPath ? L("没有标题", "見出しなし") : "—"));
  outlineEl.replaceChildren(...items);
  markedHeading = -1;
  markOutline(doc.lineAt(editor.state.selection.main.head).number);
}

function markOutline(line: number) {
  let cur = -1;
  for (let k = 0; k < headingLines.length && headingLines[k] <= line; k++) cur = k;
  if (cur === markedHeading) return;
  outlineEl.children[markedHeading]?.classList.remove("cur");
  if (cur < OUTLINE_MAX) outlineEl.children[cur]?.classList.add("cur");
  markedHeading = cur;
}

let outlineTimer = 0;
function scheduleOutline() {
  window.clearTimeout(outlineTimer);
  outlineTimer = window.setTimeout(renderOutline, 300);
}

function renderChanges() {
  const kind = { add: L("新增", "追加"), modify: L("修改", "変更"), delete: L("删除", "削除") };
  if (!gitChanges.length) {
    changesEl.replaceChildren(
      emptyItem(!currentPath ? "—" : tracked ? L("与上次提交相同", "前回のコミットと同じ") : L("新文件，尚未提交", "新しいファイル（未コミット）")),
    );
    return;
  }
  changesEl.replaceChildren(
    ...gitChanges.map((c) => {
      const li = document.createElement("li");
      li.className = `${c.kind} click`;
      const tag = document.createElement("span");
      tag.className = "tag";
      tag.textContent = kind[c.kind];
      li.append(tag, L(` 第 ${c.line} 行`, ` ${c.line} 行目`));
      li.addEventListener("click", () => gotoLine(c.line));
      return li;
    }),
  );
}

function renderUncommitted() {
  const pending = [...toReview.keys()];
  $("commit-all").hidden = pending.length === 0;
  if (!pending.length) {
    uncommittedEl.replaceChildren(emptyItem(opened ? L("没有待审阅的改动", "レビュー待ちの変更はありません") : "—"));
    return;
  }
  uncommittedEl.replaceChildren(
    ...pending.map((p) => {
      const li = document.createElement("li");
      li.textContent = p;
      li.title = p;
      li.className = "click";
      li.addEventListener("click", () => void review.openReview(p));
      return li;
    }),
  );
}

// Links to the open note from elsewhere, in the rail.
let backTimer = 0;
function scheduleBacklinks() {
  window.clearTimeout(backTimer);
  backTimer = window.setTimeout(() => void renderBacklinks(), 400);
}
async function renderBacklinks() {
  const el = $<HTMLUListElement>("backlinks");
  if (!currentPath || !rail.classList.contains("open")) {
    if (!currentPath) el.replaceChildren(emptyItem("—"));
    return;
  }
  const path = currentPath;
  let back: Backlink[] = [];
  try {
    back = (await api.links(path)).back;
  } catch {
    return;
  }
  if (path !== currentPath) return;
  if (!back.length) {
    el.replaceChildren(emptyItem(L("还没有笔记链接到这里", "ここへのリンクはまだありません")));
    return;
  }
  el.replaceChildren(
    ...back.map((b) => {
      const li = document.createElement("li");
      li.className = "click back";
      const from = document.createElement("span");
      from.className = "from";
      from.textContent = b.from.replace(/\.(md|markdown)$/i, "");
      const text = document.createElement("span");
      text.className = "sub";
      text.textContent = b.text;
      li.append(from, text);
      li.title = `${b.from}:${b.line}`;
      li.addEventListener("click", () => void openFile(b.from).then(() => gotoLine(b.line)));
      return li;
    }),
  );
}

function formatWhen(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  const p = (n: number) => String(n).padStart(2, "0");
  return `${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`;
}

async function renderRecent() {
  if (!opened) {
    recentEl.replaceChildren(emptyItem("—"));
    return;
  }
  const commits = await api.history(15).catch(() => []);
  if (!commits.length) {
    recentEl.replaceChildren(emptyItem(L("还没有提交", "コミットはまだありません")));
    return;
  }
  recentEl.replaceChildren(
    ...commits.map((c) => {
      const li = document.createElement("li");
      li.title = `${c.hash.slice(0, 7)} · ${c.author}`;
      const when = document.createElement("span");
      when.className = "tag";
      when.textContent = formatWhen(c.when);
      li.append(when, " " + c.message);
      return li;
    }),
  );
}

async function refreshGutter() {
  gitChanges = opened && currentPath ? await api.gutter(currentPath).catch(() => []) : [];
  setGitChanges(editor, gitChanges);
  renderChanges();
}

async function refreshGit() {
  if (!opened) {
    changedFiles = [];
    stGit.textContent = "";
    renderUncommitted();
    return;
  }
  const cs = await api.changes().catch(() => null);
  if (cs) {
    changedFiles = cs.map((c) => c.path);
    toReview = new Map(cs.filter((c) => !c.mine).map((c) => [c.path, c]));
  }
  const r = toReview.size;
  stGit.textContent = r ? L(`${r} 处待审阅`, `レビュー待ち ${r}`) : "";
  stGit.hidden = r === 0;
  stGit.classList.toggle("pending", r > 0);
  renderUncommitted();
  renderFiles();
  await Promise.all([refreshGutter(), rail.classList.contains("open") ? renderRecent() : undefined]);
}

/** Review of the uncommitted changes (the old "commit everything" goes through it). */
async function commitAll() {
  if (!(await leaveNote())) return;
  await review.openReview();
}

// --- rendered Markdown view ------------------------------------------------------

function noteDir(): string {
  return currentPath.includes("/") ? currentPath.slice(0, currentPath.lastIndexOf("/")) : "";
}

function renderPreview(text: string) {
  renderInto(preview, text, noteDir());
  void decorateJusLinks();
}

type LinkPreview = { app: string; installed: boolean; title?: string; at?: number; watched?: boolean; progress?: number; exists?: boolean; problem?: string };
const linkPreviews = new Map<string, Promise<LinkPreview | null>>();

const clock = (s: number) => {
  const t = Math.floor(s), h = Math.floor(t / 3600), m = Math.floor((t % 3600) / 60), sec = String(t % 60).padStart(2, "0");
  return h ? `${h}:${String(m).padStart(2, "0")}:${sec}` : `${m}:${sec}`;
};

/** jus:// links in the preview: what they point to, asked of their app (link.preview); a missing app says so. */
async function decorateJusLinks() {
  for (const a of preview.querySelectorAll<HTMLAnchorElement>('a[href^="jus://"]')) {
    const href = a.getAttribute("href")!;
    if (!linkPreviews.has(href)) linkPreviews.set(href, cap<LinkPreview>("link.preview", { link: href }).catch(() => null));
    const p = await linkPreviews.get(href);
    if (!p || !a.isConnected) continue;
    a.classList.add("jus-link", "jus-" + p.app.replace(/^jus/, ""));
    a.classList.toggle("jus-missing", !p.installed || p.exists === false || !!p.problem);
    const name = p.app.replace(/^jus/, "Jus");
    if (!p.installed) a.title = L("未安装 ", "未インストール：") + name;
    else if (p.problem) a.title = name + L(" 打不开：", " で開けません：") + p.problem; // the app's own words, after ours
    else if (p.exists === false) a.title = L("没有这篇笔记", "このノートはありません");
    else {
      const parts = [p.title ?? ""];
      if (p.at) parts.push(clock(p.at));
      if (p.watched) parts.push(L("已看完", "視聴済み"));
      else if (p.progress && p.progress > 0.01) parts.push(L("看到 ", "視聴 ") + Math.round(p.progress * 100) + "%");
      a.title = parts.filter(Boolean).join(" · ");
    }
  }
}

/** Opens the note a wiki link names; offers to create it when there is none. */
async function followWiki(target: string) {
  const { path } = await api.resolve(target, currentPath).catch(() => ({ path: "" }));
  if (path) {
    await openFile(path);
    return;
  }
  const { value } = await ask({
    title: L(`还没有「${target}」`, `「${target}」はまだありません`),
    body: L("要新建这篇笔记吗？", "このノートを作成しますか？"),
    choices: [
      { label: L("取消", "キャンセル"), value: false },
      { label: L("新建", "作成"), value: true, primary: true },
    ],
    cancel: false,
  });
  if (value) await createNote(target.includes("/") ? notePath(target) : notePath((noteDir() ? noteDir() + "/" : "") + target));
}

// Links: [[wiki]] resolves like the server does, jus://note opens that note
// here, web links go to the system browser, a relative .md link opens that
// note, and nothing navigates the page away.
function followLink(href: string) {
  if (href.startsWith("wiki:")) {
    void followWiki(href.slice(5));
  } else if (href.startsWith("jus://note/")) {
    const u = href.slice("jus://note/".length);
    const [p, query = ""] = u.split("?");
    const line = Number(new URLSearchParams(query).get("line")) || 0;
    const path = decodeURIComponent(p.split("#")[0]);
    void openFile(path).then(() => line && gotoLine(line));
  } else if (/^(https?:|mailto:)/i.test(href)) {
    window.kpOpenExternal?.(href);
  } else if (/^jus:\/\//.test(href)) {
    // Another Jus app's (jus://play/… is Jusplay's): it opens there.
    void (window.kpOpenJus?.(href) ?? Promise.resolve("notinstalled")).then((r) => {
      if (r === "notinstalled") toast(L("未安装 ", "未インストール：") + "Jus" + href.split("/")[2]); // as the link's tooltip says
    }, (e) => toast(String(e)));
  } else if (/\.(md|markdown)(#.*)?$/i.test(href)) {
    const dir = currentPath.includes("/") ? currentPath.slice(0, currentPath.lastIndexOf("/") + 1) : "";
    const parts: string[] = [];
    for (const seg of (dir + decodeURIComponent(href.split("#")[0])).split("/")) {
      if (seg === "..") parts.pop();
      else if (seg && seg !== ".") parts.push(seg);
    }
    void openFile(parts.join("/"));
  }
}
setLinkHandler(followLink);

preview.addEventListener("click", (e) => {
  const t = e.target as HTMLElement;
  // A task box writes back to the note (and autosaves like typing).
  if (t instanceof HTMLInputElement && t.type === "checkbox" && t.dataset.task) {
    e.preventDefault();
    const next = toggleTask(docText(editor), Number(t.dataset.task));
    if (next !== null) replaceText(next);
    return;
  }
  const a = t.closest("a");
  if (!a) return;
  e.preventDefault();
  if (a.dataset.wiki) void followWiki(a.dataset.wiki);
  else followLink(a.getAttribute("href") ?? "");
});

/** Replaces the whole note as an edit (undoable, autosaved), keeping the preview's scroll. */
function replaceText(text: string) {
  const top = preview.scrollTop;
  editor.dispatch({ changes: { from: 0, to: editor.state.doc.length, insert: text } });
  preview.scrollTop = top;
}

// Pasted or dropped files go next to the note, in "attachments/".
setFileHandler(async (files) => {
  if (!currentPath) return null;
  const out: string[] = [];
  for (const f of files) {
    if (f.size > 50 << 20) {
      toast(L("文件太大（上限 50 MB）：", "ファイルが大きすぎます（上限 50 MB）：") + f.name, true);
      continue;
    }
    const data = await new Promise<string>((ok, bad) => {
      const r = new FileReader();
      r.onload = () => ok(String(r.result).replace(/^data:[^,]*,/, ""));
      r.onerror = () => bad(r.error);
      r.readAsDataURL(f);
    });
    let name = f.name || "file";
    if (/^image\.\w+$/i.test(name)) {
      const d = new Date();
      const p = (n: number) => String(n).padStart(2, "0");
      name = `image-${d.getFullYear()}${p(d.getMonth() + 1)}${p(d.getDate())}-${p(d.getHours())}${p(d.getMinutes())}${p(d.getSeconds())}${name.slice(name.lastIndexOf("."))}`;
    }
    try {
      const r = await api.attach(currentPath, name, data);
      attachedHere.push(r.path);
      const label = name.replace(/\.[^.]+$/, "").replace(/[[\]]/g, "");
      out.push(f.type.startsWith("image/") ? `![${label}](${r.link})` : `[${name.replace(/[[\]]/g, "")}](${r.link})`);
    } catch (e) {
      toast(L("无法保存附件：", "添付ファイルを保存できません：") + msg(e), true);
    }
  }
  return out.length ? out.join("\n") : null;
});

function setRendered(on: boolean) {
  if (on && !currentPath) return;
  rendered = on;
  if (on) renderPreview(docText(editor));
  preview.hidden = !on;
  editor.dom.style.visibility = on ? "hidden" : "";
  paintRenderButton();
  if (!on) editor.focus();
}
function paintRenderButton() {
  $("btn-render").textContent = rendered ? L("源码", "ソース") : L("预览", "プレビュー");
  $("btn-render").setAttribute("aria-pressed", String(rendered));
}

// --- notebooks ------------------------------------------------------------------------

async function chooseNotebook() {
  let folder = "";
  if (window.kpPickFolder) folder = await window.kpPickFolder(L("选择笔记本文件夹", "ノートブックのフォルダーを選択"));
  else {
    const r = await ask({
      title: L("笔记本文件夹", "ノートブックのフォルダー"),
      input: { placeholder: "D:\\notes" },
      choices: [
        { label: L("取消", "キャンセル"), value: false },
        { label: L("打开", "開く"), value: true, primary: true },
      ],
      cancel: false,
    });
    if (r.value) folder = r.text.trim();
  }
  if (folder) await openNotebook(folder);
}

async function openNotebook(folder: string) {
  if (!(await leaveNote())) return;
  try {
    await api.open(folder);
    await load();
  } catch (e) {
    toast(L("打不开这个文件夹：", "このフォルダーを開けません：") + msg(e), true);
  }
}

async function renderRecentNotebooks(list: HTMLUListElement, onPick: () => void) {
  const recent = await api.recent().catch(() => []);
  if (!recent.length) {
    list.replaceChildren(emptyItem(L("还没有打开过笔记本", "開いたノートブックはまだありません")));
    return;
  }
  list.replaceChildren(
    ...recent.map((r) => {
      const li = document.createElement("li");
      li.className = "click" + (r.path === root ? " active" : "");
      const name = document.createElement("span");
      name.textContent = r.name;
      const path = document.createElement("span");
      path.className = "sub";
      path.textContent = r.path;
      li.append(name, path);
      li.title = r.path;
      li.addEventListener("click", () => {
        onPick();
        if (r.path !== root) void openNotebook(r.path);
      });
      return li;
    }),
  );
}

// --- panels -------------------------------------------------------------------------------

function closePanels(except?: string) {
  if (except !== "nb") nbPopover.classList.remove("open");
  if (except !== "settings") settings.classList.remove("open");
  if (except !== "files") filesPanel.classList.remove("open");
  closeMenu();
  $("pill").classList.toggle("active", except === "nb");
  $("btn-settings").setAttribute("aria-expanded", String(except === "settings"));
  $("btn-files").setAttribute("aria-expanded", String(except === "files"));
}

function toggleNbPopover(force?: boolean) {
  const show = force ?? !nbPopover.classList.contains("open");
  closePanels(show ? "nb" : undefined);
  nbPopover.classList.toggle("open", show);
  if (show) void renderRecentNotebooks(nbRecent, () => toggleNbPopover(false));
}

function toggleSettings(force?: boolean) {
  const show = force ?? !settings.classList.contains("open");
  closePanels(show ? "settings" : undefined);
  settings.classList.toggle("open", show);
}

function toggleFiles(force?: boolean) {
  const show = force ?? !filesPanel.classList.contains("open");
  closePanels(show ? "files" : undefined);
  filesPanel.classList.toggle("open", show);
}

function toggleRail(force?: boolean) {
  const show = force ?? !rail.classList.contains("open");
  rail.classList.toggle("open", show);
  document.body.classList.toggle("rail-open", show);
  $("btn-rail").setAttribute("aria-expanded", String(show));
  if (show) {
    void renderRecent();
    void renderBacklinks();
  }
}

// --- palette: commands and search -------------------------------------------------------

type Item = { label: string; hint?: string; sub?: string; head?: boolean; mark?: string; run?: () => void | Promise<void> };
type Mode = "commands" | "search";
let mode: Mode = "commands";
let shown: Item[] = [];
let sel = 0;
let searchSeq = 0;

function openPalette(m: Mode, value = "") {
  mode = m;
  closePanels();
  paletteInput.value = value;
  paletteInput.placeholder = m === "commands" ? L("输入命令", "コマンドを入力") : L("搜索笔记名或内容", "ノート名や内容を検索");
  overlay.classList.add("open");
  overlay.dataset.mode = m;
  sel = 0;
  void refreshPalette();
  paletteInput.focus();
  paletteInput.select();
}

function closePalette() {
  overlay.classList.remove("open");
}

async function refreshPalette() {
  const q = paletteInput.value.trim();
  if (mode === "commands") {
    const lq = q.toLowerCase();
    shown = commands().filter((it) => !lq || it.label.toLowerCase().includes(lq) || fuzzyScore(lq, it.label) > 0);
    paintPalette();
    return;
  }
  const names = docs
    .map((d) => ({ d, s: q ? fuzzyScore(q, d.rel) : 0 }))
    .filter((x) => x.s >= 0)
    .sort((a, b) => (q ? b.s - a.s : Date.parse(b.d.modTime) - Date.parse(a.d.modTime)))
    .slice(0, q ? 8 : 12)
    .map(({ d }): Item => ({ label: d.rel.replace(/\.(md|markdown)$/i, ""), run: () => openFile(d.rel) }));
  const head = (label: string): Item => ({ label, head: true });
  shown = names.length ? [head(q ? L("笔记", "ノート") : L("最近修改", "最近の変更")), ...names] : [];
  paintPalette();
  if (q.length < 2) return;
  const seq = ++searchSeq;
  let hits: Hit[] = [];
  try {
    hits = await api.search(q);
  } catch {
    return;
  }
  if (seq !== searchSeq || mode !== "search") return;
  if (hits.length) {
    shown = [
      ...shown,
      head(L(`内容 · ${hits.length}${hits.length >= 300 ? "+" : ""}`, `本文 · ${hits.length}${hits.length >= 300 ? "+" : ""}`)),
      ...hits.map((h): Item => ({
        label: h.text,
        mark: q,
        sub: `${h.rel.replace(/\.(md|markdown)$/i, "")}:${h.line}`,
        run: () => openFile(h.rel).then(() => gotoLine(h.line, q)),
      })),
    ];
  } else if (!names.length) {
    shown = [{ label: L("没有找到", "見つかりません"), head: true }];
  }
  paintPalette();
}

function selectable(i: number) {
  return shown[i] && !shown[i].head;
}

function paintPalette() {
  if (!selectable(sel)) sel = shown.findIndex((_, i) => selectable(i));
  paletteList.replaceChildren(
    ...shown.map((it, i) => {
      const li = document.createElement("li");
      if (it.head) {
        li.className = "head";
        li.textContent = it.label;
        return li;
      }
      if (i === sel) li.className = "sel";
      li.setAttribute("role", "option");
      const label = document.createElement("span");
      label.className = "label";
      if (it.mark) {
        const at = it.label.toLowerCase().indexOf(it.mark.toLowerCase());
        if (at >= 0) {
          const m = document.createElement("mark");
          m.textContent = it.label.slice(at, at + it.mark.length);
          label.append(it.label.slice(0, at), m, it.label.slice(at + it.mark.length));
        } else label.textContent = it.label;
      } else label.textContent = it.label;
      li.append(label);
      if (it.sub) {
        const sub = document.createElement("span");
        sub.className = "sub";
        sub.textContent = it.sub;
        li.append(sub);
      }
      if (it.hint) li.append(...keyCaps(it.hint));
      li.addEventListener("mousemove", () => {
        if (sel !== i) {
          sel = i;
          paletteList.querySelector(".sel")?.classList.remove("sel");
          li.classList.add("sel");
        }
      });
      li.addEventListener("click", () => void runItem(it));
      return li;
    }),
  );
  (paletteList.children[sel] as HTMLElement | undefined)?.scrollIntoView({ block: "nearest" });
}

function keyCaps(keys: string): HTMLElement[] {
  const wrap = document.createElement("span");
  wrap.className = "keys";
  for (const k of keys.split(" ")) {
    const kbd = document.createElement("kbd");
    kbd.textContent = k;
    wrap.append(kbd);
  }
  return [wrap];
}

async function runItem(it: Item) {
  if (!it.run) return;
  closePalette();
  await it.run();
}

/** cap: the capabilities it uses (coverage.ts), "ui" when it only moves the view. */
type Command = { zh: string; ja: string; cap: string; key?: string; run: () => void | Promise<void>; when?: () => boolean };

function commandList(): Command[] {
  const hasNote = () => !!currentPath;
  return [
    { zh: "新建笔记…", ja: "新規ノート…", cap: "write", key: "Ctrl N", run: () => newNote() },
    { zh: "搜索笔记…", ja: "ノートを検索…", cap: "search list", key: "Ctrl P", run: () => openPalette("search") },
    { zh: "在当前笔记中查找", ja: "このノート内を検索", cap: "ui", key: "Ctrl F", run: () => findInNote(), when: hasNote },
    { zh: "保存并提交", ja: "保存してコミット", cap: "write commit", key: "Ctrl S", run: saveAndCommit, when: hasNote },
    { zh: "审阅改动…", ja: "変更をレビュー…", cap: "status diff commit discard", key: "Ctrl Shift G", run: commitAll, when: () => toReview.size > 0 },
    { zh: "这篇笔记的历史…", ja: "このノートの履歴…", cap: "log show restore", key: "Ctrl Shift H", run: () => review.openHistory(currentPath), when: hasNote },
    { zh: "插入指向笔记的链接", ja: "ノートへのリンクを挿入", cap: "list", key: "[[", run: () => insertAtCursor("[["), when: hasNote },
    { zh: "预览 / 源码", ja: "プレビュー / ソース", cap: "ui", key: "Ctrl Shift V", run: () => setRendered(!rendered), when: hasNote },
    { zh: "文件", ja: "ファイル", cap: "list", key: "Ctrl O", run: () => toggleFiles() },
    { zh: "大纲与历史", ja: "アウトラインと履歴", cap: "log", key: "Ctrl J", run: () => toggleRail() },
    { zh: "重命名当前笔记…", ja: "このノートの名前を変更…", cap: "rename", run: () => renameNote(), when: hasNote },
    { zh: "删除当前笔记…", ja: "このノートを削除…", cap: "delete", run: () => deleteNote(), when: hasNote },
    { zh: "复制当前行的 jus:// 链接", ja: "この行の jus:// リンクをコピー", cap: "link", run: () => copyLink(), when: hasNote },
    { zh: "在资源管理器中显示", ja: "エクスプローラーで表示", cap: "ui:shell", run: () => reveal(currentPath), when: hasNote },
    { zh: "在终端中打开笔记本", ja: "ノートブックをターミナルで開く", cap: "ui:shell", run: () => openTerminal(), when: () => opened },
    { zh: "快速记录到日志…", ja: "日誌にすばやく記録…", cap: "capture", run: quickCapture },
    { zh: "让 AI agent 能在这里工作…", ja: "AI エージェントがここで作業できるようにする…", cap: "agents", run: prepareAgents, when: () => opened },
    { zh: "打开笔记本文件夹…", ja: "ノートブックのフォルダーを開く…", cap: "notebook.open", run: chooseNotebook },
    { zh: "增大字号", ja: "文字を大きく", cap: "prefs.set", key: "Ctrl +", run: () => bumpSize(1) },
    { zh: "减小字号", ja: "文字を小さく", cap: "prefs.set", key: "Ctrl -", run: () => bumpSize(-1) },
    { zh: "恢复默认字号", ja: "文字サイズを既定に戻す", cap: "prefs.set", key: "Ctrl 0", run: () => setPref("size", SIZE_DEFAULT) },
    { zh: "切换日间 / 夜间", ja: "ライト / ダークを切り替え", cap: "prefs.set", run: () => setPref("theme", prefs.theme === "dark" ? "light" : "dark") },
    { zh: "运行技能…", ja: "スキルを実行…", cap: "skills.list skills.run", when: () => opened, run: () => void runSkill() },
    { zh: "自动提交：开 / 关（本笔记本）", ja: "自動コミット：オン / オフ（このノートブック）", cap: "config.get config.set", when: () => opened, run: toggleAutoCommit },
    { zh: "文件列表：显示所有文本文件 / 只看笔记", ja: "ファイル一覧：すべてのテキスト / ノートのみ", cap: "prefs.set", run: () => { setPref("files", prefs.files === "all" ? "notes" : "all"); renderFiles(); } },
    { zh: "切换到日本語", ja: "中文に切り替え", cap: "prefs.set", run: () => setPref("lang", prefs.lang === "ja" ? "zh" : "ja") },
    { zh: "快捷键", ja: "ショートカット", cap: "ui", key: "F1", run: showHelp },
    { zh: "后退", ja: "戻る", cap: "editor.open", key: "Alt ←", run: goBack, when: () => back.length > 0 },
    { zh: "前进", ja: "進む", cap: "editor.open", key: "Alt →", run: goForward, when: () => forward.length > 0 },
    { zh: "复制诊断日志", ja: "診断ログをコピー", cap: "ui", run: copyLog },
  ];
}

function commands(): Item[] {
  return commandList()
    .filter((c) => !c.when || c.when())
    .map((c) => ({ label: L(c.zh, c.ja), hint: c.key, run: c.run }));
}

function findInNote() {
  if (rendered) setRendered(false);
  editor.focus();
  openSearchPanel(editor);
}

function bumpSize(d: number) {
  setPref("size", Math.min(SIZE_MAX, Math.max(SIZE_MIN, prefs.size + d)));
}

async function quickCapture() {
  if (!opened) {
    void chooseNotebook();
    return;
  }
  const types = await api.types().catch(() => []);
  const log = types.find((t) => t.id === "daily-log");
  if (!log) {
    toast(L("这个笔记本没有日志类型", "このノートブックには日誌タイプがありません"), true);
    return;
  }
  const s = await ask({
    title: L("记到哪一栏", "どの欄に記録しますか"),
    options: log.sections.map((x) => ({ value: x.name, label: x.name })),
    choices: [
      { label: L("取消", "キャンセル"), value: false },
      { label: L("下一步", "次へ"), value: true, primary: true },
    ],
    cancel: false,
  });
  if (!s.value) return;
  const t = await ask({
    title: s.text,
    input: { placeholder: L("内容", "内容") },
    choices: [
      { label: L("取消", "キャンセル"), value: false },
      { label: L("记录", "記録"), value: true, primary: true },
    ],
    cancel: false,
  });
  if (!t.value || !t.text.trim()) return;
  try {
    if (!(await leaveNote())) return;
    const r = await api.capture("daily-log", s.text, t.text.trim());
    await refreshFiles();
    currentPath = "";
    await openFile(r.path);
    toast(L("已记录并提交", "記録してコミットしました"));
  } catch (e) {
    toast(L("记录失败：", "記録できません：") + msg(e), true);
  }
}

/** Writes AGENTS.md (+ CLAUDE.md / GEMINI.md bridges) so coding agents know how to work in this notebook. */
async function prepareAgents() {
  const { value } = await ask({
    title: L("让 AI agent 能在这里工作", "AI エージェントがここで作業できるようにする"),
    body: L(
      "在笔记本根目录写入 AGENTS.md、CLAUDE.md、GEMINI.md：用 jusnote 命令，改完由你审阅。已有的不覆盖。",
      "ノートブック直下に AGENTS.md・CLAUDE.md・GEMINI.md を置きます：jusnote コマンドを使い、変更はあなたがレビュー。既存のものは上書きしません。",
    ),
    choices: [
      { label: L("取消", "キャンセル"), value: false },
      { label: L("写入", "書き込む"), value: true, primary: true },
    ],
    cancel: false,
  });
  if (!value) return;
  try {
    const r = await cap<{ files: { path: string; action: string }[]; committed: boolean }>("agents");
    const words = { written: L("已写入", "作成"), updated: L("已更新", "更新"), unchanged: L("未变", "変更なし"), kept: L("保留你的", "既存を保持") } as Record<string, string>;
    toast(r.files.map((f) => `${f.path} ${words[f.action] ?? f.action}`).join("\n"));
    await refreshFiles();
    await refreshGit();
  } catch (e) {
    toast(msg(e), true);
  }
}

async function copyLog() {
  try {
    await navigator.clipboard.writeText(await api.log.get());
    toast(L("诊断日志已复制", "診断ログをコピーしました"));
  } catch (e) {
    toast(msg(e), true);
  }
}

// --- help --------------------------------------------------------------------------------------

function showHelp() {
  closePanels();
  closePalette();
  const extra: [string, string, string][] = [
    ["Ctrl H", "替换", "置換"],
    ["Ctrl B", "粗体", "太字"],
    ["Ctrl I", "斜体", "斜体"],
    ["Ctrl E", "行内代码", "インラインコード"],
    ["Ctrl Click", "打开链接（[[笔记]] 也可以）", "リンクを開く（[[ノート]] も）"],
    ["Ctrl V", "粘贴图片（存到 attachments/）", "画像を貼り付け（attachments/ に保存）"],
    ["Alt Click", "多个光标", "複数カーソル"],
    ["Ctrl Z", "撤销", "元に戻す"],
    ["Ctrl Shift Z", "重做", "やり直し"],
    ["Esc", "关闭面板", "パネルを閉じる"],
  ];
  const rows = [...commandList().filter((c) => c.key).map((c) => [c.key!, c.zh, c.ja] as [string, string, string]), ...extra];
  $("help-keys").replaceChildren(
    ...rows.flatMap(([key, zh, ja]) => {
      const dt = document.createElement("dt");
      dt.append(...keyCaps(key));
      const dd = document.createElement("dd");
      dd.textContent = L(zh, ja).replace(/…$/, "");
      return [dt, dd];
    }),
  );
  $("help-version").textContent = "Jusnote " + (window.jusVersion ?? "");
  help.classList.add("open");
  $("help-done").focus();
}
function closeHelp() {
  help.classList.remove("open");
}

// --- settings --------------------------------------------------------------------------------

function paintSettings() {
  for (const [id, v] of [
    ["set-theme", prefs.theme],
    ["set-lang", prefs.lang],
    ["set-font", prefs.font],
  ] as const) {
    $(id).querySelectorAll<HTMLButtonElement>("button").forEach((b) => b.setAttribute("aria-pressed", String(b.dataset.v === v)));
  }
  $("size-val").textContent = String(prefs.size);
  for (const [id, on] of [
    ["set-wrap", prefs.wrap],
    ["set-numbers", prefs.numbers],
    ["set-guides", prefs.guides],
  ] as const) {
    $(id).setAttribute("aria-checked", String(on));
  }
}

// --- session -------------------------------------------------------------------------------------

function saveSession() {
  if (!opened) return;
  if (currentPath) session.cursors[currentPath] = editor.state.selection.main.head;
  session.file = currentPath;
  void api.session.set(session).catch(() => undefined);
}

// --- language ----------------------------------------------------------------------------------------

const STATIC: StaticText[] = [
  ["#btn-files", "textContent", "文件", "ファイル"],
  ["#btn-files", "aria-label", "文件", "ファイル"],
  ["#btn-search", "textContent", "搜索", "検索"],
  ["#btn-search", "aria-label", "搜索笔记", "ノートを検索"],
  ["#btn-rail", "textContent", "大纲", "アウトライン"],
  ["#btn-rail", "aria-label", "大纲、改动与历史", "アウトライン・変更・履歴"],
  ["#btn-render", "aria-label", "预览 / 源码", "プレビュー / ソース"],
  ["#btn-palette", "textContent", "命令", "コマンド"],
  ["#btn-palette", "aria-label", "全部命令", "すべてのコマンド"],
  ["#btn-settings", "textContent", "设置", "設定"],
  ["#btn-settings", "aria-label", "设置", "設定"],
  ["#st-git", "aria-label", "未提交的改动", "未コミットの変更"],
  ["#pill", "aria-label", "切换笔记本", "ノートブックを切り替え"],
  ["#h-outline", "textContent", "大纲", "アウトライン"],
  ["#h-changes", "textContent", "本篇相对上次提交", "前回のコミットからの変更"],
  ["#h-uncommitted", "textContent", "待审阅", "レビュー待ち"],
  ["#commit-all", "textContent", "审阅", "レビュー"],
  ["#h-back", "textContent", "链接到这篇", "このノートへのリンク"],
  ["#h-recent", "textContent", "最近提交", "最近のコミット"],
  ["#t-notebooks", "textContent", "笔记本", "ノートブック"],
  ["#nb-open", "textContent", "打开其它文件夹…", "ほかのフォルダーを開く…"],
  ["#l-theme", "textContent", "外观", "外観"],
  ["#set-theme [data-v=light]", "textContent", "日间", "ライト"],
  ["#set-theme [data-v=dark]", "textContent", "夜间", "ダーク"],
  ["#l-lang", "textContent", "语言", "言語"],
  ["#l-font", "textContent", "字体", "フォント"],
  ["#set-font [data-v=sans]", "textContent", "无衬线", "ゴシック"],
  ["#set-font [data-v=mono]", "textContent", "等宽", "等幅"],
  ["#l-size", "textContent", "字号", "文字サイズ"],
  ["#l-wrap", "textContent", "自动换行", "自動折り返し"],
  ["#l-numbers", "textContent", "行号", "行番号"],
  ["#l-guides", "textContent", "彩虹缩进线", "インデントガイド"],
  ["#set-help", "textContent", "快捷键", "ショートカット"],
  ["#set-log", "textContent", "复制诊断日志", "診断ログをコピー"],
  ["#set-agents", "textContent", "让 AI agent 能在这里工作…", "AI エージェント用の説明を置く…"],
  ["#t-notes", "textContent", "笔记", "ノート"],
  ["#new-note", "textContent", "新建", "新規"],
  ["#t-empty", "textContent", "这个笔记本还是空的。", "このノートブックはまだ空です。"],
  ["#empty-new", "textContent", "新建第一篇笔记", "最初のノートを作成"],
  ["#help-title", "textContent", "快捷键", "ショートカット"],
  ["#help-done", "textContent", "完成", "完了"],
  ["#t-welcome", "textContent", "选一个文件夹作笔记本。笔记就是里面的 Markdown 文件，每次保存都记入 git 历史。", "フォルダーをひとつノートブックに選びます。ノートは中の Markdown ファイルで、保存するたびに git の履歴に残ります。"],
  ["#pick-folder", "textContent", "选择文件夹…", "フォルダーを選択…"],
];

function applyLang() {
  applyStatic(STATIC);
  if (!opened) nbName.textContent = L("未选择笔记本", "ノートブック未選択");
  paintRenderButton();
  paintSave();
  paintWords();
  paintSettings();
  renderChanges();
  renderUncommitted();
  renderFiles();
  renderOutline();
  if (opened) void refreshGit();
  if (overlay.classList.contains("open")) void refreshPalette();
}

// --- wiring --------------------------------------------------------------------------------------

function wire() {
  const tips: [string, string][] = [
    ["btn-files", "Ctrl+O"],
    ["btn-search", "Ctrl+P"],
    ["btn-rail", "Ctrl+J"],
    ["btn-render", "Ctrl+Shift+V"],
    ["btn-palette", "Ctrl+K"],
    ["btn-settings", ""],
  ];
  for (const [id, key] of tips) {
    const b = $(id);
    b.setAttribute("data-tip", "");
    b.setAttribute("data-tip-at", "below");
    b.dataset.key = key;
  }

  $("pill").addEventListener("click", () => toggleNbPopover());
  $("nb-open").addEventListener("click", () => {
    closePanels();
    void chooseNotebook();
  });
  $("pick-folder").addEventListener("click", () => void chooseNotebook());
  $("empty-new").addEventListener("click", () => void newNote());
  $("btn-palette").addEventListener("click", () => openPalette("commands"));
  $("btn-search").addEventListener("click", () => openPalette("search"));
  $("btn-render").addEventListener("click", () => setRendered(!rendered));
  $("btn-settings").addEventListener("click", () => toggleSettings());
  $("btn-files").addEventListener("click", () => toggleFiles());
  $("btn-rail").addEventListener("click", () => toggleRail());
  $("st-git").addEventListener("click", () => void review.toggleReview());
  $("new-note").addEventListener("click", () => void newNote());
  $("commit-all").addEventListener("click", () => void commitAll());
  $("help-done").addEventListener("click", closeHelp);
  help.addEventListener("mousedown", (e) => e.target === help && closeHelp());
  $("set-help").addEventListener("click", showHelp);
  $("set-log").addEventListener("click", () => void copyLog());
  $("set-agents").addEventListener("click", () => {
    closePanels();
    void prepareAgents();
  });

  for (const [id, key] of [
    ["set-theme", "theme"],
    ["set-lang", "lang"],
    ["set-font", "font"],
  ] as const) {
    $(id).addEventListener("click", (e) => {
      const v = (e.target as HTMLElement).closest<HTMLButtonElement>("button")?.dataset.v;
      if (v) setPref(key, v as never);
    });
  }
  $("size-dec").addEventListener("click", () => bumpSize(-1));
  $("size-inc").addEventListener("click", () => bumpSize(1));
  $("set-wrap").addEventListener("click", () => setPref("wrap", !prefs.wrap));
  $("set-numbers").addEventListener("click", () => setPref("numbers", !prefs.numbers));
  $("set-guides").addEventListener("click", () => setPref("guides", !prefs.guides));

  document.addEventListener("mousedown", (e) => {
    const t = e.target as Node;
    if (menu.classList.contains("open") && !menu.contains(t)) closeMenu();
    if (
      (filesPanel.classList.contains("open") && !filesPanel.contains(t) && !$("btn-files").contains(t) && !menu.contains(t)) ||
      (nbPopover.classList.contains("open") && !nbPopover.contains(t) && !$("pill").contains(t)) ||
      (settings.classList.contains("open") && !settings.contains(t) && !$("btn-settings").contains(t))
    ) {
      closePanels();
    }
  });
  overlay.addEventListener("mousedown", (e) => {
    if (e.target === overlay) closePalette();
  });
  let inputTimer = 0;
  paletteInput.addEventListener("input", () => {
    sel = 0;
    window.clearTimeout(inputTimer);
    inputTimer = window.setTimeout(() => void refreshPalette(), mode === "search" ? 120 : 0);
  });
  paletteInput.addEventListener("keydown", (e) => {
    const step = (d: number) => {
      e.preventDefault();
      let i = sel;
      do i += d;
      while (i >= 0 && i < shown.length && !selectable(i));
      if (i >= 0 && i < shown.length) {
        sel = i;
        paintPalette();
      }
    };
    if (e.key === "ArrowDown") step(1);
    else if (e.key === "ArrowUp") step(-1);
    else if (e.key === "Enter" && !e.isComposing) {
      e.preventDefault();
      const it = shown[sel];
      if (it && !it.head) void runItem(it);
    }
  });

  window.addEventListener("keydown", (e) => {
    if (dialogOpen()) return;
    const mod = e.ctrlKey || e.metaKey;
    const key = e.key.toLowerCase();
    const run = (fn: () => unknown) => {
      e.preventDefault();
      void fn();
    };
    if (e.key === "Escape") {
      if (help.classList.contains("open")) closeHelp();
      else if (overlay.classList.contains("open")) closePalette();
      else if (menu.classList.contains("open")) closeMenu();
      else if (review.isOpen()) review.close();
      else closePanels();
      if (!rendered && !dialogOpen() && !overlay.classList.contains("open")) editor.focus();
      return;
    }
    if (e.key === "F1") return run(showHelp);
    if (e.altKey && e.key === "ArrowLeft") return run(goBack);
    if (e.altKey && e.key === "ArrowRight") return run(goForward);
    if (!mod) return;
    if (key === "s") run(saveAndCommit);
    else if (key === "p" || (e.shiftKey && key === "f")) run(() => openPalette("search"));
    else if (key === "k") run(() => openPalette("commands"));
    else if (key === "n") run(() => newNote());
    else if (key === "o") run(() => toggleFiles());
    else if (key === "j") run(() => toggleRail());
    else if (e.shiftKey && key === "v") run(() => setRendered(!rendered));
    else if (key === "=" || key === "+") run(() => bumpSize(1));
    else if (key === "-" || key === "_") run(() => bumpSize(-1));
    else if (key === "0") run(() => setPref("size", SIZE_DEFAULT));
    else if (key === "/") run(showHelp);
    else if (e.shiftKey && key === "g") run(() => review.toggleReview());
    else if (e.shiftKey && key === "h" && currentPath) run(() => review.toggleHistory(currentPath));
    else if ((key === "f" || key === "h") && currentPath) run(findInNote);
  });

  window.addEventListener("mouseup", (e) => {
    if (e.button === 3) void goBack();
    if (e.button === 4) void goForward();
  });
  // Outside edits are looked for while the window is in use, and at once
  // when it comes back.
  window.setInterval(() => void watch(), WATCH_MS);
  // The live session (live.ts): what is shown, for the command line; its
  // commands; and changes made elsewhere, shown at once.
  live.setState(() => ({
    page: opened ? "editor" : "welcome", path: currentPath || undefined,
    line: currentPath ? editor.state.doc.lineAt(editor.state.selection.main.head).number : undefined, dirty,
  }));
  live.listen(async (c) => {
    window.kpFront?.().catch(() => undefined);
    if (c.cmd === "notebook" && c.folder) return openNotebook(c.folder);
    if (c.cmd === "pref" && c.key) {
      setPref(c.key as keyof typeof prefs, c.value as never);
      if (c.key === "files") renderFiles();
      return;
    }
    if (c.cmd !== "open" || !c.path) throw new Error(`not an editor command: ${c.cmd}`);
    await openFile(c.path);
    if (currentPath !== c.path) throw new Error("the note did not open (unsaved changes kept?)");
    if (c.line) gotoLine(c.line);
    live.report();
  }, (ev) => {
    if (ev.kind !== "changed" || ev.source?.via === "window") return;
    void watch();
    void refreshGit();
    void refreshFiles();
    void review.refreshOpen();
  });
  for (const ev of ["keyup", "mouseup"]) window.addEventListener(ev, () => live.report());
  window.addEventListener("focus", () => {
    void watch();
    void refreshGit();
    void review.refreshOpen();
  });
  // Leaving the window (or closing it) writes what is unsaved.
  window.addEventListener("blur", () => void autosave());
  window.addEventListener("pagehide", () => {
    if (dirty && currentPath && !conflict) {
      void fetch("api/note", {
        method: "PUT",
        keepalive: true,
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ path: currentPath, text: docText(editor), base: version }),
      });
    }
  });
  window.addEventListener("error", (e) => void api.log.add(`${e.message} @ ${e.filename}:${e.lineno}`));
  window.addEventListener("unhandledrejection", (e) => void api.log.add("unhandled: " + msg(e.reason)));

  onPref((k) => {
    if (k === "wrap" || k === "guides" || k === "numbers" || k === "lang") applyEditorPrefs(editor);
    if (k === "size" || k === "font") editor.requestMeasure();
    paintSettings();
  });
  initTips();
}

async function load() {
  const info = await api.info();
  closeFile();
  if (!info.open) {
    opened = false;
    root = "";
    welcome.classList.add("open");
    void renderRecentNotebooks($<HTMLUListElement>("welcome-recent"), () => undefined);
    nbName.textContent = L("未选择笔记本", "ノートブック未選択");
    docs = [];
    renderFiles();
    showEmpty(false);
    return;
  }
  opened = true;
  welcome.classList.remove("open");
  root = info.root ?? "";
  nbName.textContent = root.replace(/[\\/]+$/, "").split(/[\\/]/).pop() || root;
  $("pill").title = root;
  session = await api.session.get().catch(() => ({ file: "", cursors: {} }));
  await refreshFiles();
  await refreshGit();
  const first = docs.find((d) => d.rel === session.file) ?? docs.find((d) => /^readme\.md$/i.test(d.rel)) ?? docs[0];
  if (first) await openFile(first.rel);
  else renderOutline();
}

function insertAtCursor(text: string) {
  if (rendered) setRendered(false);
  const at = editor.state.selection.main.head;
  editor.dispatch({ changes: { from: at, insert: text }, selection: { anchor: at + text.length } });
  editor.focus();
}

async function init() {
  wire();
  review.initReview({
    current: () => currentPath,
    open: (path, line) => openFile(path).then(() => void (line && gotoLine(line))),
    reload: reloadCurrent,
    refresh: async () => {
      await refreshFiles();
      await refreshGit();
      await renderRecent();
    },
    leave: leaveNote,
    toast,
    focus: () => {
      if (!rendered) editor.focus();
    },
  });
  onLang(applyLang);
  paintSettings();
  try {
    await load();
  } catch (e) {
    toast(L("启动失败：", "起動できません：") + msg(e), true);
  }
  if (new URLSearchParams(location.search).has("selftest")) {
    await runSelftest({
      openReview: (path) => review.openReview(path),
      openHistory: (path) => review.openHistory(path),
      closeReview: () => review.close(),
      refresh: async () => {
        await refreshFiles();
        await refreshGit();
      },
      editor,
      version: window.jusVersion ?? "",
      notes: () => docs.map((d) => d.rel),
      open: async (path) => {
        await refreshFiles();
        await openFile(path);
      },
      current: () => currentPath,
      saveState: () => saveState,
      flush: async () => {
        window.clearTimeout(autosaveTimer);
        await autosave();
        if (saving) await saving;
      },
      setRendered,
      preview,
      dialogButtons: () => [...document.querySelectorAll<HTMLButtonElement>("#dialog.open .dialog-actions button")],
      commands: commandList,
    });
  }
}

void init();
