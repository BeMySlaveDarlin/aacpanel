import { useState } from "preact/hooks";

import { html } from "../../html.js";
import { useAction } from "../../actions/gate.js";
import { knows, whyNot } from "../../exec.js";
import { shownName } from "../../agent.js";
import { openedBy } from "../sessions/kin.js";
import { contourOfLive } from "../sessions/map.js";
import { PickRow } from "./picker.js";

export const PARENT_LABEL = "Stand under…";
export const PARENT_NOTE = "choose a mapped live parent in this contour; the conversation goes on as it is";

export function parentChoices(live, snapshot) {
    if (!live || live.outside || !live.project || !live.project.id) return [];
    const sessions = (snapshot && snapshot.sessions) || [];
    const pageOf = contourOfLive(snapshot?.profileMap || [], sessions);
    const page = pageOf(live);
    const byName = new Map(sessions.map((s) => [s.session, s]));
    const below = (s) => {
        const seen = new Set();
        for (let at = s; at && !seen.has(at.session); at = byName.get(at.parent && at.parent.session)) {
            if (at.session === live.session) return true;
            seen.add(at.session);
        }
        return false;
    };
    // A parent in another contour cannot gather the child under its row.
    // Ancestry still crosses projects and contours to exclude descendants.
    return sessions.filter((s) => s.project?.id && pageOf(s) === page)
        .filter((s) => s.session !== openedBy(live) && !below(s));
}

export function parentWhy(exec, live, snapshot) {
    if (!live || live.outside) return "the panel did not start this session and cannot move it under a parent";
    if (!knows(exec, "session.parent")) return whyNot(exec, "session.parent");
    if (!live.project || !live.project.id) return "the session belongs to no project of the map";
    return parentChoices(live, snapshot).length ? "" : "no other mapped live session in this contour can be its parent";
}

export function ParentSheet({ name, live, snapshot, exec, onDone }) {
    const run = useAction();
    const [busy, setBusy] = useState(false);
    const why = parentWhy(exec, live, snapshot);
    const pick = async (parent) => {
        if (busy || why) return;
        setBusy(true);
        const result = await run("session.parent", name, { parent });
        setBusy(false);
        if (result && result.ok) onDone();
    };
    return html`
        <div class="cmdsheet">
            <div class="shead pkhead"><span class="stitle">Stand ${name} under a parent</span></div>
            <p class="cmdnote">Choose a live session from any mapped project in this contour. The conversation goes on as it is;
                Detach from parent makes this session stand on its own again.</p>
            ${why && html`<p class="cmdnote">${why}</p>`}
            <div class="pklist">
                ${parentChoices(live, snapshot).map((s) => html`
                    <${PickRow} key=${s.session}
                                name=${html`${s.project.id !== live.project.id && html`<span class="scproject">${s.project.name} · </span>`}${shownName(s)}`}
                                desc=${s.role || s.session}
                                disabled=${busy || Boolean(why)} onPick=${() => pick(s.session)} />
                `)}
            </div>
        </div>
    `;
}
