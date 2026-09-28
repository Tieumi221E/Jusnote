// Every control that does something names the capability it uses
// (data-cap, on it or a container), and so does every command of the
// palette (its cap), so the window has no ability the command line lacks
// (Jus contract 17). "ui" marks what only moves the view (a panel,
// scrolling, the preview); "ui:shell" hands something to the system
// (Explorer, a terminal). This check finds controls and commands without a
// name, and names that are not capabilities (GET api/cap, the same list as
// `jusnote help -json`).

export interface Coverage {
  controls: number;
  commands: number;
  /** Controls with no data-cap: tag#id.class "label"; commands without a cap. */
  missing: string[];
  /** Capability names used that Jusnote does not have. */
  unknown: string[];
}

const CONTROLS = "button, input, select, textarea, [role=button], [role=menuitem], [role=link], [tabindex]:not([tabindex='-1']), a[href]:not([href^='#'])";

function describe(e: Element): string {
  const id = e.id ? `#${e.id}` : "";
  const cls = e.classList.length ? `.${[...e.classList].join(".")}` : "";
  const text = (e.getAttribute("aria-label") || e.getAttribute("title") || e.textContent || "").trim().slice(0, 30);
  return `${e.tagName.toLowerCase()}${id}${cls}${text ? ` "${text}"` : ""}`;
}

export async function capCoverage(commands: { zh: string; cap: string }[], root: ParentNode = document): Promise<Coverage> {
  const help = (await fetch("api/cap").then((r) => r.json())) as { commands: { id: string }[] };
  const known = new Set(help.commands.map((c) => c.id));
  const out: Coverage = { controls: 0, commands: commands.length, missing: [], unknown: [] };
  const use = (cap: string) => {
    for (const c of cap.split(/\s+/)) {
      if (c !== "ui" && !c.startsWith("ui:") && !known.has(c) && !out.unknown.includes(c)) out.unknown.push(c);
    }
  };
  for (const e of root.querySelectorAll(CONTROLS)) {
    out.controls++;
    const cap = e.closest("[data-cap]")?.getAttribute("data-cap");
    if (cap) use(cap);
    else out.missing.push(describe(e));
  }
  for (const c of commands) {
    if (c.cap.trim()) use(c.cap);
    else out.missing.push(`command "${c.zh}"`);
  }
  return out;
}
