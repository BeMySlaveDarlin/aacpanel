import { useCallback, useRef, useState } from "preact/hooks";

import { html } from "../../html.js";
import { Icon } from "../../ui/icons.js";
import { Pie } from "../../ui/pie.js";
import { useToast } from "../../ui/toasts.js";
import { useAction } from "../../actions/gate.js";
import { hostLabel } from "../../actions/registry.js";
import { CODEX_NOTE, isCodex } from "../../agent.js";
import { endsOf } from "../sessions/controls.js";
import { outsideNote, placeOf } from "../sessions/kin.js";
import { backgroundItems } from "./claudebg.js";
import { useRemote } from "./remote.js";
import { sessionEndSectors, sessionSectors } from "./sessionpie.js";

export function SessionButton(props) {
    const { live, win, open, onOpen } = props;
    const button = useRef(null);
    const close = useCallback(() => {
        onOpen(false);
        button.current?.focus({ preventScroll: true });
    }, [onOpen]);
    return html`
        <span class="dksessctl">
            <button type="button" ref=${button} class=${`dkplace${open ? " on" : ""}`} aria-expanded=${open} aria-haspopup="menu"
                    aria-label="the session: where it lives, Remote Control and what can be done to it"
                    onClick=${() => onOpen(!open)}>
                <span class="dkplaceicon">${Icon.pin()}</span><span>${placeOf(live)}</span>
                ${live.transport !== "stream" && win.kind === "open" && html`<span class="dkplacewin">window</span>`}
                ${live.remote && html`<span class="dkrc">RC</span>`}
                <span class="dkplacechev">${Icon.chevron()}</span>
            </button>
            ${open && html`<${SessionPie} key=${props.name} ...${props} button=${button} onClose=${close} />`}
        </span>
    `;
}

function SessionPie({ name, live, exec, snapshot, cwd, way, win, work, onWindow, onLook, onTerm, button, onClose }) {
    const run = useAction();
    const toast = useToast();
    const remote = useRemote({ name, live, exec, snapshot });
    const [spot] = useState(() => {
        const box = button.current?.getBoundingClientRect();
        return { x: box ? box.left + box.width / 2 : window.innerWidth / 2,
            y: box ? box.bottom + 240 : window.innerHeight / 2, centered: true };
    });
    const codex = isCodex(live);
    const ends = endsOf({ s: live, exec, pin: {}, busy: Boolean(live.restarting) });
    const tools = sessionSectors({ name, live, exec, snapshot, cwd, run, toast, remote, way, win, work,
        onWindow, onLook, desktop: true });
    const end = sessionEndSectors({ name, live, exec, run, ends });
    const items = live.transport === "bg"
        ? backgroundItems({ live, exec, run, onTerm }).map((it) => ({ ...it, title: it.label, pick: it.act }))
        : [...tools, ...(!live.outside ? [
            codex && end.stop,
            (codex || live.home) && end.restart,
            !live.home && end.close,
        ].filter(Boolean) : [])];
    const place = live.transport === "bg" ? "Claude Code background session" : live.outside ? "Outside the panel"
        : codex ? "With codex"
        : live.transport === "stream" ? "On the stream" : `In tmux${win.kind === "open" ? " · a window shows it" : ""}`;
    const note = live.outside ? outsideNote(live) : codex ? CODEX_NOTE
        : live.transport === "stream" ? `claude -p held by the panel: no terminal and no window on ${hostLabel()}`
        : `a terminal on ${hostLabel()}`;
    return html`<${Pie} label=${`session ${name}`} name=${place} note=${note} items=${items} spot=${spot} tabCycle onClose=${onClose} />`;
}
