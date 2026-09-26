// Header: which machine is watched, how the link to it stands and what it is busy with.
import { useState } from "preact/hooks";

import { html } from "../html.js";
import { logout } from "../auth.js";
import { Icon } from "./icons.js";
import { connection } from "./route.js";
import { Sheet } from "./sheet.js";
import { level, pct, rate } from "../format.js";

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

// HostMenu is the sheet behind the host name: the install while the app is not
// installed, the theme, and the pages that have no tab of their own.
export function HostMenu({ open, onClose, hostName = "host", installable = false, onInstall, theme, onTheme, onPage }) {
    const light = theme === "sky";
    return html`
        <${Sheet} open=${open} onClose=${onClose} label="menu">
            <div class="shead">
                <div><div class="stitle">${hostName}</div><div class="ssub">monitoring panel</div></div>
            </div>
            ${installable && html`
                <button class="item install" type="button" onClick=${() => { onClose(); onInstall(); }}>
                    ${Icon.download()}Install the app<small>not installed yet</small>
                </button>
            `}
            <div class="themerow" role="group" aria-label="theme">
                ${light ? Icon.sun() : Icon.moon()}
                <span class="grow">Theme</span>
                <button class="btn" type="button" aria-pressed=${light ? "false" : "true"}
                    onClick=${() => light && onTheme()}>${Icon.moon()}Dark</button>
                <button class="btn" type="button" aria-pressed=${light ? "true" : "false"}
                    onClick=${() => !light && onTheme()}>${Icon.sun()}Light</button>
            </div>
            <button class="item" type="button" onClick=${() => onPage("settings")}>Settings</button>
            <button class="item" type="button" onClick=${() => onPage("devices")}>Devices</button>
            <button class="item" type="button" onClick=${() => onPage("journal")}>Journal</button>
            <button class="item" type="button" onClick=${() => onPage("usage")}>Usage</button>
            <button class="item danger" type="button" onClick=${logout}>Sign out</button>
        <//>
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
