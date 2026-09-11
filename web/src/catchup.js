// Waiting for an action to show up in the host snapshot.
import { useEffect, useRef, useState } from "preact/hooks";

export const CATCH_UP_MS = 2000;

export const CATCH_UP_LIMIT = 60000;

let held = null;

export function noteAction(kind, target) {
    if (kind && target && held) held(kind, target);
}

export function ownName(name, session) {
    return name === session || new RegExp(`^${session.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}-\\d+$`).test(name);
}

function waitKey(kind, target) {
    return `${kind}:${target}`;
}

export function settled(task, names, at = 0) {
    if (at && task.seen && at <= task.seen) return false;
    if (task.kind === "close") return !names.includes(task.target);
    return names.some((name) => !task.before.includes(name) && ownName(name, task.target));
}

export function useCatchUp(snapshot, refresh) {
    const [waits, setWaits] = useState({});
    const [, tick] = useState(0);
    const alive = (snapshot && snapshot.sessions) || [];
    const at = (snapshot && snapshot.at) || 0;

    const latest = useRef(null);
    latest.current = { alive, at, refresh };

    useEffect(() => {
        const start = (kind, target) => {
            const now = latest.current;
            setWaits((prev) => ({
                ...prev,
                [waitKey(kind, target)]: {
                    kind,
                    target,
                    since: Date.now(),
                    seen: now.at,
                    before: now.alive.map((s) => s.session),
                },
            }));
            if (now.refresh) now.refresh();
        };
        held = start;
        return () => { if (held === start) held = null; };
    }, []);

    const busy = Object.keys(waits).length > 0;
    useEffect(() => {
        if (!busy) return undefined;
        const timer = setInterval(() => {
            tick((n) => n + 1);
            if (refresh) refresh();
        }, CATCH_UP_MS);
        return () => clearInterval(timer);
    }, [busy, refresh]);

    useEffect(() => {
        setWaits((prev) => {
            const names = alive.map((s) => s.session);
            const next = {};
            let changed = false;
            for (const [key, task] of Object.entries(prev)) {
                if (settled(task, names, at) || Date.now() - task.since > CATCH_UP_LIMIT) {
                    changed = true;
                    continue;
                }
                next[key] = task;
            }
            return changed ? next : prev;
        });
    }, [snapshot]);

    return {
        of: (kind, target) => waits[waitKey(kind, target)] || null,
        opening: () => Object.values(waits).filter((task) => task.kind === "open"),
        busy,
    };
}
