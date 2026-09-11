// The collection layer: the cost before starting and the progress during a round.
import { html } from "../../html.js";
import { ago, duration } from "../../format.js";
import * as usage from "../../data/usage.js";
import { Layer } from "./layer.js";
import { gb } from "./today.js";

export function ScanLayer({ scan, onClose }) {
    const data = scan.scan || {};
    const state = data.state || {};
    const est = data.estimate || null;
    const progress = usage.scanProgress(state);
    const running = scan.running;

    const rows = running || progress.files > 0 ? state.contours || [] : (est && est.contours) || [];
    const totalFiles = running ? progress.files : (est ? est.files : 0);
    const totalBytes = running ? progress.bytes : (est ? est.bytes : 0);
    const pending = est ? est.pendingBytes : totalBytes;
    const eta = usage.scanETA(running ? totalBytes - progress.bytesDone : pending, data.workers);

    return html`
        <${Layer}
            title="Usage collection"
            note=${data.scannedAt && !running ? `collected ${ago(new Date(data.scannedAt * 1000).toISOString())}` : ""}
            onClose=${onClose}
        >
            ${!data.available
                ? html`<p class="hint warn">${data.reason || "usage collection is not configured"}</p>`
                : html`
                    <div class="usscan">
                        <p class="usscanline">
                            ${running
                                ? html`
                                    <b>${`${progress.filesDone} of ${progress.files}`}</b>
                                    ${` files parsed · ${gb(progress.bytesDone)} of ${gb(progress.bytes)}`
                                        + ` · about ${duration(eta)} left`}
                                `
                                : html`
                                    <b>${totalFiles}</b>
                                    ${` files · ${gb(totalBytes)} on disk`}
                                    ${est && est.pendingFiles !== est.files
                                        ? ` · ${est.pendingFiles} new since the last round`
                                        : ""}
                                    ${totalBytes > 0 ? ` · about ${duration(eta)} to parse` : ""}
                                `}
                        </p>
                        ${data.workers > 0 && html`
                            <p class="hint">
                                ${`Parsing runs in ${data.workers} ${data.workers === 1 ? "process" : "processes"}. `}
                                The number lives in the machine description: the agent shares this machine with
                                its owner and has no right to take every core for a background job.
                            </p>
                        `}
                        ${data.agentError && html`<p class="hint crit">the agent is silent: ${data.agentError}</p>`}
                        ${data.estimateError && html`<p class="hint crit">the estimate failed: ${data.estimateError}</p>`}
                        ${state.error && html`<p class="hint crit">the last round failed: ${state.error}</p>`}

                        <div class="usscanrows">
                            ${rows.map((row) => html`
                                <div class="usscanrow" key=${row.contour}>
                                    <span class="usrname">${row.contour}</span>
                                    <span class="usrval">
                                        ${running || row.filesDone > 0
                                            ? `${row.filesDone} / ${row.files} files`
                                            : `${row.files} files · ${gb(row.bytes)}`}
                                    </span>
                                    ${running && html`
                                        <span class="usrbar">
                                            <i style=${`width:${row.bytes > 0 ? Math.min(100, ((row.bytesDone || 0) / row.bytes) * 100) : 0}%`}></i>
                                        </span>
                                    `}
                                </div>
                            `)}
                            ${rows.length === 0 && html`<p class="hint">Nothing found on disk to parse.</p>`}
                        </div>

                        ${(state.failed > 0 || state.missing > 0 || state.skipped > 0) && html`
                            <p class="hint">
                                ${state.failed > 0 && html`<span class="crit">${state.failed} files failed to parse. </span>`}
                                ${state.missing > 0 && html`<span>${state.missing} disappeared while the round was running. </span>`}
                                ${state.skipped > 0 && html`<span>${state.skipped} were already up to date.</span>`}
                            </p>
                        `}

                        <div class="btnrow">
                            <button
                                class="btn primary"
                                type="button"
                                onClick=${scan.start}
                                disabled=${running || scan.starting}
                            >${running ? "Collecting…" : scan.collected ? "Collect again" : "Collect usage"}</button>
                        </div>
                        <p class="hint">
                            The round runs on its own: closing this layer does not stop it, and the widgets
                            pick up the numbers when it finishes. Progress is counted by what reached the
                            database, not by what was parsed — otherwise the counter would run ahead of the
                            history it writes.
                        </p>
                    </div>
                `}
        <//>
    `;
}
