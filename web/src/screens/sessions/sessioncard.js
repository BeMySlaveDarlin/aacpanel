// The card of a session, one skeleton of five lines for every state and for
// both the phone and the desk, an archived conversation included: a card
// that changed its height with its state would move the list under the eye.
//
//   1  the glyph of the state, the name, and the controls at the right;
//   2  the state in a word or the subject of the wait, what runs now right
//      after it, and the collector's note;
//   3  the step of the checklist in hand and n/N, over a bar a segment a step;
//   4  who runs it on which model, what queued and compacted, and how long
//      the state has lasted — or when it ends;
//   5  its settings, each in a place of its own whether on or off, and how
//      full its context is; the context bar runs along the bottom edge.
//
// The controls are quiet icons that are always there: no hover shows them,
// a phone has none and the desk would hide them from the eye that looks for
// them.

import { useState } from "preact/hooks";

import { html } from "../../html.js";
import { Icon } from "../../ui/icons.js";
import { Sheet } from "../../ui/sheet.js";
import { alarmTime, fill, tokens } from "../../format.js";
import { useAction } from "../../actions/gate.js";
import { knows, whyNot } from "../../exec.js";
import { CODEX_CLOSE, CODEX_NOTE, CODEX_RESTART, agentKey, agentName, codexRestartWhy, codexResume, isCodex, shownName } from "../../agent.js";
import { checklistOf } from "../chat/checklist.js";
import { modelTitle, stopsOf } from "../chat/head.js";
import { effortName } from "../chat/picker.js";
import { placeOf } from "./kin.js";
import { stamp } from "./card.js";
import { pinOf } from "./pin.js";
import { PauseSheet, pauseWhy } from "./pause.js";
import { runsOf, shortText, stateOf, timeOf } from "./state.js";

// The glyph of each kind of state; a kind with none draws a shape of its own
// (a dot at work, a ring for the background, a hollow ring for a closed one).
const GLYPHS = {
    ask: Icon.ask, question: Icon.ask, held: Icon.ask, permission: Icon.lock, request: Icon.hand, brief: Icon.plan,
    secret: Icon.key, call: Icon.alerts, input: Icon.hourglass, compact: Icon.compact, saving: Icon.refresh,
    restarting: Icon.refresh, limit: Icon.limit, paused: Icon.pause, done: Icon.check,
};

// The short words of the modes, each fitting the place the mode keeps.
const MODES = {
    default: "Manual", acceptEdits: "Edits", plan: "Plan", auto: "Auto", bypassPermissions: "Bypass", dontAsk: "No ask",
};

// Glyph is the mark of a state, drawn beside its word.
export function Glyph({ state }) {
    const icon = GLYPHS[state.kind];
    return html`
        <span class="scglyph" data-tone=${state.tone} data-kind=${state.kind} data-unseen=${state.unseen ? "" : undefined} aria-hidden="true">
            ${icon ? icon() : html`<i class="scshape"></i>`}
        </span>
    `;
}

// nameOf parts the name of a card into what the line around it already says
// and what is its own. A card under the heading of its project, or under the
// session that opened it, drops what it shares with them; a card that stands
// for a project of the map alone shows the project as a quiet prefix to its
// own part — a session no project holds is its name alone.
export function nameOf(name, { known = "", project = null } = {}) {
    const sep = (root) => (root && name.length > root.length && name.startsWith(root)
        && /[-_.]/.test(name[root.length]) ? root.length + 1 : 0);
    if (project && project.id) {
        const root = project.session || project.name || "";
        const at = sep(root);
        if (at) return { pre: name.slice(0, at), own: name.slice(at) };
        if (name === root || name === project.name) return { pre: "", own: name };
        return { pre: `${project.name} · `, own: name };
    }
    const at = sep(known);
    return { pre: "", own: at ? name.slice(at) : name };
}

// Name is the name of a card in its two parts, whole for a reader, with the
// address of the session in its tip where the name read is another — a
// thread of codex reads by its title; tag is a quiet word after it — who
// opened a session that stands away from its parent, the contour of a closed
// one on a shelf of several.
function Name({ full, part, tag = "", address = full }) {
    return html`
        <span class="scname" title=${address !== full ? address : undefined} aria-label=${full}>
            ${part.pre && html`<span class="scpre" aria-hidden="true">${part.pre}</span>`}<span class="scown" aria-hidden="true">${part.own}</span>
            ${tag && html`<span class="sctag">${tag}</span>`}
        </span>
    `;
}

// Line is the state's line: the word in the colour of the state, the subject
// of a wait in the ink, a command in mono; then what runs now, only what
// runs, the wake the session has ahead, and the note of the collector.
function Line({ state, runs, alarm = null, notes, busy }) {
    return html`
        <div class="scl2">
            <span class="scsay">
                ${busy ? html`<span class="scbusy" role="status"><span class="spin"></span>${busy}</span>` : html`
                    <span class="scword">${state.word}</span>
                    ${state.subject && html` <b class="scsubj">${state.subject}</b>`}
                    ${state.mono && html` <code class="scmono">${state.mono}</code>`}
                    ${state.note && html`<span class="scaside"> · ${state.note}</span>`}
                `}
            </span>
            ${runs && runs.agents > 0 && html`<span class="scrun" title=${`${runs.agents} subagents running`}>${Icon.robot()}<b>${runs.agents}</b></span>`}
            ${runs && runs.commands > 0 && html`<span class="scrun" title=${`${runs.commands} background commands running`}>${Icon.terminal()}<b>${runs.commands}</b></span>`}
            ${runs && runs.watchers > 0 && html`<span class="scrun" title=${`${runs.watchers} observers watching`}>${Icon.eye()}<b>${runs.watchers}</b></span>`}
            ${alarm && html`<span class="scalarm" title=${`${alarm.name} · ${alarmTime(alarm.next, true)}`}>${Icon.clock()}${alarmTime(alarm.next)}</span>`}
            ${notes && notes.length > 0 && html`<span class="sccollector" title=${notes.join("\n")}>${notes.join(" · ")}</span>`}
        </div>
    `;
}

// wakeOf is the alarm a session has ahead that is not the end of its pause:
// the end of a pause stands in the corner of a paused card.
function wakeOf(s) {
    const next = s.nextAlarm;
    return next && next.next && next.id !== (s.pauseAlarm && s.pauseAlarm.id) ? next : null;
}

// Steps is the checklist: the step in hand, wrapped rather than cut, and
// n/N; under it a segment a step over the whole width — done in grey, the
// one at work in the colour of the state, those ahead dark. A list with
// every step behind it is done, and its bar is whole. With no checklist the
// line says so, or what stands in for it — what an archived conversation was
// about — over an empty bar.
function Steps({ step = "", n, total, segs, over }) {
    if (!total) {
        return html`
            <div class="scl3"><span class=${`scstep${step ? "" : " scnone"}`}>${step || "no checklist"}</span></div>
            <div class="scsegs scempty" aria-hidden="true"><i></i></div>
        `;
    }
    return html`
        <div class="scl3"><span class="scstep" title=${step}>${step}</span><span class=${`scn${over ? " scover" : ""}`}><b>${n}</b>/${total}</span></div>
        <div class=${`scsegs${over ? " scover" : ""}`} aria-hidden="true">${segs.map((cls, i) => html`<i key=${i} class=${cls}></i>`)}</div>
    `;
}

// stepsOf reads a checklist for the card: the step in hand — the last one
// done for a list that is over — and a class a segment.
export function stepsOf(checklist) {
    const p = checklistOf(checklist);
    if (!p) return { total: 0 };
    const working = !p.over && p.items[p.current].status === "active";
    return {
        total: p.total,
        n: p.n,
        over: p.over,
        step: p.over ? (p.items.filter((item) => item.status === "done").pop() || p.items[p.items.length - 1]).text : p.step,
        segs: p.items.map((item, i) => (item.status === "done" || item.status === "dropped" ? "scd"
            : working && i === p.current ? "sca" : "")),
    };
}

// pastSteps reads the last checklist an archived conversation kept, as its
// archive entry carries it: the step it stood at and how far it got.
function pastSteps(last, about) {
    const total = (last && last.total) || 0;
    if (!total) return { total: 0, step: about };
    const done = Math.min(total, last.done || 0);
    return {
        total, over: done >= total, n: Math.min(total, done + (done < total ? 1 : 0)), step: last.step || about || "",
        segs: Array.from({ length: total }, (_, i) => (i < done ? "scd" : "")),
    };
}

// Agent is who runs the session, in the hue of its agent, on which model at
// which effort; then what waits in its queue and how often it was compacted,
// in words — for a closed thread of codex, what it spent; and the time of
// its state at the right.
function Agent({ who, model, effort, queued = 0, compacts = 0, spent = "", time, tone }) {
    return html`
        <div class="scl4">
            <span class="scagent"><span class="agentword" data-agent=${agentKey(who)} data-tip=${isCodex(who) ? CODEX_NOTE : undefined}>${agentName(who)}</span>${model && html` ${model}`}${effort && html` <span class="sceffort">${effort}</span>`}</span>
            ${spent && html`<span class="scmore">${spent}</span>`}
            ${queued > 0 && html`<span class="scmore">${queued} queued</span>`}
            ${compacts > 0 && html`<span class="scmore">${compacts} ${compacts === 1 ? "compaction" : "compactions"}</span>`}
            <span class="sctime" data-tone=${tone}>${time.until && html`<i class="scarrow">→</i>`}${time.text}</span>
        </div>
    `;
}

function modelOf(id) {
    return id ? modelTitle(id, { withWindow: false }) : "";
}

// Badges are the settings of a session in fixed places, an off one dimmed in
// its place: the mode, where it lives, its cap of the context and whether it
// restarts at it, Remote Control and a worktree; at the right how full the
// context is. A card that kept no settings says nothing of its cap.
function Badges({ mode, place, cap, auto, remote, worktree, ctx, known = true }) {
    const capped = cap != null && cap > 0 && cap <= 100;
    return html`
        <div class="scl5">
            <span class=${`scb${mode ? "" : " scoff"}`}>${mode ? MODES[mode] || mode.charAt(0).toUpperCase() + mode.slice(1) : "—"}</span>
            <span class=${`scb${place ? "" : " scoff"}`}>${place || "—"}</span>
            ${capped && auto
                ? html`<span class="scb scon" title=${`At ${cap}% of the context it saves its work and restarts`}>${Icon.refresh()}at ${cap}%</span>`
                : known ? html`<span class="scb">${capped ? `cap ${cap}%` : "no cap"}</span>`
                : html`<span class="scb scoff">—</span>`}
            <span class=${`scb${remote ? " scon" : " scoff"}`} title=${remote ? "Remote Control is on: the session is open on claude.ai too" : undefined}>RC</span>
            <span class=${`scb${worktree ? " scon" : " scoff"}`} title=${worktree ? `A worktree of ${worktree}` : undefined}>worktree</span>
            <span class="scctx">${ctx}</span>
        </div>
    `;
}

// Edge is the context bar along the bottom of the card, of the whole window,
// with the cap marked on it: in the accent where the session restarts there.
// What lies past the cap is the share the session never reaches, set apart.
function Edge({ pct, cap, auto, past = false }) {
    const capped = !past && cap != null && cap > 0 && cap < 100;
    return html`
        <span class=${`scedge ${past ? "scpeak" : fill(pct || 0)}`} aria-hidden="true">
            ${pct != null && html`<i style=${`width:${Math.max(0, Math.min(100, pct))}%`}></i>`}
            ${capped && html`<s class="scblock" style=${`left:${cap}%`}></s>`}
            ${capped && html`<b class=${`sccap${auto ? " scauto" : ""}`} style=${`left:${cap}%`}></b>`}
        </span>
    `;
}

// menuOn opens the menu of a card from a right click anywhere on it, the
// focus going back to the card's way into its conversation as the menu closes.
function menuOn(onMenu) {
    return onMenu ? (event) => onMenu(event, event.currentTarget.querySelector(".scopen")) : undefined;
}

// Act is one control of a card: an icon, its name for a reader and its tip;
// on paints it in the accent, pressed says a toggle's state to a reader and
// busy that its press is on its way. One that cannot be pressed now stays in
// its place, dimmed, and says why.
function Act({ label, tip, off, on = false, pressed, busy = false, onPress, children }) {
    return html`
        <button class=${`scact${on ? " scon" : ""}`} type="button" aria-label=${label} title=${tip || undefined}
                data-tip=${tip || undefined} data-tipside="left" aria-disabled=${off ? "true" : undefined}
                aria-pressed=${pressed === undefined ? undefined : String(pressed)} aria-busy=${busy ? "true" : undefined}
                onClick=${(e) => { e.stopPropagation(); if (!off) onPress(); }}>${children}</button>
    `;
}

// Pause is the pause of one session: it opens the sheet of the pause with
// the session as its scope, and on a paused card it is the continue.
function Pause({ s, project, profile, sessions, exec }) {
    const run = useAction();
    const [open, setOpen] = useState(false);
    const why = !project ? "No project of the map holds this session"
        : (knows(exec, "project.pause") ? "" : whyNot(exec, "project.pause")) || (s.sessionId ? "" : "The session has no conversation yet");
    if (s.pauseAlarm) {
        return html`
            <${Act} label=${`continue session ${s.session}`} tip=${why || "Continue now"} off=${why} on
                    onPress=${() => run("project.pause", project.session || project.name, { project: project.id, session: s.session, resume: true })}>
                ${Icon.play()}
            <//>
        `;
    }
    return html`
        <${Act} label=${`pause session ${s.session}`} tip=${why || "Pause…"} off=${why} onPress=${() => setOpen(true)}>${Icon.pause()}<//>
        ${open && html`<${Sheet} open tall onClose=${() => setOpen(false)} label="pause sessions">
            <${PauseSheet} project=${project} profile=${profile} sessions=${sessions} session=${s} exec=${exec} onDone=${() => setOpen(false)} />
        <//>`}
    `;
}

// Controls are the controls of a live card: a new session in its project on
// a card that stands for the project alone, pause, restart, pin and close.
// A session the panel did not start is only read; the home session is never
// closed; a codex in tmux restarts from its terminal.
function Controls({ s, solo, project, profile, sessions, exec, pin, busy }) {
    const run = useAction();
    if (s.outside) return null;
    const codex = isCodex(s);
    const restartWhy = busy ? "the session is already closing or restarting"
        : (codex ? codexRestartWhy(s) : "") || (knows(exec, "session.restart") ? "" : whyNot(exec, "session.restart"));
    const closeWhy = busy ? "the session is already closing or restarting"
        : knows(exec, "session.close") ? "" : whyNot(exec, "session.close");
    const lost = stopsOf(s);
    const ends = (note) => (lost ? `stops ${lost} · ${note}` : note);
    const canNew = solo && project && !s.home;
    const newWhy = knows(exec, "session.open") ? "" : whyNot(exec, "session.open");
    const pinWhy = pin.why || (knows(exec, "session.pin") ? "" : whyNot(exec, "session.pin"));
    return html`
        <span class="scacts">
            ${canNew && html`
                <${Act} label=${`new session in project ${project.name}`} tip=${newWhy || `A new session in ${project.name}`} off=${newWhy}
                        onPress=${() => run("session.open", project.session || project.name, { project: project.id })}>${Icon.plus()}<//>
            `}
            <${Pause} s=${s} project=${project} profile=${profile} sessions=${sessions} exec=${exec} />
            <${Act} label=${`restart session ${s.session}`} off=${restartWhy}
                    tip=${restartWhy || (codex ? CODEX_RESTART : ends("a new session with an empty context; this one stays in the archive"))}
                    onPress=${() => run("session.restart", s.session, {})}>${Icon.refresh()}<//>
            <${Act} label=${`${pin.on ? "unpin" : "pin"} session group ${pin.root.session}`} off=${pinWhy}
                    tip=${pinWhy || pin.tip} on=${pin.on} pressed=${pin.on} busy=${pin.pending}
                    onPress=${() => run("session.pin", pin.root.session, { on: !pin.on })}>${Icon.thumbtack()}<//>
            ${!s.home && html`
                <${Act} label=${`close session ${s.session}`} off=${closeWhy}
                        tip=${closeWhy || (codex ? CODEX_CLOSE : ends("the conversation stays in the archive"))}
                        onPress=${() => run("session.close", s.session, {})}>${Icon.close()}<//>
            `}
        </span>
    `;
}

// SessionCard is a live session. solo is the project the card stands for
// alone, with no heading of its own over it; known is the name a heading or
// the opening session above it says already. depth is how many steps in the
// card stands under the sessions that opened it; from names a parent that
// stands elsewhere. A press anywhere on the card opens the conversation;
// onMenu is the menu of a right click, where there is one; current marks
// the conversation open beside the list.
export function SessionCard({ s, limits, wait, exec, sessions = [], profile = null, notes = null, solo = null, known = "",
    depth = 0, from = "", current = false, desk = false, onOpen, onMenu }) {
    const closing = wait ? wait.of("close", s.session) : null;
    const restarting = wait ? wait.of("restart", s.session) : null;
    const busy = closing ? "closing" : restarting ? "restarting" : "";
    const state = stateOf(s, limits, { viewed: current });
    const steps = stepsOf(s.checklist);
    const project = solo && solo.id ? solo : s.project && s.project.id ? s.project : null;
    const name = shownName(s);
    const part = nameOf(name, solo ? { project: solo } : { known });
    const pin = pinOf(s, sessions.length ? sessions : [s], wait);
    const time = timeOf(state);
    const timeTone = state.tone === "wait" ? "wait" : state.unseen ? "unseen" : ["idle", "paused"].includes(state.tone) ? "quiet" : "";
    return html`
        <div class=${`sc${state.tone === "wait" ? " scwait" : ""}${depth ? " sckid" : ""}${current ? " sccur" : ""}${desk ? " scdesk" : ""}`}
             data-name=${s.session} data-tone=${state.tone} data-kind=${state.kind} style=${depth ? `--depth:${depth}` : undefined}
             onContextMenu=${menuOn(onMenu)}>
            <button class="scopen" type="button" aria-label=${`open conversation ${s.session}`}
                    onClick=${() => onOpen && onOpen(s)}></button>
            <${Glyph} state=${state} />
            <div class="scl1">
                <${Name} full=${name} address=${s.session} part=${part} tag=${from ? `opened by ${from}` : ""} />
                <${Controls} s=${s} solo=${Boolean(solo)} project=${project} profile=${profile} sessions=${sessions}
                             exec=${exec} pin=${pin} busy=${Boolean(busy || s.restarting)} />
            </div>
            <${Line} state=${state} runs=${runsOf(s.work)} alarm=${wakeOf(s)} notes=${notes} busy=${busy} />
            <${Steps} ...${steps} />
            <${Agent} who=${s} model=${modelOf(s.model)} effort=${s.effort ? effortName(s.effort) : ""}
                      queued=${s.queued || 0} compacts=${s.compacts || 0} time=${time} tone=${timeTone} />
            <${Badges} mode=${s.mode || ""} place=${placeOf(s)} cap=${s.cap} auto=${s.autoRestart} remote=${Boolean(s.remote)}
                       worktree=${s.checkout || ""} ctx=${s.noRequests ? "—" : `${Math.round(s.pct || 0)}%`} />
            <${Edge} pct=${s.noRequests ? null : s.pct || 0} cap=${s.cap} auto=${s.autoRestart} />
        </div>
    `;
}

// PastCard is an archived conversation on the same five lines, dimmed: what
// its checklist stood at when it closed — or what the person last said in
// it — who ran it on what, its settings as they were, and how full it got —
// a thread of codex keeps no share of its context, and says what it spent;
// Resume where the live card has its controls. contour names the contour of
// a card on a shelf of several.
export function PastCard({ row, about = "", exec, solo = null, known = "", contour = "", current = false, desk = false, onOpen, onMenu }) {
    const run = useAction();
    const last = row.last || {};
    const who = { agent: last.agent || row.agent };
    const steps = pastSteps(row.last, about ? `«${about}»` : "");
    const ready = knows(exec, "session.resume");
    const lastAt = stamp(row.lastAt);
    const part = nameOf(row.name, solo ? { project: solo } : { known });
    const codex = isCodex(who);
    const place = last.transport || (codex ? "daemon" : row.transport || "");
    return html`
        <div class=${`sc scpast${current ? " sccur" : ""}${desk ? " scdesk" : ""}`} data-name=${row.name} data-tone="closed" data-kind="closed"
             onContextMenu=${menuOn(onMenu)}>
            <button class="scopen" type="button" aria-label=${`open conversation ${row.name}`}
                    onClick=${() => onOpen && onOpen(row)}></button>
            <span class="scglyph" data-tone="closed" data-kind="closed" aria-hidden="true"><i class="scshape"></i></span>
            <div class="scl1">
                <${Name} full=${row.name} part=${part} tag=${contour} />
                ${row.sessionId && html`
                    <span class="scacts">
                        <button class="scresume" type="button" aria-label=${`resume conversation of ${row.name}`}
                                aria-disabled=${ready ? undefined : "true"} title=${ready ? undefined : whyNot(exec, "session.resume")}
                                data-tip=${ready ? undefined : whyNot(exec, "session.resume")} data-tipside="left"
                                onClick=${(e) => { e.stopPropagation(); if (ready) run("session.resume", row.name, { session: row.sessionId, ...codexResume(row) }); }}>Resume</button>
                    </span>
                `}
            </div>
            <div class="scl2"><span class="scsay"><span class="scword">closed</span></span></div>
            <${Steps} ...${steps} />
            <${Agent} who=${who} model=${modelOf(last.model || row.model)} effort=${last.effort || row.effort ? effortName(last.effort || row.effort) : ""}
                      spent=${codex && row.tokensUsed ? `${tokens(row.tokensUsed)} tokens` : ""}
                      time=${{ until: false, text: lastAt ? shortText(Date.now() / 1000 - lastAt) : "" }} tone="quiet" />
            <${Badges} mode=${last.mode || row.mode || ""} place=${place} cap=${last.cap} auto=${false} remote=${Boolean(last.remote)} known=${Boolean(row.last)}
                       worktree=${last.checkout || ""} ctx=${codex ? "—" : `peak ${Math.round(row.pctMax || 0)}%`} />
            <${Edge} pct=${row.pctMax || 0} past />
        </div>
    `;
}

// GhostCard holds the place of a session being raised: none of its lines is
// known yet, so it is one line, in the columns of the card.
export function GhostCard({ task, solo = null, known = "" }) {
    const sec = Math.max(0, Math.round((Date.now() - task.since) / 1000));
    const part = nameOf(task.target, solo ? { project: solo } : { known });
    return html`
        <div class="sc scghost" data-name=${task.target} role="status">
            <span class="scglyph" data-tone="busy" data-kind="ghost" aria-hidden="true"><span class="spin"></span></span>
            <div class="scl1">
                <${Name} full=${task.target} part=${part} />
                <span class="scword">starting</span>
                <span class="sctime">${sec}s</span>
            </div>
        </div>
    `;
}

// restartable lists the sessions of a heading a restart of them all reaches:
// what the panel started and can restart, and is not closing or restarting.
export function restartable(rows, wait) {
    return rows.filter((s) => !s.outside && !s.restarting && !(isCodex(s) && codexRestartWhy(s))
        && !(wait && (wait.of("close", s.session) || wait.of("restart", s.session))));
}

// HeadActs are the controls of a project's heading: a new session in it, the
// pause of the project — the continue once all of it is paused — and a
// restart of every live session under the heading, one after another, each
// through its own confirmation as a restart of one goes; a cancel stops the
// rest.
export function HeadActs({ block, profile, sessions, exec, wait }) {
    const run = useAction();
    const project = block.project;
    const canNew = project && !block.home;
    const newWhy = knows(exec, "session.open") ? "" : whyNot(exec, "session.open");
    const rows = restartable(block.live, wait);
    const restartWhy = (knows(exec, "session.restart") ? "" : whyNot(exec, "session.restart"))
        || (rows.length ? "" : "No session here can be restarted now");
    const restartAll = async () => {
        for (const s of rows) {
            const reply = await run("session.restart", s.session, {});
            if (!reply || reply.cancelled) return;
        }
    };
    return html`
        <span class="scheadacts">
            ${canNew && html`
                <${Act} label=${`new session in project ${block.name}`} tip=${newWhy || `A new session in ${block.name}`} off=${newWhy}
                        onPress=${() => run("session.open", project.session || project.name, { project: project.id })}>${Icon.plus()}<//>
            `}
            ${project && html`<${GroupPause} project=${project} profile=${profile} sessions=${sessions} rows=${block.live} exec=${exec} />`}
            <${Act} label=${`restart the sessions of ${block.name}`} off=${restartWhy}
                    tip=${restartWhy || `Restart ${rows.length} ${rows.length === 1 ? "session" : "sessions"}, each asked for on its own`}
                    onPress=${restartAll}>${Icon.refresh()}<//>
        </span>
    `;
}

// GroupPause is the pause of a project's heading: the sheet of the pause for
// the project, or, once every live session under the heading is paused, the
// continue of them all.
function GroupPause({ project, profile, sessions, rows, exec }) {
    const run = useAction();
    const [open, setOpen] = useState(false);
    const why = pauseWhy(project, profile, sessions, exec);
    const reached = rows.filter((s) => !s.outside && s.sessionId);
    if (reached.length > 0 && reached.every((s) => s.pauseAlarm)) {
        return html`
            <${Act} label=${`continue sessions in project ${project.name}`} tip=${why || "Continue them all now"} off=${why} on
                    onPress=${() => run("project.pause", project.session || project.name, { project: project.id, group: false, resume: true })}>
                ${Icon.play()}
            <//>
        `;
    }
    return html`
        <${Act} label=${`pause sessions in project ${project.name}`} tip=${why || "Pause sessions…"} off=${why}
                onPress=${() => setOpen(true)}>${Icon.pause()}<//>
        ${open && html`<${Sheet} open tall onClose=${() => setOpen(false)} label="pause sessions">
            <${PauseSheet} project=${project} profile=${profile} sessions=${sessions} exec=${exec} onDone=${() => setOpen(false)} />
        <//>`}
    `;
}
