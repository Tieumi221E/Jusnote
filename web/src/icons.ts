// Line icons on a 20×20 grid, drawn with currentColor (styled in CSS:
// `.icon svg`). Filled shapes carry class="f". Same convention as Jusplay.
const svg = (body: string) => `<svg viewBox="0 0 20 20" aria-hidden="true">${body}</svg>`;

export const ICONS = {
  files: svg(
    '<rect x="2.5" y="4" width="5" height="12" rx="1.4"/><path d="M10.5 5.2h7M10.5 8.2h5M10.5 11.2h7M10.5 14.2h5"/>',
  ),
  panel: svg('<rect x="3" y="1.5" width="14" height="17" rx="2.4"/><path d="M13 1.5v17"/>'),
  search: svg('<circle cx="8.5" cy="8.5" r="5"/><path d="m12.5 12.5 4 4"/>'),
  command: svg('<rect x="2.5" y="3.5" width="15" height="13" rx="2.4"/><path d="m6.5 8 2.5 2-2.5 2M11 12h3"/>'),
  plus: svg('<path d="M10 4v12M4 10h12"/>'),
  sun: svg('<circle cx="10" cy="10" r="3.6"/><path d="M10 2v2.2M10 15.8V18M2 10h2.2M15.8 10H18M4.4 4.4l1.6 1.6M14 14l1.6 1.6M15.6 4.4 14 6M6 14l-1.6 1.6"/>'),
  moon: svg('<path d="M16 12.2A6.5 6.5 0 0 1 7.8 4a6.6 6.6 0 1 0 8.2 8.2z"/>'),
};

export type IconName = keyof typeof ICONS;
