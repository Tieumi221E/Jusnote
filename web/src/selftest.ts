// The page selftest (`jusnote gui -selftest -notebook <copy>`), as in
// Jusplay: it drives the real editor in the real window, times what a
// person waits for, checks what must hold, and posts one report (speed and
// correctness in the same table) that the process prints and exits on.
//
// What it cannot do: real keyboard input (SendInput) — keystrokes are
// editor transactions, so "keystroke to paint" here is the editor's own
// update + layout + paint, without the OS input path. Say so in results.

import type { EditorView } from "@codemirror/view";
import { api } from "./api.ts";
import { prefs, setPref } from "./i18n.ts";

export type Hooks = {
  openReview: (path?: string) => Promise<void>;
  openHistory: (path: string) => Promise<void>;
  closeReview: () => void;
  refresh: () => Promise<void>;
  editor: EditorView;
  version: string;
  notes: () => string[];
  open: (path: string) => Promise<void>;
  current: () => string;
  saveState: () => string;
  flush: () => Promise<void>;
  setRendered: (on: boolean) => void;
  preview: HTMLElement;
  dialogButtons: () => HTMLButtonElement[];
};

type Stat = { n: number; p50: number; p95: number; max: number; unit: string };
type Check = { name: string; ok: boolean; detail?: string };

const frame = () => new Promise<number>((r) => requestAnimationFrame(r));
/** Resolves after the next paint (a frame, then another). */
const painted = async () => {
  await frame();
  return frame();
};
const wait = (ms: number) => new Promise((r) => setTimeout(r, ms));

/** Plays another program writing a note (selftest-only endpoint). */
async function writeOutside(path: string, text: string, source: Record<string, string>) {
  await fetch("api/selftest/outside", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ path, text, source }) });
}

// Latency budgets (p95, ms), in the spirit of Jusplay's: what a person
// should never wait longer for. Keystrokes are measured to the next paint
// (two animation frames), so 34 ms is "within two frames at 60 Hz". The
// 1 MB note is an extreme case with looser budgets; its preview has none
// (sanitising 2 MB of HTML is the floor, see the research log).
const BUDGET: Record<string, number> = {
  "open note": 100,
  "keystroke to paint": 34,
  "write 1 KB": 50,
  "write 64 KB": 80,
  "search \"段落\"": 100,
  "search \"the\"": 100,
  "search \"不存在的词\"": 100,
  "links + backlinks": 100,
  "language switch": 50,
  "theme switch": 50,
  "preview render (64 KB note)": 200,
  "open 1 MB note": 500,
  "keystroke to paint (1 MB note, at the end)": 50,
};

function stat(xs: number[], unit = "ms"): Stat {
  const s = [...xs].sort((a, b) => a - b);
  const q = (p: number) => (s.length ? s[Math.min(s.length - 1, Math.floor(p * (s.length - 1) + 0.5))] : NaN);
  const r = (v: number) => Math.round(v * 100) / 100;
  return { n: s.length, p50: r(q(0.5)), p95: r(q(0.95)), max: r(s[s.length - 1] ?? NaN), unit };
}

async function timeIt(n: number, fn: (i: number) => Promise<unknown>): Promise<number[]> {
  const out: number[] = [];
  for (let i = 0; i < n; i++) {
    const t = performance.now();
    await fn(i);
    out.push(performance.now() - t);
  }
  return out;
}

async function until(cond: () => boolean, ms = 5000): Promise<boolean> {
  const end = performance.now() + ms;
  while (performance.now() < end) {
    if (cond()) return true;
    await wait(20);
  }
  return cond();
}

export async function runSelftest(h: Hooks): Promise<void> {
  const t0 = performance.now();
  const timings: Record<string, Stat> = {};
  const checks: Check[] = [];
  const errors: string[] = [];
  const check = (name: string, ok: boolean, detail?: string) => checks.push({ name, ok, detail });
  const onErr = (e: ErrorEvent) => errors.push(e.message);
  const onRej = (e: PromiseRejectionEvent) => errors.push(String(e.reason));
  window.addEventListener("error", onErr);
  window.addEventListener("unhandledrejection", onRej);
  const ed = h.editor;

  try {
    const notes = h.notes();
    check("notebook has notes", notes.length > 0, `${notes.length} notes`);

    // Opening / switching notes (read + fresh editor state + gutter + paint).
    const sample = notes.slice(0, 30);
    timings["open note"] = stat((await timeIt(sample.length, async (i) => {
      await h.open(sample[i]);
      await painted();
    })).slice(1)); // the first open warms caches: left out

    // A scratch note for the edits.
    const scratch = "selftest/scratch.md";
    await api.write(scratch, "# Scratch\n\n", null);
    await h.open(scratch);
    await painted();

    // Keystroke to paint: one character per transaction, like typing.
    const end = () => ed.state.doc.length;
    timings["keystroke to paint"] = stat(await timeIt(200, async (i) => {
      ed.dispatch({ changes: { from: end(), insert: i % 40 === 39 ? "\n" : "字" }, userEvent: "input.type" });
      await painted();
    }));

    // Autosave: the edit reaches the disk, and what is on disk is the buffer.
    const tSave = performance.now();
    const saved = await until(() => h.saveState() === "saved", 5000);
    timings["autosave round trip (incl. 700 ms idle)"] = stat([performance.now() - tSave]);
    const disk = await api.read(scratch);
    check("autosave writes exactly the buffer", saved && disk.text === ed.state.doc.toString(), `${disk.text.length} vs ${ed.state.doc.length} chars`);

    // The write itself, without the idle wait, by document size.
    for (const kb of [1, 64, 512]) {
      const text = "# Size\n\n" + ("一二三四五六七八九十 abcdefghij\n".repeat(Math.ceil((kb * 1024) / 42)));
      let base = (await api.write(`selftest/size-${kb}k.md`, text, null)).version;
      timings[`write ${kb} KB`] = stat(await timeIt(10, async (i) => {
        base = (await api.write(`selftest/size-${kb}k.md`, text + i, base)).version;
      }));
    }

    // Outside edit while typing: the save stops and asks; "load from disk" takes the other side.
    await h.flush();
    const outside = "# Changed elsewhere\n";
    await api.write(scratch, outside, null);
    ed.dispatch({ changes: { from: end(), insert: "mine" }, userEvent: "input.type" });
    const asked = await until(() => h.dialogButtons().length === 3, 4000);
    check("an outside edit during typing is detected (409 + dialog)", asked, h.saveState());
    if (asked) {
      h.dialogButtons()[2].click();
      await until(() => ed.state.doc.toString() === outside, 3000);
    }
    check("'load from disk' shows the outside version", ed.state.doc.toString() === outside);

    // Undo never crosses notes.
    const other = notes.find((n) => n !== scratch) ?? scratch;
    await h.open(other);
    const before = ed.state.doc.toString();
    const { undo } = await import("@codemirror/commands");
    undo(ed);
    check("undo after switching notes does not bring the last note in", ed.state.doc.toString() === before);

    // Task boxes in the preview write back.
    const tasks = "selftest/tasks.md";
    await api.write(tasks, "- [ ] one\n- [ ] two\n\n$$E = mc^2$$\n\n[[scratch]]\n", null);
    await h.open(tasks);
    h.setRendered(true);
    await painted();
    const boxes = h.preview.querySelectorAll<HTMLInputElement>("input[data-task]");
    boxes[1]?.click();
    await painted();
    check("preview task box writes back to the note", ed.state.doc.toString().startsWith("- [ ] one\n- [x] two"));
    check("math renders", !!h.preview.querySelector(".katex"), "");
    check("wiki link renders as a link", !!h.preview.querySelector("a.wikilink[data-wiki=scratch]"));

    // Review in the drawer: two outside edits (one an agent's), accept one, discard the other.
    const btn = (sel: string, text: string) =>
      [...document.querySelectorAll<HTMLButtonElement>(sel)].find((b) => b.textContent?.trim() === text);
    const clickDialog = async (label: string) => {
      await until(() => !!btn("#dialog.open .dialog-actions button", label), 3000);
      btn("#dialog.open .dialog-actions button", label)?.click();
    };
    const agentSrc = { author: "agent", model: "selftest/model", run: "st-1" };
    const keep = "selftest/review-keep.md";
    const drop = "selftest/review-drop.md";
    await api.write(keep, "# Keep\n", null);
    await api.write(drop, "# Drop\n", null);
    await api.commit([keep, drop]);
    await writeOutside(keep, "# Keep\n\nadded by an agent\n", agentSrc);
    await writeOutside(drop, "# Drop\n\nunwanted\n", {});
    await h.openReview(keep);
    await until(() => document.querySelectorAll("#review .rv-list li").length >= 2, 3000);
    const rows = [...document.querySelectorAll("#review .rv-list li")].map((l) => (l as HTMLElement).innerText);
    check("review lists both outside edits, the agent's with its name", rows.some((r) => r.includes(keep) && r.includes("selftest/model")) && rows.some((r) => r.includes(drop)), rows.join(" / "));
    btn("#review .rv-actions button", "接受")?.click() ?? btn("#review .rv-actions button", "承認")?.click();
    await wait(600);
    const selRow = [...document.querySelectorAll<HTMLElement>("#review .rv-list li")].find((l) => l.innerText.includes(drop));
    selRow?.click();
    await wait(300);
    (btn("#review .rv-actions button", "丢弃") ?? btn("#review .rv-actions button", "破棄"))?.click();
    await clickDialog(prefs.lang === "ja" ? "破棄" : "丢弃");
    await wait(800);
    const keepLog = await api.fileLog(keep, 1);
    const dropNow = await api.read(drop);
    const left = (await api.changes()).filter((c) => c.path === keep || c.path === drop);
    check("accept commits the agent's edit with its provenance", keepLog[0]?.source?.author === "agent" && keepLog[0]?.source?.model === "selftest/model", JSON.stringify(keepLog[0]?.source));
    check("discard puts the file back as committed", dropNow.text === "# Drop\n" && left.length === 0, dropNow.text.replace(/\n/g, "\\n"));
    h.closeReview();

    // Clicking outside closes the drawer; its status-bar entry toggles it.
    await h.openReview();
    await wait(200);
    ed.contentDOM.dispatchEvent(new MouseEvent("mousedown", { bubbles: true }));
    await wait(200);
    const closedByOutside = !document.getElementById("review")!.classList.contains("open");
    document.getElementById("st-git")?.click();
    await wait(300);
    const openedByEntry = document.getElementById("review")!.classList.contains("open");
    document.getElementById("st-git")?.click();
    await wait(300);
    check("the review drawer closes on an outside click and toggles from its entry", closedByOutside && openedByEntry && !document.getElementById("review")!.classList.contains("open"));

    // A file saved with CRLF where it had LF: no markers, and the diff says so.
    const eol = "selftest/eol.md";
    await api.write(eol, "one\ntwo\n", null);
    await api.commit([eol]);
    await writeOutside(eol, "one\r\ntwo\r\n", {});
    const eolDiff = await api.diff(eol);
    const eolGutter = await api.gutter(eol);
    check("a line-ending-only change shows as that, not as every line changed", eolDiff.eolOnly && eolDiff.hunks.length === 0 && eolGutter.length === 0, JSON.stringify({ eol: eolDiff.eolOnly, hunks: eolDiff.hunks.length, marks: eolGutter.length }));

    // Back and forward through the notes opened.
    await h.open(keep);
    await h.open(drop);
    window.dispatchEvent(new KeyboardEvent("keydown", { key: "ArrowLeft", altKey: true }));
    await until(() => h.current() === keep, 2000);
    const wentBack = h.current() === keep;
    window.dispatchEvent(new KeyboardEvent("keydown", { key: "ArrowRight", altKey: true }));
    await until(() => h.current() === drop, 2000);
    check("Alt+← / Alt+→ go back and forward between notes", wentBack && h.current() === drop, h.current());

    // Restore from the history drawer.
    const hist = "selftest/history.md";
    await api.write(hist, "one\n", null);
    await api.commit([hist]);
    await api.write(hist, "two\n", null);
    await api.commit([hist]);
    await h.open(hist);
    await h.openHistory(hist);
    await until(() => document.querySelectorAll("#review .rv-list li").length >= 2, 3000);
    (btn("#review .rv-actions button", "恢复这一版") ?? btn("#review .rv-actions button", "この版に戻す"))?.click();
    await clickDialog(prefs.lang === "ja" ? "戻す" : "恢复");
    await until(() => ed.state.doc.toString() === "one\n", 3000);
    const histLog = await api.fileLog(hist, 1);
    check("restore brings the old version into the editor and commits it", ed.state.doc.toString() === "one\n" && /^restore /.test(histLog[0]?.message ?? ""), histLog[0]?.message);
    h.closeReview();

    // Pasting an image stores it next to the note and inserts the link; it is committed with the note.
    const paste = "selftest/paste.md";
    await api.write(paste, "# Paste\n\n", null);
    await h.open(paste);
    ed.dispatch({ selection: { anchor: ed.state.doc.length } });
    const dt = new DataTransfer();
    dt.items.add(new File([new Uint8Array([0x89, 0x50, 0x4e, 0x47])], "image.png", { type: "image/png" }));
    ed.contentDOM.dispatchEvent(new ClipboardEvent("paste", { clipboardData: dt, bubbles: true, cancelable: true }));
    await until(() => /!\[image-\d{8}-\d{6}\]\(attachments\/image-\d{8}-\d{6}\.png\)/.test(ed.state.doc.toString()), 4000);
    const link = /attachments\/image-\d{8}-\d{6}\.png/.exec(ed.state.doc.toString())?.[0] ?? "";
    await h.flush();
    await h.open(scratch); // leaving the note commits what the editor wrote
    await wait(400);
    const pending = (await api.changes()).map((c) => c.path);
    check("a pasted image lands in attachments/ and is committed with the note", !!link && !pending.includes("selftest/" + link) && !pending.includes(paste), link);

    // Rename through the file menu, with "update links" ticked.
    const target = "selftest/rename-me.md";
    const linker = "selftest/links-to-it.md";
    await api.write(target, "# Target\n", null);
    await api.write(linker, "see [[rename-me]]\n", null);
    await h.refresh();
    const li = [...document.querySelectorAll<HTMLElement>("#files li.file")].find((l) => l.title === target);
    li?.dispatchEvent(new MouseEvent("contextmenu", { bubbles: true, clientX: 40, clientY: 40 }));
    await wait(100);
    document.querySelectorAll<HTMLButtonElement>("#menu button")[1]?.click();
    await until(() => !!document.querySelector("#dialog.open input:not([type=checkbox])"), 2000);
    const input = document.querySelector<HTMLInputElement>("#dialog.open input:not([type=checkbox])")!;
    input.value = "selftest/renamed";
    const ticked = document.querySelector<HTMLInputElement>("#dialog.open .dialog-check input")?.checked;
    await clickDialog(prefs.lang === "ja" ? "変更" : "重命名");
    await wait(1200);
    const linkerNow = await api.read(linker);
    check("rename from the file menu updates the links to the note", !!ticked && linkerNow.text === "see [[renamed]]\n", linkerNow.text.trim());

    // Preview rendering, a large note.
    const big = "selftest/big.md";
    const para = "## 段落\n\n这是一段用来测量的文字，含 **粗体**、`代码` 和 [[scratch]] 链接。Some English words too.\n\n- [ ] 任务\n\n";
    const bigText = "# Big\n\n" + para.repeat(Math.ceil((1024 * 1024) / para.length));
    await api.write(big, bigText, null);
    h.setRendered(false);
    timings["open 1 MB note"] = stat(await timeIt(3, async () => {
      await h.open(scratch);
      await painted();
      await h.open(big);
      await painted();
    }));
    ed.dispatch({ selection: { anchor: ed.state.doc.length }, scrollIntoView: true });
    await painted();
    timings["keystroke to paint (1 MB note, at the end)"] = stat(await timeIt(60, async () => {
      ed.dispatch({ changes: { from: ed.state.doc.length, insert: "字" }, userEvent: "input.type" });
      await painted();
    }));
    // Scrolling the whole note: frame times.
    const frames: number[] = [];
    const sc = ed.scrollDOM;
    sc.scrollTop = 0;
    await painted();
    let last = performance.now();
    for (let y = 0; y < sc.scrollHeight; y += sc.clientHeight / 2) {
      sc.scrollTop = y;
      await frame();
      const now = performance.now();
      frames.push(now - last);
      last = now;
      if (frames.length > 400) break;
    }
    timings["scroll frame (1 MB note)"] = stat(frames);
    await h.flush();
    const mid = "selftest/mid.md";
    await api.write(mid, "# Mid\n\n" + para.repeat(Math.ceil((64 * 1024) / para.length)), null);
    await h.open(mid);
    timings["preview render (64 KB note)"] = stat(await timeIt(5, async () => {
      h.setRendered(true);
      await painted();
      h.setRendered(false);
    }));
    await h.open(big);
    timings["preview render (1 MB note)"] = stat(await timeIt(3, async () => {
      h.setRendered(true);
      await painted();
      h.setRendered(false);
    }));

    // Search and links, over the whole notebook.
    for (const q of ["段落", "the", "不存在的词"]) {
      timings[`search "${q}"`] = stat((await timeIt(11, () => api.search(q))).slice(1)); // the first reads changed notes into the cache
    }
    timings["links + backlinks"] = stat(await timeIt(10, () => api.links(scratch)));

    // Switching language and theme re-renders in place (Jusplay's budget:
    // 50 ms), with an ordinary note open and with the 1 MB one.
    for (const [label, path] of [["", scratch], [" (1 MB note open)", big]] as const) {
      await h.open(path);
      await painted();
      const lang = prefs.lang;
      timings["language switch" + label] = stat(await timeIt(6, async () => {
        setPref("lang", prefs.lang === "zh" ? "ja" : "zh");
        await painted();
      }));
      if (prefs.lang !== lang) setPref("lang", lang);
      const theme = prefs.theme;
      timings["theme switch" + label] = stat(await timeIt(6, async () => {
        setPref("theme", prefs.theme === "dark" ? "light" : "dark");
        await painted();
      }));
      if (prefs.theme !== theme) setPref("theme", theme);
    }
    await h.flush();

    // Leave one edit written but not committed: the process commits it when
    // the window closes (tools/bench.ps1 checks the notebook afterwards).
    await h.open(scratch);
    ed.dispatch({ changes: { from: ed.state.doc.length, insert: "\nclosing edit\n" }, userEvent: "input.type" });
    await h.flush();
  } catch (e) {
    errors.push("selftest stopped: " + (e instanceof Error ? e.stack ?? e.message : String(e)));
  }

  for (const [name, limit] of Object.entries(BUDGET)) {
    const t = timings[name];
    if (t) check(`budget: ${name} p95 ≤ ${limit} ms`, t.p95 <= limit, `p95 ${t.p95} ms`);
  }
  const mem = (performance as unknown as { memory?: { usedJSHeapSize: number; totalJSHeapSize: number } }).memory;
  const report = {
    app: "jusnote",
    version: h.version,
    when: new Date().toISOString(),
    env: {
      ua: navigator.userAgent,
      dpr: devicePixelRatio,
      screen: `${screen.width}x${screen.height}`,
      viewport: `${innerWidth}x${innerHeight}`,
      cores: navigator.hardwareConcurrency,
      deviceMemoryGB: (navigator as unknown as { deviceMemory?: number }).deviceMemory,
      jsHeapMB: mem ? Math.round(mem.usedJSHeapSize / 1048576) : undefined,
      jsHeapTotalMB: mem ? Math.round(mem.totalJSHeapSize / 1048576) : undefined,
      notes: h.notes().length,
    },
    note: "keystrokes are editor transactions, not OS input; the first note open and the first search per query are left out as warm-up",
    ok: checks.every((c) => c.ok) && errors.length === 0,
    checks,
    timings,
    errors,
    totalMs: Math.round(performance.now() - t0),
  };
  window.removeEventListener("error", onErr);
  window.removeEventListener("unhandledrejection", onRej);
  await api.selftest(report);
}
