import { useState } from "preact/hooks";

import { html } from "../../html.js";
import { ago } from "../../format.js";
import { Icon } from "../../ui/icons.js";
import { actOpen, ChainCard, chainSummary, isChain } from "./chain.js";

const kindOf = { do: "request", ask: "question" };
const timeOf = (item) => new Date(item.at || 0).getTime() || 0;
export const moveKey = (item) => `${item.kind}:${item.id || item.at || ""}`;

// Summaries locate markers; the act supplies its state and its form even when
// that marker is outside the feed window. A closed act wins over a stale poll.
export function movesOf(acts, waiting, native) {
    const moves = new Map();
    for (const item of waiting) {
        const act = acts[item.id];
        if (act && !actOpen(act)) continue;
        moves.set(moveKey(item), { ...item, ...act, kind: item.kind, at: item.at || act?.created });
    }
    for (const act of Object.values(acts)) {
        if (!actOpen(act)) continue;
        const kind = kindOf[act.kind] || act.kind;
        const key = moveKey({ ...act, kind });
        moves.set(key, { ...moves.get(key), ...act, kind, at: act.created });
    }
    for (const item of native) moves.set(moveKey(item), item);
    return [...moves.values()].sort((a, b) => Number(Boolean(b.blocking)) - Number(Boolean(a.blocking))
        || timeOf(a) - timeOf(b));
}

export function MovePill({ items, onNext }) {
    if (!items.length) return null;
    const oldest = items.reduce((a, b) => timeOf(a) < timeOf(b) ? a : b);
    return html`<button class="movepill" type="button" onClick=${onNext}>
        <span>✋ your move · ${items.length}</span><small> · ${ago(oldest.at)}</small>
    </button>`;
}

export function ActMarker({ item, record, onOpen }) {
    const kind = item.role === "ask" ? "question" : item.role;
    const question = kind === "question";
    const open = record ? actOpen(record) : true;
    const chain = isChain(record || item);
    const outcome = chain ? chainSummary(record || item) : record?.withdrawn ? "Withdrawn" : record?.outcome === "done" ? "Done"
        : record?.outcome === "cant" ? "Can't" : record?.state === "declined" ? "Won't give"
        : record?.answered || record?.state === "answered" ? (question ? "Answered" : "Filled in") : question ? "below" : "yours · below";
    const body = record || item;
    const count = (body.questions || body.body?.questions || []).length;
    return html`<div class=${`actmark${open ? " open" : " done"}`} data-act=${item.id}>
        <button type="button" class="actmarker" onClick=${() => onOpen && onOpen(item)} disabled=${!open || !onOpen}>
            ${kind === "secret" ? Icon.key() : question ? Icon.ask() : Icon.list()}<span class="actkind">${chain ? "chain" : kind}</span>
            <span class="acttitle">${question ? body.what || body.body?.what || body.title || item.title : record?.title || item.title}</span>
            <small>${question ? `${count} ${count === 1 ? "question" : "questions"} · ${outcome}` : outcome}</small>
        </button>
        ${(item.actReplies || []).map((text) => html`<${ActReply} text=${text} record=${record} onOpen=${onOpen} />`)}
    </div>`;
}

export function ActReply({ text, record, onOpen, detached = false }) {
    const [expanded, setExpanded] = useState(false);
    if (record?.kind === "ask") return html`<div class="actreply">→ agent · answered</div>`;
    if (!isChain(record)) return html`<details class="actreply"><summary>to the agent: ${text}</summary><p>${text}</p></details>`;
    const open = actOpen(record);
    const press = open && onOpen ? () => onOpen({ ...record, role: "request" })
        : !open && detached ? () => setExpanded(!expanded) : null;
    const line = chainReply(text, record);
    // The card stands beside the line, not in it: the line's faint small type
    // would carry over to the card.
    return html`<div class="actreply">
        ${press ? html`<button class="chainreply" type="button" onClick=${press}
            aria-expanded=${!open ? expanded : undefined}>${line}</button>` : html`<div class="chainreply">${line}</div>`}
    </div>
    ${detached && !open && expanded && html`<${ChainCard} item=${record} record=${record} />`}`;
}

// Prose has a transcript anchor; tool asks can arrive before their tool
// result. The act's time places either marker when that anchor is absent.
function withAskMarkers(items, acts, waiting, tail) {
    const asks = Object.values(acts).filter((act) => act.kind === "ask");
    const list = items.map((item) => {
        if (item.role !== "question") return item;
        const act = asks.find((act) => act.id === item.id || (act.message || act.body?.message) === item.id);
        return act ? { ...item, role: "ask", id: act.id } : item;
    });
    const first = timeOf(items.find((item) => item.at) || {});
    const last = timeOf(items.findLast((item) => item.at) || {});
    for (const act of asks) {
        if (list.some((item) => item.role === "ask" && item.id === act.id)) continue;
        const at = act.created;
        const stamp = timeOf({ at });
        const anchor = waiting.find((item) => item.kind === "question" && item.id === act.id)?.pos;
        const anchored = anchor != null && items.some((item) => item.pos === anchor);
        if (!anchored && (!stamp || stamp < first || !tail && stamp > last)) continue;
        const n = anchored ? list.findLastIndex((item) => item.pos <= anchor)
            : list.findLastIndex((item) => timeOf(item) <= stamp);
        list.splice(n + 1, 0, { role: "ask", id: act.id, title: act.title, at, pos: anchor ?? list[n]?.pos ?? 0 });
    }
    return list;
}

function chainReply(text, act) {
    const steps = act.steps || act.body.steps;
    const numbers = [...new Set([...text.matchAll(/^(\d+) (you|agent) · /gm)].map(match => Number(match[1])))]
        .filter(number => number > 0 && number <= steps.length).sort((a, b) => a - b);
    const prefix = "→ agent · ";
    if (!numbers.length) return prefix + "chain update";
    const stopped = numbers.find(number => ["failed", "cant"].includes(act.outputs?.[number - 1]?.state));
    if (stopped) {
        const out = act.outputs[stopped - 1];
        return prefix + (out.state === "cant" ? `Can't at step ${stopped}`
            : `step ${stopped} failed${out.exit != null ? ` · exit ${out.exit}` : ""}`);
    }
    if (!numbers.every(number => act.outputs?.[number - 1]?.state === "done")) return prefix + "chain update";
    const first = numbers[0], last = numbers.at(-1);
    const covered = numbers.length === 1 ? `step ${first}`
        : `steps ${last - first + 1 === numbers.length ? `${first}–${last}` : numbers.join(", ")}`;
    const next = last === steps.length && act.state === "answered" ? " · chain done"
        : steps[last]?.who === "agent" ? ` · step ${last + 1} is the agent's` : "";
    return prefix + covered + " done" + next;
}

// Only an id present in the act data identifies a reply; its prose can change
// without changing how the feed groups it. Legacy replies keep their parser.
export function withActReplies(items, acts, waiting = [], tail = true) {
    items = withAskMarkers(items, acts, waiting, tail);
    const markers = new Set(items.filter((item) => ["request", "secret", "ask"].includes(item.role)).map((item) => item.id));
    const known = Object.values(acts).filter((act) => ["do", "secret", "ask"].includes(act.kind) && (isChain(act) || act.state !== "open"));
    const replies = new Map();
    const rest = [];
    for (const item of items) {
        const act = item.role === "me" && !item.cut && item.state !== "failed"
            && known.find((act) => item.text?.includes(act.id));
        if (!act) rest.push(item);
        else if (!markers.has(act.id)) rest.push({ ...item, actReply: true, actReplyID: act.id });
        else replies.set(act.id, [...(replies.get(act.id) || []), item.text]);
    }
    return rest.map((item) => replies.has(item.id) ? { ...item, actReplies: replies.get(item.id) } : item);
}
