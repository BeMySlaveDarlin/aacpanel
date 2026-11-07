// The subscription limits on a profile page.

import { useState } from "preact/hooks";

import { html } from "../../html.js";
import { share } from "../../format.js";
import { pageNames, pickedPage } from "./pages.js";
import { contourOf } from "../../contour.js";
import { Icon } from "../../ui/icons.js";
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

// The compact row stays at one height while the list scrolls. Folding it
// would hide its controls or change the target under the person's finger.
export function ProfileLimits({ limits, profile, contour, stale, entry, snapshot, exec, onTasks, onSettings }) {
    const [newSession, setNewSession] = useState(false);
    const c = contourOf(limits, profile, contour);
    const codex = c && c.codex;
    const claudeOld = Boolean(c) && staleLimits(c);
    const codexOld = Boolean(codex) && staleLimits(codex);
    const ages = stale ? [] : [
        c && (c.fiveHour || c.sevenDay) && claudeOld && `claude's numbers are from ${agoText(c.ageSec)}: they are renewed when a claude session of the contour answers`,
        codexOld && `codex's numbers are from ${agoText(codex.ageSec)}: they are renewed every five minutes while the codex daemon of the contour runs`,
    ].filter(Boolean);
    return html`
        <div class="pflimits">
            <div class="pflimitrow" role="group" aria-label="Contour limits and actions">
                ${c && html`
                    <${LimitRing} agent="claude" span="5h" full="Claude, 5 hours" data=${c.fiveHour} old=${stale || claudeOld} />
                    <${LimitRing} agent="claude" span="7d" full="Claude, 7 days" data=${c.sevenDay} old=${stale || claudeOld} />
                    <${LimitRing} agent="codex" span="7d" full="Codex, 7 days" data=${codex && codex.sevenDay} old=${stale || codexOld} />
                `}
                <div class="pfactions">
                    <button class="pfaction" type="button" aria-label="Tasks" disabled=${!contour}
                            onClick=${onTasks}>${Icon.plan()}</button>
                    <button class="pfaction" type="button" aria-label="Profile" disabled=${!contour}
                            onClick=${() => onSettings(contour)}>${Icon.cube()}</button>
                    <button class="pfaction" type="button" aria-label="New session" disabled=${!entry}
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

function LimitRing({ agent, span, full, data, old }) {
    if (!data) return null;
    const label = [full, share(data.pct), resetText(data.resetsAt), old && "last known numbers"].filter(Boolean).join(", ");
    return html`
        <div class=${`limit-ring ${limitClass(data.pct)}${old ? " old" : ""}`} data-agent=${agent} role="img" aria-label=${label} title=${label}>
            <span class="agentword" data-agent=${agent}>${agent}</span>
            <span class="limit-dial" style=${`--share:${Math.max(0, Math.min(100, data.pct || 0))}`}>
                <span>${ringLeft(data.resetsAt)}${data.resetsAt ? "/" : ""}${span}</span>
            </span>
        </div>
    `;
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
