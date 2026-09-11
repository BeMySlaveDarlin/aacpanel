// Bars along the card edge: context fill for sessions, stack makeup for containers.
import { html } from "../html.js";
import { fill } from "../format.js";

// ContextBar shows how much of the context has been lived through.
export function ContextBar({ pct, edge, peak }) {
    if (pct == null) return null;
    return html`
        <div class=${`ctxbar ${edge ? "edge" : "head"} ${peak ? "peak" : fill(pct)}`} role="presentation">
            <i style=${`width:${Math.max(0, Math.min(pct, 100))}%`}></i>
        </div>
    `;
}

// SegBar shows how many containers of a stack are up.
export function SegBar({ total, done, kind }) {
    if (!total) return null;
    const pct = Math.max(0, Math.min(1, done / total)) * 100;
    return html`
        <div
            class=${`segbar ${kind}`}
            style=${`--seg:${total}`}
            data-one=${total === 1 ? "1" : "0"}
            role="presentation"
        >
            <i style=${`width:${pct}%`}></i>
        </div>
    `;
}
