// Tooltips for elements with [data-tip]: the text is the element's
// aria-label, with its shortcut (data-key) as a key cap. The first appears
// after a short rest; moving to a neighbour right after shows the next one
// at once. A press or a key hides it. Same behaviour as Jusplay.
const FIRST_MS = 450;
const WARM_MS = 900;

export function initTips(): { hide(): void } {
  const tip = document.createElement("div");
  tip.className = "tip";
  tip.setAttribute("role", "tooltip");
  tip.hidden = true;
  document.body.append(tip);
  let timer = 0;
  let warmUntil = 0;
  let current: HTMLElement | null = null;
  let shown = false;

  const place = (t: HTMLElement) => {
    const label = t.getAttribute("aria-label");
    if (!label) return;
    const kids: (Node | string)[] = [label];
    if (t.dataset.key) {
      const k = document.createElement("kbd");
      k.textContent = t.dataset.key;
      kids.push(k);
    }
    tip.replaceChildren(...kids);
    tip.hidden = false;
    const r = t.getBoundingClientRect();
    const w = tip.offsetWidth;
    const h = tip.offsetHeight;
    const below = t.dataset.tipAt === "below" || r.top <= h + 14;
    const left = Math.min(innerWidth - w - 8, Math.max(8, r.left + r.width / 2 - w / 2));
    tip.style.left = `${left}px`;
    tip.style.top = `${below ? r.bottom + 8 : r.top - h - 8}px`;
    void tip.offsetWidth;
    tip.classList.add("show");
    shown = true;
  };

  const hide = () => {
    clearTimeout(timer);
    if (shown) warmUntil = performance.now() + WARM_MS;
    shown = false;
    current = null;
    tip.classList.remove("show");
  };

  document.addEventListener("pointerover", (e) => {
    const t = (e.target as Element).closest<HTMLElement>("[data-tip]");
    if (t === current) return;
    hide();
    if (!t) return;
    current = t;
    timer = window.setTimeout(() => place(t), performance.now() < warmUntil ? 0 : FIRST_MS);
  });
  document.addEventListener("pointerdown", hide, true);
  document.addEventListener("keydown", hide, true);
  document.addEventListener("scroll", hide, true);
  return { hide };
}
