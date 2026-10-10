// The agent a live session runs. Most sessions are claude; a codex thread
// stands on the same lists and its conversation is read the same way, but
// the panel offers it what codex takes — words and files into its turn, how
// it thinks and what it may do, the stop of its turn, the answer to what it
// asks to be allowed and its close — and nothing else: every other tool of a
// session is claude's, and the host refuses it for codex.

// CODEX_NOTE says what the panel does to a codex thread, for its mark and
// for the line that says where it lives.
export const CODEX_NOTE = "a codex thread: the panel writes to it, sets how it thinks and what it may do, "
    + "stops its turn, answers what it asks and closes it";

// CODEX_CLOSE says what a close does to a codex thread, beside every button
// that closes one. There is no process of the panel's to end: the thread is
// the daemon's, and only the panel's hold on it goes.
export const CODEX_CLOSE = "the turn at work breaks off and the panel lets the thread go: the daemon unloads it "
    + "once no client is left, codex resume brings it back, and codex in tmux closes with its tmux session";

// isCodex reports whether a live session is a codex thread.
export function isCodex(s) {
    return Boolean(s) && s.agent === "codex";
}

// agentKey and agentName say who runs a live session: the key its word is
// painted by, and the word that opens the line of its model.
export function agentKey(s) {
    return isCodex(s) ? "codex" : "claude";
}

export function agentName(s) {
    return isCodex(s) ? "Codex" : "Claude";
}

// shownName is the name a person reads for a live session. A codex thread
// reads by the name the panel gave it, while codex still calls the thread by
// it, and by the session of its project otherwise, as a claude session there
// reads; the name the panel finds the session by is left for a thread no
// project holds. The session keeps that name as its address all the same:
// actions, links and keys go by it.
export function shownName(s, fallback = "") {
    if (isCodex(s) && s.title) return s.title;
    if (isCodex(s) && s.project && s.project.session) return s.project.session;
    return (s && s.session) || fallback;
}

// An archived conversation says whose it is the way a live session does: an
// archive row of a codex thread carries agent "codex", and isCodex, agentKey
// and agentName read it as they read a session.

// codexResume is what the resume of an archived thread of codex says beside
// the id of the thread: that the thread is codex's, and the contour of its
// home, in whose daemon the panel joins it; nothing for a conversation of
// claude.
export function codexResume(row) {
    return isCodex(row) ? { agent: "codex", contour: row.profile || "" } : {};
}

// spoke says whether an archived conversation has anything in it: claude
// counts its messages, and codex the tokens its thread spent — the archive of
// codex reads no rollout and counts no messages.
export function spoke(row) {
    return isCodex(row) ? (row.tokensUsed || 0) > 0 : (row.messages || 0) > 0;
}

// noTurn says why a codex thread has no turn to stop, or nothing when it has
// one: a turn runs while codex works or waits on what it asked.
export function noTurn(s) {
    return s.status === "busy" || s.status === "waiting" ? "" : "codex is not running a turn";
}
