// The answer of /usage — and of /cost, the same command in a session on the
// stream: the limits of the plan, what this session cost, and which habits
// spent the limits.

import { useState } from "preact/hooks";

import { html } from "../../html.js";
import { Icon } from "../../ui/icons.js";
import { useToast } from "../../ui/toasts.js";
import { duration, tokens, until } from "../../format.js";
import { copyText } from "./copy.js";
import { modelTitle } from "./head.js";

export const USAGE_TITLE = "Usage";

// limitName names a limit the way the client does.
export function limitName(limit) {
    if (limit.kind === "session") return "5-hour limit";
    if (limit.kind === "weekly_all") return "Weekly · all models";
    if (limit.kind === "weekly_scoped") return `Weekly · ${limit.model || "one model"}`;
    return String(limit.kind || "limit").replace(/_/g, " ");
}

// resetSay says when a limit starts over: how soon, within a day, and the date
// and the time past that — "in 150 h" is not a moment anyone can place.
export function resetSay(iso, now = Date.now()) {
    const at = new Date(iso || "");
    if (!iso || Number.isNaN(at.getTime())) return "";
    const left = at.getTime() - now;
    if (left <= 0) return "Resets now";
    if (left < 86400000) return `Resets ${until(iso)}`;
    return `Resets ${at.toLocaleString("ru-RU", { day: "2-digit", month: "2-digit", hour: "2-digit", minute: "2-digit" })}`;
}

// The loudness of a limit is the one the report gives it, not a threshold of
// the panel's own: the plan knows where its warning starts.
function tone(limit) {
    if (limit.severity === "warning") return "t-warning";
    if (limit.severity && limit.severity !== "normal") return "t-critical";
    return "t-normal";
}

function Fill({ limit }) {
    const width = Math.min(Math.max(limit.percent || 0, 0), 100);
    return html`
        <span class="cmdbar" aria-hidden="true">
            <i class=${tone(limit)} style=${`width: ${Math.max(width, 0.4)}%`}></i><b></b>
        </span>
    `;
}

function money(n) {
    return `$${(n || 0).toFixed(2)}`;
}

// cacheHit is the share of what the models read that came from the cache.
export function cacheHit(models) {
    let read = 0;
    let all = 0;
    for (const m of models || []) {
        read += m.cacheRead || 0;
        all += (m.input || 0) + (m.cacheRead || 0) + (m.cacheWrite || 0);
    }
    return all ? Math.round((read / all) * 100) : null;
}

function headline(data) {
    const limits = data.limits || [];
    const five = limits.find((l) => l.kind === "session");
    const week = limits.find((l) => l.kind === "weekly_all");
    const parts = [];
    if (five) parts.push(`5-hour ${Math.round(five.percent)}%`);
    if (week) parts.push(`week ${Math.round(week.percent)}%`);
    return parts.length ? parts.join(" · ") : money(data.cost);
}

// UsageCard is the line /usage leaves in the feed: the limits that matter now,
// and a bar of the fullest one.
export function UsageCard({ item, onOpen }) {
    const data = item.data || {};
    const fullest = [...(data.limits || [])].sort((a, b) => b.percent - a.percent)[0];
    const body = html`
        <span class="cmdico">${Icon.clock()}</span>
        <span class="arbody">
            <span class="cmdtop">
                <span class="artitle">${USAGE_TITLE}</span>
                <span class="cmdfig">${headline(data)}</span>
            </span>
            ${fullest && html`<${Fill} limit=${fullest} />`}
        </span>
        ${onOpen && html`<span class="crgo">${Icon.chevron()}</span>`}
    `;
    if (!onOpen) return html`<div class="artifact cmdcard dead">${body}</div>`;
    return html`
        <button type="button" class="artifact cmdcard" aria-label=${`${USAGE_TITLE}: ${headline(data)}`}
                onClick=${() => onOpen(item)}>${body}</button>
    `;
}

// usageText is the report as it goes to the clipboard.
export function usageText(data) {
    const lines = [USAGE_TITLE];
    for (const l of data.limits || []) {
        const reset = resetSay(l.resets);
        lines.push(`${limitName(l)}: ${Math.round(l.percent)}%${reset ? ` · ${reset.toLowerCase()}` : ""}`);
    }
    const hit = cacheHit(data.models);
    lines.push("", `This session: ${money(data.cost)}${hit == null ? "" : ` · cache hit ${hit}%`}`);
    for (const m of data.models || []) {
        lines.push(`${modelTitle(m.id)}: ${tokens(m.input)} in, ${tokens(m.output)} out, `
            + `${tokens(m.cacheRead)} cache read, ${tokens(m.cacheWrite)} cache write, ${money(m.cost)}`);
    }
    return lines.join("\n");
}

// UsageBreakdown is the sheet the card opens.
export function UsageBreakdown({ data }) {
    const toast = useToast();
    const [at, setAt] = useState(0);
    const limits = data.limits || [];
    const models = data.models || [];
    const habits = (data.behaviors && data.behaviors.windows) || [];
    const shown = habits[Math.min(at, habits.length - 1)];
    const hit = cacheHit(models);
    return html`
        <div class="cmdsheet">
            <div class="shead cmdtitle">
                <span class="cmdhead">${USAGE_TITLE}</span>
                <button class="cmdcopy" type="button" aria-label="copy the report"
                        onClick=${() => copyText(usageText(data), toast, "Copied", USAGE_TITLE)}>
                    ${Icon.copy()}
                </button>
            </div>

            ${limits.length > 0 && html`
                <section class="cmdsec cmdlimits">
                    ${limits.map((l) => html`
                        <div key=${`${l.kind}-${l.model}`} class="cmdlimit">
                            <div class="cmdmeter">
                                <span class="cmdlimname">${limitName(l)}</span>
                                <span class="cmdsub">${resetSay(l.resets)}</span>
                                <b>${Math.round(l.percent)}%</b>
                            </div>
                            <${Fill} limit=${l} />
                        </div>
                    `)}
                </section>
            `}

            <section class="cmdsec">
                <div class="cmdsechead"><span>This session</span></div>
                <div class="cmdfacts">
                    <span>Cost <b>${money(data.cost)}</b></span>
                    ${hit != null && html`<span>Cache hit <b>${hit}%</b></span>`}
                    ${data.apiMs > 0 && html`<span>API <b>${duration(data.apiMs / 1000)}</b></span>`}
                    ${(data.added > 0 || data.removed > 0) && html`
                        <span>Lines <b>+${data.added} −${data.removed}</b></span>`}
                </div>
            </section>

            ${models.map((m) => html`
                <section key=${m.id} class="cmdsec">
                    <div class="cmdsechead">
                        <span>Breakdown</span>
                        <span class="cmdaside">${modelTitle(m.id)}</span>
                    </div>
                    <ul class="cmdrows cmdusage">
                        <li><span class="cmdname">Input</span><span class="cmdtok">${tokens(m.input)}</span></li>
                        <li><span class="cmdname">Output</span><span class="cmdtok">${tokens(m.output)}</span></li>
                        ${m.thinking > 0 && html`
                            <li><span class="cmdname">Thinking</span><span class="cmdtok">${tokens(m.thinking)}</span></li>`}
                        <li><span class="cmdname">Cache read</span><span class="cmdtok">${tokens(m.cacheRead)}</span></li>
                        <li><span class="cmdname">Cache write</span><span class="cmdtok">${tokens(m.cacheWrite)}</span></li>
                        <li><span class="cmdname">Cost</span><span class="cmdtok">${money(m.cost)}</span></li>
                    </ul>
                </section>
            `)}

            ${habits.length > 0 && html`
                <section class="cmdsec">
                    <div class="cmdsechead">
                        <span>What's using your limits?</span>
                        <span class="cmdtabs" role="group" aria-label="the period">
                            ${habits.map((w, i) => html`
                                <button key=${w.window} type="button" class=${`cmdtab${w === shown ? " on" : ""}`}
                                        aria-pressed=${w === shown} onClick=${() => setAt(i)}>${w.window}</button>
                            `)}
                        </span>
                    </div>
                    ${data.behaviors.caveat && html`<p class="cmdnote">${data.behaviors.caveat}</p>`}
                    ${shown.summary && html`<p class="cmdsum">Last ${shown.window} · ${shown.summary}</p>`}
                    ${shown.lines.length
                        ? html`<ul class="cmdrows cmdhabits">
                            ${shown.lines.map((line) => html`<li key=${line}><span class="cmdname">${line}</span></li>`)}
                          </ul>`
                        : html`<p class="cmdnote">No local activity in this window.</p>`}
                </section>
            `}
        </div>
    `;
}
