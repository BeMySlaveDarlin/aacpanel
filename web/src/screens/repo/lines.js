// The lines of code themselves: the one piece both screens are built out of.
//
// Two rules shape it. The gutter with the numbers stands outside the sideways
// scroll — it is the strip a finger swipes the page by and the handle a line is
// picked up with, and a gutter that scrolls away with the code leaves neither.
// And a line that runs off the edge scrolls inside itself rather than taking
// the page along: what moves sideways in this panel is the contents of a block,
// never the screen.

import { html } from "../../html.js";

// The classes the service numbers its spans with. The order is the one the
// service sends, and the two lists are one list: a class added on one side and
// not the other paints code in the colour of nothing.
const CLASSES = ["", "cdcm", "cdstr", "cdkw", "cdnum", "cdty", "cdfn"];

// Painted turns a line and its spans into the nodes of that line. With no
// spans — a file of a kind the panel cannot read, or one over the ceiling —
// the text goes out as it came.
function Painted({ text, spans }) {
    if (!spans || !spans.length) return text;
    const out = [];
    let at = 0;
    for (let i = 0; i < spans.length; i++) {
        const [cls, len] = spans[i];
        const part = text.slice(at, at + len);
        at += len;
        if (!part) continue;
        out.push(cls ? html`<span class=${CLASSES[cls] || ""}>${part}</span>` : part);
    }
    if (at < text.length) out.push(text.slice(at));
    return out;
}

// CodeLine is one line: the gutter, the sign and the code.
export function CodeLine({ kind, no, old, text, spans, noted, picked, onPick }) {
    const sign = kind === "add" ? "+" : kind === "del" ? "−" : "";
    const number = kind === "del" ? old : no;
    return html`
        <div class=${`cdln${kind && kind !== "ctx" ? ` ${kind}` : ""}${noted ? " noted" : ""}${picked ? " picked" : ""}`}>
            <button
                class="cdgut"
                type="button"
                onClick=${onPick ? () => onPick(number, text) : undefined}
                aria-label=${`line ${number == null ? "" : number}`}
            >
                <span class="cdno">${number == null ? "" : number}</span>
                <span class="cdsign">${sign}</span>
            </button>
            <span class="cdsrc"><${Painted} text=${text} spans=${spans} /></span>
        </div>
    `;
}

// Hunk is one run of changed lines under its heading. The heading says which
// layer the run belongs to, because "already in a commit" and "not yet" are
// different things to answer for.
export function Hunk({ hunk, path, notes, picked, onPick }) {
    const tag = hunk.layer === "worktree"
        ? html`<span class="cdtag wt">not in a commit yet</span>`
        : html`<span class="cdtag done">in a commit</span>`;
    return html`
        <div class="cdhunk">
            <div class="cdhead">
                <span class="cdpath">${hunk.header || path}</span>
                ${tag}
            </div>
            <div class="cdlines">
                ${hunk.lines.map((l, i) => html`
                    <${CodeLine}
                        key=${i}
                        kind=${l.kind}
                        no=${l.new}
                        old=${l.old}
                        text=${l.text}
                        spans=${l.spans}
                        noted=${noteOn(notes, path, l)}
                        picked=${picked && picked.path === path && picked.line === (l.kind === "del" ? l.old : l.new)}
                        onPick=${onPick ? (line, text) => onPick(path, line, text, l.kind) : null}
                    />
                `)}
            </div>
        </div>
    `;
}

function noteOn(notes, path, line) {
    if (!notes || !notes.length || line.kind === "del") return false;
    return notes.some((n) => n.path === path && n.line === line.new);
}

// FileLines is a window of a whole file, not a diff: the same rows without the
// signs and the two layers.
export function FileLines({ path, first, lines, spans, notes, picked, onPick, head }) {
    return html`
        <div class="cdhunk">
            <div class="cdhead"><span class="cdpath">${head || path}</span></div>
            <div class="cdlines">
                ${lines.map((text, i) => html`
                    <${CodeLine}
                        key=${i}
                        kind="ctx"
                        no=${first + i}
                        text=${text}
                        spans=${spans && spans[i]}
                        noted=${notes && notes.some((n) => n.path === path && n.line === first + i)}
                        picked=${picked && picked.path === path && picked.line === first + i}
                        onPick=${onPick ? (line, t) => onPick(path, line, t, "ctx") : null}
                    />
                `)}
            </div>
        </div>
    `;
}

// splitRows lays a run of changed lines out as rows of two columns. The columns
// are matched up by blocks rather than by line numbers: one deletion shifts
// every line under it, so a column laid out by numbers drifts one line further
// from its neighbour with every change until the two sides no longer describe
// the same place. A block of deletions is paired with the block of additions
// that replaced it, and whichever block is shorter is filled out with nothing.
export function splitRows(lines) {
    const rows = [];
    let dels = [];
    let adds = [];

    const flush = () => {
        const n = Math.max(dels.length, adds.length);
        for (let i = 0; i < n; i++) rows.push({ left: dels[i] || null, right: adds[i] || null });
        dels = [];
        adds = [];
    };

    for (const line of lines) {
        if (line.kind === "del") {
            dels.push(line);
            continue;
        }
        if (line.kind === "add") {
            adds.push(line);
            continue;
        }
        flush();
        rows.push({ left: line, right: line });
    }
    flush();
    return rows;
}

// SplitHunk is one run of changed lines read as "before" and "after". The same
// rows as the unified view, in two columns — what it buys is seeing the line
// that was replaced next to the line that replaced it.
export function SplitHunk({ hunk, path, notes, picked, onPick }) {
    const tag = hunk.layer === "worktree"
        ? html`<span class="cdtag wt">not in a commit yet</span>`
        : html`<span class="cdtag done">in a commit</span>`;
    const rows = splitRows(hunk.lines);
    return html`
        <div class="cdhunk">
            <div class="cdhead">
                <span class="cdpath">${hunk.header || path}</span>
                ${tag}
            </div>
            <div class="cdlines cdsplit">
                ${rows.map((row, i) => html`
                    <div class="cdsprow" key=${i}>
                        <${Half} line=${row.left} side="old" path=${path}
                                 notes=${notes} picked=${picked} onPick=${onPick} />
                        <${Half} line=${row.right} side="new" path=${path}
                                 notes=${notes} picked=${picked} onPick=${onPick} />
                    </div>
                `)}
            </div>
        </div>
    `;
}

// Half is one side of a row: a line, or the empty place left where the other
// side has one more. The empty place keeps the height of a line — the two
// columns are read across, and a gap that collapses takes the rows out of step.
function Half({ line, side, path, notes, picked, onPick }) {
    if (!line) return html`<div class="cdln void"><span class="cdgut"></span><span class="cdsrc"></span></div>`;
    if (side === "old" && line.kind === "add") {
        return html`<div class="cdln void"><span class="cdgut"></span><span class="cdsrc"></span></div>`;
    }
    if (side === "new" && line.kind === "del") {
        return html`<div class="cdln void"><span class="cdgut"></span><span class="cdsrc"></span></div>`;
    }
    const no = side === "old" ? (line.old == null ? line.new : line.old) : (line.new == null ? line.old : line.new);
    return html`
        <${CodeLine}
            kind=${line.kind}
            no=${no}
            old=${no}
            text=${line.text}
            spans=${line.spans}
            noted=${noteOn(notes, path, line)}
            picked=${picked && picked.path === path && picked.line === no}
            onPick=${onPick ? (n, text) => onPick(path, n, text, line.kind) : null}
        />
    `;
}
