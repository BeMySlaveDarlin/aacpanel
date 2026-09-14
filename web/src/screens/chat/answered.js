// An answer sent from the panel, until the agent snapshot confirms it.

// ANSWER_LAG_MS is the ceiling after which the snapshot is trusted unconditionally.
export const ANSWER_LAG_MS = 30000;

// marks keeps the last mark per session for as long as the page lives: the chat screen
// unmounts on navigation, and a mark that died with it would lock the composer again
// on return until the snapshot caught up. A page reload loses them, and by then the
// snapshot has long caught up.
const marks = new Map();

// remember stores the session's mark and returns it; null forgets the session.
export function remember(name, mark) {
    if (mark) marks.set(name, mark);
    else marks.delete(name);
    return mark;
}

// recall returns the session's mark while it is fresh; an expired one is dropped.
export function recall(name, now = Date.now()) {
    const mark = marks.get(name) || null;
    if (mark && !fresh(mark, now)) {
        marks.delete(name);
        return null;
    }
    return mark;
}

// answered returns the mark: when the answer went out, to what and on what waiting.
export function answered(live, use, now = Date.now()) {
    return {
        at: now,
        use: use || "",
        waitingFor: (live && live.waitingFor) || "",
        statusAt: stampOf(live),
        settled: false,
    };
}

// settle marks that the snapshot has brought news.
export function settle(mark, live) {
    if (!mark || mark.settled) return mark;
    if (sameWait(mark, live)) return mark;
    return { ...mark, settled: true };
}

// lagging reports whether the snapshot still shows the waiting that was answered.
export function lagging(mark, live, now = Date.now()) {
    return fresh(mark, now) && !mark.settled && sameWait(mark, live);
}

// hidesAsk reports whether this question has already been answered from the panel.
export function hidesAsk(mark, use, now = Date.now()) {
    return fresh(mark, now) && Boolean(use) && mark.use === use;
}

function fresh(mark, now) {
    return Boolean(mark) && now - mark.at < ANSWER_LAG_MS;
}

function sameWait(mark, live) {
    if (!live || live.status !== "waiting" || (live.waitingFor || "") !== mark.waitingFor) return false;
    const stamp = stampOf(live);
    return !mark.statusAt || !stamp || stamp === mark.statusAt;
}

function stampOf(live) {
    const value = live && live.statusUpdatedAt;
    return typeof value === "number" && value > 0 ? value : 0;
}
