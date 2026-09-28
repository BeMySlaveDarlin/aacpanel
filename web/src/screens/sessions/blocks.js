// The sessions of a contour as blocks of their projects. A person thinks in
// projects — "is aacpanel waiting for me", not "is session four" — so a block
// holds a project's live sessions as rows and its last conversation, and the
// list puts first what needs the person, then what is working, then the rest.

import { useState } from "preact/hooks";

import { html } from "../../html.js";
import { ago, plural } from "../../format.js";
import { Icon } from "../../ui/icons.js";
import { Sheet } from "../../ui/sheet.js";
import { ContextBar } from "../../ui/bar.js";
import { waitText } from "../../ui/waits.js";
import { useAction } from "../../actions/gate.js";
import { hostLabel } from "../../actions/registry.js";
import { knows, whyNot } from "../../exec.js";
import { moveSession, useSwitchWay } from "../chat/switch.js";
import { checklistShort } from "../chat/checklist.js";
import { sessionsOf } from "./of.js";
import { kinLabel, kinOf, outsideNote, placeOf } from "./kin.js";
import { stamp, when } from "./card.js";

// The order of the list: who waits for the person, who works, who is quiet.
export const RANK = { wait: 0, busy: 1, quiet: 2 };

// How many projects with nothing live the list shows under the live ones: the
// rest is a tap away in the archive, which holds every project.
export const QUIET_MAX = 5;

// A message that says nothing about the work: "go on", "yes", the word a
// restart opens with. The card is named by the last one that says something.
const ABOUT_MIN = 12;

// unchecked says why a session on the stream runs a claude the feed was not
// checked on: claude updates itself, and a new version can change a request
// the feed depends on without a word. Empty when there is nothing to say.
export function unchecked(session, checked) {
    if (session.transport !== "stream" || !session.version) return "";
    if (checked === session.version) return "";
    return `claude ${session.version} is not checked for the feed`;
}

// stateOf says what a live session is doing, in the words and the tone of the
// row that shows it.
export function stateOf(s) {
    if (s.ask) {
        const more = s.ask.count > 1 ? `${s.ask.count} ${plural(s.ask.count, "question", "questions")}` : "";
        return { tone: "wait", text: `asks you${s.ask.header ? ` · ${s.ask.header}` : ""}`, since: more };
    }
    if (s.waitingFor || s.status === "waiting") return { tone: "wait", text: waitText(s.waitingFor), since: "" };
    const agents = (s.work && s.work.agents) || 0;
    const tasks = (s.work && s.work.tasks) || 0;
    const work = [
        agents > 0 && `${agents} ${plural(agents, "agent", "agents")}`,
        tasks > 0 && `${tasks} ${plural(tasks, "background task", "background tasks")}`,
    ].filter(Boolean).join(" · ");
    if (s.status === "busy") return { tone: "busy", text: "working", since: work };
    if (s.noRequests) return { tone: "idle", text: "no requests yet", since: work };
    return { tone: "idle", text: "idle", since: [s.lastRequestAt ? ago(s.lastRequestAt) : "", work].filter(Boolean).join(" · ") };
}

// stopsOf names what a close or a restart of a live session ends with it: its
// background tasks, agents and workflows at work, as the snapshot counts them.
export function stopsOf(s) {
    const w = (s && s.work) || {};
    const out = [];
    if (w.tasks > 0) out.push(`${w.tasks} ${plural(w.tasks, "background task", "background tasks")}`);
    if (w.agents > 0) out.push(`${w.agents} ${plural(w.agents, "agent", "agents")}`);
    if (w.workflows > 0) out.push(`${w.workflows} ${plural(w.workflows, "workflow", "workflows")}`);
    return out.join(", ");
}

// intentOf returns the first message a project opens with, as its launch line
// names it: a conversation is not named by the words it was opened with.
function intentOf(project) {
    const words = (project && project.line && project.line.words) || [];
    const said = words.find((w) => w.key === "intent");
    return said ? said.text.trim() : "";
}

// aboutOf names an archived conversation by the last thing the person said in
// it that says something: not a slash command, not the project's opening
// message, and not a word or two.
export function aboutOf(row, project) {
    const intent = intentOf(project);
    const said = (row && row.prompts) || [];
    for (let i = said.length - 1; i >= 0; i--) {
        const text = String(said[i] || "").trim();
        if (text.length < ABOUT_MIN || text.startsWith("/") || (intent && text === intent)) continue;
        return text;
    }
    return "";
}

// blocksOf lays the sessions of a contour out as blocks of their projects:
// every live session in the block of its project (a session no project holds
// gets a block of its own), the consoles being raised beside them, and the
// last conversation of a block with nothing live. A block with a live session
// shows none of its past: the way into the project is the live session, and a
// closed conversation beside it reads as that session. Projects with nothing
// live come last and at most QUIET_MAX of them.
export function blocksOf({ profile, sessions = [], recent = [], opening = [] }) {
    const projects = [];
    for (const group of (profile && profile.groups) || []) {
        for (const project of group.projects || []) projects.push({ project, group: group.name });
    }
    const out = new Map();
    const put = (key, init) => {
        if (!out.has(key)) out.set(key, { key, live: [], ghosts: [], past: null, home: false, ...init });
        return out.get(key);
    };

    const claimed = new Set();
    for (const { project, group } of projects) {
        const own = sessionsOf(project, sessions);
        if (own.length === 0) continue;
        const block = put(`p${project.id}`, { name: project.name, group, project });
        for (const s of own) {
            block.live.push(s);
            block.home = block.home || Boolean(s.home);
            claimed.add(s.session);
        }
    }
    for (const s of sessions) {
        if (claimed.has(s.session)) continue;
        const block = put(`s:${s.session}`, { name: s.session, group: s.home ? "the home session" : "outside the map", project: null });
        block.live.push(s);
        block.home = block.home || Boolean(s.home);
    }
    for (const task of opening) {
        const known = projects.find((p) => p.project.session === task.target);
        const block = known
            ? put(`p${known.project.id}`, { name: known.project.name, group: known.group, project: known.project })
            : put(`s:${task.target}`, { name: task.target, group: "", project: null });
        block.ghosts.push(task);
    }

    const live = new Set(sessions.map((s) => s.sessionId).filter(Boolean));
    let quiet = 0;
    for (const row of recent) {
        if (live.has(row.sessionId)) continue;
        const key = row.project ? `p${row.project.id}` : `d:${row.cwd || row.name}`;
        let block = out.get(key);
        if (!block) {
            if (quiet >= QUIET_MAX) continue;
            quiet++;
            const known = row.project && projects.find((p) => p.project.id === row.project.id);
            block = put(key, {
                name: (row.project && row.project.name) || row.name,
                group: (row.project && row.project.group) || (row.home ? "the home session" : ""),
                project: known ? known.project : null,
                home: Boolean(row.home),
            });
        }
        if (block.live.length > 0) continue;
        if (!block.past) block.past = row;
    }

    for (const block of out.values()) {
        const tones = block.live.map((s) => stateOf(s).tone);
        block.rank = tones.includes("wait") ? RANK.wait
            : tones.includes("busy") || block.ghosts.length > 0 ? RANK.busy
                : RANK.quiet;
        const times = [
            ...block.live.map((s) => stamp(s.lastRequestAt || s.startedAt)),
            block.past ? stamp(block.past.lastAt) : 0,
        ];
        block.at = Math.max(0, ...times);
    }
    // Among the quiet, a project with a session alive stands above one that has
    // only its past: the first can be written to right now.
    const alive = (b) => (b.live.length > 0 ? 0 : 1);
    return [...out.values()].sort((a, b) => a.rank - b.rank || alive(a) - alive(b) || b.at - a.at
        || a.name.localeCompare(b.name));
}

// ProjectBlock is one project: its name, a new session in it, and its live
// sessions or, with none, its last conversation.
export function ProjectBlock({ block, exec, wait, notes, checked, onOpen, onMore, onProject }) {
    const run = useAction();
    const project = block.project;
    const canNew = project && !block.home;
    const ready = knows(exec, "session.open");
    return html`
        <section class=${`pjblock${block.live.length === 0 && block.ghosts.length === 0 ? " pjquiet" : ""}`}>
            <div class="pjhead">
                ${project && onProject
                    ? html`<button class="pjname" type="button" aria-label=${`project ${block.name}`}
                                   onClick=${() => onProject(project, block.group)}>${block.name}</button>`
                    : html`<span class="pjname">${block.name}</span>`}
                ${block.group && html`<span class="pjgroup">${block.group}</span>`}
                ${canNew && html`
                    <button class="pjnew" type="button" disabled=${!ready}
                            aria-label=${`new session in project ${block.name}`}
                            title=${ready ? "" : whyNot(exec, "session.open")}
                            onClick=${() => run("session.open", project.session, { project: project.id })}>
                        ${Icon.plus()}<span>New</span>
                    </button>
                `}
            </div>
            <${LiveLines} list=${block.live} named=${(s) => block.live.length > 1 || s.session !== (project && project.session)}
                          notes=${notes} checked=${checked} wait=${wait} onOpen=${onOpen} onMore=${onMore} />
            ${block.ghosts.map((task) => html`<${GhostLine} key=${task.target} task=${task} />`)}
            ${block.past && html`<${PastLine} row=${block.past} project=${project} exec=${exec} onOpen=${onOpen} />`}
        </section>
    `;
}

// LiveLines lays out the live sessions of a project: the runs a session
// started inside its work fold under it, and the fold says whether one of
// them waits for the person.
export function LiveLines({ list, named, notes, checked, wait, onOpen, onMore }) {
    const { own, kids } = kinOf(list);
    const line = (s, kid = false) => html`
        <${LiveLine} key=${s.session} session=${s} named=${kid || named(s)} kid=${kid}
                     notes=${notes && notes.get(s.session)} checked=${checked} wait=${wait} onOpen=${onOpen} onMore=${onMore} />
    `;
    return own.map((s) => html`
        ${line(s)}
        ${kids.has(s.session) && html`<${KinFold} key=${`kin:${s.session}`} kids=${kids.get(s.session)} line=${line} />`}
    `);
}

function KinFold({ kids, line }) {
    const [open, setOpen] = useState(false);
    const waiting = kids.filter((s) => stateOf(s).tone === "wait").length;
    return html`
        <button class="pjkin" type="button" aria-expanded=${open ? "true" : "false"} onClick=${() => setOpen(!open)}>
            <span class=${`pjkinchev${open ? " open" : ""}`}>${Icon.chevron()}</span>
            <span>${kinLabel(kids.length)}</span>
            ${waiting > 0 && html`<span class="pjkinwait">· ${waiting} waiting</span>`}
        </button>
        ${open && kids.map((s) => line(s, true))}
    `;
}

// LiveLine is a live session inside its project: what it is doing and where
// it is in the checklist of its work, where it lives, how full it is, and the
// button of what can be done to it. The checklist takes what room the state
// leaves on its line and gives way first: the state is read whole.
export function LiveLine({ session, named, kid = false, notes, checked, wait, onOpen, onMore }) {
    const state = stateOf(session);
    const steps = checklistShort(session.checklist);
    const closing = wait ? wait.of("close", session.session) : null;
    const restarting = wait ? wait.of("restart", session.session) : null;
    const busy = closing || restarting;
    const place = placeOf(session);
    const tag = { feed: Icon.feed, console: Icon.terminal, outside: Icon.exit }[place];
    return html`
        <div class=${`pjrow${kid ? " pjkid" : ""}`}>
            <button class="pjopen" type="button" aria-label=${`open conversation ${session.session}`}
                    onClick=${() => onOpen && onOpen(session.session, session.sessionId)}>
                ${named && html`<span class="pjsess">${session.session}</span>`}
                <span class=${`pjstate pj-${state.tone}${steps ? " checklisted" : ""}`}>
                    <i class="pjdot"></i><span class="pjtext">${state.text}</span>
                    ${steps && html`<span class="pjchecklist">${steps}</span>`}
                </span>
                ${state.since && html`<span class="pjsince">${state.since}</span>`}
            </button>
            ${session.remote && html`<span class="pjrc" title="Remote Control is on: the session is open on claude.ai too">RC</span>`}
            <span class="pjtag">${tag()}${place}</span>
            <span class="pjpct">${session.noRequests ? "—" : `${Math.round(session.pct || 0)}%`}</span>
            <button class="pjmore" type="button" aria-label=${`what to do with session ${session.session}`}
                    onClick=${() => onMore(session)}>${Icon.more()}</button>
            ${!session.noRequests && html`<${ContextBar} pct=${session.pct} edge />`}
            ${busy && html`<div class="pjbusy" role="status"><span class="spin"></span>${closing ? "closing" : "restarting"}</div>`}
        </div>
        ${[...(notes || []), unchecked(session, checked)].filter(Boolean).map((note) => html`<div class="pjnote" key=${note}>${note}</div>`)}
    `;
}

// GhostLine holds the place of a console being raised.
export function GhostLine({ task }) {
    const sec = Math.max(0, Math.round((Date.now() - task.since) / 1000));
    return html`
        <div class="pjrow pjghost" role="status">
            <span class="pjstate pj-busy"><span class="spin"></span><span class="pjtext">starting on the host · ${sec} s</span></span>
        </div>
    `;
}

// PastLine is the last conversation of a project: what it was about, when, and
// the way back into it.
export function PastLine({ row, project, exec, onOpen, named = false }) {
    const run = useAction();
    const about = aboutOf(row, project);
    const ready = knows(exec, "session.resume");
    const ran = spanOf(stamp(row.startedAt), stamp(row.lastAt));
    return html`
        <div class="pjrow pjpast">
            <button class="pjopen" type="button" aria-label=${`open conversation ${row.name}`}
                    onClick=${() => onOpen && onOpen(row.name, row.sessionId)}>
                ${named && html`<span class="pjsess">${row.name}</span>`}
                <span class=${`pjabout${about ? "" : " pjnone"}`}>${about
                    ? `«${about}»`
                    : row.messages > 0 ? `${row.messages} ${plural(row.messages, "message", "messages")}` : "not a word said"}</span>
                <span class="pjwhen">${when(stamp(row.lastAt))}${ran ? ` · ${ran}` : ""} · peak ${Math.round(row.pctMax || 0)}%</span>
            </button>
            ${row.sessionId && html`
                <button class="pjresume" type="button" disabled=${!ready}
                        aria-label=${`resume conversation of ${row.name}`}
                        title=${ready ? "" : whyNot(exec, "session.resume")}
                        onClick=${() => run("session.resume", row.name, { session: row.sessionId })}>Resume</button>
            `}
        </div>
    `;
}

function spanOf(from, to) {
    const sec = Math.max(0, (to || 0) - (from || 0));
    if (!from || !to) return "";
    if (sec < 3600) return `${Math.max(1, Math.round(sec / 60))} min`;
    if (sec < 86400) return `${Math.round(sec / 3600)} h`;
    return `${Math.floor(sec / 86400)} d`;
}

// SessionSheet is what can be done to a live session from the list, each line
// with what follows from it; the gate asks before anything that ends work.
export function SessionSheet({ session, exec, onClose, onOpen }) {
    const run = useAction();
    const name = session ? session.session : "";
    const way = useSwitchWay(name, session ? session.transport : "");
    if (!session) return html`<${Sheet} open=${false} onClose=${onClose} label="session actions"><//>`;
    const stream = session.transport === "stream";
    const act = (fn) => async () => { onClose(); await fn(); };
    const lines = [];
    lines.push({ key: "open", icon: Icon.feed(), text: "Open the conversation", note: "the feed of this session",
        press: act(async () => onOpen && onOpen(session.session, session.sessionId)) });
    // A claude the panel did not start is only read: nothing else is offered.
    if (session.outside) {
        lines.push({ key: "outside", icon: Icon.exit(), text: "Outside the panel", note: outsideNote(session), why: "",
            press: null });
    }
    if (!session.outside && (way.to === "console" || way.to === "stream")) {
        const to = way.to;
        lines.push({ key: "move", icon: to === "stream" ? Icon.feed() : Icon.terminal(),
            text: to === "stream" ? "Move to the feed" : "Move to the console",
            note: "the same conversation on the other side; between turns only",
            why: whyNot(exec, "session.switch"),
            press: act(async () => moveSession({ run, exec, name, to })) });
    }
    if (!stream && !session.outside) {
        lines.push({ key: "window", icon: Icon.monitor(), text: `Open a window on ${hostLabel()}`,
            note: "a terminal on the host's desktop",
            why: knows(exec, "window.open") ? "" : whyNot(exec, "window.open"),
            press: act(async () => run("window.open", name, {})) });
    }
    // The home session is the one the panel lives beside: it is restarted from
    // scratch and never closed, the way the panel never stops its own container.
    const lost = stopsOf(session);
    const ends = (note) => (lost ? `stops ${lost} · ${note}` : note);
    const restartLine = session.home && {
        key: "restart", icon: Icon.refresh(), text: "Restart", danger: true,
        note: ends("a new session with an empty context; this one stays in the archive"),
        why: knows(exec, "session.restart") ? "" : whyNot(exec, "session.restart"),
        press: act(async () => run("session.restart", name, {})),
    };
    const closeLine = !session.home && {
        key: "close", icon: Icon.close(), text: "Close", danger: true,
        note: ends("the conversation stays in the archive"),
        why: knows(exec, "session.close") ? "" : whyNot(exec, "session.close"),
        press: act(async () => run("session.close", name, {})),
    };
    if (!session.outside) lines.push(restartLine || closeLine);
    return html`
        <${Sheet} open=${true} onClose=${onClose} label=${`actions of session ${name}`}>
            <div class="pjsheet">
                <div class="pjsheethead">
                    <span class="pjsheetname">${name}</span>
                    <span class="pjsheetsub">${[placeOf(session), session.model ? session.model.replace(/^claude-/, "") : "",
                        session.effort || "", session.noRequests ? "" : `${Math.round(session.pct || 0)}%`].filter(Boolean).join(" · ")}</span>
                </div>
                ${lines.map((l) => html`
                    <button key=${l.key} class=${`pjact${l.danger ? " pjdanger" : ""}`} type="button"
                            disabled=${Boolean(l.why) || !l.press} onClick=${l.press}>
                        <span class="pjacticon">${l.icon}</span>
                        <span class="pjactbody"><b>${l.text}</b><span>${l.why || l.note}</span></span>
                    </button>
                `)}
            </div>
        <//>
    `;
}

