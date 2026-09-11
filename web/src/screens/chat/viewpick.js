// What to watch a live session with: the feed or its real screen.

import { useCallback, useState } from "preact/hooks";

export const VIEW_KEY = "aacpanel.chat.view";

const VIEWS = ["term", "feed"];

// readView returns what was chosen last time, or "" if nothing was.
export function readView(storage = localStorage) {
    try {
        const saved = storage.getItem(VIEW_KEY);
        return VIEWS.includes(saved) ? saved : "";
    } catch {
        return "";
    }
}

// saveView remembers the choice on this device.
export function saveView(view, storage = localStorage) {
    try {
        storage.setItem(VIEW_KEY, view);
    } catch {
    }
}

// viewOf returns what to show: the choice, the default or the feed.
export function viewOf(saved, canTerm, wide) {
    if (!canTerm) return "feed";
    if (VIEWS.includes(saved)) return saved;
    return wide ? "term" : "feed";
}

// useViewPick returns the view to show and how to change it.
export function useViewPick(canTerm, wide) {
    const [saved, setSaved] = useState(readView);
    const pick = useCallback((next) => {
        setSaved(next);
        saveView(next);
    }, []);
    return [viewOf(saved, canTerm, wide), pick];
}
