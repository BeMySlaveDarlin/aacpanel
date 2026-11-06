// The feed of a subagent, a teammate or a background agent — as a conversation.

import { useCallback, useState } from "preact/hooks";

import { html } from "../../html.js";
import { BackHead, useBackClose } from "../../ui/back.js";
import { Sheet } from "../../ui/sheet.js";
import { rows, runCalls, turnCalls, weld } from "./feed.js";
import { JumpToEnd, useFeedWindow } from "./feedwindow.js";
import { FeedRow } from "./rows.js";
import { usePieceMenu } from "./feedmenu.js";
import { callsOf, FeedGrid } from "./timeline.js";
import { useWide } from "../../ui/wide.js";
import { useSteady } from "../../ui/pure.js";
import { Calls } from "./calls.js";
import { Look, LOOK_NAMES, pageLook, WORK_LISTS } from "./look.js";
import { Marquee } from "./head.js";
import { contextSay } from "./work.js";
import { useToast } from "../../ui/toasts.js";
import * as codecopy from "./copy.js";
import { agentKey, agentName, isCodex } from "../../agent.js";

const KIND_NAMES = {
    teammate: "teammate",
    subagent: "subagent",
    background: "background agent",
};

// subFeedId returns the address of an agent feed: the conversation and the agent.
function subFeedId(session, agentId) {
    if (!session || !agentId) return "";
    return `${session}:${agentId}`;
}

// agentFeedId returns the address of the feed of an agent the conversation
// started. A run of codex exec is a thread of its own, and its feed is that
// thread's, by its id; the feed of any other agent lies beside the
// conversation's.
export function agentFeedId(session, agent) {
    if (isCodex(agent)) return agent.id || "";
    return subFeedId(session, agent.id);
}

// SubChat renders the feed of an agent. An agent of codex starts agents of
// its own, and the feed of one opens over this one, by the id of its thread.
export function SubChat({ session, id, agent, live, onBack }) {
    const toast = useToast();
    const [sub, setSub] = useState(null);
    const { state, more, feedRef, topRef, onScroll, atEnd, toEnd } = useFeedWindow({
        name: session, id, live: live && !sub,
    });
    const [calls, setCalls] = useState(null);
    const [look, setLook] = useState(null);
    const wide = useWide();

    useBackClose(true, onBack);
    const openPath = (path) => setLook({ kind: "file", path });
    const pieceMenu = usePieceMenu(openPath);
    // What the rows are handed keeps one identity across renders: a handler
    // made anew would draw every row again (see FeedRow).
    const openFile = useCallback((file) => setLook({ kind: "file", ...file }), []);
    const openAgent = useSteady((next) => {
        const feedId = agentFeedId(session, next);
        if (feedId) setSub({ ...next, feedId });
    });

    if (sub) {
        return html`<${SubChat} session=${session} id=${sub.feedId} agent=${sub} live=${live}
                                onBack=${() => setSub(null)} />`;
    }

    const feed = weld(state.items);
    const kind = KIND_NAMES[agent.kind] || "agent";

    return html`
        <${BackHead} onBack=${onBack} label="to the conversation">
            <div class="chathead">
                <h2>
                    <${Marquee} text=${agent.name} />
                </h2>
                <div class="chatsub">
                    ${isCodex(agent)
                        ? html`<span class="agentword" data-agent=${agentKey(agent)}>${agentName(agent)}</span>`
                        : html`<span>${kind}</span>`}
                    ${agent.model && html`<span class="sep">·</span><span>${agent.model}</span>`}
                    ${agent.tokens > 0 && html`<span class="sep">·</span><span>${contextSay(agent)}</span>`}
                </div>
                ${agent.text && html`<div class="chatsub"><span>${agent.text}</span></div>`}
            </div>
        <//>

        <div
            class="chatfeed"
            ref=${feedRef}
            onClick=${(event) => {
                if (codecopy.fromClick(event, toast)) return;
                if (codecopy.fromInline(event)) return;
                const hit = event.target.closest && event.target.closest(".path");
                if (hit) openPath(hit.dataset.path);
            }}
            onContextMenuCapture=${pieceMenu}
            onScroll=${onScroll}
        >
            ${state.kind === "loading" && html`<p class="hint">Reading the agent feed…</p>`}
            ${state.kind === "failed" && html`<p class="hint crit">${state.error}</p>`}
            ${state.kind === "fresh" && html`
                <p class="empty">the ${kind} has only just started — the feed appears with the first word.</p>
            `}
            ${state.kind === "ready" && state.items.length === 0 && html`
                <p class="empty">Nothing has been said in this feed yet.</p>
            `}
            ${more && html`
                <div class="mearlier" ref=${topRef}>there is more above</div>
            `}
            ${state.note && html`<p class="hint warn">${state.note}</p>`}
            <${FeedGrid}
                rows=${rows(feed)}
                wide=${wide}
                onOpen=${(g, badge) => setCalls(callsOf(g, feed, runCalls, turnCalls, badge))}
                row=${(item, n) => html`<${FeedRow}
                    key=${`${item.pos}-${n}`}
                    item=${item}
                    session=${session}
                    id=${id}
                    onFile=${openFile}
                    onAgent=${openAgent}
                />`}
            />
            ${!atEnd && html`<${JumpToEnd} onJump=${toEnd} />`}
        </div>

        <${Sheet} open=${Boolean(calls)} onClose=${() => setCalls(null)} label="tool calls" inner>
            <${Calls} session=${session} id=${id} calls=${calls ? calls.list : []} turn=${calls && calls.turn}
                      onFile=${(file) => setLook({ kind: "file", ...file })} />
        <//>

        <${Sheet} open=${Boolean(look)} onClose=${() => setLook(null)}
                  label=${look ? LOOK_NAMES[look.kind] : ""} inner doc=${pageLook(look)}>
            ${look && !WORK_LISTS.has(look.kind)
                && html`<${Look} session=${session} id=${id} look=${look}
                                 onBack=${pageLook(look) ? () => setLook(null) : undefined} />`}
        <//>
    `;
}
