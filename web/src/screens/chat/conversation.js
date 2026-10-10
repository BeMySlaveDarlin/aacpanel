// The conversation of a live or closed session, as both agents have it: the
// header, the feed with its window and search, the composer and what stands
// over it, the deck under it, and the sheets over the run. What is the agent's
// own comes in its parts — see claude.js and codex.js: where the session lives
// and what it is watched with, what the composer reads in the words, the band
// under the field, the sheets of its settings, and the sections of its tools.

import { useCallback, useEffect, useMemo, useState } from "preact/hooks";

import { html } from "../../html.js";
import { BackHead, useBackClose } from "../../ui/back.js";
import { Sheet } from "../../ui/sheet.js";
import { useToast } from "../../ui/toasts.js";
import * as codecopy from "./copy.js";
import { ContextBar } from "../../ui/bar.js";
import { Ask } from "../ask.js";
import { Permit } from "./permit.js";
import { ago, tokens } from "../../format.js";
import { closed, lastPos, rows, runCalls, turnCalls, unarrived, weld } from "./feed.js";
import { JumpToEnd, useFeedWindow } from "./feedwindow.js";
import { FindBar, useFeedFind } from "./find.js";
import { agentFeedId, SubChat } from "./subchat.js";
import { RepoView } from "../repo/view.js";
import { onShelf, sealed, signal } from "../repo/notes.js";
import { Row } from "./rows.js";
import { callsOf, FeedGrid } from "./timeline.js";
import { CommandSheet } from "./command.js";
import { ShellSheet } from "./shell.js";
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
import { Composer, deliver, outcome } from "./composer.js";
import { knows, whyNot } from "../../exec.js";
import { ANSWER_LAG_MS, answered, hidesAsk, lagging, recall, remember, settle } from "./answered.js";
import { useAction } from "../../actions/gate.js";
import { QuoteTip, useSelectionQuote } from "./quotetip.js";
import { MidName, modelTitle, shortPath, stateOf } from "./head.js";
import { MoreButton, SessionButton, SessionTools, ViewTabs } from "./sessiontools.js";
import { AttachSheet } from "./tools.js";
import { DeskHead } from "./deskhead.js";
import { MoveScreen } from "./switch.js";
import { outsideNote } from "../sessions/kin.js";
import { TakeBack } from "./takeback.js";
import { Term } from "./term.js";
import { TermJump } from "../terms/jump.js";
import { useViewing } from "../../viewing.js";
import { useWide } from "../../ui/wide.js";
import { useAsOf } from "../../ui/asof.js";
import { agentKey, agentName, isCodex, shownName, spoke } from "../../agent.js";

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
// whether the feed shows and its box, the work of the turn, the items of the
// feed's tail and the rows sent from here it has not echoed yet, and the
// setters of the sheet over the run and of what goes into the composer.
export function Conversation({ name, id, live, archive, exec, snapshot, onBack, onUsage, onOpenChat, onTerm, parts }) {
    // A brief opens over the conversation, the way a subagent's letters do: a
    // layer above the run, put down by the same gesture and leaving the run
    // where it was. Sending the reader to a page of their own instead costs
    // them the conversation and the gesture both.
    const openBrief = useCallback((briefId) => setLook({ kind: "brief", id: briefId }), []);
    const toast = useToast();
    useViewing(live ? name : "");
    const [sub, setSub] = useState(null);
    const [repo, setRepo] = useState(false);
    const {
        state, shown, away, more, later, feedRef, topRef, bottomRef, onScroll, atEnd, toEnd, show,
    } = useFeedWindow({ name, id, live: live && !sub });
    const [calls, setCalls] = useState(null);
    const [look, setLook] = useState(null);
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
        setBriefs((was) => was.map((card) => (card.id === briefId ? { ...card, sent: true } : card)));
        setLook(null);
    };

    const asksSecret = state.items.some((item) => item.role === "secret")
        || (away && shown.some((item) => item.role === "secret"));
    const loadSecrets = useCallback(() => {
        secretShelf().then(setSecrets).catch(() => setSecrets(null));
    }, []);
    useEffect(() => {
        if (asksSecret) loadSecrets();
    }, [asksSecret, id, loadSecrets]);
    const openSecret = useCallback((card) => {
        setLook({ kind: "secret", item: card });
        loadSecrets();
    }, [loadSecrets]);

    useEffect(() => {
        setSub(null);
        setSecrets(null);
        setLocal([]);
        setFiles([]);
        setCalls(null);
        setLook(null);
        setPanel(false);
        setInsert(null);
    }, [name, id]);

    const backChat = useBackClose(true, onBack);
    // The search of the conversation: the bar over the feed, Ctrl+F at a
    // keyboard, an item of the tools on a phone.
    const find = useFeedFind({ name, id, feedRef, show, on: feedShown && !sub && !repo, under: backChat });


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
                    ? { ...l, hold: undefined, ...outcome(result, row.hold.asked) }
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
        // A line of the repository leads to the conversation it was written
        // in: that conversation opens in place of this one.
        const openTalk = onOpenChat
            ? (talk) => { setRepo(false); onOpenChat(talk); }
            : null;
        return html`<${RepoView} cwd=${here} name=${name}
                                 onBack=${() => setRepo(false)} onSend=${sendReview}
                                 onConversation=${openTalk} />`;
    }

    const feed = weld(state.items);
    // What the feed draws: the tail, or a window away from the end of the
    // conversation. The work under the feed is always the tail's.
    const drawn = away ? weld(shown) : feed;
    // The checklist of the work: on a phone a line over the composer, at a
    // desk a block at the top of the timeline column, where the room is.
    const checklist = (state.work && state.work.checklist) || null;

    const ctx = { wide, view, feedShown, feedRef, work: state.work, items: state.items, pending, setLook, setInsert };
    const own = look ? parts.look(look, ctx) : null;

    // A thread of codex in the archive keeps no fill of its context: its head
    // says what the thread spent instead, as its row in the archive does, and
    // nothing when it spent nothing. Null is the archive that keeps the fill.
    const pastCodex = !live && isCodex(archive);
    const pct = live ? live.pct : (archive && !pastCodex ? archive.pctMax : null);
    const spent = pastCodex ? (spoke(archive) ? `${tokens(archive.tokensUsed)} tokens` : "") : null;
    const stand = stateOf(live, still, move);
    const openRepo = here ? () => setRepo(true) : null;
    const tools = {
        name, live, archive, pct, spent, exec, snapshot, cwd: here, view, sides, win, way, work: state.work,
        sections: parts.sections, onWindow: place.onWindow, onLook: (kind) => setLook({ kind }),
    };
    const deskTools = html`
        <${ViewTabs} view=${view} sides=${live ? sides : {}} canTerm=${canTerm}
                     onView=${pickView} onRepo=${openRepo} onMove=${() => setPanel(true)} />
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

    return html`
        ${wide
            ? html`<${DeskHead} name=${shownName(live, name)} live=${live} archive=${archive} pct=${pct} spent=${spent}
                                move=${move} tools=${deskTools} />`
            : html`
        <${BackHead} kind="talk" onBack=${onBack} label="to sessions"
                     foot=${html`<${ContextBar} pct=${pct} peak=${!live} />`}
                     tools=${html`<${MoreButton} onOpen=${() => setLook({ kind: "tools" })} />`}>
            <div class="chathead">
                <h2>
                    <span class="talkdot" data-tone=${stand.tone} title=${stand.say}></span>
                    <${MidName} text=${shownName(live, name)} />
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
                    <span class="talkword" data-tone=${stand.tone}>${stand.word || (archive ? "peak" : "")}</span>
                </div>
                ${here && html`
                    <div class="chatwhere">
                        <span class="chatpath" title=${here}><bdi>${shortPath(here)}</bdi></span>
                    </div>
                `}
            </div>
        <//>
        `}

        ${move
            ? html`<${MoveScreen} move=${move} />`
            : view === "term"
            ? html`<${Term} name=${name} />`
            : html`
        <${FindBar} find=${find} />
        <div
            class="chatfeed"
            ref=${feedRef}
            onClick=${(event) => {
                if (codecopy.fromClick(event, toast)) return;
                if (codecopy.fromInline(event)) return;
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
            <${FeedGrid}
                rows=${rows(drawn, pending)}
                wide=${wide}
                checklist=${checklist}
                onOpen=${(g, badge) => setCalls(callsOf(g, drawn, runCalls, turnCalls, badge))}
                row=${(item, n) => html`<${Row}
                    key=${`${item.pos}-${n}`}
                    item=${item}
                    session=${name}
                    id=${id}
                    exec=${live && !live.outside ? exec : null}
                    copies=${copies}
                    onPage=${(card) => setLook({ kind: "artifact", card })}
                    onFile=${(file) => setLook({ kind: "file", ...file })}
                    onBrief=${openBrief}
                    briefs=${briefs}
                    secrets=${secrets}
                    onSecret=${live && !live.outside ? openSecret : null}
                    onCommand=${(row) => setLook({ kind: "command", item: row })}
                    onShell=${(row) => setLook({ kind: "shell", item: row })}
                    onTask=${(task) => (task.agent
                        ? openAgent({ id: task.id, name: task.name, kind: "background" })
                        : setLook({ kind: "task", id: task.id, text: task.name }))}
                    onAgent=${openAgent}
                />`}
                tail=${away ? [] : pending.map((row) => ({ key: `local-${row.key}`, role: row.role, node: html`
                    <${Row} item=${row} />
                    ${row.state === "queued" && live && parts.takesBack && html`
                        <${TakeBack} row=${row} name=${name} exec=${exec}
                                     onGone=${(key) => setLocal((was) => was.filter((l) => l.key !== key))}
                                     onEdit=${(text) => setInsert({ key: Date.now(), text, message: true })} />
                    `}
                ` }))}
            />
            ${later && html`<div class="mlater" ref=${bottomRef}>there is more below</div>`}
            ${!atEnd && html`<${JumpToEnd} onJump=${toEnd} away=${away} />`}
        </div>
        `}
        ${live && feedShown && !hasWork(state.work, live.status === "busy" || Boolean(live.compacting)) && live.lastRequestAt && html`
            <p class="lastreq">request ${ago(live.lastRequestAt)}</p>
        `}

        ${live && feedShown && html`
            <div class="composerbox">
                <${WorkStatus} work=${state.work} busy=${live.status === "busy"}
                               turnOver=${Boolean(live.turnOver)}
                               compacting=${live.transport === "stream" ? live.compacting || "" : ""}
                               feed=${feed} onCalls=${(run) => setCalls({ list: runCalls(feed, run) })}
                               onCall=${(run, call) => setCalls({ list: runCalls(feed, run), first: call })}
                               to=${parts.toBackground}
                               checklist=${wide ? null : checklist}
                               onChecklist=${() => setLook({ kind: "checklist" })}
                               onOpen=${(what) => setLook(what)} />
                ${live.outside
                    ? html`<p class="outsidenote">${outsideNote(live)}</p>`
                    : state.work && state.work.ask && !closed(state.items, state.work.ask.toolUseId)
                    && !hidesAsk(answer, state.work.ask.toolUseId)
                    ? html`<${Ask} ask=${state.work.ask} name=${name} exec=${exec} stream=${parts.askStream} codex=${isCodex(live)}
                                   onAnswered=${(use) => mark(answered(live, use, Date.now(), state.work.ask.at))} />`
                    : live.status === "waiting" && !holding
                    ? html`<${Permit} name=${name} exec=${exec} waitingFor=${live.waitingFor} codex=${isCodex(live)}
                                      onAnswered=${() => mark(answered(live, ""))} />`
                    : html`
                        <${Composer} name=${name} id=${id} exec=${exec} busy=${live.status === "busy"} stream=${live.transport === "stream"}
                                     ...${parts.composer(ctx)}
                                     hold=${holding}
                                     files=${files}
                                     onAsk=${() => setAsking(true)}
                                     onFiles=${(picked) => setFiles((was) => [...was, ...picked])}
                                     onDropFile=${(i) => setFiles((was) => was.filter((_, n) => n !== i))}
                                     onDropFiles=${() => setFiles([])}
                                     onLocal=${(row) => setLocal((was) => [...was, { ...row, after: lastPos(state.items) }])}
                                     onLocalDone=${(key, patch) => setLocal((was) =>
                                         was.map((l) => (l.key === key ? { ...l, ...patch } : l)))}
                                     insert=${insert}
                                     strip=${wide
                                         ? html`${parts.strip(ctx)}
                                             ${view !== "term" && html`<div class="cwork">
                                                 ${work}
                                                 <${WorkRefs} work=${state.work} pages=${myPages} briefs=${myBriefs} onOpen=${(what) => setLook(what)} />
                                                 ${termJump}
                                             </div>`}`
                                         : parts.strip(ctx)}
                                     focus=${`${name}|${id || ""}|${view}`} />
                    `}
            </div>
        `}

        ${live && feedShown && !wide && html`
            <div class="deck">
                ${live.tokensIn > 0 && html`
                    <button class="deckuse" type="button" aria-label="tokens in and out of this session, open usage"
                            onClick=${onUsage}>
                        <span class="deckin"><i aria-hidden="true">↑</i>${tokens(live.tokensIn)}</span>
                        <span class="deckout"><i aria-hidden="true">↓</i>${tokens(live.tokensOut)}</span>
                    </button>`}
                ${termJump}
                ${work}
                <div class="deckright">
                    <${WorkRefs} work=${state.work} pages=${myPages} briefs=${myBriefs} onOpen=${(what) => setLook(what)} />
                </div>
            </div>
        `}

        ${live && parts.sheets(ctx)}

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
                      onFile=${(file) => setLook({ kind: "file", ...file })} to=${parts.toBackground} />
        <//>

        <${Sheet} open=${Boolean(look)} onClose=${() => setLook(null)}
                  label=${look ? (own ? own.label : LOOK_NAMES[look.kind]) : ""} inner
                  doc=${Boolean(look) && (look.kind === "brief" || pageLook(look))}>
            ${look && (own
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
                : look.kind === "secret"
                ? (live ? html`<${SecretPad} key=${look.item.use || look.item.name} item=${look.item} session=${name}
                                             exec=${exec} shelf=${secrets} onSaved=${loadSecrets}
                                             onDone=${() => setLook(null)} />`
                    : html`<p class="cmdnote">The session has ended: there is no one to hand the secret to.</p>`)
                : look.kind === "tools"
                ? html`<${SessionTools} ...${tools}
                                        onRepo=${here ? () => { setLook(null); setRepo(true); } : null}
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

        ${parts.aside(ctx)}

        <${QuoteTip} quote=${quote} onQuote=${takeQuote} />
    `;
}
