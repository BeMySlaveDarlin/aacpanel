import { useEffect, useState } from "preact/hooks";

export const BASIS = {
    hour: "the norm from the same hour of the previous day",
    day: "the norm over the whole day — there is little history yet",
};

export function useDegradations() {
    const [state, setState] = useState({ kind: "loading" });

    useEffect(() => {
        let alive = true;
        fetch("/api/degradations", { credentials: "same-origin" })
            .then(async (response) => {
                if (!alive) return;
                if (response.status === 503 || response.status === 500) {
                    setState({ kind: "unavailable", error: "degradations are unavailable: the history is not answering" });
                    return;
                }
                if (!response.ok) {
                    setState({ kind: "failed", error: `the server answered ${response.status}` });
                    return;
                }
                const body = await response.json();
                setState({ kind: "ready", items: body.degradations || [] });
            })
            .catch(() => {
                if (alive) setState({ kind: "failed", error: "the network is unavailable" });
            });

        return () => {
            alive = false;
        };
    }, []);

    return state;
}

export function sinceText(since) {
    if (!since) return "more than 6 hours";
    const sec = Math.max(0, Date.now() / 1000 - since);
    if (sec < 3600) return `${Math.round(sec / 60)} min`;
    return `${Math.round(sec / 3600)} h`;
}

export function subjectText(subject) {
    if (subject === "host") return "host";
    return subject.startsWith("container:") ? subject.slice("container:".length) : subject;
}

export const METRIC = { cpu: "cpu", mem: "memory", load: "load average" };
