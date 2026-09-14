import { useCallback, useEffect, useState } from "preact/hooks";

export const SEVERITY = { critical: "crit", warning: "warn", info: "" };

export const OUTCOME = {
    ok: "Answers",
    network: "the network did not get through",
    timeout: "did not answer in time",
    status: "answered with an error",
    degraded: "works worse than usual",
    config: "the probe is set up wrong",
};

export function useAlerts() {
    const [state, setState] = useState({ kind: "loading", alerts: [] });
    const [older, setOlder] = useState([]);

    const load = useCallback(async () => {
        try {
            const response = await fetch("/api/alerts?limit=50", { credentials: "same-origin" });
            if (response.status === 503 || response.status === 500) {
                setState({ kind: "unavailable", alerts: [], error: "alerts are unavailable: the database is not answering" });
                return;
            }
            if (!response.ok) {
                setState({ kind: "failed", alerts: [], error: `the server answered ${response.status}` });
                return;
            }
            const body = await response.json();
            setState({
                kind: "ready",
                alerts: body.alerts || [],
                open: count(body.open),
                unread: count(body.unread),
            });
        } catch (err) {
            setState({ kind: "failed", alerts: [], error: "the network is unavailable" });
        }
    }, []);

    const more = useCallback(async () => {
        const all = [...(state.alerts || []), ...older];
        const last = all[all.length - 1];
        if (!last) return;
        try {
            const response = await fetch(`/api/alerts?limit=50&before=${last.id}`, { credentials: "same-origin" });
            if (!response.ok) return;
            const body = await response.json();
            setOlder((prev) => [...prev, ...(body.alerts || [])]);
        } catch (err) {
        }
    }, [state.alerts, older]);

    useEffect(() => {
        load();
        const timer = setInterval(() => {
            if (document.visibilityState === "visible") load();
        }, 30000);
        return () => clearInterval(timer);
    }, [load]);

    return { ...state, older, more, reload: load };
}

export function useProbes() {
    const [state, setState] = useState({ kind: "loading", probes: [] });

    useEffect(() => {
        let alive = true;
        fetch("/api/probes", { credentials: "same-origin" })
            .then(async (response) => {
                if (!alive) return;
                if (response.status === 503 || response.status === 500) {
                    setState({ kind: "unavailable", probes: [], error: "probes are unavailable: the database is not answering" });
                    return;
                }
                if (!response.ok) {
                    setState({ kind: "failed", probes: [], error: `the server answered ${response.status}` });
                    return;
                }
                const body = await response.json();
                setState({ kind: "ready", probes: body.probes || [] });
            })
            .catch(() => {
                if (alive) setState({ kind: "failed", probes: [], error: "the network is unavailable" });
            });
        return () => {
            alive = false;
        };
    }, []);

    return state;
}

// count takes a counter the server sent, and null when it sent none: a server
// that knows no counters leaves the screen to count the page itself.
function count(value) {
    return typeof value === "number" ? value : null;
}

export function open(alerts) {
    return (alerts || []).filter((a) => !a.closedAt);
}

export function unread(alerts) {
    return open(alerts).filter((a) => !a.ackedAt);
}

// openCount is the number for the badge: how many open alerts nobody has
// acknowledged. The server counts them all, and only without its count does the
// screen count the page it holds — a page misses an alert buried under fresher
// events.
export function openCount(state) {
    return typeof state.unread === "number" ? state.unread : unread(state.alerts).length;
}
