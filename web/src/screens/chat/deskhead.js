// The conversation header on the wide screen.

import { html } from "../../html.js";
import { ContextBar } from "../../ui/bar.js";
import { Icon } from "../../ui/icons.js";
import { ago, plural, share, since, tokens } from "../../format.js";
import { Marquee, modelName } from "./head.js";
import { modeInfo } from "./tools.js";
import { waitText } from "../../ui/waits.js";
import { WindowToggle } from "./window.js";

function dot(live) {
    if (!live) return { kind: "dkoff", say: "the conversation is gone" };
    if (live.ask || live.waitingFor || live.status === "waiting") {
        return { kind: "dkwaiting", say: live.ask ? "waiting for an answer to a question" : waitText(live.waitingFor) };
    }
    if (live.status === "busy") return { kind: "dkbusy", say: "handling the request" };
    if (!live.status) return { kind: "", say: "the session state is unknown" };
    return { kind: "dkidle", say: "waiting for a message" };
}

function ctxFact(row, pct, say) {
    const iffy = row && row.stale;
    return html`
        <span class="dkfact">
            <b>${pct == null ? "—" : share(pct)}</b>
            <span class=${iffy ? "dkchatguess" : ""}>${iffy ? `${say} before the compaction` : say}</span>
        </span>
    `;
}

function limitSay(row) {
    if (!row.limit) return "tokens";
    return row.limitKnown ? `of ${tokens(row.limit)}` : `of ${tokens(row.limit)} · limit guessed`;
}

function liveFacts(live, pct) {
    const mode = modeInfo(live);
    const said = live.messages || 0;
    const empty = !live.tokens;
    return html`
        ${ctxFact(live, pct, "of the context")}
        <span class="dkfact"><b>${modelName(live) || "model"}</b><span>${live.effort || "effort"}</span></span>
        <span class=${`dkfact${mode.danger ? " dkchatloud" : ""}`} title=${mode.title}>
            <b>${mode.label}</b><span>mode</span>
        </span>
        <span class="dkfact"><b>${said}</b><span>${plural(said, "message", "messages")}</span></span>
        ${!empty && html`<span class="dkfact"><b>${since(live.lastRequestAt)}</b><span>since the request</span></span>`}
        <span class="dkchatgrow"></span>
        ${empty
            ? html`<span class="dkfact right"><b>—</b><span>no requests yet</span></span>`
            : html`
                <span class="dkfact right">
                    <b>${tokens(live.tokens)}</b>
                    <span class=${live.limitKnown ? "" : "dkchatguess"}>${limitSay(live)}</span>
                </span>
            `}
    `;
}

function pastFacts(row, pct) {
    if (!row) {
        return html`<span class="dkfact"><b>the conversation is closed</b><span>the session is gone</span></span>`;
    }
    const said = row.messages || 0;
    return html`
        ${ctxFact(row, pct, "context peak")}
        <span class="dkfact"><b>${modelName(row) || "model"}</b><span>${row.effort || "effort"}</span></span>
        <span class="dkfact"><b>${said}</b><span>${plural(said, "message", "messages")}</span></span>
        ${row.lastAt && html`<span class="dkfact"><b>${ago(row.lastAt)}</b><span>last record</span></span>`}
        <span class="dkchatgrow"></span>
        ${row.tokensMax > 0 && html`
            <span class="dkfact right">
                <b>${tokens(row.tokensMax)}</b>
                <span class=${row.limitKnown ? "" : "dkchatguess"}>${limitSay(row)}</span>
            </span>
        `}
    `;
}

// DeskHead renders the conversation header on the wide screen.
export function DeskHead({ name, live, archive, pct, view, canTerm, exec, onView, onRepo }) {
    const state = dot(live);
    const cwd = ((live || archive || {}).cwd) || "";
    return html`
        <div class="dkhead">
            <div class="dkheadtop">
                <span class=${`dkdot ${state.kind}`.trim()} title=${state.say}></span>
                <span class="dkheadname dkchatname"><${Marquee} text=${name} /></span>
                <span class="dkheadpath" title=${cwd}>${cwd || "the conversation directory is unknown"}</span>
                ${onRepo && html`
                    <button class="viewbtn" type="button" title="the repository of this project"
                            aria-label="repository" onClick=${onRepo}>${Icon.code()}</button>
                `}
                ${canTerm && html`<${ViewToggle} view=${view} onView=${onView} />`}
                ${live && html`<${WindowToggle} name=${name} exec=${exec} />`}
            </div>
            <div class="dkheadbot">
                ${live ? liveFacts(live, pct) : pastFacts(archive, pct)}
            </div>
            <${ContextBar} pct=${pct} peak=${!live} />
        </div>
    `;
}

// ViewToggle renders what to watch a live session with: the terminal or the feed.
export function ViewToggle({ view, onView }) {
    return html`
        <span class="viewsw" role="group" aria-label="how to watch the session">
            <button class=${`viewbtn${view === "term" ? " on" : ""}`} type="button" aria-label="Terminal"
                    aria-label="terminal" aria-pressed=${view === "term"}
                    onClick=${() => onView("term")}><${Icon.terminal} /></button>
            <button class=${`viewbtn${view === "feed" ? " on" : ""}`} type="button" aria-label="Feed"
                    aria-label="feed" aria-pressed=${view === "feed"}
                    onClick=${() => onView("feed")}><${Icon.feed} /></button>
        </span>
    `;
}
