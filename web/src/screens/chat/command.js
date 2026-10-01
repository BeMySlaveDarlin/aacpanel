// A local command the person ran and what it answered: one card in the feed,
// and the breakdown of an answer the feed knows in a sheet.
//
// What a command prints is written for the screen it was typed at — a grid of
// coloured glyphs in a terminal, a long markdown table elsewhere — and neither
// reads in a feed. For /context and /usage the host hands over the numbers
// instead, and they are drawn here: one line in the conversation, the whole
// picture on a tap. Any other answer is the text the command printed.

import { useLayoutEffect, useRef, useState } from "preact/hooks";

import { html } from "../../html.js";
import { Icon } from "../../ui/icons.js";
import { useToast } from "../../ui/toasts.js";
import { plural, tokens } from "../../format.js";
import { copyText } from "./copy.js";
import { modelTitle } from "./head.js";
import { stampText } from "./labels.js";
import { USAGE_TITLE, UsageBreakdown, UsageCard } from "./usagecard.js";

// The colour of a category is its meaning, the same in the card, the sheet and
// the legend. A category the list does not know is drawn in the neutral one.
const CATEGORY = {
    "System prompt": "system",
    "System tools": "tools",
    "MCP tools": "mcp",
    "Custom agents": "agents",
    "Memory files": "memory",
    "Skills": "skills",
    "Messages": "messages",
};

function tone(category) {
    if (category.kind === "buffer") return "buffer";
    if (category.kind === "free") return "free";
    return CATEGORY[category.name] || "other";
}

// The names the sheet heads its parts with.
export const COMMAND_TITLES = {
    context: "Context window",
    usage: USAGE_TITLE,
};

function share(n, max) {
    if (!max) return "0%";
    const p = (n / max) * 100;
    if (p > 0 && p < 0.1) return "<0.1%";
    return `${p < 10 ? p.toFixed(1) : Math.round(p)}%`;
}

function count(row) {
    return row.under ? `<${row.under}` : tokens(row.tokens);
}

// Bar lays the window out the way it fills: what is used from the left, the
// buffer held back for compaction at the far right, and the free space between.
// What is loaded on demand takes no room until it is loaded, so it is not on it.
function Bar({ data, big = false }) {
    const max = data.max || 1;
    const cats = data.categories || [];
    const used = cats.filter((c) => c.kind === "used" && c.tokens > 0);
    const buffer = cats.filter((c) => c.kind === "buffer" && c.tokens > 0);
    const width = (c) => `width: ${Math.max((c.tokens / max) * 100, 0.4)}%`;
    return html`
        <span class=${`cmdbar${big ? " big" : ""}`} aria-hidden="true">
            ${used.map((c) => html`<i key=${c.name} class=${`t-${tone(c)}`} style=${width(c)}></i>`)}
            <b></b>
            ${buffer.map((c) => html`<i key=${c.name} class="t-buffer" style=${width(c)}></i>`)}
        </span>
    `;
}

function figure(data) {
    return `${tokens(data.used)} / ${tokens(data.max)} (${Math.round(data.percent || 0)}%)`;
}

// CommandCard is a local command and its answer as one row of the feed. The
// command is a line in the code face, small and quiet: the person typed it,
// but it is not a message of the conversation — the model never reads it as
// one, and claude answers it, not the model. So the card is not a bubble of
// the person, nor the plate in the middle of the column, which is the
// session's own: it is the width of the column, as the card of a command run
// with "!" is. The answer hangs under the command on the mark a terminal
// hangs it on, and until it comes the same place says where the command
// stands. An answer whose command the feed never saw is the answer alone.
export function CommandCard({ item, onOpen }) {
    const typed = Boolean(item.text);
    // An answer the feed knows is a card of its own, and it takes the width
    // of the card under the command rather than the room beside the mark.
    const hung = typed && !COMMAND_TITLES[item.name];
    return html`
        <div class=${`mcmd${item.err ? " failed" : ""}`}>
            ${typed && html`
                <div class="mcmdhead">
                    <span class="mcmdmark" aria-hidden="true">⌘</span>
                    <code class="mcmdtext">${item.text}</code>
                    ${item.at && html`<span class="mcmdat">${stampText(item.at)}</span>`}
                </div>
            `}
            <div class="mcmdbody">
                ${hung && html`<span class="mcmdhook" aria-hidden="true">⎿</span>`}
                <div class="mcmdsaid"><${Answer} item=${item} onOpen=${onOpen} /></div>
            </div>
        </div>
    `;
}

// Answer is what stands under the command: the card of an answer the feed
// knows, the text the command printed, or where the command stands while
// there is no answer yet.
function Answer({ item, onOpen }) {
    if (item.name === "usage") return html`<${UsageCard} item=${item} onOpen=${onOpen} />`;
    if (item.name === "context") return html`<${ContextCard} item=${item} onOpen=${onOpen} />`;
    if (!item.done) return html`<${Waiting} state=${item.state} />`;
    if (!item.out && !item.err) return html`<span class="mcmdstate">done, nothing printed</span>`;
    return html`<${Printed} text=${item.err || item.out} err=${Boolean(item.err)} cut=${item.cut} />`;
}

// Waiting says where a command without an answer stands: in the queue, where
// a command typed into a busy session waits for the turn to end; taken back
// from it; or running.
function Waiting({ state }) {
    if (state === "queued") {
        return html`<span class="mcmdstate"><span class="mclock">${Icon.clock()}</span>queued</span>`;
    }
    if (state === "withdrawn") return html`<span class="mcmdstate">taken back — it did not run</span>`;
    return html`<span class="mcmdstate run"><i class="mshelldot"></i>running</span>`;
}

// Printed is the text a command printed, its lines as it printed them. Longer
// than the lines the card shows, it is folded to them, and a row under it
// says how many more there are — counted as the screen sets them, so an
// answer of one long paragraph folds as well — and opens the rest.
function Printed({ text, err, cut }) {
    const box = useRef(null);
    const [open, setOpen] = useState(false);
    const [more, setMore] = useState(0);
    // Measured again whenever the box changes its size: a screen turned, or
    // a feed drawn while hidden and shown later.
    useLayoutEffect(() => {
        const el = box.current;
        if (!el || open) return undefined;
        const measure = () => {
            const line = parseFloat(getComputedStyle(el).lineHeight) || 1;
            setMore(Math.max(0, Math.round((el.scrollHeight - el.clientHeight) / line)));
        };
        measure();
        if (typeof ResizeObserver === "undefined") return undefined;
        const eye = new ResizeObserver(measure);
        eye.observe(el);
        return () => eye.disconnect();
    }, [text, open]);
    return html`
        <pre class=${`mcmdout${err ? " err" : ""}${open ? "" : " folded"}${more > 0 && !open ? " cut" : ""}`}
             ref=${box}>${text}</pre>
        ${more > 0 && html`
            <button class="mcmdmore" type="button" aria-expanded=${open ? "true" : "false"}
                    onClick=${() => setOpen(!open)}>
                ${open ? "fold" : `${more} more ${plural(more, "line", "lines")}`}
            </button>
        `}
        ${(open || !more) && cut && html`<p class="hint warn">The answer is longer than shown — cut.</p>`}
    `;
}

// ContextCard is the answer of /context: how full the window is, and a bar of
// what fills it.
function ContextCard({ item, onOpen }) {
    const data = item.data || {};
    const body = html`
        <span class="cmdico">${Icon.pie()}</span>
        <span class="arbody">
            <span class="cmdtop">
                <span class="artitle">${COMMAND_TITLES.context}</span>
                <span class="cmdfig">${figure(data)}</span>
            </span>
            <${Bar} data=${data} />
        </span>
        ${onOpen && html`<span class="crgo">${Icon.chevron()}</span>`}
    `;
    if (!onOpen) return html`<div class="artifact cmdcard dead">${body}</div>`;
    return html`
        <button type="button" class="artifact cmdcard" aria-label=${`${COMMAND_TITLES.context}: ${figure(data)}`}
                onClick=${() => onOpen(item)}>${body}</button>
    `;
}

// CommandSheet is the breakdown the card opens: a bottom sheet on a phone,
// a dialog on a wide screen — the Sheet it sits in decides which.
export function CommandSheet({ item }) {
    if (!item) return null;
    if (item.name === "usage") return html`<${UsageBreakdown} data=${item.data || {}} />`;
    if (item.name !== "context") return null;
    return html`<${ContextBreakdown} data=${item.data || {}} />`;
}

// plainText is the breakdown as it goes to the clipboard: the table a person
// pastes into a note or a message, not the picture.
export function plainText(data) {
    const max = data.max || 0;
    const cats = data.categories || [];
    const lines = [
        `Context window · ${modelTitle(data.model)}`,
        `${tokens(data.used)} of ${tokens(max)} tokens (${Math.round(data.percent || 0)}%)`,
        "",
        ...cats.filter((c) => c.kind !== "deferred")
            .map((c) => `${c.name}: ${count(c)} (${share(c.tokens, max)})`),
    ];
    const later = cats.filter((c) => c.kind === "deferred");
    if (later.length) {
        lines.push("", "Loaded on demand:",
            ...later.map((c) => `${c.name.replace(/\s*\(deferred\)$/i, "")}: ${count(c)}`));
    }
    return lines.join("\n");
}

function ContextBreakdown({ data }) {
    const toast = useToast();
    const max = data.max || 0;
    const cats = data.categories || [];
    const inside = cats.filter((c) => c.kind !== "deferred");
    const later = cats.filter((c) => c.kind === "deferred");
    const mcp = data.mcp || [];
    const agents = data.agents || [];
    const memory = data.memory || [];
    const skills = data.skills || [];
    const tools = mcp.reduce((n, s) => n + (s.tools || 0), 0);
    return html`
        <div class="cmdsheet">
            <div class="shead cmdtitle">
                <span class="cmdhead">${COMMAND_TITLES.context}</span>
                <button class="cmdcopy" type="button" aria-label="copy the breakdown"
                        onClick=${() => copyText(plainText(data), toast, "Copied", COMMAND_TITLES.context)}>
                    ${Icon.copy()}
                </button>
            </div>

            <section class="cmdsec">
                <div class="cmdsechead">
                    <span>In the window</span>
                    ${data.model && html`<span class="cmdaside">${modelTitle(data.model)}</span>`}
                </div>
                <div class="cmdmeter">
                    <span class="cmdsub">${tokens(data.used)} of ${tokens(max)} tokens</span>
                    <b>${Math.round(data.percent || 0)}%</b>
                </div>
                <${Bar} data=${data} big />
                <ul class="cmdcats">
                    ${inside.map((c) => html`
                        <li key=${c.name} class=${`cmdcat t-${tone(c)}`}>
                            <i class="cmddot"></i>
                            <span class="cmdname">${c.name}</span>
                            <span class="cmdtok">${count(c)}</span>
                            <span class="cmdpct">${share(c.tokens, max)}</span>
                        </li>
                    `)}
                </ul>
            </section>

            ${later.length > 0 && html`
                <section class="cmdsec">
                    <div class="cmdsechead">
                        <span>Loaded on demand</span>
                        <span class="cmdaside">not in the window until used</span>
                    </div>
                    <ul class="cmdcats later">
                        ${later.map((c) => html`
                            <li key=${c.name} class="cmdcat">
                                <span class="cmdname">${c.name.replace(/\s*\(deferred\)$/i, "")}</span>
                                <span class="cmdtok">${count(c)}</span>
                            </li>
                        `)}
                    </ul>
                </section>
            `}

            <div class="cmdparts">
            ${mcp.length > 0 && html`
                <${Part} title="MCP servers"
                         sum=${`${sized(mcp.length, "server", "servers")} · ${sized(tools, "tool", "tools")}`}
                         rows=${mcp.map((s) => ({ key: s.name, name: s.name,
                                                 note: sized(s.tools, "tool", "tools"), tok: count(s) }))} />
            `}
            ${agents.length > 0 && html`
                <${Part} title="Custom agents" sum=${sized(data.agentsTotal || agents.length, "agent", "agents")}
                         rows=${byTokens(agents).map((a) => ({ key: a.name, name: a.name, note: a.source, tok: count(a) }))} />
            `}
            ${memory.length > 0 && html`
                <${Part} title="Memory files" sum=${sized(memory.length, "file", "files")}
                         rows=${byTokens(memory).map((m) => ({ key: m.path, name: m.path, note: m.type, tok: count(m) }))} />
            `}
            ${skills.length > 0 && html`
                <${Part} title="Skills" sum=${sized(data.skillsTotal || skills.length, "skill", "skills")}
                         rows=${byTokens(skills).map((s) => ({ key: s.name, name: s.name, note: s.source, tok: count(s) }))} />
            `}
            </div>
        </div>
    `;
}

function sized(n, one, many) {
    return `${n} ${plural(n, one, many)}`;
}

function byTokens(rows) {
    return [...rows].sort((a, b) => (b.tokens || b.under || 0) - (a.tokens || a.under || 0));
}

// Part is one list of what takes room — servers, agents, files, skills —
// folded to its count until asked for: a setup lists them by the hundred.
function Part({ title, sum, rows }) {
    const [open, setOpen] = useState(false);
    return html`
        <section class=${`cmdsec cmdpart${open ? " open" : ""}`}>
            <button class="cmdsechead cmdparthead" type="button" onClick=${() => setOpen(!open)}
                    aria-expanded=${open ? "true" : "false"}>
                <span class="cmdname">${title}</span>
                <span class="cmdaside">${sum}</span>
                <span class="crgo">${Icon.chevron()}</span>
            </button>
            ${open && html`
                <ul class="cmdrows">
                    ${rows.map((r) => html`
                        <li key=${r.key}>
                            <span class="cmdname">${r.name}</span>
                            ${r.note && html`<span class="cmdrownote">${r.note}</span>`}
                            <span class="cmdtok">${r.tok}</span>
                        </li>
                    `)}
                </ul>
            `}
        </section>
    `;
}
