// The shelf: briefs the sessions have published, newest first.
import { useEffect, useMemo, useState } from "preact/hooks";

import { html } from "../html.js";
import { Brief } from "./brief.js";
import { shelf, state } from "../data/briefs.js";

function when(at) {
    if (!at) return "";
    const t = new Date(at);
    if (Number.isNaN(t.getTime())) return "";
    const today = new Date();
    const sameDay = t.toDateString() === today.toDateString();
    return sameDay
        ? t.toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" })
        : t.toLocaleDateString(undefined, { day: "2-digit", month: "short" });
}

// from says who wrote the document: the session by name while it is running,
// and the directory it worked in once it is gone. A brief is answered days
// later, so the second case is the usual one.
function from(card, snapshot) {
    const list = (snapshot && snapshot.sessions) || [];
    const live = list.find((s) => s && s.sessionId === card.sessionId);
    if (live) return { name: live.session, live: true };
    const cwd = card.cwd || "";
    const tail = cwd.split("/").filter(Boolean).pop();
    return { name: tail || "a session that has ended", live: false };
}

function Card({ card, snapshot, onOpen }) {
    const mark = state(card);
    const who = from(card, snapshot);
    return html`
        <button type="button" class=${`bcard is-${mark.tone}`} onClick=${() => onOpen(card.id)}>
            <span class="bcard-t">${card.title}</span>
            ${card.eyebrow && html`<span class="bcard-s">${card.eyebrow}</span>`}
            <span class="bcard-meta">
                <span class=${who.live ? "bcard-live" : ""}>${who.name}</span>
                ${when(card.at) && html`<span class="bcard-at">${when(card.at)}</span>`}
            </span>
            <span class="bcard-state">${mark.word}</span>
            <span class="bcard-bar" aria-hidden="true">
                <i style=${`width:${Math.round(mark.share * 100)}%`}></i>
            </span>
        </button>
    `;
}

export function Briefs({ snapshot, exec, onSession, open, onOpen, onLeave }) {
    const [cards, setCards] = useState(null);
    const [error, setError] = useState("");


    // A card in the feed of a session opens the document it names. Coming back
    // from that document leaves the shelf, which is where the person would
    // have been had they walked in through the menu.
    // Which document is open lives in the shell, not here: the back gesture is
    // claimed once, by the page, and the page has to know what is on it to
    // decide what the gesture takes off.

    useEffect(() => {
        if (open) return undefined;
        let gone = false;
        shelf()
            .then((rows) => { if (!gone) { setCards(rows); setError(""); } })
            .catch((e) => { if (!gone) setError(String(e.message || e)); });
        return () => { gone = true; };
    }, [open]);

    // What the shelf is asking of the person, counted once for the head.
    const sums = useMemo(() => {
        const rows = cards || [];
        let waiting = 0;
        let ready = 0;
        for (const card of rows) {
            if (card.sent) continue;
            if (card.questions && card.answered >= card.questions) ready++;
            else waiting++;
        }
        return { all: rows.length, waiting, ready };
    }, [cards]);

    if (open) {
        return html`<${Brief} id=${open} snapshot=${snapshot} exec=${exec}
            onBack=${onLeave || (() => onOpen(null))}
            onSession=${onSession} />`;
    }

    const say = !cards
        ? "opening…"
        : error
        ? error
        : sums.ready && sums.waiting
        ? `${sums.ready} ready to send · ${sums.waiting} still open`
        : sums.ready
        ? `${sums.ready} ready to send`
        : sums.waiting
        ? `${sums.waiting} waiting for an answer`
        : sums.all
        ? "all answered and sent"
        : "nothing waiting";

    return html`
        <div class="bshelf">
            <header class="bshelf-h">
                <h2>Briefs</h2>
                <span class=${`bshelf-s${error ? " crit" : ""}`}>${say}</span>
            </header>

            ${cards && !cards.length && !error && html`
                <p class="bshelf-none">
                    A session publishes one when what it has to ask does not fit a
                    question in the console: several questions at once, or an option
                    that takes a paragraph to explain.
                </p>
            `}

            ${cards && cards.length > 0 && html`
                <div class="bcards">
                    ${cards.map((card) => html`
                        <${Card} key=${card.id} card=${card} snapshot=${snapshot} onOpen=${onOpen} />
                    `)}
                </div>
            `}
        </div>
    `;
}
