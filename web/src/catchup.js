// Waiting for an action to show up in the host snapshot.
import { useEffect, useRef, useState } from "preact/hooks";

export const CATCH_UP_MS = 2000;

export const CATCH_UP_LIMIT = 60000;

let held = null;

// noteAction starts the wait for what a confirmed action changes, as the
// person presses: the executor answers once the whole of it is done, and a
// move or a start takes seconds the screen would otherwise spend saying
// nothing. Until the executor answers the wait is in flight and does not run
// out.
export function noteAction(kind, target, params) {
    if (kind && target && held) held.start(kind, target, params);
}

// answerAction lands a wait in flight: an action done leaves it to the
// snapshot and to the ceiling from now on, a refused one takes it away —
// nothing is coming to wait for.
export function answerAction(kind, target, ok) {
    if (kind && target && held) held.answer(kind, target, ok);
}

// liveOf finds the live session a conversation on screen belongs to. A
// conversation is known by its id first: the session holding it is its own,
// under whatever name it goes now. By the name alone it is found only when it
// has no id yet, or when it was seen live and follows its session (see follow).
// A closed conversation opened beside a live session of the same name belongs
// to nothing: its feed is not that session's, and whatever is typed under it
// would go into a conversation that is not on the screen.
export function liveOf(sessions, target) {
    const list = sessions || [];
    if (!target || target.archived) return null;
    if (target.id) {
        const own = list.find((s) => s.sessionId === target.id);
        if (own || !target.follow) return own || null;
    }
    return list.find((s) => s.session === target.name) || null;
}

// follow keeps an open conversation on its session and returns the target to
// show, the same object when nothing moved. A renamed session keeps its
// conversation and changes its name; a restarted one keeps its name and starts
// a new conversation, and a conversation once seen live goes on to the new one
// — or the screen goes on showing the old feed under the live session's head.
export function follow(sessions, target) {
    const live = liveOf(sessions, target);
    if (!live) return target;
    const id = live.sessionId || target.id || null;
    if (target.follow && live.session === target.name && id === target.id) return target;
    return { ...target, name: live.session, id, follow: true };
}

export function ownName(name, session) {
    return name === session || new RegExp(`^${session.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}-\\d+$`).test(name);
}

function waitKey(kind, target) {
    return `${kind}:${target}`;
}

// identity tells one run of a session from the next one under the same name:
// a restarted session keeps its name, and only the conversation behind it
// changes; a switched one keeps its conversation too, and only where it lives
// changes.
function identity(s) {
    return `${(s && s.sessionId) || ""}|${(s && s.startedAt) || ""}|${(s && s.transport) || ""}`;
}

// expired says the snapshot never showed what the executor reported done.
function expired(task, now) {
    return !task.flying && now - task.answered > CATCH_UP_LIMIT;
}

export function settled(task, names, at = 0, ids = {}) {
    if (at && task.seen && at <= task.seen) return false;
    if (task.kind === "close") return !names.includes(task.target);
    if (task.kind === "restart" || task.kind === "switch") {
        return names.includes(task.target) && ids[task.target] !== task.was;
    }
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
        const start = (kind, target, params) => {
            const now = latest.current;
            setWaits((prev) => ({
                ...prev,
                [waitKey(kind, target)]: {
                    kind,
                    target,
                    params: params || {},
                    since: Date.now(),
                    flying: true,
                    seen: now.at,
                    before: now.alive.map((s) => s.session),
                    was: identity(now.alive.find((s) => s.session === target)),
                },
            }));
            if (now.refresh) now.refresh();
        };
        const answer = (kind, target, ok) => {
            const key = waitKey(kind, target);
            setWaits((prev) => {
                const task = prev[key];
                if (!task || !task.flying) return prev;
                const next = { ...prev };
                if (ok) next[key] = { ...task, flying: false, answered: Date.now() };
                else delete next[key];
                return next;
            });
            if (ok && latest.current.refresh) latest.current.refresh();
        };
        const hooks = { start, answer };
        held = hooks;
        return () => { if (held === hooks) held = null; };
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
            const ids = Object.fromEntries(alive.map((s) => [s.session, identity(s)]));
            const next = {};
            let changed = false;
            for (const [key, task] of Object.entries(prev)) {
                if (settled(task, names, at, ids) || expired(task, Date.now())) {
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
