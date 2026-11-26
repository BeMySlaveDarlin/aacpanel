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
    // Vendor marks are filled paths from LobeHub; see web/vendor/LICENSE-lobehub-icons.
    claude: () => html`<svg viewBox="0 0 24 24" fill="currentColor" fill-rule="evenodd"><path d="M4.709 15.955l4.72-2.647.08-.23-.08-.128H9.2l-.79-.048-2.698-.073-2.339-.097-2.266-.122-.571-.121L0 11.784l.055-.352.48-.321.686.06 1.52.103 2.278.158 1.652.097 2.449.255h.389l.055-.157-.134-.098-.103-.097-2.358-1.596-2.552-1.688-1.336-.972-.724-.491-.364-.462-.158-1.008.656-.722.881.06.225.061.893.686 1.908 1.476 2.491 1.833.365.304.145-.103.019-.073-.164-.274-1.355-2.446-1.446-2.49-.644-1.032-.17-.619a2.97 2.97 0 01-.104-.729L6.283.134 6.696 0l.996.134.42.364.62 1.414 1.002 2.229 1.555 3.03.456.898.243.832.091.255h.158V9.01l.128-1.706.237-2.095.23-2.695.08-.76.376-.91.747-.492.584.28.48.685-.067.444-.286 1.851-.559 2.903-.364 1.942h.212l.243-.242.985-1.306 1.652-2.064.73-.82.85-.904.547-.431h1.033l.76 1.129-.34 1.166-1.064 1.347-.881 1.142-1.264 1.7-.79 1.36.073.11.188-.02 2.856-.606 1.543-.28 1.841-.315.833.388.091.395-.328.807-1.969.486-2.309.462-3.439.813-.042.03.049.061 1.549.146.662.036h1.622l3.02.225.79.522.474.638-.079.485-1.215.62-1.64-.389-3.829-.91-1.312-.329h-.182v.11l1.093 1.068 2.006 1.81 2.509 2.33.127.578-.322.455-.34-.049-2.205-1.657-.851-.747-1.926-1.62h-.128v.17l.444.649 2.345 3.521.122 1.08-.17.353-.608.213-.668-.122-1.374-1.925-1.415-2.167-1.143-1.943-.14.08-.674 7.254-.316.37-.729.28-.607-.461-.322-.747.322-1.476.389-1.924.315-1.53.286-1.9.17-.632-.012-.042-.14.018-1.434 1.967-2.18 2.945-1.726 1.845-.414.164-.717-.37.067-.662.401-.589 2.388-3.036 1.44-1.882.93-1.086-.006-.158h-.055L4.132 18.56l-1.13.146-.487-.456.061-.746.231-.243 1.908-1.312-.006.006z" /></svg>`,
    openai: () => html`<svg viewBox="0 0 24 24" fill="currentColor" fill-rule="evenodd"><path d="M9.205 8.658v-2.26c0-.19.072-.333.238-.428l4.543-2.616c.619-.357 1.356-.523 2.117-.523 2.854 0 4.662 2.212 4.662 4.566 0 .167 0 .357-.024.547l-4.71-2.759a.797.797 0 00-.856 0l-5.97 3.473zm10.609 8.8V12.06c0-.333-.143-.57-.429-.737l-5.97-3.473 1.95-1.118a.433.433 0 01.476 0l4.543 2.617c1.309.76 2.189 2.378 2.189 3.948 0 1.808-1.07 3.473-2.76 4.163zM7.802 12.703l-1.95-1.142c-.167-.095-.239-.238-.239-.428V5.899c0-2.545 1.95-4.472 4.591-4.472 1 0 1.927.333 2.712.928L8.23 5.067c-.285.166-.428.404-.428.737v6.898zM12 15.128l-2.795-1.57v-3.33L12 8.658l2.795 1.57v3.33L12 15.128zm1.796 7.23c-1 0-1.927-.332-2.712-.927l4.686-2.712c.285-.166.428-.404.428-.737v-6.898l1.974 1.142c.167.095.238.238.238.428v5.233c0 2.545-1.974 4.472-4.614 4.472zm-5.637-5.303l-4.544-2.617c-1.308-.761-2.188-2.378-2.188-3.948A4.482 4.482 0 014.21 6.327v5.423c0 .333.143.571.428.738l5.947 3.449-1.95 1.118a.432.432 0 01-.476 0zm-.262 3.9c-2.688 0-4.662-2.021-4.662-4.519 0-.19.024-.38.047-.57l4.686 2.71c.286.167.571.167.856 0l5.97-3.448v2.26c0 .19-.07.333-.237.428l-4.543 2.616c-.619.357-1.356.523-2.117.523zm5.899 2.83a5.947 5.947 0 005.827-4.756C22.287 18.339 24 15.84 24 13.296c0-1.665-.713-3.282-1.998-4.448.119-.5.19-.999.19-1.498 0-3.401-2.759-5.947-5.946-5.947-.642 0-1.26.095-1.88.31A5.962 5.962 0 0010.205 0a5.947 5.947 0 00-5.827 4.757C1.713 5.447 0 7.945 0 10.49c0 1.666.713 3.283 1.998 4.448-.119.5-.19 1-.19 1.499 0 3.401 2.759 5.946 5.946 5.946.642 0 1.26-.095 1.88-.309a5.96 5.96 0 004.162 1.713z" /></svg>`,
    gemini: () => html`<svg viewBox="0 0 24 24" fill="currentColor" fill-rule="evenodd"><path d="M20.616 10.835a14.147 14.147 0 01-4.45-3.001 14.111 14.111 0 01-3.678-6.452.503.503 0 00-.975 0 14.134 14.134 0 01-3.679 6.452 14.155 14.155 0 01-4.45 3.001c-.65.28-1.318.505-2.002.678a.502.502 0 000 .975c.684.172 1.35.397 2.002.677a14.147 14.147 0 014.45 3.001 14.112 14.112 0 013.679 6.453.502.502 0 00.975 0c.172-.685.397-1.351.677-2.003a14.145 14.145 0 013.001-4.45 14.113 14.113 0 016.453-3.678.503.503 0 000-.975 13.245 13.245 0 01-2.003-.678z" /></svg>`,
    thumbtack: () => html`<svg ...${stroke}><path d="m16 3 5 5-4 1-4 5-1 4-6-6 4-1 5-4ZM6 18l-3 3" /></svg>`,
    containers: () => html`<svg ...${stroke}><rect x="3" y="4" width="18" height="7" rx="1.5" /><rect x="3" y="13" width="18" height="7" rx="1.5" /></svg>`,

    sessions: () => html`<svg ...${stroke}><rect x="2.5" y="4" width="19" height="14" rx="2" /><path d="m7 9 2.5 2L7 13M12.5 14H17" /></svg>`,

    cpu: () => html`<svg ...${stroke}><rect x="8" y="8" width="8" height="8" rx="1.5" /><path d="M10 3v3M14 3v3M10 18v3M14 18v3M3 10h3M3 14h3M18 10h3M18 14h3" /></svg>`,

    memory: () => html`<svg ...${stroke}><rect x="3" y="7" width="18" height="10" rx="2" /><path d="M7 11v2M11 11v2M15 11v2" /></svg>`,

    info: () => html`<svg ...${stroke}><circle cx="12" cy="12" r="9" /><path d="M12 11v5" /><circle cx="12" cy="7.8" r="1" fill="currentColor" stroke="none" /></svg>`,

    settings: () => html`<svg ...${stroke}><path d="m9.5 3-.5 2-1.6.9-2-.6-2.5 4.4 1.5 1.4v1.8l-1.5 1.4 2.5 4.4 2-.6L9 19l.5 2h5l.5-2 1.6-.9 2 .6 2.5-4.4-1.5-1.4v-1.8l1.5-1.4-2.5-4.4-2 .6L15 5l-.5-2Z" /><circle cx="12" cy="12" r="3" /></svg>`,

    disk: () => html`<svg ...${stroke}><circle cx="12" cy="12" r="8.5" /><circle cx="12" cy="12" r="2.5" /></svg>`,

    // The viewer: the files of a project, which is a directory before it is
    // anything else.
    files: () => html`<svg ...${stroke}><path d="M3 7.6a2 2 0 0 1 2-2h3.6l1.8 2.2H19a2 2 0 0 1 2 2v7.6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2Z" /></svg>`,
    // Wrapping long lines, the first thing people ask to be able to turn off.
    wrap: () => html`<svg ...${stroke}><path d="M3 6h18M3 12h13a3.5 3.5 0 0 1 0 7h-3l2-2M13 19l2 2M3 18h5" /></svg>`,
    terminal: () => html`<svg ...${stroke}><rect x="2.5" y="4" width="19" height="16" rx="2" /><path d="m7 9.5 2.5 2.5L7 14.5M13 15h4" /></svg>`,

    // A prompt without a frame: the terminals of places, told apart from the
    // framed screen the sessions are drawn with.
    prompt: () => html`<svg ...${stroke}><path d="m4.5 6.5 5.5 5.5-5.5 5.5M12.5 18h7" /></svg>`,

    file: () => html`<svg ...${stroke}><path d="M13.5 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8.5Z" /><path d="M13.5 3v5.5H19" /></svg>`,

    globe: () => html`<svg ...${stroke}><circle cx="12" cy="12" r="9" /><path d="M3 12h18M12 3c2.5 2.7 2.5 15.3 0 18M12 3c-2.5 2.7-2.5 15.3 0 18" /></svg>`,

    robot: () => html`<svg ...${stroke}><rect x="4" y="8" width="16" height="11" rx="2.5" /><path d="M12 4.5V8M9 13h.01M15 13h.01M9.5 16h5" /></svg>`,

    command: () => html`<svg ...${stroke}><path d="M15 6v12a3 3 0 1 0 3-3H6a3 3 0 1 0 3 3V6a3 3 0 1 0-3 3h12a3 3 0 1 0-3-3" /></svg>`,
    list: () => html`<svg ...${stroke}><path d="M9 6h11M9 12h11M9 18h11" /><path d="M4 6h.01M4 12h.01M4 18h.01" /></svg>`,
    checklist: () => html`<svg ...${stroke}><path d="m3.6 6.5 1.6 1.6 2.9-3M3.6 12.5l1.6 1.6 2.9-3M3.6 18.5l1.6 1.6 2.9-3M11.6 7h8.6M11.6 13h8.6M11.6 19h5.6" /></svg>`,

    // A workflow: one call that fans out into agents working side by side and
    // gathers them back into one result.
    flow: () => html`<svg ...${stroke}><rect x="9.2" y="3" width="5.6" height="4.4" rx="1.2" /><rect x="3" y="16.6" width="5.6" height="4.4" rx="1.2" /><rect x="15.4" y="16.6" width="5.6" height="4.4" rx="1.2" /><path d="M12 7.4v3.1M5.8 16.6v-3.2h12.4v3.2M12 10.5v2.9" /></svg>`,

    envelope: () => html`<svg ...${stroke}><rect x="3" y="6.5" width="18" height="13" rx="2" /><path d="m3.5 8 8.5 6 8.5-6" /></svg>`,

    thinking: () => html`<svg ...${stroke}><path d="M14.6 3.8a5.6 5.6 0 0 0-9 2.5A3.6 3.6 0 0 0 4.2 12a3.6 3.6 0 0 0 3 5.8h1.3" /><path d="M14.6 3.8a6 6 0 0 1 4.9 5.8 5.9 5.9 0 0 1-3.2 5.2v6" /><path d="M10 8.6c1.8.3 3 1.9 2.7 3.6" /></svg>`,

    plug: () => html`<svg ...${stroke}><path d="M9 3v6M15 3v6" /><path d="M6.5 9h11v3a5.5 5.5 0 0 1-11 0Z" /><path d="M12 17.5V21" /></svg>`,

    skill: () => html`<svg ...${stroke}><path d="M12 3 21 12l-9 9-9-9Z" /><path d="M7.5 12h9" /></svg>`,

    ask: () => html`<svg ...${stroke}><path d="M9.2 9a2.9 2.9 0 1 1 3.6 2.8c-.5.2-.8.7-.8 1.2v.8" /><circle cx="12" cy="17.4" r="1.1" fill="currentColor" stroke="none" /><circle cx="12" cy="12" r="9" /></svg>`,

    artifact: () => html`<svg ...${stroke}><rect x="3" y="4.5" width="18" height="15" rx="2" /><path d="M3 9h18" /><circle cx="6.4" cy="6.7" r=".7" fill="currentColor" stroke="none" /></svg>`,

    pause: () => html`<svg ...${stroke}><path d="M8 5v14M16 5v14" /></svg>`,
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

    eye: () => html`<svg ...${stroke}><path d="M2.5 12S6 5.5 12 5.5 21.5 12 21.5 12 18 18.5 12 18.5 2.5 12 2.5 12Z" /><circle cx="12" cy="12" r="2.8" /></svg>`,
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

    // A badge: who a session is among the others of its repository, its role.
    badge: () => html`<svg ...${stroke}><rect x="3" y="5" width="18" height="14" rx="2" /><circle cx="9" cy="11" r="2.2" /><path d="M5.8 16.2a3.4 3.4 0 0 1 6.4 0M14.5 10h4M14.5 13.5h3" /></svg>`,

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

    remote: () => html`<svg ...${stroke}><rect x="5" y="3" width="10" height="18" rx="2" /><path d="M9 18h2M18.5 9a4.5 4.5 0 0 1 0 6M21 6.5a8 8 0 0 1 0 11" /></svg>`,
    monitor: () => html`<svg ...${stroke}><rect x="2.5" y="4.5" width="19" height="12" rx="2" /><path d="M12 16.5V19M8.5 19h7" /></svg>`,
    // Where a session lives: on the stream or in tmux on the host.
    pin: () => html`<svg ...${stroke}><path d="M12 21s-6.5-5.4-6.5-11a6.5 6.5 0 0 1 13 0c0 5.6-6.5 11-6.5 11Z" /><circle cx="12" cy="10" r="2.3" /></svg>`,
    // What else can be done here: the tools a header has no room for.
    more: () => html`<svg ...${stroke}><circle cx="5.5" cy="12" r="1.4" fill="currentColor" stroke="none" /><circle cx="12" cy="12" r="1.4" fill="currentColor" stroke="none" /><circle cx="18.5" cy="12" r="1.4" fill="currentColor" stroke="none" /></svg>`,

    download: () => html`<svg ...${stroke}><path d="M12 4v10M8 10.5l4 4 4-4M5 19h14" /></svg>`,

    // A secret a session asked for: a file of the host the session uses by
    // its path and never reads.
    key: () => html`<svg ...${stroke}><circle cx="7.5" cy="15.5" r="4" /><path d="m10.4 12.6 9.1-9.1M16 7l2.8 2.8M13.4 9.6l2 2" /></svg>`,
    // A tool that waits for the person's leave to run.
    lock: () => html`<svg ...${stroke}><rect x="5" y="10.5" width="14" height="10" rx="2" /><path d="M8 10.5V7.5a4 4 0 0 1 8 0v3" /></svg>`,
    // A conversation being compacted: four corners drawn in.
    compact: () => html`<svg ...${stroke}><path d="M4 9h5V4M20 9h-5V4M4 15h5v5M20 15h-5v5" /></svg>`,
    // A session stopped by the limit of its subscription.
    limit: () => html`<svg ...${stroke}><circle cx="12" cy="12" r="8" /><path d="m6.6 17.4 10.8-10.8" /></svg>`,
};
