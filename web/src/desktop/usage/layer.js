// Layers above the home screen: detailed usage breakdowns and the collection.
import { useEffect } from "preact/hooks";

import { html } from "../../html.js";
import { Icon } from "../../ui/icons.js";

// Layer is the layer shell: a header with the title and a body that scrolls.
export function Layer({ title, note, trail, actions, onClose, children }) {
    useEffect(() => {
        const onKey = (e) => {
            if (e.key === "Escape") {
                e.preventDefault();
                e.stopPropagation();
                onClose();
            }
        };
        window.addEventListener("keydown", onKey, true);
        return () => window.removeEventListener("keydown", onKey, true);
    }, [onClose]);

    return html`
        <div class="uslayer" role="dialog" aria-label=${title}>
            <header class="uslayerhead">
                <span class="uslayertitle">${title}</span>
                ${note && html`<span class="uslayernote">${note}</span>`}
                ${trail}
                <span class="uslayeracts">${actions}</span>
                <button class="dkclose" type="button" onClick=${onClose}><${Icon.close} /></button>
            </header>
            <div class="uslayerbody">${children}</div>
        </div>
    `;
}

// Trail renders the breadcrumbs of a breakdown.
export function Trail({ steps, onPick }) {
    return html`
        <nav class="ustrail">
            ${steps.map((step, i) => html`
                <span key=${step.label + i}>
                    ${i > 0 && html`<i class="ustrailsep">/</i>`}
                    ${i === steps.length - 1
                        ? html`<span class="ustrailhere">${step.label}</span>`
                        : html`<button class="ustrailstep" type="button" onClick=${() => onPick(i)}>${step.label}</button>`}
                </span>
            `)}
        </nav>
    `;
}
