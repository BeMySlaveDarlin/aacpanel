// The answer of a slash command: a card in the feed, and its breakdown in a sheet.
//
// What a command prints is written for the screen it was typed at — a grid of
// coloured glyphs in a terminal, a long markdown table elsewhere — and neither
// reads in a feed. The host hands over the numbers instead, and they are drawn
// here: one line in the conversation, the whole picture on a tap.

import { useState } from "preact/hooks";

import { html } from "../../html.js";
import { Icon } from "../../ui/icons.js";
import { useToast } from "../../ui/toasts.js";
import { plural, tokens } from "../../format.js";
import { copyText } from "./copy.js";

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

// CommandCard is the line a command answer takes in the feed.
export function CommandCard({ item, onOpen }) {
    const data = item.data || {};
    if (item.name !== "context") return null;
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
    if (!item || item.name !== "context") return null;
    return html`<${ContextBreakdown} data=${item.data || {}} />`;
}

// modelTitle names a model the way the client does — "Opus 5.5 · 1M" — and
// not by its id: the id is for machines, and the window rides on it as "[1m]".
export function modelTitle(id) {
    const raw = String(id || "");
    const found = /^(?:claude-)?([a-z]+)-(\d+)(?:-(\d{1,2}))?(?=-\d{8}|\[|$)/i.exec(raw);
    if (!found) return raw;
    const name = found[1][0].toUpperCase() + found[1].slice(1);
    const version = found[3] ? `${found[2]}.${found[3]}` : found[2];
    const wide = /\[1m\]/i.test(raw) ? " · 1M" : "";
    return `${name} ${version}${wide}`;
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
