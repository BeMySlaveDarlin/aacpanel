// The conversation header on the wide screen: one line of who the session is
// and how it stands, then what it is looked at with and the one control for
// the session itself. The line gives way from its end of least use: the path
// first, then the middle of the name; the tools never shrink.

import { html } from "../../html.js";
import { ContextBar } from "../../ui/bar.js";
import { fill, share } from "../../format.js";
import { useAsOf } from "../../ui/asof.js";
import { MidName, shortPath, stateOf } from "./head.js";

// DeskHead renders the conversation header on the wide screen; the tools come
// ready from the conversation.
export function DeskHead({ name, live, archive, pct, tools }) {
    const state = stateOf(live, useAsOf());
    const cwd = ((live || archive || {}).cwd) || "";
    return html`
        <div class="dkhead">
            <div class="dkheadtop">
                <span class=${`dkdot ${state.tone ? `dk${state.tone}` : ""}`.trim()} title=${state.say}></span>
                <span class="dkheadname dkchatname"><${MidName} text=${name} /></span>
                ${state.word && html`<span class="dkword" data-tone=${state.tone}>${state.word}</span>`}
                ${pct != null && html`
                    <span class="dkpct" data-fill=${live ? fill(pct) : "peak"}>${share(pct)}${live ? "" : " peak"}</span>
                `}
                <span class="dkheadpath" title=${cwd || undefined}>
                    <bdi>${cwd ? shortPath(cwd) : "the conversation directory is unknown"}</bdi>
                </span>
                ${tools}
            </div>
            <${ContextBar} pct=${pct} peak=${!live} />
        </div>
    `;
}
