import { useEffect, useRef, useState } from "preact/hooks";

import { html } from "../html.js";
import { render } from "../md.js";
import { saveDraft, sendDecisions } from "../data/briefs.js";
import { Icon } from "../ui/icons.js";

const SAVE_MS = 900;
const decisionID = (decision, index) => decision.id || String(index + 1);
const selected = (decision, answer) => decision.options.filter((option) => answer?.picks?.includes(option.key));

function Decision({ decision, index, answer, locked, onAnswer }) {
    const [editing, setEditing] = useState(false);
    const picks = selected(decision, answer);
    const label = `${index + 1} · ${decision.title}`;
    const pick = (key) => {
        if (locked) return;
        if (decision.multi) setEditing(true);
        onAnswer((value) => {
            const was = value?.picks || [];
            const next = decision.multi ? was.includes(key) ? was.filter((k) => k !== key) : [...was, key] : [key];
            return { ...value, picks: next };
        });
    };
    if (picks.length && (!editing || locked)) {
        return html`<article class="bdecision decided" data-decision=${decisionID(decision, index)}>
            <div class="bdecision-line"><span class="bdecision-tick">✓</span>
                <span>${index + 1} · <b>${picks.map((option) => option.label).join(", ")}</b></span>
                ${!locked && html`<button type="button" class="bchange" aria-label=${`Change ${decision.title}`}
                    onClick=${() => setEditing(true)}>change</button>`}
            </div>
            ${locked && answer?.note && html`<p class="bdecision-note">${answer.note}</p>`}
        </article>`;
    }
    return html`<article class="bdecision" data-decision=${decisionID(decision, index)}>
        <h3>${label}</h3>
        <div class="bdecision-ask">${render(decision.ask)}</div>
        <div class="bdecision-options" role=${decision.multi ? "group" : "radiogroup"} aria-label=${label}>
            ${decision.options.map((option) => html`<label key=${option.key} class=${`bchoice${answer?.picks?.includes(option.key) ? " picked" : ""}`}>
                <input type=${decision.multi ? "checkbox" : "radio"} name=${`decision-${decisionID(decision, index)}`}
                    value=${option.key} checked=${Boolean(answer?.picks?.includes(option.key))} disabled=${locked}
                    onChange=${() => pick(option.key)} />
                <span class="bchoice-body"><span class="bchoice-label">${option.label}
                    ${option.recommended && html`<span class="brecommended">RECOMMENDED</span>`}</span>
                    <span class="bchoice-reason">${option.reason}</span>
                    <span class="bchoice-cost">cost: ${option.cost}</span>
                    <span class="bchoice-hours">≈ ${Math.round(option.hours * 10) / 10} h</span>
                </span>
            </label>`)}
        </div>
        ${editing && !locked && html`<label class="bdecision-notefield">Decision note
            <textarea value=${answer?.note || ""} maxlength="2000" placeholder="A note about this decision"
                onInput=${(event) => { const note = event.target.value; onAnswer((value) => ({ ...value, note })); }}></textarea>
        </label>`}
        ${editing && picks.length > 0 && !locked && html`<button type="button" class="bchange bdecision-done"
            onClick=${() => setEditing(false)}>Done</button>`}
    </article>`;
}

export function BriefDecisions({ doc, draft, embedded, header, onSent, onRemove, dropping }) {
    const [answers, setAnswers] = useState(() => draft?.answers || {});
    const [sent, setSent] = useState(Boolean(draft?.sentAt));
    const [sending, setSending] = useState(false);
    const [error, setError] = useState("");
    const phase = useRef(draft?.sentAt ? "sent" : "");
    const latest = useRef(answers);
    const pending = useRef(null);
    const timer = useRef(null);
    const saving = useRef(Promise.resolve());
    const mounted = useRef(true);

    // Writes stay in order even when the person edits while an earlier save is
    // in flight. Sending waits for that queue and the last unsaved edit.
    const put = (next) => {
        const request = saving.current.then(() => saveDraft(doc.id, next));
        saving.current = request.then(() => {}, () => {});
        return request.then((body) => {
            if (pending.current === next) pending.current = null;
            if (mounted.current) setError("");
            return body;
        });
    };
    const failed = (err) => { if (mounted.current) setError(String(err.message || err)); };
    useEffect(() => {
        mounted.current = true;
        return () => {
            mounted.current = false;
            clearTimeout(timer.current);
            if (pending.current && !phase.current) put(pending.current).catch(failed);
        };
    }, []);

    const edit = (next) => {
        if (phase.current) return;
        latest.current = next;
        pending.current = next;
        setAnswers(next);
        clearTimeout(timer.current);
        timer.current = setTimeout(() => put(next).catch(failed), SAVE_MS);
    };
    const answer = (id, change) => edit({ ...latest.current, decisions: {
        ...latest.current.decisions, [id]: change(latest.current.decisions?.[id]),
    } });
    const done = doc.decisions.filter((decision, index) => selected(decision, answers.decisions?.[decisionID(decision, index)]).length).length;
    const left = doc.decisions.length - done;
    const accept = () => {
        const decisions = { ...latest.current.decisions };
        doc.decisions.forEach((decision, index) => {
            const id = decisionID(decision, index);
            if (selected(decision, decisions[id]).length) return;
            decisions[id] = { ...decisions[id], picks: decision.options.filter((option) => option.recommended).map((option) => option.key) };
        });
        edit({ ...latest.current, decisions });
    };
    const send = async () => {
        if (phase.current || !done) return;
        phase.current = "sending";
        setSending(true);
        clearTimeout(timer.current);
        try {
            if (pending.current) await put(pending.current);
            await saving.current;
            await sendDecisions(doc.id);
            phase.current = "sent";
            setSent(true);
            setError("");
            if (onSent) onSent();
        } catch (err) {
            phase.current = "";
            failed(err);
        } finally {
            if (mounted.current) setSending(false);
        }
    };
    const locked = sent || sending;
    return html`<div class=${`brief-decisions${embedded ? " embedded" : ""}`}>
        ${header}
        <div class="bdecision-scroll">
            <header class="bdecision-head"><span>${Icon.plan()} BRIEF · ${doc.decisions.length} decisions</span>
                ${!embedded && html`<h1>${doc.title}</h1>`}</header>
            <div class="btask"><div class="bdecision-label">TASK</div><p>${doc.task}</p>
                <div class="bdecision-label">WHY</div><p>${doc.why}</p></div>
            ${doc.context?.length > 0 && html`<details class="bcontext"><summary>Context · ${doc.context.length}</summary>
                ${doc.context.map((block, index) => html`<section key=${index}><h3>${block.title}</h3>${render(block.text)}</section>`)}
            </details>`}
            ${!sent && html`<button class="btn brecommend" type="button" disabled=${!left || sending} onClick=${accept}>
                Accept recommended · ${left}</button>`}
            <div class="bdecisions">${doc.decisions.map((decision, index) => html`<${Decision}
                key=${decisionID(decision, index)} decision=${decision} index=${index}
                answer=${answers.decisions?.[decisionID(decision, index)]} locked=${locked}
                onAnswer=${(value) => answer(decisionID(decision, index), value)} />`)}</div>
            ${sent ? answers.note && html`<p class="bdecision-note">${answers.note}</p>` : html`<label class="bdecision-notefield">Note
                <textarea class="bbrief-note" value=${answers.note || ""} maxlength="4000" disabled=${sending}
                    placeholder="A note for the session" onInput=${(event) => edit({ ...latest.current, note: event.target.value })}></textarea>
            </label>`}
        </div>
        <footer class="bdecision-foot"><span class="bdecision-progress" role="status">${done} of ${doc.decisions.length} decided${sent ? " · sent" : ""}</span>
            ${error && html`<p class="bdecision-error" role="alert">${error}</p>`}
            ${onRemove && html`<button type="button" class="btn bdecision-remove" disabled=${dropping === "going" || sending} onClick=${onRemove}>
                ${dropping === "going" ? "Removing…" : dropping === "asking" ? "Remove it?" : "Remove"}</button>`}
            <button class="btn primary bdecision-send" type="button" disabled=${!done || locked} onClick=${send}>
                ${sending ? "Sending…" : sent ? "Sent" : "Send decisions"}</button>
        </footer>
    </div>`;
}
