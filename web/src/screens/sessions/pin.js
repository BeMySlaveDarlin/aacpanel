// A pin belongs to the live root: pressing it on a child changes the whole
// group, including children opened after the press.
import { kinOf } from "./kin.js";

export function pinOf(s, live = []) {
    const root = kinOf(live).rootOf(s);
    const on = Boolean(root.pinnedAt);
    return {
        root, on,
        label: `${on ? "Unpin" : "Pin"} ${root === s ? "the group" : `the group of ${root.session}`}`,
        tip: "Keep this session and its children first in their contour, on every device",
    };
}

// Unpinned rows keep the order the caller gave them. The full stamp is
// compared as text: Date.parse would collapse pins made in one millisecond.
export function comparePins(a, b) {
    if (!a) return b ? 1 : 0;
    if (!b) return -1;
    return a.localeCompare(b);
}
