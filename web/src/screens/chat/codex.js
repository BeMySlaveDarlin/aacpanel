// The conversation of a codex thread: the shared screen with what codex brings
// to it. A thread lives with the daemon of its codex home, not in a place of
// the panel's: there is no terminal of it to watch, no window, no move and no
// question aside. The panel writes to it — words and files, into a queue of
// its own while a turn runs — sets how it thinks and what it may do from the
// band under the field, stops its turn, answers what it asks and closes it.

import { useState } from "preact/hooks";

import { html } from "../../html.js";
import { knows, whyNot } from "../../exec.js";
import { Icon } from "../../ui/icons.js";
import { useAction } from "../../actions/gate.js";
import { CODEX_CLOSE, CODEX_NOTE, noTurn } from "../../agent.js";
import { Conversation } from "./conversation.js";
import { CodexSheets, CodexStrip, useCatalog, useCodexPick } from "./codexpick.js";
import { IdLine, PlaceLine, ToolRow } from "./sessiontools.js";
import { useTermAvailable } from "./term.js";

// A thread is watched as a feed alone, and nothing about where it lives is
// asked of the host.
const PLACE = {
    view: "feed",
    sides: { view: "feed", pair: false, moves: "", tip: "", why: "" },
    win: { kind: "none" },
    way: { to: "" },
    move: null,
    canTerm: false,
    pickView: () => {},
    onWindow: () => {},
};

export function CodexChat(props) {
    const { name, live } = props;
    const term = useTermAvailable();
    // Which list of the band is open: how codex thinks, or what it may do.
    const [open, setOpen] = useState("");
    const catalog = useCatalog(open);
    const pick = useCodexPick(name, live, catalog);
    const cwd = (live && live.cwd) || "";

    const parts = {
        term,
        place: PLACE,
        toBackground: null,
        takesBack: true,
        work: false,
        sections: CodexSections,
        composer: () => ({ ids: true, waits: waits(live) }),
        strip: ({ wide }) => html`<${CodexStrip} wide=${wide} exec=${props.exec} now=${pick.now} open=${open}
                                                  onOpen=${setOpen} catalog=${catalog} set=${pick.set} cwd=${cwd} />`,
        look: () => null,
        sheets: ({ wide }) => !wide && html`<${CodexSheets} open=${open} onClose=${() => setOpen("")} now=${pick.now}
                                                             catalog=${catalog} set=${pick.set} cwd=${cwd} />`,
        aside: () => null,
    };
    return html`<${Conversation} ...${props} parts=${parts} />`;
}

// waits is what the field says while a turn runs: a message written now goes
// into the panel's queue and out as a turn of its own after this one, behind
// what already waits there.
function waits(live) {
    const ahead = (live && live.queued) || 0;
    return ahead > 0 ? `goes after the turn, behind ${ahead} queued` : "goes after the turn";
}

// CodexSections are the tools of a thread: where it lives, its id, the stop of
// its turn and its close — no move, no window, no bridge and no name to
// change.
export function CodexSections({ name, live, exec, onDone }) {
    const run = useAction();
    const stopWhy = noTurn(live) || (knows(exec, "session.stop") ? "" : whyNot(exec, "session.stop"));
    const closeWhy = knows(exec, "session.close") ? "" : whyNot(exec, "session.close");
    return html`
        <section class="toolsec">
            <div class="cmdsechead"><span>Where it lives</span></div>
            <ul class="mcplist toollist"><${PlaceLine} label="With codex" note=${CODEX_NOTE} /></ul>
        </section>
        <section class="toolsec">
            <div class="cmdsechead"><span>Session</span></div>
            <ul class="mcplist toollist"><${IdLine} id=${live.sessionId || ""} /></ul>
        </section>
        <section class="toolsec toolend">
            <ul class="mcplist toollist">
                <${ToolRow} icon=${Icon.stopsquare} stop label="Stop the turn" off=${stopWhy}
                            note="codex breaks off the turn it is running; the thread stays"
                            onPress=${() => { onDone(); run("session.stop", name, {}); }} />
                <${ToolRow} icon=${Icon.stop} stop label="Close the session" off=${closeWhy} note=${CODEX_CLOSE}
                            onPress=${() => { onDone(); run("session.close", name, {}); }} />
            </ul>
        </section>
    `;
}
