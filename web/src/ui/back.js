// The phone back gesture closes the topmost open layer, not the application.
import { useCallback, useEffect, useRef } from "preact/hooks";

import { html } from "../html.js";
import { Icon } from "./icons.js";

let stack = [];
let entries = 0;
let collapsing = 0;

const mark = { overlay: true };

function ours() {
    return Boolean(window.history.state && window.history.state.overlay);
}

if (typeof window !== "undefined") {
    window.addEventListener("popstate", () => {
        if (collapsing > 0) {
            collapsing -= 1;
            return;
        }
        entries = Math.max(0, entries - 1);
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
        if (entries < stack.length) {
            window.history.pushState(mark, "");
            entries += 1;
        }

        return () => {
            stack = stack.filter((was) => was !== layer);
            requestAnimationFrame(() => {
                if (stack.length < entries && ours()) {
                    entries -= 1;
                    collapsing += 1;
                    window.history.back();
                }
            });
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
