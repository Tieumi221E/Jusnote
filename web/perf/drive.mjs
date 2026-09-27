// Drives a running Chromium (the WebView2 runtime's msedge.exe, started
// with --remote-debugging-port) through the DevTools protocol, for trying
// the editor by hand from a terminal: real mouse and key events through the
// browser's input pipeline, and exact screenshots. Local development only.
//
//   node web/perf/drive.mjs <port> <step> [<step> ...]
//
// Steps: nav=<url>  click=<x>,<y>  dbl=<x>,<y>  right=<x>,<y>  move=<x>,<y>
//        key=<Key>[+ctrl|+shift|+alt]  type=<text>  wait=<ms>
//        shot=<file.png>  eval=<js expression>
const [port, ...steps] = process.argv.slice(2);
const targets = await (await fetch(`http://127.0.0.1:${port}/json`)).json();
const page = targets.find((t) => t.type === "page");
const ws = new WebSocket(page.webSocketDebuggerUrl);
await new Promise((ok) => ws.addEventListener("open", ok, { once: true }));
let id = 0;
const waiting = new Map();
ws.addEventListener("message", (e) => {
  const m = JSON.parse(e.data);
  if (m.id && waiting.has(m.id)) {
    waiting.get(m.id)(m);
    waiting.delete(m.id);
  }
});
const send = (method, params = {}) =>
  new Promise((ok) => {
    const n = ++id;
    waiting.set(n, ok);
    ws.send(JSON.stringify({ id: n, method, params }));
  });
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

const KEYS = { Enter: 13, Escape: 27, Tab: 9, Backspace: 8, Delete: 46, ArrowDown: 40, ArrowUp: 38, ArrowLeft: 37, ArrowRight: 39, End: 35, Home: 36, F1: 112 };
async function mouse(x, y, button = "left", clickCount = 1) {
  await send("Input.dispatchMouseEvent", { type: "mouseMoved", x, y });
  await send("Input.dispatchMouseEvent", { type: "mousePressed", x, y, button, clickCount });
  await send("Input.dispatchMouseEvent", { type: "mouseReleased", x, y, button, clickCount });
}
async function key(spec) {
  const [k, ...mods] = spec.split("+");
  const modifiers = (mods.includes("alt") ? 1 : 0) | (mods.includes("ctrl") ? 2 : 0) | (mods.includes("shift") ? 8 : 0);
  const code = KEYS[k] ?? k.toUpperCase().charCodeAt(0);
  const text = k.length === 1 && !(modifiers & 2) ? k : undefined;
  const base = { key: k.length === 1 ? k : k, code: k.length === 1 ? "Key" + k.toUpperCase() : k, windowsVirtualKeyCode: code, modifiers };
  await send("Input.dispatchKeyEvent", { type: text ? "keyDown" : "rawKeyDown", ...base, text });
  await send("Input.dispatchKeyEvent", { type: "keyUp", ...base });
}

for (const step of steps) {
  const at = step.indexOf("=");
  const op = step.slice(0, at);
  const arg = step.slice(at + 1);
  const xy = () => arg.split(",").map(Number);
  if (op === "nav") await send("Page.navigate", { url: arg }), await sleep(1200);
  else if (op === "click") await mouse(...xy());
  else if (op === "dbl") await mouse(...xy(), "left", 2);
  else if (op === "right") await mouse(...xy(), "right");
  else if (op === "move") await send("Input.dispatchMouseEvent", { type: "mouseMoved", x: xy()[0], y: xy()[1] });
  else if (op === "key") await key(arg);
  else if (op === "type") await send("Input.insertText", { text: arg });
  else if (op === "wait") await sleep(Number(arg));
  else if (op === "shot") {
    await sleep(350);
    const r = await send("Page.captureScreenshot", { format: "png" });
    (await import("node:fs")).writeFileSync(arg, Buffer.from(r.result.data, "base64"));
    console.log("shot", arg);
  } else if (op === "eval") {
    const r = await send("Runtime.evaluate", { expression: arg, awaitPromise: true, returnByValue: true });
    console.log(JSON.stringify(r.result?.result?.value ?? r.result));
  }
  if (op !== "wait" && op !== "shot" && op !== "eval") await sleep(250);
}
ws.close();
