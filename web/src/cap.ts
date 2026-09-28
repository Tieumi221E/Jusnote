// The window's capabilities (internal/capreg, Jus contract 17): the page
// calls the same operations the command line does, by the same names, so
// there is one implementation of each.

export class CapError extends Error {
  /** plan: for kind "confirm", what the call would do (call again with yes). */
  constructor(message: string, readonly kind: string, readonly plan?: unknown) {
    super(message);
  }
}

/** Call capability id (e.g. "subs.show") with its parameters. */
export async function cap<T>(id: string, params: Record<string, unknown> = {}, signal?: AbortSignal): Promise<T> {
  const r = await fetch(`api/cap/${id}`, {
    method: "POST",
    headers: { "Content-Type": "application/json", "X-Jus-Via": "window" },
    body: JSON.stringify(params),
    signal,
  });
  const j = await r.json().catch(() => null);
  if (!r.ok) throw new CapError(j?.error ?? `${r.status} ${r.statusText}`, j?.kind ?? "failed", j?.plan);
  return j as T;
}
