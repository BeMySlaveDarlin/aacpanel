// The conversation header: the name as a marquee, what the session thinks with
// and how much context is eaten.

import { useEffect, useRef, useState } from "preact/hooks";

import { html } from "../../html.js";

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

// short renders a context size in human figures.
export function short(n) {
    if (!n) return "0";
    if (n >= 1000000) {
        const m = n / 1000000;
        return `${Number.isInteger(m) ? m : m.toFixed(1)}M`;
    }
    if (n >= 1000) return `${Math.round(n / 1000)}k`;
    return String(n);
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

// ChatPath says where the session works, on the third line of the header. A
// path too long for the line keeps its end — the project is its last word —
// and a tap shows the whole of it.
export function ChatPath({ cwd }) {
    const [whole, setWhole] = useState(false);
    if (!cwd) return null;
    return html`
        <button class=${`chatpath${whole ? " whole" : ""}`} type="button" title=${cwd}
                aria-label=${`the directory of the session: ${cwd}`} aria-expanded=${whole ? "true" : "false"}
                onClick=${() => setWhole(!whole)}>
            <span class="chatpathtext"><bdi>${whole ? cwd : shortPath(cwd)}</bdi></span>
        </button>
    `;
}
