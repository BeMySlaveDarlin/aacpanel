// The load of the host in the top bar: three figures, always in the same place.
import { html } from "../html.js";
import { level, pct } from "../format.js";

// worstDisk is the disk to show when there is only room for one. A host runs
// out of one mount at a time, and the full one is the one worth a glance.
function worstDisk(disks) {
    let worst = null;
    for (const disk of disks || []) {
        if (!disk || typeof disk.pct !== "number") continue;
        if (!worst || disk.pct > worst.pct) worst = disk;
    }
    return worst;
}

export function headLoad(snapshot) {
    const host = snapshot && snapshot.host;
    if (!host) return null;
    const disk = worstDisk(host.disks);
    return {
        cpu: host.cpuPct || 0,
        mem: (host.mem && host.mem.pct) || 0,
        disk: disk ? disk.pct : null,
        mount: disk ? disk.mount : "",
    };
}

function Gauge({ name, value, tip }) {
    if (value === null || value === undefined) return null;
    const share = Math.min(100, Math.max(0, value));
    // The reading is the fill of the chip itself rather than a bar beside it:
    // three numbers and three bars are six things to read in a bar that has
    // room for a glance.
    return html`
        <span
            class=${`dkgauge ${level(value)}`}
            style=${`--fill:${share}%`}
            data-tip=${tip}
            data-tipside="left"
        >
            <span class="dkgauge-n">${name}</span>
            <span class="dkgauge-v">${pct(value)}</span>
        </span>
    `;
}

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
