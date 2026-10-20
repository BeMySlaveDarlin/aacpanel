import { useRef, useState } from "preact/hooks";

import { html } from "../../html.js";
import { render } from "../../md.js";
import { Icon } from "../../ui/icons.js";
import { stampText } from "./labels.js";

// A turn's dialog owns typed input until it has been answered.
export const REQUEST_HELD = "Answer the session's question or permission first.";

// RequestCard keeps the answer with its steps in the feed. Opening the
// conversation does not answer it, and an unavailable state offers no send.
export function RequestCard({ item, record, onAnswer, error = "", unavailable = "" }) {
    const [note, setNote] = useState("");
    const [busy, setBusy] = useState(false);
    const sending = useRef(false);
    const open = record && !record.answered && !record.withdrawn;
    const word = !record ? (error || "Reading request state…") : record.withdrawn ? "Withdrawn"
        : record.answered ? (record.answer === "done" ? "Done" : "Can't") : "Open";
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
        <div class="msg requeststeps">${render(record ? record.steps : item.steps || "")}</div>
        ${open && onAnswer ? html`<label class="requestnote">Note (optional)<textarea rows="2" maxLength="2000"
            value=${note} disabled=${busy} onInput=${(e) => setNote(e.currentTarget.value)}></textarea></label>
            <div class="btnrow requestanswer"><button class="btn" disabled=${busy} onClick=${() => answer("cant")}>Can't</button>
                <button class="btn primary" disabled=${busy} onClick=${() => answer("done")}>${busy ? "Sending…" : "Done"}</button></div>`
            : html`<p class="requeststate" role="status">${open && unavailable ? unavailable : word}</p>`}
        ${record && record.answered && record.note && html`<div class="requestreply">${record.note}</div>`}
    </article>`;
}

const icons = { request: Icon.list, call: Icon.alerts, brief: Icon.plan, secret: Icon.key };

// WaitingQueue is a set of places in this conversation, not another reply form.
export function WaitingQueue({ items, onPick }) {
    const [open, setOpen] = useState(true);
    if (!items.length) return null;
    return html`<section class="waitingqueue" aria-label="Waiting for you">
        <button class="waitinghead" type="button" aria-expanded=${open} onClick=${() => setOpen(!open)}>
            ${Icon.list()}<span>Waiting for you · ${items.length}</span><span class=${`waitingchev${open ? " on" : ""}`}>${Icon.chevron()}</span>
        </button>
        ${open && html`<div class="waitingitems">${items.map((item) => html`
            <button class="waitingitem" type="button" key=${`${item.kind}-${item.id || "call"}`}
                disabled=${item.pos == null} onClick=${() => { setOpen(false); onPick(item); }}>
                ${icons[item.kind]()}<span><b>${item.title}</b><small>${item.kind}${item.blocking ? " · blocking" : ""}${item.pos == null ? " · waiting for transcript" : ""}</small></span>${Icon.chevron()}
            </button>`)}</div>`}
    </section>`;
}

// revealWaiting reads an older window when necessary, then scrolls this feed
// only: scrollIntoView would move the page and its composer along with it.
export async function revealWaiting(item, show, box) {
    if (item.pos == null || !await show(item.pos)) return false;
    const row = [...box.querySelectorAll(".feedrow[data-pos]")].find((node) =>
        Number(node.dataset.pos) === item.pos && node.classList.contains(`r-${item.kind}`)
        && Number(node.dataset.nth || 0) === (item.nth || 0));
    if (!row) return false;
    for (const was of box.querySelectorAll(".waitingtarget")) was.classList.remove("waitingtarget");
    row.classList.add("waitingtarget");
    box.scrollTop += row.getBoundingClientRect().top - box.getBoundingClientRect().top - 12;
    return true;
}
