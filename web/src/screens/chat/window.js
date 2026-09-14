// The window toggle on the host: open a terminal window to this session, or close it.

import { useEffect, useState } from "preact/hooks";

import { html } from "../../html.js";
import { Icon } from "../../ui/icons.js";
import { useAction } from "../../actions/gate.js";
import { knows, whyNot } from "../../exec.js";
import { hostLabel } from "../../actions/registry.js";

const asking = { kind: "unknown", reason: "asking the host about the window" };

// WindowToggle renders one button for both positions of the window.
export function WindowToggle({ name, exec }) {
    const run = useAction();
    const [answer, setAnswer] = useState({ for: null, ...asking });
    const [tick, setTick] = useState(0);

    useEffect(() => {
        let alive = true;
        fetch(`/api/session/window?name=${encodeURIComponent(name)}`, { credentials: "same-origin" })
            .then(async (r) => {
                if (!r.ok) throw new Error((await r.text()).trim() || `the server answered ${r.status}`);
                return r.json();
            })
            .then((body) => {
                if (alive) setAnswer({ for: name, kind: body.state || "unknown", reason: body.reason || "" });
            })
            .catch((err) => {
                if (alive) setAnswer({ for: name, kind: "unknown", reason: String(err.message || err) });
            });
        return () => {
            alive = false;
        };
    }, [name, tick]);

    const state = answer.for === name ? answer : asking;
    const open = state.kind === "open";
    const kind = open ? "window.close" : "window.open";
    const able = knows(exec, kind);
    const why = state.kind === "unknown" ? (state.reason || "the window state is unknown") : whyNot(exec, kind);
    const off = state.kind === "unknown" || !able;

    const say = open
        ? `The window on ${hostLabel()} is open · close it`
        : `Open a window on ${hostLabel()}`;

    const press = async () => {
        const result = await run(kind, name, {});
        if (result && result.cancelled) return;
        setTick((n) => n + 1);
    };

    return html`
        <button class=${`winbtn${open ? " on" : ""}${off ? " off" : ""}`} type="button"
                data-tip=${off ? undefined : say} data-tipside="left"
                title=${off ? why : undefined}
                aria-label=${off ? why : say} aria-pressed=${open}
                disabled=${off}
                onClick=${press}><${Icon.monitor} /></button>
    `;
}
