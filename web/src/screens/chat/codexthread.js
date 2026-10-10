// What is done to a codex thread as a whole, behind the word Thread at the end
// of its band: compact it, have it reviewed, rename it, and give it a goal to
// work towards. And what it left running: its background processes, counted
// under the composer and listed with a stop for each.

import { useEffect, useState } from "preact/hooks";

import { html } from "../../html.js";
import { useAction } from "../../actions/gate.js";
import { useBackClose } from "../../ui/back.js";
import { knows, whyNot } from "../../exec.js";
import { share, tokens } from "../../format.js";
import { Icon } from "../../ui/icons.js";
import { shownName } from "../../agent.js";
import { shortPath } from "./head.js";
import { PickRow } from "./picker.js";

// goalSay sums a goal up in one line: what it is, how it stands and what it
// has spent of its budget.
export function goalSay(goal) {
    if (!goal || !goal.objective) return "";
    const spent = goal.tokenBudget
        ? `${tokens(goal.tokensUsed || 0)} of ${tokens(goal.tokenBudget)}`
        : `${tokens(goal.tokensUsed || 0)} spent`;
    return [goal.objective, GOAL_WORDS[goal.status] || goal.status, spent].filter(Boolean).join(" · ");
}

// GOAL_WORDS say how a goal stands in the words of a person, wherever a goal
// is shown: the thread, its pane and the line of the goal in the feed.
export const GOAL_WORDS = {
    active: "running",
    paused: "paused",
    blocked: "blocked",
    usageLimited: "stopped by the limit",
    budgetLimited: "budget spent",
    complete: "met",
};

// The rows of the thread: each says what it does, or what stands in it now.
export function threadRows(live, pct) {
    const goal = live.goal && live.goal.objective ? live.goal : null;
    const busy = live.status === "busy";
    return [
        { kind: "compact", icon: Icon.wrap, name: "Compact",
          desc: pct == null ? "retell the conversation to free the context" : `${share(pct)} of the context used` },
        { kind: "review", icon: Icon.search, name: "Review", more: true,
          desc: busy ? "when the turn under way ends" : "changes, a branch, a commit, your words", off: busy },
        { kind: "rename", icon: Icon.pencil, name: "Rename", more: true, desc: shownName(live) },
        { kind: "goal", icon: Icon.pin, name: "Goal", more: true, desc: goal ? goalSay(goal) : "none set", value: Boolean(goal) },
    ];
}

// ThreadRow is a row of the thread on a phone: a round icon, its words and
// a chevron where it opens a pane of its own.
function ThreadRow({ row, onPress }) {
    return html`
        <button type="button" class=${`pkrow${row.more ? " pkmore" : ""}`} data-thread=${row.kind}
                disabled=${Boolean(row.off)} onClick=${onPress}>
            <span class="pkround">${row.icon()}</span>
            <span class="pkbody">
                <span class="pkname">${row.name}</span>
                <span class=${`pkdesc${row.value ? " pkval" : ""}`}>${row.desc}</span>
            </span>
            ${row.more && html`<span class="crgo">${Icon.chevron()}</span>`}
        </button>
    `;
}

// useCompact asks the gate for the compaction: it costs the details of the
// beginning, and the sheet of the gate says so.
export function useCompact(name) {
    const run = useAction();
    return () => run("session.command", name, { command: "compact" });
}

// ThreadPane is the phone's list of the thread.
export function ThreadPane({ live, pct, onPane, onCompact }) {
    return html`
        <div class="shead pkhead">
            <div class="pktitles">
                <span class="stitle">The thread</span>
                <span class="ssub">What to do with the conversation</span>
            </div>
        </div>
        <div class="pklist">
            ${threadRows(live, pct).map((row) => html`
                <${ThreadRow} key=${row.kind} row=${row}
                              onPress=${() => (row.kind === "compact" ? onCompact() : onPane(row.kind))} />
            `)}
        </div>
    `;
}

// ThreadMenu is the wide screen's list of the thread, over the composer.
export function ThreadMenu({ live, pct, onPick }) {
    return html`
        <div class="pkmenu left cxmenu" role="menu" aria-label="the thread">
            <div class="pkmenuhead">The thread</div>
            ${threadRows(live, pct).map((row, i) => html`
                <${PickRow} key=${row.kind} name=${row.name} desc=${row.desc} number=${i + 1} disabled=${Boolean(row.off)}
                            onPick=${() => onPick(row.kind)} />
            `)}
        </div>
    `;
}

// Back is the head of a pane opened from the list of the thread. The gesture
// back takes the pane off and leaves the list, not the whole sheet.
function Back({ title, sub, onBack }) {
    useBackClose(Boolean(onBack), onBack || (() => {}));
    return html`
        <div class="shead pkhead">
            ${onBack && html`<button class="iconbtn pkback" type="button" aria-label="back to the thread"
                                     onClick=${onBack}><span class="chev back">${Icon.chevron()}</span></button>`}
            <div class="pktitles">
                <span class="stitle">${title}</span>
                ${sub && html`<span class="ssub">${sub}</span>`}
            </div>
        </div>
    `;
}

const TARGETS = [
    { value: "uncommitted", name: "Uncommitted changes", icon: Icon.pencil },
    { value: "branch", name: "Against a branch", icon: Icon.flow },
    { value: "commit", name: "A commit", icon: Icon.check },
    { value: "custom", name: "Your own instructions", icon: Icon.quote },
];

const HEX = /^[0-9a-f]{4,64}$/i;

// reviewOf is what a review sends, or why it cannot be sent yet.
export function reviewOf(target, fields) {
    const branch = (fields.branch || "").trim();
    const commit = (fields.commit || "").trim();
    const words = (fields.instructions || "").trim();
    if (target === "branch") return branch ? { review: { target, branch } } : { why: "Name the branch to compare with." };
    if (target === "commit") {
        if (!HEX.test(commit)) return { why: "A commit is named by its hash: 4 to 64 hex digits." };
        const title = (fields.title || "").trim();
        return { review: title ? { target, commit, title } : { target, commit } };
    }
    if (target === "custom") return words ? { review: { target, instructions: words } } : { why: "Say what to look for." };
    return { review: { target: "uncommitted" } };
}

// ReviewPane asks what codex should look at and sends the review. A thread
// at work has its turn to finish first: the host refuses a review then.
export function ReviewPane({ name, live, exec, cwd, onBack, onDone }) {
    const run = useAction();
    const [target, setTarget] = useState("uncommitted");
    const [fields, setFields] = useState({});
    const field = (key) => (e) => setFields({ ...fields, [key]: e.currentTarget.value });
    const plan = reviewOf(target, fields);
    const can = knows(exec, "session.command");
    const busy = live.status === "busy";
    const why = !can ? whyNot(exec, "session.command") : busy ? "A turn runs: the review waits until it ends." : plan.why || "";
    const send = async () => {
        if (why) return;
        const result = await run("session.command", name, { command: "review", ...plan });
        if (result && result.ok) onDone();
    };
    const desc = { uncommitted: `the changes in ${cwd ? shortPath(cwd) : "the directory of the thread"}`,
        branch: "what this branch changed since it left another", commit: "the changes one commit made",
        custom: "say what to look for" };
    return html`
        <${Back} title="Review" sub="What codex should look at — it answers in the feed" onBack=${onBack} />
        <div class="pklist">
            ${TARGETS.map((t) => html`
                <${PickRow} key=${t.value} on=${t.value === target} icon=${t.icon} tone="" name=${t.name}
                            desc=${desc[t.value]} onPick=${() => setTarget(t.value)} />
            `)}
        </div>
        ${target === "branch" && html`
            <input class="rnname cxfield" type="text" aria-label="the branch to compare with" placeholder="main"
                   autocomplete="off" autocapitalize="off" spellcheck=${false}
                   value=${fields.branch || ""} onInput=${field("branch")} />`}
        ${target === "commit" && html`
            <input class="rnname cxfield" type="text" aria-label="the hash of the commit" placeholder="a1b2c3d"
                   autocomplete="off" autocapitalize="off" spellcheck=${false}
                   value=${fields.commit || ""} onInput=${field("commit")} />
            <input class="rnname cxfield" type="text" aria-label="the title of the commit, for the feed"
                   placeholder="its title, for the feed — optional" value=${fields.title || ""} onInput=${field("title")} />`}
        ${target === "custom" && html`
            <textarea class="rnname cxfield" rows="3" aria-label="what to look for" placeholder="look at the locks around the cart"
                      value=${fields.instructions || ""} onInput=${field("instructions")}></textarea>`}
        ${why && html`<p class="cmdnote cxwhy">${why}</p>`}
        <div class="btnrow">
            <button class="btn" type="button" onClick=${onBack || onDone}>Cancel</button>
            <button class="btn primary" type="button" disabled=${Boolean(why)} onClick=${send}>Review</button>
        </div>
    `;
}

// renameTrouble says what keeps a typed title from being saved, or nothing.
// A thread's title is words a person reads: any printable line will do.
export function renameTrouble(value) {
    const title = String(value || "").trim();
    if (!title) return "Enter a name.";
    if (/[\u0000-\u001f\u007f]/.test(title)) return "A name is one line of text.";
    return "";
}

// RenamePane gives the thread a title of its own. The panel goes on finding
// the session by its address; the title is what the lists show.
export function RenamePane({ name, live, exec, onBack, onDone }) {
    const run = useAction();
    const current = shownName(live, name);
    const [value, setValue] = useState(current);
    const [busy, setBusy] = useState(false);
    const can = knows(exec, "session.rename");
    const trouble = renameTrouble(value);
    const ready = can && !busy && !trouble && value.trim() !== current;
    const save = async () => {
        if (!ready) return;
        setBusy(true);
        const result = await run("session.rename", name, { name: value.trim() });
        setBusy(false);
        if (result && result.ok) onDone();
    };
    return html`
        <${Back} title="Rename the thread" sub=${`the panel still finds it as ${name}`} onBack=${onBack} />
        <input class="rnname" type="text" aria-label="the new name of the thread" value=${value} autocomplete="off"
               onInput=${(e) => setValue(e.currentTarget.value)}
               onKeyDown=${(e) => { if (e.key === "Enter") { e.preventDefault(); save(); } }} />
        ${trouble && html`<p class="cmdnote stpwarn">${trouble}</p>`}
        ${!can && html`<p class="cmdnote">${whyNot(exec, "session.rename")}</p>`}
        <div class="btnrow">
            <button class="btn" type="button" onClick=${onBack || onDone}>Cancel</button>
            <button class="btn primary" type="button" disabled=${!ready} onClick=${save}>${busy ? "Saving…" : "Save"}</button>
        </div>
    `;
}

// GoalPane shows the goal of the thread and what can be done to it: pause or
// resume it, clear it, or set another. A thread without one is asked for its
// goal and, if the person wants one, a budget of tokens.
export function GoalPane({ name, live, exec, onBack, onDone }) {
    const run = useAction();
    const goal = live.goal && live.goal.objective ? live.goal : null;
    const [writing, setWriting] = useState(!goal);
    const [objective, setObjective] = useState("");
    const [budget, setBudget] = useState("");
    useEffect(() => { setWriting(!goal); }, [Boolean(goal)]);
    const can = knows(exec, "session.command");
    const why = can ? "" : whyNot(exec, "session.command");
    const send = async (what) => {
        const result = await run("session.command", name, { command: "goal", goal: what });
        if (result && result.ok) onDone();
    };
    const amount = budget.trim() ? Number(budget.trim().replace(/[\s_,]/g, "")) : 0;
    const badBudget = budget.trim() !== "" && !(Number.isInteger(amount) && amount > 0);
    const set = () => {
        if (!objective.trim() || badBudget) return;
        send(amount ? { do: "set", objective: objective.trim(), budget: amount } : { do: "set", objective: objective.trim() });
    };
    const used = goal && goal.tokenBudget ? Math.min(100, (100 * (goal.tokensUsed || 0)) / goal.tokenBudget) : 0;
    return html`
        <${Back} title="Goal" sub="What codex works towards between your messages" onBack=${onBack} />
        ${why && html`<p class="cmdnote">${why}</p>`}
        ${goal && html`
            <section class="cmdsec cxgoal">
                <div class="cmdsechead"><span>${goal.objective}</span>
                    <span class="cmdaside">${GOAL_WORDS[goal.status] || goal.status}</span></div>
                ${goal.tokenBudget
                    ? html`
                        <div class="cmdmeter"><span class="cmdsub">Token budget</span><b>${tokens(goal.tokensUsed || 0)}</b>
                            <span class="cmdaside">of ${tokens(goal.tokenBudget)}</span></div>
                        <span class="cmdbar big" aria-hidden="true"><i class="t-messages" style=${`width:${used}%`}></i><b></b></span>`
                    : html`<p class="cmdnote">${tokens(goal.tokensUsed || 0)} tokens spent, no budget set</p>`}
                ${goal.timeUsedSeconds > 0 && html`<p class="cmdnote">${minutes(goal.timeUsedSeconds)} of work so far</p>`}
            </section>
            <div class="btnrow cxgoalacts">
                ${goal.status === "paused"
                    ? html`<button class="btn" type="button" disabled=${!can} onClick=${() => send({ do: "resume" })}>Resume</button>`
                    : html`<button class="btn" type="button" disabled=${!can || goal.status !== "active"}
                                   onClick=${() => send({ do: "pause" })}>Pause</button>`}
                <button class="btn" type="button" disabled=${!can} onClick=${() => setWriting(!writing)}>Set another</button>
                <button class="btn danger" type="button" disabled=${!can} onClick=${() => send({ do: "clear" })}>Clear</button>
            </div>
        `}
        ${writing && html`
            <textarea class="rnname cxfield" rows="3" aria-label="the goal" placeholder="make CI green on main"
                      value=${objective} onInput=${(e) => setObjective(e.currentTarget.value)}></textarea>
            <input class="rnname cxfield" type="text" inputmode="numeric" aria-label="the budget of tokens"
                   placeholder="a budget of tokens — optional" value=${budget} onInput=${(e) => setBudget(e.currentTarget.value)} />
            ${badBudget && html`<p class="cmdnote stpwarn">A budget is a whole number of tokens.</p>`}
            <p class="cmdnote">Codex starts working towards it at once.</p>
            <div class="btnrow">
                <button class="btn" type="button" onClick=${onBack || onDone}>Cancel</button>
                <button class="btn primary" type="button" disabled=${!can || !objective.trim() || badBudget}
                        onClick=${set}>Set the goal</button>
            </div>
        `}
    `;
}

function minutes(sec) {
    const m = Math.round(sec / 60);
    return m < 60 ? `${Math.max(1, m)} min` : `${Math.floor(m / 60)} h ${m % 60} min`;
}

// ProcessesChip counts the background processes the thread left running, a
// chip of the row under the composer, while there is something to count.
export function ProcessesChip({ count, onOpen }) {
    if (!(count > 0)) return null;
    return html`
        <button class="wchip" type="button" onClick=${onOpen}
                aria-label=${`background processes: ${count} running`}>
            ${Icon.terminal()}<span class="wnum">${count}</span>
        </button>
    `;
}

// ProcessesSheet lists the commands codex started and left running in this
// thread, each with a stop, and a stop for all of them. The list is asked
// when it opens and again after a stop.
export function ProcessesSheet({ name, exec }) {
    const run = useAction();
    const [tick, setTick] = useState(0);
    const [data, setData] = useState(null);
    useEffect(() => {
        let alive = true;
        setData(null);
        fetch(`/api/session/processes?name=${encodeURIComponent(name)}`, { credentials: "same-origin" })
            .then(async (r) => {
                if (!r.ok) throw new Error((await r.text()).trim() || `the server answered ${r.status}`);
                return r.json();
            })
            .then((body) => { if (alive) setData(body); })
            .catch((err) => { if (alive) setData({ state: "unknown", reason: String(err.message || err) }); });
        return () => { alive = false; };
    }, [name, tick]);
    const canOne = knows(exec, "task.stop");
    const canAll = knows(exec, "session.command");
    const stop = async (kind, params) => {
        const result = await run(kind, name, params);
        if (result && !result.cancelled) setTick((n) => n + 1);
    };
    const head = html`<div class="shead cmdtitle"><span class="cmdhead">Background processes</span></div>`;
    if (!data) return html`<div class="cmdsheet">${head}<p class="cmdnote">Asking codex…</p></div>`;
    if (data.state !== "ok") return html`<div class="cmdsheet">${head}<p class="cmdnote">${data.reason || "Codex did not answer."}</p></div>`;
    const list = data.processes || [];
    return html`
        <div class="cmdsheet">
            ${head}
            <p class="cmdnote">Commands codex started and left running in this thread.</p>
            <section class="cmdsec">
                <div class="cmdsechead"><span>Running</span><span class="cmdaside">${list.length}</span></div>
                ${list.length === 0 && html`<p class="cmdnote">Nothing runs in the background now.</p>`}
                <ul class="mcplist">
                    ${list.map((p) => html`
                        <li key=${p.id}>
                            <div class="mcprow cxproc">
                                <span class="mcpdot ok" aria-hidden="true"></span>
                                <span class="cxprocbody">
                                    <span class="cmdname">${p.command}</span>
                                    <span class="cmdrownote">${procFacts(p)}</span>
                                </span>
                                <button class="btn" type="button" disabled=${!canOne}
                                        title=${canOne ? undefined : whyNot(exec, "task.stop")}
                                        aria-label=${`stop ${p.command}`}
                                        onClick=${() => stop("task.stop", { id: p.id })}>Stop</button>
                            </div>
                        </li>
                    `)}
                </ul>
            </section>
            ${list.length > 1 && html`
                <div class="btnrow">
                    <button class="btn danger" type="button" disabled=${!canAll}
                            onClick=${() => stop("session.command", { command: "stop" })}>Stop all</button>
                </div>
            `}
        </div>
    `;
}

// procFacts says where a process runs and, where codex measures it, what it
// costs the machine.
function procFacts(p) {
    const parts = [p.cwd ? shortPath(p.cwd) : ""];
    if (p.pid) parts.push(`pid ${p.pid}`);
    if (typeof p.cpu === "number") parts.push(`cpu ${Math.round(p.cpu)}%`);
    if (typeof p.rssKb === "number") parts.push(`${Math.round(p.rssKb / 1024)} MB`);
    return parts.filter(Boolean).join(" · ");
}
