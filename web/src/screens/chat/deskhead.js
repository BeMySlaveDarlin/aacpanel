// The conversation header on the wide screen: who the session is on the left,
// in two lines — the dot and the name, and under them how it stands, how full
// it is and where it works — and on the right, in one line, what it is looked
// at with and the one control for the session itself. The left gives way from
// its end of least use: the path loses its head first, the name its middle
// next; the tools never shrink.

import { html } from "../../html.js";
import { ContextBar } from "../../ui/bar.js";
import { fill, share } from "../../format.js";
import { useAsOf } from "../../ui/asof.js";
import { MidName, shortPath, stateOf } from "./head.js";
import { Icon } from "../../ui/icons.js";
import { CODEX_NOTE, isCodex } from "../../agent.js";

// DeskHead renders the conversation header on the wide screen; the tools come
// ready from the conversation.
export function DeskHead({ name, live, archive, pct, move, tools }) {
    const state = stateOf(live, useAsOf(), move);
    const cwd = ((live || archive || {}).cwd) || "";
    return html`
        <div class="dkhead">
            <div class="dkheadtop">
                <div class="dkwho">
                    <div class="dkwhoname">
                        <span class=${`dkdot ${state.tone ? `dk${state.tone}` : ""}`.trim()} title=${state.say}></span>
                        <span class="dkheadname dkchatname"><${MidName} text=${name} /></span>
                        ${isCodex(live) && html`<span class="dkmark" data-tip=${CODEX_NOTE}>${Icon.braces()}codex</span>`}
                    </div>
                    <div class="dkwhosub">
                        ${state.word && html`
                            <span class="dkword" data-tone=${state.tone}>${state.word}</span>
                            <span class="dkheadsep">·</span>
                        `}
                        ${pct != null && html`
                            <span class="dkpct" data-fill=${live ? fill(pct) : "peak"}>${share(pct)}${live ? "" : " peak"}</span>
                            <span class="dkheadsep">·</span>
                        `}
                        <span class="dkheadpath" title=${cwd || undefined}>
                            <bdi>${cwd ? shortPath(cwd) : "the conversation directory is unknown"}</bdi>
                        </span>
                    </div>
                </div>
                ${tools}
            </div>
            <${ContextBar} pct=${pct} peak=${!live} />
        </div>
    `;
}
