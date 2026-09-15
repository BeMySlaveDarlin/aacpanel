// The shelf: briefs the sessions have published, newest first.
import { useEffect, useState } from "preact/hooks";

import { html } from "../html.js";
import { Brief } from "./brief.js";
import { shelf } from "../data/briefs.js";

function when(at) {
    if (!at) return "";
    const t = new Date(at);
    if (Number.isNaN(t.getTime())) return "";
    return t.toLocaleString(undefined, { day: "2-digit", month: "2-digit", hour: "2-digit", minute: "2-digit" });
}

function Card({ card, onOpen }) {
    const asks = card.questions > 0;
    const mark = card.sent ? "sent" : asks ? `${card.answered}/${card.questions}` : "read";
    return html`
        <button type="button" class=${`bcard${card.sent ? " is-sent" : ""}`} onClick=${() => onOpen(card.id)}>
            <span class="bcard-t">${card.title}</span>
            <span class="bcard-s">${[when(card.at), card.eyebrow].filter(Boolean).join(" · ")}</span>
            <span class="bcard-n">${mark}</span>
        </button>
    `;
}

export function Briefs({ snapshot, exec, onBack }) {
    const [cards, setCards] = useState(null);
    const [error, setError] = useState("");
    const [open, setOpen] = useState(null);

    useEffect(() => {
        if (open) return undefined;
        let gone = false;
        shelf()
            .then((rows) => { if (!gone) { setCards(rows); setError(""); } })
            .catch((e) => { if (!gone) setError(String(e.message || e)); });
        return () => { gone = true; };
    }, [open]);

    if (open) {
        return html`<${Brief} id=${open} snapshot=${snapshot} exec=${exec} onBack=${() => setOpen(null)} />`;
    }

    if (error) return html`<div class="pad"><p class="dim">${error}</p></div>`;
    if (!cards) return html`<div class="pad"><p class="dim">opening…</p></div>`;
    if (!cards.length) {
        return html`
            <div class="pad">
                <p class="dim">
                    No briefs. A session publishes one when what it has to ask does not
                    fit a question in the console.
                </p>
            </div>
        `;
    }

    return html`
        <div class="bcards">
            ${cards.map((card) => html`<${Card} key=${card.id} card=${card} onOpen=${setOpen} />`)}
        </div>
    `;
}
