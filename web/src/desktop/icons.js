// Icons used only by the desktop layout.
import { html } from "../html.js";

const stroke = {
    fill: "none",
    stroke: "currentColor",
    "stroke-width": 2.2,
    "stroke-linecap": "round",
    "stroke-linejoin": "round",
    viewBox: "0 0 24 24",
};

export const DeskIcon = {
    home: () => html`<svg ...${stroke}><path d="M4 11.2 12 4l8 7.2" /><path d="M6 10.5V20h12v-9.5" /></svg>`,
    exit: () => html`<svg ...${stroke}><path d="M14 4H6v16h8" /><path d="m17 8 4 4-4 4M21 12H10" /></svg>`,
    feed: () => html`<svg ...${stroke}><path d="M4 6.5h16v9H9l-5 4z" /></svg>`,
    power: () => html`<svg ...${stroke}><path d="M12 3.5v7.5" /><path d="M7.5 6.4a7 7 0 1 0 9 0" /></svg>`,
    scan: () => html`<svg ...${stroke}><path d="M20 12a8 8 0 1 1-2.6-5.9" /><path d="M20 4v4.5h-4.5" /></svg>`,
};
