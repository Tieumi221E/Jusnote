// Bundles the editor page into internal/ui/dist, which the Go binary
// embeds.
import { build } from "esbuild";
import { copyFileSync, cpSync, mkdirSync, readdirSync, rmSync } from "node:fs";

const out = new URL("../internal/ui/dist/", import.meta.url);
mkdirSync(out, { recursive: true });
for (const f of readdirSync(out)) if (f !== ".gitkeep") rmSync(new URL(f, out), { recursive: true });
const common = { bundle: true, format: "esm", target: "chrome120", minify: true, sourcemap: false, legalComments: "linked" };
const file = (name) => new URL(name, out).pathname.replace(/^\/([A-Za-z]:)/, "$1");
await build({ ...common, entryPoints: ["src/main.ts"], outfile: file("app.js") });
for (const f of ["index.html", "tokens.css", "editor.css", "icon-64.png", "icon-64-dark.png"]) {
  copyFileSync(new URL(`src/${f}`, import.meta.url), new URL(f, out));
}
cpSync(new URL("src/fonts/", import.meta.url), new URL("fonts/", out), { recursive: true });
