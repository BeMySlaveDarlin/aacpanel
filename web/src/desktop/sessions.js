// The left column of the sessions section: contour picker, live sessions, limits.
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
    const known = knows(exec, "session.close");
    const can = known && !closing;
    const answerable = Boolean(s.ask);

    return html`
        <button
            class=${`dksess${current === s.session ? " on" : ""}`}
            type="button"
            style=${`--fill:${Math.min(100, s.pct || 0)}%`}
            onClick=${() => onPick({ name: s.session, id: s.sessionId })}
        >
            <span class=${`dkdot dk${closing ? "off" : status} dkside`}></span>
            <span class="dksessbody">
                <span class="dksessmain">
                    <span class="dkname">${s.session}</span>
                    ${s.home && html`<span class="dktag">home</span>`}
                    <span class="dknum">${s.limitKnown === false ? "—" : pct(s.pct)}</span>
                    <span class="dkhint">${index < 9 ? index + 1 : ""}</span>
                </span>
                <span class="dksesssub">
                    ${group && html`<span class="dkgroup dkchip">${group}</span>`}
                    <span class="dklast">${closing ? `closing · ${held(closing)} s` : last(s)}</span>
                    ${waiting && !closing && html`
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
                            : known ? "Close the session" : whyNot(exec, "session.close")}
                        data-tipside="left"
                        onClick=${async () => {
                            if (!can) return;
                            await run("session.close", s.session, {});
                        }}
                    ><${Icon.stop} /></i>
                `}
            </span>
            <span class=${`dksessbar ${fill(s.pct || 0)}`}><i style=${`width:${Math.min(100, s.pct || 0)}%`}></i></span>
        </button>
    `;
}

// FooterLimits renders the subscription limits of the shown contours.
export function FooterLimits({ limits, names, picks, active, profiles }) {
    const known = names && names.length ? names : ((limits && limits.contours) || []).map((c) => c.profile);
    const shown = picks && picks.length ? known.filter((n) => picks.includes(n)) : known;
    if (shown.length === 0) return null;

    const focus = Boolean(active) && shown.includes(active);

    const resetIn = (part) => {
        const at = part && part.resetsAt;
        if (!at) return "";
        const left = at * 1000 - Date.now();
        if (left <= 0) return "any moment";
        const hours = Math.floor(left / 3600000);
        if (hours >= 48) return `${Math.round(hours / 24)} d`;
        if (hours >= 1) return `${hours} h`;
        return `${Math.max(1, Math.round(left / 60000))} min`;
    };

    const bar = (label, part) => {
        const value = Math.round((part && part.pct) || 0);
        const level = value >= 90 ? "dkcrit" : value >= 70 ? "dkwarn" : "";
        const left = resetIn(part);
        return html`
            <span class=${`dklimline ${level}`.trim()}>
                <span class="dklimlabel">${label}</span>
                <span class="dklimbar"><i style=${`width:${Math.min(100, value)}%`}></i></span>
                <span class=${`dklimpct ${level}`.trim()}>${value}%</span>
                <span
                    class="dklimreset"
                    data-tip=${left ? `The window resets in ${left}` : "When the window resets, the snapshot does not say"}
                    data-tipside="left"
                >${left || "—"}</span>
            </span>
        `;
    };

    return html`
        <div class=${`dklimits${focus ? " dkfocus" : ""}`}>
            ${shown.map((name) => {
                const id = ((profiles || []).find((p) => p.profile === name) || {}).id || 0;
                const c = contourOf(limits, name, id);
                const old = c && staleLimits(c);
                return html`
                    <div
                        class=${`dklimit${name === active ? " dkon" : ""}${old ? " dkold" : ""}`}
                        key=${name}
                        data-tip=${old ? `The numbers are ${agoText(c.ageSec)}: the snapshot is written by statusLine, and the contour has no sessions` : ""}
                        data-tipside="left"
                    >
                        <span class="dklimname">${name}</span>
                        ${c
                            ? html`
                                <span class="dklimbars">
                                    ${bar("5 h", c.fiveHour)}
                                    ${bar("7 d", c.sevenDay)}
                                </span>
                            `
                            : html`<span class="dklimnone">no numbers yet</span>`}
                    </div>
                `;
            })}
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

    const activeContour = useMemo(() => {
        const found = all.find((s) => s.session === current);
        return found ? pageOf(found) : "";
    }, [all, current]);
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
                        </div>
                        ${list.map((s) => {
                            const found = place(s.cwd);
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
            <${FooterLimits} limits=${limits} names=${names} picks=${picks} active=${activeContour} profiles=${map} />
        </aside>
    `;
}
