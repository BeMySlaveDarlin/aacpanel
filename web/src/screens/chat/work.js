// What a session has in flight: the status bar, the counters and their lists.

import { useEffect, useRef, useState } from "preact/hooks";

import { html } from "../../html.js";
import { Icon } from "../../ui/icons.js";
import { bytes, lasted, plural, since, stopwatch, tokens, until } from "../../format.js";
import { useAction } from "../../actions/gate.js";
import { knows, whyNot } from "../../exec.js";
import { taskVoice } from "./voice.js";
import { ArtifactCard, BriefCard } from "./rows.js";
import { FlowRow, FlowRun, flowTitle } from "./flow.js";
import { fileTag } from "./files.js";
import { Look, LOOK_NAMES } from "./look.js";
import { state as briefState, waiting } from "../../data/briefs.js";
import { key as pageKey, merge } from "../../data/artifacts.js";
import { markOpened, unopened } from "../../data/opened.js";
import { NowBar, nowOf, WaitBar } from "./now.js";
import { ChecklistLine, checklistOf } from "./checklist.js";
import { agentKey as whoKey, agentName, isCodex } from "../../agent.js";

// WorkStatus renders what is happening to the session right now: the call
// going out and for how long, or the thinking between calls. A compaction is
// said whatever else the session reports: claude writes nothing to the
// conversation while it compacts, and the call before it would stand there for
// minutes as if it were still going.
//
// A session whose own turn is over is busy with the work it sent off: claude
// holds it busy while its agents run, and the thinking the bar would count is
// an answer that has already ended. The bar says that work instead, and says
// nothing when the panel sees none of it at work.
//
// The checklist the session keeps heads the bar as its first line, and stands
// alone above the composer when there is nothing else to say: where the session
// is in its work belongs beside what it is doing this moment, and a quiet
// session is still somewhere in its checklist.
//
// onCalls opens the calls of the run going on; onCall opens them on one call
// of that run, the one the bar names.
export function WorkStatus({ work, busy, turnOver, compacting, feed, checklist, onCalls, onCall, onOpen, onChecklist, to }) {
    const line = checklistOf(checklist)
        ? html`<${ChecklistLine} checklist=${checklist} onOpen=${onChecklist} />`
        : null;
    const bar = (card) => ((card || line)
        ? html`<div class=${`workbar status${line ? " checklisted" : ""}`}>${line}${card}</div>`
        : null);
    if (compacting) return bar(html`<${Compacting} key=${compacting} since=${compacting} />`);
    if (!busy) return bar(null);
    if (turnOver) {
        const waits = waitedOn(work);
        return bar(waits && html`<${WaitBar} waits=${waits} onOpen=${onOpen} />`);
    }
    const now = nowOf(feed);
    return bar(html`<${NowBar} now=${now} to=${to} onCalls=${() => now.run != null && onCalls && onCalls(now.run)}
                               onCall=${(call) => now.run != null && onCall && onCall(now.run, call)} />`);
}

// compactPct is how far a compaction has got, the way the terminal shows it.
// Claude reports only the start and the end of a compaction, so it is a guess
// from the time alone: quick at first, slower as it goes, and never full on its
// own — the end comes when claude says so.
export function compactPct(sec) {
    const t = Math.max(0, sec);
    return Math.min(95, Math.round((1 - Math.exp(-t / 90)) * 100));
}

// Compacting counts the compaction up by the second. The share only ever grows:
// a clock of the phone set back would otherwise pull the bar back under the eye.
function Compacting({ since }) {
    const [now, setNow] = useState(() => Date.now());
    const peak = useRef(0);
    useEffect(() => {
        const timer = setInterval(() => setNow(Date.now()), 1000);
        return () => clearInterval(timer);
    }, []);
    const began = new Date(since).getTime();
    const sec = Number.isFinite(began) ? Math.max(0, (now - began) / 1000) : 0;
    peak.current = Math.max(peak.current, compactPct(sec));
    const pct = peak.current;
    return html`
        <div class="workhead">
            <span class="working">compacting the conversation… (${stopwatch(sec)})</span>
            <span class="workpct">${pct}%</span>
            <div class="worktrack" role="progressbar" aria-label="compacting the conversation"
                 aria-valuemin="0" aria-valuemax="100" aria-valuenow=${pct}>
                <i style=${`width:${pct}%`}></i>
            </div>
        </div>
    `;
}

// Work renders the counters of what the session has in flight. Every chip stands
// in the row whatever the session holds: a row that grows and shrinks under the
// thumb moves the button the thumb was already going for, and a group that
// disappears when it is empty leaves a hole between the last chip and the edge.
// The number on a chip counts only what is live — the shells still running, the
// agents still working — and a chip with nothing live carries no number at all
// and goes dim: a 12 over twelve finished shells reads as twelve at work, and a
// 0 is a number where the eye expects none. The dim chip still opens its list —
// the list says out loud that there is nothing, which a button that refuses the
// tap cannot do.
const PAGE = 20;

// briefRow turns a card of the shelf into what the row of a feed draws: the
// same component, so a brief looks the same wherever it is listed.
function briefRow(card) {
    return {
        id: card.id,
        title: card.title,
        eyebrow: card.eyebrow || "",
        questions: card.questions || 0,
        mark: briefState(card),
    };
}

export function Work({ work, onOpen }) {
    const tasks = (work && work.tasks) || [];
    const flows = (work && work.workflows) || [];
    const liveTasks = running(tasks).length + heldLive(work);
    const liveFlows = flows.filter((f) => f.status === "running");

    return html`
        <div class="wchips">
            <button class=${`wchip${liveTasks > 0 ? "" : " idle"}`} type="button"
                    onClick=${() => onOpen({ kind: "tasks" })}
                    aria-label=${taskLabel(tasks, work)}>
                ${Icon.clock()}${liveTasks > 0 && html`<span class="wnum">${liveTasks}</span>`}
            </button>
            <${AgentsChip} work=${work} onOpen=${onOpen} />
            <button class=${`wchip${liveFlows.length > 0 ? "" : " idle"}`} type="button"
                    onClick=${() => onOpen({ kind: "workflows" })}
                    aria-label=${flowLabel(flows, liveFlows)}>
                ${Icon.flow()}${liveFlows.length > 0 && html`<span class="wnum">${liveFlows.length}</span>`}
            </button>
        </div>
    `;
}

// AgentsChip counts the subagents at work and opens their list: a chip of the
// row of claude's counters, and the one a codex thread has beside what it
// left running.
export function AgentsChip({ work, onOpen }) {
    const agents = (work && work.agents) || [];
    const { live } = splitAgents(agents);
    return html`
        <button class=${`wchip${live.length > 0 ? "" : " idle"}`} type="button"
                onClick=${() => onOpen({ kind: "agents" })}
                aria-label=${agentLabel(agents, live)}>
            ${Icon.robot()}${live.length > 0 && html`<span class="wnum">${live.length}</span>`}
        </button>
    `;
}

function flowLabel(flows, live) {
    if (!flows.length) return "workflow runs: none";
    return `workflow runs: ${flows.length}, ${live.length || "none"} running`;
}

function taskLabel(tasks, work) {
    const all = tasks.length + heldCount(work);
    if (!all) return "background work: none";
    return `background work: ${all}, ${running(tasks).length + heldLive(work) || "none"} running`;
}

// HeldChip counts what a codex thread asked the panel to wake it with and
// opens its list, while there is something to list: a chip of the row a
// thread has under its composer, beside what it left running.
export function HeldChip({ work, onOpen }) {
    const all = heldCount(work);
    if (!all) return null;
    const live = heldLive(work);
    return html`
        <button class=${`wchip${live > 0 ? "" : " idle"}`} type="button" onClick=${() => onOpen({ kind: "tasks" })}
                aria-label=${`observers and alarms: ${all}, ${live || "none"} running`}>
            ${Icon.clock()}${live > 0 && html`<span class="wnum">${live}</span>`}
        </button>
    `;
}

function agentLabel(agents, live) {
    if (!agents.length) return "subagents: none";
    return live.length > 0 ? `subagents: ${live.length} working` : "subagents: none working";
}

// WorkRefs renders the right half of the row: the artifacts. An artifact has no
// "over" — what the session made stays made, and a file it sent stays sent —
// so the number counts them all and, like the other chips, is absent rather
// than 0.
// WorkRefs renders the right half of the row: two chips, because the two things
// behind them are not the same kind of thing. A page is made and stays made,
// and its number counts the ones this device has not opened yet — it goes down
// as they are read. A brief asks, and its number counts the ones still waiting
// for an answer.
export function WorkRefs({ work, pages, briefs, onOpen }) {
    const made = merge((work && work.artifacts) || [], pages || []);
    const fresh = unopened(made.map(pageKey));
    const wants = waiting(briefs || []);

    return html`
        <button class=${`wchip${fresh > 0 ? "" : " idle"}`} type="button"
                onClick=${() => onOpen({ kind: "arts" })}
                aria-label=${fresh > 0
                    ? `pages this conversation published and you have not opened: ${fresh}`
                    : "pages this conversation published: all opened"}>
            ${Icon.artifact()}${fresh > 0 && html`<span class="wnum">${fresh}</span>`}
        </button>
        <button class=${`wchip${wants > 0 ? " wants" : " idle"}`} type="button"
                onClick=${() => onOpen({ kind: "briefs" })}
                aria-label=${wants > 0
                    ? `briefs waiting for your answer: ${wants}`
                    : "briefs of this conversation: none waiting"}>
            ${Icon.ask()}${wants > 0 && html`<span class="wnum">${wants}</span>`}
        </button>
    `;
}

const TASK_KINDS = {
    bash: [Icon.terminal, "command"],
    monitor: [Icon.probes, "monitoring"],
    wake: [Icon.alerts, "wake-up"],
    cron: [Icon.alerts, "alarm"],
};

// An alarm the session set itself — a wake-up or a job of its cron — runs
// nothing until it fires: its row neither opens nor stops. It says when the
// alarm is due or, for a job that repeats, how often it fires.
const ALARMS = new Set(["wake", "cron"]);

function alarmWord(task) {
    return task.kind === "cron" && task.repeats ? "repeats" : taskKind(task)[1];
}

function alarmWhen(task) {
    if (task.due) return until(task.due);
    return task.schedule || since(task.at);
}

// What a session asked the panel to wake it with stands among its background
// work: its observers — commands the host watches for it — and its alarms of
// the panel. The host holds them, not the process of the session, and they
// end with the session. An observer opens the end of its log as a shell opens
// its output, and is stopped while its command runs; an alarm runs nothing to
// read and is cancelled. heldLive counts what of it is still ahead: an
// observer whose command runs, and every alarm, as an alarm the session set
// itself counts.
function heldOf(work) {
    return { observers: (work && work.observers) || [], alarms: (work && work.alarms) || [] };
}

function heldCount(work) {
    const { observers, alarms } = heldOf(work);
    return observers.length + alarms.length;
}

export function heldLive(work) {
    const { observers, alarms } = heldOf(work);
    return observers.filter((o) => !o.done).length + alarms.length;
}

// wakeSay says what of an observer's command wakes the session.
function wakeSay(o) {
    if (o.wake === "exit") return "wakes it when it ends";
    if (o.wake === "match") return `wakes it on a line matching ${o.pattern || "its pattern"}`;
    return "wakes it on every line";
}

function wokeSay(o) {
    return timesSay(o.woken, o.lastWake, "woke it", "has not woken it yet");
}

// timesSay says how often something happened and when last: an observer
// woke its session, an alarm rang.
function timesSay(n, last, did, never) {
    if (!(n > 0)) return never;
    const when = last ? ` ${since(last)} ago` : "";
    if (n === 1) return `${did} once${when}`;
    return `${did} ${n} times${when && `, last${when}`}`;
}

// endSay says how the command of an observer ended, as the session was told.
function endSay(o) {
    const e = o.ending || {};
    const how = e.lost ? "its unit ended with no word of how"
        : e.memory ? "killed at the memory ceiling"
        : e.signal ? `killed by signal ${e.signal}`
        : typeof e.code === "number" ? `exit code ${e.code}` : "ended";
    return o.doneAt ? `${how}, ${since(o.doneAt)} ago` : how;
}

// nextSay says when an alarm rings next. A moment that has passed is a ring
// the executor has not made yet — it was down, or the ring is on its way —
// and the alarm is due.
function nextSay(a) {
    return Date.parse(a.next || "") <= Date.now() ? "due now" : until(a.next);
}

// ringSay says how an alarm rings — once, or every so long — and how often
// it has.
function ringSay(a) {
    return `${a.every > 0 ? `every ${spanSay(a.every)}` : "rings once"} · ${rangSay(a)}`;
}

// spanSay is a span between rings the way a person sets one: 10 min, 1 h,
// 1 h 30 min.
function spanSay(sec) {
    const h = Math.floor(sec / 3600);
    const m = Math.floor((sec % 3600) / 60);
    const s = Math.round(sec % 60);
    return [h > 0 && `${h} h`, m > 0 && `${m} min`, s > 0 && `${s} s`].filter(Boolean).join(" ") || "0 s";
}

function rangSay(a) {
    return timesSay(a.rang, a.lastRing, "rang", "has not rung yet");
}

// running are the ones the session still has in flight. A shell that is over
// stays in the list — its output is readable and the session screen counts it.
// A shell a subagent sent to the background is in the list too: the session
// holds it, and the row names the agent it came from, because a wait started
// by one of five agents is a different thing to stop than a wait of the
// session itself.
function running(tasks) {
    return tasks.filter((task) => !task.done);
}

function taskKind(task) {
    return TASK_KINDS[task && task.kind] || TASK_KINDS.bash;
}

function voice(task) {
    const said = taskVoice(task);
    if (!said) return null;
    return html`
        <span class=${`wvoice${said.hush ? " hush" : ""}`}>
            ${said.silent ? "no events yet" : `event ${since(task.event)} ago`}
        </span>
    `;
}

const AGENT_FADE_MS = 90 * 60 * 1000;

// How an agent sent to the background ended, in words.
const AGENT_ENDS = { completed: "finished", failed: "failed", stopped: "stopped", killed: "gone with the process" };

// agentPhase is where an agent stands: at work; reported — a teammate that
// wrote, and may still be alive; or over, the way the task of an agent sent to
// the background ended. The counter, the list and the rows all ask it.
function agentPhase(agent) {
    if (agent.status === "active") return "live";
    if (agent.status === "reported") return "reported";
    return "over";
}

function splitAgents(agents, now = Date.now()) {
    const live = [];
    const said = [];
    const faded = [];
    const reported = [];
    const over = [];
    for (const agent of agents) {
        const phase = agentPhase(agent);
        if (phase === "live") {
            live.push(agent);
            continue;
        }
        (phase === "reported" ? reported : over).push(agent);
        const at = Date.parse(agent.doneAt || agent.last || agent.reportedAt || "");
        if (Number.isNaN(at) || now - at < AGENT_FADE_MS) said.push(agent);
        else faded.push(agent);
    }
    return { live, said, faded, reported, over };
}

// agentState is the line under the name of an agent: how long it has been at
// work, how it ended, or how long a reported one has been silent.
function agentState(agent, stop) {
    if (stop && stop.done) return "stopped from the panel";
    const phase = agentPhase(agent);
    if (phase === "live") return `working for ${since(agent.at)}`;
    if (phase === "over") {
        const word = AGENT_ENDS[agent.status] || agent.status;
        if (!agent.doneAt) return word;
        const ran = agent.at ? ` · ran ${lasted(agent.at, agent.doneAt)}` : "";
        return `${word} ${since(agent.doneAt)} ago${ran}`;
    }
    const last = agent.last || agent.reportedAt;
    return last ? `silent for ${since(last)}` : "reported, time unknown";
}

function openAgent(agent, onAgent, setPick) {
    if (agent.id && onAgent) {
        onAgent({ id: agent.id, name: agent.name, kind: agent.kind, agent: agent.agent, text: agent.text,
                  model: agent.model, color: agent.color, tokens: agent.tokens,
                  limit: agent.limit, limitKnown: agent.limitKnown });
        return;
    }
    setPick({ kind: "agent", name: agent.name, text: agent.text });
}

function taskSub(tasks, work) {
    const { observers, alarms } = heldOf(work);
    const all = tasks.length + observers.length + alarms.length;
    const live = running(tasks).length + heldLive(work);
    const total = live < all
        ? `${all} ${plural(all, "task", "tasks")}, ${live || "none"} running`
        : `${all} ${plural(all, "task", "tasks")}`;
    const seen = [];
    for (const task of tasks) {
        const [, name] = taskKind(task);
        if (!seen.includes(name)) seen.push(name);
    }
    if (observers.length) seen.push(OBSERVER_WORD);
    if (alarms.length) seen.push(ALARM_WORD);
    if (seen.length < 2) return total;
    return `${total} · ${seen.join(", ")}`;
}

const stoppedWork = new Set();

function workKey(session, id) {
    return `${session}\u0000${id}`;
}

function agentKey(agent) {
    return `${agent.name}\u0000${agent.id || agent.at || ""}`;
}

function canStopTask(exec, task) {
    return knows(exec, "task.stop") && Boolean(task && task.line) && !task.done;
}

function whyStopTask(exec, task) {
    if (!knows(exec, "task.stop")) return whyNot(exec, "task.stop");
    if (task && task.done) return "the work is over, there is nothing to stop";
    if (!task || !task.line) {
        return "the panel does not know how this work is named on the session screen — "
            + "there is nothing here to stop it with";
    }
    return "";
}

function whyStopAgent(exec, agent) {
    if (!knows(exec, "task.stop")) return whyNot(exec, "task.stop");
    if (agentPhase(agent) === "over") return "the agent is over, there is nothing to stop";
    if (!agent.line) {
        return "the panel does not know how this agent is named on the session screen — "
            + "there is nothing here to stop it with";
    }
    return "";
}

function StopButton({ ready, why, busy, done, onStop, label }) {
    if (done) return html`<span class="wstop done" aria-hidden="true"></span>`;
    return html`
        <button class=${`wstop${ready ? "" : " off"}`} type="button"
                disabled=${!ready || busy} title=${ready ? "" : why}
                aria-label=${label} onClick=${onStop}>
            ${Icon.stopsquare()}
        </button>
    `;
}

// sentState is the line under the name of a sent file: its type, size, how
// many times it went and when.
function sentState(file) {
    return [
        fileTag({ name: file.file, media: file.media }),
        file.size > 0 && bytes(file.size),
        file.count > 1 && `${file.count} ${plural(file.count, "delivery", "deliveries")}`,
        file.at && since(file.at),
    ].filter(Boolean).join(" · ");
}

// WorkList renders what stands behind a counter: the tasks, the subagents or
// the artifacts. A sent file the reader cannot open — one that lies outside
// the directory of the conversation — is drawn as a row, not a button: the
// reader holds every file against that directory, and a tap would end in a
// refusal, so the row says where the file lies instead.
export function WorkList({ session, id, kind, work, exec, onAgent, pages, briefs, onPage, onBrief }) {
    const [pick, setPick] = useState(null);
    const run = useAction();
    const [busy, setBusy] = useState("");
    const [fail, setFail] = useState({});
    const [, redraw] = useState(0);

    const stop = async (key, label, send) => {
        if (busy) return;
        setBusy(key);
        setFail((was) => ({ ...was, [key]: "" }));
        const result = await send();
        setBusy("");
        if (!result.ok) {
            if (!result.cancelled) setFail((was) => ({ ...was, [key]: result.error || `${label} not stopped` }));
            return;
        }
        stoppedWork.add(workKey(session, key));
        redraw((n) => n + 1);
    };

    const stopTask = (task) => stop(task.id, "task",
        () => run("task.stop", session, { id: task.id, line: task.line }));
    const stopAgent = (agent) => stop(agentKey(agent), "agent work",
        () => run("agent.stop", session, { id: agent.name }));

    // A teammate is stopped by its name on the session screen. An agent sent
    // to the background has no name there the panel could aim at.
    // An agent sent off to work is a background task to claude: the stream
    // stops it by its id, tmux by its line on the screen of background
    // work. An agent a codex thread started is stopped in its own thread, by
    // its id, while it runs a turn. A run of codex exec has no stop here: it
    // is no task of claude's, and no daemon the panel reaches holds its
    // thread.
    const agentStopper = (agent) => (isCodex(agent) && agent.kind !== "subagent" ? null : isCodex(agent) ? {
        ready: knows(exec, "agent.stop") && agentPhase(agent) === "live",
        why: knows(exec, "agent.stop") ? "the agent runs no turn, there is nothing to stop" : whyNot(exec, "agent.stop"),
        busy: busy === agentKey(agent),
        done: stoppedWork.has(workKey(session, agentKey(agent))),
        fail: fail[agentKey(agent)] || "",
        onStop: () => stop(agentKey(agent), "agent", () => run("agent.stop", session, { id: agent.id })),
    } : agent.kind === "background" ? {
        ready: knows(exec, "task.stop") && agentPhase(agent) !== "over" && Boolean(agent.line),
        why: whyStopAgent(exec, agent),
        busy: busy === agentKey(agent),
        done: stoppedWork.has(workKey(session, agentKey(agent))),
        fail: fail[agentKey(agent)] || "",
        onStop: () => stop(agentKey(agent), "agent",
            () => run("task.stop", session, { id: agent.id, line: agent.line })),
    } : {
        ready: knows(exec, "agent.stop"),
        why: whyNot(exec, "agent.stop"),
        busy: busy === agentKey(agent),
        done: stoppedWork.has(workKey(session, agentKey(agent))),
        fail: fail[agentKey(agent)] || "",
        onStop: () => stopAgent(agent),
    });
    // What the conversation made comes from two places at once: the window of
    // the feed, and the shelf of copies which keeps what fell out of it.
    const made = kind === "arts" ? merge((work && work.artifacts) || [], pages || []) : [];
    const [shown, setShown] = useState(PAGE);
    const at = kind === "briefs" ? (briefs || []) : made;
    const visible = at.slice(0, shown);

    // An observer is stopped while its command runs, an alarm cancelled while
    // it is there: by the id the panel read it under, on the screen of the
    // session it belongs to.
    const heldStopper = (key, kind, label, item, ready, why) => ({
        ready: knows(exec, kind) && ready,
        why: knows(exec, kind) ? why : whyNot(exec, kind),
        busy: busy === key,
        done: stoppedWork.has(workKey(session, key)),
        fail: fail[key] || "",
        onStop: () => stop(key, label, () => run(kind, session, { id: item.id, name: item.name })),
    });
    const observerStopper = (o) => heldStopper(`observer-${o.id}`, "observer.stop", "observer", o, !o.done,
        "the command is over, there is nothing to stop");
    const alarmCanceller = (a) => heldStopper(`alarm-${a.id}`, "alarm.cancel", "alarm", a, true, "");

    const tasks = (work && work.tasks) || [];
    const { observers, alarms } = heldOf(work);
    const agents = (work && work.agents) || [];
    const flows = (work && work.workflows) || [];
    const { live, said, faded, reported, over } = splitAgents(agents);
    const [showFaded, setShowFaded] = useState(false);

    // A run is read out of what the list already holds: everything the panel
    // knows about it came down with the state, and there is nothing to ask
    // the host for a second time.
    if (pick && pick.kind === "workflow") {
        return html`<${FlowRun} flow=${pick.flow} onBack=${() => setPick(null)} />`;
    }
    if (pick) {
        return html`<${Look} session=${session} id=${id} look=${pick}
                             onBack=${() => setPick(null)} />`;
    }

    const asking = (briefs || []).filter((c) => c && !c.sent).length;
    const liveFlows = flows.filter((f) => f.status === "running");
    const sub = kind === "workflows"
        ? [
            liveFlows.length > 0 && `${liveFlows.length} running`,
            flows.length > liveFlows.length && `${flows.length - liveFlows.length} over`,
        ].filter(Boolean).join(", ") || "empty"
        : kind === "tasks"
        ? taskSub(tasks, work)
        : kind === "arts"
            ? (made.length > 0 ? `${made.length} published` : "empty")
            : kind === "briefs"
            ? [
                asking > 0 && `${asking} waiting`,
                (briefs || []).length > asking && `${(briefs || []).length - asking} sent`,
            ].filter(Boolean).join(", ") || "empty"
            : [
                live.length > 0 && `${live.length} working`,
                reported.length > 0 && `${reported.length} reported`,
                over.length > 0 && `${over.length} over`,
            ].filter(Boolean).join(", ") || "empty";

    return html`
        <div class="sheethead">
            <div class="chatwho">
                <h2>${LOOK_NAMES[kind]}</h2>
                <div class="chatsub"><span>${sub}</span></div>
            </div>
        </div>

        <div class="worklist">
            ${kind === "tasks" && tasks.map((task) => (ALARMS.has(task.kind)
                ? html`
                    <div class="wrow task still" key=${task.id}>
                        <span class="wicon">${taskKind(task)[0]()}</span>
                        <span class="wtext">
                            ${task.text}
                            <span class="wkind">${alarmWord(task)}</span>
                        </span>
                        <span class="wage">${alarmWhen(task)}</span>
                    </div>
                `
                : html`
                    <div class="wline" key=${task.id}>
                        <button class="wrow task" type="button"
                                onClick=${() => setPick({ kind: "task", id: task.id, text: task.text })}>
                            <span class="wicon">${taskKind(task)[0]()}</span>
                            <span class="wtext">
                                ${task.text}
                                <span class="wkind">${taskKind(task)[1]}</span>
                                ${task.agent && html`<span class="wwhose">${task.agent}</span>`}
                                ${voice(task)}
                                ${task.done
                                    ? html`<span class="wkind gone">over</span>`
                                    : stoppedWork.has(workKey(session, task.id))
                                    && html`<span class="wkind gone">stopped</span>`}
                                ${fail[task.id] && html`<span class="wfail">${fail[task.id]}</span>`}
                            </span>
                            ${task.at && html`<span class="wage">
                                ${task.done && task.doneAt ? lasted(task.at, task.doneAt) : since(task.at)}
                            </span>`}
                            <span class="crgo">${Icon.chevron()}</span>
                        </button>
                        <${StopButton} ready=${canStopTask(exec, task)}
                                       why=${whyStopTask(exec, task)}
                                       busy=${busy === task.id}
                                       done=${stoppedWork.has(workKey(session, task.id))}
                                       label=${`stop: ${task.text}`}
                                       onStop=${() => stopTask(task)} />
                    </div>
                `))}
            ${kind === "tasks" && observers.map((o) => html`
                <${ObserverRow} key=${`observer-${o.id}`} observer=${o} stop=${observerStopper(o)}
                                onOpen=${() => setPick({ kind: "observer", id: o.id, text: o.name })} />
            `)}
            ${kind === "tasks" && alarms.map((a) => html`
                <${AlarmRow} key=${`alarm-${a.id}`} alarm=${a} cancel=${alarmCanceller(a)} />
            `)}
            ${kind === "agents" && live.map((agent) => html`
                <${AgentRow} key=${`live-${agentKey(agent)}`} agent=${agent} reported=${false}
                             stop=${agentStopper(agent)}
                             onOpen=${() => openAgent(agent, onAgent, setPick)} />
            `)}
            ${kind === "agents" && said.map((agent) => html`
                <${AgentRow} key=${`said-${agentKey(agent)}`} agent=${agent} reported=${true}
                             stop=${agentStopper(agent)}
                             onOpen=${() => openAgent(agent, onAgent, setPick)} />
            `)}
            ${kind === "agents" && faded.length > 0 && !showFaded && html`
                <button class="wrow more" type="button" onClick=${() => setShowFaded(true)}>
                    <span class="wtext">${faded.length} more, quiet for a while</span>
                    <span class="crgo">${Icon.chevron()}</span>
                </button>
            `}
            ${kind === "agents" && showFaded && faded.map((agent) => html`
                <${AgentRow} key=${`faded-${agentKey(agent)}`} agent=${agent} reported=${true}
                             stop=${agentStopper(agent)}
                             onOpen=${() => openAgent(agent, onAgent, setPick)} />
            `)}
            ${kind === "agents" && reported.length > 0 && (said.length > 0 || showFaded) && html`
                <p class="whint">Reported means it sent a letter. Whether it has finished
                    for good, the session does not say.</p>
            `}
            ${kind === "arts" && visible.map((art) => html`
                <${ArtifactCard} key=${art.url || art.file || art.title} item=${art}
                                 copy=${art.kept} onOpen=${onPage}
                                 onSeen=${() => { markOpened(pageKey(art)); redraw((n) => n + 1); }} />
            `)}
            ${kind === "arts" && made.length === 0 && html`
                <p class="hint">No artifacts were published in this conversation.</p>
            `}
            ${kind === "briefs" && visible.map((card) => html`
                <${BriefCard} key=${card.id} item=${briefRow(card)} onOpen=${onBrief} />
            `)}
            ${kind === "briefs" && (briefs || []).length === 0 && html`
                <p class="hint">This conversation published no briefs.</p>
            `}
            ${(kind === "arts" || kind === "briefs") && at.length > shown && html`
                <button type="button" class="wmore" onClick=${() => setShown((n) => n + PAGE)}>
                    ${at.length - shown} more
                </button>
            `}
            ${kind === "workflows" && flows.map((flow) => html`
                <${FlowRow} key=${flow.id} flow=${flow}
                            onOpen=${(run) => setPick({ kind: "workflow", flow: run, text: flowTitle(run) })} />
            `)}
            ${kind === "workflows" && flows.length === 0 && html`
                <p class="hint">This conversation launched no workflows. A workflow is a script
                    that runs agents in a set order — the session starts one when the work is
                    wide enough to fan out.</p>
            `}
            ${kind === "tasks" && tasks.length + observers.length + alarms.length === 0
                && html`<p class="hint">There are no background commands.</p>`}
            ${kind === "agents" && agents.length === 0 && html`<p class="hint">There were no subagents in this conversation.</p>`}
        </div>
    `;
}

function AgentRow({ agent, reported, stop, onOpen }) {
    return html`
      <div class="wline tall">
        <button class=${`wrow agent${reported ? " reported" : ""}`} type="button" onClick=${onOpen}>
            <span class="wicon" style=${agent.color ? `color:var(--tm-${agent.color})` : ""}>
                ${Icon.robot()}
            </span>
            <span class="wcol">
                <span class="wname">
                    ${agent.name}
                    ${isCodex(agent) && html`<span class="agentword" data-agent=${whoKey(agent)}>${agentName(agent)}</span>`}
                    ${agent.model && html`<span class="wmodel">${agent.model}</span>`}
                </span>
                ${agent.text && html`<span class="wsub">${agent.text}</span>`}
                <span class="wstate">${agentState(agent, stop)}${agent.tokens > 0
                    && ` · ${contextSay(agent)}`}</span>
                ${stop && stop.fail && html`<span class="wfail">${stop.fail}</span>`}
            </span>
            <span class="crgo">${Icon.chevron()}</span>
        </button>
        ${stop && html`
            <${StopButton} ready=${stop.ready} why=${stop.why} busy=${stop.busy}
                           done=${stop.done} label=${`stop agent ${agent.name}`}
                           onStop=${stop.onStop} />
        `}
      </div>
    `;
}

// The words of the two kinds of what a session asked the panel for, on their
// rows and in the head of the list: an alarm of the panel stands apart from
// the alarm the session's own cron keeps.
const OBSERVER_WORD = "observer";
const ALARM_WORD = "panel alarm";

// ObserverRow is an observer: its name, the command it watches, what of the
// command wakes the session and when it last did, or how the command ended.
// A tap opens the end of its log.
function ObserverRow({ observer: o, stop, onOpen }) {
    const state = o.done
        ? `the command ended: ${endSay(o)} · ${wokeSay(o)}`
        : `${wakeSay(o)} · ${wokeSay(o)}`;
    return html`
      <div class="wline tall">
        <button class="wrow held" data-held="observer" type="button" onClick=${onOpen}>
            <span class="wicon">${Icon.eye()}</span>
            <span class="wcol">
                <span class="wname">
                    ${o.name}
                    <span class="wkind">${OBSERVER_WORD}</span>
                    ${stop.done && html`<span class="wkind gone">stopped</span>`}
                </span>
                <span class="wcmd">${o.command}</span>
                <span class="wstate">${state}</span>
                ${stop.fail && html`<span class="wfail">${stop.fail}</span>`}
            </span>
            ${o.at && html`<span class="wage">${o.done && o.doneAt ? lasted(o.at, o.doneAt) : since(o.at)}</span>`}
            <span class="crgo">${Icon.chevron()}</span>
        </button>
        <${StopButton} ready=${stop.ready} why=${stop.why} busy=${stop.busy} done=${stop.done}
                       label=${`stop observer ${o.name}`} onStop=${stop.onStop} />
      </div>
    `;
}

// AlarmRow is an alarm of the panel: its name, the note the session left
// itself, when it rings and how often it has. It runs nothing, so it opens
// nothing; it is cancelled.
function AlarmRow({ alarm: a, cancel }) {
    return html`
      <div class="wline tall">
        <div class="wrow held" data-held="alarm">
            <span class="wicon">${Icon.alerts()}</span>
            <span class="wcol">
                <span class="wname">
                    ${a.name}
                    <span class="wkind">${ALARM_WORD}</span>
                    ${cancel.done && html`<span class="wkind gone">cancelled</span>`}
                </span>
                ${a.note && html`<span class="wsub">${a.note}</span>`}
                <span class="wstate">${ringSay(a)}</span>
                ${cancel.fail && html`<span class="wfail">${cancel.fail}</span>`}
            </span>
            <span class="wage">${nextSay(a)}</span>
        </div>
        <${StopButton} ready=${cancel.ready} why=${cancel.why} busy=${cancel.busy} done=${cancel.done}
                       label=${`cancel alarm ${a.name}`} onStop=${cancel.onStop} />
      </div>
    `;
}

// contextSay names the context of an agent against the window of its model.
// The window is a guess for a model the catalog does not know, and a guess
// is marked as one.
export function contextSay(agent) {
    if (!agent.limit) return `${tokens(agent.tokens)} of context`;
    return `${tokens(agent.tokens)} of ${agent.limitKnown ? "" : "~"}${tokens(agent.limit)}`;
}

// waitedOn is what a session waits on once its own turn is over: the agents
// still at work or, with none, the workflow runs, and since when the first of
// them has been at it. Null when nothing the panel sees is at work.
export function waitedOn(work) {
    const { agents, flows } = liveWork(work);
    const list = agents.length > 0 ? agents : flows;
    const n = list.length;
    if (n === 0) return null;
    const starts = list.map((one) => Date.parse(one.at || "")).filter(Number.isFinite);
    return {
        kind: agents.length > 0 ? "agents" : "workflows",
        count: n,
        word: agents.length > 0
            ? `${n} ${plural(n, "agent", "agents")} working`
            : `${n} ${plural(n, "workflow", "workflows")} running`,
        since: starts.length > 0 ? Math.min(...starts) : NaN,
    };
}

// hasWork reports whether there is anything to put above the composer.
// liveWork is what a session has running inside its process right now: what
// stops when the process ends, whatever comes back with the conversation.
export function liveWork(work) {
    const { live } = splitAgents((work && work.agents) || []);
    return {
        tasks: running((work && work.tasks) || []),
        agents: live,
        flows: ((work && work.workflows) || []).filter((f) => f.status === "running"),
    };
}

export function hasWork(work, busy) {
    return Boolean(busy);
}
