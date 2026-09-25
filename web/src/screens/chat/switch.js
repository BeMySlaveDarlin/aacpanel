// The two sides a live session lives on — the console and the feed — and the
// move between them: the same conversation, closed on one side and resumed on
// the other.

import { useEffect, useState } from "preact/hooks";

import { knows, whyNot } from "../../exec.js";
import { plural } from "../../format.js";
import { hostLabel } from "../../actions/registry.js";
import { liveWork } from "./work.js";

const asking = { to: "", reason: "asking the panel where this session can move" };

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

// moveSession asks the executor to move a live session to the other side, with
// what stops on the way named for the sheet. A window asked for comes up over
// the console the session moves to.
export function moveSession({ run, exec, name, to, work, withWindow = false }) {
    if (!knows(exec, "session.switch")) return;
    const lost = stops(work);
    run("session.switch", name, { to, ...(withWindow ? { window: true } : {}), force: Boolean(lost), stops: lost });
}

// useSwitchWay asks the panel which way a live session can move: a session on
// the stream can always go to the console, a console goes to the feed only
// when its project lives there. It asks again when the session changes sides.
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

// sidesOf lays the pair of views over the sides of a live session. On the
// stream the feed is all there is, and the terminal moves the session to the
// console. A console whose project lives in the feed shows its terminal, and
// the feed moves it there — unless a window on the host holds it: then the
// pair only picks what to watch the console with. Anywhere else the pair is
// the choice of the device, as it always was.
export function sidesOf({ live, way, held, picked, canTerm, exec }) {
    const why = () => blocked(live) || whyNot(exec, "session.switch");
    if (live.transport === "stream") {
        if (way.to !== "console") return { view: "feed", pair: false, moves: "", tip: "", why: "" };
        return { view: "feed", pair: true, moves: "console", tip: "Move to the console", why: why() };
    }
    if (way.to === "stream" && canTerm && !held) {
        return { view: "term", pair: true, moves: "stream", tip: "Move to the feed", why: why() };
    }
    const tip = way.to === "stream" && held
        ? `Watch the console as a feed — the window on ${hostLabel()} holds the session there`
        : "";
    return { view: picked, pair: canTerm, moves: "", tip, why: "" };
}
