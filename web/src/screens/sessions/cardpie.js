// The pie of a session's card on a phone: the controls a card has at a desk,
// and what the tools of its conversation do to it, out of a long press on the
// card. The first ring is what is done to the session as a whole — its close
// and its restart, its pause, its pin, the stop of its turn — and More; the
// second is the rest, by what it answers: who the session is, where it lives,
// whose it is and how it is found again. An item does what the button or the
// line of the same name does, through the same code, and says why when it
// cannot be done.
//
// The screen of the cards mounts the host once around them; a card asks it
// for its pie (useCardPie) and draws nothing of it. The host keeps the pie and
// the sheet an item opens, which outlives the pie, and gives the focus back
// to the card as they close.
import { createContext } from "preact";
import { useCallback, useContext, useEffect, useMemo, useRef, useState } from "preact/hooks";

import { html } from "../../html.js";
import { Icon } from "../../ui/icons.js";
import { Pie } from "../../ui/pie.js";
import { Sheet } from "../../ui/sheet.js";
import { useToast } from "../../ui/toasts.js";
import { useAction } from "../../actions/gate.js";
import { knows, whyNot } from "../../exec.js";
import { CODEX_STOP, isCodex, noTurn, shownName } from "../../agent.js";
import { LOOK_NAMES } from "../chat/look.js";
import { IntentSheet } from "../chat/intent.js";
import { ParentSheet } from "../chat/parent.js";
import { useRemote } from "../chat/remote.js";
import { RenameSheet, takenNames } from "../chat/rename.js";
import { RoleSheet } from "../chat/role.js";
import { sessionItems } from "../chat/sessiontools.js";
import { StatusSheet } from "../chat/status.js";
import { moveSession, sidesOf, useSwitchWay } from "../chat/switch.js";
import { useWindow } from "../chat/window.js";
import { useTermJump } from "../terms/jump.js";
import { busyOf, closeSession, endsOf, pinGroup, restartSession } from "./controls.js";
import { openedBy } from "./kin.js";
import { PauseSheet, continueSession, sessionPauseWhy } from "./pause.js";
import { pinOf } from "./pin.js";

const CardPieContext = createContext(null);

// useCardPie returns the host's open({ name, profile, card, press }) — the
// pie of the session name, raised from the card element, with the pointer of
// the press still held — or null where no host stands.
export function useCardPie() {
    return useContext(CardPieContext);
}

// The icon and the word of a sector, for the items of the session's tools,
// and the note of the plate where the line's own is longer than a plate
// over the ring holds.
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

// The sheets an item opens, by the look of the conversation they stand for.
function sheetOf(kind, { s, profile, snapshot, exec, onDone }) {
    const name = s.session;
    switch (kind) {
    case "pause":
        return { label: "pause sessions", tall: true, node: html`
            <${PauseSheet} project=${s.project} profile=${profile} sessions=${(snapshot && snapshot.sessions) || [s]}
                           session=${s} exec=${exec} onDone=${onDone} />` };
    case "rename":
        return { label: LOOK_NAMES.rename, node: html`<${RenameSheet} name=${name} exec=${exec} taken=${takenNames(snapshot)} onDone=${onDone} />` };
    case "role":
        return { label: LOOK_NAMES.role, node: html`<${RoleSheet} name=${name} live=${s} exec=${exec} onDone=${onDone} />` };
    case "intent":
        return { label: LOOK_NAMES.intent, node: html`<${IntentSheet} name=${name} exec=${exec} onDone=${onDone} />` };
    case "status":
        return { label: LOOK_NAMES.status, node: html`<${StatusSheet} name=${name} live=${s} />` };
    case "parent":
        return { label: LOOK_NAMES.parent, node: html`<${ParentSheet} name=${name} live=${s} snapshot=${snapshot} exec=${exec} onDone=${onDone} />` };
    default:
        return null;
    }
}

// CardPieHost holds the pie of the cards drawn inside it, and the sheet one
// of its items opened. A session that goes while its pie is open takes the
// pie with it, and its sheet.
export function CardPieHost({ snapshot, exec, wait, onTerm, children }) {
    const [pie, setPie] = useState(null);
    const [sheet, setSheet] = useState(null);
    const from = useRef(null);
    const seq = useRef(0);

    const back = useCallback(() => {
        const el = from.current;
        if (el && el.isConnected) el.focus({ preventScroll: true });
    }, []);
    const open = useCallback(({ name, profile = null, card = null, press = null }) => {
        setPie((was) => {
            if (was) return was;
            from.current = card ? card.querySelector(".scopen") : null;
            seq.current += 1;
            return { name, profile, card, press, key: seq.current };
        });
    }, []);
    const host = useMemo(() => ({ open }), [open]);
    const close = useCallback(() => {
        setPie(null);
        back();
    }, [back]);
    const closeSheet = useCallback(() => {
        setSheet(null);
        back();
    }, [back]);

    const sessions = (snapshot && snapshot.sessions) || [];
    const s = pie ? sessions.find((x) => x.session === pie.name) : null;
    const of = sheet ? sessions.find((x) => x.session === sheet.name) : null;
    useEffect(() => {
        if (pie && !s) close();
    }, [pie, s, close]);
    useEffect(() => {
        if (sheet && !of) closeSheet();
    }, [sheet, of, closeSheet]);

    const shown = sheet && of ? sheetOf(sheet.kind, { s: of, profile: sheet.profile, snapshot, exec, onDone: closeSheet }) : null;
    return html`
        <${CardPieContext.Provider} value=${host}>
            ${children}
            ${pie && s && !s.outside && html`
                <${CardPie} key=${pie.key} s=${s} profile=${pie.profile} card=${pie.card} press=${pie.press}
                            snapshot=${snapshot} exec=${exec} wait=${wait} onTerm=${onTerm} onClose=${close}
                            onSheet=${(kind) => setSheet({ kind, name: s.session, profile: pie.profile })} />
            `}
            ${shown && html`
                <${Sheet} open tall=${Boolean(shown.tall)} onClose=${closeSheet} label=${shown.label}>${shown.node}<//>
            `}
        <//>
    `;
}

// CardPie is the pie of one live session. What the host is asked about the
// session — which way it can move, whether a window shows it — is asked as
// the pie opens, as the menu of a right click asks it at a desk.
function CardPie({ s, profile, card, press, snapshot, exec, wait, onTerm, onClose, onSheet }) {
    const run = useAction();
    const toast = useToast();
    const codex = isCodex(s);
    const name = s.session;
    const sessions = (snapshot && snapshot.sessions) || [s];

    const pin = pinOf(s, sessions, wait);
    const ends = endsOf({ s, exec, pin, busy: Boolean(busyOf(s, wait) || s.restarting) });
    const project = s.project && s.project.id ? s.project : null;
    const pauseWhy = sessionPauseWhy(s, project, exec);
    const turn = codex ? noTurn(s) : s.status === "busy" ? "" : "No turn is running";
    const stopWhy = turn || (knows(exec, "session.stop") ? "" : whyNot(exec, "session.stop"));

    const way = useSwitchWay(codex ? "" : name, s.transport);
    const [win] = useWindow(codex ? "" : name, s.transport);
    const remote = useRemote({ name, live: s, exec, snapshot });
    const place = (s.project && s.project.path) || s.cwd || "";
    const jump = useTermJump(place, exec, onTerm);

    const tools = Object.fromEntries(sessionItems({ name, live: s, exec, snapshot, cwd: s.cwd || "", run, toast })
        .map((it) => {
            const { note, ...sector } = SECTORS[it.key];
            return [it.key, {
                key: it.key, ...sector, title: it.label, note: note ? note(s) : it.note, off: it.off,
                pick: it.look ? () => onSheet(it.look) : it.act || it.copy,
            }];
        }));

    // Where the session can move is asked as the pie opens: until the panel
    // answers, the move from where it lives stands dimmed, so no item of the
    // ring changes its place under the finger.
    const sides = sidesOf({ live: s, way, held: win.kind === "open", picked: "feed", canTerm: false, exec });
    const to = way.pending || !sides.moves ? (s.transport === "stream" ? "console" : "stream") : sides.moves;
    const lost = ends.lost;
    const move = !codex && {
        key: "move", icon: Icon.terminal, word: to === "console" ? "To tmux" : "To stream",
        title: to === "console" ? "Move to tmux" : "Move to the stream", note: lost ? `stops ${lost}` : "",
        off: way.pending ? way.reason : sides.moves ? sides.why : (sides.tip || way.reason || "the session cannot move from where it lives"),
        pick: () => moveSession({ run, exec, name, to, lost }),
    };
    const rc = !codex && {
        key: "remote", icon: Icon.remote, word: "RC", on: remote.shown,
        title: remote.shown ? "Turn Remote Control off" : "Turn Remote Control on",
        note: remote.shown ? "on: the session is open on claude.ai too" : "off", off: remote.why, pick: remote.press,
    };
    const term = onTerm && place && {
        key: "term", icon: Icon.prompt, word: "Terminal", title: "Terminal of the project",
        note: "the one typed into last, or a new one", off: whyNot(exec, "term.start"), pick: jump.press,
    };
    const second = [tools.rename, tools.role, tools.intent, tools.status, move, rc, term,
        tools.parent, tools.detach, tools.id, tools.resume].filter(Boolean);

    // Clockwise from the top: what ends something stands farthest from the
    // thumb, what is pressed often and undone easily nearest to it. The home
    // session is never closed, and its slot stays empty, so the rest keep
    // their places.
    const first = [
        s.home ? null : {
            key: "close", icon: Icon.close, word: "Close", title: "Close the session", note: ends.closeNote,
            off: ends.closeWhy, tap: true, danger: true, pick: () => closeSession(run, s),
        },
        {
            key: "restart", icon: Icon.refresh, word: "Restart", title: "Restart the session", note: ends.restartNote,
            off: ends.restartWhy, pick: () => restartSession(run, s),
        },
        s.pauseAlarm ? {
            key: "pause", icon: Icon.play, word: "Continue", title: "Continue now", note: "before its pause ends",
            off: pauseWhy, pick: () => continueSession(run, project, s),
        } : {
            key: "pause", icon: Icon.pause, word: "Pause", title: "Pause…", note: "until a time or a word, on its sheet",
            off: pauseWhy, pick: () => onSheet("pause"),
        },
        {
            key: "pin", icon: Icon.thumbtack, word: pin.on ? "Unpin" : "Pin", title: pin.label, note: pin.tip, on: pin.on,
            off: ends.pinWhy, pick: () => pinGroup(run, pin),
        },
        { key: "more", icon: Icon.more, word: "More", title: "More", note: "who it is, where it lives, whose it is, how to find it", items: second },
        {
            key: "stop", icon: Icon.stopsquare, word: "Stop", title: "Stop the turn",
            note: codex ? CODEX_STOP : "breaks off the answer it is writing; the session stays",
            off: stopWhy, short: turn ? "no turn" : "", tap: true, danger: true, pick: () => run("session.stop", name, {}),
        },
    ];

    return html`<${Pie} label=${`session ${shownName(s)}`} name=${shownName(s)} items=${first} lift=${card} press=${press}
                        onClose=${onClose} />`;
}
