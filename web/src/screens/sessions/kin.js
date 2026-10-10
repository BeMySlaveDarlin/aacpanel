// The sessions one session started, and the sessions out of the panel's
// reach. A session another had the panel open stands under that one, a step
// in, wherever its project is; one whose parent closed stands under what is
// left of the parent. A claude started by a run inside another session's work
// stands under that session, folded, rather than as a session of its own; one
// typed into a terminal by hand, or into a tmux on a socket of its own, stands
// on its own with an honest mark. None of these can be written to, moved or
// shown in a window. A claude typed into a terminal of the panel is not among
// them: the panel reaches the tmux of its terminals.

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
// of them, the sessions it started: the runs started inside its work and the
// sessions it had the panel open. One goes under the eldest of its ancestors
// in the list; one whose parent is not in the list stands on its own. rootOf
// is that eldest ancestor, or the session itself.
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
    return { own, kids, rootOf };
}

// openedBy is the name of the session that had the panel open this one; empty
// for a session a person opened, and for a run, whose parent is the session
// it runs inside the work of. The link is by name: a restart starts another
// conversation under the same name, and the child stays under it.
export function openedBy(s) {
    return s && !s.outside && s.parent && s.parent.session ? s.parent.session : "";
}

// branchesOf splits what a session started into the sessions it had the panel
// open, in the order they were opened, and the runs started inside its work.
export function branchesOf(kids) {
    const at = (s) => Date.parse(s.startedAt || "") || 0;
    const opened = (kids || []).filter((s) => openedBy(s)).sort((a, b) => at(a) - at(b));
    const runs = (kids || []).filter((s) => !openedBy(s));
    return { opened, runs };
}

// openedLabel says how many sessions a session had the panel open, for the
// fold under it.
export function openedLabel(count) {
    return `opened ${count} ${count === 1 ? "session" : "sessions"}`;
}

// layoutOf lays the live sessions of a list out as the rows a screen draws, in
// order, as entries of four kinds:
//   row  — a live session: on its own, or a step in under the session that
//          opened it (branch "mid" or "last"), or on its own saying which
//          session opened it when that one stands elsewhere (from);
//   fold — the sessions a session had the panel open, under it: open unless
//          the person folded it, and then its sessions follow as rows;
//   runs — the runs started inside a session's work, under it;
//   stub — a parent that closed, over the sessions it opened: they keep
//          standing together, under what is left of it.
// live is every live session the screen knows of, the other contours among
// them: a parent there is alive and elsewhere, one nowhere has closed. Without
// it a parent out of the list is only named. folded holds the parents whose
// fold the person closed.
export function layoutOf(list, live = null, folded = new Set()) {
    const { own, kids } = kinOf(list);
    const here = new Set((list || []).map((s) => s.session));
    const alive = live ? new Set(live.map((s) => s.session)) : null;
    const out = [];
    const under = (s) => {
        const { opened, runs } = branchesOf(kids.get(s.session));
        if (opened.length > 0) {
            const open = !folded.has(s.session);
            out.push({ kind: "fold", parent: s, kids: opened, open });
            if (open) opened.forEach((k, i) => out.push({ kind: "row", s: k, branch: i === opened.length - 1 ? "last" : "mid" }));
        }
        if (runs.length > 0) out.push({ kind: "runs", parent: s, kids: runs });
    };
    const placed = new Set();
    for (const s of own) {
        if (placed.has(s.session)) continue;
        const parent = openedBy(s);
        const away = parent && !here.has(parent);
        if (away && alive && !alive.has(parent)) {
            const orphans = own.filter((o) => openedBy(o) === parent);
            out.push({ kind: "stub", parent });
            orphans.forEach((o, i) => {
                placed.add(o.session);
                out.push({ kind: "row", s: o, branch: i === orphans.length - 1 ? "last" : "mid" });
                under(o);
            });
            continue;
        }
        placed.add(s.session);
        out.push({ kind: "row", s, from: away ? parent : "" });
        under(s);
    }
    return out;
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
