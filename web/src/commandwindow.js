// The run window of a person's command step in a chain: Run is theirs only
// between its from and until, and before from the step is not their move.

import { useEffect, useState } from "preact/hooks";

// A full act wins over a snapshot's window, including a step that has moved on.
export function commandWindow(act) {
    const steps = act?.steps || act?.body?.steps;
    if (!Array.isArray(steps)) return act || {};
    if (act.state !== "open") return {};
    const step = steps[(act.step || 1) - 1];
    return step?.who === "you" && step.command ? step : {};
}

export function scheduled(act) {
    const full = Array.isArray(act?.steps || act?.body?.steps);
    return (full || (!act?.agent && !act?.running)) && Date.parse(commandWindow(act).from || "") > Date.now();
}

// Timers follow the nearest boundary, so a quiet screen changes without a poll.
export function useWindowTimer(moments) {
    const [tick, redraw] = useState(0);
    const future = moments.map((at) => Date.parse(at || "")).filter((at) => at > Date.now());
    const next = future.length ? Math.min(...future) : 0;
    useEffect(() => {
        if (!next) return;
        const timer = setTimeout(() => redraw((n) => n + 1), Math.min(2147483647, Math.max(0, next - Date.now())));
        return () => clearTimeout(timer);
    }, [next, tick]);
}

export function windowMoments(items) {
    return items.flatMap((item) => {
        const window = commandWindow(item);
        return [window.from, window.until];
    });
}
