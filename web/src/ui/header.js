// Header: which machine is watched, how fresh the data is and what it is busy with.
import { useState } from "preact/hooks";

import { html } from "../html.js";
import { Icon } from "./icons.js";
import { level, pct, rate } from "../format.js";

const STALE_SEC = 60;
const DEAD_SEC = 300;

function ageText(sec) {
    if (sec < 10) return "just now";
    if (sec < 60) return `${Math.round(sec)} s`;
    if (sec < 3600) return `${Math.round(sec / 60)} min`;
    return `snapshot ${Math.round(sec / 3600)} h ago`;
}

function ageClass(sec) {
    if (sec >= DEAD_SEC) return "crit";
    if (sec >= STALE_SEC) return "warn";
    return "";
}

export function Header({ hostName = "host", ageSec, machine, onMachine, alerts = 0, query, onQuery, onMenu, onAlerts, status, theme, onTheme, insecure = false }) {
    const [searching, setSearching] = useState(false);
    const stale = ageSec === null || ageSec === undefined;

    return html`
        <header class="top">
            <div class="titlerow">
                <button class="host" type="button" onClick=${onMenu}>
                    ${hostName}<span class="caret">▾</span>
                </button>
                <div class="statusbar">
                    <span class="age ${stale ? "crit" : ageClass(ageSec)}">${stale ? "no agent snapshot" : ageText(ageSec)}</span>
                    ${insecure && html`
                        <span class="age warn" title="AACP_SECURE=0 in .env while the panel is reached over https: the session cookie has no Secure flag and travels over plain http as well">cookie without Secure</span>
                    `}
                    ${status}
                </div>
                <div class="icons">
                    <button
                        class="iconbtn"
                        type="button"
                        aria-label=${theme === "sky" ? "dark theme" : "light theme"}
                        onClick=${onTheme}
                    >${theme === "sky" ? Icon.moon() : Icon.sun()}</button>
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
