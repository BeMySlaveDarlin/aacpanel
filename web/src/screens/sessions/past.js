// The session archive: a page with the list of conversations and the fill chart.

import { useEffect, useState } from "preact/hooks";

import { html } from "../../html.js";
import { BackHead, useBackClose } from "../../ui/back.js";
import { plural } from "../../format.js";
import { Icon } from "../../ui/icons.js";
import { area, Chart } from "../../chart.js";
import { bucketText, PERIODS, useSessionsArchive, useSessionsHistory } from "../../history.js";
import { Chips } from "../../ui/chips.js";
import { PastRow, stamp, when } from "./card.js";
import { PastActions } from "./actions.js";

const PAGE = 20;

export function Past({ profile = "", contour = 0, onBack, exec, onDone, chat }) {
    useBackClose(true, onBack);

    const [period, setPeriod] = useState("7d");
    const [offset, setOffset] = useState(0);
    const state = useSessionsArchive({ limit: PAGE, offset, profile, contour });
    const archive = state.kind === "ready" ? state.archive : null;
    const rows = (archive && archive.rows) || [];
    const crowdState = useSessionsHistory(period, { limit: 1 });
    const history = crowdState.kind === "ready" ? crowdState.sessions : null;
    const peaks = peakSeries(history);
    const crowd = busiest(history);

    const choosePeriod = (id) => setPeriod(id);

    useEffect(() => setOffset(0), [profile]);

    return html`
        <${BackHead} onBack=${onBack} label="to sessions">
            <h2>Session archive</h2>
            <span class="where">${profile ? `${profile} · ` : ""}one conversation per line · fresh on top</span>
        <//>

        <section class="card">
            <${Chips}
                items=${PERIODS.filter((p) => p.id === "24h" || p.id === "7d" || p.id === "30d")}
                current=${period}
                onSelect=${choosePeriod}
            />

            ${state.kind === "loading" && html`<p class="hint">Reading the transcripts…</p>`}
            ${state.kind === "unavailable" && html`<p class="hint warn">${state.error}</p>`}
            ${state.kind === "failed" && html`<p class="hint crit">${state.error}</p>`}
            ${archive && rows.length === 0 && html`<p class="hint">${profile
                ? `There are no conversations of contour ${profile} on disk.`
                : "There are no conversations on disk."}</p>`}

            ${peaks && html`
                <${Chart} data=${peaks} series=${PEAK} height=${78} yFormat=${(v) => `${Math.round(v)}%`} />
            `}
            ${history && peaks && html`
                <p class="hint">${bucketText(history.resolution, history.stepSec)} · the fill peak in a slice</p>
            `}
            ${crowd > 1 && html`
                <p class="sub">up to ${crowd} ${plural(crowd, "session", "sessions")} ran at once</p>
            `}

            ${rows.map((row) => html`
                <${PastRow}
                    key=${row.sessionId}
                    row=${row}
                    onOpen=${chat}
                    action=${html`<${PastActions} row=${row} exec=${exec} onDone=${onDone} />`}
                />
            `)}

            ${archive && html`<${Pager}
                offset=${offset}
                shown=${rows.length}
                total=${archive.total}
                onGo=${setOffset}
            />`}
        </section>
    `;
}

function Pager({ offset, shown, total, onGo }) {
    if (total <= PAGE) return null;

    const from = total === 0 ? 0 : offset + 1;
    const to = offset + shown;
    return html`
        <div class="pager">
            <button
                class="btn"
                type="button"
                disabled=${offset <= 0}
                onClick=${() => onGo(Math.max(0, offset - PAGE))}
            ><span class="chev">${Icon.chevron()}</span> back</button>
            <span class="sub">${from}–${to} of ${total}</span>
            <button
                class="btn"
                type="button"
                disabled=${to >= total}
                onClick=${() => onGo(offset + PAGE)}
            >forward <span class="chev">${Icon.chevron()}</span></button>
        </div>
    `;
}

const PEAK = [{}, area("--accent", "#63a8ff")];

function peakSeries(history) {
    if (!history || !history.t || history.t.length === 0) return null;
    if (!history.pctMax.some((v) => v !== null && v !== undefined)) return null;
    return [history.t, history.pctMax.map((v) => (v === undefined ? null : v))];
}

function busiest(history) {
    if (!history || !history.live) return 0;
    return history.live.reduce((most, v) => (v != null && v > most ? v : most), 0);
}
