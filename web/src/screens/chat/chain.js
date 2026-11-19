import { useRef, useState } from "preact/hooks";

import { html } from "../../html.js";
import { ago, duration } from "../../format.js";
import { autolink, render } from "../../md.js";
import { Icon } from "../../ui/icons.js";
import { clock, useTick } from "./now.js";

export const isChain = (act) => Array.isArray(act?.steps || act?.body?.steps);
export const actOpen = (act) => act.state === "open" || act.state === "failed";

export function chainSummary(act) {
    const steps = act.steps || act.body.steps;
    const step = act.step || 1;
    const done = (act.outputs || []).filter((out) => out?.state === "done").length;
    if (act.state === "failed") return `failed at step ${step} · below`;
    if (!actOpen(act)) return `${done} of ${steps.length} done${act.state === "withdrawn" ? " · withdrawn" : act.state === "declined" ? " · Can't" : ""}`;
    const out = act.outputs?.[step - 1];
    const word = out?.state === "running" ? `step ${step} runs`
        : `step ${step} is ${steps[step - 1]?.who === "agent" ? "the agent's" : "yours"}`;
    return `${done ? `${done} of ${steps.length} done` : `${steps.length} steps`} · ${word} · below`;
}

const seconds = (out) => {
    const from = Date.parse(out?.started), to = out?.ended ? Date.parse(out.ended) : Date.now();
    return Number.isFinite(from) && Number.isFinite(to) ? Math.max(0, (to - from) / 1000) : 0;
};
const resultOf = (out) => out?.exit != null ? `exit ${out.exit} · ${duration(seconds(out))}`
    : out?.state === "cant" ? "Can't" : out?.state === "stopped" ? "stopped" : out?.state === "done" ? "done" : "";

export function ChainCard({ item, record, onAnswer, onChainRun, onChainStop, onTerm, unavailable = "", error = "" }) {
    useTick();
    const act = record || item;
    const steps = act.steps || act.body.steps;
    const open = record && actOpen(record);
    return html`<article class=${`sent mrequest mchain${open && act.blocking ? " blocking" : ""}`}>
        <div class="senthead"><span class="sentico">${Icon.list()}</span>
            <span class="sentlabel">chain · ${record && !open ? chainSummary(act) : `step ${act.step || 1} of ${steps.length}`}</span>
            <span class="sentat">${ago(act.created || item.at)}</span></div>
        <h3 class="requesttitle">${act.title}</h3>
        <div class="chainsteps">${steps.map((step, i) => html`<${ChainStep} key=${`${act.id}:${i}`}
            step=${step} number=${i + 1} current=${open && i + 1 === act.step} act=${act}
            output=${act.outputs?.[i]} onAnswer=${onAnswer} onRun=${onChainRun} onStop=${onChainStop} onTerm=${onTerm} />`)}</div>
        ${error && html`<p class="requeststate crit" role="status">${error}</p>`}
        ${!record && !error && html`<p class="requeststate" role="status">Reading request state…</p>`}
        ${open && !onAnswer && html`<p class="requeststate" role="status">${unavailable}</p>`}
    </article>`;
}

// Every step stands open: what it asked, its command, its output and note are
// read in place, in an open chain and in a finished one alike.
function ChainStep({ step, number, current, act, output, onAnswer, onRun, onStop, onTerm }) {
    const running = output?.state === "running";
    const state = output?.state || (current ? "current" : "pending");
    const terminal = output?.term && actOpen(act) && onTerm
        ? html`<button class="chainterm" type="button" onClick=${() => onTerm(output.term)}>terminal ${output.term}</button>` : null;
    const canAnswer = current && act.state === "open" && step.who === "you";
    const content = html`<div class="chainbody">
        ${step.command && html`<div class="chaincommand">
            <div class="mdcode"><pre><code>${step.command}</code></pre><button class="mdcopy" type="button" title="Copy the code" aria-label="Copy the code">${Icon.copy()}</button></div>
        </div>`}
        ${output?.tail && html`<pre class="chainout" tabIndex="0" aria-label=${`Output of step ${number}`}>${autolink(output.tail)}</pre>`}
        ${(running || resultOf(output) || terminal) && html`<div class=${`chainstatus ${state}`}>
            <span>${running ? `running · ${clock(seconds(output))}` : resultOf(output)}</span>${terminal}
            ${canAnswer && running && onStop && html`<${ChainButton} word="Stop" action=${() => onStop(act.id, number)} />`}
        </div>`}
        ${actOpen(act) && step.who === "agent" && !output?.note && (!output || output.state === "pending" || output.state === "running")
            && html`<p class="requeststate">${current ? "the session works on it" : "waits for the session"}</p>`}
        ${output?.note && html`<div class="chainnote">${output.note}</div>`}
        ${current && act.state === "failed" && html`<p class="requeststate">The session is reviewing the failed step.</p>`}
        ${canAnswer && onAnswer && !running && html`<${StepAnswer} key=${number} act=${act.id} step=${number} command=${Boolean(step.command)}
            onAnswer=${onAnswer} run=${onRun ? () => onRun(act.id, number) : null} />`}
    </div>`;
    return html`<section class=${`chainstep ${state}${current ? " current" : ""}`} data-step=${number}>
        <div class="chainhead">
            <span class=${`chainicon${running ? " spin" : ""}`} role="img" aria-label=${`Step ${number}: ${state}`}>
                ${running ? "" : state === "done" ? "✓" : state === "failed" ? "!" : state === "cant" ? "×" : current ? "●" : "○"}</span>
            <div class="chainlead"><span class="chainwho">${number} · ${step.who}</span><div class="msg chaintext">${render(step.text)}</div></div>
        </div>
        ${content}
    </section>`;
}

function ChainButton({ word, action, primary = false, disabled = false }) {
    const sending = useRef(false);
    const [busy, setBusy] = useState(false);
    const press = async () => {
        if (sending.current) return;
        sending.current = true;
        setBusy(true);
        try { await action(); } finally { sending.current = false; setBusy(false); }
    };
    return html`<button class=${`btn${primary ? " primary" : ""}`} type="button" disabled=${disabled || busy} onClick=${press}>${word}</button>`;
}

function StepAnswer({ act, step, command, onAnswer, run }) {
    const [note, setNote] = useState("");
    const [writing, setWriting] = useState(false);
    const [cant, setCant] = useState(false);
    const [busy, setBusy] = useState(false);
    const sending = useRef(false);
    const answer = async (value) => {
        if (sending.current) return;
        sending.current = true;
        setBusy(true);
        try { await onAnswer(act, value, note, step); } finally { sending.current = false; setBusy(false); }
    };
    return html`
        ${(writing || cant) && html`<label class="requestnote">${cant ? "Reason (optional)" : "Note (optional)"}<textarea rows="2" maxLength="2000"
            value=${note} disabled=${busy} onInput=${(event) => setNote(event.currentTarget.value)}></textarea></label>`}
        <div class="actfoot requestanswer">
            ${!command && !writing && !cant && html`<button class="actnote" type="button" onClick=${() => setWriting(true)}>note</button>`}
            <button class="btn" type="button" disabled=${busy} onClick=${() => cant ? answer("cant") : setCant(true)}>Can't</button>
            ${command ? run && html`<${ChainButton} word="Run" primary disabled=${busy} action=${run} />`
                : html`<button class="btn primary" type="button" disabled=${busy} onClick=${() => answer("done")}>${busy ? "Sending…" : "Done"}</button>`}
        </div>`;
}
