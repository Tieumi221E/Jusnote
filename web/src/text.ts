// Small pure helpers, kept apart from the DOM so node --test can check them.

/**
 * Counts a note the way a writer does: every CJK character (and kana,
 * hangul) is one, every run of other letters or digits is one word.
 * Markdown marks and punctuation do not count.
 */
// Two global passes instead of a regex test per character: ~20x faster on
// a 1 MB note (the per-character version made opening one take ~150 ms).
const CJK = /[\p{Script=Han}\p{Script=Hiragana}\p{Script=Katakana}\p{Script=Hangul}]/gu;
const WORD = /[\p{L}\p{N}][\p{L}\p{N}_'’-]*/gu;
export function wordCount(text: string): number {
  let n = 0;
  const rest = text.replace(CJK, () => {
    n++;
    return " ";
  });
  for (const _ of rest.matchAll(WORD)) n++;
  return n;
}

/**
 * Fuzzy match of query against a path: every query character must appear
 * in order. Higher is better; -1 is no match. Consecutive runs, matches at
 * the start of a name segment, and matches in the file name score more.
 */
export function fuzzyScore(query: string, target: string): number {
  const q = query.toLowerCase().replace(/\s+/g, "");
  if (!q) return 0;
  const t = target.toLowerCase();
  const nameAt = t.lastIndexOf("/") + 1;
  let score = 0;
  let ti = 0;
  let run = 0;
  for (const ch of q) {
    const at = t.indexOf(ch, ti);
    if (at < 0) return -1;
    run = at === ti ? run + 1 : 1;
    score += run * 2;
    if (at === 0 || "/-_. ".includes(t[at - 1])) score += 5;
    if (at >= nameAt) score += 1;
    ti = at + 1;
  }
  return score - t.length * 0.01;
}

export type TreeNode = { name: string; path: string; dir: boolean; children: TreeNode[] };

/** Builds a folder tree from slash-separated note paths; folders first, then by name. */
export function buildTree(paths: string[]): TreeNode[] {
  const root: TreeNode = { name: "", path: "", dir: true, children: [] };
  for (const p of paths) {
    const parts = p.split("/");
    let node = root;
    parts.forEach((part, i) => {
      const leaf = i === parts.length - 1;
      const path = parts.slice(0, i + 1).join("/");
      let next = node.children.find((c) => c.name === part && c.dir === !leaf);
      if (!next) {
        next = { name: part, path, dir: !leaf, children: [] };
        node.children.push(next);
      }
      node = next;
    });
  }
  const sort = (n: TreeNode) => {
    n.children.sort((a, b) => (a.dir !== b.dir ? (a.dir ? -1 : 1) : a.name.localeCompare(b.name, undefined, { numeric: true })));
    n.children.forEach(sort);
  };
  sort(root);
  return root.children;
}

/** A note path from what the user typed: slashes normalised, ".md" added when there is no extension. */
export function notePath(input: string): string {
  let p = input.trim().replace(/\\/g, "/").replace(/^\/+/, "").replace(/\/{2,}/g, "/");
  if (p && !/\.(md|markdown)$/i.test(p)) p += ".md";
  return p;
}

// Task list items as marked counts them: a list marker, then [ ] or [x],
// possibly inside block quotes; not inside fenced code.
const TASK_RE = /^((?:[ \t]*>)*[ \t]*(?:[-*+]|\d+[.)])[ \t]+)\[( |x|X)\]/;

/** Flips the index-th task box of a note (in document order); null if there is none. */
export function toggleTask(text: string, index: number): string | null {
  const lines = text.split("\n");
  let n = 0;
  let fence = "";
  for (let i = 0; i < lines.length; i++) {
    const t = lines[i].trim();
    if (fence) {
      if (t.startsWith(fence)) fence = "";
      continue;
    }
    if (t.startsWith("```") || t.startsWith("~~~")) {
      fence = t.slice(0, 3);
      continue;
    }
    const m = TASK_RE.exec(lines[i]);
    if (!m) continue;
    if (n++ === index) {
      const mark = m[2] === " " ? "x" : " ";
      lines[i] = m[1] + "[" + mark + "]" + lines[i].slice(m[0].length);
      return lines.join("\n");
    }
  }
  return null;
}

/** The [[name]] being typed just before pos in line text, if the cursor is inside an open wiki link. */
export function openWikiLink(before: string): string | null {
  const at = before.lastIndexOf("[[");
  if (at < 0) return null;
  const inside = before.slice(at + 2);
  if (/[\]|#\n]/.test(inside)) return null;
  return inside;
}

/**
 * The part of a changed line that actually differs from its old version:
 * [start, endOld, endNew) in code units, after the common prefix and before
 * the common suffix. Used to highlight "测试夹具 → 测试用的夹具" inside a line.
 */
export function changedSpan(a: string, b: string): [number, number, number] {
  let s = 0;
  while (s < a.length && s < b.length && a[s] === b[s]) s++;
  let ea = a.length;
  let eb = b.length;
  while (ea > s && eb > s && a[ea - 1] === b[eb - 1]) {
    ea--;
    eb--;
  }
  return [s, ea, eb];
}
