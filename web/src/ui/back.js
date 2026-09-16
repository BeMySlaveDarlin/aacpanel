// The phone back gesture closes the topmost open layer, not the application.
import { useCallback, useEffect, useRef } from "preact/hooks";

import { html } from "../html.js";
import { Icon } from "./icons.js";

let stack = [];
let collapsing = 0;

// How deep the entry the browser is standing on was pushed. The history itself
// carries the number, so nothing here has to keep a count in step with it: a
// count and a history part company the moment layers change hands in one turn,
// and then a swipe either walks past the application or closes nothing at all.
function depthNow() {
    const state = window.history.state;
    if (!state || !state.overlay) return 0;
    const depth = Number(state.depth);
    return Number.isFinite(depth) && depth > 0 ? depth : 1;
}

if (typeof window !== "undefined") {
    window.addEventListener("popstate", () => {
        // A step back this module took itself, to give away the entry of a
        // layer that has gone: it is not the reader asking for anything.
        if (collapsing > 0) {
            collapsing -= 1;
            return;
        }
        const top = stack[stack.length - 1];
        if (top) top.close();
    });
}

function closeFrom(layer) {
    const at = stack.indexOf(layer);
    if (at < 0) return;
    for (const above of stack.slice(at).reverse()) above.close();
}

// useBackClose registers a layer opened above the screen with the back gesture.
export function useBackClose(open, onClose) {
    const close = useRef(onClose);
    close.current = onClose;

    const mine = useRef(null);

    useEffect(() => {
        if (!open) return undefined;

        const layer = { close: () => close.current() };
        mine.current = layer;
        stack.push(layer);
        const depth = stack.length;
        // An entry of its own, unless the layer it replaced left one standing
        // at this depth for it.
        if (depthNow() < depth) {
            window.history.pushState({ overlay: true, depth }, "");
        }

        // A layer gives its entry back as it goes, and it does so at once. Left
        // to the next frame, the count and the history part company whenever
        // layers change hands in one turn — a sheet and a conversation closing
        // while a page opens — and the entry a new layer was counting on is
        // taken out from under it. The first swipe then walks past the
        // application instead of putting down what is open.
        // A layer gives its entry back as it goes, and only the entry that is
        // its own: the depth in the history says which one that is. Left to the
        // next frame, or counted rather than read, this is where the count and
        // the history part company — a sheet and a conversation closing while a
        // page opens, and the entry the new layer was counting on goes with
        // them.
        return () => {
            const wasTop = stack[stack.length - 1] === layer;
            stack = stack.filter((was) => was !== layer);
            if (!wasTop || depthNow() !== depth) return;
            collapsing += 1;
            window.history.back();
        };
    }, [open]);

    return useCallback(() => closeFrom(mine.current), []);
}

// BackHead renders the header of a layer page: the exit arrow and its title.
export function BackHead({ onBack, label, foot, tools, children }) {
    return html`
        <div class="phead">
            <button class="pback" type="button" onClick=${onBack} aria-label=${label || "back"}>
                <span class="chev back">${Icon.chevron()}</span>
            </button>
            <div class="pheadbody">${children}</div>
            <div class="pheadtools">${tools}</div>
            ${foot}
        </div>
    `;
}
