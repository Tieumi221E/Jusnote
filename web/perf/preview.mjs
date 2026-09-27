// Where the preview's time goes for a large note, outside the browser:
// marked alone, and marked with the same kinds of extensions the page uses.
// DOM work (sanitising, layout) is measured by the page selftest.
//
//   node web/perf/preview.mjs
import { marked } from "marked";

const para = "## 段落\n\n这是一段用来测量的文字，含 **粗体**、`代码` 和 [[scratch]] 链接。Some English words too.\n\n- [ ] 任务\n\n";
const text = "# Big\n\n" + para.repeat(Math.ceil((1024 * 1024) / para.length));
const time = (label, fn) => {
  const xs = [];
  for (let i = 0; i < 4; i++) {
    const s = performance.now();
    fn();
    xs.push(performance.now() - s);
  }
  xs.shift(); // warm-up
  xs.sort((a, b) => a - b);
  console.log(`${label.padEnd(24)} p50 ${Math.round(xs[1])} ms  max ${Math.round(xs[2])} ms`);
};

console.log(`${(text.length / 1024).toFixed(0)} K characters, node ${process.version}`);
time("marked", () => marked.parse(text));
const wiki = { name: "wiki", level: "inline", start: (s) => s.indexOf("[["), tokenizer(s) { const m = /^\[\[([^[\]|#\n]+)\]\]/.exec(s); return m ? { type: "wiki", raw: m[0], t: m[1] } : undefined; }, renderer: (x) => `<a>${x.t}</a>` };
const mi = { name: "mi", level: "inline", start: (s) => s.indexOf("$"), tokenizer(s) { const m = /^\$(?![\s$])((?:\\.|[^\\$\n])+?)(?<!\s)\$(?!\d)/.exec(s); return m ? { type: "mi", raw: m[0], text: m[1] } : undefined; }, renderer: (x) => x.text };
const mb = { name: "mb", level: "block", tokenizer(s) { const m = /^\$\$[ \t]*\n?([\s\S]+?)\n?[ \t]*\$\$[ \t]*(?:\n|$)/.exec(s); return m ? { type: "mb", raw: m[0], text: m[1] } : undefined; }, renderer: (x) => x.text };
marked.use({ extensions: [mb, mi, wiki] });
time("marked + extensions", () => marked.parse(text));
console.log(`HTML ${(marked.parse(text).length / 1024).toFixed(0)} K`);
