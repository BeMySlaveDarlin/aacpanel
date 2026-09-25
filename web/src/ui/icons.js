// Interface icons, inlined as SVG so the app draws offline.
import { html } from "../html.js";

const stroke = {
    fill: "none",
    stroke: "currentColor",
    "stroke-width": "1.7",
    "stroke-linecap": "round",
    "stroke-linejoin": "round",
    viewBox: "0 0 24 24",
};

export const Icon = {
    containers: () => html`<svg ...${stroke}><rect x="3" y="4" width="18" height="7" rx="1.5" /><rect x="3" y="13" width="18" height="7" rx="1.5" /></svg>`,

    sessions: () => html`<svg ...${stroke}><rect x="2.5" y="4" width="19" height="14" rx="2" /><path d="m7 9 2.5 2L7 13M12.5 14H17" /></svg>`,

    cpu: () => html`<svg ...${stroke}><rect x="8" y="8" width="8" height="8" rx="1.5" /><path d="M10 3v3M14 3v3M10 18v3M14 18v3M3 10h3M3 14h3M18 10h3M18 14h3" /></svg>`,

    memory: () => html`<svg ...${stroke}><rect x="3" y="7" width="18" height="10" rx="2" /><path d="M7 11v2M11 11v2M15 11v2" /></svg>`,

    info: () => html`<svg ...${stroke}><circle cx="12" cy="12" r="9" /><path d="M12 11v5" /><circle cx="12" cy="7.8" r="1" fill="currentColor" stroke="none" /></svg>`,

    disk: () => html`<svg ...${stroke}><circle cx="12" cy="12" r="8.5" /><circle cx="12" cy="12" r="2.5" /></svg>`,

    // The viewer: the files of a project, which is a directory before it is
    // anything else.
    files: () => html`<svg ...${stroke}><path d="M3 7.6a2 2 0 0 1 2-2h3.6l1.8 2.2H19a2 2 0 0 1 2 2v7.6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2Z" /></svg>`,
    // Wrapping long lines, the first thing people ask to be able to turn off.
    wrap: () => html`<svg ...${stroke}><path d="M3 6h18M3 12h13a3.5 3.5 0 0 1 0 7h-3l2-2M13 19l2 2M3 18h5" /></svg>`,
    terminal: () => html`<svg ...${stroke}><rect x="2.5" y="4" width="19" height="16" rx="2" /><path d="m7 9.5 2.5 2.5L7 14.5M13 15h4" /></svg>`,

    file: () => html`<svg ...${stroke}><path d="M13.5 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8.5Z" /><path d="M13.5 3v5.5H19" /></svg>`,

    globe: () => html`<svg ...${stroke}><circle cx="12" cy="12" r="9" /><path d="M3 12h18M12 3c2.5 2.7 2.5 15.3 0 18M12 3c-2.5 2.7-2.5 15.3 0 18" /></svg>`,

    robot: () => html`<svg ...${stroke}><rect x="4" y="8" width="16" height="11" rx="2.5" /><path d="M12 4.5V8M9 13h.01M15 13h.01M9.5 16h5" /></svg>`,

    list: () => html`<svg ...${stroke}><path d="M9 6h11M9 12h11M9 18h11" /><path d="M4 6h.01M4 12h.01M4 18h.01" /></svg>`,

    // A workflow: one call that fans out into agents working side by side and
    // gathers them back into one result.
    flow: () => html`<svg ...${stroke}><rect x="9.2" y="3" width="5.6" height="4.4" rx="1.2" /><rect x="3" y="16.6" width="5.6" height="4.4" rx="1.2" /><rect x="15.4" y="16.6" width="5.6" height="4.4" rx="1.2" /><path d="M12 7.4v3.1M5.8 16.6v-3.2h12.4v3.2M12 10.5v2.9" /></svg>`,

    envelope: () => html`<svg ...${stroke}><rect x="3" y="6.5" width="18" height="13" rx="2" /><path d="m3.5 8 8.5 6 8.5-6" /></svg>`,

    thinking: () => html`<svg ...${stroke}><path d="M14.6 3.8a5.6 5.6 0 0 0-9 2.5A3.6 3.6 0 0 0 4.2 12a3.6 3.6 0 0 0 3 5.8h1.3" /><path d="M14.6 3.8a6 6 0 0 1 4.9 5.8 5.9 5.9 0 0 1-3.2 5.2v6" /><path d="M10 8.6c1.8.3 3 1.9 2.7 3.6" /></svg>`,

    plug: () => html`<svg ...${stroke}><path d="M9 3v6M15 3v6" /><path d="M6.5 9h11v3a5.5 5.5 0 0 1-11 0Z" /><path d="M12 17.5V21" /></svg>`,

    skill: () => html`<svg ...${stroke}><path d="M12 3 21 12l-9 9-9-9Z" /><path d="M7.5 12h9" /></svg>`,

    ask: () => html`<svg ...${stroke}><path d="M9.2 9a2.9 2.9 0 1 1 3.6 2.8c-.5.2-.8.7-.8 1.2v.8" /><circle cx="12" cy="17.4" r="1.1" fill="currentColor" stroke="none" /><circle cx="12" cy="12" r="9" /></svg>`,

    artifact: () => html`<svg ...${stroke}><rect x="3" y="4.5" width="18" height="15" rx="2" /><path d="M3 9h18" /><circle cx="6.4" cy="6.7" r=".7" fill="currentColor" stroke="none" /></svg>`,

    clock: () => html`<svg ...${stroke}><circle cx="12" cy="12" r="9" /><path d="M12 7v5.3l3.4 2" /></svg>`,
    hourglass: () => html`<svg ...${stroke}><path d="M6.5 3.5h11M6.5 20.5h11" /><path d="M7.5 3.5v2.3a5 5 0 0 0 2.6 4.4L12 11.2l1.9-1a5 5 0 0 0 2.6-4.4V3.5" /><path d="M7.5 20.5v-2.3a5 5 0 0 1 2.6-4.4l1.9-1 1.9 1a5 5 0 0 1 2.6 4.4v2.3" /></svg>`,

    // The four permission modes: a raised hand asks first, a pair of brackets
    // takes edits on its own, a sheet with lines plans, a bolt decides itself.
    hand: () => html`<svg ...${stroke}><path d="M8 13V5.5a1.5 1.5 0 0 1 3 0V12M11 11V4.5a1.5 1.5 0 0 1 3 0V12M14 11.5V6a1.5 1.5 0 0 1 3 0v8a7 7 0 0 1-7 7h-.5A6.5 6.5 0 0 1 4 16.4L2.8 13.8a1.5 1.5 0 0 1 2.6-1.5L8 15" /></svg>`,

    braces: () => html`<svg ...${stroke}><path d="m8 7-5 5 5 5M16 7l5 5-5 5M14 4l-4 16" /></svg>`,

    plan: () => html`<svg ...${stroke}><path d="M6 3h11a2 2 0 0 1 2 2v14a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2ZM8 8h7M8 12h7M8 16h4" /></svg>`,

    hook: () => html`<svg ...${stroke}><path d="M14 3v10.5a4.5 4.5 0 0 1-9 0V12" /><path d="m3 14 2-2 2 2" /><circle cx="14" cy="3" r=".6" fill="currentColor" /></svg>`,
    bolt: () => html`<svg ...${stroke}><path d="M13 2.5 4.5 13.5H12l-1 8 8.5-11H12Z" /></svg>`,

    // A share of a whole: how much of the context window is taken.
    pie: () => html`<svg ...${stroke}><circle cx="12" cy="12" r="9" /><path d="M12 3v9h9" /></svg>`,

    cube: () => html`<svg ...${stroke}><path d="M12 2.8 20 7v10l-8 4.2L4 17V7Z" /><path d="m4 7 8 4.2L20 7M12 11.2V21" /></svg>`,

    alerts: () => html`<svg ...${stroke}><path d="M18 8A6 6 0 0 0 6 8c0 7-3 9-3 9h18s-3-2-3-9" /><path d="M13.7 21a2 2 0 0 1-3.4 0" /></svg>`,

    probes: () => html`<svg ...${stroke}><path d="M4.5 11.5a10 10 0 0 1 15 0" /><path d="M8 15a5.5 5.5 0 0 1 8 0" /><circle cx="12" cy="18.5" r="1.2" fill="currentColor" stroke="none" /></svg>`,

    orbit: () => html`<svg ...${stroke}><circle cx="12" cy="12" r="5.2" /><ellipse cx="12" cy="12" rx="10" ry="4.2" transform="rotate(-24 12 12)" /></svg>`,

    sun: () => html`<svg ...${stroke}><circle cx="12" cy="12" r="4.2" /><path d="M12 2.5v2M12 19.5v2M4.6 4.6l1.4 1.4M18 18l1.4 1.4M2.5 12h2M19.5 12h2M4.6 19.4 6 18M18 6l1.4-1.4" /></svg>`,

    moon: () => html`<svg ...${stroke}><path d="M20 13.5A8 8 0 0 1 10.5 4a8.5 8.5 0 1 0 9.5 9.5" /></svg>`,

    search: () => html`<svg ...${stroke}><circle cx="11" cy="11" r="7" /><path d="m20 20-3.5-3.5" /></svg>`,

    chevron: () => html`<svg ...${stroke}><path d="m9 6 6 6-6 6" /></svg>`,

    close: () => html`<svg ...${stroke}><path d="M18 6 6 18M6 6l12 12" /></svg>`,
    trash: () => html`<svg ...${stroke}><path d="M4.5 7h15M9.5 7V4.5h5V7M6.5 7l.9 12.1a1.6 1.6 0 0 0 1.6 1.4h6a1.6 1.6 0 0 0 1.6-1.4L17.5 7M10 11v6M14 11v6" /></svg>`,

    send: () => html`<svg ...${stroke}><circle cx="12" cy="12" r="9" /><path d="M12 16.5v-9M8.5 11 12 7.5l3.5 3.5" /></svg>`,
    arrowup: () => html`<svg ...${stroke}><path d="M12 19V5.5M6 11.5l6-6 6 6" /></svg>`,

    quote: () => html`<svg ...${stroke}><path d="M9 6 4 11l5 5" /><path d="M4 11h9a6 6 0 0 1 6 6v1" /></svg>`,

    copy: () => html`<svg ...${stroke}><rect x="9" y="9" width="11" height="11" rx="2" /><path d="M5 15H4a1 1 0 0 1-1-1V4a1 1 0 0 1 1-1h10a1 1 0 0 1 1 1v1" /></svg>`,

    check: () => html`<svg ...${stroke}><path d="m5 12.5 4.5 4.5L19 7.5" /></svg>`,

    clip: () => html`<svg ...${stroke}><path d="M20 11.5 12 19.4a5 5 0 0 1-7.1-7L13.6 3.7a3.4 3.4 0 0 1 4.8 4.8l-8.5 8.6a1.8 1.8 0 0 1-2.5-2.5l7.9-7.9" /></svg>`,

    mic: () => html`<svg ...${stroke}><rect x="9" y="2.8" width="6" height="11" rx="3" /><path d="M5.5 11.4a6.5 6.5 0 0 0 13 0M12 17.9V21M9 21h6" /></svg>`,

    camera: () => html`<svg ...${stroke}><path d="M3 8.5a2 2 0 0 1 2-2h2.2l1.3-2h6l1.3 2H19a2 2 0 0 1 2 2v9a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2Z" /><circle cx="12" cy="13" r="3.4" /></svg>`,

    photo: () => html`<svg ...${stroke}><rect x="3" y="5" width="18" height="14" rx="2" /><circle cx="8.5" cy="10" r="1.6" /><path d="m4 17 5-5 4.5 4.5L16.5 13l3.5 3.5" /></svg>`,

    tools: () => html`<svg ...${stroke}><circle cx="5" cy="7" r="1.3" fill="currentColor" stroke="none" /><circle cx="5" cy="12" r="1.3" fill="currentColor" stroke="none" /><circle cx="5" cy="17" r="1.3" fill="currentColor" stroke="none" /><path d="M9.5 7H19M9.5 12h6.5M9.5 17h8" /></svg>`,

    stopsquare: () => html`<svg viewBox="0 0 24 24" fill="currentColor" stroke="none"><rect x="6.5" y="6.5" width="11" height="11" rx="2" /></svg>`,

    pencil: () => html`<svg ...${stroke}><path d="M4 20h4l10-10a2.1 2.1 0 0 0-3-3L5 17v3Z" /><path d="M13.5 6.5l3 3" /></svg>`,
    plus: () => html`<svg ...${stroke}><path d="M12 5v14M5 12h14" /></svg>`,

    play: () => html`<svg viewBox="0 0 24 24" fill="currentColor" stroke="none"><path d="M8 5.5v13l11-6.5z" /></svg>`,

    stop: () => html`<svg ...${stroke}><path d="M12 3.2v8.8" /><path d="M6.8 6.3a8 8 0 1 0 10.4 0" /></svg>`,

    resume: () => html`<svg ...${stroke}><path d="M3.5 5.5v5h5" /><path d="M4.2 10.5a8 8 0 1 1 .6 5" /></svg>`,

    refresh: () => html`<svg ...${stroke}><path d="M20.5 5.5v5h-5" /><path d="M19.8 10.5a8 8 0 1 0-.6 5" /></svg>`,

    exit: () => html`<svg ...${stroke}><path d="M14 4H6v16h8" /><path d="m17 8 4 4-4 4M21 12H10" /></svg>`,

    feed: () => html`<svg ...${stroke}><path d="M3.5 6.5h17v9H9l-5.5 4z" /></svg>`,

    monitor: () => html`<svg ...${stroke}><rect x="2.5" y="4.5" width="19" height="12" rx="2" /><path d="M12 16.5V19M8.5 19h7" /></svg>`,

    download: () => html`<svg ...${stroke}><path d="M12 4v10M8 10.5l4 4 4-4M5 19h14" /></svg>`,
};
