// Header: which machine is watched, how the link to it stands and what it is busy with.
import { useEffect, useState } from "preact/hooks";

import { html } from "../html.js";
import { logout } from "../auth.js";
import { shelf, waiting } from "../data/briefs.js";
import { openLimits } from "../screens/sessions/limits.js";
import { Icon } from "./icons.js";
import { connection } from "./route.js";
import { Sheet } from "./sheet.js";
import { level, pct, rate, uptime } from "../format.js";

// The bar is one line at any phone width and in any state. The chip of the
// connection keeps the words of the state; a long host name gives way instead.
// The theme and the install live in the host menu; the caret lights up while
// the install waits there, or it would never be found.
export function Header({ hostName = "host", ageSec, conn, route, flag = false, machine, onMachine, alerts = 0, query, onQuery, onMenu, onAlerts }) {
    const [searching, setSearching] = useState(false);
    const state = connection({ conn, ageSec, route });
    const leg = state.leg !== undefined;

    return html`
        <header class="top">
            <div class="titlerow">
                <button
                    class="host"
                    type="button"
                    data-flag=${flag ? "install" : undefined}
                    aria-label=${flag ? `${hostName} menu, the app can be installed` : `${hostName} menu`}
                    onClick=${onMenu}
                ><span class="hname">${hostName}</span><span class="caret">▾</span></button>
                <button
                    class="conn"
                    type="button"
                    data-tone=${state.tone || undefined}
                    data-words=${leg ? undefined : "say"}
                    aria-label=${`connection: ${state.phrase}`}
                    title=${state.phrase}
                    onClick=${route && route.onOpen}
                >
                    <span class="cbox">
                        ${state.dot && html`<i class="cdot" data-dot=${state.dot}></i>`}
                        <em class="cword">${leg
                            ? html`${state.leg}${state.age && html`<span class="csep"> · </span>${state.age}`}`
                            : state.words}</em>
                    </span>
                </button>
                ${state.signIn && html`<a class="signin" href="/login"><span>Sign in</span></a>`}
                <div class="icons">
                    <button class="iconbtn" type="button" aria-label="alerts" onClick=${onAlerts}>
                        ${Icon.alerts()}
                        ${alerts > 0 && html`<span class="badge">${alerts}</span>`}
                    </button>
                    <button
                        class="iconbtn ${searching ? "on" : ""}"
                        type="button"
                        aria-label="search"
                        aria-pressed=${searching ? "true" : "false"}
                        onClick=${() => {
                            if (searching) onQuery("");
                            setSearching(!searching);
                        }}
                    >${Icon.search()}</button>
                </div>
            </div>

            ${searching && html`
                <input
                    class="search"
                    type="search"
                    placeholder="Filter by name"
                    autocomplete="off"
                    spellcheck="false"
                    value=${query}
                    onInput=${(event) => onQuery(event.target.value)}
                />
            `}

            ${machine && html`
                <div class="hostbar">
                    <div class="hstats">
                        <${Stat} title="cpu" value=${pct(machine.cpuPct)} fill=${machine.cpuPct} />
                        <${Stat} title="memory" value=${pct(machine.memPct)} fill=${machine.memPct} />
                        <${Stat} title="network" value=${rate(machine.net)} fill=${null} />
                    </div>
                    <button class="hdetails" type="button" onClick=${onMachine}>
                        details
                        <span class="chev">${Icon.chevron()}</span>
                    </button>
                </div>
            `}
        </header>
    `;
}

// HostMenu is the sheet behind the host name. It opens on what the machine is
// doing — the load, the limits of the contour, the briefs waiting — each a
// door to its page, then the install or the update while one waits, the pages
// that have no tab of their own, the theme and the way out. Above those pages
// stand the accounts of Claude and Codex and the balancer between them, doors
// that do not open yet. It fits the height the sheet opens to: nothing in it
// needs a drag to be found. A value the app does not have leaves its line out;
// the menu says less rather than a zero.
export function HostMenu({
    open, onClose, onPage, hostName = "host", snapshot, conn, ageSec, route,
    installable = false, onInstall, updateReady = false, updating = false, onApplyUpdate, theme, onTheme,
}) {
    const light = theme === "sky";
    const counts = useMenuCounts(open);
    const state = connection({ conn, ageSec, route });
    // Without a map of addresses the chip says "live", which is not a way to
    // the host, so the line names the leg only when there is one.
    const way = route && route.here ? state.leg : undefined;
    const host = (snapshot && snapshot.host) || {};
    const up = uptime(host.uptime);
    const cpu = figure(host.cpuPct);
    const mem = figure(host.mem && host.mem.pct);
    const limits = open ? openLimits(snapshot) : null;
    const five = limits ? limits.five : null;
    const week = limits ? limits.week : null;
    const briefs = counts.briefs;
    return html`
        <${Sheet} open=${open} onClose=${onClose} label="menu">
            <div class="hm-head">
                <div class="hm-name">${hostName}</div>
                <div class="hm-sub" data-tone=${state.tone || undefined}>
                    ${state.dot && html`<i class="cdot" data-dot=${state.dot}></i>`}
                    <span>control panel${way && ` · via ${way}`}${up && ` · ${up}`}</span>
                </div>
            </div>
            <div class="hm-live">
                <button class="hm-tile" type="button" onClick=${() => onPage("machine")}>
                    <span class="t">${Icon.cpu()}Machine</span>
                    ${cpu !== null && html`<span class="n">${pct(cpu)}<small>cpu</small></span>`}
                    ${mem !== null && html`<span class="s">mem ${pct(mem)}</span>`}
                    ${cpu !== null && html`<span class="bar"><i class=${level(cpu)} style=${`width:${Math.min(100, cpu)}%`}></i></span>`}
                </button>
                <button class="hm-tile" type="button" data-stale=${limits && limits.stale ? "" : undefined}
                    title=${limits && limits.contour ? `the limits of contour ${limits.contour}` : undefined}
                    onClick=${() => onPage("usage")}>
                    <span class="t">${Icon.pie()}Usage</span>
                    ${five !== null && html`<span class="n">${pct(five)}<small>5 h</small></span>`}
                    ${week !== null && html`<span class="s">week ${pct(week)}</span>`}
                    ${five !== null && html`<span class="bar"><i class=${limits.tone} style=${`width:${Math.min(100, five)}%`}></i></span>`}
                </button>
                <button class=${`hm-tile${briefs > 0 ? " wait" : ""}`} type="button" onClick=${() => onPage("briefs")}>
                    <span class="t">${Icon.plan()}Briefs</span>
                    ${briefs !== undefined && html`<span class="n">${briefs}</span><span class="s">to answer</span>`}
                </button>
            </div>
            ${updateReady
                ? html`
                    <button class="hm-strip" type="button" disabled=${updating} onClick=${onApplyUpdate}>
                        ${Icon.refresh()}${updating ? "Updating…" : "Update the app"}<small>a new version is ready</small>
                    </button>
                `
                : installable && html`
                    <button class="hm-strip" type="button" onClick=${() => { onClose(); onInstall(); }}>
                        ${Icon.download()}Install the app<small>not installed yet</small>
                    </button>
                `}
            <div class="hm-quiet hm-accounts">
                <button class="hm-q" type="button" disabled>
                    <span class="t">${Icon.robot()}Claude</span>
                    <small>accounts · soon</small>
                </button>
                <button class="hm-q" type="button" disabled>
                    <span class="t">${Icon.braces()}Codex</span>
                    <small>accounts · soon</small>
                </button>
                <button class="hm-q" type="button" disabled>
                    <span class="t">${Icon.flow()}Balancer</span>
                    <small>settings · soon</small>
                </button>
            </div>
            <div class="hm-quiet">
                <button class="hm-q" type="button" onClick=${() => onPage("journal")}>
                    <span class="t">${Icon.list()}Journal</span>
                    ${counts.failed && html`
                        <small class=${counts.failed.n > 0 ? "crit" : undefined}>${failedWords(counts.failed)}</small>
                    `}
                </button>
                <button class="hm-q" type="button" onClick=${() => onPage("devices")}>
                    <span class="t">${Icon.skill()}Devices</span>
                    ${counts.devices !== undefined && html`<small>${counts.devices} with access</small>`}
                </button>
                <button class="hm-q" type="button" onClick=${() => onPage("settings")}>
                    <span class="t">${Icon.info()}Settings</span>
                    <small>host · device</small>
                </button>
            </div>
            <div class="hm-foot">
                <div class="hm-seg" role="group" aria-label="theme">
                    <button type="button" aria-pressed=${light ? "false" : "true"}
                        onClick=${() => light && onTheme()}>${Icon.moon()}Dark</button>
                    <button type="button" aria-pressed=${light ? "true" : "false"}
                        onClick=${() => !light && onTheme()}>${Icon.sun()}Light</button>
                </div>
                <button class="hm-out" type="button" onClick=${logout}>${Icon.exit()}Sign out</button>
            </div>
        <//>
    `;
}

// figure is a share the snapshot carries, or null when it carries none.
function figure(value) {
    return typeof value === "number" && Number.isFinite(value) ? value : null;
}

// One page of failures is enough to count a day: a day with more of them than
// the page holds says so with a plus rather than a number it cannot vouch for.
const FAILED_PAGE = 50;

function failedWords({ n, more }) {
    if (n === 0) return "none failed today";
    return `${n}${more ? "+" : ""} failed today`;
}

async function answer(url) {
    const r = await fetch(url, { credentials: "same-origin" });
    if (!r.ok) throw new Error(`${url} answered ${r.status}`);
    return r.json();
}

async function failedToday() {
    const body = await answer(`/api/actions?limit=${FAILED_PAGE}&result=failed`);
    const list = body.actions || [];
    const start = new Date();
    start.setHours(0, 0, 0, 0);
    const today = list.filter((row) => new Date(row.ts) >= start);
    return { n: today.length, more: list.length === FAILED_PAGE && today.length === list.length };
}

async function devicesWithAccess() {
    const body = await answer("/api/devices");
    return (body.devices || []).length;
}

// useMenuCounts asks for what the menu says that the snapshot does not carry:
// the briefs waiting for an answer, the actions that failed today and the
// devices with access. It asks each time the menu opens and never while it
// stays open, and the menu does not wait for it: a line fills in when its
// answer comes. An answer that does not come — no database, no network — sets
// nothing, and its line stays out. Closing forgets the counts, so the next
// opening does not show the last ones and then change them under the eyes.
function useMenuCounts(open) {
    const [counts, setCounts] = useState({});
    useEffect(() => {
        if (!open) return undefined;
        let alive = true;
        const put = (key) => (value) => {
            if (alive) setCounts((prev) => ({ ...prev, [key]: value }));
        };
        const none = () => {};
        shelf().then((cards) => put("briefs")(waiting(cards)), none);
        failedToday().then(put("failed"), none);
        devicesWithAccess().then(put("devices"), none);
        return () => {
            alive = false;
            setCounts({});
        };
    }, [open]);
    return counts;
}

function Stat({ title, value, fill }) {
    return html`
        <div class="hstat">
            <span class="hstat-title">${title}</span>
            <span class="hstat-value">${value}</span>
            <div class=${`bar${fill == null ? " blank" : ""}`}>
                ${fill != null && html`
                    <i class=${level(fill)} style=${`width:${Math.min(100, fill)}%`}></i>
                `}
            </div>
        </div>
    `;
}
