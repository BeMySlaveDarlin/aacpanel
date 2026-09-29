// The terminals of places: shells in tmux the host keeps in the home directory
// and in the projects of the map, listed by /api/terms.
//
// The list is only read here. Starting, closing and renaming a terminal are
// actions and go through the gate.
import { useCallback, useEffect, useRef, useState } from "preact/hooks";

// How often an open screen of terminals asks again: a tab renames itself after
// the command it runs, and a line of "typed" would stand still otherwise.
const POLL_MS = 5000;

// list returns every terminal the host keeps, sorted by place and then by the
// time it was started.
export async function list() {
    const r = await fetch("/api/terms", { credentials: "same-origin" });
    if (!r.ok) throw new Error(`the terminals did not come (${r.status})`);
    const body = await r.json();
    return Array.isArray(body.terms) ? body.terms : [];
}

// useTerms keeps the list fresh while the screen that asks is on: terms is
// null until the first answer, and reload asks at once — after an action whose
// consequence the screen is about to open.
export function useTerms(on = true) {
    const [state, setState] = useState({ terms: null, error: "" });
    const alive = useRef(true);

    const reload = useCallback(async () => {
        try {
            const terms = await list();
            if (alive.current) setState({ terms, error: "" });
            return terms;
        } catch (err) {
            if (alive.current) setState((was) => ({ terms: was.terms, error: String(err.message || err) }));
            return null;
        }
    }, []);

    useEffect(() => {
        alive.current = true;
        if (!on) return () => { alive.current = false; };
        reload();
        const timer = setInterval(() => {
            if (document.visibilityState === "visible") reload();
        }, POLL_MS);
        return () => {
            alive.current = false;
            clearInterval(timer);
        };
    }, [on, reload]);

    return { ...state, reload };
}

// samePlace compares two directories the way the host does: a trailing slash
// does not make another place.
export function samePlace(a, b) {
    const clean = (p) => String(p || "").replace(/\/+$/, "");
    return Boolean(a) && Boolean(b) && clean(a) === clean(b);
}

// latestIn is the terminal of a place typed into last, or null.
export function latestIn(terms, place) {
    let best = null;
    for (const t of terms || []) {
        if (!samePlace(t.place, place)) continue;
        if (!best || (t.activity || 0) > (best.activity || 0)) best = t;
    }
    return best;
}

// SHELLS are the commands a tab shows while nothing runs in it: tmux names a
// tab after what runs in its pane, and a shell waiting at its prompt is the
// shell itself.
const SHELLS = new Set(["bash", "zsh", "fish", "sh", "dash", "ksh", "mksh", "tcsh", "csh", "nu", "elvish", "xonsh"]);

// runs says whether something other than the shell runs in the terminal.
export function runs(t) {
    const cmd = String((t && t.command) || "").replace(/^-/, "");
    return Boolean(cmd) && !SHELLS.has(cmd);
}

// tabName is what a tab is called: the name given to it, or what runs in it.
export function tabName(t) {
    return (t && (t.name || t.command)) || "shell";
}

// typed says how long ago the terminal was typed into, in the few characters a
// card has for it.
export function typed(activity, now = Date.now()) {
    if (!activity) return "";
    const sec = Math.max(0, Math.round(now / 1000 - activity));
    if (sec < 60) return "typed now";
    if (sec < 3600) return `typed ${Math.floor(sec / 60)}m`;
    if (sec < 86400) return `typed ${Math.floor(sec / 3600)}h`;
    return `typed ${Math.floor(sec / 86400)}d`;
}
