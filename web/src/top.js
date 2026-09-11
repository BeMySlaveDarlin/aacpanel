// Top consumers of CPU, memory and disk over a period.
import { useEffect, useState } from "preact/hooks";

export const METRICS = [
    { id: "cpu", label: "CPU" },
    { id: "mem", label: "Memory" },
    { id: "disk", label: "Disk" },
];

export const SORT = {
    avg: "by average",
    max: "by peak",
    delta: "by growth",
};

export function useTop(metric, period) {
    const [state, setState] = useState({ kind: "loading" });

    useEffect(() => {
        let alive = true;
        setState({ kind: "loading" });

        const query = new URLSearchParams({ metric, period, limit: "10" });
        fetch(`/api/history/top?${query}`, { credentials: "same-origin" })
            .then(async (response) => {
                if (!alive) return;
                if (response.status === 503 || response.status === 500) {
                    setState({ kind: "unavailable", error: "the top is unavailable: the history is not answering" });
                    return;
                }
                if (!response.ok) {
                    setState({ kind: "failed", error: `the server answered ${response.status}` });
                    return;
                }
                setState({ kind: "ready", top: await response.json() });
            })
            .catch(() => {
                if (alive) setState({ kind: "failed", error: "the network is unavailable" });
            });

        return () => {
            alive = false;
        };
    }, [metric, period]);

    return state;
}

export function coverageText(coverage) {
    if (coverage === undefined || coverage === null || coverage > 0.9) return "";
    return `data for ${Math.round(coverage * 100)}% of the period`;
}
