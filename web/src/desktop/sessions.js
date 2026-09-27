// The left column of the sessions section: contour picker, live sessions by contour with their limits.
import { useCallback, useEffect, useMemo, useState } from "preact/hooks";

import { html } from "../html.js";
import { Icon } from "../ui/icons.js";
import { fill, pct, plural } from "../format.js";
import { knows, whyNot } from "../exec.js";
import { waitText, waitTip } from "../ui/waits.js";
import { useAction } from "../actions/gate.js";
import { settled } from "../catchup.js";
import { agoText, contourOf, staleLimits } from "../screens/sessions/limits.js";
import { contourName } from "../contour.js";
import { pageNames } from "../screens/sessions/pages.js";
import { contoursOf } from "../screens/sessions/map.js";
import { kinLabel, kinOf } from "../screens/sessions/kin.js";
import { aboutOf } from "../screens/sessions/blocks.js";
import { stamp, when } from "../screens/sessions/card.js";
import { useSessionsArchive } from "../history.js";
import { Popover } from "../ui/popover.js";

// How many cards a contour shows at least: the live sessions, and its latest
// closed conversations for the places they leave.
const MIN_CARDS = 3;

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

function last(s) {
    if (s.ask) return "waiting for an answer to a question";
    if (s.status === "waiting" || s.waitingFor) return waitText(s.waitingFor);
    if (s.status === "busy") return "handling the request";
    if (!s.lastRequestAt) return "no requests yet";
    return "waiting for a message";
}

function held(task) {
    return Math.max(0, Math.round((Date.now() - task.since) / 1000));
}

// raising lists the consoles being started that are not in the snapshot yet.
export function raising(sessions, opening, at) {
    const names = (sessions || []).map((s) => s.session);
    return (opening || []).filter((task) => !settled(task, names, at));
}

function GhostLine({ task }) {
    return html`
        <div class="dksess" role="status" style="cursor:default">
            <span class="dkdot dkwaiting dkside"></span>
            <span class="dksessbody">
                <span class="dksessmain">
                    <span class="dkname">${task.target}</span>
                    <span class="dknum">${held(task)} s</span>
                </span>
                <span class="dksesssub">
                    <span class="dklast">the console is starting on the host</span>
                </span>
            </span>
            <span class="dkrowacts"></span>
        </div>
    `;
}

function SessionLine({ s, group, current, onPick, index, exec, wait, kid = false }) {
    const run = useAction();
    const status = s.status || "idle";
    const waiting = status === "waiting" || Boolean(s.waitingFor);
    const closing = wait ? wait.of("close", s.session) : null;
    const restarting = wait ? wait.of("restart", s.session) : null;
    const busy = closing || restarting;
    const known = knows(exec, "session.close");
    const can = known && !closing;
    const restartKnown = knows(exec, "session.restart");
    const canRestart = restartKnown && !restarting;
    const answerable = Boolean(s.ask);

    return html`
        <button
            class=${`dksess${current === s.session ? " on" : ""}${kid ? " dkkid" : ""}`}
            type="button"
            style=${`--fill:${Math.min(100, s.pct || 0)}%`}
            onClick=${() => onPick({ name: s.session, id: s.sessionId })}
        >
            <span class=${`dkdot dk${busy ? "off" : status} dkside`}></span>
            <span class="dksessbody">
                <span class="dksessmain">
                    <span class="dkname">${s.session}</span>
                    ${s.home && html`<span class="dktag">home</span>`}
                    ${s.outside && !kid && html`<span class="dktag">outside</span>`}
                    ${s.remote && html`<span class="dkrc" data-tip="Remote Control is on: the session is open on claude.ai too">RC</span>`}
                    <span class="dknum">${s.limitKnown === false ? "—" : pct(s.pct)}</span>
                    <span class="dkhint">${index < 9 ? index + 1 : ""}</span>
                </span>
                <span class="dksesssub">
                    ${group && html`<span class="dkgroup dkchip">${group}</span>`}
                    <span class="dklast">${closing ? `closing · ${held(closing)} s`
                        : restarting ? `restarting · ${held(restarting)} s` : last(s)}</span>
                    ${waiting && !busy && html`
                        <span
                            class=${`dkwait${answerable ? "" : " dkpermit"}`}
                            data-tip=${answerable ? "Waiting for an answer to a question" : waitTip(s.waitingFor)}
                            data-tipside="left"
                        >${answerable ? "?" : "!"}</span>
                    `}
                </span>
            </span>
            <span class="dkrowacts" onClick=${(e) => e.stopPropagation()}>
                ${!s.home && !s.outside && html`
                    <i
                        class=${`dkact danger${can ? "" : " off"}`}
                        data-tip=${closing
                            ? "The session is already closing"
                            : known ? undefined : whyNot(exec, "session.close")}
                        data-tipside="left"
                        onClick=${async () => {
                            if (!can) return;
                            await run("session.close", s.session, {});
                        }}
                    ><${Icon.stop} /></i>
                `}
                ${s.home && html`
                    <i
                        class=${`dkact danger${canRestart ? "" : " off"}`}
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
            <span class=${`dksessbar ${fill(s.pct || 0)}`}><i style=${`width:${Math.min(100, s.pct || 0)}%`}></i></span>
        </button>
    `;
}

// KinFold holds the runs a session started inside its work, folded under it;
// the fold says whether one of them waits for the person.
function KinFold({ kids, line }) {
    const [open, setOpen] = useState(false);
    const waiting = kids.filter((s) => s.ask || s.status === "waiting" || s.waitingFor).length;
    return html`
        <button class="dkkin" type="button" aria-expanded=${open ? "true" : "false"} onClick=${() => setOpen(!open)}>
            <span class=${`dkkinchev${open ? " open" : ""}`}><${Icon.chevron} /></span>
            <span>${kinLabel(kids.length)}</span>
            ${waiting > 0 && html`<span class="dkkinwait">· ${waiting} waiting</span>`}
        </button>
        ${open && kids.map((s) => line(s, true))}
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

// PastLine is a closed conversation of a contour standing where a live
// session is missing: what it was about and when, opened from the archive,
// resumed with a press.
function PastLine({ row, on, onPick, exec }) {
    const run = useAction();
    const about = aboutOf(row, null);
    const ready = knows(exec, "session.resume");
    const group = row.project && row.project.group;
    return html`
        <button class=${`dksess dkpast${on ? " on" : ""}`} type="button"
                onClick=${() => onPick({ name: row.name, id: row.sessionId, archived: true, row })}>
            <span class="dkdot dkoff dkside"></span>
            <span class="dksessbody">
                <span class="dksessmain">
                    <span class="dkname">${row.name}</span>
                    <span class="dknum">${when(stamp(row.lastAt))}</span>
                </span>
                <span class="dksesssub">
                    ${group && html`<span class="dkgroup dkchip">${group}</span>`}
                    <span class="dklast">${about ? `«${about}»` : "closed"}</span>
                </span>
            </span>
            <span class="dkrowacts" onClick=${(e) => e.stopPropagation()}>
                <i class=${`dkact${ready ? "" : " off"}`}
                   aria-label="resume the conversation"
                   data-tip=${ready ? undefined : whyNot(exec, "session.resume")}
                   data-tipside="left"
                   onClick=${async () => {
                       if (!ready) return;
                       await run("session.resume", row.name, { session: row.sessionId });
                   }}><${Icon.resume} /></i>
            </span>
        </button>
    `;
}

// ContourSection is one contour in the column: its heading with the limit,
// its live sessions with the runs they started folded under them, and its
// latest closed conversations for the places up to MIN_CARDS the live ones
// leave.
function ContourSection({ name, id, list, limits, map, place, current, currentId, onPick, flat, exec, wait }) {
    const { own, kids } = kinOf(list);
    const need = Math.max(0, MIN_CARDS - own.length);
    const skip = list.map((s) => s.sessionId).filter(Boolean);
    const past = useSessionsArchive({ limit: MIN_CARDS, contour: id, profile: name, skip });
    const rows = need > 0 && past.kind === "ready" ? ((past.archive && past.archive.rows) || []).slice(0, need) : [];
    const line = (s, kid = false) => {
        const found = s.project === undefined ? place(s.cwd) : s.project;
        return html`<${SessionLine}
            key=${s.session}
            s=${s}
            group=${found ? found.group : ""}
            current=${current}
            onPick=${onPick}
            index=${kid ? 99 : flat.indexOf(s)}
            exec=${exec}
            wait=${wait}
            kid=${kid}
        />`;
    };
    return html`
        <section>
            <div class="dkcontour dklimhead">
                <span class="dkcontourname">${name}</span>
                <${ContourLimits} limits=${limits} name=${name} profiles=${map} />
            </div>
            ${own.map((s) => html`
                ${line(s)}
                ${kids.has(s.session) && html`<${KinFold} key=${`kin:${s.session}`} kids=${kids.get(s.session)} line=${line} />`}
            `)}
            ${rows.map((row) => html`<${PastLine} key=${row.sessionId} row=${row} on=${currentId === row.sessionId}
                                                  onPick=${onPick} exec=${exec} />`)}
            ${own.length === 0 && rows.length === 0 && past.kind !== "loading"
                && html`<p class="dkempty">nothing has been said in this contour yet</p>`}
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

export function SessionColumn({ snapshot, profiles, limits, current, currentId, onPick, picks, setPicks, onNames, onOrder, exec, wait }) {
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
    const shown = picks.length ? all.filter((s) => picks.includes(pageOf(s))) : all;

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

    useEffect(() => {
        if (onOrder) onOrder(flat.map((s) => ({ name: s.session, id: s.sessionId })));
    }, [flat.map((s) => s.session).join("\n")]);

    return html`
        <aside class="dkleft">
            <${ContourPick}
                names=${names}
                picks=${picks}
                onToggle=${(name) => setPicks((prev) => (prev.includes(name) ? prev.filter((x) => x !== name) : [...prev, name]))}
                onAll=${() => setPicks([])}
            />
            <div class="dkscroll">
                ${ghosts.map((task) => html`<${GhostLine} key=${`+${task.target}`} task=${task} />`)}
                ${sections.map((name) => html`
                    <${ContourSection} key=${name} name=${name}
                                       id=${(map.find((p) => p.profile === name) || {}).id || 0}
                                       list=${byProfile.get(name) || []} limits=${limits} map=${map} place=${place}
                                       current=${current} currentId=${currentId} onPick=${onPick} flat=${flat}
                                       exec=${exec} wait=${wait} />
                `)}
                ${sections.length === 0 && ghosts.length === 0 && html`<p class="dkempty">there are no live sessions</p>`}
            </div>
        </aside>
    `;
}
