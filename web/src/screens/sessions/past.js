// The session archive: every project of the contour to start a session in,
// and under them the conversations of the contour by project with the fill
// chart over them. The projects come first: a page of the archive is long, and
// a new session should not wait at the bottom of it.

import { useEffect, useState } from "preact/hooks";

import { html } from "../../html.js";
import { BackHead, useBackClose } from "../../ui/back.js";
import { plural } from "../../format.js";
import { Icon } from "../../ui/icons.js";
import { area, Chart } from "../../chart.js";
import { bucketText, PERIODS, useSessionsArchive, useSessionsHistory } from "../../history.js";
import { Chips } from "../../ui/chips.js";
import { PastLine } from "./blocks.js";
import { Groups } from "./map.js";
import { useAction } from "../../actions/gate.js";
import { knows, whyNot } from "../../exec.js";

// A page is read in conversations and shown in projects: the more of them a
// page holds, the fewer projects are cut between two pages.
const PAGE = 60;

// How many conversations of a project stand under it on the page; the rest is
// on the project's own screen.
const PER_PROJECT = 3;

// byProject lays a page of the archive out by project, in the order their
// freshest conversation stands.
export function byProject(rows) {
    const out = new Map();
    for (const row of rows || []) {
        const key = row.project ? `p${row.project.id}` : `d:${row.cwd || row.name}`;
        if (!out.has(key)) {
            out.set(key, {
                key,
                name: (row.project && row.project.name) || row.name,
                group: (row.project && row.project.group) || (row.home ? "the home session" : ""),
                project: row.project || null,
                rows: [],
            });
        }
        out.get(key).rows.push(row);
    }
    return [...out.values()];
}

export function Past({ profile = "", contour = 0, map = null, sessions = [], onBack, onProject, exec, chat }) {
    useBackClose(true, onBack);

    const [period, setPeriod] = useState("7d");
    const [offset, setOffset] = useState(0);
    const state = useSessionsArchive({ limit: PAGE, offset, profile, contour });
    const archive = state.kind === "ready" ? state.archive : null;
    const rows = (archive && archive.rows) || [];
    // A conversation that is live is on the list of sessions, not in the archive.
    const live = new Set(sessions.map((s) => s.sessionId).filter(Boolean));
    const crowdState = useSessionsHistory(period, { limit: 1 });
    const history = crowdState.kind === "ready" ? crowdState.sessions : null;
    const peaks = peakSeries(history);
    const crowd = busiest(history);

    const choosePeriod = (id) => setPeriod(id);

    useEffect(() => setOffset(0), [profile]);

    return html`
        <${BackHead} onBack=${onBack} label="to sessions">
            <h2>Session archive</h2>
            <span class="where">${profile ? `${profile} · ` : ""}by project, the latest on top</span>
        <//>

        ${map && html`<${Groups} profile=${map} sessions=${sessions} onOpen=${onProject} exec=${exec} />`}

        <div class="grouphead">archive</div>
        <section class="card">
            <${Chips}
                items=${PERIODS.filter((p) => p.id === "24h" || p.id === "7d" || p.id === "30d")}
                current=${period}
                onSelect=${choosePeriod}
            />

            ${state.kind === "loading" && html`<p class="hint">Reading the transcripts…</p>`}
            ${state.kind === "unavailable" && html`<p class="hint warn">${state.error}</p>`}
            ${state.kind === "failed" && html`<p class="hint crit">${state.error}</p>`}
            ${archive && rows.length === 0 && html`<p class="hint">${profile
                ? `There are no conversations of contour ${profile} on disk.`
                : "There are no conversations on disk."}</p>`}

            ${peaks && html`
                <${Chart} data=${peaks} series=${PEAK} height=${78} yFormat=${(v) => `${Math.round(v)}%`} />
            `}
            ${history && peaks && html`
                <p class="hint">${bucketText(history.resolution, history.stepSec)} · the fill peak in a slice</p>
            `}
            ${crowd > 1 && html`
                <p class="sub">up to ${crowd} ${plural(crowd, "session", "sessions")} ran at once</p>
            `}

            ${byProject(rows.filter((row) => !live.has(row.sessionId))).map((block) => html`
                <${ArchiveBlock} key=${block.key} block=${block} map=${map} exec=${exec} onOpen=${chat} onProject=${onProject} />
            `)}

            ${archive && html`<${Pager}
                offset=${offset}
                shown=${rows.length}
                total=${archive.total}
                onGo=${setOffset}
            />`}
        </section>
    `;
}

// ArchiveBlock is one project of the archive: its latest conversations, a new
// session in it, and the way to all of them on its own screen.
function ArchiveBlock({ block, map, exec, onOpen, onProject }) {
    const run = useAction();
    const ready = knows(exec, "session.open");
    const own = mapProject(map, block.project);
    // The home session is restarted from the list, never opened a second time.
    const canNew = own && !block.rows.some((row) => row.home);
    const shown = block.rows.slice(0, PER_PROJECT);
    const rest = block.rows.length - shown.length;
    return html`
        <section class="pjblock pjquiet">
            <div class="pjhead">
                ${own && onProject
                    ? html`<button class="pjname" type="button" aria-label=${`project ${block.name}`}
                                   onClick=${() => onProject({ ...own.project, profile: map.profile, contour: map.id, group: own.group })}>${block.name}</button>`
                    : html`<span class="pjname">${block.name}</span>`}
                ${block.group && html`<span class="pjgroup">${block.group}</span>`}
                ${canNew && html`
                    <button class="pjnew" type="button" disabled=${!ready}
                            aria-label=${`new session in project ${block.name}`}
                            title=${ready ? "" : whyNot(exec, "session.open")}
                            onClick=${() => run("session.open", own.project.session, { project: own.project.id })}>
                        ${Icon.plus()}<span>New</span>
                    </button>
                `}
            </div>
            ${shown.map((row) => html`<${PastLine} key=${row.sessionId} row=${row} project=${own && own.project} exec=${exec} onOpen=${onOpen} />`)}
            ${rest > 0 && html`
                <button class="pjlink" type="button" disabled=${!(own && onProject)}
                        onClick=${() => own && onProject({ ...own.project, profile: map.profile, contour: map.id, group: own.group })}>
                    ${rest} more ${plural(rest, "conversation", "conversations")} on this page
                    <span class="chev">${Icon.chevron()}</span>
                </button>
            `}
        </section>
    `;
}

// mapProject finds the project of an archive row in the contour's map.
function mapProject(map, ref) {
    if (!map || !ref) return null;
    for (const group of map.groups || []) {
        const project = (group.projects || []).find((p) => p.id === ref.id);
        if (project) return { project, group: group.name };
    }
    return null;
}

function Pager({ offset, shown, total, onGo }) {
    if (total <= PAGE) return null;

    const from = total === 0 ? 0 : offset + 1;
    const to = offset + shown;
    return html`
        <div class="pager">
            <button
                class="btn"
                type="button"
                disabled=${offset <= 0}
                onClick=${() => onGo(Math.max(0, offset - PAGE))}
            ><span class="chev">${Icon.chevron()}</span> back</button>
            <span class="sub">${from}–${to} of ${total}</span>
            <button
                class="btn"
                type="button"
                disabled=${to >= total}
                onClick=${() => onGo(offset + PAGE)}
            >forward <span class="chev">${Icon.chevron()}</span></button>
        </div>
    `;
}

const PEAK = [{}, area("--accent", "#63a8ff")];

function peakSeries(history) {
    if (!history || !history.t || history.t.length === 0) return null;
    if (!history.pctMax.some((v) => v !== null && v !== undefined)) return null;
    return [history.t, history.pctMax.map((v) => (v === undefined ? null : v))];
}

function busiest(history) {
    if (!history || !history.live) return 0;
    return history.live.reduce((most, v) => (v != null && v > most ? v : most), 0);
}
