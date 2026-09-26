// The conversation header: the name as a marquee, what the session thinks with
// and how much context is eaten.

import { useEffect, useRef, useState } from "preact/hooks";

import { html } from "../../html.js";
import { waitText } from "../../ui/waits.js";

// Marquee renders a heading that does not fit as a running line.
export function Marquee({ text }) {
    const box = useRef(null);
    const [runs, setRuns] = useState(0);

    useEffect(() => {
        const el = box.current;
        if (!el) return undefined;
        const check = () => {
            const over = el.scrollWidth - el.clientWidth;
            const still = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
            setRuns(over > 1 && !still ? over : 0);
        };
        check();
        if (typeof ResizeObserver !== "function") return undefined;
        const ro = new ResizeObserver(check);
        ro.observe(el);
        return () => ro.disconnect();
    }, [text]);

    return html`
        <span class=${`marq${runs ? " runs" : ""}`} ref=${box} title=${text}
              style=${runs ? `--shift:-${runs}px` : ""}>
            <span class="marqin">${text}</span>
        </span>
    `;
}

// modelName returns what the session thinks with.
export function modelName(session) {
    return (session.model || "").replace(/^claude-/, "").replace(/-\d{8}$/, "");
}

// modelTitle names a model the way the client does — "Opus 5.5 · 1M" — and
// not by its id: the id is for machines, and the window rides on it as "[1m]".
export function modelTitle(id, { withWindow = true } = {}) {
    const raw = String(id || "");
    const found = /^(?:claude-)?([a-z]+)-(\d+)(?:-(\d{1,2}))?(?=-\d{8}|\[|$)/i.exec(raw);
    if (!found) return raw;
    const name = found[1][0].toUpperCase() + found[1].slice(1);
    const version = found[3] ? `${found[2]}.${found[3]}` : found[2];
    const wide = withWindow && /\[1m\]/i.test(raw) ? " · 1M" : "";
    return `${name} ${version}${wide}`;
}

// exact renders the same number in full, with digit group separators.
export function exact(n) {
    return String(Math.max(0, Math.round(n || 0))).replace(/\B(?=(\d{3})+(?!\d))/g, "\u202f");
}

// shortPath drops the owner's home from the head of a path: the sessions of a
// machine mostly live under one home, and the rest is what tells them apart.
export function shortPath(cwd) {
    return String(cwd || "").replace(/^\/(?:home|Users)\/[^/]+(?=\/|$)/, "~");
}

// stateOf says how a session stands: the tone its dot is painted with, the
// line a pointer or a screen reader gets, and the word that stands beside the
// dot where a line has no room. still is the time the data stood still at
// (see useAsOf): a state read off a snapshot nobody refreshes is not said as
// if it were now, it is dated.
export function stateOf(live, still = null) {
    if (!live) return { tone: "off", say: "the conversation is gone", word: "closed" };
    if (still !== null) {
        return still
            ? { tone: "off", say: `no fresh data since ${still} — the session may stand otherwise now`, word: `as of ${still}` }
            : { tone: "off", say: "no fresh data — the session may stand otherwise now", word: "not live" };
    }
    if (live.ask || live.waitingFor || live.status === "waiting") {
        return {
            tone: "waiting",
            say: live.ask ? "waiting for an answer to a question" : waitText(live.waitingFor),
            word: "waiting for you",
        };
    }
    if (live.compacting) return { tone: "busy", say: "compacting the conversation", word: "compacting" };
    if (live.status === "busy") return { tone: "busy", say: "handling the request", word: "answering" };
    if (!live.status) return { tone: "", say: "the session state is unknown", word: "" };
    return { tone: "idle", say: "waiting for a message", word: "idle" };
}

// Most of a name the tail keeps when the line is too short for all of it.
const TAIL_MAX = 12;

// splitName parts a name into a head that gives way and a tail that stays.
// The tail is taken in whole words from the end, as many as fit in TAIL_MAX:
// sessions of one project differ at the end of their names, so the end is
// what tells two of them apart.
export function splitName(text) {
    const name = String(text || "");
    const words = name.split(/(?=[-_. ])/);
    let tail = "";
    for (let i = words.length - 1; i > 0; i--) {
        if (tail && tail.length + words[i].length > TAIL_MAX) break;
        tail = words[i] + tail;
    }
    if (!tail || tail.length > TAIL_MAX) tail = name.length > TAIL_MAX ? name.slice(-8) : "";
    return [name.slice(0, name.length - tail.length), tail];
}

// MidName renders a name that does not fit with its middle left out: the head
// is cut, the tail stays whole. Where it fits the two halves read as one.
export function MidName({ text }) {
    const [head, tail] = splitName(text);
    return html`
        <span class="midname" title=${text} aria-label=${text}>
            <span class="midhead" aria-hidden="true">${head}</span>
            ${tail && html`<span class="midtail" aria-hidden="true">${tail}</span>`}
        </span>
    `;
}
