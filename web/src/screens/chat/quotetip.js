// The quote button, beside the selection it quotes — or, on a touch screen, at
// the foot of the feed.
//
// It takes no room until there is something to quote: a strip above the
// composer was held on screen for something that happens rarely, and on the
// phone it moved the feed under the thumb at the moment of the tap.

import { useCallback, useEffect, useLayoutEffect, useRef, useState } from "preact/hooks";

import { html } from "../../html.js";
import { Icon } from "../../ui/icons.js";
import { QUOTABLE, quoteOf } from "./quote.js";

// GAP is the distance between the selection and the button, MARGIN the least
// distance the button keeps from any edge of the window.
const GAP = 8;

const MARGIN = 8;

// placeQuoteTip returns where the button goes: centred over the selection, and
// under it when the top of the window is in the way. Either way it stays inside
// the window — a selection at the very edge would otherwise put the button
// where it cannot be pressed.
export function placeQuoteTip(at, size, vw, vh) {
    let x = at.left + at.width / 2 - size.width / 2;
    let y = at.top - GAP - size.height;
    if (y < MARGIN) y = at.bottom + GAP;
    x = Math.min(Math.max(x, MARGIN), Math.max(MARGIN, vw - MARGIN - size.width));
    y = Math.min(Math.max(y, MARGIN), Math.max(MARGIN, vh - MARGIN - size.height));
    return { x: Math.round(x), y: Math.round(y) };
}

// dockQuoteTip returns where the button waits on a touch screen: in the middle
// of the foot of the feed. The system puts its own bar there — copy, select
// all, share — right over the selection, by the very rule the button follows
// with a mouse, and hangs the handles under it; a button beside the selection
// landed under that bar. The foot of the feed is clear of both, and of the
// jump to the end, which keeps to the right.
export function dockQuoteTip(foot, size, vw, vh) {
    const x = Math.min(Math.max(vw / 2 - size.width / 2, MARGIN), Math.max(MARGIN, vw - MARGIN - size.width));
    const y = Math.min(Math.max(foot - GAP - size.height, MARGIN), Math.max(MARGIN, vh - MARGIN - size.height));
    return { x: Math.round(x), y: Math.round(y) };
}

function touchScreen() {
    return typeof window !== "undefined" && typeof window.matchMedia === "function"
        && window.matchMedia("(pointer: coarse)").matches;
}

function inside(node) {
    if (!node) return false;
    const el = node.nodeType === 1 ? node : node.parentElement;
    return Boolean(el && el.closest && el.closest(QUOTABLE));
}

// selectedQuote returns the quote of the current selection and where it lies.
function selectedQuote() {
    const sel = typeof document !== "undefined" ? document.getSelection() : null;
    if (!sel || sel.isCollapsed || !sel.rangeCount) return null;
    if (!inside(sel.anchorNode) || !inside(sel.focusNode)) return null;
    const text = quoteOf(sel.toString());
    if (!text) return null;
    const box = sel.getRangeAt(0).getBoundingClientRect();
    if (!box || (!box.width && !box.height)) return null;
    return {
        text,
        at: { left: box.left, top: box.top, right: box.right, bottom: box.bottom,
              width: box.width, height: box.height },
    };
}

// useSelectionQuote returns the quote of the current selection, following it as
// the feed scrolls: the selection stays where the text is, and so does the
// button that quotes it.
export function useSelectionQuote() {
    const [quote, setQuote] = useState(null);

    useEffect(() => {
        const watch = () => setQuote(selectedQuote());
        document.addEventListener("selectionchange", watch);
        // Scrolling does not bubble, so the feed is listened to on the way down.
        window.addEventListener("scroll", watch, true);
        window.addEventListener("resize", watch);
        watch();
        return () => {
            document.removeEventListener("selectionchange", watch);
            window.removeEventListener("scroll", watch, true);
            window.removeEventListener("resize", watch);
        };
    }, []);

    return quote;
}

// QuoteTip renders the button that puts the selected piece into the composer.
export function QuoteTip({ quote, onQuote }) {
    const node = useRef(null);
    const [spot, setSpot] = useState(null);

    useLayoutEffect(() => {
        if (!quote || !node.current) {
            setSpot(null);
            return;
        }
        const size = node.current.getBoundingClientRect();
        if (touchScreen()) {
            const feed = document.querySelector(".chatfeed");
            const foot = feed ? feed.getBoundingClientRect().bottom : window.innerHeight;
            setSpot(dockQuoteTip(foot, size, window.innerWidth, window.innerHeight));
            return;
        }
        setSpot(placeQuoteTip(quote.at, size, window.innerWidth, window.innerHeight));
    }, [quote]);

    const take = useCallback(() => quote && onQuote(quote.text), [quote, onQuote]);

    if (!quote) return null;
    return html`
        <button
            class=${`quotetip${spot ? " on" : ""}`}
            type="button"
            ref=${node}
            style=${spot ? `transform: translate(${spot.x}px, ${spot.y}px)` : ""}
            aria-label="reply with a quote of the selection"
            onPointerDown=${(e) => e.preventDefault()}
            onClick=${take}
        >${Icon.quote()}<span>quote</span></button>
    `;
}
