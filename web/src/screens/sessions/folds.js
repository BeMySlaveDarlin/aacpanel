// Which folds of opened sessions the person closed. A fold stands open until
// the person closes it, and stays closed for its parent across a reload: the
// sessions a session opened are the work in view, and a list that folded them
// on its own would hide one that waits. Kept by the name of the parent, the
// way the link itself is kept; the latest few are kept, so a name that comes
// back long after is open again.

import { useCallback, useState } from "preact/hooks";

const FOLDS_KEY = "aacpanel.folds";

const FOLDS_KEPT = 32;

function readFolds() {
    try {
        const list = JSON.parse(localStorage.getItem(FOLDS_KEY) || "[]");
        return new Set(Array.isArray(list) ? list.filter((name) => typeof name === "string") : []);
    } catch {
        return new Set();
    }
}

function keepFolds(folded) {
    try {
        localStorage.setItem(FOLDS_KEY, JSON.stringify([...folded].slice(-FOLDS_KEPT)));
    } catch {
    }
}

// useFolds returns the parents whose fold is closed, and the toggle of one.
export function useFolds() {
    const [folded, setFolded] = useState(readFolds);
    const toggle = useCallback((name) => setFolded((prev) => {
        const next = new Set(prev);
        if (next.has(name)) next.delete(name);
        else next.add(name);
        keepFolds(next);
        return next;
    }), []);
    return [folded, toggle];
}
