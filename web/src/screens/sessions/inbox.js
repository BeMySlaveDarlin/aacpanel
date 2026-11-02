// Outstanding work across contours, using the queue and the session rows' state.
import { useEffect, useState } from "preact/hooks";

import { html } from "../../html.js";
import { ago } from "../../format.js";
import { isCodex, shownName } from "../../agent.js";
import { knows } from "../../exec.js";
import { useAction } from "../../actions/gate.js";
import { useWaiting, waitingOf } from "../../waiting.js";
import { Ask } from "../ask.js";
import { answered, remember, shown } from "../chat/answered.js";
import { idParam } from "../chat/api.js";
import { QuickAnswers, replyToQuestion } from "../chat/request.js";
import { stateOf, useAnswers } from "./blocks.js";
import { openedBy } from "./kin.js";
import { contourOfLive, pagesOf } from "./map.js";

export const INBOX = "\u0000inbox";

// inboxOf keeps the contour order and the oldest item first inside each one.
// Questions and permissions have their own docks, so waitingOf omits them;
// the same state that ranks a session supplies those remaining stops.
export function inboxOf(profiles, sessions, names, limits) {
    const ordered = names.length ? names : [...new Set(sessions.map(contourOfLive(profiles, sessions)))];
    return [...pagesOf(profiles, sessions, ordered)].map(([name, own]) => {
        const items = own.flatMap((session) => {
            const live = shown(session);
            const pending = waitingOf(live);
            if (live.ask) pending.push({ kind: "asked", id: live.ask.toolUseId,
                title: live.ask.text || live.ask.header || "A question", at: live.ask.at });
            const state = stateOf(live, limits);
            if (state.tone === "wait" && !live.ask && !pending.some((item) => item.kind === "question")) {
                pending.push({ kind: "state", title: state.text,
                    at: live.statusUpdatedAt ? new Date(live.statusUpdatedAt).toISOString() : "" });
            }
            return pending.map((item) => ({ session, item }));
        });
        items.sort((a, b) => (Date.parse(a.item.at) || Infinity) - (Date.parse(b.item.at) || Infinity));
        return { name, items };
    }).filter((group) => group.items.length);
}

// useInbox follows local answers before the next snapshot arrives.
export function useInbox(profiles, sessions, names, limits) {
    useWaiting(null);
    useAnswers();
    return inboxOf(profiles, sessions, names, limits);
}

function pathOf(session, sessions) {
    const byName = new Map(sessions.map((s) => [s.session, s]));
    const path = [shownName(session)];
    const seen = new Set([session.session]);
    let parent = openedBy(session);
    while (parent && !seen.has(parent)) {
        seen.add(parent);
        const live = byName.get(parent);
        path.unshift(live ? shownName(live) : parent);
        parent = live ? openedBy(live) : "";
    }
    return path.join(" → ");
}

function InboxRow({ session, item, sessions, exec, onChat, active }) {
    const run = useAction();
    const [detail, setDetail] = useState(null);
    const [error, setError] = useState("");
    const live = shown(session);
    const askId = item.kind === "asked" ? item.id : "";
    useEffect(() => {
        if (!active || !askId || session.outside) return;
        let gone = false;
        setDetail(null);
        setError("");
        fetch(`/api/chat?session=${encodeURIComponent(session.session)}${idParam(session.sessionId)}&limit=1`)
            .then((r) => { if (!r.ok) throw new Error("Could not read the question's options"); return r.json(); })
            .then((data) => { if (!gone) setDetail(data.state && data.state.ask || null); })
            .catch((err) => { if (!gone) setError(String(err.message || err)); });
        return () => { gone = true; };
    }, [active, askId, session.sessionId, session.outside]);
    const canReply = !session.outside && !isCodex(session) && !live.ask && live.status !== "waiting" && knows(exec, "session.send");
    const quick = item.kind === "question" && (item.answers || []).length > 0 && canReply;
    const ask = detail && detail.toolUseId === askId && (detail.questions || []).some((q) => (q.options || []).length) ? detail : null;
    const open = () => onChat({ name: session.session, id: session.sessionId,
        waiting: ["asked", "state"].includes(item.kind) ? null : item });
    return html`<article class="inboxrow" data-session=${session.session} data-kind=${item.kind}>
        <button class="inboxopen" type="button" onClick=${open}>
            <span class="inboxname">${pathOf(session, sessions)}</span>
            <span class="inboxwhat">${item.title}</span>
            <span class="inboxsince">${item.at ? ago(item.at) : "waiting"}</span>
        </button>
        ${quick && html`<${QuickAnswers} answers=${item.answers} compact
            onAnswer=${(value, note) => replyToQuestion(run, session, value, note)} />`}
        ${ask && html`<${Ask} key=${askId} ask=${ask} name=${session.session} exec=${exec}
            stream=${session.transport === "stream"} codex=${isCodex(session)} inline
            onAnswered=${(use) => remember(session.session, answered(session, use, Date.now(), ask.at, Boolean(ask.async)))} />`}
        ${error && html`<p class="hint">${error}</p>`}
    </article>`;
}

export function Inbox({ groups, sessions, exec, onChat, active = true }) {
    return html`<div class="sessioninbox" aria-label="Inbox">${groups.map((group) => html`
        <section class="inboxgroup" key=${group.name} aria-label=${group.name}>
            <h3 class="grouphead pjsection">${group.name}<span>${group.items.length}</span></h3>
            ${group.items.map(({ session, item }) => html`<${InboxRow}
                key=${`${session.sessionId}|${item.kind}|${item.id || item.at || ""}`}
                session=${session} item=${item} sessions=${sessions} exec=${exec} onChat=${onChat} active=${active} />`)}
        </section>`)}</div>`;
}
