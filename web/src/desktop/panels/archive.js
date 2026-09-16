// The conversation archive in the right-hand panel of the Sessions section.
import { useEffect, useState } from "preact/hooks";

import { html } from "../../html.js";
import { Icon } from "../../ui/icons.js";
import { ContourPick } from "../sessions.js";
import { pct, plural } from "../../format.js";
import { knows, whyNot } from "../../exec.js";
import { useAction } from "../../actions/gate.js";
import { ago, modelShort, useJSON } from "./util.js";

// How many conversations a contour shows at once. Contours are paged apart, so
// the number is what fits beside its neighbours rather than what fits in the
// panel; with a single contour on screen the whole panel is its.
const PER_CONTOUR = 5;

const PER_ALONE = 24;

function projectOf(row) {
    if (row && row.project && row.project.name) return row.project.name;
    const cwd = row && row.cwd;
    if (!cwd) return "";
    const parts = cwd.split("/").filter(Boolean);
    return parts.length ? parts[parts.length - 1] : "";
}

// contourQuery asks for one contour the way the archive understands it.
//
// A pick is a label of the map, and the map is free to call a contour anything: the
// collector knows it by the directory it lives in, so the label goes out as the id of
// its map entry. A contour the map does not know keeps its own name — that name came
// from the collector in the first place.
function contourQuery(name, profiles) {
    const byName = new Map(((profiles && profiles.profiles) || []).map((p) => [p.name || p.profile, p.id]));
    if (!name) return "";
    return byName.has(name)
        ? `&contour=${encodeURIComponent(byName.get(name))}`
        : `&profile=${encodeURIComponent(name)}`;
}

function Row({ r, exec, onOpen, run }) {
    return html`
        <div class="dkarch" onClick=${() => onOpen({ name: r.name, id: r.sessionId, archived: true, row: r })}>
            <span class="dkarchtop">
                <span class="dkname" title=${r.cwd}>${r.name}</span>
                <span class="dknum">${pct(r.pctMax)}</span>
                <span class="dkwhen">${ago(r.lastAt)}</span>
            </span>
            <span class="dkarchsub">
                <span class="dkgroup">${r.project ? r.project.group : "off the map"}</span>
                <span class="dklast">${r.project ? r.project.name : r.cwd}</span>
                <span class="dkarchmodel">${modelShort(r.model)}</span>
            </span>
            <span class="dkacts" onClick=${(e) => e.stopPropagation()}>
                ${r.sessionId && html`
                    <i
                        class=${`dkact${knows(exec, "session.resume") ? "" : " off"}`}
                        data-tip=${knows(exec, "session.resume") ? undefined : whyNot(exec, "session.resume")}
                        data-tipside="left"
                        onClick=${() => knows(exec, "session.resume") && run("session.resume", r.name, { session: r.sessionId })}
                    ><${Icon.resume} /></i>
                `}
                ${(r.project || projectOf(r)) && html`
                    <i
                        class=${`dkact${knows(exec, "session.open") ? "" : " off"}`}
                        data-tip=${knows(exec, "session.open") ? undefined : whyNot(exec, "session.open")}
                        data-tipside="left"
                        onClick=${() => knows(exec, "session.open") && run("session.open",
                            (r.project && r.project.name) || projectOf(r),
                            r.project ? { project: r.project.id } : {})}
                    ><${Icon.plus} /></i>
                `}
            </span>
        </div>
    `;
}

// Contour is one contour's slice of the archive, paged on its own.
//
// One list over all of them cannot show them: a page comes back sorted by the
// time of the last message, so the contour being worked in fills the first
// pages and a quieter one starts somewhere on page four — asked for together,
// the quiet contours are simply not on the screen. Asked apart, every contour
// shows its own latest conversations whatever the neighbours have been doing.
function Contour({ name, profiles, per, exec, onOpen, run }) {
    const [page, setPage] = useState(0);
    const query = contourQuery(name, profiles);
    useEffect(() => setPage(0), [query, per]);
    const { data, error } = useJSON(`/api/sessions/archive?limit=${per}&offset=${page * per}${query}`);
    const rows = (data && data.rows) || [];
    const total = (data && data.total) || 0;
    const offset = (data && data.offset) || 0;

    return html`
        <section>
            <div class="dkcontour">
                <span class="dkcontourname">${name || "the archive"}</span>
                <span class="dkcontournum">
                    ${error ? "unavailable" : `${total} ${plural(total, "conversation", "conversations")}`}
                </span>
            </div>
            ${error && html`<p class="dkempty">the archive is unavailable: ${error}</p>`}
            ${!error && data && rows.length === 0 && html`<p class="dkempty">there are no conversations</p>`}
            ${rows.map((r) => html`<${Row} key=${r.sessionId} r=${r} exec=${exec} onOpen=${onOpen} run=${run} />`)}
            ${total > per && html`
                <div class="dkpager">
                    <button
                        class="dkpagebtn"
                        type="button"
                        disabled=${page === 0}
                        onClick=${() => setPage((p) => Math.max(0, p - 1))}
                    ><${Icon.chevron} /></button>
                    <span class="dkpagenum">
                        ${rows.length ? offset + 1 : 0}–${offset + rows.length}
                        <span class="dkpagetotal">of ${total}</span>
                    </span>
                    <button
                        class="dkpagebtn next"
                        type="button"
                        disabled=${offset + rows.length >= total}
                        onClick=${() => setPage((p) => p + 1)}
                    ><${Icon.chevron} /></button>
                </div>
            `}
        </section>
    `;
}

// Archive lists the conversations of the shown contours, every contour apart.
export function Archive({ profiles, names, picks, setPicks, onOpen, exec }) {
    const run = useAction();
    const known = names || [];
    const shown = picks.length ? known.filter((n) => picks.includes(n)) : known;
    const per = shown.length > 1 ? PER_CONTOUR : PER_ALONE;

    return html`
        <div class="dkarchive">
            ${known.length > 1 && html`
                <${ContourPick}
                    names=${known}
                    picks=${picks}
                    onToggle=${(name) => setPicks((prev) => (prev.includes(name) ? prev.filter((x) => x !== name) : [...prev, name]))}
                    onAll=${() => setPicks([])}
                />
            `}
            <div class="dkscroll">
                ${shown.map((name) => html`
                    <${Contour}
                        key=${name}
                        name=${name}
                        profiles=${profiles}
                        per=${per}
                        exec=${exec}
                        onOpen=${onOpen}
                        run=${run}
                    />
                `)}
                ${shown.length === 0 && html`
                    <${Contour} name="" profiles=${profiles} per=${PER_ALONE} exec=${exec} onOpen=${onOpen} run=${run} />
                `}
            </div>
        </div>
    `;
}
