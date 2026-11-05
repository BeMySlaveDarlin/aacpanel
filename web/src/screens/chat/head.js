// The conversation header: the name as a marquee, what the session thinks with,
// how much context is eaten and how the session stands — in the words of its
// card in the list of sessions.

import { useEffect, useRef, useState } from "preact/hooks";

import { html } from "../../html.js";
import { plural } from "../../format.js";
import { backgroundOf, stateOf as cardState, timeOf } from "../sessions/state.js";

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

// The tone of the head's dot by the tone of the session's state.
const HEAD_TONE = { wait: "waiting", held: "held", busy: "busy", bg: "bg", limit: "limit", paused: "paused", idle: "idle" };

// stateOf says how a session stands in the head of its conversation, in the
// words its card in the lists says it with (see sessions/state.js): the tone
// its dot is painted with, the line a pointer or a screen reader gets, and
// the word that stands beside the dot. The head has no corner for the time,
// so a state that ends at a known moment says it in its word. still is the
// time the data stood still at (see useAsOf): a state read off a snapshot
// nobody refreshes is not said as if it were now, it is dated.
export function stateOf(row, still = null, move = null, limits = null) {
    // A session on its way to tmux or the stream is gone from the snapshot for
    // a while, and that is the move, not the end of it.
    if (move) {
        return { tone: "busy", say: move.to === "console" ? "moving to tmux" : "moving to the stream", word: "moving" };
    }
    if (!row) return { tone: "off", say: "the conversation is gone", word: "closed" };
    const state = cardState(row, limits, { viewed: true });
    if (still !== null && state.kind !== "restarting") {
        return still
            ? { tone: "off", say: `no fresh data since ${still} — the session may stand otherwise now`, word: `as of ${still}` }
            : { tone: "off", say: "no fresh data — the session may stand otherwise now", word: "not live" };
    }
    if (!row.status && state.tone === "idle") return { tone: "", say: "the session state is unknown", word: "" };
    const time = timeOf(state);
    const word = state.until && time.text ? `${state.text} · ${state.clock ? "until" : "in"} ${time.text}` : state.text;
    return { tone: HEAD_TONE[state.tone] || "", say: state.work ? `${word} · ${state.work}` : word, word };
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

// stopsOf names what a close or a restart of a live session ends with it: its
// background tasks, agents and workflows at work, as the snapshot counts them.
export function stopsOf(s) {
    const w = (s && s.work) || {};
    const out = [];
    const tasks = backgroundOf(w);
    if (tasks > 0) out.push(`${tasks} ${plural(tasks, "background task", "background tasks")}`);
    if (w.agents > 0) out.push(`${w.agents} ${plural(w.agents, "agent", "agents")}`);
    if (w.workflows > 0) out.push(`${w.workflows} ${plural(w.workflows, "workflow", "workflows")}`);
    return out.join(", ");
}
