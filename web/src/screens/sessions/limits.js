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

// ProfileLimits renders the two subscription bars of this contour and their age.
export function ProfileLimits({ limits, profile, contour, stale }) {
    const c = contourOf(limits, profile, contour);

    if (!c) {
        return html`
            <div class="pflimits none">
                <span class="limitnone">nobody has worked in this profile yet</span>
            </div>
        `;
    }

    return html`
        <div class=${`pflimits${stale || staleLimits(c) ? " stale" : ""}`}>
            <div class="limits">
                <${Limit} name="5 hours" data=${c.fiveHour} />
                <${Limit} name="7 days" data=${c.sevenDay} />
            </div>
            ${stale
                ? html`<p class="limits-note">the limits are the last known ones, the agent is silent</p>`
                : staleLimits(c) && html`
                    <p class="limits-note calm">the numbers are from ${agoText(c.ageSec)}: they are renewed when a session of the contour answers</p>
                `}
        </div>
    `;
}

function Limit({ name, data }) {
    if (!data) return null;
    const kind = limitClass(data.pct);
    return html`
        <div class="limit">
            <div class="lrow">
                <span class="lname">${name}</span>
                <span class="lval ${kind}">${share(data.pct)}</span>
            </div>
            <div class="track"><div class="fill ${kind}" style=${`width:${Math.min(100, data.pct)}%`}></div></div>
            <span class="lsub">${resetText(data.resetsAt)}</span>
        </div>
    `;
}
