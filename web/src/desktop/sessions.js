// The left column of the sessions section: the contour picker, the live
// sessions of every contour shown under a heading with its limit, and the
// closed conversations of all of them on a shelf of their own below.
//
// Live and closed stand apart. A contour holds its live sessions only, one
// row each, in an order that does not follow their state: the keys 1–9 name
// places, and a place that moved whenever a session started or stopped
// working could not be learned. What waits for the person is said by its row
// and counted in the heading of its contour.
import { useCallback, useEffect, useMemo, useState } from "preact/hooks";

import { html } from "../html.js";
import { Icon } from "../ui/icons.js";
import { fill, pct, plural, tokens } from "../format.js";
import { knows, whyNot } from "../exec.js";
import { useAction } from "../actions/gate.js";
import { settled } from "../catchup.js";
import { agoText, contourOf, staleLimits } from "../screens/sessions/limits.js";
import { contourName } from "../contour.js";
import { pageNames } from "../screens/sessions/pages.js";
import { contoursOf } from "../screens/sessions/map.js";
import { kinLabel, kinOf, outsideNote } from "../screens/sessions/kin.js";
import { aboutOf, stateOf } from "../screens/sessions/blocks.js";
import { stamp, when } from "../screens/sessions/card.js";
import { modelTitle } from "../screens/chat/head.js";
import { effortName, modeLoud, modeName, usualMode } from "../screens/chat/picker.js";
import { homeProject, homeSession } from "../ui/home.js";
import { useSessionsArchive } from "../history.js";
import { Popover } from "../ui/popover.js";
import { ago as age } from "./panels/util.js";

// How many closed conversations the shelf shows, and how many it asks the
// archive for to find them: the shelf keeps the latest conversation of a
// project and drops the rest, so it asks for more than it shows.
const SHELF = 6;

const SHELF_ASK = 30;

// The dot of a row by the tone of its state.
const DOT = { wait: "dkwaiting", busy: "dkbusy", idle: "dkidle", off: "dkoff" };

// ContourPick chooses which contours the column shows.
export function ContourPick({ names, picks, onToggle, onAll }) {
    const [open, setOpen] = useState(false);
    const label = picks.length === 0 || picks.length === names.length
        ? `all (${names.length})`
        : picks.join(" · ");
    return html`
        <div class="dkpick">
            <button class="dkpickbtn" type="button" onClick=${() => setOpen((v) => !v)}>
                <span class="dkpicklabel">contours</span>
                <span class="dkpickvalue">${label}</span>
                <span class=${`dkpickchev${open ? " open" : ""}`}><${Icon.chevron} /></span>
            </button>
            ${open && html`
                <div class="dkpicklist">
                    <button class="dkpickrow" type="button" onClick=${() => { onAll(); setOpen(false); }}>all contours</button>
                    ${names.map((n) => html`
                        <button class="dkpickrow" key=${n} type="button" onClick=${() => onToggle(n)}>
                            <span class=${`dkcheck${picks.includes(n) ? " on" : ""}`}></span>${n}
                        </button>
                    `)}
                </div>
            `}
        </div>
    `;
}

function held(task) {
    return Math.max(0, Math.round((Date.now() - task.since) / 1000));
}

// raising lists the consoles being started that are not in the snapshot yet.
export function raising(sessions, opening, at) {
    const names = (sessions || []).map((s) => s.session);
    return (opening || []).filter((task) => !settled(task, names, at));
}

// orderOf lays the live sessions out in the order of the project map: the
// projects as the person arranged them, the sessions no project holds after
// them, and sessions of one place by name. The snapshot lists sessions by how
// full they are, which changes by the minute.
export function orderOf(sessions, map) {
    const rank = new Map();
    const byPath = new Map();
    for (const entry of map || []) {
        for (const group of entry.groups || []) {
            for (const project of group.projects || []) {
                if (!rank.has(project.id)) rank.set(project.id, rank.size);
                if (project.path && !byPath.has(project.path)) byPath.set(project.path, rank.get(project.id));
            }
        }
    }
    const place = (s) => {
        if (s.project && rank.has(s.project.id)) return rank.get(s.project.id);
        if (s.cwd && byPath.has(s.cwd)) return byPath.get(s.cwd);
        return Number.MAX_SAFE_INTEGER;
    };
    return [...(sessions || [])].sort((a, b) => place(a) - place(b)
        || String(a.session).localeCompare(String(b.session)));
}

// factsOf is the quiet line under the state of a live session, in the words
// the panel says them with elsewhere, most telling first: a column too narrow
// for all of it drops them from the end, whole.
export function factsOf(s, group, usual = "default") {
    const out = [];
    if (group && group.toLowerCase() !== String(s.session || "").toLowerCase()) out.push(group);
    const model = [s.model ? modelTitle(s.model, { withWindow: false }) : "", s.effort ? effortName(s.effort) : ""]
        .filter(Boolean).join(" · ");
    if (model) out.push(model);
    // The usual mode goes without saying; one that stops the session asking
    // at all is said even where it is usual.
    if (s.mode && (s.mode !== usual || modeLoud(s.mode))) out.push(modeName(s.mode));
    if (s.tokens > 0) {
        const of = s.limit ? ` of ${s.limitKnown === false ? "~" : ""}${tokens(s.limit)}` : "";
        out.push(`${tokens(s.tokens)}${of}`);
    }
    const up = age(s.startedAt);
    if (up) out.push(`up ${up}`);
    if (s.compacts > 0) out.push(`${s.compacts} ${plural(s.compacts, "compaction", "compactions")}`);
    return out;
}

// GhostLine holds the place of a console being raised, in the contour it is
// raised in.
function GhostLine({ task }) {
    return html`
        <div class="dksess dklive dkghost" role="status">
            <span class="dkkey"></span>
            <span class="dksessmain">
                <span class="dkname">${task.target}</span>
            </span>
            <span class="dksay">
                <span class="spin"></span>
                <span class="dksaytext">starting on the host</span>
                <span class="dksaysince">· ${held(task)} s</span>
            </span>
        </div>
    `;
}

// SessionLine is one live session: its key, name and the marks of what is
// unusual about it, its state in the words of the phone, the quiet line of
// what it runs on; and on the right, in a column of their own, how full it is
// and under it the one thing done to it besides opening, on every row.
function SessionLine({ s, group, usual, current, onPick, index, exec, wait, kid = false }) {
    const run = useAction();
    const closing = wait ? wait.of("close", s.session) : null;
    const restarting = wait ? wait.of("restart", s.session) : null;
    const busy = closing || restarting;
    const known = knows(exec, "session.close");
    const can = known && !closing;
    const restartKnown = knows(exec, "session.restart");
    const canRestart = restartKnown && !restarting;
    const state = stateOf(s);
    const tone = busy ? "off" : state.tone;
    const said = closing ? "closing" : restarting ? "restarting" : state.text;
    const since = busy ? `${held(busy)} s` : state.since;
    const facts = factsOf(s, group, usual);
    const full = s.pct || 0;
    const level = full >= 90 ? "crit" : full >= 70 ? "warn" : "";

    return html`
        <button
            class=${`dksess dklive${current === s.session ? " on" : ""}${kid ? " dkkid" : ""}`}
            type="button"
            data-tone=${tone}
            onClick=${() => onPick({ name: s.session, id: s.sessionId })}
        >
            <span class="dkkey">${index >= 0 && index < 9 ? index + 1 : ""}</span>
            <span class="dksessmain">
                <span class="dkname">${s.session}</span>
                ${s.home && html`<span class="dkhome">home</span>`}
                ${s.outside && !kid && html`
                    <span class="dkmark" data-tip=${outsideNote(s)}>
                        <${Icon.exit} />outside
                    </span>
                `}
                ${!s.outside && s.transport !== "stream" && html`
                    <span class="dkmark" data-tip="Lives in a console of tmux rather than on the feed">
                        <${Icon.terminal} />console
                    </span>
                `}
                ${s.remote && html`<span class="dkrc" data-tip="Remote Control is on: the session is open on claude.ai too">RC</span>`}
            </span>
            <span class="dknum" data-level=${level}>${s.limitKnown === false || s.noRequests ? "—" : pct(full)}</span>
            <span class="dksay">
                <span class=${`dkdot ${DOT[tone]}`}></span>
                <span class="dksaytext">${said}</span>
                ${since && html`<span class="dksaysince">· ${since}</span>`}
            </span>
            <span class="dkrowacts" onClick=${(e) => e.stopPropagation()}>
                ${!s.home && !s.outside && html`
                    <i
                        class=${`dkact danger${can ? "" : " off"}`}
                        aria-label=${`close session ${s.session}`}
                        data-tip=${closing
                            ? "The session is already closing"
                            : known ? undefined : whyNot(exec, "session.close")}
                        data-tipside="left"
                        onClick=${async () => {
                            if (!can) return;
                            await run("session.close", s.session, {});
                        }}
                    ><${Icon.close} /></i>
                `}
                ${s.home && html`
                    <i
                        class=${`dkact danger${canRestart ? "" : " off"}`}
                        aria-label=${`restart session ${s.session}`}
                        data-tip=${restarting
                            ? "The session is already restarting"
                            : restartKnown ? undefined : whyNot(exec, "session.restart")}
                        data-tipside="left"
                        onClick=${async () => {
                            if (!canRestart) return;
                            await run("session.restart", s.session, {});
                        }}
                    ><${Icon.refresh} /></i>
                `}
            </span>
            ${facts.length > 0 && html`
                <span class="dkdetail">${facts.map((fact, i) => html`<span key=${i}>${fact}</span>`)}</span>
            `}
            ${!s.noRequests && html`
                <span class=${`dksessbar ${fill(full)}`}><i style=${`width:${Math.min(100, full)}%`}></i></span>
            `}
        </button>
    `;
}

// KinFold holds the runs a session started inside its work under it. A run
// that waits for the person stands outside the fold, where it is seen; the
// fold keeps the rest.
function KinFold({ kids, line }) {
    const [open, setOpen] = useState(false);
    const waiting = kids.filter((s) => stateOf(s).tone === "wait");
    const rest = kids.filter((s) => stateOf(s).tone !== "wait");
    return html`
        ${waiting.map((s) => line(s, true))}
        ${rest.length > 0 && html`
            <button class="dkkin" type="button" aria-expanded=${open ? "true" : "false"} onClick=${() => setOpen(!open)}>
                <span class=${`dkkinchev${open ? " open" : ""}`}><${Icon.chevron} /></span>
                <span>${waiting.length > 0 ? `${rest.length} more ${plural(rest.length, "run", "runs")}` : kinLabel(rest.length)}</span>
            </button>
            ${open && rest.map((s) => line(s, true))}
        `}
    `;
}

// resetIn says when a window of a limit starts over, or nothing when the
// snapshot does not say.
function resetIn(part) {
    const at = part && part.resetsAt;
    if (!at) return "";
    const left = at * 1000 - Date.now();
    if (left <= 0) return "any moment";
    const hours = Math.floor(left / 3600000);
    if (hours >= 48) return `${Math.round(hours / 24)} d`;
    if (hours >= 1) return `${hours} h`;
    return `${Math.max(1, Math.round(left / 60000))} min`;
}

// Ring is one window of a contour's limit: a circle filled by the share spent,
// the share inside it and the window beside it.
function Ring({ label, part }) {
    const value = Math.round((part && part.pct) || 0);
    const level = value >= 90 ? "dkcrit" : value >= 70 ? "dkwarn" : "";
    return html`
        <span class=${`dkring ${level}`.trim()}>
            <span class="dkringdial" style=${`--share:${Math.min(100, value)}`}><b>${value}</b></span>
            <span class="dkringlabel">${label}</span>
        </span>
    `;
}

// Window is one window of a limit in the details: the share, a bar and when
// it starts over.
function Window({ title, part }) {
    const value = Math.round((part && part.pct) || 0);
    const level = value >= 90 ? "dkcrit" : value >= 70 ? "dkwarn" : "";
    const left = resetIn(part);
    return html`
        <div class=${`dklimwin ${level}`.trim()}>
            <span class="dklimwintitle">${title}</span>
            <span class="dklimwinpct">${value}%</span>
            <span class="dklimwinbar"><i style=${`width:${Math.min(100, value)}%`}></i></span>
            <span class="dklimwinreset">${left ? `resets in ${left}` : "when it resets, the snapshot does not say"}</span>
        </div>
    `;
}

// ContourLimits is the subscription limit of a contour in its heading: two
// rings, and the details of both windows under them on a press. Numbers the
// snapshot has not renewed for a while are dimmed and say how old they are.
export function ContourLimits({ limits, name, profiles }) {
    const [open, setOpen] = useState(false);
    const close = useCallback(() => setOpen(false), []);
    const id = ((profiles || []).find((p) => p.profile === name) || {}).id || 0;
    const c = contourOf(limits, name, id);
    if (!c) return html`<span class="dkrings dknone">no numbers yet</span>`;
    const old = staleLimits(c)
        ? `The numbers are from ${agoText(c.ageSec)}: they are renewed when a session of the contour answers`
        : "";
    return html`
        <span class="dkringsctl">
            <button class=${`dkrings${old ? " dkold" : ""}`} type="button" aria-expanded=${open ? "true" : "false"}
                    aria-label=${`the limits of contour ${name}`} onClick=${() => setOpen(!open)}>
                ${old && html`<span class="dkringsage">${agoText(c.ageSec)}</span>`}
                <${Ring} label="5h" part=${c.fiveHour} />
                <${Ring} label="7d" part=${c.sevenDay} />
            </button>
            <${Popover} open=${open} onClose=${close} label=${`the limits of contour ${name}`}>
                <div class="dklimpop">
                    <div class="dklimpophead">${name}</div>
                    <${Window} title="Five hours" part=${c.fiveHour} />
                    <${Window} title="Seven days" part=${c.sevenDay} />
                    ${old && html`<p class="dklimpopnote">${old}</p>`}
                </div>
            <//>
        </span>
    `;
}

// NewSession starts a session in a project of the contour: the plus of its
// heading drops the projects of its map, grouped as the map groups them, and
// a press starts the session the way the projects panel does. The home
// project is left out while its session lives: the host has one.
function NewSession({ name, entry, snapshot, exec }) {
    const run = useAction();
    const [open, setOpen] = useState(false);
    const close = useCallback(() => setOpen(false), []);
    const home = homeSession(snapshot) ? homeProject(snapshot) : null;
    const groups = ((entry && entry.groups) || [])
        .map((g) => ({ name: g.name, projects: (g.projects || []).filter((p) => !home || p.id !== home.id) }))
        .filter((g) => g.projects.length > 0);
    if (groups.length === 0) return null;
    const ready = knows(exec, "session.open");
    const live = new Set(((snapshot && snapshot.sessions) || []).map((s) => s.project && s.project.id).filter(Boolean));
    return html`
        <span class="dknewctl">
            <button class=${`dknew${open ? " on" : ""}`} type="button" aria-expanded=${open ? "true" : "false"}
                    aria-label=${`a new session in contour ${name}`} onClick=${() => setOpen(!open)}><${Icon.plus} /></button>
            <${Popover} open=${open} onClose=${close} label=${`a new session in contour ${name}`}>
                <div class="dknewpop">
                    <div class="dklimpophead">A new session in ${name}</div>
                    ${!ready && html`<p class="dklimpopnote">${whyNot(exec, "session.open")}</p>`}
                    ${groups.map((g) => html`
                        <div class="dknewgroup" key=${`g:${g.name}`}>${g.name}</div>
                        ${g.projects.map((p) => html`
                            <button class="dknewrow" type="button" key=${p.id} disabled=${!ready}
                                    onClick=${async () => {
                                        close();
                                        await run("session.open", p.session || p.name, { project: p.id });
                                    }}>
                                <span class="dkname">${p.name}</span>
                                ${live.has(p.id) && html`<span class="dknewlive">live</span>`}
                            </button>
                        `)}
                    `)}
                </div>
            <//>
        </span>
    `;
}

// ContourSection is one contour in the column: its heading with how many of
// its sessions wait for the person, the way to a new one and the limit; its
// live sessions with the runs they started under them, and the consoles being
// raised in it.
function ContourSection({ name, entry, list, ghosts, limits, map, place, snapshot, current, onPick, keyOf, exec, wait }) {
    const { own, kids } = kinOf(list);
    const waits = list.filter((s) => stateOf(s).tone === "wait").length;
    const line = (s, kid = false) => {
        const found = s.project === undefined ? place(s.cwd) : s.project;
        return html`<${SessionLine}
            key=${s.session}
            s=${s}
            group=${found ? found.group : ""}
            usual=${usualMode(snapshot && snapshot.profiles, s.profile)}
            current=${current}
            onPick=${onPick}
            index=${kid ? 99 : keyOf(s)}
            exec=${exec}
            wait=${wait}
            kid=${kid}
        />`;
    };
    return html`
        <section>
            <div class="dkcontour dklimhead">
                <span class="dkcontourname">${name}</span>
                ${waits > 0 && html`
                    <span class="dkwaitn" data-tip=${`${waits} ${plural(waits, "session waits", "sessions wait")} for you`}>
                        <i></i>${waits}
                    </span>
                `}
                <${NewSession} name=${name} entry=${entry} snapshot=${snapshot} exec=${exec} />
                <${ContourLimits} limits=${limits} name=${name} profiles=${map} />
            </div>
            ${own.map((s) => html`
                ${line(s)}
                ${kids.has(s.session) && html`<${KinFold} key=${`kin:${s.session}`} kids=${kids.get(s.session)} line=${line} />`}
            `)}
            ${ghosts.map((task) => html`<${GhostLine} key=${`+${task.target}`} task=${task} />`)}
            ${own.length === 0 && ghosts.length === 0 && html`<p class="dkempty">nothing live</p>`}
        </section>
    `;
}

// shelfOf picks what the shelf shows out of the latest closed conversations:
// the ones where something was said, one per project — a session at work
// leaves a string of short runs in its project behind it, reviews and agents
// of its own, and those would push every other project off the shelf.
export function shelfOf(rows, max = SHELF) {
    const seen = new Set();
    const out = [];
    for (const row of rows || []) {
        if (!((row.messages || 0) > 0)) continue;
        const key = row.project && row.project.id ? `p${row.project.id}` : `d:${row.cwd || row.name}`;
        if (seen.has(key)) continue;
        seen.add(key);
        out.push(row);
        if (out.length >= max) break;
    }
    return out;
}

// ClosedLine is a closed conversation on the shelf: its name, its contour when
// the column shows more than one and when it was, over what it was about and
// Resume; what it was about gives way, the time and Resume stay.
function ClosedLine({ row, contour, project, on, onPick, exec }) {
    const run = useAction();
    const about = aboutOf(row, project);
    const ready = knows(exec, "session.resume");
    const open = () => onPick({ name: row.name, id: row.sessionId, archived: true, row });
    return html`
        <div class=${`dkclosed${on ? " on" : ""}`} role="button" tabindex="0"
             onClick=${open}
             onKeyDown=${(e) => { if (e.key === "Enter") open(); }}>
            <span class="dkcltop">
                <span class="dkclname">${row.name}</span>
                ${contour && html`<span class="dkclcontour">${contour}</span>`}
                <span class="dkclwhen">${when(stamp(row.lastAt))}</span>
            </span>
            <span class="dkclsub">
                <span class="dkabout">${about
                    ? `«${about}»`
                    : `${row.messages} ${plural(row.messages, "message", "messages")}`}</span>
                ${row.sessionId && html`
                    <button class=${`dkresume${ready ? "" : " off"}`} type="button"
                            aria-label=${`resume conversation of ${row.name}`}
                            data-tip=${ready ? undefined : whyNot(exec, "session.resume")}
                            data-tipside="left"
                            onClick=${async (e) => {
                                e.stopPropagation();
                                if (!ready) return;
                                await run("session.resume", row.name, { session: row.sessionId });
                            }}>Resume</button>
                `}
            </span>
        </div>
    `;
}

// Shelf is the closed conversations of the contours shown, newest first, under
// a heading of its own below all of them; the archive panel holds the rest.
function Shelf({ ids, others, skip, labelOf, projectOf, currentId, onPick, onArchive, exec }) {
    const past = useSessionsArchive({ limit: SHELF_ASK, contour: ids, profile: others, skip });
    if (past.kind === "loading") return null;
    const rows = past.kind === "ready" ? shelfOf((past.archive && past.archive.rows) || []) : [];
    if (past.kind === "ready" && rows.length === 0) return null;
    return html`
        <section class="dkshelf">
            <div class="dkshelfhead">
                <span class="dkshelftitle">closed</span>
                ${onArchive && html`
                    <button class="dkshelfall" type="button" onClick=${onArchive}>the archive <${Icon.chevron} /></button>
                `}
            </div>
            ${past.kind !== "ready" && html`<p class="dkempty">${past.error}</p>`}
            ${rows.map((row) => html`
                <${ClosedLine} key=${row.sessionId || row.name} row=${row} contour=${labelOf(row)}
                               project=${projectOf(row)} on=${Boolean(currentId) && currentId === row.sessionId}
                               onPick=${onPick} exec=${exec} />
            `)}
        </section>
    `;
}

function groupOf(profiles) {
    const map = new Map();
    for (const p of (profiles && profiles.profiles) || []) {
        for (const g of p.groups || []) {
            for (const pr of g.projects || []) {
                if (pr.path) map.set(pr.path, { contour: p.name || p.profile, group: g.name, project: pr.name });
            }
        }
    }
    return (cwd) => {
        if (!cwd) return null;
        if (map.has(cwd)) return map.get(cwd);
        let best = null;
        let bestLen = 0;
        for (const [path, found] of map) {
            if (cwd.startsWith(`${path}/`) && path.length > bestLen) {
                best = found;
                bestLen = path.length;
            }
        }
        return best;
    };
}

// sectionOfTask finds the contour a console is being raised in: by the project
// it was opened from, then by its name among the projects of the map; one the
// map does not know stands in the first contour shown.
function sectionOfTask(task, map, sections) {
    const project = task.params && task.params.project;
    for (const entry of map) {
        for (const group of entry.groups || []) {
            for (const p of group.projects || []) {
                if ((project && p.id === project) || (!project && p.session === task.target)) return entry.profile;
            }
        }
    }
    return sections[0] || "";
}

export function SessionColumn({ snapshot, profiles, limits, current, currentId, onPick, picks, setPicks, onNames, onOrder, onArchive, exec, wait }) {
    const all = (snapshot && snapshot.sessions) || [];

    const map = (snapshot && snapshot.profileMap) || [];
    const names = useMemo(() => {
        const known = pageNames(map, limits);
        return known.length ? known : [...new Set(all.map((s) => contourName(s.profile)))];
    }, [map, limits, all]);

    const where = useMemo(() => contoursOf(map, all), [map, all]);
    const pageOf = (s) => where.get(s.session) || contourName(s.profile);
    useEffect(() => { if (onNames) onNames(names); }, [names.join("\n")]);

    const place = useMemo(() => groupOf(profiles), [profiles]);
    const ordered = useMemo(() => orderOf(all, map), [all, map]);
    const shown = picks.length ? ordered.filter((s) => picks.includes(pageOf(s))) : ordered;

    const ghosts = raising(all, wait ? wait.opening() : [], (snapshot && snapshot.at) || 0);

    const byProfile = useMemo(() => {
        const map = new Map();
        for (const s of shown) {
            const key = pageOf(s);
            if (!map.has(key)) map.set(key, []);
            map.get(key).push(s);
        }
        return map;
    }, [shown]);

    // Every contour shown has its section, with live sessions or without:
    // those of the map in its order, then any a session names that the map
    // does not.
    const sections = useMemo(() => {
        const wanted = picks.length ? names.filter((n) => picks.includes(n)) : names;
        return [...wanted, ...[...byProfile.keys()].filter((n) => !wanted.includes(n))];
    }, [names.join("\n"), picks.join("\n"), byProfile]);

    const flat = useMemo(() => sections.flatMap((n) => kinOf(byProfile.get(n) || []).own), [sections, byProfile]);
    const keyOf = (s) => flat.indexOf(s);

    useEffect(() => {
        if (onOrder) onOrder(flat.map((s) => ({ name: s.session, id: s.sessionId })));
    }, [flat.map((s) => s.session).join("\n")]);

    const ghostsIn = new Map();
    for (const task of ghosts) {
        const name = sectionOfTask(task, map, sections);
        if (!ghostsIn.has(name)) ghostsIn.set(name, []);
        ghostsIn.get(name).push(task);
    }

    // The shelf asks for every contour shown at once: those of the map by the
    // id of their entry, the rest by the name the collector knows them by.
    const entryOf = (name) => map.find((p) => p.profile === name) || null;
    const ids = sections.map((n) => (entryOf(n) || {}).id).filter(Boolean);
    const others = sections.filter((n) => !(entryOf(n) || {}).id);
    const skip = all.map((s) => s.sessionId).filter(Boolean);
    const labels = useMemo(() => {
        const byProject = new Map();
        const byId = new Map(map.map((p) => [p.id, p.profile]));
        for (const entry of map) {
            for (const group of entry.groups || []) {
                for (const p of group.projects || []) byProject.set(p.id, { label: entry.profile, project: p });
            }
        }
        const byCollector = new Map(((limits && limits.contours) || [])
            .map((c) => [c.profile, byId.get(c.contour) || c.profile]));
        return { byProject, byCollector };
    }, [map, limits]);
    const labelOf = (row) => {
        if (sections.length < 2) return "";
        const known = row.project && labels.byProject.get(row.project.id);
        return (known && known.label) || labels.byCollector.get(row.profile) || row.profile || "";
    };
    const projectOf = (row) => {
        const known = row.project && labels.byProject.get(row.project.id);
        return known ? known.project : null;
    };

    return html`
        <aside class="dkleft">
            <${ContourPick}
                names=${names}
                picks=${picks}
                onToggle=${(name) => setPicks((prev) => (prev.includes(name) ? prev.filter((x) => x !== name) : [...prev, name]))}
                onAll=${() => setPicks([])}
            />
            <div class="dkscroll">
                ${sections.map((name) => html`
                    <${ContourSection} key=${name} name=${name} entry=${entryOf(name)}
                                       list=${byProfile.get(name) || []} ghosts=${ghostsIn.get(name) || []}
                                       limits=${limits} map=${map} place=${place} snapshot=${snapshot}
                                       current=${current} onPick=${onPick} keyOf=${keyOf}
                                       exec=${exec} wait=${wait} />
                `)}
                ${sections.length === 0 && ghosts.map((task) => html`<${GhostLine} key=${`+${task.target}`} task=${task} />`)}
                ${sections.length === 0 && ghosts.length === 0 && html`<p class="dkempty">there are no live sessions</p>`}
                ${sections.length > 0 && html`
                    <${Shelf} ids=${ids} others=${others} skip=${skip} labelOf=${labelOf} projectOf=${projectOf}
                              currentId=${currentId} onPick=${onPick} onArchive=${onArchive} exec=${exec} />
                `}
            </div>
        </aside>
    `;
}
