import { useRef, useState } from "preact/hooks";

import { html } from "../../html.js";
import { render } from "../../md.js";
import { Icon } from "../../ui/icons.js";
import { stampText } from "./labels.js";

// RequestCard keeps the answer with its steps in the composer.
// Opening it does not answer it, and an unavailable state offers no send.
export function RequestCard({ item, record, onAnswer, onRun, error = "", unavailable = "" }) {
    const [note, setNote] = useState("");
    const [writing, setWriting] = useState(false);
    const [cant, setCant] = useState(false);
    const [busy, setBusy] = useState(false);
    const sending = useRef(false);
    const open = record && !record.answered && !record.withdrawn;
    const word = !record ? (error || "Reading request state…") : record.withdrawn ? "Withdrawn"
        : record.answered ? (record.outcome === "done" ? "Done" : "Can't") : "Open";
    const answer = async (value) => {
        if (sending.current || !open || !onAnswer) return;
        sending.current = true;
        setBusy(true);
        try { await onAnswer(record.id, value, note); } finally { sending.current = false; setBusy(false); }
    };
    return html`<article class=${`sent mrequest${open && record.blocking ? " blocking" : ""}`}>
        <div class="senthead"><span class="sentico">${Icon.list()}</span><span class="sentlabel">request</span>
            ${open && record.blocking && html`<span class="requestblocking">Blocking</span>`}
            ${item.at && html`<span class="sentat">${stampText(item.at)}</span>`}</div>
        <h3 class="requesttitle">${record ? record.title : item.title}</h3>
        <div class="msg requeststeps">${render(record ? record.steps : item.steps || "", { onRun })}</div>
        ${open && onAnswer ? html`
            <div class="btnrow requestanswer"><button class="btn primary" disabled=${busy} onClick=${() => answer("done")}>${busy ? "Sending…" : "Done"}</button>
                <button class="btn" disabled=${busy} onClick=${() => cant ? answer("cant") : setCant(true)}>Can't</button></div>
            ${!writing && !cant && html`<button class="actnote" type="button" onClick=${() => setWriting(true)}>note</button>`}
            ${(writing || cant) && html`<label class="requestnote">${cant ? "Reason (optional)" : "Note (optional)"}<textarea rows="2" maxLength="2000"
                value=${note} disabled=${busy} onInput=${(e) => setNote(e.currentTarget.value)}></textarea></label>`}`
            : html`<p class="requeststate" role="status">${open && unavailable ? unavailable : word}</p>`}
        ${record && record.answered && record.note && html`<div class="requestreply">${record.note}</div>`}
    </article>`;
}

const waitingRow = (item, node) => Number(node.dataset.pos) === item.pos
    && node.classList.contains(`r-${item.kind === "question" ? "ai" : item.kind}`)
    && Number(node.dataset.nth || 0) === (item.nth || 0);

// A revealed card keeps its contour only while its item is outstanding.
export function clearWaitingTargets(items, box) {
    if (!box) return;
    for (const row of box.querySelectorAll(".waitingtarget")) {
        if (!items.some((item) => waitingRow(item, row))) row.classList.remove("waitingtarget");
    }
}

// revealWaiting reads an older window when necessary, then scrolls this feed
// only: scrollIntoView would move the page and its composer along with it.
export async function revealWaiting(item, show, box) {
    if (item.pos == null || !await show(item.pos)) return false;
    const row = [...box.querySelectorAll(".feedrow[data-pos]")].find((node) => waitingRow(item, node));
    if (!row) return false;
    for (const was of box.querySelectorAll(".waitingtarget")) was.classList.remove("waitingtarget");
    row.classList.add("waitingtarget");
    box.scrollTop += row.getBoundingClientRect().top - box.getBoundingClientRect().top - 12;
    return true;
}
