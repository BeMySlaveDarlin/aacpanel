// Host processes in the right-hand panel of the Machine section.
import { html } from "../../html.js";
import { bytes } from "../../format.js";
import { PROCS_OK, shortCmd, useProcs } from "../../procs.js";

export function Procs() {
    const { procs, error } = useProcs();
    const items = (procs && procs.items) || [];

    if (!procs && !error) return html`<div class="dkscroll"><p class="dkempty">asking the host…</p></div>`;
    if (!procs) return html`<div class="dkscroll"><p class="dkempty">processes are unavailable: ${error}</p></div>`;
    if (procs.state !== PROCS_OK) {
        return html`
            <div class="dkscroll">
                <p class="dkempty">
                    The agent on this machine does not collect processes: the snapshot has no such block at all.
                    Update aacpanel-agent — the panel will show htop as soon as it appears.
                </p>
            </div>
        `;
    }

    return html`
        <div class="dkprocs">
            ${error && html`<p class="dkprocstale">${error} — the rows below are not current</p>`}
            <div class="dkproc dkprochead">
                <span>pid</span><span>command</span>
                <span class="dkprocnum">cpu</span><span class="dkprocnum">memory</span>
            </div>
            ${items.map((p) => html`
                <div class="dkproc" key=${p.pid} title=${`${p.user ? `${p.user} · ` : ""}${p.cmd}`}>
                    <span class="dkprocpid">${p.pid}</span>
                    <span class="dkproccmd">${shortCmd(p.cmd)}</span>
                    <span class="dkprocnum">${p.cpuPct.toFixed(1)}</span>
                    <span class="dkprocnum">${bytes(p.rss)}</span>
                </div>
            `)}
            ${items.length === 0 && html`<p class="dkempty">the agent sent an empty list</p>`}
            ${items.length > 0 && html`
                <p class="dkprocfoot">the top ${items.length} by cpu and memory out of ${procs.total} on the machine</p>
            `}
        </div>
    `;
}
