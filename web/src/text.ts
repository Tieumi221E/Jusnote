// Small pure helpers, kept apart from the DOM so node --test can check them.

/**
 * Counts a note the way a writer does: every CJK character (and kana,
 * hangul) is one, every run of other letters or digits is one word.
 * Markdown marks and punctuation do not count.
 */
export function wordCount(text: string): number {
  let n = 0;
  let inWord = false;
  for (const ch of text) {
    if (/[\p{Script=Han}\p{Script=Hiragana}\p{Script=Katakana}\p{Script=Hangul}]/u.test(ch)) {
      n++;
      inWord = false;
    } else if (/[\p{L}\p{N}_'’-]/u.test(ch)) {
      if (!inWord && /[\p{L}\p{N}]/u.test(ch)) {
        n++;
        inWord = true;
      }
    } else {
      inWord = false;
    }
  }
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
