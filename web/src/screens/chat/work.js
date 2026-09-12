// What a session has in flight: the status bar, the counters and their lists.

import { useState } from "preact/hooks";

import { html } from "../../html.js";
import { Icon } from "../../ui/icons.js";
import { plural, since, until } from "../../format.js";
import { useAction } from "../../actions/gate.js";
import { knows, whyNot } from "../../exec.js";
import { taskVoice } from "./voice.js";
import { ArtifactCard } from "./rows.js";
import { Look, LOOK_NAMES } from "./look.js";

// WorkStatus renders what is happening to the session right now.
export function WorkStatus({ work, busy }) {
    if (!busy) return null;
    return html`
        <div class="workbar status">
            <div class="workhead"><span class="working">handling the request</span></div>
        </div>
    `;
}

// Work renders the counters of what the session has in flight. Every chip stands
// in the row whatever the session holds: a row that grows and shrinks under the
// thumb moves the button the thumb was already going for, and a group that
// disappears when it is empty leaves a hole between the last chip and the edge.
// An empty chip is dim and still opens its list — the list says out loud that
// there is nothing, which a button that refuses the tap cannot do.
export function Work({ work, onOpen }) {
    const tasks = (work && work.tasks) || [];
    const agents = (work && work.agents) || [];
    const { live } = splitAgents(agents);

    return html`
        <div class="wchips">
            <button class=${`wchip${running(tasks).length > 0 ? "" : " idle"}`} type="button"
                    onClick=${() => onOpen({ kind: "tasks" })}
                    aria-label=${taskLabel(tasks)}>
                ${Icon.clock()}<span class="wnum">${tasks.length}</span>
            </button>
            <button class=${`wchip${live.length > 0 ? "" : " idle"}`} type="button"
                    onClick=${() => onOpen({ kind: "agents" })}
                    aria-label=${agentLabel(agents, live)}>
                ${Icon.robot()}${live.length > 0 && html`<span class="wnum">${live.length}</span>`}
            </button>
        </div>
    `;
}

function taskLabel(tasks) {
    if (!tasks.length) return "background work: none";
    return `background work: ${tasks.length}, ${running(tasks).length || "none"} running`;
}

function agentLabel(agents, live) {
    if (!agents.length) return "subagents: none";
    return live.length > 0 ? `subagents: ${live.length} working` : "subagents: none working";
}

// WorkRefs renders the right half of the row: the plan and the artifacts.
export function WorkRefs({ work, onOpen }) {
    const plan = (work && work.plan) || [];
    const arts = (work && work.artifacts) || [];
    const docs = (work && work.docs) || [];
    const done = plan.filter((p) => p.status === "completed").length;
    const refs = arts.length + docs.length;

    return html`
        <button class=${`wchip${plan.length > 0 ? "" : " idle"}`} type="button"
                onClick=${() => onOpen({ kind: "plan" })}
                aria-label=${plan.length > 0 ? `plan: ${done} of ${plan.length}` : "plan: empty"}>
            ${Icon.list()}<span class="wnum pair">${done}/${plan.length}</span>
        </button>
        <button class=${`wchip${refs > 0 ? "" : " idle"}`} type="button"
                onClick=${() => onOpen({ kind: "arts" })}
                aria-label=${refs > 0 ? `artifacts: ${refs}` : "artifacts: none"}>
            ${Icon.artifact()}<span class="wnum">${refs}</span>
        </button>
    `;
}

const TASK_KINDS = {
    bash: [Icon.terminal, "command"],
    aacpanel: [Icon.probes, "monitoring"],
    agent: [Icon.robot, "background agent"],
    wake: [Icon.alerts, "wake-up"],
};

// running are the ones the session still has in flight. A shell that is over
// stays in the list — its output is readable and the session screen counts it.
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

function splitAgents(agents, now = Date.now()) {
    const live = [];
    const said = [];
    const faded = [];
    for (const agent of agents) {
        if (agent.status !== "reported") {
            live.push(agent);
            continue;
        }
        const at = Date.parse(agent.last || agent.reportedAt || "");
        if (Number.isNaN(at) || now - at < AGENT_FADE_MS) said.push(agent);
        else faded.push(agent);
    }
    return { live, said, faded };
}

function openAgent(agent, onAgent, setPick) {
    if (agent.id && onAgent) {
        onAgent({ id: agent.id, name: agent.name, kind: agent.kind, text: agent.text,
                  model: agent.model, color: agent.color });
        return;
    }
    setPick({ kind: "agent", name: agent.name, text: agent.text });
}

function taskSub(tasks) {
    const live = running(tasks).length;
    const total = live < tasks.length
        ? `${tasks.length} ${plural(tasks.length, "task", "tasks")}, ${live || "none"} running`
        : `${tasks.length} ${plural(tasks.length, "task", "tasks")}`;
    const seen = [];
    for (const task of tasks) {
        const [, name] = taskKind(task);
        if (!seen.includes(name)) seen.push(name);
    }
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

// WorkList renders what stands behind a counter: the plan, the tasks or the subagents.
export function WorkList({ session, id, kind, work, exec, onAgent }) {
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

    const agentStopper = (agent) => ({
        ready: knows(exec, "agent.stop"),
        why: whyNot(exec, "agent.stop"),
        busy: busy === agentKey(agent),
        done: stoppedWork.has(workKey(session, agentKey(agent))),
        fail: fail[agentKey(agent)] || "",
        onStop: () => stopAgent(agent),
    });
    const plan = (work && work.plan) || [];
    const tasks = (work && work.tasks) || [];
    const agents = (work && work.agents) || [];
    const arts = (work && work.artifacts) || [];
    const docs = (work && work.docs) || [];
    const { live, said, faded } = splitAgents(agents);
    const [showFaded, setShowFaded] = useState(false);

    if (pick) {
        return html`<${Look} session=${session} id=${id} look=${pick}
                             onBack=${() => setPick(null)} />`;
    }

    const sub = kind === "plan"
        ? `${plan.filter((p) => p.status === "completed").length} of ${plan.length}`
        : kind === "tasks"
            ? taskSub(tasks)
            : kind === "arts"
                ? [
                    arts.length > 0 && `${arts.length} published`,
                    docs.length > 0 && `${docs.length} ${plural(docs.length, "document", "documents")}`,
                ].filter(Boolean).join(", ") || "empty"
                : [
                    live.length > 0 && `${live.length} working`,
                    said.length > 0 && `${said.length} reported`,
                ].filter(Boolean).join(", ") || "empty";

    return html`
        <div class="sheethead">
            <div class="chatwho">
                <h2>${LOOK_NAMES[kind]}</h2>
                <div class="chatsub"><span>${sub}</span></div>
            </div>
        </div>

        <div class="worklist">
            ${kind === "plan" && plan.map((item, n) => html`
                <div class=${`wrow ${item.status}`} key=${`p${n}`}>
                    <span class="wmark"></span>
                    <span class="wtext">${item.text}</span>
                </div>
            `)}
            ${kind === "tasks" && tasks.map((task) => (task.kind === "wake"
                ? html`
                    <div class="wrow task still" key=${task.id}>
                        <span class="wicon">${taskKind(task)[0]()}</span>
                        <span class="wtext">
                            ${task.text}
                            <span class="wkind">${taskKind(task)[1]}</span>
                        </span>
                        <span class="wage">${task.due ? until(task.due) : since(task.at)}</span>
                    </div>
                `
                : html`
                    <div class="wline" key=${task.id}>
                        <button class="wrow task" type="button"
                                onClick=${() => (task.kind === "agent" && onAgent
                                    ? onAgent({ id: task.id, name: task.text || "background agent", kind: "task" })
                                    : setPick({ kind: "task", id: task.id, text: task.text }))}>
                            <span class="wicon">${taskKind(task)[0]()}</span>
                            <span class="wtext">
                                ${task.text}
                                <span class="wkind">${taskKind(task)[1]}</span>
                                ${voice(task)}
                                ${task.done
                                    ? html`<span class="wkind gone">over</span>`
                                    : stoppedWork.has(workKey(session, task.id))
                                    && html`<span class="wkind gone">stopped</span>`}
                                ${fail[task.id] && html`<span class="wfail">${fail[task.id]}</span>`}
                            </span>
                            ${task.at && html`<span class="wage">${since(task.at)}</span>`}
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
            ${kind === "agents" && live.map((agent) => html`
                <${AgentRow} key=${`live-${agent.name}`} agent=${agent} reported=${false}
                             stop=${agentStopper(agent)}
                             onOpen=${() => openAgent(agent, onAgent, setPick)} />
            `)}
            ${kind === "agents" && said.map((agent) => html`
                <${AgentRow} key=${`said-${agent.name}`} agent=${agent} reported=${true}
                             stop=${agentStopper(agent)}
                             onOpen=${() => openAgent(agent, onAgent, setPick)} />
            `)}
            ${kind === "agents" && faded.length > 0 && !showFaded && html`
                <button class="wrow more" type="button" onClick=${() => setShowFaded(true)}>
                    <span class="wtext">${faded.length} more reported earlier</span>
                    <span class="crgo">${Icon.chevron()}</span>
                </button>
            `}
            ${kind === "agents" && showFaded && faded.map((agent) => html`
                <${AgentRow} key=${`faded-${agent.name}`} agent=${agent} reported=${true}
                             stop=${agentStopper(agent)}
                             onOpen=${() => openAgent(agent, onAgent, setPick)} />
            `)}
            ${kind === "agents" && (said.length > 0 || showFaded) && html`
                <p class="whint">Reported means it sent a letter. Whether it has finished
                    for good, the session does not say.</p>
            `}
            ${kind === "arts" && arts.length > 0 && html`<div class="callcap">published</div>`}
            ${kind === "arts" && arts.map((art) => html`
                <${ArtifactCard} key=${art.path || art.title} item=${art} />
            `)}
            ${kind === "arts" && arts.length === 0 && html`
                <p class="hint">No artifacts were published in this conversation.</p>
            `}
            ${kind === "arts" && docs.length > 0 && html`<div class="callcap">documents</div>`}
            ${kind === "arts" && docs.map((doc) => html`
                <button class="wrow doc" type="button" key=${doc.path}
                        onClick=${() => setPick({ kind: "file", path: doc.path,
                                                  text: doc.dir ? `${doc.dir}/${doc.file}` : doc.file })}>
                    <span class="wicon">${Icon.file()}</span>
                    <span class="wcol">
                        <span class="wname">${doc.file}</span>
                        <span class="wstate">${[
                            doc.dir,
                            `${doc.count} ${plural(doc.count, "edit", "edits")}`,
                            doc.at && since(doc.at),
                        ].filter(Boolean).join(" · ")}</span>
                    </span>
                    <span class="crgo">${Icon.chevron()}</span>
                </button>
            `)}
            ${kind === "arts" && docs.length === 0 && html`
                <p class="hint">The session wrote no documents.</p>
            `}
            ${kind === "plan" && plan.length === 0 && html`<p class="hint">The plan is empty.</p>`}
            ${kind === "tasks" && tasks.length === 0 && html`<p class="hint">There are no background commands.</p>`}
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
                    ${agent.model && html`<span class="wmodel">${agent.model}</span>`}
                </span>
                ${agent.text && html`<span class="wsub">${agent.text}</span>`}
                <span class="wstate">${stop && stop.done
                    ? "stopped from the panel"
                    : reported
                        ? (agent.last || agent.reportedAt
                            ? `silent for ${since(agent.last || agent.reportedAt)}`
                            : "reported, time unknown")
                        : `working for ${since(agent.at)}`}</span>
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

// hasWork reports whether there is anything to put above the composer.
export function hasWork(work, busy) {
    return Boolean(busy);
}
