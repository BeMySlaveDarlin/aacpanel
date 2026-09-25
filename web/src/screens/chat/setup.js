// What a session on the stream is set up with, one read-only screen at a time,
// the way the client's own screens show it: its hooks, its memory, its
// skills, the kinds of subagents it can start and its merged settings. The
// panel asks the session each time a screen opens, and changes nothing: a hook
// or a skill is changed in its file or by asking Claude.

import { useEffect, useState } from "preact/hooks";

import { html } from "../../html.js";
import { Icon } from "../../ui/icons.js";
import { ago, plural } from "../../format.js";

export const SETUP_TITLES = {
    hooks: "Hooks",
    memory: "Memory",
    skills: "Skills",
    agents: "Agents",
    config: "Settings",
};

function useSetup(name, part, tick) {
    const [answer, setAnswer] = useState(null);
    useEffect(() => {
        let alive = true;
        setAnswer(null);
        fetch(`/api/session/setup?name=${encodeURIComponent(name)}&part=${encodeURIComponent(part)}`,
            { credentials: "same-origin" })
            .then(async (r) => {
                if (!r.ok) throw new Error((await r.text()).trim() || `the server answered ${r.status}`);
                return r.json();
            })
            .then((body) => { if (alive) setAnswer(body); })
            .catch((err) => { if (alive) setAnswer({ state: "unknown", reason: String(err.message || err) }); });
        return () => { alive = false; };
    }, [name, part, tick]);
    return answer;
}

function Head({ title, onBack, onAgain }) {
    return html`
        <div class="shead cmdtitle">
            ${onBack && html`
                <button class="cmdcopy mcpback" type="button" aria-label="back to the list" onClick=${onBack}>
                    ${Icon.chevron()}
                </button>`}
            <span class="cmdhead">${title}</span>
            ${onAgain && html`
                <button class="cmdcopy" type="button" aria-label="ask the session again" onClick=${onAgain}>
                    ${Icon.refresh()}
                </button>`}
        </div>
    `;
}

// SetupSheet is what /hooks, /memory, /skills, /agents and /config open in a
// session on the stream.
export function SetupSheet({ name, part }) {
    const [tick, setTick] = useState(0);
    const data = useSetup(name, part, tick);
    const title = SETUP_TITLES[part] || part;
    const again = () => setTick((n) => n + 1);
    const plain = (text) => html`<div class="cmdsheet"><${Head} title=${title} onAgain=${again} /><p class="cmdnote">${text}</p></div>`;

    if (!data) return plain("Asking the session…");
    if (data.state !== "ok") return plain(data.reason || "The session did not answer.");
    if (data.transport !== "stream") {
        return plain(`This session runs in a console: this is on its own screen there, /${part} with keys. `
            + "The panel shows it for a session in the feed.");
    }
    const head = html`<${Head} title=${title} onAgain=${again} />`;
    switch (part) {
    case "hooks": return html`<${HooksView} hooks=${data.hooks} head=${head} />`;
    case "memory": return html`<${MemoryView} memory=${data.memory} head=${head} />`;
    case "skills": return html`<${SkillsView} skills=${data.skills} head=${head} />`;
    case "agents": return html`<${AgentsView} agents=${data.agents} head=${head} />`;
    default: return html`<${ConfigView} config=${data.config} head=${head} />`;
    }
}

// hookTitle names a hook by its kind, as the client's card of one hook does.
export function hookTitle(hook) {
    const type = String(hook.type || "hook");
    return `${type.charAt(0).toUpperCase()}${type.slice(1).replace(/_/g, " ")} hook`;
}

function HooksView({ hooks, head }) {
    const [event, setEvent] = useState("");
    const [open, setOpen] = useState(-1);
    const all = hooks.hooks || [];
    const events = hooks.events || [];

    if (event && open >= 0 && all[open]) {
        const h = all[open];
        return html`
            <div class="cmdsheet">
                <${Head} title=${hookTitle(h)} onBack=${() => setOpen(-1)} />
                <p class="cmdsum">${h.event}</p>
                <ul class="cmdrows mcpfacts">
                    <li><span class="cmdname">Source</span><span class="cmdtok">${h.source}</span></li>
                    ${h.plugin && html`<li><span class="cmdname">Plugin</span><span class="cmdtok">${h.plugin}</span></li>`}
                    ${h.matcher && html`<li><span class="cmdname">Matcher</span><span class="cmdtok mcpwhere">${h.matcher}</span></li>`}
                    ${h.condition && html`<li><span class="cmdname">If</span><span class="cmdtok mcpwhere">${h.condition}</span></li>`}
                    ${h.timeout > 0 && html`<li><span class="cmdname">Timeout</span><span class="cmdtok">${h.timeout} s</span></li>`}
                    ${h.disabled && html`<li><span class="cmdname">Runs</span><span class="cmdtok">no: the mode or a policy keeps it off</span></li>`}
                </ul>
                <section class="cmdsec">
                    <div class="cmdsechead"><span>${h.textLabel || "Command"}</span></div>
                    <pre class="stpcode">${h.text}</pre>
                </section>
                <p class="cmdnote">To change or remove it, edit the settings file it comes from or ask Claude.</p>
            </div>
        `;
    }

    if (event) {
        const ev = events.find((e) => e.name === event) || { name: event, summary: "" };
        const rows = all.map((h, i) => ({ h, i })).filter(({ h }) => h.event === event);
        return html`
            <div class="cmdsheet">
                <${Head} title=${ev.name} onBack=${() => setEvent("")} />
                ${ev.summary && html`<p class="cmdnote">${ev.summary}</p>`}
                <ul class="mcplist">
                    ${rows.map(({ h, i }) => html`
                        <li key=${i}>
                            <button type="button" class=${`mcprow${h.disabled ? " stpoff" : ""}`} onClick=${() => setOpen(i)}>
                                <span class="cmdname">
                                    <span class="stptag">${h.type}</span>${h.matcher && html`<span class="stptag">${h.matcher}</span>`}${h.label}
                                </span>
                                <span class="cmdrownote">${h.disabled ? "off" : (h.plugin || h.source)}</span>
                                <span class="crgo">${Icon.chevron()}</span>
                            </button>
                        </li>
                    `)}
                </ul>
            </div>
        `;
    }

    return html`
        <div class="cmdsheet">
            ${head}
            <p class="cmdsum">${all.length} ${plural(all.length, "hook", "hooks")} configured</p>
            ${hooks.blocked && html`<p class="cmdnote stpwarn">${hooks.blocked}</p>`}
            <p class="cmdnote">Read-only: to add or change a hook, edit settings.json or ask Claude.</p>
            ${events.length > 0 && html`
                <ul class="mcplist">
                    ${events.map((e) => html`
                        <li key=${e.name}>
                            <button type="button" class="mcprow" onClick=${() => setEvent(e.name)}>
                                <span class="cmdname">${e.name} <span class="stpcount">${e.count}</span></span>
                                <span class="cmdrownote">${e.summary}</span>
                                <span class="crgo">${Icon.chevron()}</span>
                            </button>
                        </li>
                    `)}
                </ul>
            `}
        </div>
    `;
}

// FOLDERS names the memory folders by what they hold: the client's own label
// is the name of a button that opens one.
const FOLDERS = { auto: "Auto memory", team: "Team memory", agent: "Agent memory" };

function MemoryView({ memory, head }) {
    const files = memory.files || [];
    const folders = memory.folders || [];
    const saved = memory.memories || [];
    return html`
        <div class="cmdsheet">
            ${head}
            ${memory.auto && html`<p class="cmdsum">Auto-memory: ${memory.auto}</p>`}
            <section class="cmdsec">
                <div class="cmdsechead"><span>Instructions</span><span class="cmdaside">${files.length}</span></div>
                <ul class="cmdrows stplist">
                    ${files.map((f) => html`
                        <li key=${f.path}>
                            <span class="stpline">
                                <span class="stpname">${f.label}</span>
                                <span class="cmdrownote">${f.missing ? "not created" : ""}</span>
                            </span>
                            <span class="stpsub">${f.path}</span>
                        </li>
                    `)}
                </ul>
            </section>
            ${folders.length > 0 && html`
                <section class="cmdsec">
                    <div class="cmdsechead"><span>Folders</span></div>
                    <ul class="cmdrows stplist">
                        ${folders.map((f) => html`
                            <li key=${f.path}>
                                <span class="stpline"><span class="stpname">${FOLDERS[f.kind] || f.label}</span></span>
                                <span class="stpsub">${f.path}</span>
                            </li>
                        `)}
                    </ul>
                </section>
            `}
            ${saved.length > 0 && html`
                <section class="cmdsec">
                    <div class="cmdsechead"><span>Saved memories</span><span class="cmdaside">${saved.length}</span></div>
                    <ul class="cmdrows stplist">
                        ${saved.map((m) => html`
                            <li key=${m.name}>
                                <span class="stpline">
                                    <span class="stpname">${m.type && html`<span class="stptag">${m.type}</span>`}${m.name}</span>
                                    <span class="cmdrownote">${m.modified ? ago(new Date(m.modified).toISOString()) : ""}</span>
                                </span>
                                ${m.description && html`<span class="stpdesc">${m.description}</span>`}
                            </li>
                        `)}
                    </ul>
                </section>
            `}
        </div>
    `;
}

// skillNote says where a skill comes from, what it costs and what keeps it
// from changing, the way the client's row does.
export function skillNote(skill) {
    const parts = [];
    if (skill.lockedBy) parts.push(`locked by ${skill.lockedBy}`);
    if (skill.source) parts.push(skill.source);
    if (skill.tokens) parts.push(`~${skill.tokens} tok`);
    return parts.join(" · ");
}

// STATES says how a skill is used when it is not simply on.
const STATES = { "name-only": "name only", "user-invocable-only": "by hand only", off: "off" };

// matches keeps the rows whose name or description has the words typed.
export function matches(rows, typed) {
    const words = String(typed || "").toLowerCase().split(/\s+/).filter(Boolean);
    if (!words.length) return rows;
    return rows.filter((r) => {
        const text = `${r.name} ${r.description || ""}`.toLowerCase();
        return words.every((w) => text.includes(w));
    });
}

// Expandable is a row that opens to its description on a tap.
function Expandable({ title, note, tag, text, dim }) {
    const [open, setOpen] = useState(false);
    return html`
        <li class=${`stpexp${open ? " open" : ""}`}>
            <button type="button" class=${`mcprow${dim ? " stpoff" : ""}`} aria-expanded=${open ? "true" : "false"}
                    onClick=${() => setOpen((v) => !v)} disabled=${!text}>
                <span class="cmdname">${tag && html`<span class="stptag">${tag}</span>`}${title}</span>
                <span class="cmdrownote">${note}</span>
            </button>
            ${open && text && html`<p class="stpdesc">${text}</p>`}
        </li>
    `;
}

function SkillsView({ skills, head }) {
    const [typed, setTyped] = useState("");
    const rows = skills || [];
    const shown = matches(rows, typed);
    return html`
        <div class="cmdsheet">
            ${head}
            <p class="cmdsum">${rows.length} ${plural(rows.length, "skill", "skills")}</p>
            <input class="stpfind" type="search" placeholder="Search skills…" aria-label="search skills"
                   value=${typed} onInput=${(e) => setTyped(e.currentTarget.value)} />
            <ul class="mcplist">
                ${shown.map((s) => html`
                    <${Expandable} key=${s.name} title=${s.name} note=${skillNote(s)} text=${s.description}
                                   tag=${STATES[s.state] || ""} dim=${s.state === "off"} />
                `)}
            </ul>
            ${shown.length === 0 && html`<p class="cmdnote">No skill has these words.</p>`}
            <p class="cmdnote">Read-only: a skill is turned on or off in the console, /skills with keys.</p>
        </div>
    `;
}

function AgentsView({ agents, head }) {
    const rows = agents || [];
    return html`
        <div class="cmdsheet">
            ${head}
            <p class="cmdsum">${rows.length} ${plural(rows.length, "agent", "agents")}</p>
            <ul class="mcplist">
                ${rows.map((a) => html`<${Expandable} key=${a.name} title=${a.name} note=${a.model || ""} text=${a.description} />`)}
            </ul>
            <p class="cmdnote">Ask Claude to create or change a subagent, or edit the files: .claude/agents/ for this project,
                ~/.claude/agents/ for all of them.</p>
        </div>
    `;
}

// valueText shows a setting the way it reads best: a word or a number as it
// is, anything bigger as the JSON it is written in.
export function valueText(value) {
    if (typeof value === "string") return { inline: true, text: value };
    if (value === null || typeof value !== "object") return { inline: true, text: String(value) };
    return { inline: false, text: JSON.stringify(value, null, 2) };
}

function ConfigView({ config, head }) {
    const rows = config || [];
    return html`
        <div class="cmdsheet">
            ${head}
            <p class="cmdsum">${rows.length} ${plural(rows.length, "setting", "settings")}, merged from every settings file</p>
            <ul class="cmdrows mcpfacts stpconfig">
                ${rows.map((c) => {
                    const v = valueText(c.value);
                    return v.inline
                        ? html`<li key=${c.key}><span class="cmdname">${c.key}</span><span class="cmdtok mcpwhere">${v.text}</span></li>`
                        : html`<li key=${c.key} class="stpblock"><span class="cmdname">${c.key}</span><pre class="stpcode">${v.text}</pre></li>`;
                })}
            </ul>
            <p class="cmdnote">Not shown here: env, which carries keys; the rules of permissions; and hooks, which /hooks lists.</p>
        </div>
    `;
}
