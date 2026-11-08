import { useEffect, useState } from "preact/hooks";

// A report may cross the API's page boundary. Merge each issue's pages before
// presenting totals; a second request replaces the whole report, never its tail.
export function useTaskPeriod(contour, period) {
    const [state, setState] = useState({ kind: "loading" });
    const range = periodRange(period);
    const from = range.from.toISOString();
    const until = range.until.toISOString();
    useEffect(() => {
        const controller = new AbortController();
        setState({ kind: "loading" });
        (async () => {
            try {
                const groups = new Map();
                let offset = 0;
                let total = 0;
                do {
                    const query = new URLSearchParams({ contour, from, until, limit: 500, offset });
                    const response = await fetch(`/api/tasks/period?${query}`, { credentials: "same-origin", signal: controller.signal });
                    if (!response.ok) throw new Error(`The report is unavailable (${response.status}).`);
                    const page = await response.json();
                    total = page.totalTasks;
                    for (const group of page.groups) {
                        const own = groups.get(group.external) || [];
                        own.push(...group.tasks);
                        groups.set(group.external, own);
                    }
                    offset += page.limit;
                    if (!page.limit && offset < total) throw new Error("The report page did not advance.");
                } while (offset < total);
                if (!controller.signal.aborted) setState({ kind: "ready", groups: [...groups].map(([external, tasks]) => ({ external, tasks })), total, from, until });
            } catch (err) {
                if (!controller.signal.aborted) setState({ kind: "failed", error: err.message });
            }
        })();
        return () => controller.abort();
    }, [contour, from, until]);
    return state;
}

export function periodRange(period, now = new Date()) {
    const today = new Date(now.getFullYear(), now.getMonth(), now.getDate());
    const from = new Date(today);
    const until = new Date(today);
    until.setDate(until.getDate() + 1);
    if (period === "yesterday") {
        from.setDate(from.getDate() - 1);
        until.setDate(until.getDate() - 1);
    } else if (period === "week") from.setDate(from.getDate() - 6);
    return { from, until };
}

export function within(at, period) {
    if (!at) return false;
    const range = periodRange(period);
    const value = new Date(at);
    return value >= range.from && value < range.until;
}

export function age(at) {
    if (!at) return "undated";
    const days = Math.max(0, Math.floor((Date.now() - new Date(at).getTime()) / 86400000));
    return days === 0 ? "today" : `${days}d`;
}

export function eventDate(event) {
    const date = new Date(event.at);
    if (event.precision === "date") return date.toLocaleDateString("en-GB", { day: "2-digit", month: "short", timeZone: "UTC" });
    return date.toLocaleString("en-GB", { day: "2-digit", month: "short", hour: "2-digit", minute: "2-digit" });
}

export function eventWords(event) {
    if (event.kind === "status_changed") return `${event.payload.from} → ${event.payload.to}`;
    return { created: "Created", changed: "Changed", closed: event.payload.status === "cancelled" ? "Cancelled" : "Closed" }[event.kind] || event.kind;
}
