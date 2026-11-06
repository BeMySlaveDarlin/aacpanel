// A pause is one service action. The recipient list previews the same map
// scope; the service resolves it again when the person presses the button.
import { useState } from "preact/hooks";

import { html } from "../../html.js";
import { useToast } from "../../ui/toasts.js";
import { useAction } from "../../actions/gate.js";
import { knows, whyNot } from "../../exec.js";
import { shownName } from "../../agent.js";
import { openedBy } from "./kin.js";
import { sessionsOf } from "./of.js";

function groupOf(project, profile) {
    return (profile?.groups || []).find((g) => (g.projects || []).some((p) => p.id === project.id));
}

// pauseRows lists the live sessions a pause reaches: those of the project,
// of its whole group, or the one session it is given, and one step down the
// sessions any of them had the panel open, wherever those run, as the
// service takes them.
export function pauseRows(project, profile, sessions = [], group = false, only = null) {
    const reached = (s) => !s.outside && s.sessionId;
    const projects = group ? groupOf(project, profile)?.projects || [project] : [project];
    const scope = only ? [only].filter(reached) : sessions.filter((s) => reached(s) && projects.some((p) => sessionsOf(p, [s]).length));
    const names = new Set(scope.map((s) => s.session));
    return [...scope, ...sessions.filter((s) => reached(s) && !names.has(s.session) && names.has(openedBy(s)))];
}

export function pauseWhy(project, profile, sessions, exec) {
    return (knows(exec, "project.pause") ? "" : whyNot(exec, "project.pause")) || (pauseRows(project, profile, sessions, true).length ? "" : "No live sessions in this project or group");
}

function localInput(date) {
    const local = new Date(date.getTime() - date.getTimezoneOffset() * 60000);
    return local.toISOString().slice(0, 16);
}

// Carry the selected date's offset, so the instruction names the person's
// local clock even when the service runs in another timezone.
export function pauseUntil(value) {
    const date = new Date(value);
    if (!Number.isFinite(date.getTime())) return "";
    const offset = -date.getTimezoneOffset();
    const n = Math.abs(offset);
    return `${localInput(date)}:00${offset < 0 ? "-" : "+"}${String(Math.floor(n / 60)).padStart(2, "0")}:${String(n % 60).padStart(2, "0")}`;
}

// nextHour is the next o'clock of hour on the person's clock after now:
// today while it is still ahead, tomorrow once it has come.
export function nextHour(hour, now) {
    const at = new Date(now);
    at.setHours(hour, 0, 0, 0);
    if (at.getTime() <= now) at.setDate(at.getDate() + 1);
    return at;
}

// The quick choices of the end of a pause, under the word they stand by: a
// span from now, the next of a time of the day, or no end at all. A choice
// puts its moment into the field, and the field is what the request carries.
const NO_END = "No end";
const CHOICES = [
    ["For", [1, 2, 4, 6, 12].map((h) => [`${h} h`, (now) => new Date(now + h * 3600000)])],
    ...[["Until the morning", [8, 9, 10]], ["Until lunch", [12, 13, 14]], ["Until the evening", [17, 18, 19]]]
        .map(([word, hours]) => [word, hours.map((h) => [`${h}:00`, (now) => nextHour(h, now)])]),
    ["Until lifted", [[NO_END, null]]],
];

// PauseSheet pauses the live sessions of a project or of its group, or the
// one session it is opened from, until a time the person picks or with no
// end, until they lift it.
export function PauseSheet({ project, profile, sessions = [], session = null, exec, onDone }) {
    const run = useAction();
    const toast = useToast();
    const group = groupOf(project, profile);
    const [scope, setScope] = useState(session ? "session"
        : !pauseRows(project, profile, sessions).length && group ? "group" : "project");
    const whole = scope === "group";
    const [value, setValue] = useState(() => localInput(new Date(Date.now() + 3600000)));
    const [chosen, setChosen] = useState("1 h");
    const noEnd = chosen === NO_END;
    const [busy, setBusy] = useState(false);
    const [result, setResult] = useState(null);
    const rows = pauseRows(project, profile, sessions, whole, scope === "session" ? session : null);
    const paused = rows.filter((s) => s.pauseAlarm);
    const why = whyNot(exec, "project.pause");
    const at = new Date(value).getTime();
    const valid = noEnd || (Number.isFinite(at) && at > Date.now() && at <= Date.now() + 7 * 86400000);
    const send = async (resume) => {
        setBusy(true);
        const reply = await run("project.pause", project.session || project.name, {
            project: project.id, ...(scope === "session" ? { session: session.session } : { group: whole }),
            ...(resume ? { resume: true } : noEnd ? { noEnd: true } : { until: pauseUntil(value) }),
        });
        setBusy(false);
        if (!reply.ok) return;
        const data = reply.data;
        if (data?.failed?.length) { setResult(data); return; }
        toast(resume ? "Pause lifted" : "Pause requested", data?.detail);
        onDone();
    };
    return html`
        <div class="cmdsheet pausesheet">
            <div class="shead cmdtitle"><span class="cmdhead">Pause sessions</span></div>
            <p class="cmdnote">Sessions finish their current step, set their own wake-up alarm and wait. A busy session reads the request when its turn allows it.</p>
            <label class="cmdnote">Scope
                <select class="rnname cxfield pausescope" value=${scope} onChange=${(e) => { setScope(e.currentTarget.value); setResult(null); }}>
                    ${session && html`<option value="session">This session · ${shownName(session)}</option>`}
                    <option value="project">This project · ${project.name}</option>
                    ${group && html`<option value="group">Whole group · ${group.name}</option>`}
                </select>
            </label>
            <div class="pausepresets">
                ${CHOICES.map(([word, choices]) => html`
                    <div class="pausepreset" key=${word}>
                        <span class="pauselabel">${word}</span>
                        <div class="pausechips">${choices.map(([name, at]) => html`
                            <button key=${name} type="button" class="chip" aria-pressed=${chosen === name ? "true" : "false"}
                                    onClick=${() => { setChosen(name); setValue(at ? localInput(at(Date.now())) : ""); }}>${name}</button>`)}
                        </div>
                    </div>`)}
            </div>
            <label class="cmdnote">Until (your local time)
                <input class="rnname cxfield pauseuntil" type="datetime-local" value=${value} disabled=${noEnd}
                       onInput=${(e) => { setChosen(""); setValue(e.currentTarget.value); }} />
            </label>
            ${noEnd ? html`<p class="cmdnote pausenoend">No end: the sessions wait until the pause is lifted.</p>`
                : !valid && html`<p class="hint warn">Choose a time in the next seven days.</p>`}
            <p class="cmdnote">${rows.length} live ${rows.length === 1 ? "session will" : "sessions will"} receive the pause request:</p>
            <ul class="pauserecipients">${rows.map((s) => html`<li key=${s.sessionId}>${shownName(s)}${s.pauseAlarm ? " · paused" : ""}</li>`)}</ul>
            ${why && html`<p class="hint warn">${why}</p>`}
            ${result && html`<div class="pauseresult"><p>${result.detail}</p><ul>${result.failed.map((f) => html`<li>${f.session}: ${f.error}</li>`)}</ul></div>`}
            <div class="btnrow">
                ${paused.length > 0 && html`<button class="btn pauseresume" type="button" disabled=${busy || Boolean(why)} onClick=${() => send(true)}>Resume ${paused.length}</button>`}
                <button class="btn" type="button" onClick=${onDone}>Cancel</button>
                <button class="btn primary pauseapply" type="button" disabled=${busy || Boolean(why) || !rows.length || !valid} onClick=${() => send(false)}>${busy ? "Sending…" : "Pause"}</button>
            </div>
        </div>
    `;
}
