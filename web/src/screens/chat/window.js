// The window on the host: whether a terminal window shows this session, and
// how it is opened or closed.

import { useEffect, useState } from "preact/hooks";

import { whyNot } from "../../exec.js";
import { hostLabel } from "../../actions/registry.js";
import { blocked, moveSession } from "./switch.js";

const asking = { kind: "unknown", reason: "asking the host about the window" };

// useWindow asks the host whether a window shows this session, again when the
// session changes sides, and again on the call it returns second.
export function useWindow(name, transport) {
    const [answer, setAnswer] = useState({ for: null, ...asking });
    const [tick, setTick] = useState(0);
    const key = `${name}|${transport}`;

    useEffect(() => {
        if (!name) return undefined;
        let alive = true;
        fetch(`/api/session/window?name=${encodeURIComponent(name)}`, { credentials: "same-origin" })
            .then(async (r) => {
                if (!r.ok) throw new Error((await r.text()).trim() || `the server answered ${r.status}`);
                return r.json();
            })
            .then((body) => {
                if (alive) setAnswer({ for: key, kind: body.state || "unknown", reason: body.reason || "" });
            })
            .catch((err) => {
                if (alive) setAnswer({ for: key, kind: "unknown", reason: String(err.message || err) });
            });
        return () => {
            alive = false;
        };
    }, [key, tick]);

    return [answer.for === key ? answer : asking, () => setTick((n) => n + 1)];
}

// windowOf says what the window button does for this session and why it
// cannot, when it cannot. A session in the feed has nothing a window could
// show: the window takes it to the console first, and holds it there while it
// is open.
export function windowOf({ name, live, work, exec, win, way, run, onChange }) {
    if (live.transport === "stream" && way.to === "console") {
        return {
            moves: true,
            open: false,
            why: blocked(live) || whyNot(exec, "session.switch") || whyNot(exec, "window.open"),
            say: `Move to the console and open a window on ${hostLabel()}`,
            press: () => moveSession({ run, exec, name, to: "console", work, withWindow: true }),
        };
    }

    const open = win.kind === "open";
    const kind = open ? "window.close" : "window.open";
    return {
        moves: false,
        open,
        why: win.kind === "unknown" ? (win.reason || "the window state is unknown") : whyNot(exec, kind),
        say: open ? `The window on ${hostLabel()} is open · close it` : `Open a window on ${hostLabel()}`,
        press: async () => {
            const result = await run(kind, name, {});
            if (result && result.cancelled) return;
            onChange();
        },
    };
}
