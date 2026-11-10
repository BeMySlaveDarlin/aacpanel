// Bars along the card edge: context fill for sessions, stack makeup for containers.
import { html } from "../html.js";
import { fill } from "../format.js";

// ContextBar shows how much of the context has been lived through, and where
// the cap of the session stands, as its card in the list does: the track past
// the cap is hatched and a tick marks the cap, in the accent when the session
// restarts there by itself. The used fill stays above the hatching when a
// session passes its cap; the tick stays above the fill.
export function ContextBar({ pct, edge, peak, cap, autoRestart }) {
    if (pct == null) return null;
    const capped = cap != null && cap > 0 && cap < 100;
    return html`
        <div class=${`ctxbar ${edge ? "edge" : "head"} ${peak ? "peak" : fill(pct)}`} role="presentation">
            ${capped && html`<span class="ctxburn" style=${`left:${cap}%`} aria-hidden="true"></span>`}
            <i style=${`width:${Math.max(0, Math.min(pct, 100))}%`}></i>
            ${capped && html`<b class=${`ctxcap${autoRestart ? " auto" : ""}`} style=${`left:${cap}%`} aria-hidden="true"></b>`}
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
