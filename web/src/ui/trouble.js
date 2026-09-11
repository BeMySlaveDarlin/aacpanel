import { html } from "../html.js";
import { ago } from "../format.js";

export function Trouble({ what, error, hint }) {
    return html`
        <section class="card trouble">
            <h2>${what}</h2>
            <p class="numbers"><span class="dot crit"></span> unavailable</p>
            ${error && html`<p class="sub">${error}</p>`}
            ${hint && html`<p class="hint">${hint}</p>`}
        </section>
    `;
}

const STALE_SEC = 60;

export function Stale({ ageSec }) {
    if (ageSec === null || ageSec === undefined || ageSec < STALE_SEC) return null;

    const minutes = Math.round(ageSec / 60);
    return html`
        <p class="stale-note">
            <span class="dot warn"></span>
            The agent has been silent for ${minutes < 60 ? `${minutes} min` : `${Math.round(minutes / 60)} h`} —
            the numbers below are not current
        </p>
    `;
}

const BLOCK_NAMES = {
    host: "host load",
    disks: "disks",
    net: "network",
    sessions: "sessions",
    probes: "probes",
};

// NotRecorded lists what from the snapshot is not reaching the history.
export function NotRecorded({ faults, blocks }) {
    const mine = (faults || []).filter((f) => !blocks || blocks.includes(f.block));
    if (mine.length === 0) return null;

    return html`
        <section class="card trouble">
            <h2>Not being written to the history</h2>
            ${mine.map(
                (f) => html`
                    <p class="numbers" key=${f.block}>
                        <span class="dot warn"></span>
                        ${BLOCK_NAMES[f.block] || f.block}
                        ${f.count > 1 && html`<span class="sub"> · ${f.count} snapshots in a row</span>`}
                    </p>
                    <p class="sub">started ${ago(f.since)} · ${f.error}</p>
                `,
            )}
            <p class="hint">The live numbers on the screen do not depend on this — the panel reads them from the snapshot itself.</p>
        </section>
    `;
}
