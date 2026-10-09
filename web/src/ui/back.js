// The phone back gesture closes the topmost open layer, not the application.
import { useCallback, useLayoutEffect, useRef } from "preact/hooks";

import { html } from "../html.js";
import { Icon } from "./icons.js";

let stack = [];
// Steps back this module asked of the browser that have not landed yet.
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

// sync brings the history to one entry per open layer: the browser stands on
// the entry pushed at the depth of the stack. Entries belong to no layer and
// only their number is kept, so layers changing hands in one turn — a sheet and
// a conversation going while a page opens — cannot take the page's entry with
// them: the page stands on whatever entry is at its depth once the history
// settles.
//
// A step back lands when the browser gets to it, and until then history.state
// still names the entry that is going. A depth read in that window is the old
// one, and acting on it either keeps an entry no layer stands on — a swipe
// that closes nothing — or gives one back twice, and the swipe walks past the
// application. So while a step of this module is on its way sync does nothing,
// and the popstate that lands the step calls it again with the stack as it is
// by then. The price: a layer opened in that window gets its entry only after
// the step lands, and a swipe made while a step is on its way is taken for it.
function sync() {
    if (collapsing > 0) return;
    const have = depthNow();
    const want = stack.length;
    for (let depth = have + 1; depth <= want; depth += 1) {
        window.history.pushState({ overlay: true, depth }, "");
    }
    if (have > want) {
        collapsing += 1;
        window.history.go(want - have);
    }
}

if (typeof window !== "undefined") {
    window.addEventListener("popstate", () => {
        // A step back this module took itself has landed: it is not the reader
        // asking for anything, and the history can be read again — whatever
        // opened or went while the step was on its way is brought in now.
        if (collapsing > 0) {
            collapsing -= 1;
            sync();
            return;
        }
        const top = stack[stack.length - 1];
        if (!top) return;
        // A layer holding something unsaved asks before it goes: the entry
        // the gesture took is given back, so the layer stays where it was.
        if (top.hold()) {
            sync();
            return;
        }
        top.close();
        // A layer may put down only what is open on it and stay — a page
        // closing the document on it — while the gesture has taken its entry
        // all the same. Once the close is drawn the history is brought in: a
        // layer that went changes nothing, one that stayed gets its entry
        // back, or the next swipe walks out of the application. Preact draws a
        // change of state in a microtask and a layer lets go of the gesture
        // within that render, so a timer of zero comes after both. The price:
        // a layer whose close finishes later than that gets its entry back
        // first and gives it back when it does go.
        setTimeout(sync, 0);
    });
}

function closeFrom(layer) {
    const at = stack.indexOf(layer);
    if (at < 0) return;
    for (const above of stack.slice(at).reverse()) above.close();
}

// useBackClose registers a layer opened above the screen with the back gesture.
// hold, when given, is asked first: true keeps the layer open — it has put up
// its own question about what would be lost.
export function useBackClose(open, onClose, hold) {
    const close = useRef(onClose);
    close.current = onClose;
    const keep = useRef(hold);
    keep.current = hold;

    const mine = useRef(null);

    // The claim is made and taken back in the commit of a render, not after
    // the paint: a sheet that stays drawn closed lets go of the gesture in the
    // render that closed it, so the sync after a gesture finds it gone rather
    // than giving it an entry and taking that back a frame later. The price:
    // whatever else pushes entries has to do it before the layers are drawn,
    // or its entries land above theirs.
    useLayoutEffect(() => {
        if (!open) return undefined;

        const layer = { close: () => close.current(), hold: () => Boolean(keep.current && keep.current()) };
        mine.current = layer;
        stack.push(layer);
        sync();

        // A layer that goes takes the history down with it, whether it was on
        // top or under another: an entry left standing above the open layers
        // is a swipe that closes nothing.
        return () => {
            stack = stack.filter((was) => was !== layer);
            sync();
        };
    }, [open]);

    const closer = useCallback(() => closeFrom(mine.current), []);
    // isTop says whether this layer is the one a back gesture or Escape is
    // for: several layers stand side by side on a wide screen, and a key
    // meant for the rightmost must not close the ones under it too.
    closer.isTop = () => stack[stack.length - 1] === mine.current;
    return closer;
}

// BackHead renders the header of a layer page: the exit arrow and its title.
// A kind names a header laid out its own way.
export function BackHead({ onBack, label, foot, tools, children, kind = "" }) {
    return html`
        <div class=${`phead${kind ? ` ${kind}` : ""}`}>
            <button class="pback" type="button" onClick=${onBack} aria-label=${label || "back"}>
                <span class="chev back">${Icon.chevron()}</span>
            </button>
            <div class="pheadbody">${children}</div>
            <div class="pheadtools">${tools}</div>
            ${foot}
        </div>
    `;
}
