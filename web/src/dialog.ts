// The centred dialog: the one kind of panel that stops for an answer
// (design-language: "centre = things that need a stop"). It replaces the
// browser's prompt/confirm, which look foreign in the app and block the
// page. Enter takes the primary choice, Esc the cancel one.

export type Choice<T> = { label: string; value: T; primary?: boolean; danger?: boolean };

export type DialogSpec<T> = {
  title: string;
  body?: string;
  /** An input line; its value comes back as the answer's text. */
  input?: { value?: string; placeholder?: string; select?: [number, number] };
  /** Options for a select line (value and label). */
  options?: { value: string; label: string }[];
  choices: Choice<T>[];
  /** What Esc or a click outside answers. */
  cancel: T;
};

export type Answer<T> = { value: T; text: string };

const root = document.createElement("div");
root.id = "dialog";
root.className = "layer";
root.innerHTML = '<div class="dialog-card panel" role="dialog" aria-modal="true"><h2></h2><p></p><select></select><input spellcheck="false" autocomplete="off"><div class="dialog-actions"></div></div>';
document.body.append(root);
const card = root.querySelector<HTMLDivElement>(".dialog-card")!;
const titleEl = root.querySelector("h2")!;
const bodyEl = root.querySelector("p")!;
const selectEl = root.querySelector("select")!;
const inputEl = root.querySelector("input")!;
const actions = root.querySelector<HTMLDivElement>(".dialog-actions")!;

let pending: ((a: Answer<unknown>) => void) | null = null;
let cancelValue: unknown;
let primaryValue: unknown;

export function isOpen(): boolean {
  return root.classList.contains("open");
}

function finish(value: unknown): void {
  if (!pending) return;
  const done = pending;
  pending = null;
  root.classList.remove("open");
  const text = selectEl.hidden ? inputEl.value : selectEl.value;
  done({ value, text });
}

export function ask<T>(spec: DialogSpec<T>): Promise<Answer<T>> {
  if (pending) finish(cancelValue);
  titleEl.textContent = spec.title;
  bodyEl.textContent = spec.body ?? "";
  bodyEl.hidden = !spec.body;
  inputEl.hidden = !spec.input;
  inputEl.value = spec.input?.value ?? "";
  inputEl.placeholder = spec.input?.placeholder ?? "";
  selectEl.hidden = !spec.options;
  selectEl.replaceChildren(
    ...(spec.options ?? []).map((o) => {
      const e = document.createElement("option");
      e.value = o.value;
      e.textContent = o.label;
      return e;
    }),
  );
  cancelValue = spec.cancel;
  primaryValue = (spec.choices.find((c) => c.primary) ?? spec.choices[spec.choices.length - 1]).value;
  actions.replaceChildren(
    ...spec.choices.map((c) => {
      const b = document.createElement("button");
      b.textContent = c.label;
      b.className = c.primary ? "primary" : c.danger ? "quiet danger" : "quiet";
      b.addEventListener("click", () => finish(c.value));
      return b;
    }),
  );
  root.classList.add("open");
  requestAnimationFrame(() => {
    if (spec.input) {
      inputEl.focus();
      const [a, b] = spec.input.select ?? [0, inputEl.value.length];
      inputEl.setSelectionRange(a, b);
    } else if (spec.options) {
      selectEl.focus();
    } else {
      actions.querySelector<HTMLButtonElement>(".primary")?.focus();
    }
  });
  return new Promise((resolve) => {
    pending = resolve as (a: Answer<unknown>) => void;
  });
}

root.addEventListener("mousedown", (e) => {
  if (e.target === root) finish(cancelValue);
});
card.addEventListener("keydown", (e) => {
  if (e.key === "Escape") {
    e.preventDefault();
    e.stopPropagation();
    finish(cancelValue);
  } else if (e.key === "Enter" && !e.isComposing && (e.target === inputEl || e.target === selectEl)) {
    e.preventDefault();
    finish(primaryValue);
  }
});
