// Log panel that docks above the bottom menu and survives tab switches.
import { useEffect, useRef } from "preact/hooks";

import { html } from "../html.js";
import { useLogs } from "../logs.js";
import { Icon } from "./icons.js";

const STATE_WORD = {
    connecting: "connecting",
    live: "the stream is running",
    reconnecting: "reconnecting",
    ended: "the stream ended",
    failed: "the stream broke off",
    idle: "",
};

export function LogBar({ target, expanded, onToggle, onClose }) {
    const { lines, state, error } = useLogs(target && target.id);
    const body = useRef(null);
    const stick = useRef(true);

    useEffect(() => {
        const node = body.current;
        if (!node || !expanded || !stick.current) return;
        node.scrollTop = node.scrollHeight;
    }, [lines, expanded]);

    if (!target) return null;

    const onScroll = () => {
        const node = body.current;
        if (!node) return;
        stick.current = node.scrollHeight - node.scrollTop - node.clientHeight < 40;
    };

    return html`
        <div class="logbar on ${expanded ? "big" : ""}">
            <div class="loghead" onClick=${onToggle}>
                <span class="pulse ${state}"></span>
                <span class="logname">
                    ${target.name}
                    <small>${STATE_WORD[state]}${state === "live" ? ` · ${lines.length} lines` : ""}</small>
                </span>
                <button class="kebab" type="button" aria-label=${expanded ? "collapse" : "expand"}
                        onClick=${(e) => { e.stopPropagation(); onToggle(); }}>
                    <span class="chev ${expanded ? "down" : "up"}">${Icon.chevron()}</span>
                </button>
                <button class="kebab" type="button" aria-label="close"
                        onClick=${(e) => { e.stopPropagation(); onClose(); }}>${Icon.close()}</button>
            </div>

            <div class="logbody" ref=${body} onScroll=${onScroll}>
                ${error && html`<div class="ln er">${error}</div>`}
                ${lines.length === 0 && !error && html`<div class="ln dim">${STATE_WORD[state]}…</div>`}
                ${lines.map((entry) => html`
                    <div class="ln ${entry.stream === "stderr" ? "er" : ""}" key=${entry.key}>${entry.line}</div>
                `)}
            </div>
        </div>
    `;
}
