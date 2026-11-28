// The conversation of a live or closed session, as both agents have it: the
// header, the feed with its window and search, the composer and what stands
// over it, the deck under it, and the sheets over the run. What is the agent's
// own comes in its parts — see claude.js and codex.js: where the session lives
// and what it is watched with, what the composer reads in the words, the band
// under the field, the sheets of its settings, and the sections of its tools.

import { useCallback, useEffect, useMemo, useRef, useState } from "preact/hooks";

import { list as actList } from "../../data/acts.js";
import { settleWaiting, useWaiting } from "../../waiting.js";
import { commandWindow, useWindowTimer, windowMoments } from "../../commandwindow.js";
import { clearWaitingTargets, RequestCard, revealWaiting } from "./request.js";
import { actOpen, agentsMove, commandRunning, isChain, personsMove } from "./chain.js";
import { html } from "../../html.js";
import { BackHead, useBackClose } from "../../ui/back.js";
import { Sheet } from "../../ui/sheet.js";
import { ParentSheet } from "./parent.js";
import { useToast } from "../../ui/toasts.js";
import * as codecopy from "./copy.js";
import { ContextBar } from "../../ui/bar.js";
import { Ask } from "../ask.js";
import { Permit } from "./permit.js";
import { ago, tokens } from "../../format.js";
import { Icon } from "../../ui/icons.js";
import { closed, lastPos, outboxRows, queuedLocal, queuedRows, rows, runCalls, turnCalls, unarrived, weld } from "./feed.js";
import { JumpToEnd, JumpToPrevious, useFeedWindow } from "./feedwindow.js";
import { FindBar, useFeedFind } from "./find.js";
import { agentFeedId, SubChat } from "./subchat.js";
import { FilesButton, RepoView, useRepo } from "../repo/view.js";
import { onShelf, sealed, signal } from "../repo/notes.js";
import { FeedRow, FeedWrites } from "./rows.js";
import { useSteady } from "../../ui/pure.js";
import { callsOf, FeedGrid } from "./timeline.js";
import { CommandSheet } from "./command.js";
import { blockCommand, canRunShell, sendShell, ShellBar, shellNow, ShellSheet } from "./shell.js";
import { SecretPad } from "./secret.js";
import { Calls } from "./calls.js";
import { Look, LOOK_NAMES, pageLook, WORK_LISTS } from "./look.js";
import { ArtifactPage } from "../artifact.js";
import { Brief } from "../brief.js";
import { index, shelf as pageShelf } from "../../data/artifacts.js";
import { shelf as briefShelf } from "../../data/briefs.js";
import { list as secretShelf } from "../../data/secrets.js";
import { hasWork, WorkList, WorkRefs, WorkStatus } from "./work.js";
import { ChecklistSheet } from "./checklist.js";
import { CommandsChip, CommandsSheet } from "./commands.js";
import { QuickCommands } from "./quick.js";
import { moveKey, movesOf, MovePill, withActReplies } from "./actform.js";
import { BackgroundActions } from "./claudebg.js";
import { Composer, deliver, messageID, outcome } from "./composer.js";
import { knows, whyNot } from "../../exec.js";
import { ANSWER_LAG_MS, answered, hidesAsk, lagging, recall, remember, settle } from "./answered.js";
import { useAction } from "../../actions/gate.js";
import { QuoteTip, useSelectionQuote } from "./quotetip.js";
import { modelTitle, shortPath, stateOf } from "./head.js";
import { HeadName } from "./headname.js";
import { takenNames } from "./rename.js";
import { SessionButton } from "./sessionbutton.js";
import { MoreButton, SessionTools, ViewTabs } from "./sessiontools.js";
import { AttachSheet } from "./tools.js";
import { DeskHead } from "./deskhead.js";
import { MoveScreen } from "./switch.js";
import { outsideNote } from "../sessions/kin.js";
import { resume } from "../sessions/actions.js";
import { TakeBack } from "./takeback.js";
import { usePieceMenu } from "./feedmenu.js";
import { Term } from "./term.js";
import { TermJump } from "../terms/jump.js";
import { useViewing } from "../../viewing.js";
import { useWide } from "../../ui/wide.js";
import { useAsOf } from "../../ui/asof.js";
import { agentKey, agentName, isCodex, shownName, spoke } from "../../agent.js";
import { PlaceContext } from "./place.js";

// Conversation draws the screen; parts carry what its agent brings:
//
//   term         whether the installation has a terminal at all
//   place        where the session lives and what it is watched with:
//                view, sides, win, way, move, canTerm, pickView, onWindow
//   toBackground what a call the turn waits on is sent to the background by,
//                or null where nothing is
//   takesBack    whether a queued message can be taken back from its queue
//   askStream    whether a question of the agent is answered by its protocol
//                rather than by keys in a terminal
//   work         ctx → what runs beside the turn: claude's agents, workflows
//                and tasks, codex's background processes
//   sections     the sections of the session's tools
//   composer     ctx → what the composer reads in the words beyond them
//   strip        ctx → the band under the field, after the paperclip
//   look         (look, ctx) → { label, node } for a sheet of its own kind
//   sheets       ctx → the sheets of its settings
//   aside        ctx → what stands beside the feed
//
// ctx is what the parts may reach of the screen: whether it is wide, the view,
// whether the feed shows and its box, the work of the turn, and the setters of
// the sheet over the run and of what goes into the composer.
//
// toAct is the act a tap on a push asked to be shown, as { id }: a new object
// at every tap, so the same act asked for twice is shown twice.
export function Conversation({ name, id, live, archive, exec, snapshot, onBack, onUsage, onOpenChat, onTerm, toAct, parts }) {
    const toast = useToast();
    useViewing(live ? name : "");
    const [sub, setSub] = useState(null);
    const {
        state, shown, away, more, later, feedRef, bindFeed, topRef, bottomRef, onScroll, atEnd, toEnd, show, hasPrevious, toPrevious,
    } = useFeedWindow({ name, id, live: live && !sub });
    const [calls, setCalls] = useState(null);
    const [look, setLook] = useState(null);
    const [quick, setQuick] = useState(null);
    // The pages this conversation published and the panel kept a copy of. The
    // shelf is asked for once: a card looks itself up in it rather than asking
    // per artifact.
    const [copies, setCopies] = useState(null);
    const [pages, setPages] = useState([]);
    const [briefs, setBriefs] = useState([]);
    // The secrets of the host, asked only by a conversation that has a card
    // of one: the card says when its file was saved, the notepad what it
    // would replace.
    const [secrets, setSecrets] = useState(null);
    const [acts, setActs] = useState({});
    useWindowTimer(windowMoments(Object.values(acts)));
    const [actError, setActError] = useState("");
    const [actRefresh, refreshActs] = useState(0);
    const actRunning = Object.values(acts).some(commandRunning);
    const waitingItems = useWaiting(live, true, true);
    useEffect(() => clearWaitingTargets(waitingItems, feedRef.current), [waitingItems]);
    const actCards = [...state.items, ...(away ? shown : [])].filter((item) => ["request", "secret", "ask"].includes(item.role) || item.claim || item.re).map((item) => item.id || item.claim || item.re).join("|");
    useEffect(() => {
        if (!id) return;
        let active = true;
        actList(id).then((list) => {
            if (!active) return;
            setActs(Object.fromEntries(list.map((row) => [row.id, row])));
            setActError("");
            for (const row of list) if (!actOpen(row)) settleWaiting(live, row.kind === "do" ? "request" : row.kind, row.id);
        }).catch((err) => { if (active) setActError(String(err.message || err)); });
        return () => { active = false; };
    }, [id, actCards, live && live.waiting, live && live.claims, actRefresh]);
    useEffect(() => {
        if (!actRunning) return;
        const timer = setInterval(() => refreshActs((n) => n + 1), 2000);
        return () => clearInterval(timer);
    }, [id, actRunning]);
    const [local, setLocal] = useState([]);
    const [taken, setTaken] = useState(new Set());
    const [, redraw] = useState(0);
    const answer = recall(name);
    const mark = (next) => {
        remember(name, next);
        redraw((n) => n + 1);
    };
    const run = useAction();
    const answerRequest = async (request, answer, note, step) => {
        const result = await deliver(run, name, { act: request, answer, note, ...(step ? { step } : {}) });
        if (!result.ok) return;
        if (step) { refreshActs((n) => n + 1); return; }
        setActs((was) => ({ ...was, [request]: { ...was[request], state: "answered", answered: new Date().toISOString(), outcome: answer, note } }));
        settleWaiting(live, "request", request);
    };
    const runChain = async (kind, act, step) => {
        const result = await run(kind, name, { act, step });
        if (result.ok) refreshActs((n) => n + 1);
    };
    const cancelChain = async (act, note) => {
        const result = await run("act.cancel", name, { act, note });
        if (result.ok) refreshActs((n) => n + 1);
    };
    const withdrawClaim = useSteady(async (act) => {
        const result = await run("act.withdraw", act.session, { act: act.id });
        if (result.ok) setActs((was) => ({ ...was, [act.id]: { ...was[act.id], state: "withdrawn", withdrawn: new Date().toISOString() } }));
    });
    const pickWaiting = async (item) => {
        try {
            if (!await revealWaiting(item, show, feedRef.current)) toast("The card is not in the transcript yet");
        } catch (err) { toast("Could not open the card", String(err.message || err), true); }
    };
    const [selected, setSelected] = useState("");
    // An answer to a question of claude, from the moment it went until the
    // transcript draws its card; a minute at most, if the card never comes.
    const [sentAsk, setSentAsk] = useState(null);
    const sentShown = Boolean(sentAsk && !closed(state.items, sentAsk.use));
    useEffect(() => {
        if (!sentAsk) return undefined;
        if (!sentShown) { setSentAsk(null); return undefined; }
        const t = setTimeout(() => setSentAsk(null), 60000);
        return () => clearTimeout(t);
    }, [sentAsk, sentShown]);
    const [files, setFiles] = useState([]);
    const [asking, setAsking] = useState(false);

    const wide = useWide();
    const still = useAsOf();
    const { term, place } = parts;
    const { view, sides, win, way, move, canTerm, pickView } = place;
    // A session between its sides has neither: the screen shows the move
    // until the other side is up.
    const feedShown = !move && view !== "term";
    // The session panel of the wide screen: a view that is reached only by a
    // move opens it on the move.
    const [panel, setPanel] = useState(false);

    // Everything a conversation made is filtered by the directory it worked in.
    // A session that has ended leaves its pages and its briefs to the next one
    // in the same place, which is the session that carries the work on.
    const here = (live && live.cwd) || (archive && archive.cwd) || "";
    const repo = useRepo({ cwd: here, name, onFile: () => pickView("feed") });
    // Without a directory there is nothing to match on, and showing everything
    // would put the documents of other projects into this conversation.
    const mine = useCallback((cards) => (here
        ? (cards || []).filter((c) => c.cwd === here)
        : []), [here]);
    const myPages = useMemo(() => mine(pages), [mine, pages]);
    const myBriefs = useMemo(() => mine(briefs), [mine, briefs]);

    const quote = useSelectionQuote();
    const [insert, setInsert] = useState(null);
    const takeQuote = (text) => {
        setInsert({ key: Date.now(), text });
        const sel = document.getSelection();
        if (sel) sel.removeAllRanges();
        if (look) setLook(null);
    };

    // The shelves are read once per conversation, and they are read whole: a
    // brief and a page outlive the session that made them, and the work in a
    // directory is carried on by whoever sits there next. What belongs to this
    // conversation is decided below, by the directory rather than by the id.
    useEffect(() => {
        if (!id) {
            setCopies(null);
            return undefined;
        }
        let alive = true;
        pageShelf()
            .then((cards) => {
                if (!alive) return;
                setCopies(index(cards));
                setPages(cards);
            })
            // A collector without the copies, or one that is down, leaves the
            // cards exactly as they were: a link out and nothing broken.
            .catch(() => {
                if (!alive) return;
                setCopies(null);
                setPages([]);
            });
        briefShelf()
            .then((cards) => alive && setBriefs(cards))
            .catch(() => alive && setBriefs([]));
        return () => { alive = false; };
    }, [id]);

    // Answers sent from here mark their card at once: the shelf was read when
    // the conversation opened, and its own mark is written after the send.
    const sentBrief = (briefId) => {
        settleWaiting(live, "brief", briefId);
        setBriefs((was) => was.map((card) => (card.id === briefId ? { ...card, sent: true } : card)));
        setLook(null);
    };

    const asksSecret = Object.values(acts).some((act) => act.kind === "secret" && act.state === "open") || state.items.some((item) => item.role === "secret")
        || (away && shown.some((item) => item.role === "secret"));
    const loadSecrets = useCallback(() => {
        secretShelf().then(setSecrets).catch(() => setSecrets(null));
    }, []);
    useEffect(() => {
        if (asksSecret) loadSecrets();
    }, [asksSecret, id, loadSecrets]);
    const openSecret = useCallback((card) => {
        if (card.id) setSelected(moveKey({ ...card, kind: "secret" }));
        else setLook({ kind: "secret", item: card });
        loadSecrets();
    }, [loadSecrets]);

    useEffect(() => {
        setSub(null);
        setSecrets(null);
        setActs({});
        setActError("");
        setLocal([]);
        setTaken(new Set());
        setFiles([]);
        setCalls(null);
        setLook(null);
        setPanel(false);
        setInsert(null);
        setSelected("");
    }, [name, id]);

    const backChat = useBackClose(true, onBack);
    // The search of the conversation: the bar over the feed, Ctrl+F at a
    // keyboard, an item of the tools on a phone.
    const find = useFeedFind({ name, id, feedRef, show, on: feedShown && !sub && !repo.file && (wide || !repo.open), under: backChat });


    // The rows still on their way are what the feed draws after its items;
    // the ones the transcript has echoed leave the state afterwards, and the
    // screen never shows a message twice in between.
    const queue = state.work && state.work.queue;
    const settled = queuedLocal(local, queue);
    const pending = unarrived(settled, state.items);
    const waiting = [...pending, ...outboxRows(queue, pending, taken, state.items)];
    useEffect(() => {
        if (pending.length === local.length && pending.every((row, i) => row === local[i])) return;
        setLocal((was) => unarrived(queuedLocal(was, queue), state.items));
    }, [state.items, local, queue]);

    // Words of the feed put back into the composer to be written again: a
    // message taken back from the queue, or one written before.
    const putBack = (text) => setInsert({ key: Date.now(), text, message: true });
    // What takes a message back from the queue of the session, under it and
    // in its menu alike.
    const queueOf = (row) => (row.state === "queued" && live && parts.takesBack ? {
        name, exec, onEdit: putBack,
        onGone: () => {
            setTaken((was) => new Set([...was, row.messageId]));
            setLocal((was) => was.filter((item) => item.messageId !== row.messageId));
        },
    } : null);
    const queueTools = (row) => {
        const queue = queueOf(row);
        return queue && html`<${TakeBack} row=${row} ...${queue} />`;
    };
    const openPath = (path) => setLook({ kind: "file", path });
    const pieceMenu = usePieceMenu(openPath);

    // A row just sent grows the feed the way a new item does, and a feed at
    // its end follows it the same way — or the message stands under the edge
    // until the echo comes and the feed jumps to it.
    useEffect(() => {
        const box = feedRef.current;
        if (box && atEnd) box.scrollTop = box.scrollHeight;
    }, [waiting.length]);

    const liveStatus = live ? live.status : "";
    const liveWait = (live && live.waitingFor) || "";
    const liveStatusAt = (live && live.statusUpdatedAt) || 0;
    useEffect(() => {
        const next = settle(answer, live);
        if (next !== answer) mark(next);
    }, [name, liveStatus, liveWait, liveStatusAt]);

    useEffect(() => {
        if (!answer) return undefined;
        const left = answer.at + ANSWER_LAG_MS - Date.now();
        const timer = setTimeout(() => mark(null), Math.max(left, 0));
        return () => clearTimeout(timer);
    }, [answer]);

    const holding = lagging(answer, live);

    useEffect(() => {
        if (holding) return;
        const held = local.filter((l) => l.state === "held");
        if (!held.length) return;
        setLocal((was) => was.map((l) => (l.state === "held" ? { ...l, state: "sending" } : l)));
        (async () => {
            for (const row of held) {
                const result = await deliver(run, name, row.hold);
                setLocal((was) => was.map((l) => (l.key === row.key
                    ? { ...l, hold: undefined, ...outcome(result) }
                    : l)));
            }
        })();
    }, [holding, local]);

    const openAgent = (agent) => {
        const feedId = agentFeedId(id, agent);
        if (!feedId) return;
        setLook(null);
        setSub({ ...agent, feedId });
    };

    // What the rows of the feed are handed, and what the feed lends them,
    // keeps one identity across renders, each hand running what the latest
    // render made of it: a handler made anew by every render would draw every
    // row again (see FeedRow). The handler made further down is reached by late.
    const late = useRef({});
    const onPage = useCallback((card) => setLook({ kind: "artifact", card }), []);
    const onFile = useCallback((file, files) => setLook({ kind: "file", ...file, files }), []);
    const onCommand = useCallback((row) => setLook({ kind: "command", item: row }), []);
    const onShell = useCallback((row) => setLook({ kind: "shell", item: row }), []);
    const onAgent = useSteady(openAgent);
    const onTask = useSteady((task) => (task.agent
        ? openAgent({ id: task.id, name: task.name, kind: "background" })
        : setLook({ kind: "task", id: task.id, text: task.name })));
    const steadyAnswerRequest = useSteady(answerRequest);
    const steadyQuote = useSteady(takeQuote);
    const steadyEdit = useSteady(putBack);
    const steadyQueueOf = useSteady(queueOf);
    const steadyRun = useSteady((text, lang) => late.current.onRun(text, lang));
    const ctx = { wide, view, feedShown, feedRef, asideRef: repo.mainRef, work: state.work, pages: myPages, setLook, setInsert };
    const composer = parts.composer(ctx);
    // The feed lends these to its rows alone: a file or a brief opened beside
    // it cannot acquire the session's command buttons. What a row asks of
    // queueOf turns on whether the session takes messages back, so that is
    // what the lent things are made again on, with what decides their set.
    const writable = Boolean(live && !live.outside && live.transport !== "bg");
    const placeActions = useMemo(() => ({
        ask: writable && knows(exec, "session.send") ? (place) => {
            setInsert({ key: Date.now(), place });
            setCalls(null);
            setLook(null);
        } : null,
        open: (place) => {
            if (place.kind === "file") {
                setLook({ kind: "file", path: place.path, name: place.path.split("/").pop() });
                return;
            }
            const call = { ...place, name: place.tool };
            setCalls({ list: [call], first: call });
        },
    }), [writable, exec]);
    const runs = writable && canRunShell(exec, live.transport === "stream" || isCodex(live), composer.shell || isCodex(live));
    const takes = Boolean(live && parts.takesBack);
    const lent = useMemo(() => (writable
        ? { quote: steadyQuote, edit: steadyEdit, queueOf: steadyQueueOf, exec, onRun: runs ? steadyRun : null }
        : { queueOf: steadyQueueOf, exec }), [writable, runs, takes, exec]);

    const feed = weld(queuedRows(state.items, queue, taken));
    // What the feed draws: the tail, or a window away from the end of the
    // conversation. The work under the feed is always the tail's.
    const drawn = withActReplies(away ? weld(queuedRows(shown, queue, taken)) : feed, acts, !away);
    // The checklist of the work: on a phone a line over the composer, at a
    // desk a block at the top of the timeline column, where the room is.
    const checklist = (state.work && state.work.checklist) || null;

    const own = look ? parts.look(look, ctx) : null;
    const onLocal = (row) => setLocal((was) => [...was, { ...row, after: lastPos(state.items) }]);
    const onLocalDone = (key, patch) => setLocal((was) => was.map((l) => (l.key === key ? { ...l, ...patch } : l)));
    // An async native question settles when the session takes the message.
    const onSent = (key, patch) => {
        onLocalDone(key, patch);
        if (patch.state !== "queued") return;
        if (goesOn && asksTurn) mark(answered(live, ask.toolUseId, Date.now(), ask.at, true));
    };
    const sendQuick = async (text) => {
        if (dismissBeforeSend && !await dismissBeforeSend()) return;
        const key = `${Date.now()}-${Math.random().toString(36).slice(2, 7)}`;
        const messageId = messageID(composer.ids, []);
        const row = { role: "me", key, at: new Date().toISOString(), text, sent: text,
            from: { name, text, files: 0 }, messageId, done: (patch) => onLocalDone(key, patch) };
        if (holding) {
            onLocal({ ...row, state: "held", hold: { text, messageId } });
            return;
        }
        onLocal({ ...row, state: "sending" });
        onSent(key, outcome(await deliver(run, name, { text, messageId })));
    };
    late.current = {
        onRun: (text, lang) => sendShell(run, name, { command: blockCommand(text, lang), messageId: messageID(composer.ids, []),
                                                     onLocal, onLocalDone, confirm: true }),
    };

    // A native dialog and the panel's acts share the form above the field.
    // The answer mark keeps a lagging host snapshot from reopening a dialog.
    const ask = (state.work && state.work.ask) || null;
    const permits = Boolean(live) && live.status === "waiting" && !holding;
    const goesOn = Boolean(ask && ask.async);
    const asksTurn = Boolean(ask && !closed(state.items, ask.toolUseId) && !hidesAsk(answer, ask.toolUseId)
        && !(goesOn && (permits || pending.some((row) => row.asked === ask.toolUseId))));
    const onRequestAnswer = live && !live.outside && knows(exec, "session.send") ? steadyAnswerRequest : null;
    const requestUnavailable = "This session cannot receive an answer.";
    // What an open chain is worked with in its own row. The rows are drawn
    // again only when what they are handed changes, so every hand keeps its
    // identity, and only the rows of requests are handed it.
    const steadyRunChain = useSteady(runChain);
    const steadyCancelChain = useSteady(cancelChain);
    const steadyChainTerm = useSteady((term) => onTerm({ id: term, place: live?.cwd || "" }));
    const answerable = Boolean(live && !live.outside);
    const chainHands = useMemo(() => (answerable ? {
        onAnswer: onRequestAnswer,
        onChainRun: writable && knows(exec, "act.run") ? (act, step) => steadyRunChain("act.run", act, step) : null,
        onChainStop: writable && knows(exec, "act.stop") ? (act, step) => steadyRunChain("act.stop", act, step) : null,
        onChainCancel: writable && knows(exec, "act.cancel") ? steadyCancelChain : null,
        onTerm: onTerm ? steadyChainTerm : null,
        error: actError, unavailable: requestUnavailable,
    } : { unavailable: requestUnavailable }), [answerable, onRequestAnswer, writable, exec, Boolean(onTerm), actError]);
    // An answer to it goes as a message of the person, and stands as one on
    // its way: the card of the round answered (see panelsaid.js).
    const sentAnswer = (use, text) => setLocal((was) => [...was, {
        role: "me", key: `asked-${use}-${Date.now()}`, at: new Date().toISOString(), text, sent: text,
        state: "answered", asked: use, after: lastPos(state.items),
    }]);

    const moves = movesOf(acts, waitingItems, [
        ...(asksTurn ? [{ kind: "asked", id: ask.toolUseId, blocking: !goesOn, at: ask.at || live?.statusUpdatedAt, pos: ask.pos }] : []),
        ...(permits && !asksTurn ? [{ kind: "permit", blocking: true, at: live.statusUpdatedAt, pos: state.items.findLast((row) => row.role === "permitted")?.pos }] : []),
    ]);
    const yourMoves = moves.some(personsMove);
    // A chain is never docked in the form: it stays open for hours, and what
    // the agent writes meanwhile would pile up above it. It is answered in its
    // row, which the pill and a push lead to. It is told by its act, its row
    // of the transcript or the host's mark of the agent's turn, whichever is
    // here first: a chain taken for a plain request would flash in the form.
    const chained = (item) => isChain(item) || Boolean(item.agent || item.running || item.scheduled || item.from || item.until)
        || drawn.some((row) => row.role === "request" && row.id === item.id && isChain(row));
    // Where the row of a chain stands: the host's summary of what waits places
    // it even outside the window drawn, and the drawn feed places the rest.
    const chainRow = (act) => {
        const row = moves.find((item) => item.id === act && item.pos != null)
            || drawn.find((item) => item.role === "request" && item.id === act) || {};
        return { kind: "request", pos: row.pos, nth: row.nth };
    };
    const docked = moves.filter((item) => !chained(item));
    const current = docked.find((item) => moveKey(item) === selected) || docked[0];
    const currentKey = current ? moveKey(current) : "";
    const openBrief = useSteady((briefId) => {
        const move = moves.find((item) => item.kind === "brief" && item.id === briefId);
        const decisions = briefs.find((card) => card.id === briefId)?.decisions
            || state.items.find((item) => item.role === "brief" && item.id === briefId)?.decisions;
        // An open form has the draft still being typed. A second editor over
        // it would start from the saved copy and overwrite those last edits.
        if (move && decisions) {
            setSelected(moveKey(move));
            toEnd();
            return;
        }
        // A document over the run leaves the feed where it was on closing.
        setLook({ kind: "brief", id: briefId });
    });
    // An older render must not undo a newer marker press.
    useEffect(() => { setSelected((was) => was === selected ? currentKey : was); }, [currentKey, selected]);
    // A press on the marker or the reply line of an act: a chain is revealed
    // in its row, any other move is put into the form.
    const selectAct = useSteady((item) => {
        const kind = item.role || item.kind;
        if (kind === "request" && chained({ ...item, ...acts[item.id] })) {
            pickWaiting(chainRow(item.id));
            return;
        }
        setSelected(moveKey({ ...item, kind }));
    });
    // The pill leads through the moves in turn: one the form takes is put into
    // it at the end of the feed, and a chain is revealed in its row. The form
    // does not hold a chain, so the pill keeps the one it led to itself.
    const led = useRef("");
    const nextAct = () => {
        const from = moves.some((item) => moveKey(item) === led.current && chained(item)) ? led.current : currentKey;
        const next = moves[(moves.findIndex((item) => moveKey(item) === from) + 1) % moves.length];
        if (!next) return;
        led.current = moveKey(next);
        if (chained(next)) {
            pickWaiting(chainRow(next.id));
            return;
        }
        setSelected(moveKey(next));
        toEnd();
    };
    // A push about an act opens the conversation on it once the feed and the
    // act are read, once a tap: whatever the reader scrolls to afterwards
    // stays theirs.
    const reached = useRef(null);
    useEffect(() => {
        if (!toAct || reached.current === toAct || state.kind !== "ready" || !feedShown) return;
        const act = acts[toAct.id];
        if (!act) return;
        reached.current = toAct;
        if (chained(act)) {
            pickWaiting(chainRow(act.id));
            return;
        }
        const move = docked.find((item) => item.id === act.id);
        if (!move) return;
        setSelected(moveKey(move));
        toEnd();
    }, [toAct, state.kind, feedShown, acts]);
    const nativeAnswered = (use, text, summary) => {
        if (goesOn) sentAnswer(use, text);
        else if (summary && summary.length) setSentAsk({ use, summary });
        mark(answered(live, use, Date.now(), ask.at, goesOn));
    };
    const dismissBeforeSend = asksTurn && !goesOn && !(isCodex(live) && (ask.server || ask.message)) ? async () => {
        if (!knows(exec, "session.dismiss")) {
            toast("Could not dismiss the question", whyNot(exec, "session.dismiss"), true);
            return false;
        }
        const result = await deliver(run, name, { dismiss: ask.toolUseId, exec });
        if (!result.ok) return false;
        mark(answered(live, ask.toolUseId, Date.now(), ask.at, goesOn));
        return true;
    } : null;
    const finishSecret = (item, state) => {
        setActs((was) => Object.fromEntries(Object.entries(was).map(([id, act]) => [id,
            act.kind === "secret" && (id === item.id || state === "answered" && act.name === item.name)
                ? { ...act, state, answered: new Date().toISOString() } : act])));
        settleWaiting(live, "secret", item.id || item.name);
        loadSecrets();
    };
    const form = !current ? null : current.kind === "asked"
        ? html`<${Ask} key=${currentKey} ask=${ask} name=${name} exec=${exec} stream=${parts.askStream} codex=${isCodex(live)}
            onAnswered=${nativeAnswered} />`
        : current.kind === "permit"
        ? html`<${Permit} name=${name} exec=${exec} waitingFor=${live.waitingFor} codex=${isCodex(live)} onAnswered=${() => mark(answered(live, ""))} />`
        : current.kind === "request"
        ? html`<div onClick=${(event) => codecopy.fromClick(event, toast)}><${RequestCard} key=${currentKey} item=${current} record=${acts[current.id]}
            error=${actError} onAnswer=${onRequestAnswer} unavailable=${requestUnavailable} onRun=${lent.onRun} /></div>`
        : current.kind === "secret"
        ? html`<${SecretPad} key=${currentKey} item=${current} session=${name} exec=${exec} shelf=${secrets}
            onSaved=${(state) => finishSecret(current, state)} onDone=${() => {}} />`
        : current.kind === "brief"
        ? html`<${Brief} key=${currentKey} id=${current.id} snapshot=${snapshot} exec=${exec} embedded
            onOpen=${openBrief} onSession=${() => sentBrief(current.id)} />` : null;

    if (sub) {
        const fresh = ((state.work && state.work.agents) || []).find((a) => a.id === sub.id);
        return html`<${SubChat} session=${name} id=${sub.feedId} agent=${fresh ? { ...sub, ...fresh } : sub}
                                live=${Boolean(live)} onBack=${() => setSub(null)} />`;
    }

    // The shelf must exist before its signal is queued; only a delivered
    // signal settles the reading as sent.
    const sendReview = async ({ id, notes }) => {
        // Refuse before writing a shelf file the host cannot deliver.
        if (!knows(exec, "session.send")) {
            throw new Error(whyNot(exec, "session.send") || "this host cannot write to a session");
        }
        const put = await onShelf(id);
        const result = await deliver(run, name, { text: signal(put.path, put.notes || notes) });
        if (!result || result.ok === false) {
            throw new Error(result && result.error ? result.error : "the session did not take the signal");
        }
        await sealed(id, put.path);
    };
    const openTalk = onOpenChat ? (talk) => onOpenChat(talk) : null;

    // A thread of codex in the archive keeps no fill of its context: its head
    // says what the thread spent instead, as its row in the archive does, and
    // nothing when it spent nothing. Null is the archive that keeps the fill.
    const pastCodex = !live && isCodex(archive);
    const pct = live ? live.pct : (archive && !pastCodex ? archive.pctMax : null);
    const spent = pastCodex ? (spoke(archive) ? `${tokens(archive.tokensUsed)} tokens` : "") : null;
    // Acts refresh on Run before the host snapshot catches up, so the head
    // takes the same current turns as the pill and the chain card.
    const standing = live ? { ...live, waiting: moves.map((item) => {
        const { from, until } = commandWindow(item);
        return { ...item, from, until, agent: agentsMove(item), running: commandRunning(item) };
    }) } : live;
    const stand = stateOf(standing, still, move, snapshot && snapshot.limits);
    const tools = {
        name, live, archive, pct, spent, exec, snapshot, cwd: here, view, sides, win, way, work: state.work,
        sections: parts.sections, onTerm: term.route ? onTerm : null, onWindow: place.onWindow, onLook: (kind) => setLook({ kind }),
    };
    const deskTools = html`
        <${ViewTabs} view=${repo.file ? "" : view} sides=${live ? sides : {}} canTerm=${canTerm}
                     onView=${(next) => { repo.setFile(""); pickView(next); }} onMove=${() => setPanel(true)} />
        <${FilesButton} repo=${repo} />
        ${live && html`<${SessionButton} ...${tools} open=${panel} onOpen=${setPanel} />`}
    `;
    // The terminal of the session's project: the root the map knows it by,
    // or the directory it works in. A listener without the terminal route
    // has no such button at all.
    const termPlace = (live && live.project && live.project.path) || here;
    const termJump = onTerm && term.route && live && termPlace && knows(exec, "term.start")
        ? html`<${TermJump} place=${termPlace} exec=${exec} onTerm=${onTerm} />`
        : null;
    const work = parts.work(ctx);
    const movePill = html`<${MovePill} items=${moves} onNext=${nextAct} />`;
    const deckTools = html`
        <div class="decktools">
            <${CommandsChip} onOpen=${setQuick} />
            ${termJump}
            ${work}
            <div class="deckright"><${WorkRefs} work=${state.work} pages=${myPages} briefs=${myBriefs}
                onOpen=${(look) => look.kind === "brief" ? openBrief(look.id) : setLook(look)} /></div>
        </div>
    `;

    return html`
        <${PlaceContext.Provider} value=${placeActions}>
        ${wide
            ? html`<${DeskHead} limits=${snapshot && snapshot.limits} name=${shownName(live, name)} live=${standing} archive=${archive} pct=${pct} spent=${spent}
                                move=${move} yours=${yourMoves} tools=${deskTools} exec=${exec} taken=${takenNames(snapshot)} />`
            : html`
        <${BackHead} kind="talk" onBack=${onBack} label="to sessions"
                     foot=${html`<${ContextBar} pct=${pct} peak=${!live} cap=${live && live.cap} autoRestart=${live && live.autoRestart} />`}
                     tools=${html`<${FilesButton} repo=${repo} /><${MoreButton} onOpen=${() => setLook({ kind: "tools" })} />`}>
            <div class="chathead">
                <h2>
                    <span class="talkdot" data-tone=${stand.tone} title=${stand.say}></span>
                    <${HeadName} key=${name} name=${shownName(live, name)} live=${live} exec=${exec} taken=${takenNames(snapshot)} />
                </h2>
                <div class="chatsub">
                    ${live && html`
                        <span class="agentword" data-agent=${agentKey(live)}>${agentName(live)}</span>
                        ${live.model && html`<span class="sep">·</span><span>${modelTitle(live.model, { withWindow: false })}</span>`}
                        <span class="sep">·</span>
                    `}
                    ${pct != null && html`
                        <span class="chatpct">${pct.toFixed(1)}<span class="u">%</span></span>
                        <span class="sep">·</span>
                    `}
                    ${spent && html`
                        <span class="chatspent">${spent}</span>
                        <span class="sep">·</span>
                    `}
                    <span class="talkword" data-tone=${yourMoves ? "waiting" : stand.tone}>${yourMoves ? "your move" : stand.word || (archive ? "peak" : "")}</span>
                </div>
                ${here && html`
                    <div class="chatwhere">
                        <span class="chatpath" title=${here}><bdi>${shortPath(here)}</bdi></span>
                    </div>
                `}
            </div>
        <//>
        `}

        <${RepoView} repo=${repo} onSend=${sendReview} onConversation=${openTalk} feed=${html`
        ${move
            ? html`<${MoveScreen} move=${move} />`
            : view === "term"
            ? html`<${Term} name=${name} />`
            : html`
        <${FindBar} find=${find} />
        <div
            class="chatfeed"
            ref=${bindFeed}
            onClick=${(event) => {
                if (codecopy.fromClick(event, toast)) return;
                if (codecopy.fromInline(event)) return;
                const hit = event.target.closest && event.target.closest(".path");
                if (hit) openPath(hit.dataset.path);
            }}
            onContextMenuCapture=${pieceMenu}
            onScroll=${onScroll}
        >
            ${state.kind === "loading" && html`<p class="hint">Reading the conversation…</p>`}
            ${state.kind === "failed" && html`
                <p class="hint crit">${state.error}</p>
                <p class="hint">The feed is parsed by aacpanel-agent on the host: without it the chat is
                unavailable, while the other screens work.</p>
            `}
            ${state.kind === "fresh" && html`
                <p class="empty">The conversation has not started yet — the feed appears with the first message.</p>
            `}
            ${state.kind === "ready" && state.items.length === 0 && waiting.length === 0 && html`
                <p class="empty">Nothing has been said in this conversation yet.</p>
            `}
            ${more && html`
                <div class="mearlier" ref=${topRef}>there is more above</div>
            `}
            ${state.note && html`<p class="hint warn">${state.note}</p>`}
            <${FeedWrites.Provider} value=${lent}>
                <${FeedGrid}
                    rows=${rows(drawn, pending, goesOn && asksTurn && Boolean(live) && !live.outside)}
                    wide=${wide}
                    busy=${Boolean(live && live.status === "busy" && !live.turnOver
                        && !(live.transport === "stream" && live.compacting))}
                    checklist=${checklist}
                    onOpen=${(g, badge) => setCalls(callsOf(g, drawn, runCalls, turnCalls, badge))}
                    row=${(item, n) => html`<${FeedRow}
                        key=${`${item.pos}-${n}`}
                        item=${item}
                        session=${name}
                        id=${id}
                        copies=${copies}
                        onPage=${onPage}
                        onFile=${onFile}
                        onBrief=${openBrief}
                        briefs=${briefs}
                        act=${acts[item.claim || item.re || item.actReplyID || item.id]}
                        onClaimWithdraw=${knows(exec, "act.withdraw") ? withdrawClaim : null}
                        onAct=${selectAct}
                        onDock=${answerable ? selectAct : null}
                        chain=${item.role === "request" ? chainHands : null}
                        secrets=${secrets}
                        onSecret=${live && !live.outside ? openSecret : null}
                        onCommand=${onCommand}
                        onShell=${onShell}
                        onTask=${onTask}
                        onAgent=${onAgent}
                    />${queueTools(item)}`}
                    tail=${away ? [] : waiting.map((row) => ({ key: `local-${row.key}`, role: row.role, node: html`
                        <${FeedRow} item=${row} />
                        ${queueTools(row)}
                    ` })).concat(form && live && !live.outside ? [{ key: "act-form", role: "act", node: html`
                        <section class="actform" aria-label="Your move">${form}</section>
                    ` }] : []).concat(sentShown && live && !live.outside ? [{ key: `sent-${sentAsk.use}`, role: "act", node: html`
                        <section class="actform actsent" aria-label="Your answer, on its way">
                            <div class="senthead"><span class="sentico">${Icon.ask()}</span><span class="sentlabel">answer accepted · the session reads it</span><span class="spin actspin" aria-hidden="true"></span></div>
                            ${sentAsk.summary.map((line) => html`<p class="actsentline">${line}</p>`)}
                        </section>
                    ` }] : [])}
                />
            <//>
            ${later && html`<div class="mlater" ref=${bottomRef}>there is more below</div>`}
            ${hasPrevious && html`<${JumpToPrevious} onJump=${toPrevious} above=${!atEnd} />`}
            ${!atEnd && html`<${JumpToEnd} onJump=${toEnd} away=${away} />`}
        </div>
        `}
        `}>
        ${live && feedShown && !hasWork(state.work, live.status === "busy" || Boolean(live.compacting)) && live.lastRequestAt && html`
            <p class="lastreq">last reply ${ago(live.lastRequestAt)}</p>
        `}

        ${live && feedShown && html`
            <div class="composerbox">
                <${ShellBar} item=${shellNow(feed, pending)} onOpen=${toEnd} />
                <${WorkStatus} work=${state.work} busy=${live.status === "busy"}
                               turnOver=${Boolean(live.turnOver)}
                               compacting=${live.transport === "stream" ? live.compacting || "" : ""}
                               feed=${feed} onCalls=${(run) => setCalls({ list: runCalls(feed, run) })}
                               onCall=${(run, call) => setCalls({ list: runCalls(feed, run), first: call })}
                               to=${parts.toBackground}
                               checklist=${wide ? null : checklist}
                               onChecklist=${() => setLook({ kind: "checklist" })}
                               onOpen=${(what) => setLook(what)} />
                ${live.transport === "bg"
                    ? html`<${BackgroundActions} live=${live} exec=${exec} onTerm=${term.route ? onTerm : null} />`
                    : live.outside
                    ? html`<p class="outsidenote">${outsideNote(live)}</p>`
                    : html`
                        <${Composer} name=${name} id=${id} exec=${exec} busy=${live.status === "busy"} stream=${live.transport === "stream"}
                                     ...${composer}
                                     hold=${holding}
                                     files=${files}
                                     onAsk=${() => setAsking(true)}
                                     onFiles=${(picked) => setFiles((was) => [...was, ...picked])}
                                     onDropFile=${(i) => setFiles((was) => was.filter((_, n) => n !== i))}
                                     onDropFiles=${() => setFiles([])}
                                     onLocal=${onLocal}
                                     onLocalDone=${onSent}
                                     insert=${insert}
                                     beforeSend=${dismissBeforeSend}
                                     hint=${dismissBeforeSend ? "your words dismiss the question" : ""}
                                     strip=${html`${parts.strip(ctx)}
                                         ${wide && html`${movePill}${deckTools}`}
                                         ${live.tokensIn > 0 && html`<button class="deckuse" type="button"
                                             aria-label="tokens in and out of this session, open usage" onClick=${onUsage}>
                                             <span class="deckin"><i aria-hidden="true">↑</i>${tokens(live.tokensIn)}</span>
                                             <span class="deckout"><i aria-hidden="true">↓</i>${tokens(live.tokensOut)}</span>
                                         </button>`}`}
                                     focus=${`${name}|${id || ""}|${view}`} />
                    `}
            </div>
        `}

        ${!live && archive?.sessionId && feedShown && html`
            <${ClosedResume} key=${archive.sessionId} row=${archive} exec=${exec} />
        `}

        ${live && feedShown && (!wide || live.outside) && html`
            <div class="deck">
                ${movePill}
                ${deckTools}
            </div>
        `}

        <//>

        ${live && parts.sheets(ctx)}
        ${live && quick && html`<${QuickCommands} name=${name} live=${live} exec=${exec} ...${quick}
            onClose=${() => { setQuick(null); quick.lift?.focus({ preventScroll: true }); }}
            onCommands=${() => setLook({ kind: "commands" })} onSend=${sendQuick} />`}

        ${live && html`<${AttachSheet}
            open=${asking}
            onClose=${() => setAsking(false)}
            exec=${exec}
            files=${files}
            onFiles=${(picked) => setFiles((was) => [...was, ...picked])}
        />`}

        <${Sheet} open=${Boolean(calls)} onClose=${() => setCalls(null)} label="tool calls" inner>
            <${Calls} session=${name} id=${id} calls=${calls ? calls.list : []} turn=${calls && calls.turn}
                      first=${calls && calls.first}
                      onFile=${onFile} to=${parts.toBackground} />
        <//>

        <${Sheet} open=${Boolean(look) && look.kind !== "file"} onClose=${() => setLook(null)}
                  label=${look ? (own ? own.label : LOOK_NAMES[look.kind]) : ""} inner
                  doc=${Boolean(look) && (look.kind === "brief" || pageLook(look))}>
            ${look && look.kind !== "file" && (own
                ? own.node
                : look.kind === "brief"
                ? html`<${Brief} id=${look.id} snapshot=${snapshot} exec=${exec}
                                 onBack=${() => setLook(null)}
                                 onSession=${() => sentBrief(look.id)} />`
                : look.kind === "artifact"
                ? html`<${ArtifactPage} card=${look.card} />`
                : look.kind === "command"
                ? html`<${CommandSheet} item=${look.item} />`
                : look.kind === "shell"
                ? html`<${ShellSheet} item=${look.item} />`
                : look.kind === "checklist"
                ? html`<${ChecklistSheet} checklist=${checklist} />`
                : look.kind === "commands"
                ? html`<${CommandsSheet} name=${name} live=${live} exec=${exec} work=${state.work} pages=${myPages}
                    onScreen=${(kind) => setLook({ kind })} onWork=${setLook} onDone=${() => setLook(null)} />`
                : look.kind === "secret"
                ? html`<${SecretPad} item=${look.item} session=${name} exec=${exec} shelf=${secrets}
                    onSaved=${loadSecrets} onDone=${() => setLook(null)} />`
                : look.kind === "parent"
                ? html`<${ParentSheet} name=${name} live=${live} snapshot=${snapshot} exec=${exec}
                                      onDone=${() => setLook(null)} />`
                : look.kind === "tools"
                ? html`<${SessionTools} ...${tools}
                                        onFind=${feedShown ? () => { setLook(null); find.start(); } : null}
                                        onPick=${(next) => { setLook(null); pickView(next); }}
                                        onDone=${() => setLook(null)} />`
                : WORK_LISTS.has(look.kind)
                ? html`<${WorkList} session=${name} id=${id} kind=${look.kind} work=${state.work}
                                    exec=${exec} onAgent=${openAgent}
                                    pages=${myPages} briefs=${myBriefs} onBrief=${openBrief}
                                    onPage=${(card) => setLook({ kind: "artifact", card })} />`
                : html`<${Look} session=${name} id=${id} look=${look}
                                 onBack=${pageLook(look) ? () => setLook(null) : undefined} />`)}
        <//>

        ${look && look.kind === "file" && html`<${Look} session=${name} id=${id} look=${look}
            onClose=${() => setLook(null)} onFile=${onFile} />`}

        ${parts.aside(ctx)}

        <${QuoteTip} quote=${quote} onQuote=${takeQuote} />
        <//>
    `;
}

function ClosedResume({ row, exec }) {
    const run = useAction();
    const [pending, setPending] = useState(false);
    const fired = useRef(false);
    const why = knows(exec, "session.resume") ? "" : whyNot(exec, "session.resume");
    const press = async () => {
        if (fired.current || why) return;
        fired.current = true;
        setPending(true);
        const result = await resume(run, row, exec);
        // Success waits for the live snapshot: the executor's reply alone
        // does not put a composer on this conversation yet.
        if (!result.ok) {
            fired.current = false;
            setPending(false);
        }
    };
    return html`<div class="resumebox">
        <div class="resumebar" aria-busy=${pending ? "true" : "false"}>
            <div class="resumenote">This session is closed
                ${why && html`<p class="resumewhy">${why}</p>`}
            </div>
            <button class="btn primary" type="button" disabled=${pending || Boolean(why)}
                    data-tip=${why || undefined} onClick=${press}>
                ${pending ? html`<span class="spin" aria-hidden="true"></span>` : html`<${Icon.resume} />`}
                ${pending ? "Resuming…" : "Resume"}
            </button>
        </div>
    </div>`;
}
