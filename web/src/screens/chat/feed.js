// Assembling the conversation feed: what folds into what before it is shown.

const HIDDEN = new Set(["artifactlink"]);

// LINES are what arrives beside the conversation — a background task done, a
// hook's message, a warning of claude. One that arrives inside a run of calls
// does not end it: it hangs under the run's badges.
const LINES = new Set(["taskdone", "notice"]);

const CHIP = /^(?:\s*\[Image #\d+\])+/;

// bare returns the message text without the harness additions.
export function bare(text) {
    return String(text || "").replace(CHIP, "").trim();
}

// withoutPaths drops the file paths the host puts after a caption: a message
// sent with files reaches the session as its words and then one path a line.
function withoutPaths(text, count) {
    const lines = String(text || "").split("\n");
    let n = count;
    while (n > 0 && lines.length && /^\/\S+$/.test(lines[lines.length - 1].trim())) {
        lines.pop();
        n -= 1;
    }
    return lines.join("\n");
}

// sameReply reports whether a transcript message and a locally shown one are the same.
export function sameReply(text, local) {
    const mine = bare(local.sent !== undefined ? local.sent : local.text);
    const files = (local.from && local.from.files) || 0;
    const theirs = bare(files ? withoutPaths(text, files) : text);
    if (mine !== theirs) return false;
    return mine !== "" || theirs !== String(text || "").trim();
}

// sameShell reports whether a shell command the console ran is the one shown
// locally: the person typed it with the "!" that runs it, the transcript keeps
// the command alone.
export function sameShell(command, local) {
    const mine = bare(local.sent !== undefined ? local.sent : local.text).replace(/^!\s*/, "");
    return mine !== "" && mine === String(command || "").trim();
}

// arrived reports whether the feed already carries a row shown locally: the
// transcript echoes a message as it was sent, and a shell command as the
// command alone. A row that failed or is held has not gone anywhere yet.
export function arrived(items, local) {
    if (local.state === "failed" || local.state === "held") return false;
    return items.some((item) => (item.role === "me" && sameReply(item.text, local))
        || (item.role === "shell" && sameShell(item.text, local)));
}

// unarrived returns the local rows the feed has not echoed yet, in the order
// they were sent: these are the ones drawn after the feed. It is read at
// render time, so the echo and the local row never share a frame.
export function unarrived(local, items) {
    return local.filter((row) => !arrived(items, row));
}

// merge inserts newly arrived items into the feed.
export function merge(items, incoming) {
    if (!incoming.length) return items;
    const out = items.slice();
    for (const item of incoming) {
        const i = out.findIndex((was) => was.pos === item.pos
            && (was.role === item.role || was.role === item.fixes));
        if (i >= 0) {
            out[i] = item;
            continue;
        }
        if (item.role === "think" && tail(out).length) {
            const run = tail(out)[0].run;
            const was = tail(out).find((x) => x.role === "think");
            if (was) {
                out[out.indexOf(was)] = weldThink(was, item);
                continue;
            }
            out.push({ ...item, run });
            continue;
        }
        if (item.role === "tools" && tail(out).length) {
            const run = tail(out)[0].run;
            const at = tail(out).findIndex((was) => was.role === "tools" && was.kind === item.kind);
            if (at >= 0) {
                const was = tail(out)[at];
                const seen = new Set(was.calls.map((call) => `${call.pos}-${call.index}`));
                const fresh = (item.calls || []).filter((call) => !seen.has(`${call.pos}-${call.index}`));
                if (!fresh.length) continue;
                out[out.indexOf(was)] = { ...was, calls: [...was.calls, ...fresh] };
                continue;
            }
            out.push({ ...item, run });
            continue;
        }
        out.push(item);
    }
    return out;
}

// rows returns the feed ready for display, folding one run into a single row.
export function rows(items) {
    const done = new Map();
    const links = new Map();
    for (const item of items) {
        if (item.role === "taskdone" && item.use) done.set(item.use, item);
        if (item.role === "artifactlink" && item.use) links.set(item.use, item.url);
    }
    const seen = new Set();

    const out = [];
    for (const raw of items) {
        if (HIDDEN.has(raw.role)) continue;
        const prev = out[out.length - 1];
        if (LINES.has(raw.role) && prev && prev.role === "toolrow") {
            prev.lines = [...(prev.lines || []), raw];
            continue;
        }
        let item = raw.role === "tools" && done.size ? withDone(raw, done) : raw;
        if (item.role === "artifact") {
            const url = links.get(item.use) || "";
            item = { ...item, url, again: Boolean(url && seen.has(url)) };
            if (url) seen.add(url);
        }
        const last = out[out.length - 1];
        if ((item.role === "tools" || item.role === "think")
            && last && last.role === "toolrow" && last.run === item.run) {
            if (item.role === "think") last.think = item;
            else last.groups.push(item);
            continue;
        }
        if (item.role === "tools") {
            out.push({ role: "toolrow", run: item.run, pos: item.pos, groups: [item] });
            continue;
        }
        if (item.role === "think") {
            out.push({ role: "toolrow", run: item.run, pos: item.pos, groups: [], think: item });
            continue;
        }
        out.push(item);
    }
    return out;
}

function withDone(item, done) {
    const calls = item.calls || [];
    if (!calls.some((call) => call.use && done.has(call.use))) return item;
    return {
        ...item,
        calls: calls.map((call) => {
            const mark = call.use && done.get(call.use);
            return mark
                ? { ...call, done: { status: mark.status, summary: mark.summary, at: mark.at } }
                : call;
        }),
    };
}

// closed reports whether the question has already been answered in this feed.
export function closed(items, use) {
    return Boolean(use) && items.some((item) => item.role === "asked" && item.use === use);
}

function tail(items) {
    const out = [];
    for (let i = items.length - 1; i >= 0; i--) {
        if (HIDDEN.has(items[i].role) || LINES.has(items[i].role)) continue;
        if (items[i].role !== "tools" && items[i].role !== "think") break;
        out.unshift(items[i]);
    }
    return out;
}

// weld returns a feed where one run is one row again, however it was split.
export function weld(items) {
    const out = [];
    let spots = new Map();
    let run = null;
    for (const item of items) {
        if (HIDDEN.has(item.role) || LINES.has(item.role)) {
            out.push(item);
            continue;
        }
        if (item.role !== "tools" && item.role !== "think") {
            spots = new Map();
            run = null;
            out.push(item);
            continue;
        }
        if (run == null) run = item.run;
        const key = item.role === "think" ? "think" : `tools:${item.kind}`;
        const at = spots.get(key);
        if (at == null) {
            spots.set(key, out.length);
            out.push(item.run === run ? item : { ...item, run });
            continue;
        }
        out[at] = item.role === "think" ? weldThink(out[at], item) : weldTools(out[at], item);
    }
    return out;
}

function weldTools(was, more) {
    const calls = was.calls || [];
    const seen = new Set(calls.map((call) => `${call.pos}-${call.index}`));
    const fresh = (more.calls || []).filter((call) => !seen.has(`${call.pos}-${call.index}`));
    return fresh.length ? { ...was, calls: [...calls, ...fresh] } : was;
}

function weldThink(was, more) {
    return {
        ...was,
        count: was.count + more.count,
        tokens: (was.tokens || 0) + (more.tokens || 0),
        spots: [...(was.spots || []), ...(more.spots || [])],
    };
}

// runCalls returns every call of one run, in event order.
export function runCalls(items, run) {
    return callsOf(items, (item) => item.run === run);
}

// turnCalls returns every call of the turn that ended at this position: the
// runs between the end of the turn before it and this one.
export function turnCalls(items, pos) {
    const end = items.findIndex((item) => item.role === "turn" && item.pos === pos);
    if (end < 0) return [];
    let start = end;
    while (start > 0 && items[start - 1].role !== "turn") start -= 1;
    const runs = new Set(items.slice(start, end)
        .filter((item) => item.role === "tools" || item.role === "think")
        .map((item) => item.run));
    return callsOf(items, (item) => runs.has(item.run));
}

function callsOf(items, pick) {
    const done = new Map();
    for (const item of items) {
        if (item.role === "taskdone" && item.use) done.set(item.use, item);
    }
    const all = [];
    for (const item of items) {
        if (!pick(item)) continue;
        if (item.role === "think") {
            for (const spot of item.spots || []) {
                all.push({ ...spot, kind: "think", still: true });
            }
            continue;
        }
        if (item.role !== "tools") continue;
        for (const call of item.calls || []) {
            const mark = call.use && done.get(call.use);
            all.push({
                ...call,
                kind: item.kind,
                done: mark ? { status: mark.status, summary: mark.summary, at: mark.at } : undefined,
            });
        }
    }
    return all.sort((a, b) => (a.pos - b.pos) || ((a.index || 0) - (b.index || 0)));
}
