// The conversation archive in the right-hand panel of the Sessions section.
import { useEffect, useMemo, useState } from "preact/hooks";

import { html } from "../../html.js";
import { Icon } from "../../ui/icons.js";
import { pct, plural } from "../../format.js";
import { knows, whyNot } from "../../exec.js";
import { useAction } from "../../actions/gate.js";
import { PAGE, ago, modelShort, useJSON } from "./util.js";

function projectOf(row) {
    if (row && row.project && row.project.name) return row.project.name;
    const cwd = row && row.cwd;
    if (!cwd) return "";
    const parts = cwd.split("/").filter(Boolean);
    return parts.length ? parts[parts.length - 1] : "";
}

// contourQuery asks for the picked contours the way the archive understands them.
//
// A pick is a label of the map, and the map is free to call a contour anything: the
// collector knows it by the directory it lives in, so the label goes out as the id of
// its map entry. A contour the map does not know keeps its own name — that name came
// from the collector in the first place.
function contourQuery(picks, profiles) {
    const byName = new Map(((profiles && profiles.profiles) || []).map((p) => [p.name || p.profile, p.id]));
    return picks
        .map((name) => (byName.has(name)
            ? `&contour=${encodeURIComponent(byName.get(name))}`
            : `&profile=${encodeURIComponent(name)}`))
        .join("");
}

// freshest returns when the top conversation of a contour last spoke.
function freshest(list) {
    const at = list.length ? Date.parse(list[0].lastAt) : NaN;
    return Number.isNaN(at) ? 0 : at;
}

// Archive lists conversations by page, split by contour.
export function Archive({ profiles, picks, onOpen, exec }) {
    const run = useAction();
    const [page, setPage] = useState(0);
    const want = [...picks].sort();
    const query = contourQuery(want, profiles);
    useEffect(() => setPage(0), [want.join("\n")]);
    const { data, error } = useJSON(`/api/sessions/archive?limit=${PAGE}&offset=${page * PAGE}${query}`);
    const rows = (data && data.rows) || [];

    const byContour = useMemo(() => {
        const map = new Map();
        for (const r of rows) {
            const name = r.profile || "";
            if (!map.has(name)) map.set(name, []);
            map.get(name).push(r);
        }
        return [...map].sort((a, b) => freshest(b[1]) - freshest(a[1]));
    }, [rows]);

    if (error) return html`<p class="dkempty">the archive is unavailable: ${error}</p>`;

    return html`
        <div class="dkscroll">
            ${byContour.map(([name, list]) => html`
                <section key=${name || "—"}>
                    <div class="dkcontour">
                        <span class="dkcontourname">${name || "no name"}</span>
                        <span class="dkcontournum">${list.length} ${plural(list.length, "conversation", "conversations")}</span>
                    </div>
                    ${list.map((r) => html`
                        <div class="dkarch" key=${r.sessionId} onClick=${() => onOpen({ name: r.name, id: r.sessionId, archived: true, row: r })}>
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
                                        data-tip=${knows(exec, "session.resume") ? "Resume the conversation" : whyNot(exec, "session.resume")}
                                        data-tipside="left"
                                        onClick=${() => knows(exec, "session.resume") && run("session.resume", r.name, { session: r.sessionId })}
                                    ><${Icon.resume} /></i>
                                `}
                                ${projectOf(r) && html`
                                    <i
                                        class=${`dkact${knows(exec, "session.open") ? "" : " off"}`}
                                        data-tip=${knows(exec, "session.open") ? `New session in ${projectOf(r)}` : whyNot(exec, "session.open")}
                                        data-tipside="left"
                                        onClick=${() => knows(exec, "session.open") && run("session.open", projectOf(r), {})}
                                    ><${Icon.plus} /></i>
                                `}
                            </span>
                        </div>
                    `)}
                </section>
            `)}
            ${rows.length === 0 && html`<p class="dkempty">there are no conversations</p>`}
            <div class="dkpager">
                <button
                    class="dkpagebtn"
                    type="button"
                    disabled=${page === 0}
                    onClick=${() => setPage((p) => Math.max(0, p - 1))}
                ><${Icon.chevron} /></button>
                <span class="dkpagenum">
                    ${(data && data.offset) || 0 ? (data.offset + 1) : (rows.length ? 1 : 0)}–${((data && data.offset) || 0) + rows.length}
                    <span class="dkpagetotal">of ${(data && data.total) || 0}</span>
                </span>
                <button
                    class="dkpagebtn next"
                    type="button"
                    disabled=${((data && data.offset) || 0) + rows.length >= ((data && data.total) || 0)}
                    onClick=${() => setPage((p) => p + 1)}
                ><${Icon.chevron} /></button>
            </div>
        </div>
    `;
}
