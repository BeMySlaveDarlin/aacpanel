// The rows of a codex thread that claude has no word for: the plan codex
// wrote in plan mode, a review begun and ended, a goal set or changed, and an
// agent started. They are cards of the build the feed draws what arrives
// with — a head that says what it is and when, and the plate under it.

import { useState } from "preact/hooks";

import { html } from "../../html.js";
import { Icon } from "../../ui/icons.js";
import { dedent, inline, render } from "../../md.js";
import { stopwatch, tokens } from "../../format.js";
import { sizeOf, stampText } from "./labels.js";
import { GOAL_WORDS } from "./codexthread.js";

// PlanCard is the plan codex wrote: read whole, since in plan mode it is what
// the turn was for.
export function PlanCard({ item }) {
    return html`
        <div class="sent xrplan">
            <div class="senthead">
                <span class="sentico">${Icon.plan()}</span>
                <span class="sentlabel">plan</span>
                ${item.at && html`<span class="sentat">${stampText(item.at)}</span>`}
            </div>
            <div class="sentcap">${render(item.text)}</div>
            ${item.cut && html`<p class="hint warn">The plan is longer than shown — cut.</p>`}
        </div>
    `;
}

// The tone of a verdict: codex words it as a sentence, and only the two it
// gives a patch are told apart.
const VERDICT_TONES = { "patch is correct": "ok", "patch is incorrect": "crit" };

// The tone of a finding by its priority: 0 and 1 are what breaks, 2 what
// should be fixed, 3 a remark.
const PRIORITY_TONES = ["crit", "crit", "warn", "faint"];

// sure says how sure codex is, a share of one, in percent.
function sure(score) {
    return typeof score === "number" ? `${Math.round(score * 100)}% sure` : "";
}

// ReviewCard is a review of codex. Its start is a line — what codex looks
// at; its end is the card of the verdict with the explanation, and a row a
// finding under it. The answer codex writes after a review says the same and
// is folded into the card (see rows in feed.js).
export function ReviewCard({ item }) {
    if (item.state === "start") {
        return html`
            <div class="mside s-info xrstart">
                <span class="msidedot" aria-hidden="true"></span>
                <span class="msidetext"><b class="msidefrom">Review</b> ${item.text || "codex looks at the code"}</span>
            </div>
        `;
    }
    const findings = item.findings || [];
    const tone = VERDICT_TONES[item.verdict] || "faint";
    return html`
        <div class=${`sent xrreview v-${tone}`}>
            <div class="senthead">
                <span class="sentico">${Icon.search()}</span>
                <span class="sentlabel">review</span>
                ${item.at && html`<span class="sentat">${stampText(item.at)}</span>`}
            </div>
            <div class="xrverdict">
                <b>${item.verdict || "no verdict"}</b>
                ${sure(item.confidence) && html`<span>${sure(item.confidence)}</span>`}
            </div>
            ${item.text && html`<div class="sentcap">${render(item.text)}</div>`}
            ${item.cut && html`<p class="hint warn">The explanation is longer than shown — cut.</p>`}
            ${findings.length > 0 && html`
                <div class="mflist">
                    ${findings.map((f, n) => html`<${Finding} key=${n} f=${f} />`)}
                </div>
            `}
        </div>
    `;
}

// where names the lines of a finding: a line, or the first and the last.
function where(f) {
    const name = String(f.path || "").split("/").pop();
    const [from, to] = f.lines || [];
    if (!name) return "";
    if (!from) return name;
    return to && to !== from ? `${name}:${from}–${to}` : `${name}:${from}`;
}

// Finding is a row of a review: the priority as its tag, the title, and the
// place in the code; the body under it opens on a tap. Codex puts the
// priority into the title as well, and the tag already says it; it marks code
// in both with backticks, as in an answer.
function Finding({ f }) {
    const [open, setOpen] = useState(false);
    const known = typeof f.priority === "number";
    const title = known ? String(f.title || "").replace(/^\[P\d\]\s*/, "") : f.title;
    const tone = known ? PRIORITY_TONES[Math.min(f.priority, 3)] : "faint";
    const at = where(f);
    return html`
        <button class=${`mfile askedrow xrfind s-${tone}${open ? " open" : ""}`} type="button"
                aria-expanded=${open ? "true" : "false"} onClick=${() => setOpen(!open)}>
            <span class="mftag">${known ? `P${f.priority}` : "note"}</span>
            <span class="askeda">${inline(title)}</span>
            ${at && html`<code class="askedsubj" title=${f.path}>${at}</code>`}
            ${f.body && html`<span class="xrbody">${inline(f.body)}</span>`}
        </button>
    `;
}

// The tone of a goal by how it stands: at work, met, or stopped by something.
const GOAL_TONES = {
    active: "info", complete: "ok", paused: "faint", blocked: "warn", usageLimited: "warn", budgetLimited: "warn",
};

// GoalCard is a goal set or changed: what codex works towards, how it stands,
// and what it has spent — of its budget, when it has one — and how long.
export function GoalCard({ item }) {
    const spent = item.tokensUsed || 0;
    const budget = item.tokenBudget || 0;
    const tokensLine = budget ? `${tokens(spent)} of ${tokens(budget)} tokens` : `${tokens(spent)} tokens`;
    const share = budget ? Math.min(100, Math.round((spent / budget) * 100)) : 0;
    return html`
        <div class=${`sent xrgoal g-${GOAL_TONES[item.status] || "faint"}`}>
            <div class="senthead">
                <span class="sentico">${Icon.pin()}</span>
                <span class="sentlabel">goal</span>
                ${item.status && html`<span class="xrgoalword">${GOAL_WORDS[item.status] || item.status}</span>`}
                ${item.at && html`<span class="sentat">${stampText(item.at)}</span>`}
            </div>
            <div class="pnmtitle">${item.text}</div>
            ${item.cut && html`<p class="hint warn">The goal is longer than shown — cut.</p>`}
            <div class="pnmmeta">
                <b>${tokensLine}</b>
                ${item.timeUsedSeconds > 0 && html`<span>${stopwatch(item.timeUsedSeconds)}</span>`}
            </div>
            ${budget > 0 && html`<div class="pnmbar" aria-hidden="true"><i style=${`width: ${share}%`}></i></div>`}
        </div>
    `;
}

// How codex says an agent stands: the word of its row, the mark of its tag
// and the tone of the tag. An agent at work keeps the accent of the tag.
const AGENT_STATES = {
    pending_init: ["starting", "•", ""],
    running: ["at work", "•", ""],
    interrupted: ["interrupted", "–", "faint"],
    completed: ["finished", "✓", "ok"],
    errored: ["failed", "✗", "crit"],
    shutdown: ["closed", "–", "faint"],
    not_found: ["gone", "–", "faint"],
};

// whoOf names an agent codex started: by the nickname codex gave it, or by
// its role.
function whoOf(agent) {
    return agent.name || agent.role || "agent";
}

// SpawnCard is an agent codex started, a card of the build of the tasks done:
// the head says an agent started and when; its row names it, its role, the
// model it runs on and how it stands as the last call to it said, and opens
// the agent's own thread; the task it was given lies folded under it, as the
// call of an agent of claude keeps its prompt inside.
export function SpawnCard({ item, onAgent }) {
    const [open, setOpen] = useState(false);
    const agents = item.spawned || [];
    const failed = item.status === "failed";
    const task = dedent(item.text || "");
    const head = failed ? "agent did not start"
        : agents.length > 1 ? `${agents.length} agents started` : "agent started";
    return html`
        <div class=${`sent mtasks xrspawn${failed ? " failed" : ""}`}>
            <div class="senthead">
                <span class="sentico">${Icon.robot()}</span>
                <span class="sentlabel">${head}</span>
                ${item.at && html`<span class="sentat">${stampText(item.at)}</span>`}
            </div>
            <div class="mflist">
                ${agents.map((agent) => html`<${SpawnedRow} key=${agent.id} agent=${agent} onAgent=${onAgent} />`)}
                ${task && html`
                    <button class="mfile mlettermore xrtaskmore" type="button" onClick=${() => setOpen(!open)}
                            aria-expanded=${open ? "true" : "false"}>
                        <span class="mfico">${open ? Icon.close() : Icon.list()}</span>
                        <span class="mfname">${open ? "Fold the task" : "The task"}</span>
                        <span class="mfsize">${sizeOf(task)}</span>
                    </button>
                `}
            </div>
            ${open && html`<div class="sentcap xrtask">${render(task)}</div>`}
            ${open && item.cut && html`<p class="hint warn">The task is longer than shown — cut.</p>`}
        </div>
    `;
}

// SpawnedRow is one agent of a start. It opens the agent's thread by its id,
// the way a run of codex exec opens among the agents of claude; without a
// way to open it, it only says.
function SpawnedRow({ agent, onAgent }) {
    const [word, mark, tone] = AGENT_STATES[agent.state] || ["", "•", ""];
    const runs = [agent.model, agent.effort].filter(Boolean).join(" ");
    const about = [agent.name && agent.role, runs, word].filter(Boolean).join(" · ");
    const who = whoOf(agent);
    const body = html`
        <span class="mftag">${mark}</span>
        <span class="mfname">${who}${about && html`<span class="mfnote">${about}</span>`}</span>
    `;
    const cls = `mfile tagged mtask${tone ? ` s-${tone}` : ""}`;
    if (!onAgent) return html`<div class=${cls} role="note" aria-label=${about ? `${who}, ${about}` : who}>${body}</div>`;
    return html`
        <button class=${cls} type="button" aria-label=${`${who} — open the thread of the agent`}
                onClick=${() => onAgent({ id: agent.id, name: who, kind: "subagent", agent: "codex",
                                          model: agent.model, text: agent.name ? agent.role : "" })}>
            ${body}<span class="crgo">${Icon.chevron()}</span>
        </button>
    `;
}
