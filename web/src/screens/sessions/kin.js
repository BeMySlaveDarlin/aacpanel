// Sessions the panel did not start. A claude started by a run inside another
// session's work stands under that session, folded, rather than as a session
// of its own; one typed into a terminal by hand stands on its own with an
// honest mark. Neither can be written to, moved or shown in a window.

// placeOf names where a live session lives, for its mark.
export function placeOf(s) {
    if (s.transport === "stream") return "stream";
    if (s.outside) return "outside";
    return "tmux";
}

// kinOf splits live sessions into those standing on their own and, under each
// of them, the runs started inside its work. A run goes under the eldest of
// its ancestors in the list; one whose parent is not in the list stands on
// its own.
export function kinOf(list) {
    const by = new Map((list || []).map((s) => [s.session, s]));
    const rootOf = (s) => {
        let at = s;
        const seen = new Set();
        while (at.parent && by.has(at.parent.session) && !seen.has(at.session)) {
            seen.add(at.session);
            at = by.get(at.parent.session);
        }
        return at;
    };
    const own = [];
    const kids = new Map();
    for (const s of list || []) {
        const root = rootOf(s);
        if (root === s) {
            own.push(s);
            continue;
        }
        if (!kids.has(root.session)) kids.set(root.session, []);
        kids.get(root.session).push(s);
    }
    return { own, kids };
}

// kinLabel says how many runs a session started, for the fold under it.
export function kinLabel(count) {
    return `${count} ${count === 1 ? "run" : "runs"} started by it`;
}

// outsideNote says why the panel only reads a session it did not start.
export function outsideNote(s) {
    return s.parent && s.parent.session
        ? `started by ${s.parent.session}, inside its work — the panel reads this conversation and cannot write to it`
        : "started outside the panel, in a terminal of its own — the panel reads this conversation and cannot write to it";
}
