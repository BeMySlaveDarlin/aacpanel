// Moving a call the turn waits on to the background, as Ctrl+B does in the
// terminal: the call answers the session at once, the turn goes on, and the
// work runs on among the background tasks, where it is stopped like any other.

import { useState } from "preact/hooks";

import { html } from "../../html.js";
import { Icon } from "../../ui/icons.js";
import { useAction } from "../../actions/gate.js";
import { knows, whyNot } from "../../exec.js";

// The calls claude can move: a shell command and a subagent. Either one sent
// to the background from the start answers at once, so one still out is one
// the turn waits on.
const MOVABLE = new Set(["Bash", "Agent", "Task"]);

// inForeground says whether a call of the feed runs in the foreground now.
export function inForeground(call) {
    return Boolean(call) && call.open === true && MOVABLE.has(call.name) && Boolean(call.use);
}

// aims says whether one call can be named to the session. On the stream claude
// takes the id of the call; in the console the one key there moves every call
// at once, so a button over one call would move the others behind its back.
export function aims(to) {
    return Boolean(to) && to.stream === true;
}

// ToBackground moves one call, or with no call every call in the foreground.
// to is the session: { name, exec, stream }.
export function ToBackground({ to, call, count }) {
    const run = useAction();
    const [state, setState] = useState("");
    const exec = to.exec;
    const ready = knows(exec, "session.background");
    const all = !call;
    const word = state === "sent" ? "in background"
        : all ? (count > 1 ? `all ${count} to background` : "all to background")
        : "to background";
    const press = async (event) => {
        event.stopPropagation();
        if (state) return;
        setState("sending");
        const result = await run("session.background", to.name, all ? {} : { use: call.use });
        setState(result.ok ? "sent" : "");
    };
    const said = all
        ? "every call the turn waits on goes to the background, as Ctrl+B in the terminal"
        : `${call.name} goes to the background: the turn goes on, and the call runs on beside it`;
    return html`
        <button class=${`tobg${state === "sent" ? " sent" : ""}`} type="button"
                disabled=${!ready || Boolean(state)}
                title=${ready ? said : whyNot(exec, "session.background")}
                aria-label=${all ? "send every call in the foreground to the background" : `send ${call.name} to the background`}
                onClick=${press}>
            ${Icon.clock()}<span>${word}</span>
        </button>
    `;
}
