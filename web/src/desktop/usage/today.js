// The tile of today: how much was spent and when, and how much of it the
// cache carried. Two parts of one tile, each opening its own breakdown — the
// projects of the day, the sessions with the worst cache first.
import { html } from "../../html.js";
import { ago, pct, tokens } from "../../format.js";
import { ContextBar } from "../../ui/bar.js";
import * as usage from "../../data/usage.js";
import { Icon } from "../../ui/icons.js";
import { Blank, Head, opener, stop, troubleOf } from "./parts.js";

// The hours named under the bars of the day.
const MARKS = [0, 6, 12, 18];

// Today shows the usage of the current day: the spend, its hours as bars, and
// the share the cache carried.
export function Today({ summary, series, scan, onOpen, onScan, onCache }) {
    const data = summary.kind === "ready" ? summary.data : null;
    const sum = (data && data.summary) || null;
    const hit = data ? (data.hit || 0) * 100 : 0;
    const open = scan.collected ? onOpen : null;
    const cache = scan.collected ? onCache : null;

    return html`
        <section class="dkwidget uwg uday">
            <div class=${`udaypart${open ? " open" : ""}`} ...${opener(open)}>
                <${Head}
                    title="Usage today"
                    note=${collectedNote(scan)}
                    actions=${html`<${ScanButton} scan=${scan} onScan=${onScan} />`}
                />
                ${!scan.collected
                    ? html`<${FirstScan} scan=${scan} onScan=${onScan} />`
                    : troubleOf(summary) || html`
                        <p class="uwgbig">${tokens(data.inbound)}<span class="uwgunit">inbound</span></p>
                        <p class="uwgsub">
                            ${`${tokens(sum.output)} out · ${sum.answers} ${sum.answers === 1 ? "answer" : "answers"}`}
                        </p>
                        ${scan.running
                            ? html`<${Progress} scan=${scan} />`
                            : html`<${HourBars} series=${series} />`}
                    `}
            </div>
            <div class=${`udaypart${cache ? " open" : ""}`} ...${opener(cache)}>
                <${Head} title="Cache" note="today" />
                ${!scan.collected
                    ? html`<${Blank}>Nothing collected yet.<//>`
                    : troubleOf(summary) || html`
                        <p class="uwgbig">${pct(hit)}<span class="uwgunit">of the inbound came from cache</span></p>
                        <${ContextBar} pct=${hit} />
                        <p class="uwgsub">
                            ${`read ${tokens(sum.cacheRead)} · written ${tokens(sum.cacheCreation)}`}
                        </p>
                    `}
            </div>
        </section>
    `;
}

// HourBars is the day by its hours: a bar for every hour that has passed, a
// stub for every one ahead, the hour under way lit. The tile has the room for
// bars, and a day of bars says at a glance when the spend went, where a line
// stretched over the tile only says that it did.
export function HourBars({ series }) {
    const points = (series.kind === "ready" && series.data.points) || [];
    const by = new Array(24).fill(0);
    for (const p of points) by[new Date(p.at).getHours()] += usage.inboundOf(p);
    const peak = Math.max(...by);
    const now = new Date().getHours();
    return html`
        <div class="udaybars" role="img" aria-label="inbound by hour today">
            ${by.map((value, hour) => html`
                <i
                    key=${hour}
                    class=${hour > now ? "ahead" : hour === now ? "now" : ""}
                    style=${hour > now ? undefined : `--h:${peak > 0 ? Math.max(2, (value / peak) * 100) : 2}%`}
                ></i>
            `)}
        </div>
        <div class="udayaxis">
            ${MARKS.map((hour) => html`<span key=${hour} style=${`grid-column:${hour + 1} / span 2`}>${String(hour).padStart(2, "0")}</span>`)}
            <span class="udayby">by hour</span>
        </div>
    `;
}

function FirstScan({ scan, onScan }) {
    const est = scan.scan.estimate;
    const err = scan.scan.agentError || scan.scan.estimateError || scan.error || "";
    if (!scan.scan.available) {
        return html`<${Blank} kind="warn">${scan.scan.reason || "usage collection is not configured"}<//>`;
    }
    if (scan.running) {
        return html`
            <div class="uwgfirst" onClick=${stop}>
                <p class="uwgsub">Collecting…</p>
                <${Progress} scan=${scan} />
                <button class="btn" type="button" onClick=${onScan}>Show progress</button>
            </div>
        `;
    }
    return html`
        <div class="uwgfirst" onClick=${stop}>
            <p class="uwgsub">Usage has not been collected yet.</p>
            ${est && html`
                <p class="uwgsub">${`${est.files} files · ${gb(est.bytes)}`}</p>
            `}
            ${err && html`<p class="uwgsub crit">${err}</p>`}
            <button class="btn" type="button" onClick=${onScan}>Collect usage</button>
        </div>
    `;
}

function Progress({ scan }) {
    const done = usage.scanProgress(scan.scan.state || {});
    if (!done.files) return null;
    return html`
        <div class="usprogress">
            <p class="uwgsub">${`${done.filesDone} of ${done.files} files`}</p>
            <span class="usrbar"><i style=${`width:${done.pct}%`}></i></span>
        </div>
    `;
}

function ScanButton({ scan, onScan }) {
    if (!scan.collected && !scan.running) return null;
    return html`
        <button
            class=${`dkact${scan.running ? " busy" : ""}`}
            type="button"
            data-tip=${scan.running ? "Collection is running" : undefined}
            onClick=${onScan}
        ><${Icon.refresh} /></button>
    `;
}

function collectedNote(scan) {
    if (scan.running) return "collecting…";
    if (!scan.scan.scannedAt) return "";
    return `collected ${ago(new Date(scan.scan.scannedAt * 1000).toISOString())}`;
}

// gb renders a size on disk in gigabytes.
export function gb(value) {
    const n = (value || 0) / (1024 * 1024 * 1024);
    if (n >= 1) return `${Math.round(n * 10) / 10} GB`;
    return `${Math.max(1, Math.round((value || 0) / (1024 * 1024)))} MB`;
}
