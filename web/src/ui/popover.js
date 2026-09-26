// A panel that drops from the control that opened it, on the wide screen.
// It closes on a press anywhere outside that control and on Escape; the
// control itself toggles it, so the press that opened it does not count as
// one outside.

import { useEffect, useRef } from "preact/hooks";

import { html } from "../html.js";

export function Popover({ open, onClose, label, children }) {
    const box = useRef(null);

    useEffect(() => {
        if (!open) return undefined;
        const outside = (event) => {
            const anchor = box.current && box.current.parentElement;
            if (anchor && !anchor.contains(event.target)) onClose();
        };
        const key = (event) => {
            if (event.key === "Escape") onClose();
        };
        document.addEventListener("pointerdown", outside, true);
        document.addEventListener("keydown", key);
        return () => {
            document.removeEventListener("pointerdown", outside, true);
            document.removeEventListener("keydown", key);
        };
    }, [open, onClose]);

    if (!open) return null;
    return html`<div class="dkpop" role="dialog" aria-label=${label} ref=${box}>${children}</div>`;
}
