// The subscription limits on a profile page.

import { useEffect, useRef, useState } from "preact/hooks";

import { html } from "../../html.js";
import { alarmTime, share } from "../../format.js";
import { pageNames, pickedPage } from "./pages.js";
import { contourOf } from "../../contour.js";
import { Icon } from "../../ui/icons.js";
import { Popover } from "../../ui/popover.js";
import { Sheet } from "../../ui/sheet.js";
import { NewSessionList } from "./new.js";

const LIMITS_STALE_SEC = 900;
const LIMIT_ICONS = new Map([["claude", Icon.claude], ["codex", Icon.openai], ["gemini", Icon.gemini]]);

// agoText renders how long ago the limits snapshot was taken.
export function agoText(sec) {
    if (typeof sec !== "number") return "at an unknown time";
    if (sec < 3600) return `${Math.max(1, Math.round(sec / 60))} min ago`;
    if (sec < 86400) return `${Math.round(sec / 3600)} h ago`;
    return `${Math.round(sec / 86400)} d ago`;
}

export function staleLimits(c) {
    return !c || typeof c.ageSec !== "number" || c.ageSec >= LIMITS_STALE_SEC;
}

function limitClass(pct) {
    if (pct >= 85) return "crit";
    if (pct >= 60) return "warn";
    return "ok";
}

// windowTone is the tone of one window of a limit. The collector forecasts a
// window from its recent pace: "out" when it runs out before its reset, "tight"
// when it reaches the reset nearly full, "fine" otherwise. A window it has no
// pace for yet — a fresh window, a collector just started — falls back to its
// share. One rule for every place that colours a limit: a column, a chip and a
// ring that disagree about the same window would be worse than none.
export function windowTone(data) {
    const tone = data && data.forecast && data.forecast.tone;
    if (tone === "out") return "crit";
    if (tone === "tight") return "warn";
    if (tone === "fine") return "ok";
    return limitClass(data && typeof data.pct === "number" ? data.pct : 0);
}

// timeLeft is how long until a window starts over, "97 h" or "15 min", and
// null once its time has come.
function timeLeft(resetsAt) {
    const left = resetsAt * 1000 - Date.now();
    if (left <= 0) return null;
    const hours = Math.floor(left / 3600000);
    if (hours >= 1) return `${hours} h`;
    return `${Math.max(1, Math.round(left / 60000))} min`;
}

function resetText(resetsAt) {
    if (!resetsAt) return "";
    const left = timeLeft(resetsAt);
    return left ? `resets in ${left}` : "resets any moment";
}

export { contourOf };

// openLimits returns the limits of the contour the sessions page stands on,
// the two windows as bare shares with the tone of the five hours, or null when
// that contour has no snapshot of limits. A window the snapshot does not carry
// is null rather than a zero: a place that shows it leaves it out.
export function openLimits(snapshot) {
    const profiles = (snapshot && snapshot.profileMap) || [];
    const limits = (snapshot && snapshot.limits) || null;
    const name = pickedPage(pageNames(profiles, limits));
    const c = contourOf(limits, name, (profiles.find((p) => p.profile === name) || {}).id || 0);
    if (!c) return null;
    const part = (data) => (data && typeof data.pct === "number" ? data.pct : null);
    const five = part(c.fiveHour);
    return {
        contour: name,
        five,
        week: part(c.sevenDay),
        tone: five === null ? "" : windowTone(c.fiveHour),
        stale: staleLimits(c),
    };
}

// Claude's snapshot is the contour itself; other providers carry the same
// windows in nested records. The views consume one list for both shapes.
export function limitGroups(contour, stale = false) {
    if (!contour) return [];
    const sources = [["claude", contour], ...Object.entries(contour).filter(([, source]) =>
        source && typeof source === "object" && (source.fiveHour || source.sevenDay || source.day))];
    return sources.map(([agent, source]) => ({
        agent, old: stale || staleLimits(source), ageSec: source.ageSec, reached: source.reached,
        windows: [
            { span: "5h", full: `${agent[0].toUpperCase() + agent.slice(1)}, 5 hours`, data: source.fiveHour },
            { span: "7d", full: `${agent[0].toUpperCase() + agent.slice(1)}, 7 days`, data: source.sevenDay },
            { span: "day", full: `${agent[0].toUpperCase() + agent.slice(1)}, day`, data: source.day },
        ].filter((window) => window.data),
    })).filter((group) => group.windows.length > 0);
}

export function LimitNotes({ contour, groups, stale, compact }) {
    const codex = contour && contour.codex;
    const ages = codex && !codex.sevenDay && !groups.some((group) => group.agent === "codex")
        ? [...groups, { agent: "codex", old: staleLimits(codex), ageSec: codex.ageSec }]
        : groups;
    const renewal = {
        claude: ": they are renewed when a claude session of the contour answers",
        codex: ": they are renewed every five minutes while the codex daemon of the contour runs",
    };
    return html`
        ${codex && !codex.sevenDay && html`<p class="limits-note calm">${CODEX_NO_WEEK}</p>`}
        ${codex && codex.reached && html`<p class="limits-note">${CODEX_REACHED}</p>`}
        ${stale && html`<p class="limits-note">the limits are the last known ones, the agent is silent</p>`}
        ${!stale && ages.filter((group) => group.old && (!compact || !groups.includes(group))).map((group) => html`
            <p class="limits-note calm" key=${group.agent}>${compact ? `${group.agent}: ${agoText(group.ageSec)}` : `${group.agent}'s numbers are from ${agoText(group.ageSec)}${renewal[group.agent] || ""}`}</p>
        `)}
    `;
}

// Full groups carry their vendor on the frame. Past the row, the compact
// readings keep that meaning in their accessible labels and leave the actions
// available without taking the list's room for the full columns.
export function ProfileLimits({ limits, profile, contour, stale, entry, snapshot, exec, onTasks, onSettings }) {
    const [newSession, setNewSession] = useState(false);
    const [opened, setOpened] = useState(null);
    const box = useRef(null);
    const { tucked, stuck } = useTucked(box, profile);
    // Folding replaces the controls, so a bubble cannot keep its old anchor.
    useEffect(() => setOpened(null), [tucked, profile]);
    const toggleLimit = (key, event) => {
        const reading = event.currentTarget.getBoundingClientRect();
        const row = event.currentTarget.closest(".pflimitrow").getBoundingClientRect();
        setOpened((current) => current && current.key === key ? null : { key, x: reading.left + reading.width / 2 - row.left });
    };
    const closeLimit = () => setOpened(null);
    const c = contourOf(limits, profile, contour);
    const groups = limitGroups(c, stale);
    return html`
        <div class=${`pflimits${tucked ? " tucked" : ""}${stuck ? " stuck" : ""}`} ref=${box}>
            <div class="pflimitrow" role="group" aria-label="Contour limits and actions">
                ${groups.map((group) => html`<${LimitGroup} key=${group.agent} group=${group} tucked=${tucked}
                    opened=${opened} onToggle=${toggleLimit} onClose=${closeLimit} />`)}
                <div class="pfactions" role="group" aria-label=${`${profile} actions`}>
                    <button class="pfaction" type="button" aria-label="Tasks" title="Tasks" disabled=${!contour}
                            onClick=${onTasks}>${Icon.checklist()}</button>
                    <button class="pfaction" type="button" aria-label="Profile" title="Profile" disabled=${!contour}
                            onClick=${() => onSettings(contour)}>${Icon.badge()}</button>
                    <button class="pfaction" type="button" aria-label="New session" title="New session" disabled=${!entry}
                            onClick=${() => setNewSession(true)}>${Icon.plus()}</button>
                </div>
            </div>
            ${!c && html`<p class="limits-note calm">nobody has worked in this profile yet</p>`}
            <${LimitNotes} contour=${c} groups=${groups} stale=${stale} />
        </div>
        <${Sheet} open=${newSession} onClose=${() => setNewSession(false)} label="New session">
            <div class="shead"><span class="stitle">A new session in ${profile}</span></div>
            <${NewSessionList} entry=${entry} snapshot=${snapshot} exec=${exec} onClose=${() => setNewSession(false)} />
        <//>
    `;
}

function ringLeft(resetsAt) {
    if (!resetsAt) return "";
    const left = resetsAt * 1000 - Date.now();
    if (left <= 0) return "0m";
    if (left >= 86400000) return `${Math.floor(left / 86400000)}d`;
    if (left >= 3600000) return `${Math.floor(left / 3600000)}h`;
    return `${Math.max(1, Math.round(left / 60000))}m`;
}

function forecastText(data) {
    const forecast = data.forecast;
    if (!forecast) return "";
    if (data.pct >= 100) return "limit reached";
    if (forecast.runsOutAt) {
        const minutes = Math.max(1, Math.round((forecast.runsOutAt * 1000 - Date.now()) / 60000));
        const hours = Math.floor(minutes / 60);
        const rest = minutes % 60;
        const duration = [hours && `${hours} h`, rest && `${rest} min`].filter(Boolean).join(" ");
        return `runs out in ~${duration}`;
    }
    if (forecast.tone === "tight") return `tight: ~${Math.round(forecast.pct)}% at reset`;
    return "lasts until reset";
}

function limitShares(data) {
    const value = Math.max(0, Number(data.pct) || 0);
    const pct = Math.min(100, value);
    const whole = value < 100 ? Math.min(99, Math.round(value)) : Math.round(value);
    const forecast = data.forecast && Number(data.forecast.pct);
    const fore = Number.isFinite(forecast) ? Math.max(pct, Math.min(100, forecast)) : pct;
    return { whole, style: `--share:${pct};--forecast:${fore}` };
}

function ForecastBar({ data }) {
    return html`<span class="limit-bar" aria-hidden="true" style=${limitShares(data).style}>
        <span class="limit-used"></span>
        ${data.forecast && html`<span class="limit-ghost"></span>`}
    </span>`;
}

export function LimitGroup({ group, tucked, chip, opened, onToggle, onClose }) {
    const VendorIcon = LIMIT_ICONS.get(group.agent) || Icon.limit;
    if (chip) {
        const windows = group.windows.map((window) => {
            const key = `${group.agent}:${window.span}`;
            return html`<${LimitWindow} ...${window} key=${window.span} group=${group}
                opened=${opened && opened.key === key ? opened : null}
                onToggle=${(event) => onToggle(key, event)} onClose=${onClose} />`;
        });
        return html`<div class=${`limit-group limit-chip-group${group.old ? " old" : ""}`} data-agent=${group.agent}
                        role="group" aria-label=${`${group.agent} limits`} style=${`--limit-windows:${group.windows.length}`}>
            <span class="limit-vendor agentword" data-agent=${group.agent} aria-hidden="true"><${VendorIcon} /></span>
            ${windows}
        </div>`;
    }
    return html`<fieldset class=${`limit-group limit-columns${tucked ? " compact" : ""}`} data-agent=${group.agent} aria-label=${`${group.agent} limits`}>
        <legend><span class="limit-vendor agentword" data-agent=${group.agent} aria-hidden="true"><${VendorIcon} /></span></legend>
        <div class="limit-accounts">
            ${group.windows.map((window, i) => {
                const key = `${group.agent}:${window.span}`;
                return html`<${LimitColumn} ...${window} key=${window.span} group=${group} tucked=${tucked}
                    clock=${group.old && i === group.windows.length - 1} opened=${opened && opened.key === key ? opened : null}
                    onToggle=${(event) => onToggle(key, event)} onClose=${onClose} />`;
            })}
        </div>
    </fieldset>`;
}

// A column is one window of an account, and its own gauge: the share fills it
// from the bottom, the forecast to the reset is hatched over the share, and
// the share in words and the time to the reset stand over both, as on a chip
// at a desk; the span is in its label and in the bubble. Every window stands
// as a column of its own, so the five hours stay in sight beside the week.
// Folded over the list, a column keeps its share alone.
function LimitColumn({ group, full, data, tucked, clock, opened, onToggle, onClose }) {
    const { whole, style } = limitShares(data);
    const label = [full, share(data.pct), resetText(data.resetsAt), forecastText(data), group.old && "last known numbers"].filter(Boolean).join(", ");
    return html`<span class="limit-anchor" style=${opened ? `--limit-anchor:${opened.x}px` : undefined}>
        <button class=${`limit-reading limit-column${tucked ? " limit-mini" : ""} ${windowTone(data)}${data.forecast ? " forecast" : ""}${group.old ? " old" : ""}`}
                    data-agent=${group.agent} type="button" aria-label=${label} title=${label} style=${style}
                    aria-expanded=${Boolean(opened)} aria-haspopup="dialog" onClick=${onToggle}>
            ${!tucked && data.forecast && data.forecast.pct > 100 && html`<span class="limit-cap" aria-hidden="true">▲</span>`}
            <span class="limit-percent">${whole}<small>%</small></span>
            ${!tucked && html`<span class="limit-window limit-left">${ringLeft(data.resetsAt)}</span>`}
            ${clock && html`<span class="limit-old" aria-hidden="true">${Icon.clock()}</span>`}
        </button>
        <${LimitPopover} group=${group} opened=${opened} onClose=${onClose} />
    </span>`;
}

function LimitWindow({ group, span, full, data, opened, onToggle, onClose }) {
    const { whole } = limitShares(data);
    const pct = Math.min(100, Math.max(0, Number(data.pct) || 0));
    const label = [full, share(data.pct), resetText(data.resetsAt), group.old && "last known numbers"].filter(Boolean).join(", ");
    return html`<span class="limit-anchor" style=${opened ? `--limit-anchor:${opened.x}px` : undefined}>
        <button class=${`limit-reading limit-chip ${windowTone(data)}`}
                    data-agent=${group.agent} type="button" aria-label=${label} title=${label} style=${`--share:${pct}`}
                    aria-expanded=${Boolean(opened)} aria-haspopup="dialog" onClick=${onToggle}>
            <span class="limit-percent">${whole}<small>%</small></span>
            <span class="limit-window limit-left">${ringLeft(data.resetsAt)}</span>
        </button>
        <${LimitPopover} group=${group} opened=${opened} onClose=${onClose} />
    </span>`;
}

function LimitPopover({ group, opened, onClose }) {
    const name = `${group.agent[0].toUpperCase() + group.agent.slice(1)} limits`;
    return html`<${Popover} open=${Boolean(opened)} onClose=${onClose} label=${name} className="limit-popover">
        <strong>${name}</strong>
        ${group.windows.map(({ span, full, data }) => html`<div class=${`limit-detail ${windowTone(data)}`} key=${span}>
            <div class="limit-detail-reading" aria-label=${full}>
                <span class="limit-window">${span}</span><${ForecastBar} data=${data} /><span class="limit-detail-percent">${share(data.pct)} used</span>
            </div>
            ${data.forecast && html`<span class="limit-forecast">${forecastText(data)}</span>`}
            ${Boolean(data.resetsAt) && html`<span class="limit-reset"><time dateTime=${new Date(data.resetsAt * 1000).toISOString()}>${alarmTime(data.resetsAt * 1000, true)}</time> · ${resetText(data.resetsAt)}</span>`}
        </div>`)}
        <span>Updated ${agoText(group.ageSec)}</span>
        ${group.old && html`<span>last known numbers</span>`}
        ${group.agent === "codex" && group.reached && html`<span>${CODEX_REACHED}</span>`}
    <//>`;
}

const folded = new Map();

// Hysteresis keeps shrinking the row from opening it again through scroll
// anchoring. A restored list starts folded as it was left, before its first
// paint can move the card the person came back to.
function useTucked(ref, profile) {
    const [state, setState] = useState(() => ({ tucked: folded.get(profile) || false, stuck: folded.get(profile) || false }));
    useEffect(() => {
        const el = ref.current;
        const list = el && el.closest(".pfpage, .body");
        if (!list) return undefined;
        let now = folded.get(profile) || false;
        let stuck = now;
        const onScroll = () => {
            const scrolled = list.scrollTop > 0;
            const next = scrolled && (now || el.getBoundingClientRect().bottom <= list.getBoundingClientRect().top);
            if (next === now && scrolled === stuck) return;
            now = next;
            stuck = scrolled;
            folded.set(profile, next);
            setState({ tucked: next, stuck: scrolled });
        };
        list.addEventListener("scroll", onScroll, { passive: true });
        onScroll();
        return () => list.removeEventListener("scroll", onScroll);
    }, [profile]);
    return state;
}

// What codex has not told, or has run into, in the same words wherever its
// week is shown.
export const CODEX_NO_WEEK = "codex has told no weekly window yet";
export const CODEX_REACHED = "codex says the limit is reached";
const CODEX_NO_WEEK_SHORT = "no week yet";

// CodexWeek is the weekly window of a codex account, the one window of
// codex's the panel shows: a window as claude's are, headed by name and a
// plate under span as Limit is, or, while the account has told no week, a
// line saying so in place of an empty bar, headed by head where one is
// given. A short line, a third of a phone wide, says it in three words where
// a plate has its last line and carries the whole in its title.
export function CodexWeek({ codex, name, head, span, full, short }) {
    if (codex.sevenDay) return html`<${Limit} name=${name} data=${codex.sevenDay} agent="codex" span=${span} full=${full} />`;
    return html`
        <div class="limit lempty" data-agent="codex" title=${short ? CODEX_NO_WEEK : undefined}>
            ${head && html`<div class="lrow"><span class="lname">${head}</span></div>`}
            <span class="lsub">${short ? CODEX_NO_WEEK_SHORT : CODEX_NO_WEEK}</span>
        </div>
    `;
}

// Limit is one window of a limit: its share, a bar and when it starts over.
// Agent marks a window that is not claude's. A window given a span is a
// plate, as narrow as a third of a phone: name and the share over the bar,
// the span of the window and the time to its reset without the word under
// it, and the whole window in its title, under full.
export function Limit({ name, data, agent, span, full }) {
    if (!data) return null;
    const kind = windowTone(data);
    const reset = resetText(data.resetsAt);
    const title = span ? [`${full}: ${share(data.pct)}`, reset].filter(Boolean).join(", ") : undefined;
    return html`
        <div class="limit" data-agent=${agent} title=${title}>
            <div class="lrow">
                <span class="lname">${name}</span>
                <span class="lval ${kind}">${share(data.pct)}</span>
            </div>
            <div class="track"><div class="fill ${kind}" style=${`width:${Math.min(100, data.pct)}%`}></div></div>
            ${span
                ? html`
                    <div class="lrow">
                        <span class="lname">${span}</span>
                        <span class="lsub">${data.resetsAt ? timeLeft(data.resetsAt) || "any moment" : ""}</span>
                    </div>
                `
                : html`<span class="lsub">${reset}</span>`}
        </div>
    `;
}
