// What to watch a live session with: the feed or its real screen. The choice
// is the session's own, kept on this device by the session's name: a session
// in tmux watched as a feed stays a feed, and every session opens the way it
// was last watched here, or by the width of the screen when it never was.

import { useCallback, useMemo, useState } from "preact/hooks";

export const VIEW_KEY = "aacpanel.chat.views";

const VIEWS = ["term", "feed"];

// A ceiling, so sessions long gone do not pile up in the storage. The oldest
// choices go first, and a session that falls out opens by the width again.
const VIEWS_MAX = 200;

function readAll(storage) {
    try {
        const raw = JSON.parse(storage.getItem(VIEW_KEY) || "[]");
        return Array.isArray(raw)
            ? raw.filter((one) => Array.isArray(one) && typeof one[0] === "string" && VIEWS.includes(one[1]))
            : [];
    } catch {
        return [];
    }
}

// readView returns what was chosen for the session last time, or "" if
// nothing was.
export function readView(name, storage = localStorage) {
    const one = readAll(storage).find(([who]) => who === name);
    return one ? one[1] : "";
}

// saveView remembers the choice for the session on this device.
export function saveView(name, view, storage = localStorage) {
    if (!name || !VIEWS.includes(view)) return;
    const all = readAll(storage).filter(([who]) => who !== name);
    all.push([name, view]);
    try {
        storage.setItem(VIEW_KEY, JSON.stringify(all.slice(-VIEWS_MAX)));
    } catch {
    }
}

// viewOf returns what to show: the choice, the default or the feed.
export function viewOf(saved, canTerm, wide) {
    if (!canTerm) return "feed";
    if (VIEWS.includes(saved)) return saved;
    return wide ? "term" : "feed";
}

// useViewPick returns the view to show for the session and how to change it.
// A pick stands on this screen even where the storage refuses to keep it.
export function useViewPick(name, canTerm, wide) {
    const [picks, setPicks] = useState(() => new Map());
    const stored = useMemo(() => readView(name), [name]);
    const pick = useCallback((next) => {
        setPicks((was) => new Map(was).set(name, next));
        saveView(name, next);
    }, [name]);
    return [viewOf(picks.has(name) ? picks.get(name) : stored, canTerm, wide), pick];
}
