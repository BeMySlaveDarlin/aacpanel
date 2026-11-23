import { useRef, useState } from "preact/hooks";

import { html } from "../../html.js";
import { render } from "../../md.js";
import { Icon } from "../../ui/icons.js";
import { actState } from "./actform.js";
import { stampText } from "./labels.js";

// RequestCard keeps the answer with its steps in the form of the feed: a
// note or a reason above one row of what can be answered.
// Opening it does not answer it, and an unavailable state offers no send.
// A chain is not one of them: it is answered in its own row (see ChainCard).
//
// Placed, it is the request in its own row of the feed, its state on its
// head. An open one is answered in the form at the end of the feed, so its
// row names it and leads there (onOpen, absent where no form stands): the
// steps drawn twice would read as two requests. A row whose act is not read
// yet stands as an open one, the way the transcript placed it. A closed one
// keeps its steps, how it was answered and the person's note.
export function RequestCard({ item, record, onAnswer, onRun, onOpen, placed = false, error = "", unavailable = "" }) {
    const [note, setNote] = useState("");
    const [writing, setWriting] = useState(false);
    const [cant, setCant] = useState(false);
    const [busy, setBusy] = useState(false);
    const sending = useRef(false);
    const open = record && !record.answered && !record.withdrawn;
    const waits = placed && (!record || open);
    const word = !record ? (error || "Reading request state…") : record.withdrawn ? "Withdrawn"
        : record.answered ? (record.outcome === "done" ? "Done" : "Can't") : "Open";
    const answer = async (value) => {
        if (sending.current || !open || !onAnswer) return;
        sending.current = true;
        setBusy(true);
        try { await onAnswer(record.id, value, note); } finally { sending.current = false; setBusy(false); }
    };
    return html`<article class=${`sent mrequest${open && record.blocking ? " blocking" : ""}`} data-act=${placed ? item.id : undefined}>
        <div class="senthead"><span class="sentico">${Icon.list()}</span>
            <span class="sentlabel">${placed && record ? `request · ${actState(record)}` : "request"}</span>
            ${open && record.blocking && html`<span class="requestblocking">Blocking</span>`}
            ${item.at && html`<span class="sentat">${stampText(item.at)}</span>`}</div>
        <h3 class="requesttitle">${record ? record.title : item.title}</h3>
        ${!waits && html`<div class="msg requeststeps">${render(record ? record.steps : item.steps || "", { onRun })}</div>`}
        ${placed ? waits && onOpen && html`<div class="btnrow actfoot">
            <button class="btn primary" type="button" onClick=${() => onOpen(item)}>Answer</button></div>`
            : open && onAnswer ? html`
            ${(writing || cant) && html`<label class="requestnote">${cant ? "Reason (optional)" : "Note (optional)"}<textarea rows="2" maxLength="2000"
                value=${note} disabled=${busy} onInput=${(e) => setNote(e.currentTarget.value)}></textarea></label>`}
            <div class="btnrow requestanswer actfoot">
                ${!writing && !cant && html`<button class="actnote" type="button" onClick=${() => setWriting(true)}>note</button>`}
                <button class="btn" disabled=${busy} onClick=${() => cant ? answer("cant") : setCant(true)}>Can't</button>
                <button class="btn primary" disabled=${busy} onClick=${() => answer("done")}>${busy ? "Sending…" : "Done"}</button>
            </div>`
            : html`<p class="requeststate" role="status">${open && unavailable ? unavailable : word}</p>`}
        ${record && record.answered && record.note && html`<div class="requestreply">${record.note}</div>`}
    </article>`;
}

const waitingRow = (item, node) => Number(node.dataset.pos) === item.pos
    && node.classList.contains(`r-${item.kind}`)
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
