// The lines of code themselves: the one piece both screens are built out of.
//
// Two rules shape it. The gutter with the numbers stands outside the sideways
// scroll — it is the strip a finger swipes the page by and the handle a line is
// picked up with, and a gutter that scrolls away with the code leaves neither.
// And a line that runs off the edge scrolls inside itself rather than taking
// the page along: what moves sideways in this panel is the contents of a block,
// never the screen.

import { html } from "../../html.js";
import { place } from "./notes.js";

// The classes the service numbers its spans with. The order is the one the
// service sends, and the two lists are one list: a class added on one side and
// not the other paints code in the colour of nothing.
const CLASSES = ["", "cdcm", "cdstr", "cdkw", "cdnum", "cdty", "cdfn"];

// Painted turns a line and its spans into the nodes of that line. With no
// spans — a file of a kind the panel cannot read, or one over the ceiling —
// the text goes out as it came.
export function Painted({ text, spans }) {
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
export function Hunk({ hunk, path, notes, picked, onPick, composer }) {
    const tag = hunk.layer === "worktree"
        ? html`<span class="cdtag wt">not in a commit yet</span>`
        : html`<span class="cdtag done">in a commit</span>`;
    // Where the notes of this file stand now: a branch moves while it is being
    // read, so the lines in front of us decide, not the numbers written down.
    const { byIndex } = place(notes, path, hunk.lines);
    return html`
        <div class="cdhunk">
            <div class="cdhead">
                <span class="cdpath">${hunk.header || path}</span>
                ${tag}
            </div>
            <div class="cdlines">
                ${hunk.lines.map((l, i) => [
                    html`
                        <${CodeLine}
                            key=${i}
                            kind=${l.kind}
                            no=${l.new}
                            old=${l.old}
                            text=${l.text}
                            spans=${l.spans}
                            noted=${byIndex.has(i)}
                            picked=${standsOn(picked, path, l)}
                            onPick=${onPick ? (line, text) => onPick(path, line, text, l.kind) : null}
                        />
                    `,
                    standsOn(picked, path, l) ? composer : null,
                ])}
            </div>
        </div>
    `;
}

// standsOn says whether a mark — a note, or the line being written on —
// belongs to this line. The number alone does not answer it: one deletion and
// the addition that replaced it wear the same number in a diff, and a mark read
// by number would light both. The text of the line settles it, which is also
// what the mark carries to find its way back after the branch moves.
function standsOn(mark, path, line) {
    if (!mark || mark.path !== path) return false;
    const no = line.kind === "del" ? line.old : line.new;
    return no != null && mark.line === no && mark.quote === line.text;
}

// FileLines is a window of a whole file, not a diff: the same rows without the
// signs and the two layers.
export function FileLines({ path, first, lines, spans, notes, picked, onPick, composer, head }) {
    const rows = lines.map((text, i) => ({ kind: "ctx", new: first + i, text }));
    const { byIndex } = place(notes, path, rows);
    return html`
        <div class="cdhunk">
            <div class="cdhead"><span class="cdpath">${head || path}</span></div>
            <div class="cdlines">
                ${lines.map((text, i) => {
                    const line = { kind: "ctx", new: first + i, text };
                    return [
                        html`
                            <${CodeLine}
                                key=${i}
                                kind="ctx"
                                no=${first + i}
                                text=${text}
                                spans=${spans && spans[i]}
                                noted=${byIndex.has(i)}
                                picked=${standsOn(picked, path, line)}
                                onPick=${onPick ? (no, t) => onPick(path, no, t, "ctx") : null}
                            />
                        `,
                        standsOn(picked, path, line) ? composer : null,
                    ];
                })}
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
export function SplitHunk({ hunk, path, notes, picked, onPick, composer }) {
    const tag = hunk.layer === "worktree"
        ? html`<span class="cdtag wt">not in a commit yet</span>`
        : html`<span class="cdtag done">in a commit</span>`;
    const rows = splitRows(hunk.lines);
    // The notes are placed against the run as a whole and handed to the halves
    // as the lines they landed on: a half sees one line and cannot tell where a
    // note that moved should have gone.
    const { byIndex } = place(notes, path, hunk.lines);
    const noted = new Set([...byIndex.keys()].map((i) => hunk.lines[i]));
    return html`
        <div class="cdhunk">
            <div class="cdhead">
                <span class="cdpath">${hunk.header || path}</span>
                ${tag}
            </div>
            <div class="cdlines cdsplit">
                ${rows.map((row, i) => [
                    html`
                        <div class="cdsprow" key=${i}>
                            <${Half} line=${row.left} side="old" path=${path}
                                     noted=${noted} picked=${picked} onPick=${onPick} />
                            <${Half} line=${row.right} side="new" path=${path}
                                     noted=${noted} picked=${picked} onPick=${onPick} />
                        </div>
                    `,
                    // The box stands under the whole row rather than in the
                    // column the line is in: half a screen wide, it is a
                    // sentence a word at a time.
                    onRow(picked, path, row) ? composer : null,
                ])}
            </div>
        </div>
    `;
}

// Half is one side of a row: a line, or the empty place left where the other
// side has one more. The empty place keeps the height of a line — the two
// columns are read across, and a gap that collapses takes the rows out of step.
function Half({ line, side, path, noted, picked, onPick }) {
    if (!line) return html`<div class="cdln void"><span class="cdgut"></span><span class="cdsrc"></span></div>`;
    if (side === "old" && line.kind === "add") {
        return html`<div class="cdln void"><span class="cdgut"></span><span class="cdsrc"></span></div>`;
    }
    if (side === "new" && line.kind === "del") {
        return html`<div class="cdln void"><span class="cdgut"></span><span class="cdsrc"></span></div>`;
    }
    const no = side === "old" ? (line.old == null ? line.new : line.old) : (line.new == null ? line.old : line.new);
    // What the column shows and what a note is pinned to are not the same
    // number: a line carried through unchanged wears its old number on the left
    // and its new one on the right, and a note pinned to the old one points at
    // a place in a file nobody has any more. The note goes to the line as the
    // file has it now, whichever column it was written from.
    const anchor = line.kind === "del" ? (line.old == null ? no : line.old) : (line.new == null ? no : line.new);
    return html`
        <${CodeLine}
            kind=${line.kind}
            no=${no}
            old=${no}
            text=${line.text}
            spans=${line.spans}
            noted=${Boolean(noted && noted.has(line))}
            picked=${standsOn(picked, path, line)}
            onPick=${onPick ? (n, text) => onPick(path, anchor, text, line.kind) : null}
        />
    `;
}

// onRow says whether the line being written on is one of the two in this row.
function onRow(picked, path, row) {
    return standsOn(picked, path, row.left || {}) || standsOn(picked, path, row.right || {});
}
