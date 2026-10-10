// The conversation of a claude session: the shared screen with what claude
// brings to it. A claude lives on the stream or in tmux and moves between
// them, is watched as a feed or as its terminal, has a window on the host,
// takes slash commands, a shell after "!" and questions aside, lists its
// model, effort and mode under the field, and sends a call its turn waits on
// to the background.

import { useState } from "preact/hooks";

import { html } from "../../html.js";
import { knows } from "../../exec.js";
import { useWide } from "../../ui/wide.js";
import { Conversation } from "./conversation.js";
import { LOOK_NAMES } from "./look.js";
import { McpSheet } from "./mcp.js";
import { StatusSheet } from "./status.js";
import { SetupSheet, SETUP_TITLES } from "./setup.js";
import { CommandsChip, CommandsSheet } from "./commands.js";
import { RenameSheet } from "./rename.js";
import { PickBar, PickSheet, PickWords } from "./picker.js";
import { SessionSections } from "./sessiontools.js";
import { SideChat, useSideChat } from "./sidechat.js";
import { useWindow } from "./window.js";
import { Work } from "./work.js";
import { sidesOf, useMove, useSwitchWay } from "./switch.js";
import { useTermAvailable } from "./term.js";
import { useViewPick } from "./viewpick.js";

// The sheets of claude's own kinds: its commands, the screens some of them
// answer with, its MCP servers, the info of the session and its new name.
const OWN_LOOKS = new Set(["commands", "mcp", "status", "setup", "rename"]);

export function ClaudeChat(props) {
    const { name, id, live, exec, snapshot, wait } = props;
    const wide = useWide();
    const term = useTermAvailable();
    // Which list of settings is open: the model, the effort or the mode.
    const [picking, setPicking] = useState("");
    // A claude out of the panel's reach lives in no pane of a tmux the panel
    // attaches to: there is no screen of it to attach to.
    const canTerm = term.ok && Boolean(live) && !live.outside;
    // What to watch it with is the session's own choice on this device.
    const [picked, pickView] = useViewPick(name, canTerm, wide);

    // Where the session lives decides what the pair of views does: see
    // sidesOf. The window on the host is asked here, since it holds tmux.
    const transport = (live && live.transport) || "";
    const way = useSwitchWay(live ? name : "", transport);
    const [win, askWindow] = useWindow(live ? name : "", transport);
    const sides = live
        ? sidesOf({ live, way, held: win.kind === "open", picked, canTerm, exec })
        : { view: picked, pair: false, moves: "", tip: "", why: "" };
    const move = useMove(wait, name, live, way);

    // Questions asked aside: on the stream the panel asks them itself, and
    // they reach neither the conversation nor its transcript.
    const sideChat = useSideChat(name, id);
    const onStream = Boolean(live) && live.transport === "stream";
    const asksAside = onStream;
    const onPicker = knows(exec, "session.set") ? setPicking : null;

    // A command the panel answers with a screen opens it in the sheet; the
    // screens of settings share one view, told apart by their part.
    const screenLook = (setLook) => (what) => setLook(SETUP_TITLES[what] ? { kind: "setup", part: what } : { kind: what });
    const commandsChip = (setLook) => html`<${CommandsChip} onOpen=${() => setLook({ kind: "commands" })} />`;

    const parts = {
        term,
        place: { view: sides.view, sides, win, way, move, canTerm, pickView, onWindow: askWindow },
        // A call the turn waits on goes to the background in a live session the
        // panel reaches: on the stream by its id, in the console all at once.
        toBackground: live && !live.outside ? { name, exec, stream: onStream } : null,
        // A message is named for the queue only on the stream: a terminal
        // session's queue is on its screen.
        takesBack: Boolean(live) && live.transport === "stream",
        askStream: onStream,
        work: ({ work, setLook }) => html`<${Work} work=${work} onOpen=${(what) => setLook(what)} />`,
        sections: SessionSections,
        composer: ({ setLook }) => ({
            commands: true,
            shell: true,
            ids: onStream,
            onPicker,
            onScreen: screenLook(setLook),
            onSide: asksAside ? sideChat.ask : null,
        }),
        strip: ({ wide: desk, setLook }) => (desk
            ? html`<${PickBar} name=${name} live=${live} exec=${exec} lead=${commandsChip(setLook)} />`
            : html`<${PickWords} live=${live} exec=${exec} onPick=${setPicking} lead=${commandsChip(setLook)} />`),
        look: (look, { setLook, setInsert }) => {
            if (!OWN_LOOKS.has(look.kind)) return null;
            const label = look.kind === "setup" ? SETUP_TITLES[look.part] : LOOK_NAMES[look.kind];
            const node = look.kind === "mcp"
                ? html`<${McpSheet} name=${name} exec=${exec} />`
                : look.kind === "status"
                ? (live ? html`<${StatusSheet} name=${name} live=${live} />`
                    : html`<p class="cmdnote">The session has ended: there is no one to ask.</p>`)
                : look.kind === "commands"
                ? html`<${CommandsSheet} name=${name} live=${live} exec=${exec} onScreen=${screenLook(setLook)}
                                         onPicker=${onPicker ? (what) => { setLook(null); setPicking(what); } : null}
                                         onSide=${asksAside ? () => { setLook(null); setInsert({ key: Date.now(), text: "/btw ", command: true }); } : null}
                                         onDone=${() => setLook(null)} />`
                : look.kind === "rename"
                ? (live ? html`<${RenameSheet} name=${name} exec=${exec} onDone=${() => setLook(null)}
                                               taken=${((snapshot && snapshot.sessions) || []).map((s) => s.session)} />`
                    : html`<p class="cmdnote">The session has ended: there is nothing to rename.</p>`)
                : (live ? html`<${SetupSheet} name=${name} part=${look.part} />`
                    : html`<p class="cmdnote">The session has ended: there is no one to ask.</p>`);
            return { label, node };
        },
        sheets: () => html`<${PickSheet} what=${picking} onClose=${() => setPicking("")} name=${name} live=${live} exec=${exec} />`,
        aside: ({ wide: desk, feedShown, feedRef }) => asksAside && feedShown
            && html`<${SideChat} chat=${sideChat} wide=${desk} feedRef=${feedRef} />`,
    };
    return html`<${Conversation} ...${props} parts=${parts} />`;
}
