// Pause owns the clocks and recipients; Pie owns the scale, gesture and bar.
import { useRef, useState } from "preact/hooks";

import { html } from "../../html.js";
import { shownName } from "../../agent.js";
import { useAction } from "../../actions/gate.js";
import { knows, whyNot } from "../../exec.js";
import { Icon } from "../../ui/icons.js";
import { Pie } from "../../ui/pie.js";
import { useToast } from "../../ui/toasts.js";
import { groupOf, pauseAway, pauseDay, pauseEnd, pauseHour, pauseRows, pauseUntil } from "./pause.js";

const MINUTE = 60000;
const HOUR = 60 * MINUTE;
const DAY = 24 * HOUR;
const OTHER = { from: .715, to: .905 };
const NO_END = .962;

function dateAt(now, day, hour, minute = 0) {
    const at = new Date(now);
    at.setDate(at.getDate() + day);
    at.setHours(hour, minute, 0, 0);
    return at;
}

// The first section snaps relative to now; the rest snaps to wall-clock
// half-hours. Morning is strictly beyond the end of the first section.
export function todayScale(now) {
    const four = now + 4 * HOUR;
    const morning = dateAt(now, 0, 9);
    while (morning.getTime() <= four) morning.setDate(morning.getDate() + 1);
    const fraction = (at) => at <= four ? .42 * (at - now) / (4 * HOUR) : .42 + .27 * (at - four) / (morning.getTime() - four);
    const stop = (at) => ({ at: fraction(at), value: at, label: pauseEnd(at, { full: true, distance: true, now }) });
    const near = [];
    for (let n = 10; n <= 240; n += 10) near.push(stop(now + n * MINUTE));
    const far = [];
    const next = new Date(four);
    next.setSeconds(0, 0);
    next.setMinutes(Math.floor(next.getMinutes() / 30) * 30 + 30);
    for (let at = next.getTime(); at <= morning.getTime(); at += 30 * MINUTE) far.push(stop(at));
    const first = new Date(now);
    first.setHours(first.getHours() + 1, 0, 0, 0);
    const ticks = [];
    const words = [{ at: .035, word: "now", inner: true }];
    for (let at = first.getTime(), n = 0; at <= morning.getTime(); at += HOUR, n += 1) {
        const date = new Date(at);
        const last = at === morning.getTime();
        const named = n < 2 || date.getHours() === 18 || date.getHours() === 22 || last;
        ticks.push({ at: fraction(at), major: named });
        if (named) words.push({ at: fraction(at), word: pauseHour(date), icon: last ? Icon.sun : null });
    }
    const noEnd = { at: NO_END, value: null, label: "until lifted" };
    words.push({ at: NO_END, word: "no end", icon: Icon.pause, inner: true });
    return { segments: [{ from: 0, to: .42, stops: near }, { from: .42, to: .715, stops: far },
        { from: .905, to: 1, stops: [noEnd] }], ticks, words, fraction };
}

export function dayScale(now, day) {
    const chosen = day != null;
    const date = day || 1;
    const limit = now + 7 * DAY;
    const stops = [];
    const ticks = [];
    const words = [];
    for (let min = 7 * 60; min <= 23 * 60; min += 30) {
        const value = dateAt(now, date, 0, min).getTime();
        const at = (min / 60 - 7) / 16;
        const off = !chosen || value > limit;
        stops.push({ at, value, off, label: pauseEnd(value, { full: true, distance: true, now }) });
        if (min % 60) continue;
        const hour = min / 60;
        ticks.push({ at, off, major: hour % 2 === 0 });
        if (hour % 2 === 0) words.push({ at, word: `${hour}:00`, off, icon: hour === 8 ? Icon.sun : hour === 18 ? Icon.moon : null });
    }
    return { segments: [{ from: 0, to: 1, stops }], ticks, words };
}

const namesOf = (rows) => rows.map(shownName).join(", ");
const joinedNames = (rows) => {
    const names = rows.map(shownName);
    return names.length < 2 ? names[0] || "" : `${names.slice(0, -1).join(", ")} and ${names.at(-1)}`;
};

export function usePauseDial({ project, profile, sessions = [], session = null, exec, onClose, initial = false }) {
    const run = useAction();
    const toast = useToast();
    const [now] = useState(() => Date.now());
    const [active, setActive] = useState(initial);
    const opened = session && project ? pauseRows(project, profile, sessions, false, session).slice(1) : [];
    const sleepingOpened = opened.filter((row) => row.pauseAlarm);
    const [view, setView] = useState(Boolean(session?.pauseAlarm));
    const [moving, setMoving] = useState(false);
    const [withOpened, setWithOpened] = useState(Boolean(session?.pauseAlarm) && sleepingOpened.length > 0);
    const [whole, setWhole] = useState(false);
    const [days, setDays] = useState(false);
    const [day, setDay] = useState(null);
    const [selected, setSelected] = useState(null);
    const [busy, setBusy] = useState(false);
    const sending = useRef(false);
    const [failure, setFailure] = useState("");
    const group = project ? groupOf(project, profile) : null;
    const toggled = view ? sleepingOpened : opened;
    const rows = project ? pauseRows(project, profile, sessions, whole, session, session ? !withOpened : false) : [];
    const title = session ? `${shownName(session)}${withOpened && toggled.length ? ` + ${toggled.length}` : ""}`
        : whole && group ? `group ${group.name}` : project?.name || "";
    const scope = () => ({ project: project.id, ...(session ? { session: session.session, alone: !withOpened } : { group: whole }) });
    const why = knows(exec, "project.pause") ? "" : whyNot(exec, "project.pause");
    const valid = selected && (selected.value == null || (selected.value > Date.now() && selected.value <= Date.now() + 7 * DAY));
    const send = async (resume = false) => {
        if (sending.current || why || !rows.length || (!resume && !valid)) return;
        sending.current = true;
        setBusy(true);
        const params = scope();
        const target = project.session || project.name;
        const reply = await run("project.pause", target, { ...params,
            ...(resume ? { resume: true } : selected.value == null ? { noEnd: true } : { until: pauseUntil(selected.value) }) });
        sending.current = false;
        setBusy(false);
        if (!reply.ok) return;
        if (reply.data?.failed?.length) {
            setFailure(reply.data.failed.map((f) => `${f.session}: ${f.error}`).join("; "));
            return;
        }
        toast(resume ? "Pause lifted" : "Pause requested", reply.data?.detail, false,
            resume ? null : { label: "Undo", run: () => run("project.pause", target, { ...params, resume: true }) });
        onClose();
    };
    const dial = days ? dayScale(now, day) : todayScale(now);
    const end = session?.pauseAlarm;
    const until = end && end.name !== "pause-no-end" ? Date.parse(end.next || "") || null : null;
    const markAt = !view ? 0 : !until ? NO_END : new Date(until).toDateString() !== new Date(now).toDateString() || dial.fraction(until) > .69
        ? (OTHER.from + OTHER.to) / 2 : Math.max(0, dial.fraction(until));
    const mark = view ? { at: markAt, label: pauseEnd(until, { now }) } : null;
    const markOther = mark && markAt === (OTHER.from + OTHER.to) / 2;
    const zones = days ? [] : [{ ...OTHER, label: "Another day", pick: () => { setDays(true); setDay(null); setSelected(null); } }];
    const words = [...dial.words, ...(!days ? [{ at: (OTHER.from + OTHER.to) / 2,
        word: markOther ? pauseEnd(until, { now }).replace("until ", "") : html`another<br />day`,
        icon: markOther ? null : Icon.clock, inner: true }] : [])];
    const note = view ? sleepingOpened.length ? `${joinedNames(sleepingOpened)} sleep with it` : "Continue it, or change when its pause ends"
        : `${joinedNames(rows)} ${rows.length > 1 ? "finish their step" : "finishes its step"}, then ${rows.length > 1 ? "sleep" : "sleeps"}`;
    const crown = active ? { ...dial, words, zones, label: days ? "Hour of the selected day" : "End of the pause",
        off: view || (days && day == null), value: selected, mark,
        change: setSelected,
        read: (value, zone) => [view ? until ? `Sleeps ${pauseEnd(until, { now })} · ${pauseAway(until, now)} left` : "Sleeps until lifted"
            : value ? value.label : zone ? "Another day" : days ? day ? `Pick an hour for ${pauseDay(dateAt(now, day, 0))}` : "Pick a day, then its hour"
                : `Pause ${title}`, failure || note] } : null;
    const toggle = session && toggled.length > 0 && html`
        <button type="button" class="nfrow pausetoggle" role="switch" aria-checked=${String(withOpened)}
                onClick=${() => setWithOpened(!withOpened)}>
            <span class="nfsw"></span><span class="pausetogglewords"><b>${view ? `Wake ${toggled.length} too` : `With ${toggled.length} opened`}</b>
                <small>${namesOf(toggled)}</small></span>
        </button>`;
    const bar = active ? html`${toggle}
        ${view && html`<button type="button" class="btn pausechange" onClick=${() => { setView(false); setMoving(true); setSelected(null); }}>Change the end</button>`}
        <button type="button" class="btn primary pauseapply" data-primary disabled=${busy || Boolean(why) || !rows.length || (!view && !valid)}
                onClick=${() => send(view)}>
            <b>${busy ? "Sending…" : view ? "Continue" : !selected ? "Pick when it ends" : `${moving ? "Move the end of" : "Pause"} ${title}`}</b>
            ${view ? html`<small>${title}</small>` : selected && html`<small>${pauseEnd(selected.value, { now })}</small>`}
        </button>` : null;
    const layer = days ? { key: "days", className: "pausedays", hubLabel: "Back to the dial", hubWord: "to the dial",
        back: () => { setDays(false); setDay(null); setSelected(null); },
        items: [...Array.from({ length: 7 }, (_, i) => {
            const n = i + 1;
            const date = dateAt(now, n, 0);
            return { key: `day-${n}`, icon: () => null, word: pauseDay(date), lines: [pauseDay(date), html`<b>${date.getDate()}</b>`],
                title: `${pauseDay(date)} ${date.getDate()}`, stay: true,
                stand: () => { if (day !== n) { setDay(n); setSelected(null); } } };
        }), { key: "next-week", icon: Icon.clock, word: "Next week", title: "Next week", short: "over 7 d", off: "over 7 d" }] } : null;
    const scopes = project ? [false, ...(group?.projects.length > 1 ? [true] : [])].map((grouped) => {
        const list = pauseRows(project, profile, sessions, grouped);
        return { key: grouped ? "group" : "project", icon: grouped ? Icon.flow : Icon.pause,
            word: grouped ? `group ${group.name} · ${list.length}` : `${project.name} · ${list.length}`,
            title: grouped ? `Pause group ${group.name}` : `Pause project ${project.name}`,
            off: !list.length ? "No sessions in this scope" : why, stay: true, stand: () => setWhole(grouped) };
    }) : [];
    const paused = rows.filter((row) => row.pauseAlarm).length;
    if (paused) scopes.push({ key: "continue", icon: Icon.play, word: `Continue ${paused}`, title: `Continue ${paused}`, off: why,
        stay: true, pick: () => send(true) });
    return { activate: () => setActive(true), crown, bar, layer, scopes };
}

// The project heading and desktop menus open the same dial as the card.
export function PausePie({ project, profile, sessions, session = null, exec, spot = null, onClose }) {
    const pause = usePauseDial({ project, profile, sessions, session, exec, onClose, initial: true });
    return html`<${Pie} label="pause sessions" name=${session ? shownName(session) : project.name}
        items=${session ? [{ key: "pause", icon: session.pauseAlarm ? Icon.play : Icon.pause,
            word: session.pauseAlarm ? "Continue" : "Pause", title: session.pauseAlarm ? "Continue" : "Pause", stay: true }] : pause.scopes}
        crown=${pause.crown} bar=${pause.bar} layer=${pause.layer} spot=${spot} onClose=${onClose} />`;
}
