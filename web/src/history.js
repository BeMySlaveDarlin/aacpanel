import { useEffect, useState } from "preact/hooks";

export const PERIODS = [
    { id: "30m", label: "30 min" },
    { id: "1h", label: "Hour" },
    { id: "24h", label: "Day" },
    { id: "7d", label: "Week" },
    { id: "30d", label: "Month" },
];

const RESOLUTION = {
    raw: "raw samples",
    "1m": "minute averages",
    "1h": "hour averages",
};

export function resolutionText(resolution, stepSec) {
    const name = RESOLUTION[resolution] || resolution;
    if (!stepSec) return name;
    if (stepSec < 60) return `${name}, step ${stepSec} s`;
    if (stepSec < 3600) return `${name}, step ${Math.round(stepSec / 60)} min`;
    return `${name}, step ${Math.round(stepSec / 3600)} h`;
}

export function bucketText(resolution, stepSec) {
    const name = resolution === "1h" ? "hour slices" : "minute slices";
    if (!stepSec) return name;
    if (stepSec < 3600) return `${name}, step ${Math.round(stepSec / 60)} min`;
    return `${name}, step ${Math.round(stepSec / 3600)} h`;
}

function useEndpoint(url, what) {
    const [state, setState] = useState({ kind: "loading" });

    useEffect(() => {
        let alive = true;
        setState({ kind: "loading" });

        fetch(url, { credentials: "same-origin" })
            .then(async (response) => {
                if (!alive) return;
                if (response.status === 503 || response.status === 500) {
                    setState({
                        kind: "unavailable",
                        error: response.status === 503
                            ? `${what} is unavailable: the database is not answering`
                            : `${what} is unavailable: the server could not give it away`,
                    });
                    return;
                }
                if (!response.ok) {
                    setState({ kind: "failed", error: await text(response) });
                    return;
                }
                setState({ kind: "ready", data: await response.json() });
            })
            .catch(() => {
                if (alive) setState({ kind: "failed", error: "the network is unavailable" });
            });

        return () => {
            alive = false;
        };
    }, [url, what]);

    return state;
}

export function useHistory(subject, metric, period) {
    const query = new URLSearchParams({ subject, metric, period });
    const state = useEndpoint(`/api/history?${query}`, "the history");
    return state.kind === "ready" ? { kind: "ready", series: state.data } : state;
}

export function useSessionsHistory(period, page = {}) {
    const query = new URLSearchParams({ period });
    if (page.limit) query.set("limit", page.limit);
    if (page.offset) query.set("offset", page.offset);
    const state = useEndpoint(`/api/history/sessions?${query}`, "the session history");
    return state.kind === "ready" ? { kind: "ready", sessions: state.data } : state;
}

export function useSessionsArchive({ limit, offset, skip, started, profile, contour } = {}) {
    const query = new URLSearchParams();
    if (limit) query.set("limit", limit);
    if (offset) query.set("offset", offset);
    if (skip && skip.length) query.set("skip", skip.join(","));
    if (started) query.set("started", "1");
    if (contour) query.set("contour", contour);
    else if (profile) query.set("profile", profile);
    const state = useEndpoint(`/api/sessions/archive?${query}`, "the session archive");
    return state.kind === "ready" ? { kind: "ready", archive: state.data } : state;
}

export function useNetHistory(period) {
    const state = useEndpoint(`/api/history/net?period=${encodeURIComponent(period)}`, "the network history");
    return state.kind === "ready" ? { kind: "ready", net: state.data } : state;
}

// netPlot converts one interface into the uPlot format.
export function netPlot(history, iface) {
    if (!history || !history.t || history.t.length === 0 || !iface) return null;
    const has = (row) => row.some((v) => v !== null && v !== undefined);
    if (!has(iface.rx) && !has(iface.tx)) return null;
    const fix = (row) => row.map((v) => (v === undefined ? null : v));
    return [history.t, fix(iface.rx), fix(iface.tx)];
}

async function text(response) {
    const body = (await response.text()).trim();
    try {
        const parsed = JSON.parse(body);
        if (parsed && parsed.error) return parsed.error;
    } catch (err) {
    }
    return body || `the server answered ${response.status}`;
}

// toPlot converts the server response into the uPlot format.
export function toPlot(series) {
    if (!series || !series.t || series.t.length === 0) return null;

    const step = series.stepSec || 0;
    if (!step) return [series.t, series.avg, series.max];

    const t = [];
    const avg = [];
    const max = [];

    for (let i = 0; i < series.t.length; i++) {
        if (i > 0 && series.t[i] - series.t[i - 1] > step * 1.5) {
            t.push(series.t[i - 1] + step);
            avg.push(null);
            max.push(null);
        }
        t.push(series.t[i]);
        avg.push(series.avg[i]);
        max.push(series.max[i]);
    }

    return [t, avg, max];
}
