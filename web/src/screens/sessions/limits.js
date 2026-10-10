// The subscription limits on a profile page.

import { html } from "../../html.js";
import { share } from "../../format.js";
import { pageNames, pickedPage } from "./pages.js";

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

function resetText(resetsAt) {
    if (!resetsAt) return "";
    const left = resetsAt * 1000 - Date.now();
    if (left <= 0) return "resets any moment";
    const hours = Math.floor(left / 3600000);
    if (hours >= 1) return `resets in ${hours} h`;
    return `resets in ${Math.max(1, Math.round(left / 60000))} min`;
}

// contourOf returns the limits snapshot of this contour, or null if there is none.
export function contourOf(limits, profile, contour) {
    if (!limits) return null;
    const known = limits.contours && limits.contours.length ? limits.contours : [limits];
    if (!profile || (known.length === 1 && !known[0].profile)) return known[0];
    if (contour) {
        const own = known.find((c) => c.contour === contour);
        if (own) return own;
    }
    return known.find((c) => c.profile === profile) || null;
}

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

// ProfileLimits renders the subscription bars of this contour and their age:
// claude's two windows, and on a row under them codex's week where codex has
// spent in the contour; a contour codex alone has spent in has codex's row
// alone. Each agent renews its own numbers, so numbers one of them has left
// old are dimmed apart from the other's, and the whole block only once all of
// them are old or the agent that brings them is silent.
export function ProfileLimits({ limits, profile, contour, stale }) {
    const c = contourOf(limits, profile, contour);

    if (!c) {
        return html`
            <div class="pflimits none">
                <span class="limitnone">nobody has worked in this profile yet</span>
            </div>
        `;
    }

    const codex = c.codex || null;
    const claude = Boolean(c.fiveHour || c.sevenDay) || !codex;
    const claudeOld = claude && staleLimits(c);
    const codexOld = Boolean(codex) && staleLimits(codex);
    const allOld = stale || ((!claude || claudeOld) && (!codex || codexOld));
    const ages = stale ? [] : [
        claudeOld && (codex
            ? `claude's numbers are from ${agoText(c.ageSec)}: they are renewed when a claude session of the contour answers`
            : `the numbers are from ${agoText(c.ageSec)}: they are renewed when a session of the contour answers`),
        codexOld && `codex's numbers are from ${agoText(codex.ageSec)}: they are renewed while the panel holds a thread of the contour`,
    ].filter(Boolean);
    const word = html`<span class="agentword" data-agent="codex">Codex</span>`;

    return html`
        <div class=${`pflimits${allOld ? " stale" : ""}`}>
            ${claude && html`
                <div class=${`limits${claudeOld && !allOld ? " pfold" : ""}`}>
                    <${Limit} name="5 hours" data=${c.fiveHour} />
                    <${Limit} name="7 days" data=${c.sevenDay} />
                </div>
            `}
            ${codex && html`
                <div class=${`limits pfcodex${codexOld && !allOld ? " pfold" : ""}`}>
                    <${CodexWeek} codex=${codex} name=${html`${word} · 7 days`} head=${word} />
                </div>
            `}
            ${codex && codex.reached && html`<p class="limits-note">${CODEX_REACHED}</p>`}
            ${stale && html`<p class="limits-note">the limits are the last known ones, the agent is silent</p>`}
            ${ages.map((line) => html`<p class="limits-note calm" key=${line}>${line}</p>`)}
        </div>
    `;
}

// What codex has not told, or has run into, in the same words wherever its
// week is shown.
export const CODEX_NO_WEEK = "codex has told no weekly window yet";
export const CODEX_REACHED = "codex says the limit is reached";

// CodexWeek is the weekly window of a codex account, the one window of
// codex's the panel shows: a window as claude's are, headed by name, or, while
// the account has told no week, a line saying so in place of an empty bar,
// headed by head where one is given.
export function CodexWeek({ codex, name, head }) {
    if (codex.sevenDay) return html`<${Limit} name=${name} data=${codex.sevenDay} agent="codex" />`;
    return html`
        <div class="limit lempty" data-agent="codex">
            ${head && html`<div class="lrow"><span class="lname">${head}</span></div>`}
            <span class="lsub">${CODEX_NO_WEEK}</span>
        </div>
    `;
}

// Limit is one window of a limit: its share, a bar and when it starts over.
// Agent marks a window that is not claude's.
export function Limit({ name, data, agent }) {
    if (!data) return null;
    const kind = limitClass(data.pct);
    return html`
        <div class="limit" data-agent=${agent}>
            <div class="lrow">
                <span class="lname">${name}</span>
                <span class="lval ${kind}">${share(data.pct)}</span>
            </div>
            <div class="track"><div class="fill ${kind}" style=${`width:${Math.min(100, data.pct)}%`}></div></div>
            <span class="lsub">${resetText(data.resetsAt)}</span>
        </div>
    `;
}
