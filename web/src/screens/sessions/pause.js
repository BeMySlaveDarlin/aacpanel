// A pause is one service action. The recipient list previews the same map
// scope; the service resolves it again when the person presses the button.
import { useState } from "preact/hooks";

import { html } from "../../html.js";
import { Icon } from "../../ui/icons.js";
import { Sheet } from "../../ui/sheet.js";
import { useToast } from "../../ui/toasts.js";
import { useAction } from "../../actions/gate.js";
import { knows, whyNot } from "../../exec.js";
import { shownName } from "../../agent.js";
import { sessionsOf } from "./of.js";

function groupOf(project, profile) {
    return (profile?.groups || []).find((g) => (g.projects || []).some((p) => p.id === project.id));
}

export function pauseRows(project, profile, sessions = [], group = false) {
    const projects = group ? groupOf(project, profile)?.projects || [project] : [project];
    return sessions.filter((s) => !s.outside && s.sessionId && projects.some((p) => sessionsOf(p, [s]).length));
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

export function PauseSheet({ project, profile, sessions = [], exec, onDone }) {
    const run = useAction();
    const toast = useToast();
    const group = groupOf(project, profile);
    const [whole, setWhole] = useState(!pauseRows(project, profile, sessions).length && Boolean(group));
    const [value, setValue] = useState(() => localInput(new Date(Date.now() + 3600000)));
    const [busy, setBusy] = useState(false);
    const [result, setResult] = useState(null);
    const rows = pauseRows(project, profile, sessions, whole);
    const paused = rows.filter((s) => s.pauseAlarm);
    const why = whyNot(exec, "project.pause");
    const at = new Date(value).getTime();
    const valid = Number.isFinite(at) && at > Date.now() && at <= Date.now() + 7 * 86400000;
    const send = async (resume) => {
        setBusy(true);
        const reply = await run("project.pause", project.session || project.name, {
            project: project.id, group: whole, ...(resume ? { resume: true } : { until: pauseUntil(value) }),
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
                <select class="rnname cxfield pausescope" value=${whole ? "group" : "project"} onChange=${(e) => { setWhole(e.currentTarget.value === "group"); setResult(null); }}>
                    <option value="project">This project · ${project.name}</option>
                    ${group && html`<option value="group">Whole group · ${group.name}</option>`}
                </select>
            </label>
            <div class="pausepresets">
                ${[[30, "+30 min"], [60, "+1 h"], [120, "+2 h"], [240, "+4 h"]].map(([minutes, label]) => html`
                    <button type="button" class="btn" onClick=${() => setValue(localInput(new Date(Date.now() + minutes * 60000)))}>${label}</button>`)}
            </div>
            <label class="cmdnote">Until (your local time)
                <input class="rnname cxfield pauseuntil" type="datetime-local" value=${value} onInput=${(e) => setValue(e.currentTarget.value)} />
            </label>
            ${!valid && html`<p class="hint warn">Choose a time in the next seven days.</p>`}
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

export function PauseControl({ project, profile, sessions, exec, desktop = false }) {
    const [open, setOpen] = useState(false);
    const why = pauseWhy(project, profile, sessions, exec);
    return html`
        <button class=${desktop ? "dkprojpause" : "pjpause"} type="button" aria-disabled=${why ? "true" : undefined}
                aria-label=${`pause sessions in project ${project.name}`} title=${why || "Pause sessions…"}
                onClick=${() => !why && setOpen(true)}><${Icon.pause} /></button>
        ${open && html`<${Sheet} open tall onClose=${() => setOpen(false)} label="pause sessions">
            <${PauseSheet} project=${project} profile=${profile} sessions=${sessions} exec=${exec} onDone=${() => setOpen(false)} />
        <//>`}
    `;
}
