// Times of a conversation as the session cards show them.

import { ago } from "../../format.js";

// stamp returns a transcript timestamp in seconds of the epoch.
export function stamp(iso) {
    if (!iso) return 0;
    const ms = Date.parse(iso);
    return Number.isFinite(ms) ? Math.round(ms / 1000) : 0;
}

// when renders a timestamp from the history as how long ago it was, in the
// words the rest of the interface uses: a date spelled by the locale of the
// browser reads in another language than the screen around it.
export function when(sec) {
    if (!sec) return "—";
    return ago(new Date(sec * 1000).toISOString());
}
