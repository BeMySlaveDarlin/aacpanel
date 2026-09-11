import { useEffect } from "preact/hooks";

import { html } from "../html.js";

const LIFETIME = 3600;

export function Toast({ toast, onHide }) {
    useEffect(() => {
        if (!toast) return undefined;
        const timer = setTimeout(onHide, LIFETIME);
        return () => clearTimeout(timer);
    }, [toast && toast.at, onHide]);

    return html`
        <div class="toast ${toast ? "on" : ""} ${toast && toast.bad ? "bad" : ""}" role="status" aria-live="polite">
            ${toast && html`
                <span class="tmain">${toast.text}</span>
                ${toast.sub && html`<span class="tsub">${toast.sub}</span>`}
            `}
        </div>
    `;
}
