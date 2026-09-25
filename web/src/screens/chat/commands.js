// The commands the panel does itself, gathered behind one button under the
// composer: the screens it answers with, the pickers of how the session runs,
// and the commands that go to the session. Each does what typing it does — the
// list is read from the same registry the composer reads, so it cannot offer
// what the composer would refuse.

import { html } from "../../html.js";
import { useAction } from "../../actions/gate.js";
import { knows, whyNot } from "../../exec.js";
import { COMMANDS, SCREENS } from "../../actions/registry.js";
import { Icon } from "../../ui/icons.js";
import { modelTitle } from "./head.js";
import { effortName, modeName } from "./picker.js";

// SENT are the commands of the list that go to the session, in the order a
// person reaches for them.
const SENT = ["context", "usage", "compact"];

// panelCommands lays out what the panel does itself for a session, by group.
// A screen the console keeps to its own keys is left out of a console session,
// and so is a command the stream does not take.
export function panelCommands({ stream, live, pick, side }) {
    const screens = Object.keys(SCREENS)
        .filter((id) => stream || SCREENS[id].console)
        .map((id) => ({ id, line: `/${id}`, note: SCREENS[id].name, kind: "screen" }));
    const session = pick ? [
        { id: "model", line: "/model", note: live && live.model ? modelTitle(live.model) : "Model", kind: "pick" },
        { id: "effort", line: "/effort", note: live && live.effort ? effortName(live.effort) : "Effort", kind: "pick" },
        { id: "mode", line: "Mode", note: live && live.mode ? modeName(live.mode) : "Permission mode", kind: "pick" },
    ] : [];
    const talk = [
        ...(side ? [{ id: "btw", line: "/btw", note: "A question aside", kind: "side" }] : []),
        ...SENT.filter((id) => COMMANDS[id] && !(stream && COMMANDS[id].console))
            .map((id) => ({ id, line: `/${id}`, note: COMMANDS[id].name, kind: "command" })),
    ];
    return [
        { title: "Screens", rows: screens },
        { title: "How the session runs", rows: session },
        { title: "The conversation", rows: talk },
    ].filter((g) => g.rows.length);
}

// CommandsChip opens the list from the composer's strip, right after the
// paperclip and ahead of the model, the effort and the mode.
export function CommandsChip({ onOpen }) {
    return html`
        <button class="pkcmds" type="button" aria-label="commands of the panel" onClick=${onOpen}>
            ${Icon.command()}
        </button>
    `;
}

// CommandsSheet is the list itself. A row opens its screen or its picker, puts
// a question aside in front of what is typed, or sends its command through the
// same confirmation the composer's would pass. A host whose executor cannot
// send commands shows those rows with the reason instead.
export function CommandsSheet({ name, live, exec, onScreen, onPicker, onSide, onDone }) {
    const run = useAction();
    const stream = Boolean(live) && live.transport === "stream";
    const canSend = knows(exec, "session.command");
    const why = canSend ? "" : whyNot(exec, "session.command");
    const groups = panelCommands({ stream, live, pick: Boolean(onPicker), side: Boolean(onSide) });

    const press = async (row) => {
        if (row.kind === "screen") return onScreen(row.id);
        if (row.kind === "command" && !canSend) return undefined;
        if (row.kind === "pick") return onPicker(row.id);
        if (row.kind === "side") return onSide();
        const result = await run("session.command", name, { command: row.id, arg: "" });
        if (result && result.ok) onDone();
        return undefined;
    };

    return html`
        <div class="cmdsheet">
            <div class="shead cmdtitle"><span class="cmdhead">Commands</span></div>
            <p class="cmdnote">What the panel does itself in this session: each works as typing it in the composer does.</p>
            ${groups.map((g) => html`
                <section key=${g.title} class="cmdsec">
                    <div class="cmdsechead"><span>${g.title}</span></div>
                    <ul class="mcplist">
                        ${g.rows.map((row) => {
                            const off = row.kind === "command" && !canSend;
                            return html`
                            <li key=${row.id}>
                                <button type="button" class="mcprow" onClick=${() => press(row)} disabled=${off}>
                                    <span class="cmdname">${row.line}</span>
                                    <span class="cmdrownote">${off ? why : row.note}</span>
                                    <span class="crgo">${Icon.chevron()}</span>
                                </button>
                            </li>
                        `;
                        })}
                    </ul>
                </section>
            `)}
        </div>
    `;
}
