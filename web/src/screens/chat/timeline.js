// The feed as a column of prose with the timeline of the work beside it.
//
// book() takes the rows feed.js folds and splits them: what is said and what
// arrives — answers, thoughts, the person, letters, files, tasks done — stays
// in the column; a run of calls and the end of a turn become a mark on the
// timeline, tied to the row that follows them.
//
// The timeline scrolls with the feed because it is in the feed: one scroll
// box, two columns, and every mark stands at the height of its row.
//   phone — a strip as wide as a finger; marks that would run into each
//           other become one mark with the sum, and a tap opens their calls.
//   desk  — a column with the time, what the work was, its calls by name and
//           the files it touched; an entry that would run into the one above
//           it stands under it instead.

import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "preact/hooks";

import { html } from "../../html.js";
import { Icon } from "../../ui/icons.js";
import { stopwatch } from "../../format.js";
import { callWord, countCalls, KIND_NAMES, kindIcon, stampText, turnLeft, turnTook } from "./labels.js";

const WORK = new Set(["toolrow", "turn"]);

// The height of a mark on the strip and the least room between two: marks
// closer than both become one.
export const MARK_H = 34;
export const MARK_GAP = 4;

// The room between two entries of the desk column.
export const ENTRY_GAP = 10;

function doneName(summary) {
    const quoted = /"(.+)"/.exec(summary || "");
    return quoted ? quoted[1] : (summary || "a background task");
}

// book splits the rows into the column and the marks of the timeline. A mark
// knows the index of the row it stands beside: the first row after its work,
// or the end of the column when nothing has followed it yet. Tasks that ended
// side by side become one row; a thought after a thought carries no word of
// its own; a letter of a subagent is signed with the task the subagent ran.
export function book(rows) {
    const names = new Map();
    for (const r of rows) {
        for (const x of r.role === "toolrow" ? (r.lines || []) : [r]) {
            if (x.role === "taskdone" && x.task) names.set(x.task, doneName(x.summary));
        }
    }
    const prose = [];
    const marks = [];
    let pending = [];
    const push = (r) => {
        const had = pending.length > 0;
        if (had) {
            marks.push({ at: prose.length, rows: pending });
            pending = [];
        }
        const last = prose[prose.length - 1];
        if (r.role === "taskdone") {
            if (!had && last && last.role === "tasks") {
                last.list = [...last.list, r];
                return;
            }
            prose.push({ role: "tasks", pos: r.pos, at: r.at, list: [r] });
            return;
        }
        if (r.role === "mind") {
            prose.push({ ...r, head: !(last && last.role === "mind") });
            return;
        }
        if (r.role === "mail" && names.has(r.from)) {
            prose.push({ ...r, whoName: names.get(r.from) });
            return;
        }
        prose.push(r);
    };
    for (const r of rows) {
        if (WORK.has(r.role)) {
            pending.push(r);
            for (const line of r.lines || []) push(line);
            continue;
        }
        push(r);
    }
    if (pending.length) marks.push({ at: prose.length, rows: pending, end: true });
    return { prose, marks };
}

// sumOf is what a group of marks did: calls by kind, thinking, the ends of
// turns, the runs to open and the calls themselves in order.
export function sumOf(rows) {
    const kinds = new Map();
    let think = 0;
    const runs = [];
    const turns = [];
    const calls = [];
    for (const r of rows) {
        if (r.role === "turn") {
            turns.push(r);
            continue;
        }
        runs.push(r.run);
        if (r.think) think += r.think.count || 0;
        for (const g of r.groups || []) {
            const k = kinds.get(g.kind) || { kind: g.kind, count: 0, failed: 0 };
            for (const c of g.calls || []) {
                k.count += 1;
                if (c.failed) k.failed += 1;
                calls.push({ ...c, kind: g.kind });
            }
            kinds.set(g.kind, k);
        }
    }
    calls.sort((a, b) => (a.pos - b.pos) || ((a.index || 0) - (b.index || 0)));
    const list = [...kinds.values()].filter((k) => k.count).sort((a, b) => b.count - a.count);
    const total = list.reduce((n, k) => n + k.count, 0);
    const failed = list.reduce((n, k) => n + k.failed, 0);
    return { kinds: list, total, failed, think, runs, turns, calls };
}

const ONE = {
    bash: "command", files: "file", web: "web", agents: "agent", skill: "skill", ask: "question",
    artifact: "artifact", time: "schedule", browser: "browser", mcp: "mcp", hook: "hook", other: "other",
};

// said is a group in words: its calls by kind, a run that only thought, the
// end of a turn with how long it took, the calls it made and what it left.
export function said(sum) {
    const parts = sum.kinds.map((k) => `${k.count} ${k.count === 1
        ? (ONE[k.kind] || "call")
        : (KIND_NAMES[k.kind] || KIND_NAMES.other)}`);
    if (sum.think && !sum.kinds.length) parts.push("thinking");
    for (const t of sum.turns) {
        parts.push([turnTook(t), t.calls > 0 && countCalls(t.calls), turnLeft(t)].filter(Boolean).join(" · "));
    }
    return parts.join(" · ");
}

const ms = (iso) => (iso ? Date.parse(iso) : NaN);

// hhmm is the time of day an entry began, in the words stampText uses.
function hhmm(iso) {
    const full = stampText(iso);
    return full ? full.split(" · ").pop() : "";
}

const base = (p) => String(p || "").split("/").filter(Boolean).pop() || String(p || "");

const FILE_TOOLS = new Set(["Read", "Edit", "Write", "MultiEdit", "NotebookEdit"]);
const WRITES = new Set(["Edit", "Write", "MultiEdit", "NotebookEdit"]);

// callLabel is a call in a few words: the command a shell ran, the tool of a
// server with what it was given, the name and the argument of anything else.
export function callLabel(c) {
    const arg = String(c.arg || "").split(" ⏎ ")[0].trim();
    if (FILE_TOOLS.has(c.name)) return base(arg);
    if (c.name.includes(": ")) return c.name.split(": ").pop() + (arg ? ` ${arg}` : "");
    if (c.name === "Bash") return arg || c.name;
    return arg ? `${c.name} ${arg}` : c.name;
}

// filesOf is the files a group touched, each once: written if any call of the
// group wrote it.
export function filesOf(calls) {
    const seen = new Map();
    for (const c of calls) {
        if (!FILE_TOOLS.has(c.name) || !c.arg) continue;
        const name = base(c.arg);
        const was = seen.get(c.arg);
        seen.set(c.arg, { name, path: c.arg, wrote: Boolean(was && was.wrote) || WRITES.has(c.name) });
    }
    return [...seen.values()];
}

// useGeometry measures where every row of the column stands, and again
// whenever the column changes its size: a block coloured, a letter opened, a
// font come in.
function useGeometry(box, count) {
    const [geo, setGeo] = useState(null);
    const measure = () => {
        const el = box.current;
        if (!el) return;
        // From the top of the column: the timeline beside it starts there.
        const zero = el.offsetTop;
        const tops = [];
        const bottoms = [];
        for (const kid of el.children) {
            tops.push(kid.offsetTop - zero);
            bottoms.push(kid.offsetTop - zero + kid.offsetHeight);
        }
        setGeo((was) => (was && same(was.tops, tops) && same(was.bottoms, bottoms) ? was : { tops, bottoms }));
    };
    useLayoutEffect(measure, [count]);
    useEffect(() => {
        const el = box.current;
        if (!el || typeof ResizeObserver === "undefined") return undefined;
        let frame = 0;
        const eye = new ResizeObserver(() => {
            cancelAnimationFrame(frame);
            frame = requestAnimationFrame(measure);
        });
        eye.observe(el);
        for (const kid of el.children) eye.observe(kid);
        return () => {
            eye.disconnect();
            cancelAnimationFrame(frame);
        };
    }, [count]);
    return geo;
}

function same(a, b) {
    if (!a || a.length !== b.length) return false;
    for (let i = 0; i < a.length; i++) if (a[i] !== b[i]) return false;
    return true;
}

// anchorY is the height a mark stands at: the top of the row after its work,
// or just under the last row when the work is the last thing so far.
export function anchorY(mark, geo) {
    if (!geo || !geo.tops.length) return 0;
    if (mark.at < geo.tops.length) return geo.tops[mark.at];
    return geo.bottoms[geo.bottoms.length - 1] + 6;
}

// railGroups lays the marks out on the strip: one that would stand closer to
// the mark above than a mark's height and the gap joins it.
export function railGroups(marks, geo) {
    const out = [];
    for (const m of marks) {
        const y = anchorY(m, geo);
        const last = out[out.length - 1];
        if (last && y < last.y + MARK_H + MARK_GAP) {
            last.rows = [...last.rows, ...m.rows];
            last.merged += 1;
            continue;
        }
        out.push({ y, rows: [...m.rows], merged: 1, at: m.at });
    }
    return out.map((g) => ({ ...g, sum: sumOf(g.rows) }));
}

// stack lays the entries of the desk column out top down: each at the height
// of its row, or under the entry above when that one is still in the way.
export function stack(ys, heights, gap = ENTRY_GAP) {
    let floor = -Infinity;
    return ys.map((y, n) => {
        const at = Math.max(y, floor);
        floor = at + (heights[n] || 0) + gap;
        return at;
    });
}

function toneOf(sum) {
    if (sum.kinds.length) return `k-${sum.kinds[0].kind}`;
    return sum.turns.length && !sum.think ? "k-turn" : "k-think";
}

function RailMark({ g, onOpen }) {
    const s = g.sum;
    const lead = s.kinds[0];
    const onlyTurn = !s.total && s.turns.length > 0;
    const icon = lead ? kindIcon(lead.kind) : onlyTurn ? Icon.hourglass() : Icon.thinking();
    const turnCalls = s.turns.reduce((n, t) => n + (t.calls || 0), 0);
    const num = s.total || (onlyTurn ? turnCalls : s.think);
    const label = said(s) || "work";
    return html`
        <button type="button" class=${`railmark ${toneOf(s)}${g.merged > 1 ? " merged" : ""}${s.failed ? " failed" : ""}`}
                style=${`top:${Math.round(g.y)}px`} onClick=${() => onOpen(g)}
                title=${label} aria-label=${`${label} — open the calls`}>
            <span class="rmicon">${icon}</span>
            ${num > 0 && html`<span class="rmnum">${num}</span>`}
        </button>
    `;
}

function deskGroups(marks, geo, prose) {
    return marks.map((m) => {
        const sum = sumOf(m.rows);
        const first = (sum.calls[0] && sum.calls[0].at) || (m.rows[0] && m.rows[0].at) || "";
        const next = prose[m.at];
        return {
            y: anchorY(m, geo), rows: m.rows, sum, merged: 1, at: m.at,
            first, from: ms(first), until: next ? ms(next.at) : NaN,
        };
    });
}

function DeskEntry({ g, y, onOpen, entryRef }) {
    const s = g.sum;
    // A file tool says itself by its file: those calls are the chips under
    // the lines, the rest are lines.
    const lines = s.calls.filter((c) => !FILE_TOOLS.has(c.name));
    const shown = lines.slice(0, 3);
    const rest = lines.length - shown.length;
    const files = filesOf(s.calls);
    const took = Number.isFinite(g.until) && Number.isFinite(g.from) && g.until >= g.from
        ? stopwatch((g.until - g.from) / 1000)
        : "";
    const words = said(s) || "work";
    return html`
        <button type="button" class=${`tlentry ${toneOf(s)}${s.failed ? " failed" : ""}`}
                ref=${entryRef} style=${`top:${Math.round(y)}px`} onClick=${() => onOpen(g)}
                aria-label=${`${words} — open the calls`}>
            <span class="tlhead">
                <span class="tltime">${hhmm(g.first)}</span>
                <span class="tlsum">${words}</span>
                ${took && html`<span class="tldur">${took}</span>`}
            </span>
            ${shown.map((c) => html`
                <span class=${`tlcall k-${c.kind}${c.failed ? " failed" : ""}`} key=${`${c.pos}-${c.index}`}>
                    <span class="tlicon">${kindIcon(c.kind)}</span>
                    <span class="tlarg">${callLabel(c)}</span>
                </span>
            `)}
            ${rest > 0 && html`<span class="tlmore">+${rest} more ${callWord(rest)}</span>`}
            ${files.length > 0 && html`
                <span class="tlfiles">
                    ${files.slice(0, 4).map((f) => html`
                        <span class=${`tlfile${f.wrote ? " wrote" : ""}`} key=${f.path} title=${f.path}>${f.name}</span>
                    `)}
                    ${files.length > 4 && html`<span class="tlfile">+${files.length - 4}</span>`}
                </span>
            `}
        </button>
    `;
}

function DeskColumn({ groups, onOpen }) {
    const refs = useRef([]);
    const [heights, setHeights] = useState([]);
    useLayoutEffect(() => {
        const hs = groups.map((_, n) => (refs.current[n] ? refs.current[n].offsetHeight : 0));
        setHeights((was) => (same(was, hs) ? was : hs));
    });
    const ys = stack(groups.map((g) => g.y), heights);
    return html`
        <div class="tline" aria-label="timeline of the work">
            ${groups.map((g, n) => html`
                <${DeskEntry} key=${g.at} g=${g} y=${ys[n]} onOpen=${onOpen}
                              entryRef=${(el) => { refs.current[n] = el; }} />
            `)}
        </div>
    `;
}

// FeedGrid draws the column and its timeline in one scroll box. row draws a
// row of the column; tail is what follows the rows — the messages on their
// way, as { key, role, node } — set as rows are, so a message keeps its place
// when the transcript echoes it; onOpen gets a group of marks when one is
// tapped.
export function FeedGrid({ rows, wide, row, tail, onOpen }) {
    const { prose, marks } = useMemo(() => book(rows), [rows]);
    const col = useRef(null);
    const geo = useGeometry(col, prose.length);
    const groups = useMemo(() => (geo ? (wide ? deskGroups(marks, geo, prose) : railGroups(marks, geo)) : []),
        [marks, geo, wide, prose]);
    return html`
        <div class=${`feedgrid${wide ? " desk" : ""}`}>
            <div class="feedcol" ref=${col}>
                ${prose.map((item, n) => html`
                    <div class=${`feedrow r-${item.role}`} key=${`${item.pos}-${item.role}-${n}`}>${row(item, n)}</div>
                `)}
                ${(tail || []).map((t) => html`<div class=${`feedrow r-${t.role}`} key=${t.key}>${t.node}</div>`)}
            </div>
            ${wide
                ? html`<${DeskColumn} groups=${groups} onOpen=${onOpen} />`
                : html`
                    <div class="rail" aria-label="timeline of the work">
                        ${groups.map((g) => html`<${RailMark} key=${g.at} g=${g} onOpen=${onOpen} />`)}
                    </div>
                `}
        </div>
    `;
}

// callsOf is what a tapped group opens: the calls of its runs, or, for the
// end of a turn alone, the calls of that turn.
export function callsOf(g, feed, runCalls, turnCalls) {
    const turn = g.rows.find((r) => r.role === "turn");
    const list = g.sum.runs.flatMap((run) => runCalls(feed, run));
    if (turn && !list.length) return { list: turnCalls(feed, turn.pos), turn };
    return { list: list.sort((a, b) => (a.pos - b.pos) || ((a.index || 0) - (b.index || 0))) };
}
