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
        tone: five === null ? "" : limitClass(five),
        stale: staleLimits(c),
    };
}

// Full groups carry their vendor on the frame. Past the row, the compact
// readings keep that meaning in their accessible labels and leave the actions
// available without taking the list's room for the full rings.
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
    const codex = c && c.codex;
    const claudeOld = Boolean(c) && staleLimits(c);
    const codexOld = Boolean(codex) && staleLimits(codex);
    const ages = stale ? [] : [
        c && (c.fiveHour || c.sevenDay) && claudeOld && `claude's numbers are from ${agoText(c.ageSec)}: they are renewed when a claude session of the contour answers`,
        codexOld && `codex's numbers are from ${agoText(codex.ageSec)}: they are renewed every five minutes while the codex daemon of the contour runs`,
    ].filter(Boolean);
    const group = (agent, old, source, windows) => ({ agent, old, ageSec: source.ageSec, reached: source.reached, windows: windows.filter((window) => window.data) });
    const groups = [
        c && group("claude", stale || claudeOld, c, [
            { span: "5h", full: "Claude, 5 hours", data: c.fiveHour },
            { span: "7d", full: "Claude, 7 days", data: c.sevenDay },
        ]),
        codex && group("codex", stale || codexOld, codex, [{ span: "7d", full: "Codex, 7 days", data: codex.sevenDay }]),
    ].filter((group) => group && group.windows.length > 0);
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
            ${codex && !codex.sevenDay && html`<p class="limits-note calm">${CODEX_NO_WEEK}</p>`}
            ${codex && codex.reached && html`<p class="limits-note">${CODEX_REACHED}</p>`}
            ${stale && html`<p class="limits-note">the limits are the last known ones, the agent is silent</p>`}
            ${ages.map((line) => html`<p class="limits-note calm" key=${line}>${line}</p>`)}
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

function LimitGroup({ group, tucked, opened, onToggle, onClose }) {
    const windows = group.windows.map((window) => {
        const key = `${group.agent}:${window.span}`;
        return html`<${LimitWindow} ...${window} key=${window.span} agent=${group.agent} old=${group.old}
            ageSec=${group.ageSec} reached=${group.reached} tucked=${tucked} opened=${opened && opened.key === key ? opened : null}
            onToggle=${(event) => onToggle(key, event)} onClose=${onClose} />`;
    });
    if (tucked) return html`<div class="limit-group compact" data-agent=${group.agent}>${windows}</div>`;
    return html`<fieldset class="limit-group" data-agent=${group.agent}>
        <legend><span class="agentword" data-agent=${group.agent}>${group.agent}</span></legend>
        <div class="limit-windows">${windows}</div>
    </fieldset>`;
}

function LimitWindow({ agent, span, full, data, old, ageSec, reached, tucked, opened, onToggle, onClose }) {
    const value = Math.max(0, Number(data.pct) || 0);
    const pct = Math.min(100, value);
    const whole = value < 100 ? Math.min(99, Math.round(value)) : Math.round(value);
    const known = old && "last known numbers";
    const label = [full, share(data.pct), resetText(data.resetsAt), known].filter(Boolean).join(", ");
    const reading = html`<span class="limit-percent">${whole}<small>%</small></span>`;
    return html`<span class="limit-anchor" style=${opened ? `--limit-anchor:${opened.x}px` : undefined}>
        <button class=${`limit-reading ${tucked ? "limit-mini" : "limit-ring"} ${limitClass(data.pct)}${old ? " old" : ""}`}
                    data-agent=${agent} type="button" aria-label=${label} title=${label} style=${`--share:${pct}`}
                    aria-expanded=${Boolean(opened)} aria-haspopup="dialog" onClick=${onToggle}>
            ${tucked ? html`
                ${reading}
                <span class="limit-minitrack" aria-hidden="true"></span>
            ` : html`
                <span class="limit-gauge" aria-hidden="true"></span>
                <span class="limit-dial">
                    ${reading}
                    <span class="limit-window">${ringLeft(data.resetsAt)}${data.resetsAt ? "/" : ""}${span}</span>
                </span>
            `}
            ${old && html`<span class="limit-old" aria-hidden="true">${Icon.clock()}</span>`}
        </button>
        <${Popover} open=${Boolean(opened)} onClose=${onClose} label=${full} className="limit-popover">
            <strong>${full}</strong>
            <span>${share(data.pct)} used</span>
            ${Boolean(data.resetsAt) && html`<span><time dateTime=${new Date(data.resetsAt * 1000).toISOString()}>${alarmTime(data.resetsAt * 1000, true)}</time> · ${resetText(data.resetsAt)}</span>`}
            <span>Updated ${agoText(ageSec)}</span>
            ${known && html`<span>${known}</span>`}
            ${agent === "codex" && reached && html`<span>${CODEX_REACHED}</span>`}
        <//>
    </span>`;
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
    const kind = limitClass(data.pct);
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
