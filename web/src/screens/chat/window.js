// The window toggle on the host: open a terminal window to this session, or close it.

import { useEffect, useState } from "preact/hooks";

import { html } from "../../html.js";
import { Icon } from "../../ui/icons.js";
import { useAction } from "../../actions/gate.js";
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

// WindowToggle renders one button for both positions of the window. A session
// in the feed has nothing a window could show: the window takes it to the
// console first, and holds it there while it is open.
export function WindowToggle({ name, live, work, exec, win, way, onChange }) {
    const run = useAction();

    if (live.transport === "stream" && way.to === "console") {
        const press = () => moveSession({ run, exec, name, to: "console", work, withWindow: true });
        return button({
            open: false,
            why: blocked(live) || whyNot(exec, "session.switch") || whyNot(exec, "window.open"),
            say: `Move to the console and open a window on ${hostLabel()}`,
            press,
        });
    }

    const open = win.kind === "open";
    const kind = open ? "window.close" : "window.open";
    const press = async () => {
        const result = await run(kind, name, {});
        if (result && result.cancelled) return;
        onChange();
    };
    return button({
        open,
        why: win.kind === "unknown" ? (win.reason || "the window state is unknown") : whyNot(exec, kind),
        say: open ? `The window on ${hostLabel()} is open · close it` : `Open a window on ${hostLabel()}`,
        press,
    });
}

function button({ open, why, say, press }) {
    const off = Boolean(why);
    return html`
        <button class=${`winbtn${open ? " on" : ""}${off ? " off" : ""}`} type="button"
                data-tip=${off ? undefined : say} data-tipside="left"
                title=${off ? why : undefined}
                aria-label=${off ? why : say} aria-pressed=${open}
                disabled=${off}
                onClick=${press}><${Icon.monitor} /></button>
    `;
}
