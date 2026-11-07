// The contour's registry: active work first, the backlog folded by project.
import { useState } from "preact/hooks";

import { html } from "../html.js";
import { useEndpoint } from "../history.js";
import { BackHead, useBackClose } from "../ui/back.js";
import { Icon } from "../ui/icons.js";
import { Trouble } from "../ui/trouble.js";
import { Empty, Heading, Issue, Person, PrioritySplit, TaskRows } from "./tasks/parts.js";
import { TaskSheet } from "./tasks/detail.js";
import { ReportSheet, TASK_PERIODS } from "./tasks/report.js";
import { within } from "./tasks/state.js";

const PAGE = 40;

export function Tasks({ contour, name, onClose }) {
    const [retry, setRetry] = useState(0);
    const [query, setQuery] = useState("");
    const [task, setTask] = useState(null);
    const [report, setReport] = useState(null);
    const [closedPeriod, setClosedPeriod] = useState("");
    const [shown, setShown] = useState(PAGE);
    const close = useBackClose(true, onClose);
    const state = useEndpoint(`/api/tasks/board?contour=${contour}&refresh=${retry}`, "tasks");
    const all = state.kind === "ready" ? state.data.tasks : [];
    const terms = query.trim().toLowerCase().split(/\s+/).filter(Boolean);
    const tasks = all.filter((row) => {
        const text = [`#${row.number}`, row.title, row.project, row.lastActorName, ...(row.tags || []), ...(row.externalRefs || [])].join(" ").toLowerCase();
        return terms.every((term) => text.includes(term));
    });
    const work = tasks.filter((row) => row.status === "open" && row.inWork)
        .sort((a, b) => String(b.lastEventAt || "").localeCompare(String(a.lastEventAt || "")) || a.number - b.number);
    const quiet = tasks.filter((row) => row.status === "open" && !row.inWork);
    const deferred = tasks.filter((row) => row.status === "deferred");
    const closed = tasks.filter((row) => row.status === "done" || row.status === "cancelled");
    const unfiled = state.kind === "ready" ? (state.data.noTask || []).filter((row) => terms.every((term) =>
        `${row.external} ${row.people.map((person) => person.name).join(" ")}`.toLowerCase().includes(term))) : [];
    const months = new Map();
    for (const row of closed) {
        const key = row.closedAt ? row.closedAt.slice(0, 7) : "undated";
        const list = months.get(key) || [];
        list.push(row);
        months.set(key, list);
    }
    return html`
        <${BackHead} onBack=${close} label="back to sessions"><h2>Tasks · ${name}</h2><//>
        <section class="tqscreen" aria-label=${`Tasks in ${name}`}>
            ${state.kind === "loading" && html`<p class="empty">Loading tasks…</p>`}
            ${state.error && html`<${Trouble} what="Tasks" error=${state.error} /><button class="btn" type="button" onClick=${() => setRetry(retry + 1)}>Retry</button>`}
            ${state.kind === "ready" && html`
                ${(state.data.notes || []).map((note) => html`<p class="hint" key=${note}>${note}</p>`)}
                ${all.length === 0 && unfiled.length === 0 ? html`<${Empty} name=${name} />` : html`
                    <label class="tqsearch">${Icon.search()}<input type="search" aria-label="Find a task" placeholder="Find a task: title, #, SER-…" value=${query}
                        onInput=${(event) => { setQuery(event.currentTarget.value); setShown(PAGE); }} /></label>
                    ${terms.length > 0 && tasks.length === 0 && unfiled.length === 0 && html`<p class="empty">No tasks match this search.</p>`}
                    <${Heading} count=${work.length}>In work<//>
                    ${work.length > 0 ? html`<${TaskRows} tasks=${work.slice(0, shown)} onOpen=${setTask} />` : html`<p class="tqhintline">No open tasks were touched recently or are held by a live session.</p>`}
                    ${work.length > shown && html`<button class="tqmore" type="button" onClick=${() => setShown(shown + PAGE)}>Show more in work (${work.length - shown})</button>`}
                    ${unfiled.length > 0 && html`
                        <${Heading} count=${unfiled.length}>No task<//>
                        <div class="pjblock tqnotasks">${unfiled.map((row) => html`<div class="tqnotask" key=${row.external}>
                            <${Issue} value=${row.external} />
                            <span class="tqpeople">${row.people.map((person) => html`<${Person} key=${person.name} person=${person} count />`)}</span>
                        </div>`)}</div>
                    `}
                    <${FoldedProjects} title="Quiet" tasks=${quiet} onOpen=${setTask} />
                    <${FoldedProjects} title="Deferred" tasks=${deferred} onOpen=${setTask} />
                    <${Heading} count=${closed.length}>Closed<//>
                    <div class="tqtiles">
                        ${TASK_PERIODS.map((item) => html`<button class=${`tqtile${closedPeriod === item.id ? " on" : ""}`} type="button" key=${item.id}
                            aria-pressed=${closedPeriod === item.id ? "true" : "false"} onClick=${() => setClosedPeriod(closedPeriod === item.id ? "" : item.id)}>
                            <b>${closed.filter((row) => within(row.closedAt, item.id)).length}</b><span>${item.label}</span>
                        </button>`)}
                        <button class="tqtile report" type="button" onClick=${() => setReport(closedPeriod || "today")}>${Icon.list()}<span>Report</span></button>
                    </div>
                    ${closedPeriod && html`<${FoldGroup} key=${closedPeriod} label=${`${TASK_PERIODS.find((item) => item.id === closedPeriod).label}: closed`}
                        tasks=${closed.filter((row) => within(row.closedAt, closedPeriod))} onOpen=${setTask} initial />`}
                    <div class="pjblock pjquiet tqlines">${[...months].sort(([a], [b]) => a === "undated" ? 1 : b === "undated" ? -1 : b.localeCompare(a)).map(([key, rows]) => html`
                        <${FoldGroup} key=${key} label=${key === "undated" ? "Date unknown" : new Date(`${key}-01T12:00:00Z`).toLocaleDateString("en-US", { month: "long", year: "numeric" })} tasks=${rows} onOpen=${setTask} />
                    `)}</div>
                `}
            `}
        </section>
        ${report && html`<${ReportSheet} contour=${contour} name=${name} period=${report} onClose=${() => setReport(null)} onOpen=${setTask} />`}
        ${task && html`<${TaskSheet} key=${`${task.projectId}:${task.number}`} task=${task} onClose=${() => setTask(null)} />`}
    `;
}

function FoldedProjects({ title, tasks, onOpen }) {
    const groups = new Map();
    for (const task of tasks) {
        const own = groups.get(task.projectId) || { name: task.project, tasks: [] };
        own.tasks.push(task);
        groups.set(task.projectId, own);
    }
    return html`
        <${Heading} count=${tasks.length}>${title}<//>
        ${tasks.length > 0 && html`<div class="pjblock pjquiet tqlines" aria-label=${title}>
            ${[...groups].sort(([, a], [, b]) => b.tasks.length - a.tasks.length || a.name.localeCompare(b.name)).map(([id, group]) => html`
                <${FoldGroup} key=${id} label=${group.name} tasks=${group.tasks} onOpen=${onOpen} />
            `)}
        </div>`}
    `;
}

function FoldGroup({ label, tasks, onOpen, initial = false }) {
    const [open, setOpen] = useState(initial);
    const [shown, setShown] = useState(PAGE);
    return html`<div class="tqfoldgroup">
        <button class=${`tqpline${open ? " open" : ""}`} type="button" aria-expanded=${open ? "true" : "false"} onClick=${() => setOpen(!open)}>
            <span class="tqpname">${label}</span><${PrioritySplit} tasks=${tasks} /><span class="tqpn">${tasks.length}</span>
            <span class="chev" aria-hidden="true">${Icon.chevron()}</span>
        </button>
        ${open && html`
            <${TaskRows} tasks=${tasks.slice(0, shown)} onOpen=${onOpen} />
            ${tasks.length === 0 && html`<p class="empty">No closed tasks in this period.</p>`}
            ${tasks.length > shown && html`<button class="tqmore" type="button" onClick=${() => setShown(shown + PAGE)}>Show more (${tasks.length - shown})</button>`}
        `}
    </div>`;
}
