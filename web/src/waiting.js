// The queue and both lists read the same summary. A completed item leaves
// this device at once while the next host poll catches up with the answer.
import { useEffect, useState } from "preact/hooks";

import { calledMark } from "./viewing.js";
import { scheduled, useWindowTimer, windowMoments } from "./commandwindow.js";

const settled = new Set();
const listeners = new Set();
const kinds = new Set(["request", "call", "brief", "secret"]);
const keyOf = (session, item) => `${session.sessionId}|${item.kind}|${item.id || ""}|${item.at || ""}`;

// waitingOf returns only outstanding work; questions and permissions have
// their own visible docks, and opening a conversation settles its call only.
// A chain handed to the agent or a command waits on no action of the person.
export function waitingOf(session, viewed = false, includeBackground = false) {
    if (!session) return [];
    const items = session.waiting || (session.called ? [{ kind: "call", title: "Called you", at: session.called }] : []);
    return items.filter((item) => kinds.has(item.kind) && (includeBackground || (!item.agent && !item.running && !scheduled(item))) && !settled.has(keyOf(session, item))
        && (item.kind !== "call" || (!viewed && calledMark({ ...session, called: item.at }))));
}

// agentStepsOf returns the chains that stand on the agent's turn.
export function agentStepsOf(session) {
    return (session?.waiting || []).filter((item) => item.agent && !settled.has(keyOf(session, item)));
}

export function runningStepsOf(session) {
    return (session?.waiting || []).filter((item) => item.running && !settled.has(keyOf(session, item)));
}

export function scheduledStepsOf(session) {
    return (session?.waiting || []).filter((item) => scheduled(item) && !settled.has(keyOf(session, item)));
}

// settleWaiting hides an item after its successful answer, never after a tap.
export function settleWaiting(session, kind, id) {
    if (!session) return;
    for (const item of session.waiting || []) {
        if (item.kind === kind && item.id === id) settled.add(keyOf(session, item));
    }
    while (settled.size > 512) settled.delete(settled.values().next().value);
    for (const notify of listeners) notify();
}

// useWaiting keeps a row beside the conversation in step with its answers.
export function useWaiting(session, viewed = false, includeBackground = false) {
    useWindowTimer(windowMoments(session?.waiting || []));
    const [, redraw] = useState(0);
    useEffect(() => {
        const changed = () => redraw((n) => n + 1);
        listeners.add(changed);
        return () => listeners.delete(changed);
    }, []);
    return waitingOf(session, viewed, includeBackground);
}
