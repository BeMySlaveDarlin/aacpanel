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

// How far from the question a waiting may begin and still be the one it put
// the session into.
const ASKED_SLACK_MS = 10000;

// answered returns the mark: when the answer went out, to what and on what
// waiting. The question reaches the screen from the agent sooner than the
// snapshot learns that the session waits on it, so an answer can go out while
// the snapshot still shows the session at work: such a mark is early, and the
// waiting it answered is still to come, stamped about when the question was
// asked.
export function answered(live, use, now = Date.now(), askedAt = "") {
    return {
        at: now,
        use: use || "",
        waitingFor: (live && live.waitingFor) || "",
        statusAt: stampOf(live),
        early: !live || live.status !== "waiting",
        askedAt: momentOf(askedAt),
        settled: false,
    };
}

// settle marks that the snapshot has brought news. An early mark has none to
// hear until its waiting arrives: what comes before it is the snapshot still
// catching up.
export function settle(mark, live) {
    if (!mark || mark.settled) return mark;
    if (sameWait(mark, live)) return mark.early && !mark.met ? { ...mark, met: true } : mark;
    if (mark.early && !mark.met && live) return mark;
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
    if (!live || live.status !== "waiting") return false;
    const stamp = stampOf(live);
    if (mark.early) return !mark.askedAt || !stamp || Math.abs(stamp - mark.askedAt) <= ASKED_SLACK_MS;
    if ((live.waitingFor || "") !== mark.waitingFor) return false;
    return !mark.statusAt || !stamp || stamp === mark.statusAt;
}

function momentOf(at) {
    const ms = Date.parse(at || "");
    return Number.isFinite(ms) ? ms : 0;
}

function stampOf(live) {
    const value = live && live.statusUpdatedAt;
    return typeof value === "number" && value > 0 ? value : 0;
}
