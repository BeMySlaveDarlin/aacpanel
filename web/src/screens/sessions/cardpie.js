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
import { whyNot } from "../../exec.js";
import { isCodex, shownName } from "../../agent.js";
import { LOOK_NAMES } from "../chat/look.js";
import { IntentSheet } from "../chat/intent.js";
import { ParentSheet } from "../chat/parent.js";
import { useRemote } from "../chat/remote.js";
import { RenameSheet, takenNames } from "../chat/rename.js";
import { RoleSheet } from "../chat/role.js";
import { sessionEndSectors, sessionSectors } from "../chat/sessionpie.js";
import { StatusSheet } from "../chat/status.js";
import { useSwitchWay } from "../chat/switch.js";
import { useWindow } from "../chat/window.js";
import { useTermJump } from "../terms/jump.js";
import { busyOf, endsOf, pinGroup } from "./controls.js";
import { sessionPauseWhy } from "./pause.js";
import { usePauseDial } from "./pausedial.js";
import { pinOf } from "./pin.js";

const CardPieContext = createContext(null);

// useCardPie returns the host's open({ name, profile, card, press, spot }) —
// the pie of the session name, raised from the card element, with the pointer
// of the press still held, or at a desk around the point of a right click
// (spot) — or null where no host stands.
export function useCardPie() {
    return useContext(CardPieContext);
}

// The sheets an item opens, by the look of the conversation they stand for.
function sheetOf(kind, { s, profile, snapshot, exec, onDone }) {
    const name = s.session;
    switch (kind) {
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
    const open = useCallback(({ name, profile = null, card = null, press = null, spot = null }) => {
        setPie((was) => {
            if (was) return was;
            from.current = card ? card.querySelector(".scopen") : null;
            seq.current += 1;
            return { name, profile, card, press, spot, key: seq.current };
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
                <${CardPie} key=${pie.key} s=${s} profile=${pie.profile} card=${pie.card} press=${pie.press} spot=${pie.spot}
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
function CardPie({ s, profile, card, press, spot, snapshot, exec, wait, onTerm, onClose, onSheet }) {
    const run = useAction();
    const toast = useToast();
    const codex = isCodex(s);
    const name = s.session;
    const sessions = (snapshot && snapshot.sessions) || [s];

    const pin = pinOf(s, sessions, wait);
    const ends = endsOf({ s, exec, pin, busy: Boolean(busyOf(s, wait) || s.restarting) });
    const project = s.project && s.project.id ? s.project : null;
    const pauseWhy = sessionPauseWhy(s, project, exec);
    const pause = usePauseDial({ project, profile, sessions, session: s, exec, onClose });
    const end = sessionEndSectors({ name, live: s, exec, run, ends });

    const way = useSwitchWay(codex ? "" : name, s.transport);
    const [win] = useWindow(codex ? "" : name, s.transport);
    const remote = useRemote({ name, live: s, exec, snapshot });
    const place = (s.project && s.project.path) || s.cwd || "";
    const jump = useTermJump(place, exec, onTerm);

    const tools = sessionSectors({ name, live: s, exec, snapshot, cwd: s.cwd || "", run, toast,
        onLook: onSheet, way, win, remote, lost: ends.lost });
    const term = onTerm && place && {
        key: "term", icon: Icon.prompt, word: "Terminal", title: "Terminal of the project",
        note: "the one typed into last, or a new one", off: whyNot(exec, "term.start"), pick: jump.press,
    };
    const tail = tools.findIndex((it) => ["parent", "detach", "id", "resume"].includes(it.key));
    const split = tail < 0 ? tools.length : tail;
    const second = [...tools.slice(0, split), term, ...tools.slice(split)].filter(Boolean);

    // Clockwise from the top: what ends something stands farthest from the
    // thumb, what is pressed often and undone easily nearest to it. The home
    // session is never closed, and its slot stays empty, so the rest keep
    // their places.
    const first = [
        s.home ? null : { ...end.close, tap: true },
        end.restart,
        s.pauseAlarm ? {
            key: "pause", icon: Icon.play, word: "Continue", title: "Continue now", note: "before its pause ends",
            off: pauseWhy, stay: true, pick: pause.activate,
        } : {
            key: "pause", icon: Icon.pause, word: "Pause", title: "Pause…", note: "pick its end on the dial, then confirm below",
            off: pauseWhy, stay: true, pick: pause.activate,
        },
        {
            key: "pin", icon: Icon.thumbtack, word: pin.on ? "Unpin" : "Pin", title: pin.label, note: pin.tip, on: pin.on,
            off: ends.pinWhy, pick: () => pinGroup(run, pin),
        },
        { key: "more", icon: Icon.more, word: "More", title: "More", note: "who it is, where it lives, whose it is, how to find it", items: second },
        { ...end.stop, tap: true },
    ];

    const items = s.sleeping ? first.map((item) => item && !["close", "pause"].includes(item.key)
        ? { ...item, off: "sleeping", items: item.items?.map((sub) => ({ ...sub, off: "sleeping" })) } : item) : first;
    return html`<${Pie} label=${`session ${shownName(s)}`} name=${shownName(s)} items=${items} lift=${card} press=${press}
                        spot=${spot} focusFirst=${!press && !spot} crown=${pause.crown} bar=${pause.bar} layer=${pause.layer} onClose=${onClose} />`;
}
