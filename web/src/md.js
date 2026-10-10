// Renders markdown from model answers into preact nodes.
//
// A block of code or a table carries its copy button over its corner rather
// than on a bar of its own; a list is prose and carries none. Code in a line
// is copied by a tap, and a block of code in a language the highlighter knows
// is coloured when it comes near the screen.
import { useEffect, useRef, useState } from "preact/hooks";

import { html } from "./html.js";
import { Icon } from "./ui/icons.js";
import { highlight, kept, langOf } from "./hl.js";

const INLINE = /(`[^`]+`)|(\*\*[^*]+\*\*)|(\*[^*\n]+\*)|(\[[^\]]+\]\([^)\s]+\))/g;

// SAFE_LINK is an address the panel makes a link of: the web's own schemes.
// The address comes from a model or a server, and any other scheme, pressed,
// would run in the panel's own page.
export const SAFE_LINK = /^https?:\/\//i;

const BARE_URL = /https?:\/\/[^\s<>"'`]+[^\s<>"'`.,;:!?)\]]/g;

const PATHLIKE = /^(?:~\/|\.{0,2}\/)?(?:[\w.@+-]+\/)*[\w.@+-]+\.[A-Za-z][\w]{0,9}(?::\d+)?$/;

// isPath reports whether the token looks like a path to a project file.
export function isPath(token) {
    if (!PATHLIKE.test(token)) return false;
    if (!token.includes("/")) return CODE_EXT.test(token);
    return true;
}

const CODE_EXT = /\.(js|css|go|py|sh|md|json|ya?ml|html|sql|txt|toml|ini|conf|mod|sum|env|jsonl|tsx?|jsx?)(:\d+)?$/i;

export function inline(text) {
    const out = [];
    let last = 0;
    for (const m of String(text).matchAll(INLINE)) {
        if (m.index > last) out.push(text.slice(last, m.index));
        const token = m[0];
        if (token.startsWith("`")) {
            const body = token.slice(1, -1);
            out.push(isPath(body)
                ? html`<code class="path" data-path=${body}>${body}</code>`
                : html`<code class="copyable">${body}</code>`);
        } else if (token.startsWith("**")) {
            // What is set in bold or italics is text like any other: an address
            // or a link in it is followed, not shown as dead words.
            out.push(html`<b>${inline(token.slice(2, -2))}</b>`);
        } else if (token.startsWith("*")) {
            out.push(html`<i>${inline(token.slice(1, -1))}</i>`);
        } else {
            const cut = token.indexOf("](");
            const label = token.slice(1, cut);
            const href = token.slice(cut + 2, -1);
            out.push(SAFE_LINK.test(href)
                ? html`<a href=${href} target="_blank" rel="noopener noreferrer">${label}</a>`
                : `${label} (${href})`);
        }
        last = m.index + token.length;
    }
    if (last < text.length) out.push(text.slice(last));
    return out.flatMap((node) => (typeof node === "string" ? autolink(node) : node));
}

function autolink(text) {
    const out = [];
    let last = 0;
    for (const m of text.matchAll(BARE_URL)) {
        if (m.index > last) out.push(text.slice(last, m.index));
        out.push(html`<a href=${m[0]} target="_blank" rel="noopener noreferrer">${m[0]}</a>`);
        last = m.index + m[0].length;
    }
    if (!out.length) return text;
    if (last < text.length) out.push(text.slice(last));
    return out;
}

const TABLE_SPLIT = /^\|[\s:|-]+\|$/;

function cells(line) {
    return line.replace(/^\||\|$/g, "").split("|").map((c) => c.trim());
}

// A cell is a number when, markup aside, it starts with one and holds only
// digits, what numbers are written with and short units in any script —
// "3261", "+37 / -3308", "28m 1s", "12 KB". A word that stands apart between
// two numbers joins them into a phrase, "3 of 5", and is no unit. A column is
// numeric when every cell that says something is.
const NUMBER_LEAD = /^[~≈<>≤≥±+\-−–]?\s*\d/;
const NUMBER_CHARS = /^[\p{L}\d\s.,:_/+\-−–→·×%()~≈<>≤≥±]*$/u;
const EMPTY = /^(?:|—|–|-|·|n\/a)$/i;

function isNumber(cell) {
    if (!NUMBER_LEAD.test(cell) || !NUMBER_CHARS.test(cell)) return false;
    for (const m of cell.matchAll(/\p{L}+/gu)) {
        if (m[0].length > 4) return false;
        const glued = /\d/.test(cell[m.index - 1] || "");
        const between = /^\s*\d/.test(cell.slice(m.index + m[0].length));
        if (!glued && between) return false;
    }
    return true;
}

function numeric(column) {
    let seen = 0;
    for (const raw of column) {
        const cell = String(raw || "").replace(/\*\*|`/g, "").trim();
        if (EMPTY.test(cell)) continue;
        if (!isNumber(cell)) return false;
        seen += 1;
    }
    return seen > 0;
}

// Code is the text of a block, coloured once it has come near the screen.
// Until then, and in a language the highlighter does not know, it is the
// plain text; what was coloured once is taken from what the highlighter kept.
function Code({ lang, children }) {
    const text = String(children == null ? "" : children);
    const box = useRef(null);
    const name = langOf(lang, text);
    const [pieces, setPieces] = useState(() => (name ? kept(name, text) : null));
    useEffect(() => {
        if (!name) {
            setPieces(null);
            return undefined;
        }
        const was = kept(name, text);
        if (was) {
            setPieces(was);
            return undefined;
        }
        setPieces(null);
        const el = box.current;
        if (!el || typeof IntersectionObserver === "undefined") {
            setPieces(highlight(name, text));
            return undefined;
        }
        const eye = new IntersectionObserver((seen) => {
            if (!seen.some((e) => e.isIntersecting)) return;
            eye.disconnect();
            setPieces(highlight(name, text));
        }, { rootMargin: "240px 0px" });
        eye.observe(el);
        return () => eye.disconnect();
    }, [name, text]);
    if (!pieces) return html`<code ref=${box}>${text}</code>`;
    return html`
        <code ref=${box} class=${`hl l-${name}`}>${pieces.map((p) => (typeof p === "string"
            ? p
            : html`<span class=${`hl-${p[0]}`}>${p[1]}</span>`))}</code>
    `;
}

export function render(text, opts) {
    const breaks = Boolean(opts && opts.breaks);
    const lines = String(text || "").split("\n");
    const out = [];
    let i = 0;

    while (i < lines.length) {
        const line = lines[i];

        if (line.startsWith("```")) {
            const lang = line.slice(3).trim();
            const body = [];
            i += 1;
            while (i < lines.length && !lines[i].startsWith("```")) body.push(lines[i++]);
            i += 1;
            const code = body.join("\n");
            out.push(html`
                <div class=${`mdcode${body.length === 1 ? " oneline" : ""}`}>
                    <pre><${Code} lang=${lang}>${code}<//></pre>
                    <button type="button" class="mdcopy"
                        title="Copy the code" aria-label="Copy the code">${Icon.copy()}</button>
                </div>
            `);
            continue;
        }

        if (line.startsWith("|") && TABLE_SPLIT.test(lines[i + 1] || "")) {
            const head = cells(line);
            const raw = [line, lines[i + 1]];
            i += 2;
            const rows = [];
            while (i < lines.length && lines[i].startsWith("|")) {
                raw.push(lines[i]);
                rows.push(cells(lines[i++]));
            }
            const right = cells(raw[1]).map((c) => /^:?-+:$/.test(c) && !/^:-+:$/.test(c));
            const cls = head.map((_, col) => (right[col] || numeric(rows.map((r) => r[col])) ? "mdnum" : undefined));
            out.push(html`
                <div class=${`mdtab${head.length <= 3 ? " mdfit" : ""}`}>
                    <div class="mdtable">
                        <table>
                            <thead><tr>${head.map((c, col) => html`<th class=${cls[col]}>${inline(c)}</th>`)}</tr></thead>
                            <tbody>
                                ${rows.map((r, n) => html`<tr key=${n}>${r.map((c, col) => html`<td class=${cls[col]}>${inline(c)}</td>`)}</tr>`)}
                            </tbody>
                        </table>
                    </div>
                    <button type="button" class="mdcopy" data-md=${raw.join("\n")}
                        title="Copy the table" aria-label="Copy the table">${Icon.copy()}</button>
                </div>
            `);
            continue;
        }

        const heading = /^(#{1,4})\s+(.*)$/.exec(line);
        if (heading) {
            const level = Math.min(heading[1].length, 3);
            out.push(html`<div class=${`mdh mdh${level}`}>${inline(heading[2])}</div>`);
            i += 1;
            continue;
        }

        if (/^\s*(---|\*\*\*|___)\s*$/.test(line)) {
            out.push(html`<div class="mdrule"></div>`);
            i += 1;
            continue;
        }

        if (/^\s*>\s?/.test(line)) {
            const quote = [];
            while (i < lines.length && /^\s*>\s?/.test(lines[i])) {
                quote.push(lines[i].replace(/^\s*>\s?/, ""));
                i += 1;
            }
            out.push(html`<blockquote class="mdquote">${inline(quote.join(" "))}</blockquote>`);
            continue;
        }

        const bullet = /^\s*([-*+])\s+/;
        const numbered = /^\s*(\d+)[.)]\s+/;
        if (bullet.test(line) || numbered.test(line)) {
            const ordered = numbered.test(line);
            const mark = ordered ? numbered : bullet;
            const items = [];
            while (i < lines.length && mark.test(lines[i])) {
                items.push(lines[i].replace(mark, ""));
                i += 1;
                let next = i;
                while (next < lines.length && !lines[next].trim()) next += 1;
                if (next > i && next < lines.length && mark.test(lines[next])) i = next;
            }
            const start = ordered ? Number(numbered.exec(line)[1]) : 0;
            out.push(ordered
                ? html`<ol class="mdlist" start=${start}>${items.map((it, n) => html`<li key=${n}>${inline(it)}</li>`)}</ol>`
                : html`<ul class="mdlist">${items.map((it, n) => html`<li key=${n}>${inline(it)}</li>`)}</ul>`);
            continue;
        }

        if (!line.trim()) {
            i += 1;
            continue;
        }

        const para = [line];
        i += 1;
        while (i < lines.length && lines[i].trim()
               && !lines[i].startsWith("```") && !lines[i].startsWith("|")
               && !/^(#{1,4})\s/.test(lines[i]) && !/^\s*>/.test(lines[i])
               && !bullet.test(lines[i]) && !numbered.test(lines[i])) {
            para.push(lines[i]);
            i += 1;
        }
        out.push(html`<p>${breaks ? hard(para) : inline(para.join(" "))}</p>`);
    }

    return out;
}

// dedent takes off the indent a letter came with as a whole: a report that
// arrives two spaces in has tables and fences that do not start a line.
export function dedent(text) {
    const raw = String(text || "");
    const lines = raw.split("\n");
    const pads = lines.filter((l) => l.trim()).map((l) => /^[ \t]*/.exec(l)[0].length);
    const cut = pads.length ? Math.min(...pads) : 0;
    return cut ? lines.map((l) => l.slice(cut)).join("\n") : raw;
}

// leadOf is the first thing a letter says: its first paragraph after the
// headings, which is what a reader decides by whether to open the rest.
export function leadOf(text) {
    const lines = String(text || "").split("\n");
    let i = 0;
    while (i < lines.length && (!lines[i].trim() || /^#{1,4}\s/.test(lines[i]))) i += 1;
    const para = [];
    while (i < lines.length && lines[i].trim() && !/^(```|\||#{1,4}\s|\s*[-*+]\s|\s*\d+[.)]\s|>)/.test(lines[i])) {
        para.push(lines[i]);
        i += 1;
    }
    return para.join("\n") || lines.find((l) => l.trim()) || "";
}

function hard(lines) {
    const out = [];
    for (let n = 0; n < lines.length; n++) {
        if (n) out.push(html`<br />`);
        out.push(inline(lines[n]));
    }
    return out;
}
