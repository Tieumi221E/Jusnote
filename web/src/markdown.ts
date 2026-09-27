// The preview: Markdown (GFM) with wiki links, math and live task boxes.
// marked parses, KaTeX typesets, DOMPurify cleans; nothing in a note can
// run script or navigate the page.

import { marked, type TokenizerAndRendererExtension } from "marked";
import DOMPurify from "dompurify";
import katex from "katex";

const wikiExt: TokenizerAndRendererExtension = {
  name: "wiki",
  level: "inline",
  start: (src) => src.indexOf("[["),
  tokenizer(src) {
    const m = /^\[\[([^[\]|#\n]+)(#[^[\]|\n]*)?(?:\|([^[\]\n]*))?\]\]/.exec(src);
    if (!m) return undefined;
    return { type: "wiki", raw: m[0], target: m[1].trim(), heading: (m[2] ?? "").slice(1), label: (m[3] ?? "").trim() };
  },
  renderer(t) {
    const label = t.label || t.target + (t.heading ? " › " + t.heading : "");
    const a = document.createElement("a");
    a.className = "wikilink";
    a.href = "#";
    a.dataset.wiki = t.target;
    if (t.heading) a.dataset.heading = t.heading;
    a.textContent = label;
    return a.outerHTML;
  },
};

function tex(src: string, display: boolean): string {
  try {
    return katex.renderToString(src, { displayMode: display, throwOnError: false, output: "htmlAndMathml" });
  } catch {
    const c = document.createElement("code");
    c.textContent = src;
    return c.outerHTML;
  }
}

// No start(): marked calls a block extension's start() on the whole rest of
// the document at every paragraph, and indexOf("$$") there is O(n²) — 2.8 s
// for a 1 MB note in the selftest. Without it, $$ opens a block where a
// block may begin (after a blank line), which is how math is written anyway.
const mathBlock: TokenizerAndRendererExtension = {
  name: "mathBlock",
  level: "block",
  tokenizer(src) {
    const m = /^\$\$[ \t]*\n?([\s\S]+?)\n?[ \t]*\$\$[ \t]*(?:\n|$)/.exec(src);
    return m ? { type: "mathBlock", raw: m[0], text: m[1] } : undefined;
  },
  renderer: (t) => '<div class="math-block">' + tex(t.text, true) + "</div>",
};

const mathInline: TokenizerAndRendererExtension = {
  name: "mathInline",
  level: "inline",
  start: (src) => src.indexOf("$"),
  tokenizer(src) {
    // $x$, not "$5 and $6": no space just inside the dollars, no digit after.
    const m = /^\$(?![\s$])((?:\\.|[^\\$\n])+?)(?<!\s)\$(?!\d)/.exec(src);
    return m ? { type: "mathInline", raw: m[0], text: m[1] } : undefined;
  },
  renderer: (t) => tex(t.text, false),
};

let taskIndex = 0;
marked.use({
  gfm: true,
  extensions: [mathBlock, mathInline, wikiExt],
  renderer: {
    checkbox({ checked }) {
      return `<input type="checkbox" data-task="${taskIndex++}"${checked ? " checked" : ""}>`;
    },
  },
});

/**
 * Renders note text, sanitised, into el. dir is the note's folder, so a
 * relative image resolves through the notebook's raw endpoint. The HTML is
 * parsed once: DOMPurify hands back the fragment it already built (the
 * string round trip parsed a 1 MB note three times).
 */
export function renderInto(el: HTMLElement, text: string, dir: string): void {
  taskIndex = 0;
  const html = marked.parse(text) as string;
  const frag = DOMPurify.sanitize(html, {
    ADD_ATTR: ["data-wiki", "data-heading", "data-task"],
    ALLOWED_URI_REGEXP: /^(?:(?:https?|mailto|jus):|[^a-z]|[a-z+.-]+(?:[^a-z+.\-:]|$))/i,
    RETURN_DOM_FRAGMENT: true,
  });
  frag.querySelectorAll("img").forEach((img) => {
    const src = img.getAttribute("src") ?? "";
    if (src && !/^([a-z][a-z0-9+.-]*:|\/)/i.test(src)) {
      let rel = src;
      try {
        rel = decodeURIComponent(src);
      } catch {
        /* keep as written */
      }
      img.setAttribute("src", "api/raw?path=" + encodeURIComponent(dir ? dir + "/" + rel : rel));
      img.setAttribute("loading", "lazy");
    }
  });
  const md = document.createElement("div");
  md.className = "md";
  md.append(frag);
  el.replaceChildren(md);
}
