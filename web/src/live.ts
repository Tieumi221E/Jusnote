// The window's side of the live session (internal/server/live.go, Jus
// contract 18). The page listens to events.watch and carries out the
// commands that come from the command line or another app, and reports
// what it shows to api/state, so `jusnote editor status` and
// `jusnote events watch` see what the person sees.

export interface LiveState {
  page: "welcome" | "editor";
  notebook?: string;
  path?: string;
  line?: number;
  dirty?: boolean;
}

export interface Command {
  id: string;
  cmd: "open" | "notebook" | "pref";
  path?: string;
  folder?: string;
  key?: string;
  value?: unknown;
  line?: number;
}

/** One line of events.watch. */
export interface LiveEvent {
  kind: "hello" | "state" | "changed" | "command" | "tick";
  source?: { via?: string; harness?: string; run?: string };
  data?: Record<string, unknown>;
}

let current: () => LiveState = () => ({ page: "welcome" });
let timer = 0;

function post(body: { state: LiveState; ack?: string; result?: unknown; error?: string }): Promise<void> {
  return fetch("api/state", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) })
    .then(() => undefined, () => undefined);
}

/** What this page shows now, from f; reported at once. */
export function setState(f: () => LiveState): void {
  current = f;
  report();
}

/** The state changed: report it (a burst of changes is reported once). */
export function report(): void {
  clearTimeout(timer);
  timer = window.setTimeout(() => void post({ state: current() }), 120);
}

/** The state now, as reported. */
export function state(): LiveState {
  return current();
}

/**
 * Listen for commands (the top page only). run carries one out; when it
 * returns, what it returned (else the state then) goes back with the
 * command's id: that is the caller's answer.
 */
export function listen(run: (c: Command) => Promise<unknown>, onEvent?: (ev: LiveEvent) => void): void {
  const handle = (line: string) => {
    let ev: LiveEvent;
    try {
      ev = JSON.parse(line);
    } catch {
      return;
    }
    if (ev.kind !== "command" || !ev.data) {
      onEvent?.(ev);
      return;
    }
    const c = ev.data as unknown as Command;
    run(c).then(
      (result) => post({ state: current(), ack: c.id, result: result ?? undefined }),
      (e) => post({ state: current(), ack: c.id, error: String((e as Error)?.message ?? e) }),
    );
  };
  void (async () => {
    for (let wait = 250; ; wait = Math.min(wait * 2, 4000)) {
      try {
        const res = await fetch(`api/cap/events.watch?p=${encodeURIComponent("{}")}`, { headers: { "X-Jus-Via": "window" } });
        if (res.ok && res.body) {
          wait = 250;
          report(); // the window's state for whoever asks now
          const reader = res.body.pipeThrough(new TextDecoderStream()).getReader();
          let buf = "";
          for (;;) {
            const { value, done } = await reader.read();
            if (done) break;
            buf += value;
            for (let i = buf.indexOf("\n"); i >= 0; i = buf.indexOf("\n")) {
              handle(buf.slice(0, i));
              buf = buf.slice(i + 1);
            }
          }
        }
      } catch {
        // The server went away for a moment: try again.
      }
      await new Promise((r) => setTimeout(r, wait));
    }
  })();
}
