// The checklist a session keeps of its work through the panel's checklist
// tool, read only: a line of it at the top of the card above the composer on
// a phone, the whole of it in a sheet from that line, and on a desk a block
// at the top of the timeline column that folds to one line. The steps come in
// the order the model keeps them, each pending, active, done or dropped.

import { useState } from "preact/hooks";

import { html } from "../../html.js";
import { Icon } from "../../ui/icons.js";
import { stampText } from "./labels.js";

// Whether the block on a desk is folded is kept per browser, not per session:
// a person who wants the column for the timeline wants it in every
// conversation.
export const CHECKLIST_KEY = "aacpanel.chat.checklist";

// Past this many steps the ticks of the line stand closer, or forty of them
// would push the step itself off the line.
const DENSE = 12;

// checklistOf reads a checklist for the screens: its steps, how many are done
// and dropped, the step the session stands on — the first at work, or failing
// that the first still to do — and its number. A dropped step is behind the
// session as a done one is, so the number counts both. A checklist with no
// step left is over, and is shown so until the next checklist replaces it. No
// checklist, or one without steps, is null.
export function checklistOf(checklist) {
    const items = checklist && Array.isArray(checklist.items) ? checklist.items : [];
    if (!items.length) return null;
    let current = items.findIndex((s) => s.status === "active");
    if (current < 0) current = items.findIndex((s) => s.status === "pending");
    const done = items.filter((s) => s.status === "done").length;
    const dropped = items.filter((s) => s.status === "dropped").length;
    const total = items.length;
    const over = current < 0;
    return {
        items, total, done, dropped, current, over,
        n: over ? total : Math.min(total, done + dropped + 1),
        step: over ? "" : items[current].text,
        note: checklist.note || "",
        at: checklist.at || "",
    };
}

// checklistShort is the checklist in the few words a card of the sessions
// list has room for: where the session is and the step it is on.
export function checklistShort(checklist) {
    const p = checklistOf(checklist);
    if (!p) return "";
    return p.over ? `checklist done · ${p.total}/${p.total}` : `${p.n}/${p.total} · ${p.step}`;
}

// clockOf is the time of day a step took its status, with the day when it
// was not today.
export function clockOf(iso) {
    const full = stampText(iso);
    if (!full) return "";
    const [day, time] = full.split(" · ");
    return stampText(new Date(Date.now()).toISOString()).startsWith(`${day} · `) ? time : full;
}

// stepTime says when a step stood where it stands: a done one when it was
// done, the one at work since when; the step the session goes to next,
// with none at work, is next.
function stepTime(step, now) {
    if (step.status === "done") return clockOf(step.since);
    if (step.status === "active") return step.since ? `since ${clockOf(step.since)}` : "";
    return now ? "next" : "";
}

function checklistSub(p) {
    return [
        `${p.n} of ${p.total}`,
        p.done > 0 && `${p.done} done`,
        p.dropped > 0 && `${p.dropped} dropped`,
        p.at && `updated ${clockOf(p.at)}`,
    ].filter(Boolean).join(" · ");
}

function Ticks({ p }) {
    return html`
        <span class=${`ckticks${p.total > DENSE ? " dense" : ""}`} style=${`--n:${p.total}`} aria-hidden="true">
            ${p.items.map((s, i) => html`<i key=${i} class=${`t-${s.status}${i === p.current ? " now" : ""}`}></i>`)}
        </span>
    `;
}

// The words every form of the checklist heads with: the checklist and where
// the session is in it, or that it is done.
function Head({ p }) {
    return html`
        <span class="ckword">${p.over ? "Checklist done" : "Checklist"}</span>
        <span class="cknum">${p.n} of ${p.total}</span>
    `;
}

function said(p) {
    return p.over
        ? `checklist done, ${p.total} of ${p.total}`
        : `checklist: step ${p.n} of ${p.total}, ${p.step}`;
}

// ChecklistLine is the checklist in one line — where the session is, the step
// it is on and a tick for every step — and a tap opens the whole of it.
export function ChecklistLine({ checklist, onOpen }) {
    const p = checklistOf(checklist);
    if (!p) return null;
    return html`
        <button class=${`ckline${p.over ? " over" : ""}`} type="button" onClick=${onOpen}
                aria-label=${`${said(p)} — open the checklist`}>
            <${Head} p=${p} />
            ${!p.over && html`<span class="ckstep">${p.step}</span>`}
            <${Ticks} p=${p} />
            <span class="ckchev">${Icon.chevron()}</span>
        </button>
    `;
}

// StepList is every step with its mark: a tick for a done one and when it was
// done, the one at work lit and since when, the rest hollow, a dropped one
// struck out.
function StepList({ p }) {
    return html`
        <ol class="cklist">
            ${p.items.map((s, i) => {
                const now = i === p.current;
                const time = stepTime(s, now);
                return html`
                    <li key=${i} class=${`ckitem s-${s.status}${now ? " now" : ""}`}>
                        <span class="ckmark" aria-hidden="true">
                            ${s.status === "done" ? Icon.check() : s.status === "dropped" ? Icon.close() : ""}
                        </span>
                        ${s.status === "dropped"
                            ? html`<s class="cktext">${s.text}</s>`
                            : html`<span class="cktext">${s.text}</span>`}
                        ${time && html`<span class="ckat">${time}</span>`}
                    </li>
                `;
            })}
        </ol>
    `;
}

// ChecklistSheet is the whole checklist in the sheet the line opens, its note
// on top.
export function ChecklistSheet({ checklist }) {
    const p = checklistOf(checklist);
    if (!p) return html`<p class="cmdnote">The session keeps no checklist now.</p>`;
    return html`
        <div class="sheethead">
            <div class="chatwho">
                <h2>${p.over ? "checklist done" : "checklist"}</h2>
                <div class="chatsub"><span>${checklistSub(p)}</span></div>
            </div>
        </div>
        ${p.note && html`<p class="cknote">${p.note}</p>`}
        <${StepList} p=${p} />
    `;
}

function readFolded(storage) {
    try {
        return storage.getItem(CHECKLIST_KEY) === "folded";
    } catch {
        return false;
    }
}

function saveFolded(folded, storage) {
    try {
        storage.setItem(CHECKLIST_KEY, folded ? "folded" : "open");
    } catch {
    }
}

// ChecklistBlock is the checklist at the top of the timeline column of a
// desk, held there while the feed scrolls under it. Folded, it is the one
// line of the phone; its head folds and unfolds it.
export function ChecklistBlock({ checklist, storage = localStorage }) {
    const [folded, setFolded] = useState(() => readFolded(storage));
    const p = checklistOf(checklist);
    if (!p) return null;
    const flip = () => {
        setFolded(!folded);
        saveFolded(!folded, storage);
    };
    return html`
        <section class=${`ckcol${folded ? " folded" : ""}${p.over ? " over" : ""}`}
                 aria-label="checklist of the session">
            <button class="ckhead" type="button" onClick=${flip} aria-expanded=${folded ? "false" : "true"}
                    aria-label=${`${said(p)} — ${folded
                        ? "show the whole checklist" : "fold the checklist to one line"}`}>
                <${Head} p=${p} />
                ${folded && !p.over && html`<span class="ckstep">${p.step}</span>`}
                <span class="ckfold">${Icon.chevron()}</span>
            </button>
            ${!folded && p.note && html`<p class="cknote">${p.note}</p>`}
            ${!folded && html`<${StepList} p=${p} />`}
        </section>
    `;
}
