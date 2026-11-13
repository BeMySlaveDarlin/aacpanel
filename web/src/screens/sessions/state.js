// The one vocabulary of how a live session stands, read by its card in the
// lists and by the head of its conversation alike, so the two never say it
// in different words: the word or two of what it does now, the subject of
// what it waits for, the tone it is painted in, the glyph that stands for it,
// and when the state began or ends.
//
// tone is the family of colour and of rank: wait lifts the session's block
// into what needs the person, held waits at the parent and does not, busy is
// an answer at work, bg work the answer left running, limit a session stopped
// by the limit of its subscription, paused and idle the quiet ones. kind
// names the state within its tone, and picks the glyph.

import { alarmTime, plural } from "../../format.js";
import { contourOf, limitState } from "../../contour.js";
import { waitingOf } from "../../waiting.js";
import { waitWords } from "../../ui/waits.js";
import { seenAt as seenHere } from "../../viewing.js";
import { shown } from "../chat/answered.js";
import { checklistOf } from "../chat/checklist.js";

// PAUSE_NO_END is the alarm a session keeps through a pause with no end: it
// repeats only to keep the mark of the pause, and its ring is not the end.
export const PAUSE_NO_END = "pause-no-end";

// The word each kind of outstanding work opens its line with.
const OUTSTANDING = { question: "asks", request: "request", brief: "brief", secret: "secret", call: "called you" };

// runsOf counts what of a session's background work runs, by the counter
// its card shows it under: subagents and workflows, the commands it left
// running bar its wakes, and the observers it asked the panel for. A timer
// runs nothing — a wake the session set itself or an alarm the panel holds
// for it — and the session sleeps until it.
export function runsOf(work) {
    const w = work || {};
    return {
        agents: (w.agents || 0) + (w.workflows || 0),
        commands: Math.max(0, (w.tasks || 0) - (w.wakes || 0)),
        watchers: w.observers || 0,
    };
}

// runningOf is the whole of what runs in the background: one with only
// timers ahead is idle.
export function runningOf(work) {
    const r = runsOf(work);
    return r.agents + r.commands + r.watchers;
}

// backgroundOf counts the background tasks of a session as the snapshot counts
// them: its own, and the observers and the alarms it asked the panel for —
// they end with it the same.
export function backgroundOf(work) {
    const w = work || {};
    return (w.tasks || 0) + (w.observers || 0) + (w.alarms || 0);
}

// workOf says what a session has in its background, for a tip: its agents
// and its background tasks.
export function workOf(work) {
    const agents = (work && work.agents) || 0;
    const tasks = backgroundOf(work);
    return [
        agents > 0 && `${agents} ${plural(agents, "agent", "agents")}`,
        tasks > 0 && `${tasks} ${plural(tasks, "background task", "background tasks")}`,
    ].filter(Boolean).join(" · ");
}

// msOf reads a time of the snapshot in milliseconds: the collector gives the
// status time as milliseconds of the epoch, the service its times as ISO.
function msOf(at) {
    if (typeof at === "number") return Number.isFinite(at) ? at : 0;
    return Date.parse(at || "") || 0;
}

// endedAt is when the last turn of a quiet session ended, in milliseconds.
function endedAt(s) {
    return msOf(s.statusUpdatedAt) || msOf(s.lastRequestAt);
}

// unseenOf says whether the last turn of a quiet session ended after the
// person last had its conversation open, on any device or on this one. A
// row that carries no seen mark at all says nothing either way.
function unseenOf(s, viewed) {
    if (viewed || !Object.hasOwn(s, "seenAt")) return false;
    const ended = endedAt(s);
    const seen = Math.max(Date.parse(s.seenAt || "") || 0, seenHere(s.session));
    return ended > 0 && ended > seen;
}

// limitOf reads the limit a session stopped at: the window it names, and
// when that window starts over, in milliseconds; null when it runs free.
function limitOf(s, limits) {
    const hit = limitState(s, limits);
    if (!hit) return null;
    if (hit.tone === "wait") return { over: true, window: "", until: 0 };
    const c = contourOf(limits, s.profile, s.contour);
    const keys = s.limitHit.window === "5h" ? ["fiveHour"] : s.limitHit.window === "7d" ? ["sevenDay"] : ["fiveHour", "sevenDay"];
    const now = Date.now() / 1000;
    const ends = keys.map((key) => (c && c[key] && c[key].resetsAt) || 0).filter((at) => at > now);
    return { over: false, window: s.limitHit.window || "", until: ends.length ? Math.max(...ends) * 1000 : 0 };
}

// stateOf says how a live session stands. limits are the subscription limits
// of the snapshot; viewed is true while the person has the conversation open
// here, which settles its call and its unseen end. A question or a
// permission answered from this page no longer has the session waiting,
// whatever the snapshot has not caught up with yet (see shown in answered.js).
//
// The state carries: tone and kind (above); word, subject, mono and note, the
// parts of its line — "allow" "Bash" "tar -xf", "asks" "Layout" "2 questions";
// text, the same as one string; since, when it began (ISO), or until, when
// it ends (milliseconds) and clock, whether that end reads as a time of day;
// unseen; and work, what runs in the background, for a tip.
export function stateOf(row, limits = null, { viewed = false } = {}) {
    const s = shown(row);
    const make = (tone, kind, word, more = {}) => {
        const state = { tone, kind, word, subject: "", mono: "", note: "", since: s.statusUpdatedAt || "", until: 0,
            clock: false, unseen: false, work: workOf(s.work), ...more };
        state.text = [[state.word, state.subject, state.mono].filter(Boolean).join(" "), state.note]
            .filter(Boolean).join(" · ");
        return state;
    };

    if (s.sleeping) {
        const until = Date.parse(s.pauseAlarm?.next || "") || 0;
        return make("paused", "paused", "sleeping", { subject: until ? `until ${pauseAt(until)}` : "" });
    }
    const phase = s.restartPhase;
    if (phase && phase.phase === "saving") {
        return make("busy", "saving", phase.finish === "close" ? "saving before close" : phase.finish === "finalize" ? "wrapping up" : "saving for restart", { until: Date.parse(phase.forceAt || "") || 0 });
    }
    if (s.restarting || (phase && phase.phase === "forcing")) {
        return make("busy", "restarting", phase?.finish === "close" ? "closing" : "restarting", { until: phase ? Date.parse(phase.forceAt || "") || 0 : 0 });
    }
    const limit = limitOf(s, limits);
    if (limit && limit.over) return make("wait", "limit", "stopped by the limit");
    if (limit) {
        return make("limit", "limit", limit.window ? `${limit.window} limit` : "limit", { until: limit.until, clock: true });
    }
    // A question a child asked is held at its parent first: the child waits
    // there, and the person is asked only when the parent lets it through.
    const held = s.parentQuestion;
    if (held && held.parent) {
        const due = Date.parse(held.due || "") || 0;
        return make("held", "held", "asked", { subject: held.parent, note: due ? `to you ${inText(due)}` : "" });
    }
    // A question codex asked without waiting for it leaves the thread at work:
    // it asks the person, and works on.
    if (s.ask) {
        const count = s.ask.count || 1;
        const going = s.ask.async && s.status === "busy";
        return make("wait", "ask", "asks", {
            subject: s.ask.header || "a question",
            note: going ? "works on" : count > 1 ? `${count} ${plural(count, "question", "questions")}` : "",
            since: s.ask.at || s.statusUpdatedAt || "",
        });
    }
    if (s.permission) {
        return make("wait", "permission", "allow", { subject: s.permission.tool || "a tool", mono: s.permission.head || "" });
    }
    if (s.waitingFor || s.status === "waiting") {
        const said = waitWords(s.waitingFor);
        return make("wait", said.permission ? "permission" : "input", said.word, { subject: said.subject });
    }
    // A question asked in the conversation goes first: it holds the turn up;
    // the rest stand in the order the session left them.
    const outstanding = waitingOf(s, viewed);
    if (outstanding.length > 0) {
        const first = outstanding.find((item) => item.kind === "question") || outstanding[0];
        return make("wait", first.kind, OUTSTANDING[first.kind], {
            subject: first.kind === "call" ? "" : first.title || "",
            note: outstanding.length > 1 ? `${outstanding.length - 1} more` : "",
            since: first.at || s.statusUpdatedAt || "",
        });
    }
    if (s.compacting) return make("busy", "compact", "compacting", { since: s.compacting });
    if (s.status === "busy" && !s.turnOver) return make("busy", "work", "working");
    // The answer is over and what it started still runs: the session takes a
    // message, but it is not quiet.
    const runs = runsOf(s.work);
    if (runs.agents > 0 || (s.status === "busy" && runningOf(s.work) === 0)) return make("bg", "inside", "agents running");
    if (runs.commands > 0) return make("bg", "inside", "commands running");
    if (runs.watchers > 0) return make("bg", "inside", "watching");
    if (s.noRequests) return make("idle", "fresh", "no replies yet");
    if (s.pauseAlarm) {
        if (s.pauseAlarm.name === PAUSE_NO_END) return make("paused", "paused", "paused", { subject: "with no end" });
        const until = Date.parse(s.pauseAlarm.next || "") || 0;
        return make("paused", "paused", "paused", { subject: until ? `until ${pauseAt(until)}` : "" });
    }
    const list = checklistOf(s.checklist);
    const unseen = unseenOf(s, viewed);
    const word = s.finalizedAt ? "finalized" : list && list.over ? "done" : "idle";
    return make("idle", word, word, { unseen, note: unseen ? "not seen" : "" });
}

// inText says how soon a moment comes, for a line: "in 3m", "now".
function inText(ms) {
    const left = leftText(ms);
    return left === "now" ? "now" : `in ${left}`;
}

// shortText says how long a state has lasted the way the card's corner does:
// seconds under a minute, minutes under an hour, hours and minutes under
// ten hours, whole hours after.
export function shortText(sec) {
    const s = Math.max(0, Math.round(sec));
    if (s < 60) return `${s}s`;
    const m = Math.round(s / 60);
    if (m < 60) return `${m}m`;
    if (m < 600) {
        const h = Math.floor(m / 60);
        const r = m % 60;
        return r >= 10 ? `${h}h${String(r).padStart(2, "0")}` : `${h}h`;
    }
    return `${Math.floor(m / 60)}h`;
}

// leftText says how much is left until a moment, as shortText says a span.
function leftText(ms) {
    const sec = (ms - Date.now()) / 1000;
    return sec <= 0 ? "now" : shortText(sec);
}

// clockText names a moment by the time of day while it is within the day
// ahead, and by its weekday past that.
// pauseAt is when a pause ends, beside its word: the time of day, with the
// date when it is not today.
function pauseAt(ms) {
    const at = new Date(ms);
    if (at.toDateString() === new Date().toDateString()) return alarmTime(ms);
    const day = `${String(at.getDate()).padStart(2, "0")}.${String(at.getMonth() + 1).padStart(2, "0")}`;
    return `${day} ${alarmTime(ms)}`;
}

function clockText(ms) {
    if (ms - Date.now() > 20 * 3600e3) return new Date(ms).toLocaleDateString("en-GB", { weekday: "short" });
    return alarmTime(ms);
}

// timeOf is the corner of a card: how long the state has lasted, or, for a
// state that ends at a known moment, an arrow and that moment — the time of
// day a pause or a limit ends at, how soon a restart is forced.
export function timeOf(state) {
    if (state.until) return { until: true, text: state.clock ? clockText(state.until) : leftText(state.until) };
    const at = msOf(state.since);
    if (!at) return { until: false, text: "" };
    return { until: false, text: shortText((Date.now() - at) / 1000) };
}
