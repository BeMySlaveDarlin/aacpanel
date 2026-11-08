// The left column of the sessions section: the contour picker, the live
// sessions of every contour shown under a heading with its limit, and the
// closed conversations of all of them on a shelf of their own below.
//
// Live and closed stand apart. A contour holds its live sessions only, in
// blocks of their projects as the phone has them — a heading over every
// project, a card a session, the card the phone draws — in an
// order that does not follow their state: a place that moved whenever a
// session started or stopped working could not be learned, and the eye and
// the arrow keys go down the column by those places. What waits for the
// person is said by its card and counted in the heading of its contour.
// Pinned groups keep their creation order before the rest of that contour.
import { useCallback, useEffect, useMemo, useState } from "preact/hooks";

import { html } from "../html.js";
import { Icon } from "../ui/icons.js";
import { knows } from "../exec.js";
import { useAction } from "../actions/gate.js";
import { settled } from "../catchup.js";
import { agoText, contourOf, staleLimits } from "../screens/sessions/limits.js";
import { contourName } from "../contour.js";
import { pageNames } from "../screens/sessions/pages.js";
import { contourOfLive } from "../screens/sessions/map.js";
import { kinOf, layoutOf } from "../screens/sessions/kin.js";
import { comparePins, pinAt } from "../screens/sessions/pin.js";
import { useFolds } from "../screens/sessions/folds.js";
import { spoke } from "../agent.js";
import { LiveLines, aboutOf, headed, knownOf, liveBlocksOf, splitNotes, stateOf, useAnswers } from "../screens/sessions/blocks.js";
import { GhostCard, HeadActs, PastCard } from "../screens/sessions/sessioncard.js";
import { NewSessionList, newSessionGroups } from "../screens/sessions/new.js";
import { useSessionsArchive } from "../history.js";
import { Popover } from "../ui/popover.js";
import { useMenu } from "../ui/menu.js";
import { plural } from "../format.js";
import { ClosedMenu, ProjectMenu, liveMenuOf } from "./sessionmenu.js";

// How many closed conversations the shelf shows, and how many it asks the
// archive for to find them: the shelf keeps the latest conversation of a
// project and drops the rest, so it asks for more than it shows.
const SHELF = 6;

const SHELF_ASK = 30;

// ContourPick chooses which contours the column shows. Given live and waits,
// maps by name, a contour says how many of its sessions are open and, with a
// dot before its name as the tab on the phone, that some wait for the person.
export function ContourPick({ names, picks, onToggle, live = null, waits = null }) {
    return html`
        <div class="dkpick" role="group" aria-label="contours">
            ${names.map((name) => {
                const open = (live && live.get(name)) || 0;
                const waiting = (waits && waits.get(name)) || 0;
                const said = [name, open > 0 && `${open} open`, waiting > 0 && `${waiting} waiting for you`].filter(Boolean).join(", ");
                return html`
                    <button class="dkpickbtn" key=${name} type="button" title=${said} aria-label=${said}
                            aria-pressed=${picks.includes(name) ? "true" : "false"}
                            onClick=${() => onToggle(name)}>
                        ${waiting > 0 && html`<span class="waitn" aria-hidden="true"><i></i></span>`}
                        <span class="dkpickname">${name}</span>
                        ${live && html`<span class="dkpickn" aria-hidden="true">${open}</span>`}
                    </button>
                `;
            })}
        </div>
    `;
}

// raising lists the sessions being started that are not in the snapshot yet.
export function raising(sessions, opening, at) {
    const names = (sessions || []).map((s) => s.session);
    return (opening || []).filter((task) => !settled(task, names, at));
}

// orderOf lays the live sessions out in the order of the project map: the
// projects as the person arranged them, the sessions no project holds after
// them, and sessions of one place by name. The snapshot lists sessions by how
// full they are, which changes by the minute.
export function orderOf(sessions, map, wait = null) {
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
    const ordered = [...(sessions || [])].sort((a, b) => place(a) - place(b)
        || String(a.session).localeCompare(String(b.session)));
    if (!ordered.some((s) => pinAt(s, wait))) return ordered;
    const pageOf = contourOfLive(map || [], ordered);
    const contours = new Map((map || []).map((p) => [p.profile, []]));
    for (const s of ordered) {
        const page = pageOf(s);
        if (!contours.has(page)) contours.set(page, []);
        contours.get(page).push(s);
    }
    return [...contours.values()].flatMap((list) => {
        const { rootOf } = kinOf(list);
        const roots = list.filter((s) => rootOf(s) === s && pinAt(s, wait))
            .sort((a, b) => comparePins(pinAt(a, wait), pinAt(b, wait)));
        return [...roots.flatMap((root) => [root, ...list.filter((s) => s !== root && rootOf(s) === root)]),
            ...list.filter((s) => !roots.includes(rootOf(s)))];
    });
}

// leftOf is how long a window of a limit has before it starts over, rounded
// up in the one unit it is said in: minutes under an hour, hours under
// dayFrom of them, days past that. Rounding up that reaches the next unit is
// said in it: 59.5 minutes are an hour. Nothing when the snapshot does not
// say; nought once the moment has passed and the snapshot has not caught up.
function leftOf(part, dayFrom) {
    const at = part && part.resetsAt;
    if (!at) return null;
    const left = at * 1000 - Date.now();
    if (left <= 0) return { n: 0, unit: "" };
    const minutes = Math.ceil(left / 60000);
    if (minutes < 60) return { n: minutes, unit: "m" };
    const hours = Math.ceil(left / 3600000);
    if (hours < dayFrom) return { n: hours, unit: "h" };
    return { n: Math.ceil(left / 86400000), unit: "d" };
}

// resetIn says in the details when a window of a limit starts over, or
// nothing when the snapshot does not say. The details keep hours up to two
// days: they are where the finer figure is read.
function resetIn(part) {
    const left = leftOf(part, 48);
    if (!left) return "";
    if (left.n === 0) return "any moment";
    return `${left.n} ${left.unit === "m" ? "min" : left.unit}`;
}

// shortLeft is the time left under a ring: a number and one letter, a day
// from twenty-four hours on. A moment already past reads 0m: the name of the
// window beside a countdown would read as hours left. Nothing when the
// snapshot does not say.
function shortLeft(part) {
    const left = leftOf(part, 24);
    if (!left) return "";
    return left.n === 0 ? "0m" : `${left.n}${left.unit}`;
}

// limitLevel is the tone of a share of a limit: warning from 70, critical
// from 90, the usual colour under that.
function limitLevel(value) {
    return value >= 90 ? "dkcrit" : value >= 70 ? "dkwarn" : "";
}

// Ring is one window of a contour's limit: a circle filled by the share spent,
// the share inside it, and under it how long until the window starts over —
// or the window itself when that is not known. The fill is one colour, which
// steps at the thresholds of limitLevel.
function Ring({ label, title, part, agent = "" }) {
    const value = Math.round((part && part.pct) || 0);
    const when = resetIn(part);
    return html`
        <span class=${`dkring ${limitLevel(value)}`.trim()} data-agent=${agent || undefined}
              data-tip=${when ? `${title}: resets in ${when}` : title}>
            <span class="dkringdial" style=${`--share:${Math.min(100, value)}`}><b>${value}</b></span>
            <span class="dkringlabel">${shortLeft(part) || label}</span>
        </span>
    `;
}

// Window is one window of a limit in the details: the share, a bar and when
// it starts over.
function Window({ title, part }) {
    const value = Math.round((part && part.pct) || 0);
    const level = limitLevel(value);
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

// ContourLimits is the subscription limit of a contour in its heading: a ring
// a window claude tells, and their details under them on a press; an account
// with no five-hour window has the week alone. Numbers the
// snapshot has not renewed for a while are dimmed and say how old they are.
export function ContourLimits({ limits, name, profiles }) {
    const [open, setOpen] = useState(false);
    const close = useCallback(() => setOpen(false), []);
    const id = ((profiles || []).find((p) => p.profile === name) || {}).id || 0;
    const c = contourOf(limits, name, id);
    if (!c) return html`<span class="dkrings dknone">no numbers yet</span>`;
    // Codex's week stands beside claude's windows, a ring of its own in
    // codex's hue; a contour codex alone has spent in has that ring alone.
    const week = (c.codex && c.codex.sevenDay) || null;
    const old = staleLimits(c)
        ? `The numbers are from ${agoText(c.ageSec)}: they are renewed when a session of the contour answers`
        : "";
    return html`
        <span class="dkringsctl">
            <button class=${`dkrings${old ? " dkold" : ""}`} type="button" aria-expanded=${open ? "true" : "false"}
                    aria-label=${`the limits of contour ${name}`} onClick=${() => setOpen(!open)}>
                ${old && html`<span class="dkringsage">${agoText(c.ageSec)}</span>`}
                ${c.fiveHour && html`<${Ring} label="5h" title="Five hours" part=${c.fiveHour} />`}
                ${c.sevenDay && html`<${Ring} label="7d" title="Seven days" part=${c.sevenDay} />`}
                ${week && html`<${Ring} label="7d" title="Codex, seven days" part=${week} agent="codex" />`}
            </button>
            <${Popover} open=${open} onClose=${close} label=${`the limits of contour ${name}`}>
                <div class="dklimpop">
                    <div class="dklimpophead">${name}</div>
                    ${c.fiveHour && html`<${Window} title="Five hours" part=${c.fiveHour} />`}
                    ${c.sevenDay && html`<${Window} title="Seven days" part=${c.sevenDay} />`}
                    ${week && html`<${Window} title="Codex · seven days" part=${week} />`}
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
    const [open, setOpen] = useState(false);
    const close = useCallback(() => setOpen(false), []);
    const groups = newSessionGroups(entry, snapshot);
    if (groups.length === 0) return null;
    return html`
        <span class="dknewctl">
            <button class=${`dknew${open ? " on" : ""}`} type="button" aria-expanded=${open ? "true" : "false"}
                    aria-label=${`a new session in contour ${name}`} onClick=${() => setOpen(!open)}><${Icon.plus} /></button>
            <${Popover} open=${open} onClose=${close} label=${`a new session in contour ${name}`}>
                <div class="dknewpop">
                    <div class="dklimpophead">A new session in ${name}</div>
                    <${NewSessionList} entry=${entry} snapshot=${snapshot} exec=${exec} onClose=${close} desktop />
                </div>
            <//>
        </span>
    `;
}

// ProjectHead names the project of a block (see headed): its name, the group
// of the map it stands in, and its controls — a new session in it, its pause
// and a restart of all of it (see HeadActs). The heading opens the menu of
// the project on a right click: its pause, a new session, its terminal, its
// page in the settings of the map (onSettings, with where the page stands).
function ProjectHead({ block, entry, snapshot, exec, wait, onTerm, onSettings }) {
    const run = useAction();
    const menu = useMenu();
    const p = block.project;
    const sessions = snapshot?.sessions || [];
    const start = p && !block.home ? () => {
        if (!knows(exec, "session.open")) return;
        return run("session.open", p.session || p.name, { project: p.id });
    } : null;
    return html`
        <div class="dkproj" onContextMenu=${menu && p && menu(ProjectMenu, {
            project: p, profile: entry, sessions, exec, onNew: start, onTerm,
            onSettings: onSettings && entry ? () => onSettings({ contour: entry.id, project: p.id }) : null,
        }, `project ${block.name}`)}>
            <span class="dkprojname">${block.name}</span>
            ${block.group && html`<span class="dkprojgroup">${block.group}</span>`}
            <${HeadActs} block=${block} profile=${entry} sessions=${sessions} exec=${exec} wait=${wait} />
        </div>
    `;
}

// ContourSection is one contour in the column: its heading with how many of
// its sessions wait for the person, the way to a new one and the limit; then
// its projects, each under its heading, with the cards of their live
// sessions, the sessions those opened and the runs they started under them,
// and the sessions being raised in them. blocks holds the projects as the
// column lays them out.
function ContourSection({ name, entry, list, blocks, archive, folds, limits, map, snapshot, current, onPick, onTerm, onSettings, exec, wait }) {
    const menu = useMenu();
    const waits = list.filter((s) => stateOf(s, snapshot && snapshot.limits).tone === "wait").length;
    const open = (session, id) => onPick({ name: session, id });
    const menuOf = (s) => menu && menu(...liveMenuOf(s, {
        exec, snapshot, wait, onOpen: () => open(s.session, s.sessionId), onTerm, profile: entry,
    }));
    const notes = splitNotes((snapshot && snapshot.sessionNotes) || [], (snapshot && snapshot.sessions) || []).own;
    return html`
        <section>
            <div class="dkcontour dklimhead">
                <span class="dkcontourname">${name}</span>
                ${waits > 0 && html`
                    <span class="waitn dkwaitn" data-tip=${`${waits} ${plural(waits, "session waits", "sessions wait")} for you`}>
                        <i></i>${waits}
                    </span>
                `}
                <${NewSession} name=${name} entry=${entry} snapshot=${snapshot} exec=${exec} />
                <${ContourLimits} limits=${limits} name=${name} profiles=${map} />
            </div>
            ${blocks.map((block) => {
                const known = knownOf(block);
                return html`
                    <div class="dkblock" key=${block.key}>
                        ${headed(block) && html`<${ProjectHead} block=${block} entry=${entry} snapshot=${snapshot} exec=${exec} wait=${wait}
                                                                onTerm=${onTerm} onSettings=${onSettings} />`}
                        <${LiveLines} list=${block.live} live=${snapshot?.sessions || []} past=${archive} limits=${limits} wait=${wait} notes=${notes}
                                      exec=${exec} profile=${entry} profiles=${map} known=${known}
                                      folds=${folds} desk current=${current} menuOf=${menuOf} onOpen=${open}
                                      onStub=${(row) => onPick({ name: row.name, id: row.sessionId, archived: true, row })} />
                        ${block.ghosts.map((task) => html`<${GhostCard} key=${`+${task.target}`} task=${task} known=${known} />`)}
                    </div>
                `;
            })}
            ${blocks.length === 0 && html`<p class="dkempty">nothing live</p>`}
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
        if (!spoke(row)) continue;
        const key = row.project && row.project.id ? `p${row.project.id}` : `d:${row.cwd || row.name}`;
        if (seen.has(key)) continue;
        seen.add(key);
        out.push(row);
        if (out.length >= max) break;
    }
    return out;
}

// Shelf is the closed conversations of the contours shown, newest first, under
// a heading of its own below all of them, each a card of its own dimmed, its
// contour beside its name when the column shows more than one; the archive
// panel holds the rest. A right click opens the menu of a closed one.
function Shelf({ past, labelOf, projectOf, currentId, onPick, onArchive, exec, snapshot }) {
    const menu = useMenu();
    if (past.kind === "loading") return null;
    const rows = past.kind === "ready" ? shelfOf((past.archive && past.archive.rows) || []) : [];
    if (past.kind === "ready" && rows.length === 0) return null;
    const open = (row) => onPick({ name: row.name, id: row.sessionId, archived: true, row });
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
                <${PastCard} key=${row.sessionId || row.name} row=${row} about=${aboutOf(row, projectOf(row))} exec=${exec}
                             contour=${labelOf(row)} desk current=${Boolean(currentId) && currentId === row.sessionId}
                             onOpen=${open}
                             onMenu=${menu && menu(ClosedMenu, { row, exec, onOpen: () => open(row), snapshot }, `conversation ${row.name}`)} />
            `)}
        </section>
    `;
}

// sectionOfTask finds the contour a session is being raised in: by the project
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

export function SessionColumn({ snapshot, limits, current, currentId, onPick, onTerm, onSettings, picks, setPicks, onNames, onOrder, onArchive, exec, wait }) {
    const all = (snapshot && snapshot.sessions) || [];
    // A row stops waiting with the head of its conversation, not at the next
    // snapshot.
    useAnswers();

    const map = (snapshot && snapshot.profileMap) || [];
    const names = useMemo(() => {
        const known = pageNames(map, limits);
        return known.length ? known : [...new Set(all.map((s) => contourName(s.profile)))];
    }, [map, limits, all]);

    // A session that names no contour and that no contour of the map claims
    // stands in the personal contour of the map, as on the phone; with no map
    // at all, in the one the panel calls personal.
    const pageOf = useMemo(() => contourOfLive(map, all), [map, all]);
    useEffect(() => { if (onNames) onNames(names); }, [names.join("\n")]);

    const ordered = useMemo(() => orderOf(all, map, wait), [all, map, wait]);
    const shown = picks.length ? ordered.filter((s) => picks.includes(pageOf(s))) : ordered;

    // What the picker says of every contour, shown or not: its open sessions
    // and those of them that wait for the person.
    const live = new Map();
    const waits = new Map();
    for (const s of all) {
        const key = pageOf(s);
        live.set(key, (live.get(key) || 0) + 1);
        if (stateOf(s, snapshot && snapshot.limits).tone === "wait") waits.set(key, (waits.get(key) || 0) + 1);
    }

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

    const ghostsIn = new Map();
    for (const task of ghosts) {
        const name = sectionOfTask(task, map, sections);
        if (!ghostsIn.has(name)) ghostsIn.set(name, []);
        ghostsIn.get(name).push(task);
    }

    // The rows of every contour as they stand: its projects in the order of
    // the map, a block each as the phone lays them out, and in a block its
    // live sessions with the sessions a session opened under it while their
    // fold is open. The arrow keys go down the same rows, block after block;
    // a run and a session still being raised are not among them.
    const entryOf = (name) => map.find((p) => p.profile === name) || null;
    const folds = useFolds();
    const [folded] = folds;
    const blocksIn = new Map(sections.map((n) => [n, liveBlocksOf({
        profile: entryOf(n), sessions: byProfile.get(n) || [], opening: ghostsIn.get(n) || [], wait,
    }).map((block) => ({ block, rows: layoutOf(block.live, all, folded) }))]));
    const flat = [...blocksIn.values()].flat().flatMap((b) => b.rows)
        .filter((e) => e.kind === "row").map((e) => e.s);

    useEffect(() => {
        if (onOrder) onOrder(flat.map((s) => ({ name: s.session, id: s.sessionId })));
    }, [flat.map((s) => s.session).join("\n")]);

    // The shelf asks for every contour shown at once: those of the map by the
    // id of their entry, the rest by the name the collector knows them by.
    const ids = sections.map((n) => (entryOf(n) || {}).id).filter(Boolean);
    const others = sections.filter((n) => !(entryOf(n) || {}).id);
    const skip = all.map((s) => s.sessionId).filter(Boolean);
    // The closed conversations are read once for the shelf and for the
    // parents that closed: a stub says when its parent closed and opens it.
    const past = useSessionsArchive({ limit: SHELF_ASK, contour: ids, profile: others, skip });
    const archive = past.kind === "ready" ? (past.archive && past.archive.rows) || [] : [];
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
                live=${live}
                waits=${waits}
                onToggle=${(name) => setPicks((prev) => (prev.includes(name) ? prev.filter((x) => x !== name) : [...prev, name]))}
            />
            <div class="dkscroll">
                ${sections.map((name) => html`
                    <${ContourSection} key=${name} name=${name} entry=${entryOf(name)}
                                       list=${byProfile.get(name) || []} blocks=${(blocksIn.get(name) || []).map((b) => b.block)}
                                       archive=${archive} folds=${folds} limits=${limits} map=${map}
                                       snapshot=${snapshot} current=${current} onPick=${onPick}
                                       onTerm=${onTerm} onSettings=${onSettings} exec=${exec} wait=${wait} />
                `)}
                ${sections.length === 0 && ghosts.map((task) => html`<${GhostCard} key=${`+${task.target}`} task=${task} />`)}
                ${sections.length === 0 && ghosts.length === 0 && html`<p class="dkempty">there are no live sessions</p>`}
                ${sections.length > 0 && html`
                    <${Shelf} past=${past} labelOf=${labelOf} projectOf=${projectOf}
                              currentId=${currentId} onPick=${onPick} onArchive=${onArchive} exec=${exec} snapshot=${snapshot} />
                `}
            </div>
        </aside>
    `;
}
