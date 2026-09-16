import { useCallback, useEffect, useMemo, useRef, useState } from "preact/hooks";

import { html } from "../html.js";
import { BackHead, useBackClose } from "../ui/back.js";
import { Sheet } from "../ui/sheet.js";
import { Icon } from "../ui/icons.js";
import { useToast } from "../ui/toasts.js";
import * as codecopy from "./chat/copy.js";
import { ContextBar } from "../ui/bar.js";
import { Ask } from "./ask.js";
import { Permit } from "./chat/permit.js";
import { ago, tokens } from "../format.js";
import { closed, rows, runCalls, unarrived, weld } from "./chat/feed.js";
import { JumpToEnd, useFeedWindow } from "./chat/feedwindow.js";
import { SubChat, subFeedId } from "./chat/subchat.js";
import { Row } from "./chat/rows.js";
import { Calls } from "./chat/calls.js";
import { Look, LOOK_NAMES, WORK_LISTS } from "./chat/look.js";
import { ArtifactPage } from "./artifact.js";
import { index, shelf as pageShelf } from "../data/artifacts.js";
import { shelf as briefShelf } from "../data/briefs.js";
import { hasWork, Work, WorkList, WorkRefs, WorkStatus } from "./chat/work.js";
import { Composer, deliver, outcome } from "./chat/composer.js";
import { ANSWER_LAG_MS, answered, hidesAsk, lagging, recall, remember, settle } from "./chat/answered.js";
import { useAction } from "../actions/gate.js";
import { QuoteTip, useSelectionQuote } from "./chat/quotetip.js";
import { Marquee, short } from "./chat/head.js";
import { AttachSheet, HeadTools } from "./chat/tools.js";
import { DeskHead, ViewToggle } from "./chat/deskhead.js";
import { WindowToggle } from "./chat/window.js";
import { Term, useTermAvailable } from "./chat/term.js";
import { useViewPick } from "./chat/viewpick.js";
import { useViewing } from "../viewing.js";
import { useWide } from "../ui/wide.js";


export function Chat({ name, id, live, archive, exec, onBack, onUsage, onBrief }) {
    const toast = useToast();
    useViewing(live ? name : "");
    const [sub, setSub] = useState(null);
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

    const wide = useWide();
    const term = useTermAvailable();
    const canTerm = term.ok && Boolean(live);
    const [view, pickView] = useViewPick(canTerm, wide);

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

    const feed = weld(state.items);

    const pct = live ? live.pct : (archive ? archive.pctMax : null);

    return html`
        ${wide
            ? html`<${DeskHead} name=${name} live=${live} archive=${archive} pct=${pct}
                                view=${view} canTerm=${canTerm} exec=${exec} onView=${pickView} />`
            : html`
        <${BackHead} onBack=${onBack} label="to sessions" foot=${html`<${ContextBar} pct=${pct} peak=${!live} />`}
                     tools=${live && html`
                         ${canTerm && html`<${ViewToggle} view=${view} onView=${pickView} />`}
                         <${WindowToggle} name=${name} exec=${exec} />
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
                ${live && html`<${HeadTools} live=${live} />`}
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
                onBrief=${onBrief}
            />`)}
            ${pending.map((row) => html`<${Row} key=${`local-${row.key}`} item=${row} />`)}
            ${!atEnd && html`<${JumpToEnd} onJump=${toEnd} />`}
        </div>
        `}
        ${live && !hasWork(state.work, live.status === "busy") && live.lastRequestAt && html`
            <p class="lastreq">request ${ago(live.lastRequestAt)}</p>
        `}

        ${live && view !== "term" && html`
            <div class="composerbox">
                <${WorkStatus} work=${state.work} busy=${live.status === "busy"} />
                ${state.work && state.work.ask && !closed(state.items, state.work.ask.toolUseId)
                    && !hidesAsk(answer, state.work.ask.toolUseId)
                    ? html`<${Ask} ask=${state.work.ask} name=${name} exec=${exec}
                                   onAnswered=${(use) => mark(answered(live, use))} />`
                    : live.status === "waiting" && !holding
                    ? html`<${Permit} name=${name} exec=${exec} waitingFor=${live.waitingFor}
                                      onAnswered=${() => mark(answered(live, ""))} />`
                    : html`
                        <${Composer} name=${name} id=${id} exec=${exec} busy=${live.status === "busy"}
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
                                     focus=${`${name}|${id || ""}|${view}`} />
                    `}
            </div>
        `}

        ${live && view !== "term" && html`
            <div class="deck">
                ${live.tokensIn > 0 && html`
                    <button class="deckuse" type="button" title="tokens in and out of this session — open usage"
                            aria-label="tokens in and out of this session, open usage" onClick=${onUsage}>
                        <span class="deckin"><i>in</i>${tokens(live.tokensIn)}</span>
                        <span class="deckout"><i>out</i>${tokens(live.tokensOut)}</span>
                    </button>`}
                <${Work} work=${state.work} onOpen=${(what) => setLook(what)} />
                <div class="deckright">
                    <${WorkRefs} work=${state.work} briefs=${myBriefs} onOpen=${(what) => setLook(what)} />
                </div>
            </div>
        `}

        ${live && html`<${AttachSheet}
            open=${asking}
            onClose=${() => setAsking(false)}
            exec=${exec}
            files=${files}
            onFiles=${(picked) => setFiles((was) => [...was, ...picked])}
        />`}

        <${Sheet} open=${Boolean(calls)} onClose=${() => setCalls(null)} label="tool calls" inner>
            <${Calls} session=${name} id=${id} calls=${calls || []}
                      onFile=${(file) => setLook({ kind: "file", ...file })} />
        <//>

        <${Sheet} open=${Boolean(look)} onClose=${() => setLook(null)}
                  label=${look ? LOOK_NAMES[look.kind] : ""} inner>
            ${look && (look.kind === "artifact"
                ? html`<${ArtifactPage} card=${look.card} />`
                : WORK_LISTS.has(look.kind)
                ? html`<${WorkList} session=${name} id=${id} kind=${look.kind} work=${state.work}
                                    exec=${exec} onAgent=${openAgent}
                                    pages=${myPages} briefs=${myBriefs} onBrief=${onBrief}
                                    onPage=${(card) => setLook({ kind: "artifact", card })} />`
                : html`<${Look} session=${name} id=${id} look=${look} />`)}
        <//>

        <${QuoteTip} quote=${quote} onQuote=${takeQuote} />
    `;
}
