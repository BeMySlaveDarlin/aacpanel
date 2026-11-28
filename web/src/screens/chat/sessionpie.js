import { Icon } from "../../ui/icons.js";
import { knows, whyNot } from "../../exec.js";
import { hostLabel } from "../../actions/registry.js";
import { CODEX_STOP, isCodex, noTurn } from "../../agent.js";
import { closeSession, restartSession } from "../sessions/controls.js";
import { openedBy } from "../sessions/kin.js";
import { sessionItems } from "./sessiontools.js";
import { moveSession, sidesOf, stops } from "./switch.js";
import { windowOf } from "./window.js";

const SECTORS = {
    rename: { icon: Icon.pencil, word: "Rename" },
    role: { icon: Icon.badge, word: "Role" },
    intent: { icon: Icon.envelope, word: "Messages" },
    status: { icon: Icon.info, word: "Info" },
    parent: { icon: Icon.flow, word: "Stand under", lines: ["Stand", "under"] },
    detach: { icon: Icon.exit, word: "Detach", note: (s) => `under ${openedBy(s)}` },
    id: { icon: Icon.copy, word: "Copy ID" },
    resume: { icon: Icon.resume, word: "Resume cmd", lines: ["Resume", "cmd"] },
};

// Both pies use these sectors. The conversation adds the choices the phone's
// tools sheet owns; a card keeps its familiar ring and direct move.
export function sessionSectors({ name, live, exec, snapshot, cwd, run, toast, onLook, remote, way, win,
    lost = "", work, onWindow, desktop = false }) {
    const codex = isCodex(live);
    const tools = Object.fromEntries(sessionItems({ name, live, exec, snapshot, cwd, run, toast }).map((it) => {
        const { note, ...sector } = SECTORS[it.key];
        return [it.key, {
            key: it.key, ...sector, title: it.label, note: note ? note(live) : it.note, off: it.off,
            pick: it.look ? () => onLook(it.look) : it.act || it.copy,
        }];
    }));
    if (live.outside) return codex ? [tools.id, tools.resume].filter(Boolean) : Object.values(tools);

    // Keep the move's slot dimmed while the panel answers, so it never
    // appears under a pointer already choosing another sector.
    const sides = sidesOf({ live, way, held: win.kind === "open", picked: "feed", canTerm: false, exec });
    const to = way.pending || !sides.moves ? (live.transport === "stream" ? "console" : "stream") : sides.moves;
    const gone = work === undefined ? lost : stops(work);
    const note = gone ? (desktop ? `Stops ${gone} — they do not come back after the move` : `stops ${gone}`) : "";
    const move = !codex && {
        key: "move", icon: Icon.terminal, word: to === "console" ? "To tmux" : "To stream",
        title: to === "console" ? "Move to tmux" : "Move to the stream", note,
        off: way.pending ? way.reason : sides.moves ? sides.why : (sides.tip || way.reason || "the session cannot move from where it lives"),
        pick: () => moveSession({ run, exec, name, to, lost: gone }),
    };
    if (desktop && move?.off && note) move.off += ` · ${note}`;
    if (desktop && move && to === "console") {
        move.items = [
            { ...move, key: "move-only", word: "Move", lines: ["Move", "to tmux"] },
            { ...move, key: "move-window", icon: Icon.monitor, word: "Move + window", lines: ["Move +", "window"],
                title: "Move and open a window", note: `${note}${note ? " · " : ""}Open a window on ${hostLabel()} after the move`,
                off: move.off || whyNot(exec, "window.open"),
                pick: () => moveSession({ run, exec, name, to, lost: gone, withWindow: true }) },
        ];
    }
    const rc = !codex && {
        key: "remote", icon: Icon.remote, word: "RC", on: remote.shown,
        title: remote.shown ? "Turn Remote Control off" : "Turn Remote Control on",
        note: remote.shown ? (desktop && live.remote ? `on: ${live.remote}` : "on: the session is open on claude.ai too") : "off",
        off: remote.why || (desktop && remote.off ? "waiting for Remote Control to change" : ""), pick: remote.press,
    };
    const window = desktop && !codex && live.transport !== "stream" && windowOf({ name, live, exec, win, way, run, work, onChange: onWindow });
    return [tools.rename, tools.role, tools.intent, tools.status, move, rc,
        desktop && rc && remote.shown && live.remote && {
            key: "remote-open", icon: Icon.globe, word: "claude.ai", title: "Open in claude.ai", note: live.remote,
            pick: () => globalThis.open(live.remote, "_blank", "noopener"),
        },
        window && { key: "window", icon: Icon.monitor, word: window.open ? "Close window" : "Open window",
            title: `${window.open ? "Close" : "Open"} the window on ${hostLabel()}`, note: window.open ? "open" : "closed",
            off: window.why, pick: window.press },
        desktop && codex && { key: "mcp", icon: Icon.plug, word: "MCP", title: "MCP servers",
            note: "what codex can reach, as it reports it", pick: () => onLook("mcp") },
        desktop && codex && { key: "skills", icon: Icon.skill, word: "Skills", title: "Skills",
            note: "read-only: set in codex's config", pick: () => onLook("skills") },
        tools.parent, tools.detach, tools.id, tools.resume].filter(Boolean);
}

export function sessionEndSectors({ name, live, exec, run, ends }) {
    const turn = isCodex(live) ? noTurn(live) : live.status === "busy" ? "" : "No turn is running";
    return {
        close: { key: "close", icon: Icon.close, word: "Close", title: "Close the session", note: ends.closeNote,
            off: ends.closeWhy, danger: true, pick: () => closeSession(run, { ...live, session: name }) },
        restart: { key: "restart", icon: Icon.refresh, word: "Restart", title: "Restart the session", note: ends.restartNote,
            off: ends.restartWhy, pick: () => restartSession(run, { ...live, session: name }) },
        stop: { key: "stop", icon: Icon.stopsquare, word: "Stop", title: "Stop the turn",
            note: isCodex(live) ? CODEX_STOP : "breaks off the answer it is writing; the session stays",
            off: turn || (knows(exec, "session.stop") ? "" : whyNot(exec, "session.stop")), short: turn ? "no turn" : "", danger: true,
            pick: () => run("session.stop", name, {}) },
    };
}
