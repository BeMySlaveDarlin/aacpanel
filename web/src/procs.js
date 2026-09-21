// Host processes: the top consumers the machine has right now, and the way
// they join the containers of the history in one list.
import { useEffect, useState } from "preact/hooks";

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

// procNote is the line under a process: who runs it and that the number is
// not an average of anything.
export function procNote(p) {
    return `pid ${p.pid}${p.user ? ` · ${p.user}` : ""} · right now`;
}

// mergeTop puts the containers of the history and the processes of the host
// into one list sorted by the same column: the containers carry what the
// period recorded, the processes carry what the machine is doing this second.
// A container is measured by `field`, because the caller decides whether the
// list is about averages or about peaks.
export function mergeTop(top, procs, metric, { field = "max", limit = 12 } = {}) {
    const containers = ((top && top.rows) || []).map((r) => ({
        key: `c/${r.container}`,
        kind: "container",
        name: r.container,
        value: r[field],
        avg: r.avg,
        max: r.max,
        alive: r.alive,
        coverage: r.coverage,
        lastSeen: r.lastSeen,
    }));
    const host = ((procs && procs.items) || []).map((p) => ({
        key: `p/${p.pid}`,
        kind: "process",
        name: shortCmd(p.cmd),
        value: metric === "mem" ? p.rss : p.cpuPct,
        alive: true,
        hint: `pid ${p.pid}${p.user ? ` · ${p.user}` : ""} · ${p.cmd}`,
        note: procNote(p),
    }));
    return [...containers, ...host]
        .sort((a, b) => (b.value || 0) - (a.value || 0))
        .slice(0, limit);
}
