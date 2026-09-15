// The first two usage tiles: today's spend and how much the cache helped.
import { html } from "../../html.js";
import { area, Chart } from "../../chart.js";
import { ago, pct, tokens } from "../../format.js";
import { ContextBar } from "../../ui/bar.js";
import * as usage from "../../data/usage.js";
import { Icon } from "../../ui/icons.js";
import { Blank, Widget, stop, troubleOf } from "./parts.js";

const SPARK = [{}, area("--accent", "#63a8ff")];

// Today shows the usage of the current day.
export function Today({ summary, series, scan, onOpen, onScan }) {
    const data = summary.kind === "ready" ? summary.data : null;
    const sum = (data && data.summary) || null;
    const points = (series.kind === "ready" && series.data.points) || [];
    const plot = [
        points.map((p) => Date.parse(p.at) / 1000),
        points.map(usage.inboundOf),
    ];

    return html`
        <${Widget}
            span="w3 h1"
            title="Usage today"
            note=${collectedNote(scan)}
            actions=${html`<${ScanButton} scan=${scan} onScan=${onScan} />`}
            onOpen=${scan.collected ? onOpen : null}
        >
            ${!scan.collected
                ? html`<${FirstScan} scan=${scan} onScan=${onScan} />`
                : troubleOf(summary) || html`
                    <p class="uwgbig">${tokens(data.inbound)}<span class="uwgunit">inbound</span></p>
                    <p class="uwgsub">
                        ${`${tokens(sum.output)} out · ${sum.answers} ${sum.answers === 1 ? "answer" : "answers"}`}
                    </p>
                    ${scan.running
                        ? html`<${Progress} scan=${scan} />`
                        : points.length > 0 && html`
                            <${Chart} data=${plot} series=${SPARK} compact=${true} height=${34} />
                        `}
                `}
        <//>
    `;
}

// Cache shows how much of the inbound tokens came from the cache.
export function Cache({ summary, scan, onOpen }) {
    const data = summary.kind === "ready" ? summary.data : null;
    const sum = (data && data.summary) || null;
    const hit = data ? (data.hit || 0) * 100 : 0;

    return html`
        <${Widget}
            span="w3 h1"
            title="Cache efficiency"
            note="today"
            onOpen=${scan.collected ? onOpen : null}
        >
            ${scan.collected && sum && html`<${ContextBar} pct=${hit} edge=${true} />`}
            ${!scan.collected
                ? html`<${Blank}>Nothing collected yet.<//>`
                : troubleOf(summary) || html`
                    <p class="uwgbig">${pct(hit)}<span class="uwgunit">of the inbound came from cache</span></p>
                    <p class="uwgsub">
                        ${`read ${tokens(sum.cacheRead)} · written ${tokens(sum.cacheCreation)}`}
                    </p>
                `}
        <//>
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
