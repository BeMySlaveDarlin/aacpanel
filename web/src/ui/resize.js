import { useEffect, useRef, useState } from "preact/hooks";

import { html } from "../html.js";

// Both edges keep rem rather than pixels: zoom changes the size of the root,
// and a saved column must follow it just as an untouched column does.
export function useColumnWidth({ key, min, max, initial = null, side = "right", half = false, label }) {
    const [width, setWidth] = useState(() => {
        try {
            const saved = Number(localStorage.getItem(key));
            if (Number.isFinite(saved) && saved > 0) return Math.min(max, Math.max(min, saved));
        } catch {
        }
        return initial;
    });
    const ref = useRef(null);
    const stop = useRef(null);
    const [dragging, setDragging] = useState(false);
    useEffect(() => {
        try {
            if (width === null) localStorage.removeItem(key);
            else localStorage.setItem(key, String(width));
        } catch {
        }
    }, [key, width]);
    useEffect(() => () => { if (stop.current) stop.current(); }, []);

    const rem = () => parseFloat(getComputedStyle(document.documentElement).fontSize);
    const clamp = (value) => {
        const ceiling = half && ref.current ? Math.min(max, ref.current.parentElement.clientWidth / rem() / 2) : max;
        return Math.round(Math.min(ceiling, Math.max(Math.min(min, ceiling), value)) * 1000) / 1000;
    };
    const down = (event) => {
        if (event.button !== 0 || !ref.current) return;
        event.preventDefault();
        if (stop.current) stop.current();
        const start = ref.current.getBoundingClientRect().width / rem();
        const x = event.clientX;
        const unit = rem();
        const pointer = event.pointerId;
        const selection = document.body.style.userSelect;
        document.body.style.userSelect = "none";
        setDragging(true);
        const move = (e) => {
            if (e.pointerId !== pointer) return;
            setWidth(clamp(start + (e.clientX - x) / unit * (side === "left" ? -1 : 1)));
        };
        const end = (e) => { if (e.pointerId === pointer) stop.current(); };
        stop.current = () => {
            document.removeEventListener("pointermove", move);
            document.removeEventListener("pointerup", end);
            document.removeEventListener("pointercancel", end);
            window.removeEventListener("blur", stop.current);
            document.body.style.userSelect = selection;
            setDragging(false);
            stop.current = null;
        };
        document.addEventListener("pointermove", move);
        document.addEventListener("pointerup", end);
        document.addEventListener("pointercancel", end);
        window.addEventListener("blur", stop.current);
    };

    const edge = html`<div class=${`dkresize ${side}${dragging ? " dragging" : ""}`} role="separator"
        aria-label=${label} aria-orientation="vertical" aria-valuemin=${min} aria-valuemax=${max}
        aria-valuenow=${width === null ? undefined : width} tabindex="0"
        onPointerDown=${down} onDblClick=${() => setWidth(initial)}
        onKeyDown=${(event) => {
            if (event.key === "Home") setWidth(initial);
            else if (event.key === "ArrowLeft" || event.key === "ArrowRight") {
                const step = (event.key === "ArrowRight" ? 1 : -1) * (side === "left" ? -1 : 1);
                setWidth(clamp(ref.current.getBoundingClientRect().width / rem() + step));
            } else return;
            event.preventDefault();
        }}></div>`;
    return { width, ref, edge };
}
