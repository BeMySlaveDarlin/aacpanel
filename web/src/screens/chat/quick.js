// Quick phrases go through the composer's send path without borrowing its
// draft. The crown holds session actions; only Pause takes a sliding release.
import { useEffect, useRef, useState } from "preact/hooks";

import { html } from "../../html.js";
import { useAction } from "../../actions/gate.js";
import { knows, whyNot } from "../../exec.js";
import { shownName } from "../../agent.js";
import { Icon } from "../../ui/icons.js";
import { Pie } from "../../ui/pie.js";
import { endsOf, restartSession } from "../sessions/controls.js";
import { sessionPauseWhy } from "../sessions/pause.js";
import { usePauseDial } from "../sessions/pausedial.js";
import { openedBy } from "../sessions/kin.js";

const ICONS = [Icon.ask, Icon.play, Icon.checklist, Icon.info, Icon.moon];
const REPORT = "Write your report and close this session: to your parent if a session opened you, otherwise to the person in the conversation.";

export function QuickCommands({ name, live, exec, lift, press, spot, keyboard, onClose, onCommands, onSend }) {
    const run = useAction();
    const [phrases, setPhrases] = useState([]);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState("");
    const sent = useRef(false);
    useEffect(() => {
        let active = true;
        fetch(`/api/session/intent?name=${encodeURIComponent(name)}`, { credentials: "same-origin" })
            .then(async (r) => {
                if (!r.ok) throw new Error((await r.text()).trim() || `the server answered ${r.status}`);
                return r.json();
            })
            .then((data) => {
                if (!active) return;
                const quick = data.own?.quick || data.inherited?.find((v) => v.key === "quick")?.value || "";
                setPhrases(quick.split("\n").map((line) => line.trim()).filter(Boolean).slice(0, 5));
                setLoading(false);
            })
            .catch((err) => { if (active) { setError(String(err.message || err)); setLoading(false); } });
        return () => { active = false; };
    }, [name]);
    const sendWhy = live.outside ? "The session is outside the panel" : knows(exec, "session.send") ? "" : whyNot(exec, "session.send");
    const phraseWhy = loading ? "Loading quick phrases…" : error || sendWhy;
    const send = (text) => {
        if (sent.current) return;
        sent.current = true;
        onSend(text);
    };
    const session = { ...live, session: name };
    const project = live.project?.id ? live.project : null;
    const pause = usePauseDial({ project, session, exec, onClose });
    const ends = endsOf({ s: session, exec, pin: { why: "" }, busy: live.restarting });
    const phrase = (text, i) => ({ key: `quick-${i}`, icon: ICONS[i], word: text,
        lines: text === "AskUserQuestion" ? ["AskUser", "Question"] : null, title: `“${text}”`,
        note: "goes as your message", off: phraseWhy, pick: () => send(text) });
    // Clockwise from the top, the frequent phrases meet the thumb first.
    const slots = phrases.map(phrase);
    const items = [
        { key: "commands", icon: Icon.command, word: "All commands", lines: ["All", "commands"], title: "All commands", pick: onCommands },
        ...slots.filter((_, i) => i % 2 === 1).reverse(),
        ...slots.filter((_, i) => i % 2 === 0),
    ];
    const crown = pause.crown ? { ...pause.crown, reverse: true } : { items: [
        { key: "restart", icon: Icon.refresh, word: "Restart", title: "Restart", tap: true,
            off: live.outside ? sendWhy : ends.restartWhy, note: ends.restartNote, pick: () => restartSession(run, session) },
        { key: "report", icon: Icon.exit, word: "Report & close", title: "Report & close", tap: true, danger: true,
            off: sendWhy, note: openedBy(live) ? "Write the report to the parent and close this session" : "Write the report in the conversation and close this session",
            pick: () => send(REPORT) },
        { key: "pause", icon: Icon.pause, word: "Pause", title: "Pause", stay: true,
            off: sessionPauseWhy(live, project, exec), note: "Only this session; pick its end on the dial", pick: pause.activate },
    ] };
    const layer = pause.layer || (pause.crown ? { key: "pause", className: "quickpause", items: items.map((item) => ({ ...item, off: "Pick the end of the pause, or go back to the ring" })),
        hubLabel: "Back to quick commands", hubWord: "to the ring", back: pause.activate } : null);
    return html`<${Pie} className="piequick" label="Quick commands" name=${error || (loading ? "Loading quick phrases…" : shownName(live) || name)} items=${items}
        lift=${lift} press=${press} spot=${spot} focusFirst=${keyboard} crown=${crown}
        bar=${pause.bar} layer=${layer} onClose=${onClose} />`;
}
