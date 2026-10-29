// Claude contours and their subscription limits.

import { alarmTime } from "./format.js";

export const PERSONAL = "personal";

export function contourName(profile) {
    return profile || PERSONAL;
}

// contourOf returns the limits snapshot of this contour, or null if there is none.
export function contourOf(limits, profile, contour) {
    if (!limits) return null;
    const known = limits.contours && limits.contours.length ? limits.contours : [limits];
    if (contour) {
        const own = known.find((c) => c.contour === contour);
        if (own) return own;
    }
    if (!profile || (known.length === 1 && !known[0].profile)) return known[0];
    return known.find((c) => c.profile === profile) || null;
}

// A reset frees the account, but does not resume a conversation that stopped
// at the limit. Missing limits cannot prove a reset and keep the warning.
export function limitState(row, limits) {
    if (!row.limitHit) return null;
    const hit = row.limitHit;
    const c = contourOf(limits, row.profile, row.contour);
    const keys = hit.window === "5h" ? ["fiveHour"] : hit.window === "7d" ? ["sevenDay"] : ["fiveHour", "sevenDay"];
    const windows = keys.map((key) => c && c[key]).filter(Boolean);
    const now = Date.now() / 1000;
    const fresh = c && c.at * 1000 > Date.parse(hit.at);
    const reset = (w) => (w.resetsAt > 0 && w.resetsAt <= now)
        || (w.pct === 0 && !w.resetsAt) || (fresh && w.pct < 100);
    const pending = windows.filter((w) => !reset(w));
    const reached = pending.filter((w) => w.pct >= 100);
    const active = hit.window ? pending : reached;
    if ((windows.length > 0 && pending.length === 0) || (!hit.window && reached.length === 0 && windows.some(reset))) {
        return { tone: "wait", text: "stopped by the limit" };
    }
    const until = Math.max(0, ...active.map((w) => w.resetsAt || 0));
    return { tone: "limit", text: until > now ? `limit · until ${alarmTime(until * 1000)}` : "limit" };
}
