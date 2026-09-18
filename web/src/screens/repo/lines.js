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
