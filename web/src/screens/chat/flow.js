// One workflow run: the script, its phases and what its agents cost.
//
// A run says different things at different times. While it goes, the harness
// writes nothing about it but the transcripts of its agents, so what there is
// to show is the phases the script declared, how many agents have started and
// how long it has been going. When it is over a snapshot appears beside the
// transcript, and with it the log of the agents that stalled and were retried,
// and whatever the script returned.

import { html } from "../../html.js";
import { Icon } from "../../ui/icons.js";
import { BackHead, useBackClose } from "../../ui/back.js";
import { since, tokens } from "../../format.js";

// The word for a status, and the tone the row is drawn in.
const WORDS = {
    running: ["running", "live"],
    completed: ["completed", "done"],
    failed: ["failed", "bad"],
    killed: ["killed", "bad"],
    cancelled: ["cancelled", "gone"],
    stopped: ["stopped", "gone"],
};

export function flowWord(flow) {
    return (WORDS[flow.status] || [flow.status || "running", "live"])[0];
}

export function flowTone(flow) {
    return (WORDS[flow.status] || ["", "live"])[1];
}

// A run of forty minutes is read in minutes, not in milliseconds.
export function took(ms) {
    if (!ms) return "";
    const s = Math.round(ms / 1000);
    if (s < 60) return `${s} s`;
    const m = Math.round(s / 60);
    if (m < 60) return `${m} min`;
    const h = Math.floor(m / 60);
    return `${h} h ${m % 60} min`;
}

export function flowTitle(flow) {
    return flow.name || flow.text || flow.id;
}

// The counters of a run: only the ones it has an answer for. A zero here is a
// number where the eye expects a fact — a run that started no agent yet has
// nothing to say about agents.
function Counts({ flow }) {
    const items = [
        flow.agents > 0 && [flow.agents, flow.agents === 1 ? "agent" : "agents"],
        flow.tokens > 0 && [tokens(flow.tokens), "read"],
        flow.calls > 0 && [flow.calls, flow.calls === 1 ? "tool call" : "tool calls"],
        flow.ms > 0 && [took(flow.ms), "long"],
    ].filter(Boolean);
    if (!items.length) return null;
    return html`
        <div class="wfnums">
            ${items.map(([value, label], i) => html`
                <div key=${i}><span class="wfnum">${value}</span><span class="wfnumlabel">${label}</span></div>
            `)}
        </div>
    `;
}

export function FlowRun({ flow, onBack }) {
    // The run is a layer over the list, and the gesture that puts a layer down
    // has to be claimed by it: uncovered, the swipe takes the whole panel off.
    useBackClose(Boolean(onBack), onBack);

    const phases = flow.phases || [];
    const logs = flow.logs || [];
    const live = flow.status === "running";
    return html`
        <${BackHead} onBack=${onBack} label="to the runs">
            <div class="chatwho">
                <h2>${flowTitle(flow)}</h2>
                <div class="chatsub">
                    <span class=${`wfstate ${flowTone(flow)}`}>${flowWord(flow)}</span>
                    ${flow.at && html`<span class="sep">·</span><span>started ${since(flow.at)}</span>`}
                </div>
            </div>
        <//>

        <div class="callbody wfbody">
            ${flow.text && flow.text !== flowTitle(flow) && html`<p class="wfsum">${flow.text}</p>`}
            <${Counts} flow=${flow} />

            ${phases.length > 0 && html`
                <div class="wfblock">
                    <div class="wfhead">phases</div>
                    <ol class="wfphases">
                        ${phases.map((phase, i) => html`
                            <li key=${i}>
                                <span class="wfphn">${i + 1}</span>
                                <span>
                                    <span class="wfphtitle">${phase.title}</span>
                                    ${phase.detail && html`<span class="wfphdetail">${phase.detail}</span>`}
                                </span>
                            </li>
                        `)}
                    </ol>
                    ${live && html`<p class="whint">Which phase it is in now, the run does not say
                        until it is over: it writes its record at the end.</p>`}
                </div>
            `}

            ${logs.length > 0 && html`
                <div class="wfblock">
                    <div class="wfhead">what it said along the way</div>
                    <ul class="wflogs">
                        ${logs.map((line, i) => html`<li key=${i}>${line}</li>`)}
                    </ul>
                </div>
            `}

            ${flow.result && html`
                <div class="wfblock">
                    <div class="wfhead">what it returned</div>
                    <pre class="callpre wfresult">${flow.result}</pre>
                </div>
            `}

            ${flow.script && html`
                <div class="wfblock">
                    <div class="wfhead">script</div>
                    <p class="wfpath">${flow.script}</p>
                </div>
            `}

            ${live && phases.length === 0 && html`
                <p class="hint">${flow.agents > 0
                    ? `${flow.agents} agents started so far. The run writes its record when it ends.`
                    : "The run has started no agent yet."}</p>
            `}
        </div>
    `;
}

// FlowRow is one run in the list: what it is, where it stands, what it cost.
export function FlowRow({ flow, onOpen }) {
    return html`
        <button class="wrow wflow" type="button" onClick=${() => onOpen(flow)}>
            <span class="wicon">${Icon.flow()}</span>
            <span class="wtext">
                ${flowTitle(flow)}
                <span class=${`wkind ${flowTone(flow)}`}>${flowWord(flow)}</span>
                ${flow.agents > 0 && html`<span class="wkind">${flow.agents} agents</span>`}
                ${flow.ms > 0 && html`<span class="wkind">${took(flow.ms)}</span>`}
                ${flow.text && flow.text !== flowTitle(flow)
                    && html`<span class="wflowsum">${flow.text}</span>`}
            </span>
            ${flow.at && html`<span class="wage">${since(flow.at)}</span>`}
            <span class="crgo">${Icon.chevron()}</span>
        </button>
    `;
}
