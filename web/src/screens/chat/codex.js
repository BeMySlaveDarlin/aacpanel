// The conversation of a codex thread: the shared screen with what codex brings
// to it. A thread lives with the daemon of its codex home, not in a place of
// the panel's: there is no terminal of it to watch, no window, no move and no
// question aside. The panel writes to it — words and files, into the turn
// that runs as claude takes a message written during a turn, and the answer
// to a question it asked — sets how it thinks and what it may do from the
// band under the field, stops its turn, answers what it asks, restarts and
// closes it; the agents the thread started are listed as claude's are, each opening its
// own thread.

import { useState } from "preact/hooks";

import { html } from "../../html.js";
import { knows, whyNot } from "../../exec.js";
import { Icon } from "../../ui/icons.js";
import { useAction } from "../../actions/gate.js";
import { useToast } from "../../ui/toasts.js";
import { CODEX_CLOSE, CODEX_NOTE, CODEX_RESTART, codexRestartWhy, noTurn } from "../../agent.js";
import { Conversation } from "./conversation.js";
import { CodexSheets, CodexStrip, useCatalog, useCodexPick } from "./codexpick.js";
import { ProcessesChip, ProcessesSheet, useCompact } from "./codexthread.js";
import { LOOK_NAMES } from "./look.js";
import { AgentsChip, HeldChip } from "./work.js";
import { McpSheet } from "./mcp.js";
import { RoleSheet, roleWhy } from "./role.js";
import { IntentSheet, intentWhy } from "./intent.js";
import { SetupSheet } from "./setup.js";
import { codexResumeOf, copyResume, DetachLine, IdLine, ParentLine, PlaceLine, ROLE_NONE, ToolRow } from "./sessiontools.js";
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
// MCP servers, skills and background processes.
const OWN_LOOKS = {
    mcp: LOOK_NAMES.mcp,
    role: LOOK_NAMES.role,
    intent: LOOK_NAMES.intent,
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
        composer: () => ({ ids: true, busy: live.status === "busy" && !live.turnOver }),
        strip: ({ wide }) => html`<${CodexStrip} wide=${wide} exec=${exec} live=${live} pct=${pct} now=${pick.now}
                                                  open=${open} onOpen=${setOpen} onCompact=${compact}
                                                  catalog=${catalog} set=${pick.set} cwd=${cwd} />`,
        look: (look, { setLook }) => {
            if (!OWN_LOOKS[look.kind]) return null;
            const node = look.kind === "role"
                ? html`<${RoleSheet} name=${name} live=${live} exec=${exec} onDone=${() => setLook(null)} />`
                : look.kind === "intent"
                ? html`<${IntentSheet} name=${name} exec=${exec} onDone=${() => setLook(null)} />`
                : look.kind === "mcp"
                ? html`<${McpSheet} name=${name} exec=${exec} />`
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
// claude's counters stand and in their order: what it left running and the
// observers and alarms it asked the panel for, each while there is something,
// and the agents it started — a chip that stands whatever the thread holds,
// as claude's does.
function CodexWork({ work, processes, onOpen }) {
    return html`
        <div class="wchips">
            <${ProcessesChip} count=${processes} onOpen=${() => onOpen({ kind: "processes" })} />
            <${HeldChip} work=${work} onOpen=${onOpen} />
            <${AgentsChip} work=${work} onOpen=${onOpen} />
        </div>
    `;
}

// CodexSections are the tools of a thread: where it lives, what it has, its
// id, the session it stands under while it stands under one, the stop of its
// turn, its restart and its close — no move, no window and no bridge.
// Its name is changed from the thread, behind the band. A codex running on
// its own has where it lives and how to resume it: the panel asks it nothing.
export function CodexSections({ name, live, exec, snapshot, onDone, onLook }) {
    const run = useAction();
    const toast = useToast();
    const resume = codexResumeOf(live, snapshot);
    const resumeLine = resume && html`<${ToolRow} icon=${Icon.copy} label="Copy the resume command" note=${resume}
                                              onPress=${() => copyResume(resume, toast)} />`;
    if (live.outside) {
        return html`
            <section class="toolsec">
                <div class="cmdsechead"><span>Where it lives</span></div>
                <ul class="mcplist toollist"><${PlaceLine} label="Outside the panel" note=${outsideNote(live)} /></ul>
            </section>
            <section class="toolsec">
                <div class="cmdsechead"><span>Session</span></div>
                <ul class="mcplist toollist"><${IdLine} id=${live.sessionId || ""} />${resumeLine}</ul>
            </section>
        `;
    }
    const stopWhy = noTurn(live) || (knows(exec, "session.stop") ? "" : whyNot(exec, "session.stop"));
    const closeWhy = knows(exec, "session.close") ? "" : whyNot(exec, "session.close");
    const restartWhy = codexRestartWhy(live) || (live.restarting ? "The session is already restarting" : "")
        || (knows(exec, "session.restart") ? "" : whyNot(exec, "session.restart"));
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
            <ul class="mcplist toollist">
                <${ToolRow} icon=${Icon.badge} label="Role…" note=${live.role || ROLE_NONE} clamp off=${roleWhy(exec)}
                            onPress=${() => { onDone(); onLook("role"); }} />
                <${ToolRow} icon=${Icon.pencil} label="Session messages…" note="the first message and the message after a restart"
                            off=${intentWhy(exec)} onPress=${() => { onDone(); onLook("intent"); }} />
                <${IdLine} id=${live.sessionId || ""} />
                ${resumeLine}
                <${DetachLine} name=${name} live=${live} exec=${exec} run=${run} onDone=${onDone} />
                <${ParentLine} live=${live} exec=${exec} snapshot=${snapshot} onDone=${onDone} onLook=${onLook} />
            </ul>
        </section>
        <section class="toolsec toolend">
            <ul class="mcplist toollist">
                <${ToolRow} icon=${Icon.stopsquare} stop label="Stop the turn" off=${stopWhy}
                            note="codex breaks off the turn it is running; the thread stays"
                            onPress=${() => { onDone(); run("session.stop", name, {}); }} />
                <${ToolRow} icon=${Icon.refresh} stop label="Restart the session" off=${restartWhy} note=${CODEX_RESTART}
                            onPress=${() => { onDone(); run("session.restart", name, {}); }} />
                <${ToolRow} icon=${Icon.stop} stop label="Close the session" off=${closeWhy} note=${CODEX_CLOSE}
                            onPress=${() => { onDone(); run("session.close", name, {}); }} />
            </ul>
        </section>
    `;
}
