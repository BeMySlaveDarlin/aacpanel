// Choice is a pick the confirmation sheet asks beside its question: a row of
// segments with the taken one lit and the one the project names marked, and
// under it why a segment is switched off — on a phone there is no hover to
// tell it, and a segment that only refuses to press reads as a broken one.
import { html } from "../html.js";

export function Choice({ choice, value, onPick }) {
    const off = choice.options.filter((o) => o.off);
    return html`
        <div class="gtchoice">
            <div class="pkscope" role="radiogroup" aria-label=${choice.label}>
                ${choice.options.map((o) => html`
                    <button key=${o.value} type="button" role="radio"
                            class=${`pkseg${value === o.value ? " on" : ""}`}
                            aria-checked=${value === o.value ? "true" : "false"}
                            disabled=${Boolean(o.off)}
                            onClick=${() => onPick(o.value)}>
                        ${o.label}${o.mark && html`<span class="gtmark">${o.mark}</span>`}
                    </button>
                `)}
            </div>
            ${off.map((o) => html`<p key=${o.value} class="hint">${o.label} is off — ${o.off}</p>`)}
        </div>
    `;
}
