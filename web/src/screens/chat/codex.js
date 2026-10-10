// The conversation of a codex thread: the shared screen with what codex brings
// to it. A thread lives with the daemon of its codex home, not in a place of
// the panel's: there is no terminal of it to watch, no window, no move and no
// question aside. The panel writes to it — words and files, into a queue of
// its own while a turn runs — sets how it thinks and what it may do from the
// band under the field, stops its turn, answers what it asks and closes it;
// the agents the thread started are listed as claude's are, each opening its
// own thread.

import { useState } from "preact/hooks";

import { html } from "../../html.js";
import { knows, whyNot } from "../../exec.js";
import { Icon } from "../../ui/icons.js";
import { useAction } from "../../actions/gate.js";
import { CODEX_CLOSE, CODEX_NOTE, noTurn } from "../../agent.js";
import { Conversation } from "./conversation.js";
import { CodexSheets, CodexStrip, useCatalog, useCodexPick } from "./codexpick.js";
import { ProcessesChip, ProcessesSheet, useCompact } from "./codexthread.js";
import { LOOK_NAMES } from "./look.js";
import { AgentsChip } from "./work.js";
import { McpSheet } from "./mcp.js";
import { SetupSheet } from "./setup.js";
import { IdLine, PlaceLine, ToolRow } from "./sessiontools.js";
import { outsideNote } from "../sessions/kin.js";
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

// What a thread has and what it left running open as sheets of its own: its
// MCP servers and skills, read-only, and its background processes.
const OWN_LOOKS = {
    mcp: LOOK_NAMES.mcp,
    skills: "skills",
    processes: "background processes",
};

export function CodexChat(props) {
    const { name, live, exec } = props;
    const term = useTermAvailable();
    // Which list or pane of the band is open: how codex thinks, what it may
    // do, the thread, or a pane of the thread.
    const [open, setOpen] = useState("");
    const catalog = useCatalog(name, open);
    const pick = useCodexPick(name, live, catalog);
    const compact = useCompact(name);
    const cwd = (live && live.cwd) || "";
    const pct = live ? live.pct : null;

    const parts = {
        term,
        place: PLACE,
        toBackground: null,
        takesBack: true,
        // A question of codex is answered by its protocol wherever it lives:
        // codex in tmux has no screen of claude's to press keys into.
        askStream: true,
        work: ({ work, setLook }) => html`<${CodexWork} work=${work} processes=${live.processes} onOpen=${setLook} />`,
        sections: CodexSections,
        // A thread whose own turn is over is free while its agents work: a
        // message starts a turn at once, and there is no turn for the button
        // to stop.
        composer: () => ({ ids: true, waits: waits(live), busy: live.status === "busy" && !live.turnOver }),
        strip: ({ wide }) => html`<${CodexStrip} wide=${wide} exec=${exec} live=${live} pct=${pct} now=${pick.now}
                                                  open=${open} onOpen=${setOpen} onCompact=${compact}
                                                  catalog=${catalog} set=${pick.set} cwd=${cwd} />`,
        look: (look) => {
            if (!OWN_LOOKS[look.kind]) return null;
            const node = look.kind === "mcp"
                ? html`<${McpSheet} name=${name} exec=${exec} readOnly />`
                : look.kind === "skills"
                ? html`<${SetupSheet} name=${name} part="skills"
                                      readNote="Read-only: a skill is turned on or off in codex's config." />`
                : html`<${ProcessesSheet} name=${name} exec=${exec} />`;
            return { label: OWN_LOOKS[look.kind], node };
        },
        sheets: ({ wide }) => html`<${CodexSheets} wide=${wide} name=${name} live=${live} exec=${exec} pct=${pct}
                                                    open=${open} onOpen=${setOpen} onClose=${() => setOpen("")}
                                                    onCompact=${compact} now=${pick.now} catalog=${catalog}
                                                    set=${pick.set} cwd=${cwd} />`,
        aside: () => null,
    };
    return html`<${Conversation} ...${props} parts=${parts} />`;
}

// CodexWork counts what a thread has in flight under the composer, where
// claude's counters stand and in their order: what it left running, while
// something is, and the agents it started — a chip that stands whatever the
// thread holds, as claude's does.
function CodexWork({ work, processes, onOpen }) {
    return html`
        <div class="wchips">
            <${ProcessesChip} count=${processes} onOpen=${() => onOpen({ kind: "processes" })} />
            <${AgentsChip} work=${work} onOpen=${onOpen} />
        </div>
    `;
}

// waits is what the field says while a turn runs: a message written now goes
// into the panel's queue and out as a turn of its own after this one, behind
// what already waits there.
function waits(live) {
    const ahead = (live && live.queued) || 0;
    return ahead > 0 ? `goes after the turn, behind ${ahead} queued` : "goes after the turn";
}

// CodexSections are the tools of a thread: where it lives, what it has, its
// id, the stop of its turn and its close — no move, no window and no bridge.
// Its name is changed from the thread, behind the band. A codex running on
// its own has where it lives and its id alone: the panel asks it nothing.
export function CodexSections({ name, live, exec, onDone, onLook }) {
    const run = useAction();
    if (live.outside) {
        return html`
            <section class="toolsec">
                <div class="cmdsechead"><span>Where it lives</span></div>
                <ul class="mcplist toollist"><${PlaceLine} label="Outside the panel" note=${outsideNote(live)} /></ul>
            </section>
            <section class="toolsec">
                <div class="cmdsechead"><span>Session</span></div>
                <ul class="mcplist toollist"><${IdLine} id=${live.sessionId || ""} /></ul>
            </section>
        `;
    }
    const stopWhy = noTurn(live) || (knows(exec, "session.stop") ? "" : whyNot(exec, "session.stop"));
    const closeWhy = knows(exec, "session.close") ? "" : whyNot(exec, "session.close");
    return html`
        <section class="toolsec">
            <div class="cmdsechead"><span>Where it lives</span></div>
            <ul class="mcplist toollist"><${PlaceLine} label="With codex" note=${CODEX_NOTE} /></ul>
        </section>
        <section class="toolsec">
            <div class="cmdsechead"><span>What it has</span></div>
            <ul class="mcplist toollist">
                <${ToolRow} icon=${Icon.plug} label="MCP servers" note="what codex can reach, as it reports it"
                            more onPress=${() => { onDone(); onLook("mcp"); }} />
                <${ToolRow} icon=${Icon.skill} label="Skills" note="read-only: set in codex's config"
                            more onPress=${() => { onDone(); onLook("skills"); }} />
            </ul>
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
