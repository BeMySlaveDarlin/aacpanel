import { useCallback, useEffect, useMemo, useState } from "preact/hooks";

import { html } from "../html.js";
import { BackHead, useBackClose } from "../ui/back.js";
import { Sheet } from "../ui/sheet.js";
import { useToast } from "../ui/toasts.js";
import * as codecopy from "./chat/copy.js";
import { ContextBar } from "../ui/bar.js";
import { Ask } from "./ask.js";
import { Permit } from "./chat/permit.js";
import { ago, tokens } from "../format.js";
import { closed, rows, runCalls, unarrived, weld } from "./chat/feed.js";
import { JumpToEnd, useFeedWindow } from "./chat/feedwindow.js";
import { SubChat, subFeedId } from "./chat/subchat.js";
import { RepoView } from "./repo/view.js";
import { onShelf, sealed, signal } from "./repo/notes.js";
import { Icon } from "../ui/icons.js";
import { Row } from "./chat/rows.js";
import { CommandSheet } from "./chat/command.js";
import { McpSheet } from "./chat/mcp.js";
import { StatusSheet } from "./chat/status.js";
import { SetupSheet, SETUP_TITLES } from "./chat/setup.js";
import { CommandsChip, CommandsSheet } from "./chat/commands.js";
import { Calls } from "./chat/calls.js";
import { Look, LOOK_NAMES, WORK_LISTS } from "./chat/look.js";
import { ArtifactPage } from "./artifact.js";
import { Brief } from "./brief.js";
import { index, shelf as pageShelf } from "../data/artifacts.js";
import { shelf as briefShelf } from "../data/briefs.js";
import { hasWork, Work, WorkList, WorkRefs, WorkStatus } from "./chat/work.js";
import { Composer, deliver, outcome } from "./chat/composer.js";
import { knows, whyNot } from "../exec.js";
import { ANSWER_LAG_MS, answered, hidesAsk, lagging, recall, remember, settle } from "./chat/answered.js";
import { useAction } from "../actions/gate.js";
import { QuoteTip, useSelectionQuote } from "./chat/quotetip.js";
import { SideChat, useSideChat } from "./chat/sidechat.js";
import { ChatPath, Marquee, short } from "./chat/head.js";
import { AttachSheet } from "./chat/tools.js";
import { PickBar, PickSheet, PickWords } from "./chat/picker.js";
import { DeskHead, ViewToggle } from "./chat/deskhead.js";
import { WindowToggle, useWindow } from "./chat/window.js";
import { moveSession, sidesOf, useSwitchWay } from "./chat/switch.js";
import { TakeBack } from "./chat/takeback.js";
import { Term, useTermAvailable } from "./chat/term.js";
import { useViewPick } from "./chat/viewpick.js";
import { useViewing } from "../viewing.js";
import { useWide } from "../ui/wide.js";


// RepoButton opens the repository of this conversation. It stands to the left
// of the pair that says what a session is watched with, in the same set of
// tools: the code of a project belongs beside its terminal and its run, not on
// a screen of its own that has to be found.
function RepoButton({ onOpen }) {
    return html`
        <button class="viewbtn solo" type="button" title="the files of this project"
                aria-label="the files of this project" onClick=${onOpen}>
            ${Icon.files()}
        </button>
    `;
}

export function Chat({ name, id, live, archive, exec, snapshot, onBack, onUsage }) {
    // A brief opens over the conversation, the way a subagent's letters do: a
    // layer above the run, put down by the same gesture and leaving the run
    // where it was. Sending the reader to a page of their own instead costs
    // them the conversation and the gesture both.
    const openBrief = useCallback((briefId) => setLook({ kind: "brief", id: briefId }), []);
    const toast = useToast();
    useViewing(live ? name : "");
    const [sub, setSub] = useState(null);
    const [repo, setRepo] = useState(false);
    const { state, more, feedRef, topRef, onScroll, atEnd, toEnd } = useFeedWindow({
        name, id, live: live && !sub,
    });
    const [calls, setCalls] = useState(null);
    const [look, setLook] = useState(null);
    // The pages this conversation published and the panel kept a copy of. The
    // shelf is asked for once: a card looks itself up in it rather than asking
    // per artifact.
    const [copies, setCopies] = useState(null);
    const [pages, setPages] = useState([]);
    const [briefs, setBriefs] = useState([]);
    const [local, setLocal] = useState([]);
    const [, redraw] = useState(0);
    const answer = recall(name);
    const mark = (next) => {
        remember(name, next);
        redraw((n) => n + 1);
    };
    const run = useAction();
    const [files, setFiles] = useState([]);
    const [asking, setAsking] = useState(false);
    // Which list of settings is open: the model, the effort or the mode.
    const [picking, setPicking] = useState("");

    const wide = useWide();
    const term = useTermAvailable();
    const canTerm = term.ok && Boolean(live);
    const [picked, pickView] = useViewPick(canTerm, wide);

    // Which side the session lives on decides what the pair of views does: see
    // sidesOf. The window on the host is asked here, since it holds a console.
    const transport = (live && live.transport) || "";
    const way = useSwitchWay(live ? name : "", transport);
    const [win, askWindow] = useWindow(live ? name : "", transport);
    const sides = live
        ? sidesOf({ live, way, held: win.kind === "open", picked, canTerm, exec })
        : { view: picked, pair: false, moves: "", tip: "", why: "" };
    const view = sides.view;
    const onView = (next) => {
        if (next === view) return;
        if (!sides.moves) {
            pickView(next);
            return;
        }
        moveSession({ run, exec, name, to: sides.moves, work: state.work });
    };

    // Everything a conversation made is filtered by the directory it worked in.
    // A session that has ended leaves its pages and its briefs to the next one
    // in the same place, which is the session that carries the work on.
    const here = (live && live.cwd) || (archive && archive.cwd) || "";
    // Without a directory there is nothing to match on, and showing everything
    // would put the documents of other projects into this conversation.
    const mine = useCallback((cards) => (here
        ? (cards || []).filter((c) => c.cwd === here)
        : []), [here]);
    const myPages = useMemo(() => mine(pages), [mine, pages]);
    const myBriefs = useMemo(() => mine(briefs), [mine, briefs]);

    // Questions asked aside: on the stream the panel asks them itself, and
    // they reach neither the conversation nor its transcript.
    const sideChat = useSideChat(name, id);
    const onStream = Boolean(live) && live.transport === "stream";

    const quote = useSelectionQuote();
    const [insert, setInsert] = useState(null);
    // A command the panel answers with a screen opens it in the sheet; the
    // screens of settings share one view, told apart by their part.
    const screenLook = (what) => setLook(SETUP_TITLES[what] ? { kind: "setup", part: what } : { kind: what });
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

    useEffect(() => {
        setSub(null);
        setLocal([]);
        setFiles([]);
        setCalls(null);
        setLook(null);
        setInsert(null);
    }, [name, id]);

    useBackClose(true, onBack);


    // The rows still on their way are what the feed draws after its items;
    // the ones the transcript has echoed leave the state afterwards, and the
    // screen never shows a message twice in between.
    const pending = unarrived(local, state.items);
    useEffect(() => {
        if (pending.length === local.length) return;
        setLocal((was) => unarrived(was, state.items));
    }, [state.items, local]);

    // A row just sent grows the feed the way a new item does, and a feed at
    // its end follows it the same way — or the message stands under the edge
    // until the echo comes and the feed jumps to it.
    useEffect(() => {
        const box = feedRef.current;
        if (box && atEnd) box.scrollTop = box.scrollHeight;
    }, [pending.length]);

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
        const feedId = subFeedId(id, agent.id);
        if (!feedId) return;
        setLook(null);
        setSub({ ...agent, feedId });
    };

    if (sub) {
        const fresh = ((state.work && state.work.agents) || []).find((a) => a.id === sub.id);
        return html`<${SubChat} session=${name} id=${sub.feedId} agent=${fresh ? { ...sub, ...fresh } : sub}
                                live=${Boolean(live)} onBack=${() => setSub(null)} />`;
    }

    // The repository of this conversation, read as a page of its own. A layer
    // over the run rather than a screen beside it: the way back is one tap and
    // the conversation is still underneath when it comes.
    if (repo) {
        // Sending a reading is three steps in one act: the file is written to
        // the shelf, the session is told where it is through the same queue any
        // message goes through, and only then is the reading written down as
        // gone. Told first and written second, the session would open a path to
        // nothing; settled before the signal, a reading that never left would
        // read as delivered.
        const sendReview = async ({ id, notes }) => {
            // A host whose executor does not know how to write to a session
            // would take the reading, put the file on the shelf and tell
            // nobody. Better to say so before anything is written.
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
        return html`<${RepoView} cwd=${here} name=${name}
                                 onBack=${() => setRepo(false)} onSend=${sendReview} />`;
    }

    const feed = weld(state.items);

    const pct = live ? live.pct : (archive ? archive.pctMax : null);
    const tools = live && html`
        ${sides.pair && html`<${ViewToggle} view=${view} onView=${onView} tip=${sides.tip} why=${sides.why} />`}
        <${WindowToggle} name=${name} live=${live} work=${state.work} exec=${exec}
                         win=${win} way=${way} onChange=${askWindow} />
    `;

    return html`
        ${wide
            ? html`<${DeskHead} name=${name} live=${live} archive=${archive} pct=${pct} tools=${tools}
                                onRepo=${here ? () => setRepo(true) : null} />`
            : html`
        <${BackHead} onBack=${onBack} label="to sessions" foot=${html`<${ContextBar} pct=${pct} peak=${!live} />`}
                     tools=${html`
                         ${here && html`<${RepoButton} onOpen=${() => setRepo(true)} />`}
                         ${live && tools}
                     `}>
            <div class="chathead">
                <h2>
                    ${live && html`<span class="livedot" title="live"></span>`}
                    <${Marquee} text=${name} />
                </h2>
                <div class="chatsub">
                    ${pct != null
                        ? html`
                            <span class="chatpct">${pct.toFixed(1)}<span class="u">%</span></span>
                            ${live && live.tokens > 0 && html`
                                <span class="chatvol">${short(live.tokens)}${live.limit ? ` of ${short(live.limit)}` : ""}</span>
                            `}
                        `
                        : html`<span>the conversation is closed</span>`}
                </div>
                ${((live || archive || {}).cwd) && html`
                    <div class="chatmeta"><${ChatPath} cwd=${(live || archive).cwd} /></div>
                `}
            </div>
        <//>
        `}

        ${view === "term"
            ? html`<${Term} name=${name} />`
            : html`
        <div
            class="chatfeed"
            ref=${feedRef}
            onClick=${(event) => {
                if (codecopy.fromClick(event, toast)) return;
                const hit = event.target.closest && event.target.closest(".path");
                if (hit) setLook({ kind: "file", path: hit.dataset.path });
            }}
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
            ${state.kind === "ready" && state.items.length === 0 && html`
                <p class="empty">Nothing has been said in this conversation yet.</p>
            `}
            ${more && html`
                <div class="mearlier" ref=${topRef}>there is more above</div>
            `}
            ${state.note && html`<p class="hint warn">${state.note}</p>`}
            ${rows(feed).map((item, n) => html`<${Row}
                key=${`${item.pos}-${n}`}
                item=${item}
                session=${name}
                id=${id}
                copies=${copies}
                onPage=${(card) => setLook({ kind: "artifact", card })}
                onCalls=${() => setCalls(runCalls(feed, item.run))}
                onFile=${(file) => setLook({ kind: "file", ...file })}
                onBrief=${openBrief}
                onCommand=${(row) => setLook({ kind: "command", item: row })}
            />`)}
            ${pending.map((row) => html`
                <${Row} key=${`local-${row.key}`} item=${row} />
                ${row.state === "queued" && live && live.transport === "stream" && html`
                    <${TakeBack} key=${`back-${row.key}`} row=${row} name=${name} exec=${exec}
                                 onGone=${(key) => setLocal((was) => was.filter((l) => l.key !== key))}
                                 onEdit=${(text) => setInsert({ key: Date.now(), text, message: true })} />
                `}
            `)}
            ${!atEnd && html`<${JumpToEnd} onJump=${toEnd} />`}
        </div>
        `}
        ${live && view !== "term" && !hasWork(state.work, live.status === "busy" || Boolean(live.compacting)) && live.lastRequestAt && html`
            <p class="lastreq">request ${ago(live.lastRequestAt)}</p>
        `}

        ${live && view !== "term" && html`
            <div class="composerbox">
                <${WorkStatus} work=${state.work} busy=${live.status === "busy"}
                               compacting=${live.transport === "stream" ? live.compacting || "" : ""} />
                ${state.work && state.work.ask && !closed(state.items, state.work.ask.toolUseId)
                    && !hidesAsk(answer, state.work.ask.toolUseId)
                    ? html`<${Ask} ask=${state.work.ask} name=${name} exec=${exec} stream=${live.transport === "stream"}
                                   onAnswered=${(use) => mark(answered(live, use))} />`
                    : live.status === "waiting" && !holding
                    ? html`<${Permit} name=${name} exec=${exec} waitingFor=${live.waitingFor}
                                      onAnswered=${() => mark(answered(live, ""))} />`
                    : html`
                        <${Composer} name=${name} id=${id} exec=${exec} busy=${live.status === "busy"} stream=${live.transport === "stream"}
                                     hold=${holding}
                                     files=${files}
                                     onAsk=${() => setAsking(true)}
                                     onFiles=${(picked) => setFiles((was) => [...was, ...picked])}
                                     onDropFile=${(i) => setFiles((was) => was.filter((_, n) => n !== i))}
                                     onDropFiles=${() => setFiles([])}
                                     onLocal=${(row) => setLocal((was) => [...was, row])}
                                     onLocalDone=${(key, patch) => setLocal((was) =>
                                         was.map((l) => (l.key === key ? { ...l, ...patch } : l)))}
                                     insert=${insert}
                                     onPicker=${knows(exec, "session.set") ? setPicking : null}
                                     onScreen=${screenLook}
                                     onSide=${onStream ? sideChat.ask : null}
                                     strip=${wide
                                         ? html`<${PickBar} name=${name} live=${live} exec=${exec} />`
                                         : html`<${PickWords} live=${live} exec=${exec} onPick=${setPicking} />`}
                                     focus=${`${name}|${id || ""}|${view}`} />
                    `}
            </div>
        `}

        ${live && view !== "term" && html`
            <div class="deck">
                ${!wide && live.tokensIn > 0 && html`
                    <button class="deckuse" type="button" aria-label="tokens in and out of this session, open usage"
                            onClick=${onUsage}>
                        <span class="deckin"><i>in</i>${tokens(live.tokensIn)}</span>
                        <span class="deckout"><i>out</i>${tokens(live.tokensOut)}</span>
                    </button>`}
                <${Work} work=${state.work} onOpen=${(what) => setLook(what)} />
                <div class="deckright">
                    <${WorkRefs} work=${state.work} pages=${myPages} briefs=${myBriefs} onOpen=${(what) => setLook(what)} />
                    <${CommandsChip} onOpen=${() => setLook({ kind: "commands" })} />
                </div>
            </div>
        `}

        ${live && html`<${PickSheet} what=${picking} onClose=${() => setPicking("")} name=${name} live=${live} exec=${exec} />`}

        ${live && html`<${AttachSheet}
            open=${asking}
            onClose=${() => setAsking(false)}
            live=${live}
            onMode=${knows(exec, "session.set") ? () => { setAsking(false); setPicking("mode"); } : null}
            exec=${exec}
            files=${files}
            onFiles=${(picked) => setFiles((was) => [...was, ...picked])}
        />`}

        <${Sheet} open=${Boolean(calls)} onClose=${() => setCalls(null)} label="tool calls" inner>
            <${Calls} session=${name} id=${id} calls=${calls || []}
                      onFile=${(file) => setLook({ kind: "file", ...file })} />
        <//>

        <${Sheet} open=${Boolean(look)} onClose=${() => setLook(null)}
                  label=${look ? (look.kind === "setup" ? SETUP_TITLES[look.part] : LOOK_NAMES[look.kind]) : ""} inner
                  doc=${Boolean(look) && look.kind === "brief"}>
            ${look && (look.kind === "brief"
                ? html`<${Brief} id=${look.id} snapshot=${snapshot} exec=${exec}
                                 onBack=${() => setLook(null)}
                                 onSession=${() => setLook(null)} />`
                : look.kind === "artifact"
                ? html`<${ArtifactPage} card=${look.card} />`
                : look.kind === "command"
                ? html`<${CommandSheet} item=${look.item} />`
                : look.kind === "mcp"
                ? html`<${McpSheet} name=${name} exec=${exec} />`
                : look.kind === "status"
                ? (live ? html`<${StatusSheet} name=${name} live=${live} />`
                    : html`<p class="cmdnote">The session has ended: there is no one to ask.</p>`)
                : look.kind === "commands"
                ? html`<${CommandsSheet} name=${name} live=${live} exec=${exec} onScreen=${screenLook}
                                         onPicker=${knows(exec, "session.set") ? (what) => { setLook(null); setPicking(what); } : null}
                                         onSide=${onStream ? () => { setLook(null); setInsert({ key: Date.now(), text: "/btw ", command: true }); } : null}
                                         onDone=${() => setLook(null)} />`
                : look.kind === "setup"
                ? (live ? html`<${SetupSheet} name=${name} part=${look.part} />`
                    : html`<p class="cmdnote">The session has ended: there is no one to ask.</p>`)
                : WORK_LISTS.has(look.kind)
                ? html`<${WorkList} session=${name} id=${id} kind=${look.kind} work=${state.work}
                                    exec=${exec} onAgent=${openAgent}
                                    pages=${myPages} briefs=${myBriefs} onBrief=${openBrief}
                                    onPage=${(card) => setLook({ kind: "artifact", card })} />`
                : html`<${Look} session=${name} id=${id} look=${look} />`)}
        <//>

        ${onStream && view !== "term" && html`<${SideChat} chat=${sideChat} wide=${wide} feedRef=${feedRef} />`}

        <${QuoteTip} quote=${quote} onQuote=${takeQuote} />
    `;
}
