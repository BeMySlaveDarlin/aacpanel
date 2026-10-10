// The button on a push that turns its source off. It is pressed in the
// notification shade, where no page and no gate exist: the press is its own
// confirmation, and it is the one request the worker sends on its own.

import { LATE, within } from "./deadline.js";

// quietOf is the button that turns the source of a push off, as the panel
// described it, or nothing.
export function quietOf(payload) {
    const q = payload.quiet;
    if (!q || typeof q.what !== "string" || typeof q.key !== "string" || typeof q.label !== "string") return null;
    return { what: q.what, key: q.key, label: q.label };
}

// quiet asks the panel the worker belongs to, with the session of that panel,
// to turn the source off, and says how it went in a push of its own, since
// nothing else on the phone would. A panel that has not answered by the
// deadline did not take it, as far as the phone can tell.
export async function quiet(q, icon) {
    let ok = false;
    try {
        const r = await within((signal) => fetch(new URL("/api/push/quiet", self.location.origin).href, {
            method: "POST",
            credentials: "same-origin",
            signal,
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ what: q.what, key: q.key }),
        }));
        ok = r !== LATE && r.ok;
    } catch (err) {
    }
    const name = q.label.replace(/^Quiet:?\s*/, "");
    await self.registration.showNotification(ok ? `Quieted: ${name}` : "Not quieted", {
        body: ok
            ? "It is turned back on in the panel, in the settings of notifications."
            : "The panel did not take it: open the panel and turn it off there.",
        tag: `quiet:${q.what}:${q.key}`,
        silent: true,
        icon,
        badge: icon,
    });
}
