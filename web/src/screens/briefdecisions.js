import { useEffect, useRef, useState } from "preact/hooks";

import { html } from "../html.js";
import { render } from "../md.js";
import { saveDraft, sendDecisions } from "../data/briefs.js";
import { Icon } from "../ui/icons.js";

const SAVE_MS = 900;
const decisionID = (decision, index) => decision.id || String(index + 1);
const selected = (decision, answer) => decision.options.filter((option) => answer?.picks?.includes(option.key));
const recommended = (decision) => decision.options.filter((option) => option.recommended);
const labels = (options) => options.map((option) => option.label).join(", ");

function Decision({ decision, index, answer, locked, onAnswer }) {
    const label = `${index + 1} · ${decision.title}`;
    const pick = (key) => {
        if (locked) return;
        onAnswer((value) => {
            const was = value?.picks || [];
            const next = decision.multi ? was.includes(key) ? was.filter((k) => k !== key) : [...was, key] : [key];
            return { ...value, picks: next };
        }, !decision.multi);
    };
    return html`<article class="bdecision" data-decision=${decisionID(decision, index)}>
        <h3>${decision.title}</h3>
        ${decision.ask && html`<div class="bdecision-ask">${render(decision.ask)}</div>`}
        ${decision.multi && html`<p class="bdecision-hint">Several may be picked. Press Next when ready.</p>`}
        <div class="bdecision-options" role=${decision.multi ? "group" : "radiogroup"} aria-label=${label}>
            ${decision.options.map((option) => html`<div key=${option.key} class=${`bchoice${answer?.picks?.includes(option.key) ? " picked" : ""}`}>
                <label class="bchoice-pick">
                    <input type=${decision.multi ? "checkbox" : "radio"} name=${`decision-${decisionID(decision, index)}`}
                        value=${option.key} checked=${Boolean(answer?.picks?.includes(option.key))} disabled=${locked}
                        onChange=${() => pick(option.key)} />
                    <span class="bchoice-body"><span class="bchoice-label">${option.label}
                        ${option.recommended && html`<span class="brecommended">recommended</span>`}</span>
                        <span class="bchoice-reason">${option.reason}</span>
                    </span>
                    <span class="bchoice-hours">≈ ${Math.round(option.hours * 10) / 10} h</span>
                </label>
                <details class="bchoice-details"><summary><span class="bchoice-cost">Cost: ${option.cost}</span><span>Details</span></summary>
                    <p>${option.reason}</p><p class="bchoice-cost">Cost: ${option.cost}</p>
                </details>
            </div>`)}
        </div>
        ${locked ? answer?.note && html`<p class="bdecision-note">${answer.note}</p>` : html`
            <details class="bdecision-addnote" open=${Boolean(answer?.note)}><summary>${answer?.note ? "Decision note" : "Add a note"}</summary>
                <label class="bdecision-notefield">Decision note
                    <textarea value=${answer?.note || ""} maxlength="2000" placeholder="A note about this decision"
                        onInput=${(event) => { const note = event.target.value; onAnswer((value) => ({ ...value, note })); }}></textarea>
                </label>
            </details>`}
    </article>`;
}

function Summary({ doc, answers, onDecision }) {
    return html`<section class="bdecision-summary"><h3>Review decisions</h3>
        <div class="bsummary-list">${doc.decisions.map((decision, index) => {
            const answer = answers.decisions?.[decisionID(decision, index)];
            const picks = selected(decision, answer);
            const proposal = recommended(decision);
            const changed = picks.length > 0 && proposal.length > 0 &&
                (picks.length !== proposal.length || picks.some((option) => !option.recommended));
            return html`<button type="button" key=${decisionID(decision, index)} class="bsummary-row" onClick=${() => onDecision(index)}>
                <span class="bsummary-question">${index + 1} · ${decision.title}</span>
                <span class=${picks.length ? "bsummary-answer" : "bsummary-open"}>${picks.length ? labels(picks) : "Open"}</span>
                ${changed && html`<span class="bsummary-changed">changed · recommended ${labels(proposal)}</span>`}
                ${!picks.length && html`<span class="bsummary-proposal">${proposal.length
                    ? `Take recommended: ${labels(proposal)}` : "No recommended option; choose an answer"}</span>`}
                ${answer?.note && html`<span class="bdecision-note">${answer.note}</span>`}
            </button>`;
        })}</div>
    </section>`;
}

export function BriefDecisions({ doc, draft, embedded, header, onSent, onRemove, dropping }) {
    const [answers, setAnswers] = useState(() => draft?.answers || {});
    const [current, setCurrent] = useState(() => {
        if (draft?.sentAt) return doc.decisions.length;
        const first = doc.decisions.findIndex((decision, index) => !selected(decision, draft?.answers?.decisions?.[decisionID(decision, index)]).length);
        return first < 0 ? doc.decisions.length : first;
    });
    const [sent, setSent] = useState(Boolean(draft?.sentAt));
    const [sending, setSending] = useState(false);
    const [error, setError] = useState("");
    const phase = useRef(draft?.sentAt ? "sent" : "");
    const latest = useRef(answers);
    const pending = useRef(null);
    const timer = useRef(null);
    const saving = useRef(Promise.resolve());
    const mounted = useRef(true);
    const card = useRef(null);
    const total = doc.decisions.length;
    const review = current === total;
    const move = (index) => {
        setCurrent(Math.max(0, Math.min(total, index)));
        card.current?.focus({ preventScroll: true });
    };
    const advance = () => {
        for (let step = 1; step <= total; step++) {
            const index = (current + step) % total;
            if (!selected(doc.decisions[index], latest.current.decisions?.[decisionID(doc.decisions[index], index)]).length) {
                move(index);
                return;
            }
        }
        move(total);
    };
    const keydown = (event) => {
        if (event.altKey || event.ctrlKey || event.metaKey || event.shiftKey || event.defaultPrevented ||
            event.target.closest('textarea, select, input:not([type="radio"]):not([type="checkbox"]), [contenteditable]:not([contenteditable="false"])')) return;
        if (event.key !== "ArrowLeft" && event.key !== "ArrowRight") return;
        event.preventDefault();
        move(current + (event.key === "ArrowLeft" ? -1 : 1));
    };

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
    const answer = (id, change, goNext) => {
        edit({ ...latest.current, decisions: {
            ...latest.current.decisions, [id]: change(latest.current.decisions?.[id]),
        } });
        if (goNext) advance();
    };
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
        if (phase.current || !done || !review) return;
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
    return html`<div class=${`brief-decisions${embedded ? " embedded" : ""}`} ref=${card} tabindex="0" onKeyDown=${keydown}>
        ${header}
        <div class="bdecision-body">
            <header class="bdecision-head"><span>${Icon.plan()} Brief · ${doc.decisions.length} decisions</span>
                ${embedded ? html`<h2>${doc.title}</h2>` : html`<h1>${doc.title}</h1>`}</header>
            <details class="bcontext"><summary>Task, why${doc.context?.length ? ` · context ${doc.context.length}` : ""}</summary>
                <div class="btask"><div class="bdecision-label">Task</div><p>${doc.task}</p>
                    <div class="bdecision-label">Why</div><p>${doc.why}</p></div>
                ${doc.context?.map((block, index) => html`<section key=${index}><h3>${block.title}</h3>${render(block.text)}</section>`)}
            </details>
            <nav class="bdecision-steps" aria-label="Decisions">${doc.decisions.map((decision, index) => {
                const decided = selected(decision, answers.decisions?.[decisionID(decision, index)]).length > 0;
                return html`<button type="button" key=${decisionID(decision, index)} data-step=${index}
                    class=${`bdecision-segment ${decided ? "answered" : "open"}${current === index ? " current" : ""}`}
                    aria-label=${`${index + 1} · ${decision.title} · ${decided ? "answered" : "open"}`}
                    aria-current=${current === index ? "step" : null} onClick=${() => move(index)}><span aria-hidden="true"></span></button>`;
            })}</nav>
            <div class="bdecision-position" role="status">${review ? "Summary" : `Decision ${current + 1} of ${total}`}</div>
            ${review ? html`<${Summary} doc=${doc} answers=${answers} onDecision=${move} />` : html`<${Decision}
                key=${decisionID(doc.decisions[current], current)} decision=${doc.decisions[current]} index=${current}
                answer=${answers.decisions?.[decisionID(doc.decisions[current], current)]} locked=${locked}
                onAnswer=${(value, goNext) => answer(decisionID(doc.decisions[current], current), value, goNext)} />`}
            ${review && !sent && html`<button class="btn brecommend" type="button" disabled=${!left || sending} onClick=${accept}>
                Take recommended · ${left}</button>`}
            ${review && (sent ? answers.note && html`<p class="bdecision-note">${answers.note}</p>` : html`<label class="bdecision-notefield">Note for the session
                <textarea class="bbrief-note" value=${answers.note || ""} maxlength="4000" disabled=${sending}
                    placeholder="A note for the session" onInput=${(event) => edit({ ...latest.current, note: event.target.value })}></textarea>
            </label>`)}
        </div>
        <footer class="bdecision-foot"><span class="bdecision-progress" role="status">${done} of ${doc.decisions.length} decided${sent ? " · sent" : ""}</span>
            ${error && html`<p class="bdecision-error" role="alert">${error}</p>`}
            ${onRemove && html`<button type="button" class="btn bdecision-remove" disabled=${dropping === "going" || sending} onClick=${onRemove}>
                ${dropping === "going" ? "Removing…" : dropping === "asking" ? "Remove it?" : "Remove"}</button>`}
            <button class="btn bdecision-prev" type="button" aria-label="Previous decision" disabled=${current === 0} onClick=${() => move(current - 1)}>‹</button>
            <button class="btn bdecision-next" type="button" aria-label="Next decision" disabled=${review}
                onClick=${() => doc.decisions[current]?.multi && !locked ? advance() : move(current + 1)}>${doc.decisions[current]?.multi ? "Next ›" : "›"}</button>
            ${review ? html`<button class="btn primary bdecision-send" type="button" disabled=${!done || locked} onClick=${send}>
                ${sending ? "Sending…" : sent ? "Sent" : "Send decisions"}</button>` : html`
                <button class="btn primary bdecision-review" type="button" onClick=${() => move(total)}>${sent ? "View answers" : "Review decisions"}</button>`}
        </footer>
    </div>`;
}
