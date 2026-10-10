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

// resetLeft is how long until a window starts over: "in 97 h", "in 15 min",
// or "any moment" once its time has come.
function resetLeft(resetsAt) {
    const left = resetsAt * 1000 - Date.now();
    if (left <= 0) return "any moment";
    const hours = Math.floor(left / 3600000);
    if (hours >= 1) return `in ${hours} h`;
    return `in ${Math.max(1, Math.round(left / 60000))} min`;
}

function resetText(resetsAt) {
    return resetsAt ? `resets ${resetLeft(resetsAt)}` : "";
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
// claude's two windows, and beside them, a third in the row, codex's week
// where codex has spent in the contour; three to a row, the windows are
// short. A contour codex alone has spent in has codex's window alone. Each
// agent renews its own numbers, so numbers one of them has left old are
// dimmed apart from the other's, and the whole block only once all of them
// are old or the agent that brings them is silent.
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
    const three = claude && Boolean(codex);
    const allOld = stale || ((!claude || claudeOld) && (!codex || codexOld));
    const ages = stale ? [] : [
        claudeOld && (codex
            ? `claude's numbers are from ${agoText(c.ageSec)}: they are renewed when a claude session of the contour answers`
            : `the numbers are from ${agoText(c.ageSec)}: they are renewed when a session of the contour answers`),
        codexOld && `codex's numbers are from ${agoText(codex.ageSec)}: they are renewed while the panel holds a thread of the contour`,
    ].filter(Boolean);
    const word = html`<span class="agentword" data-agent="codex">Codex</span>`;

    return html`
        <div class=${`pflimits${three ? " pfthree" : ""}${allOld ? " stale" : ""}`}>
            ${claude && html`
                <div class=${`limits pfclaude${claudeOld && !allOld ? " pfold" : ""}`}>
                    <${Limit} name="5 hours" data=${c.fiveHour} short=${three} />
                    <${Limit} name="7 days" data=${c.sevenDay} short=${three} />
                </div>
            `}
            ${codex && html`
                <div class=${`limits pfcodex${codexOld && !allOld ? " pfold" : ""}`}>
                    <${CodexWeek} codex=${codex} name=${three ? word : html`${word} · 7 days`} full="Codex · 7 days"
                                  head=${word} short=${three} />
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
const CODEX_NO_WEEK_SHORT = "no week yet";

// CodexWeek is the weekly window of a codex account, the one window of
// codex's the panel shows: a window as claude's are, headed by name, or, while
// the account has told no week, a line saying so in place of an empty bar,
// headed by head where one is given. A short window says it in three words
// and carries the whole line in its title, as Limit does.
export function CodexWeek({ codex, name, head, short, full }) {
    if (codex.sevenDay) return html`<${Limit} name=${name} data=${codex.sevenDay} agent="codex" short=${short} full=${full} />`;
    return html`
        <div class="limit lempty" data-agent="codex" title=${short ? CODEX_NO_WEEK : undefined}>
            ${head && html`<div class="lrow"><span class="lname">${head}</span></div>`}
            <span class="lsub">${short ? CODEX_NO_WEEK_SHORT : CODEX_NO_WEEK}</span>
        </div>
    `;
}

// Limit is one window of a limit: its share, a bar and when it starts over.
// Agent marks a window that is not claude's. A short window, a third of a
// phone wide, tells the time to its reset without the word and carries the
// whole window in its title, under full where name is not a plain word.
export function Limit({ name, data, agent, short, full }) {
    if (!data) return null;
    const kind = limitClass(data.pct);
    const reset = resetText(data.resetsAt);
    const title = short ? [`${full || name}: ${share(data.pct)}`, reset].filter(Boolean).join(", ") : undefined;
    return html`
        <div class="limit" data-agent=${agent} title=${title}>
            <div class="lrow">
                <span class="lname">${name}</span>
                <span class="lval ${kind}">${share(data.pct)}</span>
            </div>
            <div class="track"><div class="fill ${kind}" style=${`width:${Math.min(100, data.pct)}%`}></div></div>
            <span class="lsub">${short && data.resetsAt ? resetLeft(data.resetsAt) : reset}</span>
        </div>
    `;
}
