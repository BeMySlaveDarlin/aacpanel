// The calls sheet: the run of one badge as a list, and one call in full.

import { useEffect, useState } from "preact/hooks";

import { html } from "../../html.js";
import { BackHead, useBackClose } from "../../ui/back.js";
import { FileCard, fileOfCall } from "./files.js";
import { Icon } from "../../ui/icons.js";
import { idParam } from "./api.js";
import { countCalls, kindIcon, shortTokens, tokenWord } from "./labels.js";

// Calls renders the page listing the calls of one badge.
export function Calls({ session, id, calls, onFile }) {
    const [pick, setPick] = useState(null);
    const real = calls.filter((call) => !call.still);
    const place = (n) => real.indexOf(calls[n]) + 1;

    if (pick != null) {
        return html`<${CallView}
            session=${session}
            id=${id}
            call=${calls[pick]}
            place=${`${place(pick)} of ${real.length}`}
            onBack=${() => setPick(null)}
            onFile=${onFile}
        />`;
    }

    return html`
        <div class="sheethead">
            <div class="chatwho">
                <h2>Calls</h2>
                <div class="chatsub"><span>${countCalls(real.length)}</span></div>
            </div>
        </div>

        <div class="calltime">
            ${calls.map((call, n) => (call.still ? html`
                <div class="callnode still" key=${`think-${call.pos}-${call.seq}`}>
                    <span class="cnmark think">${Icon.thinking()}</span>
                    <span class="cnbody">
                        <span class="cnname">thinking</span>
                        <span class="cnarg">
                            ${call.tokens > 0 ? `${shortTokens(call.tokens)} ${tokenWord(call.tokens)} · ` : ""}text not recorded
                        </span>
                    </span>
                </div>
            ` : html`
                <button class="callnode" type="button" key=${`${call.pos}-${call.index}`}
                        onClick=${() => setPick(n)}>
                    <span class=${`cnmark ${call.kind || "other"}`}>${kindIcon(call.kind)}</span>
                    <span class="cnbody">
                        <span class="cnname">${call.name}</span>
                        ${call.arg && html`<span class="cnarg">${call.arg}</span>`}
                        ${call.done && html`
                            <span class=${`cndone ${doneKind(call.done.status)}`}>
                                ${doneText(call.done)}
                            </span>
                        `}
                    </span>
                    <span class="crgo">${Icon.chevron()}</span>
                </button>
            `))}
        </div>
    `;
}

function doneKind(status) {
    if (status === "completed") return "ok";
    if (status === "killed") return "warn";
    return "crit";
}

function doneText(done) {
    const word = { completed: "finished", failed: "failed", killed: "stopped" }[done.status]
        || done.status;
    const code = /exit code (\d+)/.exec(done.summary || "");
    return code ? `${word} · code ${code[1]}` : word;
}

function fetchCall(session, id, at) {
    return fetch(`/api/chat/call?session=${encodeURIComponent(session)}${idParam(id)}&pos=${at.pos}&i=${at.index}`)
        .then(async (r) => {
            if (!r.ok) throw new Error((await r.text()).trim() || `response ${r.status}`);
            return r.json();
        });
}

function CallView({ session, id, call, place, onBack, onFile }) {
    const [state, setState] = useState({ kind: "loading" });

    useBackClose(true, onBack);

    useEffect(() => {
        let alive = true;
        setState({ kind: "loading" });
        fetchCall(session, id, call)
            .then((data) => alive && setState({ kind: "ready", ...data }))
            .catch((e) => alive && setState({ kind: "failed", error: String(e.message || e) }));
        return () => { alive = false; };
    }, [session, id, call.pos, call.index]);

    const took = tookText(state.at, state.resultAt);

    return html`
        <${BackHead} onBack=${onBack} label="to the calls">
            <div class="chatwho">
                <h2>${call.name}</h2>
                <div class="chatsub">
                    <span>${place}</span>
                    ${state.tool && state.tool !== call.name && html`<span class="sep">·</span><span>${state.tool}</span>`}
                    ${took && html`<span class="sep">·</span><span>${took}</span>`}
                </div>
            </div>
        <//>

        <div class="callbody">
            ${state.kind === "loading" && html`<p class="hint">Reading the call…</p>`}
            ${state.kind === "failed" && html`<p class="hint crit">${state.error}</p>`}
            ${state.kind === "ready" && html`
                <h3 class="callcap">what it was called with</h3>
                ${onFile && fileOfCall(state.args) && html`
                    <${FileCard} file=${fileOfCall(state.args)} onOpen=${onFile} />
                `}
                <pre class="callpre">${state.args || "no arguments"}</pre>
                ${state.argsCut && html`<p class="hint warn">The arguments are longer than shown — cut.</p>`}

                <h3 class="callcap">${state.failed ? "returned an error" : "what came out"}</h3>
                ${state.pending
                    ? html`<p class="hint warn">There is no answer: the call never finished — the session was
                        interrupted or the window was closed before it came back.</p>`
                    : html`<pre class=${`callpre${state.failed ? " failed" : ""}`}>${state.result || "empty"}</pre>`}
                ${state.resultCut && html`<p class="hint warn">The output is longer than shown — cut.</p>`}
            `}
        </div>
    `;
}

function tookText(from, to) {
    if (!from || !to) return "";
    const ms = Date.parse(to) - Date.parse(from);
    if (!Number.isFinite(ms) || ms < 0) return "";
    if (ms < 1000) return `${ms} ms`;
    if (ms < 60000) return `${(ms / 1000).toFixed(1)} s`;
    return `${Math.round(ms / 60000)} min`;
}
