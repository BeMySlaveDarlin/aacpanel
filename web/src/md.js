// Renders markdown from model answers into preact nodes.
import { html } from "./html.js";
import { Icon } from "./ui/icons.js";

const INLINE = /(`[^`]+`)|(\*\*[^*]+\*\*)|(\*[^*\n]+\*)|(\[[^\]]+\]\([^)\s]+\))/g;

const SAFE_LINK = /^https?:\/\//i;

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
                : html`<code>${body}</code>`);
        } else if (token.startsWith("**")) {
            out.push(html`<b>${token.slice(2, -2)}</b>`);
        } else if (token.startsWith("*")) {
            out.push(html`<i>${token.slice(1, -1)}</i>`);
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

function copyTool(md, label) {
    return html`
        <div class="mdtools">
            <button type="button" class="mdcopy" data-md=${md}
                title=${label} aria-label=${label}>${Icon.copy()}</button>
        </div>
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
            out.push(html`
                <div class="mdcode">
                    <div class="mdbar">
                        <span class="mdlang">${lang}</span>
                        <button type="button" class="mdcopy"
                            title="Copy the code" aria-label="Copy the code">${Icon.copy()}</button>
                    </div>
                    <pre><code>${body.join("\n")}</code></pre>
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
            out.push(html`
                <div class="mdblock">
                    ${copyTool(raw.join("\n"), "Copy the table")}
                    <div class="mdtable">
                        <table>
                            <thead><tr>${head.map((c) => html`<th>${inline(c)}</th>`)}</tr></thead>
                            <tbody>
                                ${rows.map((r, n) => html`<tr key=${n}>${r.map((c) => html`<td>${inline(c)}</td>`)}</tr>`)}
                            </tbody>
                        </table>
                    </div>
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
            const raw = [];
            while (i < lines.length && mark.test(lines[i])) {
                raw.push(lines[i]);
                items.push(lines[i].replace(mark, ""));
                i += 1;
                let next = i;
                while (next < lines.length && !lines[next].trim()) next += 1;
                if (next > i && next < lines.length && mark.test(lines[next])) i = next;
            }
            const start = ordered ? Number(numbered.exec(line)[1]) : 0;
            out.push(html`
                <div class="mdblock">
                    ${copyTool(raw.join("\n"), "Copy the list")}
                    ${ordered
                        ? html`<ol class="mdlist" start=${start}>${items.map((it, n) => html`<li key=${n}>${inline(it)}</li>`)}</ol>`
                        : html`<ul class="mdlist">${items.map((it, n) => html`<li key=${n}>${inline(it)}</li>`)}</ul>`}
                </div>
            `);
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

function hard(lines) {
    const out = [];
    for (let n = 0; n < lines.length; n++) {
        if (n) out.push(html`<br />`);
        out.push(inline(lines[n]));
    }
    return out;
}
