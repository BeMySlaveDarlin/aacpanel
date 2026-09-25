// The conversation header on the wide screen.

import { html } from "../../html.js";
import { ContextBar } from "../../ui/bar.js";
import { Icon } from "../../ui/icons.js";
import { ago, plural, share, since, tokens } from "../../format.js";
import { Marquee, modelName } from "./head.js";
import { waitText } from "../../ui/waits.js";

function dot(live) {
    if (!live) return { kind: "dkoff", say: "the conversation is gone" };
    if (live.ask || live.waitingFor || live.status === "waiting") {
        return { kind: "dkwaiting", say: live.ask ? "waiting for an answer to a question" : waitText(live.waitingFor) };
    }
    if (live.compacting) return { kind: "dkbusy", say: "compacting the conversation" };
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
    const said = live.messages || 0;
    const empty = !live.tokens;
    return html`
        ${ctxFact(live, pct, "of the context")}
        ${live.tokensIn > 0 && html`
            <span class="dkfact"><b>${tokens(live.tokensIn)}</b><span>in</span></span>
            <span class="dkfact"><b>${tokens(live.tokensOut)}</b><span>out</span></span>
        `}
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

// DeskHead renders the conversation header on the wide screen; the tools of a
// live session come ready from the conversation, the same as on a phone.
export function DeskHead({ name, live, archive, pct, tools, onRepo }) {
    const state = dot(live);
    const cwd = ((live || archive || {}).cwd) || "";
    return html`
        <div class="dkhead">
            <div class="dkheadtop">
                <span class=${`dkdot ${state.kind}`.trim()} title=${state.say}></span>
                <span class="dkheadname dkchatname"><${Marquee} text=${name} /></span>
                <span class="dkheadpath" title=${cwd}>${cwd || "the conversation directory is unknown"}</span>
                ${onRepo && html`
                    <button class="viewbtn solo" type="button" title="the files of this project"
                            aria-label="the files of this project" onClick=${onRepo}>${Icon.files()}</button>
                `}
                ${live && tools}
            </div>
            <div class="dkheadbot">
                ${live ? liveFacts(live, pct) : pastFacts(archive, pct)}
            </div>
            <${ContextBar} pct=${pct} peak=${!live} />
        </div>
    `;
}

// ViewToggle renders what to watch a live session with: the terminal or the
// feed. Where the pair also moves the session between the console and the
// feed, the other button says so, or why it cannot move it now.
export function ViewToggle({ view, onView, tip = "", why = "" }) {
    const one = (id, label, icon) => {
        const other = id !== view;
        const off = other && Boolean(why);
        const say = other && tip ? tip : label;
        return html`
            <button class=${`viewbtn${other ? "" : " on"}`} type="button"
                    aria-label=${off ? why : say} aria-pressed=${!other}
                    data-tip=${other && tip && !off ? tip : undefined}
                    title=${off ? why : undefined} disabled=${off}
                    onClick=${() => onView(id)}><${icon} /></button>
        `;
    };
    return html`
        <span class="viewsw" role="group" aria-label="how to watch the session">
            ${one("term", "terminal", Icon.terminal)}
            ${one("feed", "feed", Icon.feed)}
        </span>
    `;
}
