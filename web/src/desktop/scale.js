// How large the desktop interface is drawn.
//
// The scale lives on the root font size because that is the one place from
// which it reaches everything at once: the columns, the rows, the buttons and
// the type scale are all written in rem, so a single number moves them
// together and no size has to be kept in step by hand. A monitor at arm's
// length and a monitor across the desk want different numbers, and only the
// person in front of it knows which.
//
// The number is put on the root while the desktop shell is mounted and taken
// away with it: the phone layout has no way to change it, and a scale it could
// not undo would follow the same browser to the small screen.
import { useCallback, useEffect, useState } from "preact/hooks";

const KEY = "aacpanel.desktop.scale";

export const MIN = 1;

export const MAX = 1.6;

const STEP = 0.1;

// The interface starts a fifth larger than the drawing calls for: at a desk
// the panel is read from further away than a phone, and every row of it is a
// place where the pointer aims for a small mark next to a destructive one.
const DEFAULT = 1.2;

function round(value) {
    return Math.round(value * 100) / 100;
}

function clamp(value) {
    return Math.min(MAX, Math.max(MIN, round(value)));
}

function read() {
    try {
        const saved = Number(localStorage.getItem(KEY));
        return saved > 0 ? clamp(saved) : DEFAULT;
    } catch {
        return DEFAULT;
    }
}

// useScale returns the scale in force and the two steps that change it.
export function useScale() {
    const [scale, setScale] = useState(read);

    useEffect(() => {
        const root = document.documentElement;
        root.style.setProperty("--ui-scale", String(scale));
        try {
            localStorage.setItem(KEY, String(scale));
        } catch {
        }
        return () => root.style.removeProperty("--ui-scale");
    }, [scale]);

    const bigger = useCallback(() => setScale((v) => clamp(v + STEP)), []);
    const smaller = useCallback(() => setScale((v) => clamp(v - STEP)), []);

    return { scale, bigger, smaller };
}
