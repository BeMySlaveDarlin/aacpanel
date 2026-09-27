// Where every item of a feed lands once it is cut into episodes: the checks
// ask this rather than the markup, so that a row nobody draws shows up as a
// row with no place at all.
import { rows, weld } from "../../src/screens/chat/feed.js";
import { episodes, isWaiting, marksCalls } from "../../src/screens/chat/episodes.js";
import { items as roles } from "./feedroles.js";

const keyOf = (item) => `${item.role}:${item.pos}`;

// places returns, for every item of the feed given — or of the feed of every
// role when none is — the places it was put in, and the shape of each
// episode. An item folded into a run is found by its calls or its thinking.
export function places(given, busy = true, marks = null) {
    const items = given || roles;
    const list = rows(weld(items));
    const eps = episodes(list, busy, marks == null ? marksCalls(items) : marks);
    const found = new Map();
    const put = (key, place) => {
        if (!found.has(key)) found.set(key, []);
        found.get(key).push(place);
    };
    const callsOf = new Map();
    const thinkOf = new Map();
    for (const item of items) {
        if (item.role === "tools") for (const call of item.calls || []) callsOf.set(`${call.pos}-${call.index}`, keyOf(item));
        if (item.role === "think") for (const spot of item.spots || []) thinkOf.set(spot.at, keyOf(item));
    }
    const putRun = (row, place) => {
        for (const group of row.groups || []) {
            for (const call of group.calls || []) put(callsOf.get(`${call.pos}-${call.index}`), place);
        }
        if (row.think) for (const spot of row.think.spots || []) put(thinkOf.get(spot.at), place);
        for (const line of row.lines || []) put(keyOf(line), place);
    };
    eps.forEach((ep, n) => {
        for (const row of ep.heads) put(keyOf(row), `head@${n}`);
        for (const entry of ep.log) {
            for (const row of entry.texts) put(keyOf(row), `log@${n}`);
            if (entry.row) putRun(entry.row, `log@${n}`);
        }
        for (const row of ep.outcome) put(keyOf(row), `outcome@${n}`);
        for (const row of ep.shown) put(keyOf(row), `shown@${n}`);
        if (ep.turn) put(keyOf(ep.turn), `turn@${n}`);
    });
    for (const row of list) {
        if (isWaiting(row)) put(keyOf(row), "waiting");
        if (row.role === "artifact" && row.url) {
            const link = items.find((item) => item.role === "artifactlink" && item.url === row.url);
            if (link) put(keyOf(link), "url of its artifact");
        }
    }
    // A set of the same key twice from one call is one place, not two.
    const where = {};
    for (const item of items) where[keyOf(item)] = [...new Set(found.get(keyOf(item)) || [])];
    return {
        where,
        order: items.map(keyOf),
        eps: eps.map((ep) => ({
            how: ep.how,
            open: ep.open,
            heads: ep.heads.map(keyOf),
            outcome: ep.outcome.map(keyOf),
            settled: ep.settled.length,
            log: ep.log.length,
            files: ep.files.map((f) => f.name),
            now: ep.now && {
                running: ep.now.running,
                call: ep.now.call ? ep.now.call.name : "",
                said: ep.now.said ? keyOf(ep.now.said) : "",
                thought: ep.now.thought ? keyOf(ep.now.thought) : "",
            },
        })),
    };
}
