// The left column of the sessions section: the switch of contours, the live
// sessions of the contour it stands on under a heading with its limit, and the
// closed conversations of that contour on a shelf of their own below.
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
import { liveOf, settled } from "../catchup.js";
import { contourOf, limitGroups, LimitGroup, LimitNotes } from "../screens/sessions/limits.js";
import { contourName } from "../contour.js";
import { pageNames, useProfilePage } from "../screens/sessions/pages.js";
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

// ContourTabs is the switch of the column, as the tabs of the phone are: a tab
// a contour, and the column shows the one pressed. live and waits, maps by
// name, give how many of a contour's sessions are open, said after its name,
// and how many of them wait for the person, a dot before it.
export function ContourTabs({ names, current, onPick, live, waits }) {
    return html`
        <div class="dkpick" role="tablist" aria-label="contours">
            ${names.map((name) => {
                const open = live.get(name) || 0;
                const waiting = waits.get(name) || 0;
                const said = [name, open > 0 && `${open} open`, waiting > 0 && `${waiting} waiting for you`].filter(Boolean).join(", ");
                return html`
                    <button class="dkpickbtn" key=${name} type="button" role="tab" title=${said} aria-label=${said}
                            aria-selected=${name === current ? "true" : "false"}
                            onClick=${() => onPick(name)}>
                        ${waiting > 0 && html`<span class="waitn" aria-hidden="true"><i></i></span>`}
                        <span class="dkpickname">${name}</span>
                        <span class="dkpickn" aria-hidden="true">${open}</span>
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

// The desktop groups a subscription's windows while sharing the phone's
// details, so adding a provider needs no separate layout.
export function ContourLimits({ limits, name, profiles }) {
    const [opened, setOpened] = useState(null);
    const close = useCallback(() => setOpened(null), []);
    const toggle = (key, event) => {
        const reading = event.currentTarget.getBoundingClientRect();
        const row = event.currentTarget.closest(".dklimitrow").getBoundingClientRect();
        setOpened((current) => current && current.key === key ? null : { key, x: reading.left + reading.width / 2 - row.left });
    };
    const id = ((profiles || []).find((p) => p.profile === name) || {}).id || 0;
    const c = contourOf(limits, name, id);
    const groups = limitGroups(c);
    return html`
        <div class="dklimitsummary">
            <div class="dklimitrow" role="group" aria-label=${`the limits of contour ${name}`}>
                ${groups.map((group) => html`<${LimitGroup} key=${group.agent} group=${group} chip
                    opened=${opened} onToggle=${toggle} onClose=${close} />`)}
                ${groups.length === 0 && html`<span class="dklimitnone">no numbers yet</span>`}
            </div>
            <${LimitNotes} contour=${c} groups=${groups} compact />
        </div>
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
                    aria-label=${`a new session in contour ${name}`} data-tip="New session"
                    onClick=${() => setOpen(!open)}><${Icon.plus} /></button>
            <${Popover} open=${open} onClose=${close} label=${`a new session in contour ${name}`}>
                <div class="dknewpop">
                    <div class="dknewhead">A new session in ${name}</div>
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
function ProjectHead({ block, entry, snapshot, exec, wait, onTerm, onSettings, folded, onToggle }) {
    const run = useAction();
    const menu = useMenu();
    const p = block.project;
    const sessions = snapshot?.sessions || [];
    const waits = block.live.filter((s) => stateOf(s, snapshot?.limits).tone === "wait").length;
    const start = p && !block.home ? () => {
        if (!knows(exec, "session.open")) return;
        return run("session.open", p.session || p.name, { project: p.id });
    } : null;
    return html`
        <div class="dkproj" onClick=${(event) => { if (!event.target.closest("button")) onToggle(); }}
             onContextMenu=${menu && p && menu(ProjectMenu, {
            project: p, profile: entry, sessions, exec, onNew: start, onTerm,
            onSettings: onSettings && entry ? () => onSettings({ contour: entry.id, project: p.id }) : null,
        }, `project ${block.name}`)}>
            <button class="dkprojname" type="button" data-tip=${block.name}
                    aria-expanded=${folded ? "false" : "true"} onClick=${onToggle}>
                <span class="dkprojchev" aria-hidden="true"><${Icon.chevron} /></span>
                <span class="dkprojlabel">${block.name}</span>
            </button>
            ${block.group && html`<span class="dkprojgroup">${block.group}</span>`}
            ${folded && html`<span class="dkprojcount">${block.live.length} ${plural(block.live.length, "session", "sessions")}</span>`}
            ${folded && waits > 0 && html`<span class="waitn dkwaitn" role="img" aria-label=${`${waits} waiting for you`}><i></i>${waits}</span>`}
            <${HeadActs} block=${block} profile=${entry} sessions=${sessions} exec=${exec} wait=${wait} />
        </div>
    `;
}

// ContourSection is one contour in the column: its heading in two rows — the
// name with how many of its sessions wait for the person, and at the edge the
// contour's actions as the phone has them over its page: its task registry
// (onTasks, with its entry of the map), its page in the settings of the map
// (onSettings) and a new session; under them the limit. A contour the map
// does not hold has neither a registry nor a page, and says so with the two
// buttons off. Then its projects, each under its heading, with the cards of
// their live sessions, the sessions those opened and the runs they started
// under them, and the sessions being raised in them. blocks holds the
// projects as the column lays them out.
function ContourSection({ name, entry, list, blocks, archive, folds, projectFolds, limits, map, snapshot, current, onPick, onTerm, onSettings, onTasks, exec, wait }) {
    const menu = useMenu();
    const waits = list.filter((s) => stateOf(s, snapshot && snapshot.limits).tone === "wait").length;
    const open = (session, id) => onPick({ name: session, id });
    const menuOf = (s) => menu && menu(...liveMenuOf(s, {
        exec, snapshot, wait, onOpen: () => open(s.session, s.sessionId), onTerm, profile: entry,
    }));
    const notes = splitNotes((snapshot && snapshot.sessionNotes) || [], (snapshot && snapshot.sessions) || []).own;
    const [folded, toggle] = projectFolds;
    return html`
        <section>
            <div class="dkcontour dklimhead">
                <div class="dkcontourrow">
                    <span class="dkcontourname">${name}</span>
                    ${waits > 0 && html`
                        <span class="waitn dkwaitn" data-tip=${`${waits} ${plural(waits, "session waits", "sessions wait")} for you`}>
                            <i></i>${waits}
                        </span>
                    `}
                    <span class="dkcontouracts" role="group" aria-label=${`the actions of contour ${name}`}>
                        ${onTasks && html`
                            <button class="dknew" type="button" aria-label="Tasks" data-tip="Tasks" disabled=${!entry}
                                    onClick=${() => onTasks(entry)}><${Icon.checklist} /></button>
                        `}
                        ${onSettings && html`
                            <button class="dknew" type="button" aria-label="Profile" data-tip="Profile" disabled=${!entry}
                                    onClick=${() => onSettings({ contour: entry.id })}><${Icon.badge} /></button>
                        `}
                        <${NewSession} name=${name} entry=${entry} snapshot=${snapshot} exec=${exec} />
                    </span>
                </div>
                <div class="dkcontourrow">
                    <${ContourLimits} limits=${limits} name=${name} profiles=${map} />
                </div>
            </div>
            ${blocks.map((block) => {
                const known = knownOf(block);
                const closed = headed(block) && folded.has(block.key);
                return html`
                    <div class="dkblock" key=${block.key}>
                        ${headed(block) && html`<${ProjectHead} block=${block} entry=${entry} snapshot=${snapshot} exec=${exec} wait=${wait}
                                                                onTerm=${onTerm} onSettings=${onSettings} folded=${closed}
                                                                onToggle=${() => toggle(block.key)} />`}
                        <div class="dkprojectrows" hidden=${closed}>
                        <${LiveLines} list=${block.live} live=${snapshot?.sessions || []} past=${archive} limits=${limits} wait=${wait} notes=${notes}
                                      exec=${exec} profile=${entry} profiles=${map} project=${block.project} known=${known}
                                      folds=${folds} desk current=${current} menuOf=${menuOf} onOpen=${open}
                                      onStub=${(row) => onPick({ name: row.name, id: row.sessionId, archived: true, row })} />
                        ${block.ghosts.map((task) => html`<${GhostCard} key=${`+${task.target}`} task=${task} known=${known} />`)}
                        </div>
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

// Shelf is the closed conversations of the contour shown, newest first, under
// a heading of its own below its live sessions, each a card of its own
// dimmed; the archive panel holds the rest. A right click opens the menu of a
// closed one.
function Shelf({ past, projectOf, currentId, onPick, onArchive, exec, snapshot }) {
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
                             desk current=${Boolean(currentId) && currentId === row.sessionId}
                             onOpen=${open}
                             onMenu=${menu && menu(ClosedMenu, { row, exec, onOpen: () => open(row), snapshot }, `conversation ${row.name}`)} />
            `)}
        </section>
    `;
}

// contourOfTask finds the contour a session is being raised in: by the project
// it was opened from, then by its name among the projects of the map; nothing
// for one the map does not know.
function contourOfTask(task, map) {
    const project = task.params && task.params.project;
    for (const entry of map) {
        for (const group of entry.groups || []) {
            for (const p of group.projects || []) {
                if ((project && p.id === project) || (!project && p.session === task.target)) return entry.profile;
            }
        }
    }
    return "";
}

// contourOfTalk finds the contour of a conversation opened in the centre: a
// live one stands where its session stands (pageOf); a closed one stands in
// the contour of the map that holds its project, else in the one the
// collector names it by — through the id the limits give that name, or the
// name itself where there is no map. Nothing when its row is not at hand.
function contourOfTalk(talk, { all, pageOf, map, limits }) {
    const live = liveOf(all, talk);
    if (live) return pageOf(live);
    const row = talk && talk.row;
    if (!row) return "";
    const project = row.project && row.project.id;
    const holder = project && map.find((e) => (e.groups || []).some((g) => (g.projects || []).some((p) => p.id === project)));
    if (holder) return holder.profile;
    const known = ((limits && limits.contours) || []).find((c) => c.profile === row.profile);
    const entry = known && map.find((e) => e.id === known.contour);
    return entry ? entry.profile : contourName(row.profile);
}

export function SessionColumn({ snapshot, limits, current, currentId, onPick, onTerm, onSettings, onTasks, onNames, onShown, onOrder, onArchive, reveal, onRevealed, exec, wait }) {
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
    // at all, in the one the panel calls personal. Either way it stands in one
    // of names, so every live session has a tab to be found under.
    const pageOf = useMemo(() => contourOfLive(map, all), [map, all]);
    useEffect(() => { if (onNames) onNames(names); }, [names.join("\n")]);

    // The contour the switch stands on is kept as the phone keeps the page of
    // its sessions tab, under the same key: one device keeps one choice of
    // contour, as it keeps one set of folded projects. With none kept, or one
    // no longer among the contours, it is the first contour.
    const [shown, show] = useProfilePage(names);
    useEffect(() => { if (onShown) onShown(shown); }, [shown]);

    // A conversation opened from outside the column — a push, home, a
    // terminal, the archive — moves the switch to its contour as a press on
    // its tab would, so its row stands lit and the arrow keys go on from it.
    // One whose contour has no tab leaves the switch where it is. It is read
    // once the snapshot is there, and once only: the shell lets go of it on
    // onRevealed, so the column drawn again later keeps the tab it was left on.
    useEffect(() => {
        if (!reveal || !snapshot) return;
        const contour = contourOfTalk(reveal, { all, pageOf, map, limits });
        if (names.includes(contour) && contour !== shown) show(contour);
        if (onRevealed) onRevealed();
    }, [reveal, snapshot]);

    const ordered = useMemo(() => orderOf(all, map, wait), [all, map, wait]);
    const list = ordered.filter((s) => pageOf(s) === shown);

    // What the switch says of every contour, shown or not: its open sessions
    // and those of them that wait for the person.
    const live = new Map();
    const waits = new Map();
    for (const s of all) {
        const key = pageOf(s);
        live.set(key, (live.get(key) || 0) + 1);
        if (stateOf(s, snapshot && snapshot.limits).tone === "wait") waits.set(key, (waits.get(key) || 0) + 1);
    }

    // A session being raised stands in its contour, and one the map does not
    // know in the contour shown, so it is seen while it comes up.
    const ghosts = raising(all, wait ? wait.opening() : [], (snapshot && snapshot.at) || 0);
    const opening = ghosts.filter((task) => [shown, ""].includes(contourOfTask(task, map)));

    // The rows of the contour as they stand: its projects in the order of the
    // map, a block each as the phone lays them out, and in a block its live
    // sessions with the sessions a session opened under it while their fold is
    // open. The arrow keys go down the same rows, block after block; a run and
    // a session still being raised are not among them.
    const entry = map.find((p) => p.profile === shown) || null;
    const folds = useFolds();
    const projectFolds = useFolds("aacpanel.project-folds");
    const [folded] = folds;
    const [projectsFolded] = projectFolds;
    const blocks = liveBlocksOf({ profile: entry, sessions: list, opening, wait });
    const flat = blocks.flatMap((block) =>
        headed(block) && projectsFolded.has(block.key) ? [] : layoutOf(block.live, all, folded))
        .filter((e) => e.kind === "row").map((e) => e.s);

    useEffect(() => {
        if (onOrder) onOrder(flat.map((s) => ({ name: s.session, id: s.sessionId })));
    }, [flat.map((s) => s.session).join("\n")]);

    // The shelf asks for the contour shown: one of the map by the id of its
    // entry, any other by the name the collector knows it by.
    const skip = all.map((s) => s.sessionId).filter(Boolean);
    // The closed conversations are read once for the shelf and for the
    // parents that closed: a stub says when its parent closed and opens it.
    const past = useSessionsArchive(entry && entry.id
        ? { limit: SHELF_ASK, contour: entry.id, skip }
        : { limit: SHELF_ASK, profile: shown, skip });
    const archive = past.kind === "ready" ? (past.archive && past.archive.rows) || [] : [];
    const projects = useMemo(() => {
        const byId = new Map();
        for (const e of map) {
            for (const group of e.groups || []) {
                for (const p of group.projects || []) byId.set(p.id, p);
            }
        }
        return byId;
    }, [map]);
    const projectOf = (row) => (row.project && projects.get(row.project.id)) || null;

    return html`
        <aside class="dkleft">
            <${ContourTabs} names=${names} current=${shown} onPick=${show} live=${live} waits=${waits} />
            <div class="dkscroll" role=${shown ? "tabpanel" : undefined} aria-label=${shown || undefined}>
                ${shown && html`
                    <${ContourSection} key=${shown} name=${shown} entry=${entry} list=${list} blocks=${blocks}
                                       archive=${archive} folds=${folds} projectFolds=${projectFolds} limits=${limits} map=${map}
                                       snapshot=${snapshot} current=${current} onPick=${onPick}
                                       onTerm=${onTerm} onSettings=${onSettings} onTasks=${onTasks} exec=${exec} wait=${wait} />
                    <${Shelf} past=${past} projectOf=${projectOf}
                              currentId=${currentId} onPick=${onPick} onArchive=${onArchive} exec=${exec} snapshot=${snapshot} />
                `}
                ${!shown && ghosts.map((task) => html`<${GhostCard} key=${`+${task.target}`} task=${task} />`)}
                ${!shown && ghosts.length === 0 && html`<p class="dkempty">there are no live sessions</p>`}
            </div>
        </aside>
    `;
}
