"""Writes THIRD_PARTY_NOTICES.txt: the license of everything inside the
released jusnote.exe that Jusnote did not write. Run from the repository
root after changing a dependency (and after `npm --prefix web ci`):

  python tools/notices.py

What is inside is read, not listed by hand:

- Go: every module linked into ./cmd/jusnote (go list -deps), with its
  license from the module cache (go env GOMODCACHE); the Go runtime and
  standard library from the Go distribution (go env GOROOT); and the
  WebView2 loader that go-webview2 embeds.
- The editor page: every npm package esbuild actually bundles into app.js
  (its metafile), with the license file from web/node_modules.
- The fonts (web/src/fonts/LICENSE.txt, OFL-1.1, renamed subsets).
"""
import json, os, subprocess, sys

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
LICENSE_NAMES = ["LICENSE", "LICENSE.md", "LICENSE.txt", "LICENCE", "COPYING", "LICENSE-MIT", "license", "license.md"]


def go(*args):
    return subprocess.check_output(["go", *args], text=True, cwd=ROOT).strip()


def read_license(d):
    for n in LICENSE_NAMES:
        f = os.path.join(d, n)
        if os.path.isfile(f):
            return open(f, encoding="utf-8", errors="replace").read().strip()
    # Several licenses (e.g. LICENSE.BSD + LICENSE.MPL-2.0 + COPYING.md): all of them.
    many = sorted(n for n in os.listdir(d) if n.upper().startswith(("LICENSE", "COPYING")) and os.path.isfile(os.path.join(d, n))) if os.path.isdir(d) else []
    if many:
        return "\n\n".join(f"[{n}]\n\n" + open(os.path.join(d, n), encoding="utf-8", errors="replace").read().strip() for n in many)
    return None


goroot = go("env", "GOROOT")
cache = go("env", "GOMODCACHE")
mods = {}
for line in go("list", "-deps", "-f", "{{if .Module}}{{.Module.Path}} {{.Module.Version}}{{end}}", "./cmd/jusnote").splitlines():
    p = line.split()
    if len(p) == 2 and not p[0].startswith("github.com/Tieumi221E/Jusnote"):
        mods[p[0]] = p[1]


def escape(path):
    # The module cache spells capitals as "!" + lowercase.
    return "".join("!" + c.lower() if c.isupper() else c for c in path)


parts = [("Go runtime and standard library", "https://go.dev  (BSD-3-Clause)", read_license(goroot))]
missing = []
for path in sorted(mods):
    d = os.path.join(cache, f"{escape(path)}@{mods[path]}")
    text = read_license(d)
    if text is None:
        missing.append(path)
        continue
    parts.append((f"{path} {mods[path]}", "https://" + path, text))
if "github.com/jchv/go-webview2" in mods:
    d = os.path.join(cache, f"github.com/jchv/go-webview2@{mods['github.com/jchv/go-webview2']}", "webviewloader", "sdk")
    parts.append(("Microsoft WebView2Loader.dll (embedded by go-webview2)", "https://aka.ms/webview2", read_license(d)))

# The npm packages in the bundle, from esbuild's metafile.
script = """
import { build } from "esbuild";
const r = await build({ entryPoints: ["src/main.ts"], bundle: true, write: false, metafile: true, format: "esm", logLevel: "silent" });
console.log(JSON.stringify(Object.keys(r.metafile.inputs)));
"""
inputs = json.loads(subprocess.check_output(["node", "--input-type=module", "-e", script], text=True, cwd=os.path.join(ROOT, "web")))
pkgs = set()
for i in inputs:
    i = i.replace("\\", "/")
    if "node_modules/" not in i:
        continue
    rest = i.split("node_modules/")[-1].split("/")
    pkgs.add("/".join(rest[:2]) if rest[0].startswith("@") else rest[0])
for name in sorted(pkgs):
    d = os.path.join(ROOT, "web", "node_modules", *name.split("/"))
    meta = json.load(open(os.path.join(d, "package.json"), encoding="utf-8"))
    text = read_license(d)
    if text is None:
        missing.append(name)
        continue
    url = (meta.get("homepage") or f"https://www.npmjs.com/package/{name}").split("#")[0]
    parts.append((f"{name} {meta['version']} (in the editor page)", f"{url}  ({meta.get('license', '')})", text))

parts.append(("Fonts: Jus Sans (Source Han Sans 2.005), subsets, renamed", "https://github.com/adobe-fonts/source-han-sans  (OFL-1.1)",
              open(os.path.join(ROOT, "web", "src", "fonts", "LICENSE.txt"), encoding="utf-8").read().strip()))

if missing:
    sys.exit("no license file found for: " + ", ".join(missing))

out = ["Third-party software in Jusnote",
       "===============================",
       "",
       "Jusnote itself is under the MIT License (LICENSE). jusnote.exe also contains",
       "the following, each under its own license, reproduced below.",
       ""]
for name, url, text in parts:
    out += ["-" * 78, name, url, "-" * 78, "", text, ""]
with open(os.path.join(ROOT, "THIRD_PARTY_NOTICES.txt"), "w", encoding="utf-8", newline="\n") as f:
    f.write("\n".join(out))
print(f"THIRD_PARTY_NOTICES.txt: {len(parts)} components")
