// The switch between the console and the feed: the same conversation, closed
// on one side and resumed on the other.

import { useEffect, useState } from "preact/hooks";

import { html } from "../../html.js";
import { Icon } from "../../ui/icons.js";
import { useAction } from "../../actions/gate.js";
import { knows, whyNot } from "../../exec.js";
import { plural } from "../../format.js";
import { liveWork } from "./work.js";

const asking = { to: "", reason: "asking the panel where this session can move" };

// stops names what runs inside the process and ends with it. A resumed
// conversation does not bring it back, so the person reads it before pressing.
export function stops(work) {
    const { tasks, agents, flows } = liveWork(work);
    const out = [];
    if (tasks.length) out.push(`${tasks.length} ${plural(tasks.length, "background task", "background tasks")}`);
    if (agents.length) out.push(`${agents.length} ${plural(agents.length, "agent", "agents")}`);
    if (flows.length) out.push(`${flows.length} ${plural(flows.length, "workflow", "workflows")}`);
    return out.join(", ");
}

// blocked says why the session cannot move right now, or nothing: a switch
// happens between turns, and never under an open question.
function blocked(live) {
    if (!live) return "the session is not live";
    if (live.status === "busy") return "the session is answering — switch once it finishes";
    if (live.status === "waiting") return "the session is waiting for an answer — answer it first";
    return "";
}

// SwitchToggle renders the button that moves the session to the other side. It
// is there only when that side is open to this session: a stream session can
// always go to the console, a console goes to the feed only when its project
// lives there.
export function SwitchToggle({ name, live, work, exec }) {
    const run = useAction();
    const [answer, setAnswer] = useState({ for: null, ...asking });
    const transport = (live && live.transport) || "";

    useEffect(() => {
        let alive = true;
        fetch(`/api/session/switch?name=${encodeURIComponent(name)}`, { credentials: "same-origin" })
            .then(async (r) => {
                if (!r.ok) throw new Error((await r.text()).trim() || `the server answered ${r.status}`);
                return r.json();
            })
            .then((body) => {
                if (alive) setAnswer({ for: name, to: body.to || "", reason: body.reason || "" });
            })
            .catch((err) => {
                if (alive) setAnswer({ for: name, to: "", reason: String(err.message || err) });
            });
        return () => {
            alive = false;
        };
    }, [name, transport]);

    const way = answer.for === name ? answer : asking;
    if (!way.to) return null;

    const toConsole = way.to === "console";
    const why = blocked(live) || whyNot(exec, "session.switch");
    const off = Boolean(why) || !knows(exec, "session.switch");
    const say = toConsole ? "Move to the console" : "Move to the feed";

    const press = () => {
        const lost = stops(work);
        run("session.switch", name, { to: way.to, force: Boolean(lost), stops: lost });
    };

    return html`
        <button class=${`winbtn${off ? " off" : ""}`} type="button"
                data-tip=${off ? undefined : say} data-tipside="left"
                title=${off ? why : undefined}
                aria-label=${off ? why : say}
                disabled=${off}
                onClick=${press}><${Icon.swap} /></button>
    `;
}
