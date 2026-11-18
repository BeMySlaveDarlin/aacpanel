import { html } from "../../html.js";
import { ago } from "../../format.js";
import { Icon } from "../../ui/icons.js";
import { actOpen, chainSummary, isChain } from "./chain.js";

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
    const kind = item.role;
    const open = record ? actOpen(record) : true;
    const chain = isChain(record || item);
    const outcome = chain ? chainSummary(record || item) : record?.withdrawn ? "Withdrawn" : record?.outcome === "done" ? "Done"
        : record?.outcome === "cant" ? "Can't" : record?.state === "declined" ? "Won't give"
        : record?.answered ? "Filled in" : "yours · below";
    return html`<div class=${`actmark${open ? " open" : " done"}`} data-act=${item.id}>
        <button type="button" class="actmarker" onClick=${() => onOpen && onOpen(item)} disabled=${!open || !onOpen}>
            ${kind === "secret" ? Icon.key() : Icon.list()}<span class="actkind">${chain ? "chain" : kind}</span>
            <span class="acttitle">${record?.title || item.title}</span><small>${outcome}</small>
        </button>
        ${(item.actReplies || []).map((text) => html`<${ActReply} text=${text} />`)}
    </div>`;
}

export function ActReply({ text }) {
    return html`<details class="actreply"><summary>to the agent: ${text}</summary><p>${text}</p></details>`;
}

// Only an id present in the act data identifies a reply; its prose can change
// without changing how the feed groups it. Legacy replies keep their parser.
export function withActReplies(items, acts) {
    const markers = new Set(items.filter((item) => ["request", "secret"].includes(item.role)).map((item) => item.id));
    const known = Object.values(acts).filter((act) => ["do", "secret"].includes(act.kind) && (isChain(act) || act.state !== "open"));
    const replies = new Map();
    const rest = [];
    for (const item of items) {
        const act = item.role === "me" && !item.cut && item.state !== "failed"
            && known.find((act) => item.text?.includes(act.id));
        if (!act) rest.push(item);
        else if (!markers.has(act.id)) rest.push({ ...item, actReply: true });
        else replies.set(act.id, [...(replies.get(act.id) || []), item.text]);
    }
    return rest.map((item) => replies.has(item.id) ? { ...item, actReplies: replies.get(item.id) } : item);
}
