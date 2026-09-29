// The two places a live session lives in — tmux and the stream — and the move
// between them: the same conversation, closed in one and resumed in the other.

import { useEffect, useRef, useState } from "preact/hooks";

import { html } from "../../html.js";
import { knows, whyNot } from "../../exec.js";
import { plural } from "../../format.js";
import { hostLabel } from "../../actions/registry.js";
import { liveWork } from "./work.js";

const asking = { to: "", reason: "asking the panel where this session can move", pending: true };

// stops names what runs inside the process and ends with it. A resumed
// conversation does not bring it back, so the person reads it before pressing.
export function stops(work) {
    const { tasks, agents, flows } = liveWork(work);
    const out = [];
    if (tasks.length) out.push(`${tasks.length} ${plural(tasks.length, "background task", "background tasks")}`);
    if (agents.length) out.push(`${agents.length} ${plural(agents.length, "agent", "agents")}`);
    if (flows.length) out.push(`${flows.length} ${plural(flows.length, "workflow", "workflows")}`);
    return out.join(", ");
}

// blocked says why the session cannot move right now, or nothing: a switch
// happens between turns, never under an open question, and never before the
// session has taken what was sent to it.
export function blocked(live) {
    if (!live) return "the session is not live";
    if (live.status === "busy") return "the session is answering — switch once it finishes";
    if (live.status === "waiting") return "the session is waiting for an answer — answer it first";
    if (live.queued > 0) {
        return `the session has not taken ${live.queued} ${plural(live.queued, "message", "messages")} yet — `
            + "switch once it does";
    }
    return "";
}

// moveSession asks the executor to move a live session to where it can go,
// with what stops on the way named for the sheet. A window asked for comes up
// over tmux, where the session moves.
export function moveSession({ run, exec, name, to, work, withWindow = false }) {
    if (!knows(exec, "session.switch")) return;
    const lost = stops(work);
    run("session.switch", name, { to, ...(withWindow ? { window: true } : {}), force: Boolean(lost), stops: lost });
}

// useSwitchWay asks the panel which way a live session can move: a session on
// the stream can always go to tmux, a session in tmux goes to the stream only
// when its project lives there. It asks again when the session moves.
export function useSwitchWay(name, transport) {
    const [answer, setAnswer] = useState({ for: null, ...asking });
    const key = `${name}|${transport}`;

    useEffect(() => {
        if (!name) return undefined;
        let alive = true;
        fetch(`/api/session/switch?name=${encodeURIComponent(name)}`, { credentials: "same-origin" })
            .then(async (r) => {
                if (!r.ok) throw new Error((await r.text()).trim() || `the server answered ${r.status}`);
                return r.json();
            })
            .then((body) => {
                if (alive) setAnswer({ for: key, to: body.to || "", reason: body.reason || "" });
            })
            .catch((err) => {
                if (alive) setAnswer({ for: key, to: "", reason: String(err.message || err) });
            });
        return () => {
            alive = false;
        };
    }, [key]);

    return answer.for === key ? answer : asking;
}

// sidesOf lays the pair of views over where a live session lives. On the
// stream the feed is all there is, and the terminal is tmux on the other side,
// reached by the move. A session in tmux is watched either way — its terminal
// or its transcript as a feed — and the pair only picks which: the session
// stays in tmux, where whoever started it may be reading its pane. A session
// in tmux whose project lives on the stream moves there from the tools of the
// session, unless a window on the host holds it.
export function sidesOf({ live, way, held, picked, canTerm, exec }) {
    const why = () => blocked(live) || whyNot(exec, "session.switch");
    if (live.transport === "stream") {
        if (way.to !== "console") return { view: "feed", pair: false, moves: "", tip: "", why: "" };
        return { view: "feed", pair: false, moves: "console", tip: "Move to tmux", why: why() };
    }
    if (way.to === "stream" && !held) {
        return { view: picked, pair: canTerm, moves: "stream", tip: "", why: why() };
    }
    const tip = way.to === "stream" && held
        ? `Watch it as a feed — the window on ${hostLabel()} holds the session in tmux`
        : "";
    return { view: picked, pair: canTerm, moves: "", tip, why: "" };
}

// useMove says the session is between its places: from the press until the
// snapshot shows it on the other side and the panel has said what the pair of
// views does there. Neither view is worth showing meanwhile — the one being
// left closes under the person, the one being reached is not up yet — so the
// screen shows the move. A move that failed, or a session that did not come
// back, ends it at once: the note of the gate says why.
export function useMove(wait, name, live, way) {
    const task = wait && name ? wait.of("switch", name) : null;
    const held = useRef(null);
    if (task) {
        held.current = { to: task.params && task.params.to === "console" ? "console" : "stream", since: task.since };
    } else if (held.current && (!live || !way.pending)) {
        held.current = null;
    }
    return held.current;
}

// MoveScreen stands where the view was while the session moves.
export function MoveScreen({ move }) {
    const [, tick] = useState(0);
    useEffect(() => {
        const timer = setInterval(() => tick((n) => n + 1), 1000);
        return () => clearInterval(timer);
    }, []);
    const sec = Math.max(0, Math.round((Date.now() - move.since) / 1000));
    const toTmux = move.to === "console";
    return html`
        <div class="viewwait" role="status" aria-live="polite">
            <span class="spin"></span>
            <p class="viewwaittitle">${toTmux ? "Moving to tmux" : "Moving to the stream"}</p>
            <p class="viewwaitsub">${toTmux
                ? "the stream closes and the same conversation comes up in tmux"
                : "tmux closes and the same conversation comes up on the stream"} · ${sec} s</p>
        </div>
    `;
}
