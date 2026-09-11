// Client for the /api/usage/* endpoints behind both usage screens.
import { useCallback, useEffect, useReducer, useRef, useState } from "preact/hooks";

export const PERIODS = [
    { id: "today", label: "today", days: 1 },
    { id: "7d", label: "7 days", days: 7 },
    { id: "14d", label: "14 days", days: 14 },
    { id: "30d", label: "30 days", days: 30 },
    { id: "all", label: "all time", days: 0 },
];

export function periodById(id) {
    return PERIODS.find((p) => p.id === id) || null;
}

// zone returns the browser time zone name for daily buckets.
export function zone() {
    try {
        return Intl.DateTimeFormat().resolvedOptions().timeZone || "";
    } catch (err) {
        return "";
    }
}

// periodRange returns the period boundaries in unix seconds.
export function periodRange(id, now = new Date()) {
    const period = periodById(id);
    if (!period || period.days === 0) return { all: true, to: Math.floor(now.getTime() / 1000) };

    const start = new Date(now);
    start.setHours(0, 0, 0, 0);
    start.setDate(start.getDate() - (period.days - 1));
    return { from: Math.floor(start.getTime() / 1000), to: Math.floor(now.getTime() / 1000) };
}

// stepFor returns the series step for a period: hour or day.
export function stepFor(id) {
    const period = periodById(id);
    return period && period.days > 0 && period.days <= 14 ? "hour" : "day";
}

// usageQuery turns the screen filter into request parameters.
export function usageQuery(filter = {}) {
    const q = new URLSearchParams();
    const range = filter.from != null || filter.to != null
        ? { from: filter.from, to: filter.to }
        : periodRange(filter.period || "30d");

    if (range.all) q.set("period", "all");
    else if (range.from != null) q.set("from", String(Math.floor(range.from)));
    if (filter.to != null) q.set("to", String(Math.floor(filter.to)));

    for (const contour of filter.contours || []) if (contour) q.append("contour", contour);

    if (filter.group) q.set("group", filter.group);
    if (filter.project) q.set("project", filter.project);
    if (filter.session) q.set("session", filter.session);
    if (filter.outside) q.set("outside", "1");

    const tz = filter.tz === undefined ? zone() : filter.tz;
    if (tz) q.set("tz", tz);
    return q;
}

// inboundOf returns the inbound tokens of a row as counted by the database.
export function inboundOf(row) {
    return row ? row.inbound || 0 : 0;
}

// hitOf returns the cache hit share of a row as counted by the database.
export function hitOf(row) {
    return row ? row.hit || 0 : 0;
}

export const BY_CONTOUR = "contour";
export const BY_GROUP = "group";
export const BY_PROJECT = "project";
export const BY_SESSION = "session";
export const BY_CWD = "cwd";

export function summary(filter) {
    return get("/api/usage/summary", usageQuery(filter));
}

export function series(filter, step) {
    const q = usageQuery(filter);
    if (step) q.set("step", step);
    return get("/api/usage/series", q);
}

export function breakdown(filter, by, limit) {
    const q = usageQuery(filter);
    if (by) q.set("by", by);
    if (limit) q.set("limit", String(limit));
    return get("/api/usage/breakdown", q);
}

export function models(filter) {
    return get("/api/usage/models", usageQuery(filter));
}

export function tools(filter, limit) {
    const q = usageQuery(filter);
    if (limit) q.set("limit", String(limit));
    return get("/api/usage/tools", q);
}

// contours lists the contours present in the collected data, for the filter chips.
export function contours() {
    return get("/api/usage/contours", new URLSearchParams());
}

// scanState reports what is on disk, what is parsed and whether a scan is running.
export function scanState({ estimate = true } = {}) {
    const q = new URLSearchParams();
    if (!estimate) q.set("estimate", "0");
    return get("/api/usage/scan", q);
}

// startScan kicks off a collection round and returns without waiting for it.
export async function startScan() {
    const response = await fetch("/api/usage/scan", {
        method: "POST",
        headers: { Accept: "application/json" },
    });
    if (response.status === 409) throw failure(response, "a scan is already running");
    if (!response.ok) throw failure(response, await response.text());
    return response.json();
}

// scanProgress sums the round counters across contours.
export function scanProgress(state) {
    const rows = (state && state.contours) || [];
    const totals = { files: 0, filesDone: 0, bytes: 0, bytesDone: 0 };
    for (const row of rows) {
        totals.files += row.files || 0;
        totals.filesDone += row.filesDone || 0;
        totals.bytes += row.bytes || 0;
        totals.bytesDone += row.bytesDone || 0;
    }
    totals.pct = totals.bytes > 0 ? Math.min(100, (totals.bytesDone / totals.bytes) * 100) : 0;
    return totals;
}

const PARSE_BYTES_PER_SEC = 350 * 1024 * 1024;

// scanETA estimates how many seconds the parsing will take.
export function scanETA(bytes, workers) {
    if (!bytes) return 0;
    return Math.max(1, Math.round(bytes / (PARSE_BYTES_PER_SEC * Math.max(1, workers || 1))));
}

// useUsage runs one endpoint request and keeps its loading state.
export function useUsage(load, deps) {
    const [state, setState] = useState({ kind: "loading" });
    const [round, again] = useReducer((n) => n + 1, 0);
    const fn = useRef(load);
    fn.current = load;
    const key = JSON.stringify(deps);
    const asked = useRef(null);

    useEffect(() => {
        let alive = true;
        const same = asked.current === key;
        asked.current = key;
        setState((prev) => waiting(prev, same));
        fn.current()
            .then((data) => {
                if (alive) setState({ kind: "ready", data });
            })
            .catch((err) => {
                if (!alive) return;
                const soft = err.status === 503 || err.status === 500;
                setState({ kind: soft ? "unavailable" : "failed", error: err.message });
            });
        return () => { alive = false; };
    }, [key, round]);

    return { ...state, reload: again };
}

// waiting returns what to show while the answer is on its way.
export function waiting(prev, sameQuery) {
    return sameQuery && prev.kind === "ready" ? { ...prev, stale: true } : { kind: "loading" };
}

export function useUsageSummary(filter, round) {
    const key = usageQuery(filter).toString();
    return useUsage(() => summary(filter), [key, round]);
}

export function useUsageSeries(filter, step, round) {
    const key = usageQuery(filter).toString();
    return useUsage(() => series(filter, step), [key, step, round]);
}

export function useUsageBreakdown(filter, by, limit, round) {
    const key = usageQuery(filter).toString();
    return useUsage(() => breakdown(filter, by, limit), [key, by, limit, round]);
}

export function useUsageModels(filter, round) {
    const key = usageQuery(filter).toString();
    return useUsage(() => models(filter), [key, round]);
}

export function useUsageTools(filter, limit, round) {
    const key = usageQuery(filter).toString();
    return useUsage(() => tools(filter, limit), [key, limit, round]);
}

export function useUsageContours(round) {
    return useUsage(() => contours(), [round]);
}

const SCAN_POLL_RUNNING_MS = 1000;
const SCAN_POLL_IDLE_MS = 60000;

// useUsageScan exposes the collection state, the start button and the round counter.
export function useUsageScan() {
    const was = useRef(false);
    const state = useUsage(() => scanState({ estimate: !was.current }), []);
    const [rounds, setRounds] = useState(0);
    const [starting, setStarting] = useState(false);

    const data = state.kind === "ready" ? state.data : {};
    const running = Boolean(data.state && data.state.running);

    useEffect(() => {
        if (was.current && !running) setRounds((n) => n + 1);
        was.current = running;
    }, [running]);

    const { reload } = state;
    useEffect(() => {
        const timer = setInterval(reload, running ? SCAN_POLL_RUNNING_MS : SCAN_POLL_IDLE_MS);
        return () => clearInterval(timer);
    }, [running, reload]);

    const start = useCallback(async () => {
        setStarting(true);
        try {
            await startScan();
            was.current = true;
            reload();
        } finally {
            setStarting(false);
        }
    }, [reload]);

    return {
        ...state,
        scan: data,
        running,
        rounds,
        start,
        starting,
        collected: Boolean(data.available && (data.scannedAt || (data.files || 0) > 0)),
    };
}

async function get(path, q) {
    const query = q.toString();
    const response = await fetch(query ? `${path}?${query}` : path, {
        headers: { Accept: "application/json" },
    });
    if (!response.ok) throw failure(response, await response.text());
    return response.json();
}

function failure(response, body) {
    const text = (body || "").trim();
    const err = new Error(text || `the server answered ${response.status}`);
    err.status = response.status;
    err.unauthorized = response.status === 401;
    return err;
}
