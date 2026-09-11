import { useCallback, useEffect, useRef, useState } from "preact/hooks";

import { html } from "../html.js";

export function Chips({ items, current, onSelect }) {
    const box = useRef(null);
    const [edges, setEdges] = useState({ left: false, right: false });

    const measure = useCallback(() => {
        const el = box.current;
        if (!el) return;
        const slack = el.scrollWidth - el.clientWidth;
        setEdges({
            left: el.scrollLeft > 1,
            right: slack > 1 && el.scrollLeft < slack - 1,
        });
    }, []);

    useEffect(() => {
        measure();
        const el = box.current;
        if (!el || typeof ResizeObserver !== "function") return undefined;
        const ro = new ResizeObserver(measure);
        ro.observe(el);
        return () => ro.disconnect();
    }, [measure, items]);

    const cls = `chips${edges.left ? " more-left" : ""}${edges.right ? " more-right" : ""}`;
    return html`
        <div class=${cls} ref=${box} onScroll=${measure}>
            ${items.map((chip) => html`
                <button
                    key=${chip.id}
                    class="chip"
                    type="button"
                    aria-pressed=${current === chip.id ? "true" : "false"}
                    onClick=${() => onSelect(chip.id)}
                >
                    ${chip.label}${chip.count !== undefined && html`<span class="n">${chip.count}</span>`}
                </button>
            `)}
        </div>
    `;
}
