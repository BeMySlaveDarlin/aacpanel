// The menus of the sessions column: of a live session, of a closed
// conversation and of a project's heading. An item does what the button of
// the same name does elsewhere — on the row, in the tools of the
// conversation, on the shelf — through the same code, and an item that
// cannot be pressed now says why. What the host is asked about the session —
// which way it can move, whether a window shows it — is asked as the menu
// opens and not before.
import { html } from "../html.js";
import { knows, whyNot } from "../exec.js";
import { useAction } from "../actions/gate.js";
import { hostLabel } from "../actions/registry.js";
import { useToast } from "../ui/toasts.js";
import { MenuItem, MenuList, useMenuSheet } from "../ui/menu.js";
import { CODEX_CLOSE, CODEX_RESTART, codexRestartWhy, isCodex, noTurn, shownName } from "../agent.js";
import { codexResumeOf, copyId, copyResume, DETACH, detach, detachWhy, renameWhy, resumeOf } from "../screens/chat/sessiontools.js";
import { LOOK_NAMES } from "../screens/chat/look.js";
import { RenameSheet, takenNames } from "../screens/chat/rename.js";
import { RoleSheet, roleWhy } from "../screens/chat/role.js";
import { IntentSheet, intentWhy } from "../screens/chat/intent.js";
import { PARENT_LABEL, PARENT_NOTE, ParentSheet, parentWhy } from "../screens/chat/parent.js";
import { useRemote } from "../screens/chat/remote.js";
import { moveSession, sidesOf, useSwitchWay } from "../screens/chat/switch.js";
import { useWindow, windowOf } from "../screens/chat/window.js";
import { useTermJump } from "../screens/terms/jump.js";
import { projectOf, resume, startIn } from "../screens/sessions/actions.js";
import { stopsOf } from "../screens/sessions/blocks.js";
import { PauseSheet, pauseWhy } from "../screens/sessions/pause.js";
import { pinOf } from "../screens/sessions/pin.js";
import { openedBy } from "../screens/sessions/kin.js";

// What the close and the restart of a live session say while they are under
// way, on the icons of its row and in its menu alike.
export const UNDER_WAY = { close: "The session is already closing", restart: "The session is already restarting" };

// liveMenuOf is what a right click on a live session opens wherever the
// session is drawn — its row in the column, its node in the graph of the
// home: the body of the menu, its props and its label, as useMenu's handler
// takes them. What the session's close or restart is doing it asks the
// waits of the panel (wait), what they would stop the snapshot.
export function liveMenuOf(s, { exec, snapshot, wait, onOpen, onTerm }) {
    const closing = wait ? wait.of("close", s.session) : null;
    const restarting = wait ? wait.of("restart", s.session) : null;
    return [SessionMenu, {
        s, exec, snapshot, wait, onOpen, onTerm, lost: stopsOf(s),
        closing: closing ? UNDER_WAY.close : "", restarting: restarting || s.restarting ? UNDER_WAY.restart : "",
    }, `session ${shownName(s)}`];
}

// SessionMenu is the menu of a live session's row. The row hands it what it
// shows itself: how the session opens, what its close or its restart says
// while it is under way (closing, restarting), and what they would stop
// (lost) — the count of the snapshot, the only one a row has. A session the
// panel did not start is only read: it is opened and found again, nothing
// more. A thread of codex takes what codex takes.
export function SessionMenu(props) {
    if (props.s.outside) return html`<${OutsideMenu} ...${props} />`;
    if (isCodex(props.s)) return html`<${ThreadMenu} ...${props} />`;
    return html`<${ClaudeMenu} ...${props} />`;
}

// copies are the two ways to find a session again: its id and the command
// that resumes it in a terminal of its agent.
function copies(row, toast, snapshot) {
    const id = row.sessionId;
    const codex = isCodex(row);
    const resumeLine = codex ? codexResumeOf(row, snapshot) : resumeOf(id, row.cwd);
    return [
        id && html`<${MenuItem} key="id" label=${codex ? "Copy the thread ID" : "Copy the session ID"}
                                onPick=${() => copyId(id, toast)} />`,
        resumeLine && html`<${MenuItem} key="resume" label="Copy the resume command"
                                        onPick=${() => copyResume(resumeLine, toast)} />`,
    ];
}

// detachItem takes a session from under the session it stands under, as the
// line of its tools does; a session standing on its own has none.
function detachItem(s, exec, run) {
    const parent = openedBy(s);
    return parent && html`<${MenuItem} key="detach" label="Detach from parent" note=${`under ${parent}`} tip=${DETACH}
                                       off=${detachWhy(exec)} onPick=${() => detach(run, s.session)} />`;
}

function parentItem(s, snapshot, exec, sheet) {
    return html`<${MenuItem} key="parent" label=${PARENT_LABEL} tip=${PARENT_NOTE}
                            off=${parentWhy(exec, s, snapshot)}
                            onPick=${() => sheet(LOOK_NAMES.parent, (close) => html`
                                <${ParentSheet} name=${s.session} live=${s} snapshot=${snapshot} exec=${exec} onDone=${close} />`)} />`;
}

function pinItem(s, snapshot, exec, run, wait) {
    const pin = pinOf(s, (snapshot && snapshot.sessions) || [], wait);
    return html`<${MenuItem} key="pin" label=${pin.label} tip=${pin.tip} note=${pin.why}
                            off=${pin.why || (knows(exec, "session.pin") ? "" : whyNot(exec, "session.pin"))}
                            onPick=${() => run("session.pin", pin.root.session, { on: !pin.on })} />`;
}

function OutsideMenu({ s, onOpen, snapshot }) {
    const toast = useToast();
    return html`<${MenuList} groups=${[
        [html`<${MenuItem} key="open" label="Open" onPick=${onOpen} />`],
        copies(s, toast, snapshot),
    ]} />`;
}

// endsOf says why the close and the restart of a session cannot be pressed
// now: under way already, or not known to the host.
function endsOf(exec, closing, restarting) {
    return {
        closeWhy: closing || restarting || (knows(exec, "session.close") ? "" : whyNot(exec, "session.close")),
        restartWhy: restarting || closing || (knows(exec, "session.restart") ? "" : whyNot(exec, "session.restart")),
    };
}

function ThreadMenu({ s, exec, snapshot, wait, onOpen, closing, restarting }) {
    const run = useAction();
    const sheet = useMenuSheet();
    const toast = useToast();
    const { closeWhy, restartWhy } = endsOf(exec, closing, restarting);
    const stopWhy = noTurn(s) || (knows(exec, "session.stop") ? "" : whyNot(exec, "session.stop"));
    return html`<${MenuList} groups=${[
        [html`<${MenuItem} key="open" label="Open" onPick=${onOpen} />`],
        [detachItem(s, exec, run), parentItem(s, snapshot, exec, sheet), pinItem(s, snapshot, exec, run, wait)],
        [
            html`<${MenuItem} key="role" label="Role…" off=${roleWhy(exec)}
                onPick=${() => sheet(LOOK_NAMES.role, (close) => html`
                    <${RoleSheet} name=${s.session} live=${s} exec=${exec} onDone=${close} />`)} />`,
            html`<${MenuItem} key="intent" label="Session messages…" off=${intentWhy(exec)}
                onPick=${() => sheet(LOOK_NAMES.intent, (close) => html`
                    <${IntentSheet} name=${s.session} exec=${exec} onDone=${close} />`)} />`,
        ],
        copies(s, toast, snapshot),
        [
            s.status === "busy" && html`<${MenuItem} key="stop" label="Stop the turn" off=${stopWhy}
                                                     onPick=${() => run("session.stop", s.session, {})} />`,
            html`<${MenuItem} key="restart" label="Restart the session" off=${codexRestartWhy(s) || restartWhy} tip=${CODEX_RESTART}
                              onPick=${() => run("session.restart", s.session, {})} />`,
            html`<${MenuItem} key="close" label="Close the session" off=${closeWhy} tip=${CODEX_CLOSE}
                              onPick=${() => run("session.close", s.session, {})} />`,
        ],
    ]} />`;
}

function ClaudeMenu({ s, exec, snapshot, wait, onOpen, onTerm, closing, restarting, lost }) {
    const run = useAction();
    const { closeWhy, restartWhy } = endsOf(exec, closing, restarting);
    const toast = useToast();
    const sheet = useMenuSheet();
    const way = useSwitchWay(s.session, s.transport);
    const [win, askWindow] = useWindow(s.session, s.transport);
    const rc = useRemote({ name: s.session, live: s, exec, snapshot });
    const place = (s.project && s.project.path) || s.cwd || "";
    const jump = useTermJump(place, exec, onTerm);

    // Where the session can move is asked as the menu opens: until the panel
    // answers, the move it could make from where it lives stands dimmed.
    const sides = sidesOf({ live: s, way, held: win.kind === "open", picked: "feed", canTerm: false, exec });
    const to = way.pending ? (s.transport === "stream" ? "console" : "stream") : sides.moves;
    const onHost = windowOf({ name: s.session, live: s, work: null, exec, win, way, run, onChange: askWindow });
    const stops = lost ? `stops ${lost}` : "";
    const rename = () => sheet(LOOK_NAMES.rename, (close) => html`
        <${RenameSheet} name=${s.session} exec=${exec} onDone=${close}
                        taken=${takenNames(snapshot)} />
    `);

    // Stop follows the identity settings so optional transport and copy items
    // cannot push it beyond the menu's digit keys.
    return html`<${MenuList} groups=${[
        [
            html`<${MenuItem} key="open" label="Open" onPick=${onOpen} />`,
            onTerm && place && html`<${MenuItem} key="term" label="Terminal of the project" off=${whyNot(exec, "term.start")}
                                                 onPick=${jump.press} />`,
        ],
        [
            html`<${MenuItem} key="rename" label="Rename…" off=${renameWhy(s, exec)} onPick=${rename} />`,
            html`<${MenuItem} key="role" label="Role…" off=${roleWhy(exec)}
                onPick=${() => sheet(LOOK_NAMES.role, (close) => html`
                    <${RoleSheet} name=${s.session} live=${s} exec=${exec} onDone=${close} />`)} />`,
            html`<${MenuItem} key="intent" label="Session messages…" off=${intentWhy(exec)}
                onPick=${() => sheet(LOOK_NAMES.intent, (close) => html`
                    <${IntentSheet} name=${s.session} exec=${exec} onDone=${close} />`)} />`,
        ],
        [
            s.status === "busy" && html`<${MenuItem} key="stop" label="Stop the turn"
                                                     off=${knows(exec, "session.stop") ? "" : whyNot(exec, "session.stop")}
                                                     onPick=${() => run("session.stop", s.session, {})} />`,
            to && html`<${MenuItem} key="move" label=${to === "console" ? "Move to tmux" : "Move to the stream"}
                                    note=${way.pending ? "asking…" : stops} off=${way.pending ? way.reason : sides.why}
                                    onPick=${() => moveSession({ run, exec, name: s.session, to, lost })} />`,
            !onHost.moves && s.transport !== "stream" && html`
                <${MenuItem} key="window" label=${`${onHost.open ? "Close the window" : "Open a window"} on ${hostLabel()}`}
                             off=${onHost.why} onPick=${onHost.press} />`,
            html`<${MenuItem} key="remote" label=${rc.shown ? "Turn Remote Control off" : "Turn Remote Control on"}
                              off=${rc.why} onPick=${rc.press} />`,
            detachItem(s, exec, run),
            parentItem(s, snapshot, exec, sheet),
            pinItem(s, snapshot, exec, run, wait),
        ],
        copies(s, toast, snapshot),
        [
            html`<${MenuItem} key="restart" label="Restart the session" note=${stops} off=${restartWhy}
                              onPick=${() => run("session.restart", s.session, {})} />`,
            !s.home && html`<${MenuItem} key="close" label="Close the session" note=${stops} off=${closeWhy}
                                         onPick=${() => run("session.close", s.session, {})} />`,
        ],
    ]} />`;
}

// ClosedMenu is the menu of a closed conversation, on the shelf or in the
// archive: opened as its row opens (onOpen), resumed as Resume resumes it, a
// new session started where the archive starts one, and the ways to find it
// again, through the command of the conversation's own agent.
export function ClosedMenu({ row, exec, onOpen, snapshot }) {
    const run = useAction();
    const toast = useToast();
    return html`<${MenuList} groups=${[
        [
            html`<${MenuItem} key="open" label="Open" onPick=${onOpen} />`,
            row.sessionId && html`<${MenuItem} key="resume" label="Resume" off=${whyNot(exec, "session.resume")}
                                               onPick=${() => resume(run, row, exec)} />`,
            (row.project || projectOf(row)) && html`
                <${MenuItem} key="new" label="New session in the project" off=${whyNot(exec, "session.open")}
                             onPick=${() => startIn(run, row, exec)} />`,
        ],
        copies(row, toast, snapshot),
    ]} />`;
}

// ProjectMenu is the menu of a project's heading in the column: a new session
// in it as its plus starts one (onNew, none for the home project), its
// terminal, and its page in the settings of the map.
export function ProjectMenu({ project, profile, sessions, exec, onNew, onTerm, onSettings }) {
    const jump = useTermJump(project.path, exec, onTerm);
    const sheet = useMenuSheet();
    return html`<${MenuList} groups=${[
        [
            html`<${MenuItem} key="pause" label="Pause…" off=${pauseWhy(project, profile, sessions, exec)}
                onPick=${() => sheet("pause sessions", (close) => html`<${PauseSheet} project=${project} profile=${profile} sessions=${sessions} exec=${exec} onDone=${close} />`)} />`,
            onNew && html`<${MenuItem} key="new" label="New session" off=${whyNot(exec, "session.open")} onPick=${onNew} />`,
            onTerm && project.path && html`
                <${MenuItem} key="term" label="Terminal of the project" off=${whyNot(exec, "term.start")} onPick=${jump.press} />`,
        ],
        [onSettings && html`<${MenuItem} key="settings" label="Settings of the project" onPick=${onSettings} />`],
    ]} />`;
}
