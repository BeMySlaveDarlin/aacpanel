// Sessions out of the panel's reach. A claude started by a run inside another
// session's work stands under that session, folded, rather than as a session
// of its own; one typed into a terminal by hand, or into a tmux on a socket of
// its own, stands on its own with an honest mark. None of them can be written
// to, moved or shown in a window. A claude typed into a terminal of the panel
// is not among them: the panel reaches the tmux of its terminals.

import { isCodex } from "../../agent.js";

// placeOf names where a live session lives, for its mark. A codex thread
// lives on the app-server daemon of its codex home, not on the panel's
// stream, whatever its transport says, or on its own, out of the panel's
// reach; who runs it is said by the line of its model.
export function placeOf(s) {
    if (isCodex(s)) return s.outside ? "outside" : "daemon";
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

// outsideNote says why the panel only reads a session out of its reach.
export function outsideNote(s) {
    const only = "the panel reads this conversation and cannot write to it";
    if (isCodex(s)) return `a codex running on its own, with no daemon the panel is a client of — ${only}`;
    if (s.parent && s.parent.session) return `started by ${s.parent.session}, inside its work — ${only}`;
    if (s.tmuxServer) return `started in a tmux of its own (tmux ${s.tmuxServer}), which the panel does not reach — ${only}`;
    return `started outside the panel, in a terminal of its own — ${only}`;
}
