// The “reply with a quote” button above the composer.

import { useEffect, useState } from "preact/hooks";

import { html } from "../../html.js";
import { Icon } from "../../ui/icons.js";
import { QUOTABLE, quoteOf } from "./quote.js";

function selectedQuote() {
    const sel = typeof document !== "undefined" ? document.getSelection() : null;
    if (!sel || sel.isCollapsed || !sel.rangeCount) return null;
    if (!inside(sel.anchorNode) || !inside(sel.focusNode)) return null;
    return quoteOf(sel.toString()) || null;
}

function inside(node) {
    if (!node) return false;
    const el = node.nodeType === 1 ? node : node.parentElement;
    return Boolean(el && el.closest && el.closest(QUOTABLE));
}

// useSelectionQuote returns the quote of the current selection.
export function useSelectionQuote() {
    const [quote, setQuote] = useState(null);
    useEffect(() => {
        const watch = () => setQuote(selectedQuote());
        document.addEventListener("selectionchange", watch);
        watch();
        return () => document.removeEventListener("selectionchange", watch);
    }, []);
    return quote;
}

// QuoteBar renders the bar above the composer that puts the quote into the field.
export function QuoteBar({ quote, onQuote }) {
    if (!quote) return null;
    const peek = quote.split("\n")[0].replace(/^> /, "");
    return html`
        <button class="workbar status quotebar" type="button"
                aria-label="reply with a quote of the selection"
                onPointerDown=${(e) => e.preventDefault()}
                onClick=${() => onQuote(quote)}>
            <span class="quoteico">${Icon.quote()}</span>
            <span class="quotelabel">reply with a quote</span>
            <span class="quotepeek">${peek}</span>
        </button>
    `;
}
