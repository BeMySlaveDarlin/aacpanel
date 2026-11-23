import { html } from "../../html.js";
import { ago } from "../../format.js";
import { Icon } from "../../ui/icons.js";
import { actOpen, agentsMove, ChainCard, chainSummary, isChain } from "./chain.js";

const kindOf = { do: "request" };
const timeOf = (item) => new Date(item.at || 0).getTime() || 0;
export const moveKey = (item) => `${item.kind}:${item.id || item.at || ""}`;

// Summaries locate markers; the act supplies its state and its form even when
// that marker is outside the feed window. A closed act wins over a stale poll.
export function movesOf(acts, waiting, native) {
    const moves = new Map();
    for (const item of waiting) {
        const act = acts[item.id];
        if (["claim", "question"].includes(item.kind) || ["claim", "ask"].includes(act?.kind)) continue;
        if (act && !actOpen(act)) continue;
        moves.set(moveKey(item), { ...item, ...act, kind: item.kind, at: item.at || act?.created });
    }
    for (const act of Object.values(acts)) {
        if (["claim", "ask"].includes(act.kind)) continue;
        if (!actOpen(act)) continue;
        const kind = kindOf[act.kind] || act.kind;
        const key = moveKey({ ...act, kind });
        moves.set(key, { ...moves.get(key), ...act, kind, at: act.created });
    }
    for (const item of native) moves.set(moveKey(item), item);
    // The person's moves come before the chains standing on the agent's turn.
    return [...moves.values()].sort((a, b) => Number(agentsMove(a)) - Number(agentsMove(b))
        || Number(Boolean(b.blocking)) - Number(Boolean(a.blocking)) || timeOf(a) - timeOf(b));
}

// The icon of each kind of move; a request and a chain share the list.
const moveIcon = { asked: Icon.ask, request: Icon.list, secret: Icon.key, permit: Icon.lock, brief: Icon.plan };

// MovePill is one square of the row, the size of the tools beside it whatever
// waits: the icon says what the first move is, the corner how many there are,
// and the words and the time stand in its label. A pill as wide as its words
// pushed the tools of the row onto a second line. With no move of the
// person's left, the chains on the agent's turn stand in it as the agent's.
export function MovePill({ items, onNext }) {
    if (!items.length) return null;
    const yours = items.filter((item) => !agentsMove(item));
    const agent = yours.length === 0;
    const shown = agent ? items : yours;
    const oldest = shown.reduce((a, b) => timeOf(a) < timeOf(b) ? a : b);
    const first = shown[0];
    const label = `${agent ? "agent's step" : "your move"} · ${shown.length} · ${ago(oldest.at)}`;
    return html`<button class=${`movepill${agent ? " agent" : ""}`} type="button" data-kind=${agent ? "agent" : first.kind}
        aria-label=${label} title=${label} onClick=${onNext}>
        ${(agent ? Icon.robot : moveIcon[first.kind] || Icon.hand)()}<span class="movecount" aria-hidden="true">${shown.length}</span>
    </button>`;
}

// actState is the word the head of an act's card says it stands at: how the
// person answered it, or that it is theirs to answer.
export function actState(act) {
    if (actOpen(act)) return "yours";
    if (act.state === "withdrawn") return "withdrawn";
    if (act.kind === "secret") return act.state === "declined" ? "won't give" : "filled in";
    return act.outcome === "done" ? "done" : "can't";
}

// ActMarker stands in the row of a chain until its act is read: the
// transcript tells a chain by its steps, and its card is drawn from the act.
export function ActMarker({ item, onOpen }) {
    return html`<div class="actmark open" data-act=${item.id}>
        <button type="button" class="actmarker" onClick=${() => onOpen && onOpen(item)} disabled=${!onOpen}>
            ${Icon.list()}<span class="actkind">chain</span>
            <span class="acttitle">${item.title}</span>
            <small>${chainSummary(item)}</small>
        </button>
        ${(item.actReplies || []).map((text) => html`<${ActReply} text=${text} onOpen=${onOpen} />`)}
    </div>`;
}

export function AskCard({ item, record }) {
    const act = record || item;
    const questions = act.questions || act.body?.questions || [];
    const answers = act.answer?.answers;
    const state = act.state === "withdrawn" ? "withdrawn" : act.state === "declined" ? "declined"
        : answers?.length ? "answered" : "answered in chat";
    // A tool ask names itself by its first question: printing it twice
    // would read as two questions.
    const title = act.what || act.body?.what || act.title;
    const heading = questions.some((q) => q.question === title) ? "" : title;
    return html`<article class="sent asked mquestion" data-act=${act.id}>
        <div class="senthead"><span class="sentico">${Icon.ask()}</span>
            <span class="sentlabel">question · ${state}</span>
            <span class="sentat">${ago(act.created || item.at)}</span></div>
        ${heading && html`<h3 class="requesttitle">${heading}</h3>`}
        <div class="mflist">${questions.map((q, i) => html`<div class="mfile askedrow" key=${i}>
            ${q.header && html`<span class="mftag">${q.header}</span>`}
            <span class="askedq">${q.question}</span>
            ${answers?.[i]?.picks?.length > 0 && html`<span class="askeda">${answers[i].picks.join(", ")}</span>`}
            ${answers?.[i]?.text && html`<span class="pnmnote">${answers[i].text}</span>`}
        </div>`)}</div>
    </article>`;
}

// A finished chain whose own row is out of the feed stands as its card under
// the last line of its replies, open as it is: one card per chain, whatever
// number of replies it took.
export function ActReply({ text, record, onOpen, card = false }) {
    if (record?.kind === "ask") return html`<div class="actreply">→ agent · answered</div>`;
    if (!isChain(record)) return html`<details class="actreply"><summary>to the agent: ${text}</summary><p>${text}</p></details>`;
    const open = actOpen(record);
    const line = chainReply(text, record);
    // The card stands beside the line, not in it: the line's faint small type
    // would carry over to the card.
    return html`<div class="actreply">
        ${open && onOpen ? html`<button class="chainreply" type="button" onClick=${() => onOpen({ ...record, role: "request" })}>${line}</button>`
            : html`<div class="chainreply">${line}</div>`}
    </div>
    ${card && !open && html`<${ChainCard} item=${record} record=${record} />`}`;
}

// Closed asks stay in the feed at their recorded time, even without a transcript marker.
function withAskMarkers(items, acts, tail) {
    const asks = Object.values(acts).filter((act) => act.kind === "ask" && !actOpen(act));
    const list = [...items];
    const first = timeOf(items.find((item) => item.at) || {});
    const last = timeOf(items.findLast((item) => item.at) || {});
    for (const act of asks) {
        if (list.some((item) => item.role === "ask" && item.id === act.id)) continue;
        const at = act.created;
        const stamp = timeOf({ at });
        if (!stamp || stamp < first || !tail && stamp > last) continue;
        const n = list.findLastIndex((item) => timeOf(item) <= stamp);
        list.splice(n + 1, 0, { role: "ask", id: act.id, title: act.title, at, pos: list[n]?.pos ?? 0 });
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
export function withActReplies(items, acts, tail = true) {
    items = withAskMarkers(items, acts, tail);
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
    const last = new Map(rest.map((item, i) => [item.actReplyID, i]));
    return rest.map((item, i) => replies.has(item.id) ? { ...item, actReplies: replies.get(item.id) }
        : item.actReply && last.get(item.actReplyID) === i ? { ...item, actReplyCard: true } : item);
}
