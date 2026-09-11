// Host processes in the right-hand panel of the Machine section.
import { useEffect, useState } from "preact/hooks";
import { html } from "../../html.js";
import { bytes } from "../../format.js";

const EVERY = 5000;

export const PROCS_OK = "ok";

let shared = { procs: null, error: "" };
const readers = new Set();
let timer = 0;

function publish(next) {
    shared = next;
    readers.forEach((set) => set(shared));
}

function poll() {
    fetch("/api/procs", { credentials: "same-origin" })
        .then((r) => (r.ok
            ? r.json()
            : r.text().then((body) => Promise.reject(new Error(body.trim() || `code ${r.status}`)))))
        .then((data) => publish({ procs: data, error: "" }))
        .catch((e) => publish({ procs: shared.procs, error: String(e.message || e) }))
        .finally(() => {
            if (readers.size > 0) timer = setTimeout(poll, EVERY);
        });
}

// useProcs returns host processes, refreshed while the screen is open.
export function useProcs(enabled = true) {
    const [state, setState] = useState(shared);

    useEffect(() => {
        if (!enabled) return undefined;
        readers.add(setState);
        setState(shared);
        if (readers.size === 1) poll();

        return () => {
            readers.delete(setState);
            if (readers.size === 0) {
                clearTimeout(timer);
                timer = 0;
            }
        };
    }, [enabled]);

    return state;
}

// shortCmd returns the command without the directory of the executable.
export function shortCmd(cmd) {
    const text = String(cmd || "");
    if (!text.startsWith("/")) return text;
    const cut = text.indexOf(" ");
    const head = cut < 0 ? text : text.slice(0, cut);
    return head.slice(head.lastIndexOf("/") + 1) + (cut < 0 ? "" : text.slice(cut));
}

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
