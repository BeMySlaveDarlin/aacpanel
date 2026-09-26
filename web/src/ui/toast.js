import { html } from "../html.js";

// Toast draws the note the host holds; when and how long it shows is the host's call.
export function Toast({ toast, onAct }) {
    const act = toast && toast.act;
    return html`
        <div class="toast ${toast ? "on" : ""} ${toast && toast.bad ? "bad" : ""}" role="status" aria-live="polite">
            ${toast && html`
                <span class="tmain">${toast.text}</span>
                ${toast.sub && html`<span class="tsub">${toast.sub}</span>`}
                ${act && html`
                    <button class="btn tact" type="button"
                            onClick=${() => { if (onAct) onAct(); act.run(); }}>${act.label}</button>
                `}
            `}
        </div>
    `;
}
