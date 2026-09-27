// The left column of the sessions section: contour picker, live sessions by contour with their limits.
import { useEffect, useMemo, useState } from "preact/hooks";

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

function SessionLine({ s, group, current, onPick, index, exec, wait }) {
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
            class=${`dksess${current === s.session ? " on" : ""}`}
            type="button"
            style=${`--fill:${Math.min(100, s.pct || 0)}%`}
            onClick=${() => onPick({ name: s.session, id: s.sessionId })}
        >
            <span class=${`dkdot dk${busy ? "off" : status} dkside`}></span>
            <span class="dksessbody">
                <span class="dksessmain">
                    <span class="dkname">${s.session}</span>
                    ${s.home && html`<span class="dktag">home</span>`}
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
                ${!s.home && html`
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

// Meter is one window of a contour's limit: a bar and the share spent. When
// it resets is said on the line only once the window runs out — before that
// nobody plans around it — and in the tip always.
function Meter({ label, span, part, old }) {
    const value = Math.round((part && part.pct) || 0);
    const level = value >= 90 ? "dkcrit" : value >= 70 ? "dkwarn" : "";
    const left = resetIn(part);
    const tip = old || (left ? `The ${span} window resets in ${left}` : "When the window resets, the snapshot does not say");
    return html`
        <span class=${`dkmeter ${level}`.trim()} data-tip=${tip} data-tipside="left">
            <span class="dkmeterlabel">${label}</span>
            <span class="dkmeterbar"><i style=${`width:${Math.min(100, value)}%`}></i></span>
            <span class="dkmeterpct">${value}%</span>
            ${level && left && html`<span class="dkmeterreset">${left}</span>`}
        </span>
    `;
}

// ContourLimits is the subscription limit of a contour, beside its name: the
// limit belongs to the contour, and the sessions spending it stand right
// under. Numbers the snapshot has not renewed for a while are dimmed and say
// how old they are.
export function ContourLimits({ limits, name, profiles }) {
    const id = ((profiles || []).find((p) => p.profile === name) || {}).id || 0;
    const c = contourOf(limits, name, id);
    if (!c) return html`<span class="dkmeters dknone">no numbers yet</span>`;
    const old = staleLimits(c)
        ? `The numbers are from ${agoText(c.ageSec)}: statusLine writes them while a session of the contour answers`
        : "";
    return html`
        <span class=${`dkmeters${old ? " dkold" : ""}`}>
            <${Meter} label="5h" span="five-hour" part=${c.fiveHour} old=${old} />
            <${Meter} label="7d" span="seven-day" part=${c.sevenDay} old=${old} />
        </span>
    `;
}

// QuietLimits holds the limits of the shown contours with no live session:
// they have no heading in the list to stand beside, and one line each under
// it is all they are worth until work starts there.
export function QuietLimits({ limits, names, picks, profiles, live }) {
    const shown = (picks && picks.length ? names.filter((n) => picks.includes(n)) : names)
        .filter((name) => !live.has(name));
    if (shown.length === 0) return null;
    return html`
        <div class="dklimits dkquiet">
            <div class="dkquiethead">no live sessions</div>
            ${shown.map((name) => html`
                <div class="dkquietrow" key=${name}>
                    <span class="dkquietname">${name}</span>
                    <${ContourLimits} limits=${limits} name=${name} profiles=${profiles} />
                </div>
            `)}
        </div>
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

export function SessionColumn({ snapshot, profiles, limits, current, onPick, picks, setPicks, onNames, onOrder, exec, wait }) {
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

    const flat = useMemo(() => [...byProfile].flatMap(([, list]) => list), [byProfile]);

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
                ${[...byProfile].map(([profile, list]) => html`
                    <section key=${profile}>
                        <div class="dkcontour">
                            <span class="dkcontourname">${profile}</span>
                            <span class="dkcontournum">${list.length} live</span>
                            <${ContourLimits} limits=${limits} name=${profile} profiles=${map} />
                        </div>
                        ${list.map((s) => {
                            const found = s.project === undefined ? place(s.cwd) : s.project;
                            return html`<${SessionLine}
                                key=${s.session}
                                s=${s}
                                group=${found ? found.group : ""}
                                current=${current}
                                onPick=${onPick}
                                index=${flat.indexOf(s)}
                                exec=${exec}
                                wait=${wait}
                            />`;
                        })}
                    </section>
                `)}
                ${shown.length === 0 && ghosts.length === 0 && html`<p class="dkempty">there are no live sessions</p>`}
            </div>
            <${QuietLimits} limits=${limits} names=${names} picks=${picks} profiles=${map} live=${new Set(byProfile.keys())} />
        </aside>
    `;
}
