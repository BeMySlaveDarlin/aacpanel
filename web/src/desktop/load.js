import { html } from "../html.js";
import { Gauge, headLoad } from "../ui/load.js";

// HeadLoad draws what the machine is doing right now, at the end of the top bar.
export function HeadLoad({ snapshot, onOpen }) {
    const load = headLoad(snapshot);
    if (!load) return null;
    return html`
        <button class="dkload" type="button" aria-label="Machine" onClick=${onOpen}>
            <${Gauge} name="cpu" value=${load.cpu} tip="processor across all cores" />
            <${Gauge} name="mem" value=${load.mem} tip="memory in use" />
            <${Gauge} name="disk" value=${load.disk} tip=${`the fullest disk: ${load.mount}`} />
        </button>
    `;
}
