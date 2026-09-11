// Container logs in the right-hand panel of the Containers section.
import { html } from "../../html.js";
import { useLogs } from "../../logs.js";

export function Logs({ container }) {
    const { lines, state, error } = useLogs(container ? container.id : null, 200);
    if (!container) return html`<p class="dkempty">no container picked</p>`;
    const timed = lines.some((l) => l.time);
    return html`
        <div class="dklogs">
            ${state === "connecting" && html`<p class="dkempty">connecting to the stream…</p>`}
            ${error && html`<p class="dkempty">the stream broke off: ${error}</p>`}
            ${lines.map((l) => html`
                <div class=${`dklogline${l.stream === "stderr" ? " err" : ""}`} key=${l.key}>
                    ${timed && html`<span class="dklogtime">${l.time}</span>`}
                    <span class="dklogmsg">${l.line}</span>
                </div>
            `)}
            ${state === "live" && lines.length === 0 && html`<p class="dkempty">the container is silent</p>`}
        </div>
    `;
}
