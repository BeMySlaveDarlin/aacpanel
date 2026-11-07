// The controls of a live session's card — its restart, its close and its pin:
// whether each can be pressed now, what it ends, and the press itself. The
// buttons of a card at a desk and the pie of a card on a phone both come here,
// so the two cannot part.
import { knows, whyNot } from "../../exec.js";
import { CODEX_CLOSE, CODEX_RESTART, codexRestartWhy, isCodex } from "../../agent.js";
import { stopsOf } from "../chat/head.js";

// busyOf names the close or the restart of a session on its way, from the
// press until the snapshot shows it done, or nothing.
export function busyOf(s, wait) {
    const closing = wait ? wait.of("close", s.session) : null;
    const restarting = wait ? wait.of("restart", s.session) : null;
    return closing ? "closing" : restarting ? "restarting" : "";
}

// endsOf says why the restart, the close and the pin of a session cannot be
// pressed now, what ends with its process (lost) and what a restart and a
// close leave. busy is a close or a restart already on its way; pin is the
// session's pinOf.
export function endsOf({ s, exec, pin, busy }) {
    const codex = isCodex(s);
    const lost = stopsOf(s);
    const ends = (note) => (lost ? `stops ${lost} · ${note}` : note);
    const under = busy ? "the session is already closing or restarting" : "";
    return {
        lost,
        restartWhy: s.sleeping ? "sleeping" : under || (codex ? codexRestartWhy(s) : "") || (knows(exec, "session.restart") ? "" : whyNot(exec, "session.restart")),
        closeWhy: under || (knows(exec, "session.close") ? "" : whyNot(exec, "session.close")),
        pinWhy: s.sleeping ? "sleeping" : pin.why || (knows(exec, "session.pin") ? "" : whyNot(exec, "session.pin")),
        restartNote: codex ? CODEX_RESTART : ends("a new session with an empty context; this one stays in the archive"),
        closeNote: codex ? CODEX_CLOSE : ends("the conversation stays in the archive"),
    };
}

// The presses, each through the gate and its confirmation.
export const restartSession = (run, s) => run("session.restart", s.session, {});
export const closeSession = (run, s) => run("session.close", s.session, {});
// pinGroup pins or unpins the whole group of a session, at its root.
export const pinGroup = (run, pin) => run("session.pin", pin.root.session, { on: !pin.on });
