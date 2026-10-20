// The sessions of a contour as blocks of their projects. A person thinks in
// projects — "is aacpanel waiting for me", not "is session four" — so a block
// holds a project's live sessions as rows and its last conversation, and the
// list puts pinned groups first, then what needs the person, and the rest
// in the order of the map.

import { useEffect, useState } from "preact/hooks";

import { html } from "../../html.js";
import { ago, plural, since, tokens } from "../../format.js";
import { Icon } from "../../ui/icons.js";
import { ContextBar } from "../../ui/bar.js";
import { waitText } from "../../ui/waits.js";
import { useAction } from "../../actions/gate.js";
import { knows, whyNot } from "../../exec.js";
import { checklistShort } from "../chat/checklist.js";
import { sessionsOf } from "./of.js";
import { kinLabel, kinOf, layoutOf, openedLabel, placeOf } from "./kin.js";
import { useFolds } from "./folds.js";
import { CODEX_CLOSE, agentKey, agentName, codexResume, isCodex, shownName, spoke } from "../../agent.js";
import { backgroundOf, modelTitle, runningOf, workOf } from "../chat/head.js";
import { listen, shown } from "../chat/answered.js";
import { stamp, when } from "./card.js";
import { WaitingMark } from "../../waiting.js";
import { comparePins, pinAt, pinOf } from "./pin.js";

// The most pressing state of a block: who waits for the person, who works —
// on an answer or in its background — and who is quiet.
export const RANK = { wait: 0, busy: 1, quiet: 2 };

// How many projects with nothing live the list shows under the live ones: the
// rest is a tap away in the archive, which holds every project.
export const QUIET_MAX = 5;

// A message that says nothing about the work: "go on", "yes", the word a
// restart opens with. The card is named by the last one that says something.
const ABOUT_MIN = 12;

// stateOf says what a live session is doing, in the words and the tone of the
// row that shows it: since is all that follows the state on one line, and
// when and work are its two halves — since when, and what runs in the
// background — for a row that gives each a line of its own. A question or a
// permission answered from this page no longer has the session waiting,
// whatever the snapshot has not caught up with yet (see shown in answered.js).
export function stateOf(row) {
    const s = shown(row);
    if (s.ask) {
        const more = s.ask.count > 1 ? `${s.ask.count} ${plural(s.ask.count, "question", "questions")}` : "";
        return { tone: "wait", text: `asks you${s.ask.header ? ` · ${s.ask.header}` : ""}`, since: more, when: more, work: "" };
    }
    if (s.waitingFor || s.status === "waiting") {
        return { tone: "wait", text: waitText(s.waitingFor), since: "", when: "", work: "" };
    }
    const work = workOf(s.work);
    if (s.status === "busy") return { tone: "busy", text: "working", since: work, when: "", work };
    if (s.noRequests) return { tone: "idle", text: "no requests yet", since: work, when: "", work };
    const when = s.lastRequestAt ? ago(s.lastRequestAt) : "";
    const since = [when, work].filter(Boolean).join(" · ");
    // The answer is over and what it started still runs (see runningOf): the
    // row says so rather than idle.
    if (runningOf(s.work) > 0) return { tone: "bg", text: "background", since, when, work };
    return { tone: "idle", text: "idle", since, when, work };
}

// useAnswers redraws a list of sessions when an answer from this page marks a
// session answered, or the mark goes: the list stands beside the conversation
// at a desk, and the row should stop waiting with the head of it, not at the
// next snapshot.
export function useAnswers() {
    const [, redraw] = useState(0);
    useEffect(() => listen(() => redraw((n) => n + 1)), []);
}

// stopsOf names what a close or a restart of a live session ends with it: its
// background tasks, agents and workflows at work, as the snapshot counts them.
export function stopsOf(s) {
    const w = (s && s.work) || {};
    const out = [];
    const tasks = backgroundOf(w);
    if (tasks > 0) out.push(`${tasks} ${plural(tasks, "background task", "background tasks")}`);
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

// projectsIn lists the projects of a contour's map in its order, each with the
// name of the group it stands in.
function projectsIn(profile) {
    const out = [];
    for (const group of (profile && profile.groups) || []) {
        for (const project of group.projects || []) out.push({ project, group: group.name });
    }
    return out;
}

// liveBlocksOf lays the live sessions of a contour out as blocks of their
// projects, in the order of the map: every live session in the block of its
// project, and a session being raised in the block of the project it is
// raised in, at the place of the project whether anything there lives yet or
// not; a session no project holds, or raised in no project, in a block of its
// own after them.
// A session another started — opened through the panel or run inside its
// work — stands in the block of the eldest of them, whatever its own project:
// the group is read as one. A block keeps its sessions in the order of the
// list it is given, the ones another started after the rest; how they stand
// is the business of the screen that draws them.
export function liveBlocksOf({ profile, sessions = [], opening = [], wait = null }) {
    const { rootOf } = kinOf(sessions);
    const tops = sessions.filter((s) => rootOf(s) === s);
    const projects = projectsIn(profile);
    const out = new Map();
    const put = (key, init) => {
        if (!out.has(key)) out.set(key, { key, live: [], ghosts: [], home: false, ...init });
        return out.get(key);
    };

    const claimed = new Set();
    const raised = new Set();
    for (const { project, group } of projects) {
        const own = sessionsOf(project, tops);
        const ghosts = opening.filter((task) => !raised.has(task) && task.target === project.session);
        if (own.length === 0 && ghosts.length === 0) continue;
        const block = put(`p${project.id}`, { name: project.name, group, project });
        for (const s of own) {
            block.live.push(s);
            block.home = block.home || Boolean(s.home);
            claimed.add(s.session);
        }
        for (const task of ghosts) {
            block.ghosts.push(task);
            raised.add(task);
        }
    }
    for (const s of tops) {
        if (claimed.has(s.session)) continue;
        const block = put(`s:${s.session}`, { name: s.session, group: s.home ? "the home session" : "outside the map", project: null });
        block.live.push(s);
        block.home = block.home || Boolean(s.home);
    }
    const blockOf = new Map();
    for (const block of out.values()) {
        for (const s of block.live) blockOf.set(s.session, block);
    }
    for (const s of sessions) {
        const root = rootOf(s);
        if (root !== s && blockOf.has(root.session)) blockOf.get(root.session).live.push(s);
    }
    for (const task of opening) {
        if (!raised.has(task)) put(`s:${task.target}`, { name: task.target, group: "", project: null }).ghosts.push(task);
    }
    // Only the pinned root and its children move. Moving its whole project
    // would lift unrelated sessions and mix the order of several pins.
    const pinned = [];
    const rest = [];
    for (const block of out.values()) {
        const roots = block.live.filter((s) => rootOf(s) === s && pinAt(s, wait));
        for (const root of roots) {
            const live = block.live.filter((s) => rootOf(s) === root);
            pinned.push({ ...block, key: `pin:${root.session}`, live, ghosts: [],
                home: live.some((s) => s.home), pinnedAt: pinAt(root, wait) });
        }
        block.live = block.live.filter((s) => !roots.includes(rootOf(s)));
        if (block.live.length || block.ghosts.length) rest.push(block);
    }
    return [...pinned.sort((a, b) => comparePins(a.pinnedAt, b.pinnedAt)), ...rest];
}

// blocksOf lays the sessions of a contour out as the blocks of the list: the
// live ones as liveBlocksOf has them, and the last conversation of a block
// with nothing live. After the pins, a block that needs the person comes
// first; the rest keep the map's order whatever their activity. A block with
// a live session shows none of its past: the way into the project is the live
// session, and a closed conversation beside it reads as that session.
// Projects with nothing live come last and at most QUIET_MAX of them.
export function blocksOf({ profile, sessions = [], recent = [], opening = [], wait = null }) {
    const projects = projectsIn(profile);
    // The snapshot follows context fill; names keep rows within a project
    // and outside the map from moving whenever their work changes.
    const ordered = [...sessions].sort((a, b) => a.session.localeCompare(b.session));
    const out = new Map(liveBlocksOf({ profile, sessions: ordered, opening, wait }).map((block) => [block.key, { ...block, past: null }]));
    const put = (key, init) => {
        if (!out.has(key)) out.set(key, { key, live: [], ghosts: [], past: null, home: false, ...init });
        return out.get(key);
    };

    const live = new Set(sessions.map((s) => s.sessionId).filter(Boolean));
    const liveProjects = new Set([...out.values()].filter((b) => b.live.length && b.project).map((b) => b.project.id));
    let quiet = 0;
    for (const row of recent) {
        if (live.has(row.sessionId)) continue;
        if (row.project && liveProjects.has(row.project.id)) continue;
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
            : tones.includes("busy") || tones.includes("bg") || block.ghosts.length > 0 ? RANK.busy
                : RANK.quiet;
    }
    // liveBlocksOf supplies the map's order and the archive is appended.
    // Only attention lifts an unpinned block; activity cannot move the rest.
    const attention = (b) => !b.pinnedAt && b.rank === RANK.wait ? 0 : 1;
    return [...out.values()].sort((a, b) => comparePins(a.pinnedAt, b.pinnedAt) || attention(a) - attention(b));
}

// ProjectBlock is one project: its name, a new session in it, and its live
// sessions or, with none, its last conversation.
export function ProjectBlock({ block, exec, wait, notes, live, past, onOpen, onProject }) {
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
            <${LiveLines} list=${block.live} live=${live} past=${past} notes=${notes} wait=${wait}
                          onOpen=${onOpen} exec=${exec} />
            ${block.ghosts.map((task) => html`<${GhostLine} key=${task.target} task=${task} />`)}
            ${block.past && html`<${PastLine} row=${block.past} project=${project} exec=${exec} onOpen=${onOpen} />`}
        </section>
    `;
}

// LiveLines lays out the live sessions of a project: the sessions a session
// had the panel open stand a step in under it, behind a fold that is open
// until the person closes it and says who of them waits; the runs it started
// inside its work fold under it, closed; the sessions of a parent that closed
// stand under what is left of it. live is every live session the screen
// knows of, and past the closed conversations it has read: they tell a parent
// alive elsewhere from one that closed, and when it closed.
export function LiveLines({ list, live = null, past = [], notes, wait, exec, onOpen }) {
    const [folded, fold] = useFolds();
    const line = (s, kid = false, branch = "", from = "") => html`
        <${LiveLine} key=${s.session} session=${s} kid=${kid} branch=${branch} from=${from}
                     pin=${pinOf(s, live || list, wait)}
                     notes=${notes && notes.get(s.session)} wait=${wait} onOpen=${onOpen} exec=${exec} />
    `;
    return layoutOf(list, live, folded).map((e) => {
        switch (e.kind) {
        case "fold":
            return html`<${OpenedFold} key=${`fold:${e.parent.session}`} kids=${e.kids} open=${e.open}
                                      onToggle=${() => fold(e.parent.session)} />`;
        case "runs":
            return html`<${KinFold} key=${`kin:${e.parent.session}`} kids=${e.kids} line=${line} />`;
        case "stub":
            return html`<${ParentStub} key=${`stub:${e.parent}`} parent=${e.parent} row=${closedRow(past, e.parent)}
                                      onOpen=${onOpen} />`;
        default:
            return line(e.s, false, e.branch || "", e.from || "");
        }
    });
}

// waitsOf says who of the sessions a session opened waits for the person: by
// name where one does, by count where several do.
export function waitsOf(kids) {
    const waiting = kids.filter((s) => stateOf(s).tone === "wait");
    if (waiting.length === 0) return "";
    if (waiting.length > 1) return `${waiting.length} wait for you`;
    return `${shownName(waiting[0])} ${shown(waiting[0]).ask ? "asks you" : "waits"}`;
}

// closedRow is the latest closed conversation of a session, by its name, among
// the ones a screen has read; null when it has read none.
export function closedRow(past, name) {
    return (past || []).find((row) => row.name === name) || null;
}

// OpenedFold heads the sessions a session had the panel open: how many, and
// who of them waits for the person — in the colour of waiting, so a closed
// fold does not hide a question. Closed, it shows the state of each by a dot.
function OpenedFold({ kids, open, onToggle }) {
    const waits = waitsOf(kids);
    return html`
        <button class="pjrow pjfold" type="button" aria-expanded=${open ? "true" : "false"}
                onClick=${onToggle}>
            <span class=${`pjkinchev${open ? " open" : ""}`}>${Icon.chevron()}</span>
            <span class="pjfoldtext">${openedLabel(kids.length)}</span>
            ${!open && html`<span class="pjfolddots">${kids.map((s) => html`<i key=${s.session} data-tone=${stateOf(s).tone}></i>`)}</span>`}
            ${waits && html`<span class="pjfoldwaits">· ${waits}</span>`}
        </button>
    `;
}

// ParentStub holds the place of a parent that closed, over the sessions it
// opened: its name, struck through, and when it closed. Where the screen has
// read its conversation a press opens it, as a closed conversation opens.
function ParentStub({ parent, row, onOpen }) {
    const closed = row ? `closed ${when(stamp(row.lastAt))}` : "closed";
    if (!row || !onOpen) {
        return html`
            <div class="pjrow pjstub"><span class="pjstubname">${parent}</span><span class="pjstubnote">${closed}</span></div>
        `;
    }
    return html`
        <button class="pjrow pjstub" type="button" aria-label=${`open the closed conversation ${parent}`}
                onClick=${() => onOpen(row.name, row.sessionId)}>
            <span class="pjstubname">${parent}</span>
            <span class="pjstubnote">${closed}</span>
            <span class="chev">${Icon.chevron()}</span>
        </button>
    `;
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

// LiveLine is a live session inside its project, a line for each question:
// what it is called, what it is doing, for how long it has lived and whether
// Remote Control is up, which work of the repository is its — its role, where
// it has one — where it is in the checklist of its work, who runs it
// on which model and when it last asked, and what runs in its background. On
// a phone one line holds no more than one of these: the state is read whole,
// the checklist cut at its end. Every row opens with the name of its session,
// the only session of its project included, and Restart and Close at the
// right edge. The name yields to the buttons; where the session lives follows
// Remote Control on the state's line. The home session is never closed,
// codex refuses a restart, and a session outside the panel is only read.
// How full the session is stands level with the last line on the left, so
// the lines under the name give up to the right no more than that figure. A
// session another opened stands a step in under it, on the line of the branch
// (branch "mid" or "last"); one whose parent stands elsewhere says which
// session opened it (from).
export function LiveLine({ session, wait, pin = pinOf(session, [], wait), kid = false, branch = "", from = "", notes, exec, onOpen }) {
    const run = useAction();
    const state = stateOf(session);
    const steps = checklistShort(session.checklist);
    const closing = wait ? wait.of("close", session.session) : null;
    const restarting = wait ? wait.of("restart", session.session) : null;
    const busy = closing || restarting;
    const codex = isCodex(session);
    const canRestart = knows(exec, "session.restart");
    const canClose = knows(exec, "session.close");
    const lost = stopsOf(session);
    const ends = (note) => (lost ? `stops ${lost} · ${note}` : note);
    const restartNote = ends("a new session with an empty context; this one stays in the archive");
    const closeNote = codex ? CODEX_CLOSE : ends("the conversation stays in the archive");
    const place = placeOf(session);
    const tag = { stream: Icon.feed, tmux: Icon.terminal, outside: Icon.exit, daemon: Icon.plug }[place];
    // The quiet line opens with who runs the session, in the hue of its agent.
    const lived = session.startedAt ? since(session.startedAt) : "";
    const runs = [session.model ? modelTitle(session.model, { withWindow: false }) : "", state.when]
        .filter(Boolean).map((part) => ` · ${part}`).join("");
    return html`
        <div class=${`pjrow pjcard${kid ? " pjkid" : ""}${branch ? ` pjbranch${branch === "last" ? " pjbranchend" : ""}` : ""}`}>
            <div class="pjtop">
                <span class="pjsess">${shownName(session)}</span>
                ${!session.outside && html`
                    <div class="pjacts">
                        <button class="pjact" type="button" disabled=${pin.pending || !knows(exec, "session.pin")}
                                aria-label=${`${pin.on ? "unpin" : "pin"} session group ${pin.root.session}`}
                                aria-pressed=${pin.on ? "true" : "false"}
                                aria-busy=${pin.pending ? "true" : undefined}
                                title=${pin.why || whyNot(exec, "session.pin") || pin.tip}
                                onClick=${() => run("session.pin", pin.root.session, { on: !pin.on })}>${Icon.thumbtack()}</button>
                        ${!codex && html`
                            <button class="pjact" type="button" disabled=${!canRestart || Boolean(busy)}
                                    aria-label=${`restart session ${session.session}`}
                                    title=${busy ? "the session is already closing or restarting" : canRestart ? restartNote : whyNot(exec, "session.restart")}
                                    onClick=${() => run("session.restart", session.session, {})}>${Icon.refresh()}</button>
                        `}
                        ${!session.home && html`
                            <button class="pjact" type="button" disabled=${!canClose || Boolean(busy)}
                                    aria-label=${`close session ${session.session}`}
                                    title=${busy ? "the session is already closing or restarting" : canClose ? closeNote : whyNot(exec, "session.close")}
                                    onClick=${() => run("session.close", session.session, {})}>${Icon.close()}</button>
                        `}
                    </div>
                `}
            </div>
            <button class="pjopen" type="button" aria-label=${`open conversation ${session.session}`}
                    onClick=${() => onOpen && onOpen(session.session, session.sessionId)}>
                <span class=${`pjstate pj-${state.tone}`}>
                    <i class="pjdot"></i><span class="pjtext">${state.text}</span>
                    ${lived && html`<span class="pjlived">${lived}</span>`}
                    ${session.remote && html`<span class="pjremote" title="Remote Control is on: the session is open on claude.ai too">RC</span>`}
                    ${html`<${WaitingMark} session=${session} />`}
                    <span class="pjtag">${tag()}${place}</span>
                </span>
                ${session.role && html`<span class="pjrole">${session.role}</span>`}
                ${steps && html`<span class="pjchecklist">${steps}</span>`}
                <span class="pjsince"><span class="agentword" data-agent=${agentKey(session)}>${agentName(session)}</span>${runs}</span>
                ${state.work && html`<span class="pjwork">${state.work}</span>`}
                ${from && html`<span class="pjfrom">opened by <b>${from}</b></span>`}
            </button>
            <span class="pjpct">${session.noRequests ? "—" : `${Math.round(session.pct || 0)}%`}</span>
            ${!session.noRequests && html`<${ContextBar} pct=${session.pct} edge />`}
            ${busy && html`<div class="pjbusy" role="status"><span class="spin"></span>${closing ? "closing" : "restarting"}</div>`}
        </div>
        ${(notes || []).map((note) => html`<div class="pjnote" key=${note}>${note}</div>`)}
    `;
}

// GhostLine holds the place of a session being raised.
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
    const codex = isCodex(row);
    const about = codex ? "" : aboutOf(row, project);
    const ready = knows(exec, "session.resume");
    const ran = spanOf(stamp(row.startedAt), stamp(row.lastAt));
    // A thread of codex keeps no words in the archive: its line says who ran
    // it on which model, and what it spent, where a conversation of claude
    // says what it was about and how full it got.
    return html`
        <div class="pjrow pjpast">
            <button class="pjopen" type="button" aria-label=${`open conversation ${row.name}`}
                    onClick=${() => onOpen && onOpen(row.name, row.sessionId)}>
                ${named && html`<span class="pjsess">${row.name}</span>`}
                ${codex
                    ? html`<span class="pjabout"><span class="agentword" data-agent=${agentKey(row)}>${agentName(row)}</span>${pastModel(row)}</span>`
                    : html`<span class=${`pjabout${about ? "" : " pjnone"}`}>${about
                        ? `«${about}»`
                        : row.messages > 0 ? `${row.messages} ${plural(row.messages, "message", "messages")}` : "not a word said"}</span>`}
                <span class="pjwhen">${when(stamp(row.lastAt))}${ran ? ` · ${ran}` : ""} · ${codex
                    ? (spoke(row) ? `${tokens(row.tokensUsed)} tokens` : "not a word said")
                    : `peak ${Math.round(row.pctMax || 0)}%`}</span>
            </button>
            ${row.sessionId && html`
                <button class="pjresume" type="button" disabled=${!ready}
                        aria-label=${`resume conversation of ${row.name}`}
                        title=${ready ? "" : whyNot(exec, "session.resume")}
                        onClick=${() => run("session.resume", row.name, { session: row.sessionId, ...codexResume(row) })}>Resume</button>
            `}
        </div>
    `;
}

// pastModel is the model and the effort an archived thread ran at, after the
// word of its agent.
export function pastModel(row) {
    return [row.model ? modelTitle(row.model, { withWindow: false }) : "", row.effort || ""]
        .filter(Boolean).map((part) => ` · ${part}`).join("");
}

function spanOf(from, to) {
    const sec = Math.max(0, (to || 0) - (from || 0));
    if (!from || !to) return "";
    if (sec < 3600) return `${Math.max(1, Math.round(sec / 60))} min`;
    if (sec < 86400) return `${Math.round(sec / 3600)} h`;
    return `${Math.floor(sec / 86400)} d`;
}
