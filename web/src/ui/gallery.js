import { useEffect, useLayoutEffect, useRef } from "preact/hooks";

import { html } from "../html.js";
import { useBackClose } from "./back.js";
import { Icon } from "./icons.js";

export function Gallery({ children, label = "Attachment", onClose, index = 0, total = 1, onPrevious, onNext }) {
    const close = useBackClose(true, onClose);
    const dialog = useRef(null);
    const dismiss = useRef(null);
    const drag = useRef(null);
    const many = total > 1;

    useLayoutEffect(() => {
        const before = document.activeElement;
        const node = dialog.current;
        // The top layer escapes a sheet's transform and backdrop filter.
        node.showModal();
        dismiss.current?.focus({ preventScroll: true });
        return () => {
            node.close();
            if (before?.isConnected) before.focus({ preventScroll: true });
        };
    }, []);

    useEffect(() => {
        const key = (event) => {
            if (!close.isTop()) return;
            if (event.key === "Escape") {
                event.preventDefault();
                event.stopImmediatePropagation();
                close();
                return;
            }
            if (!many || event.target.closest?.("input, textarea, select, video, audio, [contenteditable=true]")) return;
            if (event.key !== "ArrowLeft" && event.key !== "ArrowRight") return;
            event.preventDefault();
            event.stopImmediatePropagation();
            (event.key === "ArrowLeft" ? onPrevious : onNext)?.();
        };
        // A gallery may stand over a sheet; Escape belongs to the top layer.
        document.addEventListener("keydown", key, true);
        return () => document.removeEventListener("keydown", key, true);
    }, [close, many, onPrevious, onNext]);

    const onDown = (event) => {
        drag.current = null;
        if (event.pointerType !== "touch" || !event.isPrimary) return;
        if (event.target.closest?.(".pvstage, button, a, input, textarea, select, video, audio, iframe, table")) return;
        const body = event.target.closest?.(".callbody");
        if (body?.scrollTop > 0) return;
        drag.current = { x: event.clientX, y: event.clientY, id: event.pointerId };
    };
    const onUp = (event) => {
        const start = drag.current;
        drag.current = null;
        if (!start || start.id !== event.pointerId || window.getSelection()?.isCollapsed === false) return;
        const dx = event.clientX - start.x, dy = event.clientY - start.y;
        if (dy > 60 && dy > Math.abs(dx)) close();
        else if (Math.abs(dx) > 60 && Math.abs(dx) > Math.abs(dy)) (dx > 0 ? onPrevious : onNext)?.();
    };

    return html`
        <dialog class="pv" ref=${dialog} role="dialog" aria-modal="true" aria-label=${label}
                onPointerDown=${(event) => event.stopPropagation()}
                onCancel=${(event) => { event.preventDefault(); close(); }}>
            <div class="pvbackdrop" aria-hidden="true" onClick=${close}></div>
            <div class="pvpanel"
                 onPointerDown=${onDown} onPointerUp=${onUp} onPointerCancel=${() => { drag.current = null; }}>
                <button class="pvclose" type="button" aria-label="Close attachment" ref=${dismiss} onClick=${close}>${Icon.close()}</button>
                ${children}
                ${many && html`<div class="pvcount" aria-live="polite">${index + 1} / ${total}</div>`}
            </div>
            ${many && html`
                <button class="pvnav previous" type="button" aria-label="Previous attachment" disabled=${!onPrevious} onClick=${onPrevious}>${Icon.chevron()}</button>
                <button class="pvnav next" type="button" aria-label="Next attachment" disabled=${!onNext} onClick=${onNext}>${Icon.chevron()}</button>
            `}
        </dialog>
    `;
}
