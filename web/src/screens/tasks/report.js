import { useState } from "preact/hooks";

import { html } from "../../html.js";
import { Icon } from "../../ui/icons.js";
import { Sheet } from "../../ui/sheet.js";
import { Trouble } from "../../ui/trouble.js";
import { Issue, TaskRows } from "./parts.js";
import { eventDate, eventWords, useTaskPeriod } from "./state.js";

export const TASK_PERIODS = [{ id: "today", label: "Today" }, { id: "yesterday", label: "Yesterday" }, { id: "week", label: "Week" }];

export function ReportSheet({ contour, name, period: initial, onClose, onOpen }) {
    const [period, setPeriod] = useState(initial || "today");
    const state = useTaskPeriod(contour, period);
    const groups = state.kind === "ready" ? state.groups : [];
    return html`<${Sheet} open tall onClose=${onClose} label="Report">
        <div class="shead"><span class="stitle">Report · ${name}</span></div>
        <div class="tqperiods" role="group" aria-label="Report period">${TASK_PERIODS.map((item) => html`
            <button class="chip" type="button" key=${item.id} aria-pressed=${period === item.id ? "true" : "false"} onClick=${() => setPeriod(item.id)}>${item.label}</button>
        `)}</div>
        ${state.kind === "loading" && html`<p class="empty">Loading report…</p>`}
        ${state.error && html`<${Trouble} what="Report" error=${state.error} />`}
        ${state.kind === "ready" && state.total === 0 && html`<p class="empty">No recorded activity in this period.</p>`}
        <div class="tqreport">${groups.map((group) => html`<${ReportGroup} key=${group.external} group=${group} onOpen=${onOpen} />`)}</div>
    <//>`;
}

function ReportGroup({ group, onOpen }) {
    const [open, setOpen] = useState(false);
    const events = group.tasks.flatMap((activity) => activity.events);
    const names = [...new Set(events.map((event) => event.actorName || event.actor).filter(Boolean))];
    const count = (kind) => events.filter((event) => event.kind === kind).length;
    return html`<div class="tqreportgroup">
        <button class="tqrline" type="button" aria-expanded=${open ? "true" : "false"} onClick=${() => setOpen(!open)}>
            <span class="tqrname">${group.external ? html`<${Issue} value=${group.external} />` : "Other work"}</span>
            <span class="tqcounts" aria-label="Activity counts">${count("closed")} closed · ${count("created")} new · ${count("changed") + count("status_changed")} changed</span>
            <span class="tqrwho">${names.join(", ") || "session unknown"}</span>
            <span class="chev" aria-hidden="true">${Icon.chevron()}</span>
        </button>
        ${open && group.tasks.map((activity) => html`<div class="tqactivity" key=${`${activity.task.projectId}:${activity.task.number}`}>
            <${TaskRows} tasks=${[activity.task]} onOpen=${onOpen} />
            <ul>${activity.events.map((event) => html`<li key=${event.id}><time datetime=${event.at}>${eventDate(event)}</time> ${eventWords(event)} · ${event.actorName || event.actor || "session unknown"}</li>`)}</ul>
        </div>`)}
    </div>`;
}
