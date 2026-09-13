// The feed of a subagent, a teammate or a background agent — as a conversation.

import { useState } from "preact/hooks";

import { html } from "../../html.js";
import { BackHead, useBackClose } from "../../ui/back.js";
import { Sheet } from "../../ui/sheet.js";
import { rows, runCalls, weld } from "./feed.js";
import { JumpToEnd, useFeedWindow } from "./feedwindow.js";
import { Row } from "./rows.js";
import { Calls } from "./calls.js";
import { Look, LOOK_NAMES, WORK_LISTS } from "./look.js";
import { Marquee } from "./head.js";
import { useToast } from "../../ui/toasts.js";
import * as codecopy from "./copy.js";

const KIND_NAMES = {
    teammate: "teammate",
    subagent: "subagent",
    task: "background agent",
};

// subFeedId returns the address of an agent feed: the conversation and the agent.
export function subFeedId(session, agentId) {
    if (!session || !agentId) return "";
    return `${session}:${agentId}`;
}

// SubChat renders the feed of an agent.
export function SubChat({ session, id, agent, live, onBack }) {
    const toast = useToast();
    const { state, more, feedRef, topRef, onScroll, atEnd, toEnd } = useFeedWindow({ name: session, id, live });
    const [calls, setCalls] = useState(null);
    const [look, setLook] = useState(null);

    useBackClose(true, onBack);

    const feed = weld(state.items);
    const kind = KIND_NAMES[agent.kind] || "agent";

    return html`
        <${BackHead} onBack=${onBack} label="to the conversation">
            <div class="chathead">
                <h2>
                    <${Marquee} text=${agent.name} />
                </h2>
                <div class="chatsub">
                    <span>${kind}</span>
                    ${agent.model && html`<span class="sep">·</span><span>${agent.model}</span>`}
                </div>
                ${agent.text && html`<div class="chatsub"><span>${agent.text}</span></div>`}
            </div>
        <//>

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
            ${rows(feed).map((item, n) => html`<${Row}
                key=${`${item.pos}-${n}`}
                item=${item}
                session=${session}
                id=${id}
                onCalls=${() => setCalls(runCalls(feed, item.run))}
                onFile=${(file) => setLook({ kind: "file", ...file })}
            />`)}
            ${!atEnd && html`<${JumpToEnd} onJump=${toEnd} />`}
        </div>

        <${Sheet} open=${Boolean(calls)} onClose=${() => setCalls(null)} label="tool calls" inner>
            <${Calls} session=${session} id=${id} calls=${calls || []}
                      onFile=${(file) => setLook({ kind: "file", ...file })} />
        <//>

        <${Sheet} open=${Boolean(look)} onClose=${() => setLook(null)}
                  label=${look ? LOOK_NAMES[look.kind] : ""} inner>
            ${look && !WORK_LISTS.has(look.kind)
                && html`<${Look} session=${session} id=${id} look=${look} />`}
        <//>
    `;
}
