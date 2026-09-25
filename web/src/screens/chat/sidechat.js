// The side chat: questions asked aside of a session on the stream.
//
// Claude answers a question aside from what the conversation holds and writes
// neither the question nor the answer into it. It keeps no side chat of its
// own either: the one so far goes along with every question. The panel keeps
// it only while the conversation is open — a question in passing, not a second
// conversation — and the bin drops it.

import { useEffect, useLayoutEffect, useRef, useState } from "preact/hooks";

import { html } from "../../html.js";
import { render } from "../../md.js";
import { Icon } from "../../ui/icons.js";
import { Sheet } from "../../ui/sheet.js";

// useSideChat holds the side chat of one session: what was asked, what came
// back, and whether it is open. Another session starts with none.
export function useSideChat(name, id) {
    const [turns, setTurns] = useState([]);
    const [open, setOpen] = useState(false);
    const now = useRef(turns);
    now.current = turns;
    // An answer that comes back after the chat was dropped, or after the
    // conversation changed, belongs to nothing on screen.
    const era = useRef(0);
    useEffect(() => {
        era.current += 1;
        setTurns([]);
        setOpen(false);
    }, [name, id]);

    const ask = async (question) => {
        setOpen(true);
        const text = String(question || "").trim();
        if (!text || !name) return;
        const history = now.current
            .filter((t) => typeof t.answer === "string")
            .map((t) => ({ question: t.question, response: t.answer }));
        const key = `${Date.now()}-${Math.random().toString(36).slice(2, 7)}`;
        const mine = era.current;
        setTurns((was) => [...was, { key, question: text, waiting: true }]);
        let patch;
        try {
            const r = await fetch("/api/session/btw", {
                method: "POST",
                credentials: "same-origin",
                headers: { "Content-Type": "application/json" },
                body: JSON.stringify({ name, question: text, history }),
            });
            const body = r.ok ? await r.json().catch(() => null) : null;
            patch = body && body.state === "ok" && typeof body.answer === "string"
                ? { answer: body.answer }
                : { error: (body && body.reason) || `the server answered ${r.status}` };
        } catch {
            patch = { error: "the network is unavailable" };
        }
        if (mine !== era.current) return;
        setTurns((was) => was.map((t) => (t.key === key ? { ...t, ...patch, waiting: false } : t)));
    };

    return {
        turns,
        open,
        ask,
        hide: () => setOpen(false),
        clear: () => {
            era.current += 1;
            setTurns([]);
        },
    };
}

// SideChat shows the side chat: a card over the feed on a wide screen, beside
// the conversation it asks about, and a sheet on a phone.
export function SideChat({ chat, wide, feedRef }) {
    if (wide) return html`<${SideCard} chat=${chat} feedRef=${feedRef} />`;
    return html`
        <${Sheet} open=${chat.open} onClose=${chat.hide} label="side chat" inner>
            <${SideBody} chat=${chat} />
        <//>
    `;
}

// dock is where the card stands: at the top right of the feed, as tall as the
// feed leaves room for.
function useDock(feedRef, open) {
    const [at, setAt] = useState(null);
    useLayoutEffect(() => {
        if (!open) return undefined;
        const place = () => {
            const feed = feedRef && feedRef.current;
            if (!feed) {
                setAt(null);
                return;
            }
            const r = feed.getBoundingClientRect();
            setAt({
                top: Math.round(r.top + 8),
                right: Math.round(Math.max(8, window.innerWidth - r.right + 14)),
                height: Math.round(Math.max(180, r.height - 16)),
            });
        };
        place();
        window.addEventListener("resize", place);
        return () => window.removeEventListener("resize", place);
    }, [open]);
    return at;
}

function SideCard({ chat, feedRef }) {
    const at = useDock(feedRef, chat.open);
    if (!chat.open) return null;
    const style = at ? `top:${at.top}px;right:${at.right}px;max-height:${at.height}px` : "";
    return html`
        <aside class="btwcard" role="dialog" aria-label="side chat" style=${style}
               onKeyDown=${(e) => { if (e.key === "Escape") chat.hide(); }}>
            <${SideBody} chat=${chat} card />
        </aside>
    `;
}

function SideBody({ chat, card = false }) {
    const [text, setText] = useState("");
    const list = useRef(null);
    const field = useRef(null);
    useEffect(() => {
        const el = list.current;
        if (el) el.scrollTop = el.scrollHeight;
    }, [chat.turns.length, chat.turns.length && chat.turns[chat.turns.length - 1].waiting]);
    useEffect(() => {
        if (chat.open && field.current) field.current.focus();
    }, [chat.open]);

    const send = (e) => {
        e.preventDefault();
        const question = text.trim();
        if (!question) return;
        setText("");
        chat.ask(question);
    };

    return html`
        <div class=${`btwhead${card ? " card" : ""}`}>
            <span class="btwtitle">Side chat</span>
            <button class="iconbtn btwbin" type="button" aria-label="drop the side chat"
                    title="drop the side chat" disabled=${chat.turns.length === 0}
                    onClick=${chat.clear}>${Icon.trash()}</button>
            ${card && html`
                <button class="iconbtn btwclose" type="button" aria-label="close the side chat"
                        onClick=${chat.hide}>${Icon.close()}</button>
            `}
        </div>
        <div class="btwlist" ref=${list}>
            ${chat.turns.length === 0 && html`
                <p class="btwempty">A question aside: claude answers from the conversation, and neither the
                question nor the answer goes into it.</p>
            `}
            ${chat.turns.map((t) => html`
                <div class="btwturn" key=${t.key}>
                    <div class="btwq">${t.question}</div>
                    ${t.waiting && html`<p class="btwwait">thinking…</p>`}
                    ${typeof t.answer === "string" && html`<div class="btwa msg ai">${render(t.answer)}</div>`}
                    ${t.error && html`<p class="btwerr">${t.error}</p>`}
                </div>
            `)}
        </div>
        <form class="btwask" onSubmit=${send}>
            <input ref=${field} type="text" placeholder="Follow up…" aria-label="a question aside"
                   value=${text} onInput=${(e) => setText(e.target.value)} />
            <button class="iconbtn" type="submit" aria-label="ask aside" disabled=${!text.trim()}>
                ${Icon.arrowup()}
            </button>
        </form>
    `;
}
