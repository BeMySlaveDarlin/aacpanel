import { html } from "../html.js";
import { bytes, duration } from "../format.js";

function Run({ run, waiting, now }) {
    const dir = (run.dir || "").split("/").filter(Boolean).slice(-2).join("/");
    const elapsed = run.since ? duration(Math.max(0, now - run.since)) : "time unknown";
    return html`
        <div class="panelrow" data-heavy=${waiting ? "waiting" : "running"}>
            <div>
                <strong>${waiting ? `${run.place}. ` : ""}${run.name || run.unit || "heavy run"}</strong>
                <p class="hint">${[run.session, dir].filter(Boolean).join(" · ") || "session unknown"}</p>
                <p class="hint">asked ${run.ask ? bytes(run.ask) : "no ceiling"}${!waiting && ` · holds ${bytes(run.taken)}`} · ${waiting ? "waiting" : "running"} ${elapsed}</p>
            </div>
        </div>
    `;
}

export function HeavyRuns({ queue, error }) {
    if (!queue && !error) return null;
    const running = queue?.running || [];
    const waiting = queue?.waiting || [];
    const now = Date.now() / 1000;
    return html`
        <section class="card heavy" aria-label="Heavy runs">
            <h3>Heavy runs</h3>
            ${error ? html`<p class="hint">Queue unavailable: ${error}</p>` : html`
                <p class="hint">${running.length} of ${queue.slots || "unlimited"} slots occupied${!running.length && !waiting.length ? " · no heavy runs" : ""}</p>
                ${running.map((run) => html`<${Run} key=${run.unit} run=${run} now=${now} />`)}
                ${waiting.map((run) => html`<${Run} key=${run.place} run=${run} waiting=${true} now=${now} />`)}
                ${waiting.length > 0 && html`<p class="hint" data-heavy="head">${queue.head ? `First run waits for ${queue.head}` : "First run is being checked for admission"}</p>`}
            `}
        </section>
    `;
}
