// A pin belongs to the live root: pressing it on a child changes the whole
// group, including children opened after the press.
import { kinOf } from "./kin.js";

// pinAt gives a root its expected place while the snapshot catches up. A new
// pin uses the press's time so it follows the pins already on the screen.
export function pinAt(s, wait) {
    const task = wait ? wait.of("pin", s.session) : null;
    if (!task) return s.pinnedAt || "";
    return task.params.on ? s.pinnedAt || new Date(task.since).toISOString() : "";
}

// pinOf gives every row of a group the root's pin and its pending state.
export function pinOf(s, live = [], wait = null) {
    const root = kinOf(live).rootOf(s);
    const on = Boolean(pinAt(root, wait));
    const pending = Boolean(wait && wait.of("pin", root.session));
    return {
        root, on, pending,
        why: pending ? `${on ? "Pinning" : "Unpinning"} the group` : "",
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
