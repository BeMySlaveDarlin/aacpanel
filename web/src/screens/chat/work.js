// What a session has in flight: the status bar, the counters and their lists.

import { useState } from "preact/hooks";

import { html } from "../../html.js";
import { Icon } from "../../ui/icons.js";
import { bytes, plural, since, tokens, until } from "../../format.js";
import { useAction } from "../../actions/gate.js";
import { knows, whyNot } from "../../exec.js";
import { taskVoice } from "./voice.js";
import { ArtifactCard, BriefCard } from "./rows.js";
import { fileTag } from "./files.js";
import { Look, LOOK_NAMES } from "./look.js";
import { merge } from "../../data/artifacts.js";

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
        eyebrow: card.eyebrow || (card.sent ? "answered and sent" : ""),
        questions: card.questions || 0,
    };
}

export function Work({ work, onOpen }) {
    const tasks = (work && work.tasks) || [];
    const agents = (work && work.agents) || [];
    const { live } = splitAgents(agents);
    const liveTasks = running(tasks);

    return html`
        <div class="wchips">
            <button class=${`wchip${liveTasks.length > 0 ? "" : " idle"}`} type="button"
                    onClick=${() => onOpen({ kind: "tasks" })}
                    aria-label=${taskLabel(tasks)}>
                ${Icon.clock()}${liveTasks.length > 0 && html`<span class="wnum">${liveTasks.length}</span>`}
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

// WorkRefs renders the right half of the row: the artifacts. An artifact has no
// "over" — what the session made stays made, and a file it sent stays sent —
// so the number counts them all and, like the other chips, is absent rather
// than 0.
export function WorkRefs({ work, onOpen }) {
    const arts = (work && work.artifacts) || [];
    const docs = (work && work.docs) || [];
    const sent = (work && work.sent) || [];
    const refs = arts.length + docs.length + sent.length;

    return html`
        <button class=${`wchip${refs > 0 ? "" : " idle"}`} type="button"
                onClick=${() => onOpen({ kind: "arts" })}
                aria-label=${refs > 0 ? `artifacts: ${refs}` : "artifacts: none"}>
            ${Icon.artifact()}${refs > 0 && html`<span class="wnum">${refs}</span>`}
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
                  model: agent.model, color: agent.color, tokens: agent.tokens,
                  limit: agent.limit, limitKnown: agent.limitKnown });
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

    const agentStopper = (agent) => ({
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
    const [tab, setTab] = useState("made");
    const [shown, setShown] = useState(PAGE);
    const at = kind === "arts" && tab === "briefs" ? (briefs || []) : made;
    const visible = at.slice(0, shown);

    const tasks = (work && work.tasks) || [];
    const agents = (work && work.agents) || [];
    const arts = (work && work.artifacts) || [];
    const docs = (work && work.docs) || [];
    const sent = (work && work.sent) || [];
    const { live, said, faded } = splitAgents(agents);
    const [showFaded, setShowFaded] = useState(false);

    if (pick) {
        return html`<${Look} session=${session} id=${id} look=${pick}
                             onBack=${() => setPick(null)} />`;
    }

    const sub = kind === "tasks"
        ? taskSub(tasks)
        : kind === "arts"
            ? [
                arts.length > 0 && `${arts.length} published`,
                docs.length > 0 && `${docs.length} ${plural(docs.length, "document", "documents")}`,
                sent.length > 0 && `${sent.length} sent`,
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
            ${kind === "arts" && html`
                <div class="worktabs">
                    <button type="button" class="chip" aria-pressed=${tab === "made" ? "true" : "false"}
                            onClick=${() => { setTab("made"); setShown(PAGE); }}>
                        published${made.length > 0 ? ` · ${made.length}` : ""}
                    </button>
                    <button type="button" class="chip" aria-pressed=${tab === "briefs" ? "true" : "false"}
                            onClick=${() => { setTab("briefs"); setShown(PAGE); }}>
                        briefs${(briefs || []).length > 0 ? ` · ${briefs.length}` : ""}
                    </button>
                </div>
            `}
            ${kind === "arts" && tab === "made" && visible.map((art) => html`
                <${ArtifactCard} key=${art.url || art.file || art.title} item=${art}
                                 copy=${art.kept} onOpen=${onPage} />
            `)}
            ${kind === "arts" && tab === "made" && made.length === 0 && html`
                <p class="hint">No artifacts were published in this conversation.</p>
            `}
            ${kind === "arts" && tab === "briefs" && visible.map((card) => html`
                <${BriefCard} key=${card.id} item=${briefRow(card)} onOpen=${onBrief} />
            `)}
            ${kind === "arts" && tab === "briefs" && (briefs || []).length === 0 && html`
                <p class="hint">This conversation published no briefs.</p>
            `}
            ${kind === "arts" && at.length > shown && html`
                <button type="button" class="wmore" onClick=${() => setShown((n) => n + PAGE)}>
                    ${at.length - shown} more
                </button>
            `}
            ${kind === "arts" && tab === "made" && docs.length > 0 && html`<div class="callcap">documents</div>`}
            ${kind === "arts" && tab === "made" && docs.map((doc) => html`
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
            ${kind === "arts" && tab === "made" && docs.length === 0 && html`
                <p class="hint">The session wrote no documents.</p>
            `}
            ${kind === "arts" && tab === "made" && sent.length > 0 && html`<div class="callcap">sent to you</div>`}
            ${kind === "arts" && tab === "made" && sent.map((file) => (file.outside
                ? html`
                    <div class="wrow doc outside" key=${file.path}>
                        <span class="wicon">${Icon.file()}</span>
                        <span class="wcol">
                            <span class="wname">${file.file}</span>
                            <span class="wstate">${sentState(file)}</span>
                            <span class="wnote">outside the conversation directory</span>
                        </span>
                    </div>
                `
                : html`
                    <button class="wrow doc" type="button" key=${file.path}
                            onClick=${() => setPick({ kind: "file", path: file.path, text: file.path })}>
                        <span class="wicon">${Icon.file()}</span>
                        <span class="wcol">
                            <span class="wname">${file.file}</span>
                            <span class="wstate">${sentState(file)}</span>
                        </span>
                        <span class="crgo">${Icon.chevron()}</span>
                    </button>
                `))}
            ${kind === "arts" && tab === "made" && sent.length === 0 && html`
                <p class="hint">The session sent no files.</p>
            `}
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
                        : `working for ${since(agent.at)}`}${agent.tokens > 0
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

// contextSay names the context of an agent against the window of its model.
// The window is a guess for a model the catalog does not know, and a guess
// is marked as one.
export function contextSay(agent) {
    if (!agent.limit) return `${tokens(agent.tokens)} of context`;
    return `${tokens(agent.tokens)} of ${agent.limitKnown ? "" : "~"}${tokens(agent.limit)}`;
}

// hasWork reports whether there is anything to put above the composer.
export function hasWork(work, busy) {
    return Boolean(busy);
}
