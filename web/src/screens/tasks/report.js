import { useEffect, useRef, useState } from "preact/hooks";

import { html } from "../../html.js";
import { render } from "../../md.js";
import { knows, useExec, whyNot } from "../../exec.js";
import { useAction } from "../../actions/gate.js";
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
        ${state.kind === "ready" && state.total > 0 && html`<${Summary} key=${`${contour}:${state.from}:${state.until}`} contour=${contour} from=${state.from} until=${state.until} />`}
        <div class="tqreport">${groups.map((group) => html`<${ReportGroup} key=${group.external} group=${group} onOpen=${onOpen} />`)}</div>
    <//>`;
}

function Summary({ contour, from, until }) {
    const run = useAction();
    const exec = useExec();
    const alive = useRef(true);
    const [state, setState] = useState({ loading: true, summary: null });
    useEffect(() => {
        const controller = new AbortController();
        alive.current = true;
        const query = new URLSearchParams({ contour, from, until });
        (async () => {
            try {
                const response = await fetch(`/api/tasks/summary?${query}`, { credentials: "same-origin", signal: controller.signal });
                if (!response.ok) throw new Error(`The summary is unavailable (${response.status}).`);
                const body = await response.json();
                if (alive.current) setState({ loading: false, summary: body.summary });
            } catch (err) {
                if (alive.current) setState({ loading: false, summary: null, error: err.message });
            }
        })();
        return () => { alive.current = false; controller.abort(); };
    }, [contour, from, until]);
    const summarize = async () => {
        const result = await run("task.summary", String(contour), { contour: Number(contour), from, until }, {
            onSend: () => { if (alive.current) setState((was) => ({ ...was, running: true, error: "" })); },
        });
        if (!alive.current) return;
        if (result.ok && result.data?.summary) setState({ summary: result.data.summary });
        else if (result.ok) setState((was) => ({ ...was, running: false, error: "The host returned no summary." }));
        else setState((was) => ({ ...was, running: false, error: result.cancelled ? "" : result.error }));
    };
    const copy = async () => {
        try {
            await navigator.clipboard.writeText(state.summary.text);
        } catch (err) {
            if (alive.current) setState((was) => ({ ...was, error: "The summary could not be copied." }));
        }
    };
    return html`<section class="tqtask tqsummary" aria-label="Activity summary" aria-busy=${state.running ? "true" : "false"}>
        ${state.loading && html`<p class="hint">Loading summary…</p>`}
        ${state.summary && html`
            <div class="msg tqbody">${render(state.summary.text)}</div>
            <p class="hint"><time datetime=${state.summary.createdAt}>${new Date(state.summary.createdAt).toLocaleString()}</time> · ${state.summary.model}</p>
        `}
        ${state.error && html`<${Trouble} what="Summary" error=${state.error} />`}
        <button class="btn" type="button" disabled=${state.loading || state.running || !knows(exec, "task.summary")} title=${whyNot(exec, "task.summary")} onClick=${summarize}>
            ${state.running ? "Summarizing…" : state.summary ? "Summarize again" : "Summarize"}
        </button>
        ${state.summary && html`<button class="btn" type="button" onClick=${copy}>Copy</button>`}
    </section>`;
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
