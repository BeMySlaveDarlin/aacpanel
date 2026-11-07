import { html } from "../../html.js";
import { render } from "../../md.js";
import { useEndpoint } from "../../history.js";
import { Sheet } from "../../ui/sheet.js";
import { Trouble } from "../../ui/trouble.js";
import { Heading, Issue, Person, Priority } from "./parts.js";
import { age, eventDate, eventWords } from "./state.js";

export function TaskSheet({ task, onClose }) {
    const state = useEndpoint(`/api/projects/${task.projectId}/tasks/${task.number}`, "the task");
    const detail = state.kind === "ready" ? state.data : null;
    const row = detail ? detail.task : task;
    return html`<${Sheet} open onClose=${onClose} label="Task" tall>
        <div class="shead"><span class="stitle"><span class="tqpageproj">${row.project}</span> #${row.number}</span></div>
        <div class="tqtask">
            <h2 class="tqtasktitle">${row.title}</h2>
            <div class="tqtaskmarks">
                <span class="tqstatus">${row.status}</span><${Priority} value=${row.priority} /><span class="hint">${row.priority}</span>
                ${(row.externalRefs || []).map((ref) => html`<${Issue} key=${ref} value=${ref} url=${row.externalUrl && (row.externalRefs.length === 1 || row.externalUrl.toUpperCase().includes(ref)) ? row.externalUrl : ""} />`)}
            </div>
            ${state.kind === "loading" && html`<p class="empty">Loading task…</p>`}
            ${state.error && html`<${Trouble} what="Task" error=${state.error} />`}
            ${detail && html`
                <div class="tqnow"><span class="tqnowlabel">Last activity</span>
                    <p>${age(row.lastEventAt)} · ${row.lastActorName || "session unknown"}</p>
                </div>
                <div class="tqpeople" aria-label="Who touched this task">
                    ${(row.people || []).map((person) => html`<${Person} key=${person.name} person=${person} count />`)}
                </div>
                <div class="msg tqbody">${render(row.markdown || "No description.")}</div>
                <${Heading} count=${detail.events.length}>Events<//>
                <ol class="tqevents">${detail.events.map((event) => html`
                    <li key=${event.id}>
                        <time datetime=${event.at}>${eventDate(event)}</time>
                        <span>${eventWords(event)}</span><span class="tqwho">${event.actorName || event.actor || "session unknown"}</span>
                    </li>
                `)}</ol>
                ${detail.handoffs.length > 0 && html`
                    <${Heading} count=${detail.handoffs.length}>Handoffs<//>
                    ${detail.handoffs.map((handoff) => html`<details class="tqhandoff" key=${handoff.file}>
                        <summary>${handoff.title || handoff.key}</summary>
                        <div class="msg">${render(typeof handoff.body === "string" ? handoff.body : "")}</div>
                    </details>`)}
                `}
            `}
        </div>
    <//>`;
}
