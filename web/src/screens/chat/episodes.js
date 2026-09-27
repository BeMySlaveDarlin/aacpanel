// The conversation as episodes: what the person put in, the work it set going
// and what came of it. Episodes are cut from the rows the feed draws — rows()
// of feed.js — so a run is already one toolrow there, with the lines that
// hang under its badges.
//
// Nothing is read for meaning, only for shape: who spoke, in what order, how
// long a reply is. Every row lands in one place of an episode: its head, the
// log of its work, what came of it, what was handed over, or its turn.

import { KIND_NAMES } from "./labels.js";

// What the person puts in: every one opens an episode. A letter the session
// sent is not one of them — it is a call of its work, drawn as a letter.
const INPUTS = new Set(["me", "asked", "permitted", "shell", "mail", "wake", "command"]);
// What rides on an input: the pictures of a message, the output of a command.
const RIDERS = new Set(["shots", "shellout"]);
// What the session hands over to the person.
export const DELIVERIES = new Set(["sent", "artifact", "brief"]);
// What arrives beside the conversation: a task done, a warning of claude.
const LINES = new Set(["taskdone", "notice"]);

// isInput reports whether a row is something the person put in and the
// session read. A message still in the queue of the session has opened
// nothing yet, and one taken back never will.
export function isInput(row) {
    return INPUTS.has(row.role) && !(row.role === "mail" && row.dir === "out") && !row.state;
}

// isWaiting reports whether a row is a message the session has not read yet.
// It stands after everything the session did meanwhile, with the messages
// still on their way from this screen, and opens its episode once read.
export function isWaiting(row) {
    return row.role === "me" && Boolean(row.state) && row.state !== "withdrawn";
}

// onTheWay reports whether a row is said in the course of the work: a reply,
// a thought, a letter to someone else, a line arriving beside it.
function onTheWay(row) {
    return row.role === "ai" || row.role === "mind" || LINES.has(row.role)
        || (row.role === "mail" && row.dir === "out");
}

// A report is a reply long enough, or laid out in paragraphs or a list, to be
// read as one. Work that goes on after it goes on without the person.
const REPORT_CHARS = 280;

export function isReport(row) {
    if (!row || row.role !== "ai") return false;
    const text = String(row.text || "");
    return text.length >= REPORT_CHARS || /\n\s*\n|\n\s*[-*] /.test(text);
}

const ms = (iso) => (iso ? Date.parse(iso) : NaN);

// marksCalls reports whether the host says of a call whether it is still out
// or has failed. Without it the last call of a busy session is taken to be
// the one running, which is all a feed without the marks can tell.
export function marksCalls(items) {
    return (items || []).some((item) => item.role === "tools"
        && (item.calls || []).some((call) => "open" in call || "failed" in call));
}

// episodes cuts the rows at every input of the person and at the end of a
// turn. A report followed by more work closes its episode too: the work after
// it gets one of its own, headed as having gone on without the person. The
// last episode of a busy session is open: it has no outcome yet, and what it
// does now is its last step.
export function episodes(list, busy = false, marks = false) {
    const out = [];
    let ep = null;
    let report = -1;
    const open = (heads, how = "") => {
        ep = { heads, how: how || (out.length === 0 && heads.length === 0 ? "earlier" : ""), entries: [], turn: null };
        out.push(ep);
        report = -1;
    };
    for (const row of list) {
        if (isWaiting(row)) continue;
        if (isInput(row)) {
            // Inputs one after another, before any work, are one episode.
            if (ep && ep.heads.length && !ep.how && !ep.entries.length) ep.heads.push(row);
            else open([row]);
            continue;
        }
        if (!ep) open([]);
        if (row.role === "turn") {
            ep.turn = row;
            ep = null;
            continue;
        }
        // What rides on an input, and a line or a note that comes before any
        // work, stands with the head: it is what the episode began with.
        if (!ep.entries.length && ep.how !== "continued"
            && (RIDERS.has(row.role) || LINES.has(row.role) || row.role === "note")) {
            ep.heads.push(row);
            continue;
        }
        if (row.role === "toolrow" && report >= 0) {
            const moved = ep.entries.splice(report + 1);
            open([], "continued");
            ep.entries.push(...moved);
        }
        ep.entries.push(row);
        if (isReport(row)) report = ep.entries.length - 1;
    }
    return out.map((one, n) => finish(one, busy && n === out.length - 1 && !one.turn, marks));
}

function keyOf(ep) {
    const first = ep.heads[0] || ep.entries[0] || ep.turn;
    return first ? `${ep.how || "ep"}:${first.role}:${first.pos}` : ep.how || "ep";
}

// finish lays an episode out: the log of its work — what was said on the way
// and the run it led to, one step a run — what came of it, what it handed
// over, and the files its replies named.
function finish(ep, open, marks) {
    const log = [];
    let said = [];
    const shown = [];
    for (const row of ep.entries) {
        if (row.role === "toolrow") {
            log.push({ key: `run:${row.run}:${row.pos}`, texts: said, row, files: [] });
            said = [];
            continue;
        }
        if (onTheWay(row)) {
            said.push(row);
            continue;
        }
        // Handed over, noted by the harness, or of a kind this screen does
        // not know yet: drawn in the episode, where the eye is.
        shown.push(row);
    }
    // After the last run the replies are what came of the episode, unless the
    // session is still at it: then they are what it says on the way. An
    // episode cut off in the middle of its work — the person spoke first —
    // ends on the last thing it said, which stays in its log as well.
    const trailing = open ? [] : said.filter((row) => row.role === "ai");
    for (const row of said) {
        if (trailing.includes(row)) continue;
        log.push({ key: `said:${row.role}:${row.pos}`, texts: [row], row: null, files: [] });
    }
    const replies = ep.entries.filter((row) => row.role === "ai");
    const outcome = open || trailing.length || !replies.length ? trailing : [replies[replies.length - 1]];
    // The files a reply names are the files of the run before it.
    let step = null;
    for (const entry of log) {
        for (const text of entry.texts) {
            if (text.role !== "ai") continue;
            addFiles((step || entry).files, text.files);
        }
        if (entry.row) step = entry;
    }
    if (step) for (const reply of trailing) addFiles(step.files, reply.files);
    const files = [];
    for (const row of ep.entries) if (row.role === "ai") addFiles(files, row.files);

    const done = {
        key: keyOf(ep),
        heads: ep.heads,
        how: ep.how,
        turn: ep.turn,
        entries: ep.entries,
        log,
        outcome,
        shown,
        files,
        open,
        ...spanOf([...ep.heads, ...ep.entries, ...(ep.turn ? [ep.turn] : [])]),
    };
    done.work = sumOf(log);
    // The work settled so far: all of it, or on an open episode what stands
    // before the step going on now.
    const last = open ? lastStep(log) : -1;
    done.settled = last >= 0 ? log.slice(0, last) : log;
    done.now = open ? nowOf(done, last, marks) : null;
    return done;
}

function addFiles(into, files) {
    for (const file of files || []) {
        const key = file.path || file.name;
        if (into.some((was) => (was.path || was.name) === key)) continue;
        into.push(file);
    }
}

function lastStep(log) {
    for (let i = log.length - 1; i >= 0; i--) if (log[i].row) return i;
    return -1;
}

// timesOf lists every moment a row carries: its own, those of its calls and
// of its thinking.
function timesOf(row) {
    const out = [];
    if (row.at) out.push(ms(row.at));
    for (const group of row.groups || []) {
        if (group.at) out.push(ms(group.at));
        for (const call of group.calls || []) if (call.at) out.push(ms(call.at));
    }
    if (row.think) {
        if (row.think.at) out.push(ms(row.think.at));
        for (const spot of row.think.spots || []) if (spot.at) out.push(ms(spot.at));
    }
    return out.filter(Number.isFinite);
}

function spanOf(rows) {
    const all = rows.flatMap(timesOf);
    return all.length ? { from: Math.min(...all), to: Math.max(...all) } : { from: null, to: null };
}

const KIND_ORDER = Object.keys(KIND_NAMES);

// sumOf adds up a piece of work: its thinking, its calls a kind, the runs
// they came from, the thoughts and replies said on the way, and its span.
export function sumOf(log) {
    const kinds = [];
    const runs = [];
    let think = 0;
    let thinkTokens = 0;
    let minds = 0;
    let said = 0;
    let other = 0;
    const rows = [];
    for (const entry of log) {
        for (const text of entry.texts) {
            rows.push(text);
            if (text.role === "mind") minds += 1;
            else if (text.role === "ai") said += 1;
            else other += 1;
        }
        const row = entry.row;
        if (!row) continue;
        rows.push(row);
        if (!runs.includes(row.run)) runs.push(row.run);
        if (row.think) {
            think += row.think.count || 0;
            thinkTokens += row.think.tokens || 0;
        }
        for (const group of row.groups || []) {
            const calls = group.calls || [];
            if (!calls.length) continue;
            const failed = calls.filter((call) => call.failed).length;
            const was = kinds.find((k) => k.kind === group.kind);
            if (was) {
                was.count += calls.length;
                was.failed += failed;
            } else {
                kinds.push({ kind: group.kind, count: calls.length, failed });
            }
        }
    }
    const place = (kind) => {
        const at = KIND_ORDER.indexOf(kind);
        return at < 0 ? KIND_ORDER.length : at;
    };
    kinds.sort((a, b) => place(a.kind) - place(b.kind));
    const calls = kinds.reduce((n, k) => n + k.count, 0);
    return { kinds, runs, think, thinkTokens, calls, minds, said, other, ...spanOf(rows) };
}

const later = (a, b) => (a.pos - b.pos) || ((a.index || 0) - (b.index || 0));

// nowOf is what an open episode does now: the last call of its last step,
// whether it is still out, since when, what the session said last and the
// latest thought it had. A call is running when the host says it is still
// out; a host that marks nothing leaves the last call running until anything
// arrives after it.
function nowOf(ep, last, marks) {
    const step = last >= 0 ? ep.log[last] : null;
    let call = null;
    let kind = "other";
    if (step) {
        for (const group of step.row.groups || []) {
            for (const one of group.calls || []) {
                if (!call || later(one, call) > 0) {
                    call = one;
                    kind = group.kind;
                }
            }
        }
    }
    const spoken = [...(step ? step.texts : []), ...ep.log.slice(last + 1).flatMap((e) => e.texts)]
        .filter((row) => row.role === "ai" || row.role === "mind");
    const said = spoken[spoken.length - 1] || null;
    const minds = ep.entries.filter((row) => row.role === "mind");
    const latest = minds[minds.length - 1] || null;
    const lastAt = ep.to;
    const callAt = call ? ms(call.at) : NaN;
    const running = Boolean(call) && (call.open === true
        || (!marks && Number.isFinite(callAt) && (lastAt == null || callAt >= lastAt)));
    return {
        step,
        call,
        kind,
        running,
        since: running ? callAt : lastAt,
        said,
        thought: latest && latest !== said ? latest : null,
    };
}

// firstPara is the head of a reply: its paragraphs until there is a sentence's worth.
export function firstPara(text) {
    const paras = String(text || "").split(/\n\s*\n/);
    const out = [];
    for (const p of paras) {
        out.push(p);
        if (out.join(" ").length >= 60) break;
    }
    return out.join("\n\n");
}

// lineCount is how many lines of text a reply has, blank ones aside.
export function lineCount(text) {
    return String(text || "").split("\n").filter((line) => line.trim()).length;
}
