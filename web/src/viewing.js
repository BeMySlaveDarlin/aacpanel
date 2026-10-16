// Which session's question is on screen right now, and when this device last
// had the feed of each session on screen.
import { useEffect } from "preact/hooks";

let seen = "";
const looked = new Map();
let told = null;

export function viewing() {
    return seen;
}

// tellViewing names what to call when a feed comes on screen. The host hears
// what a device is looking at with the next poll, and a glance shorter than
// the gap between polls would otherwise never reach it.
export function tellViewing(fn) {
    told = fn;
}

// seenAt is when this device last had the feed of the session on screen, in
// milliseconds: now while it is there, 0 when it never was.
export function seenAt(name) {
    if (!name) return 0;
    if (name === seen) return Date.now();
    return looked.get(name) || 0;
}

// calledMark is the time of the call a row of the lists marks its session
// with, or "" for no mark. The host marks a session until it hears that a
// feed of it was open; the device that opened it knows that before the next
// poll says so, and a call made after the visit marks the session again.
export function calledMark(s) {
    const at = s && s.called ? Date.parse(s.called) : NaN;
    if (!Number.isFinite(at)) return "";
    return seenAt(s.session) >= at ? "" : s.called;
}

export function useViewing(name) {
    useEffect(() => {
        seen = name || "";
        if (name) {
            looked.set(name, Date.now());
            if (told) told();
        }
        return () => {
            if (name) looked.set(name, Date.now());
            if (seen === name) seen = "";
        };
    }, [name]);
}
