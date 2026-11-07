import { html } from "../../html.js";
import { Icon } from "../../ui/icons.js";
import { age } from "./state.js";

export function Priority({ value }) {
    const pr = { HIGH: "H", MEDIUM: "M", LOW: "L" }[value] || "";
    return html`<span class="tqpr" data-pr=${pr} aria-label=${`priority ${value || "not set"}`}><i></i><i></i><i></i></span>`;
}

export function Issue({ value, url }) {
    return url
        ? html`<a class="tqext link" href=${url} target="_blank" rel="noopener noreferrer">${value}<span aria-hidden="true">↗</span></a>`
        : html`<span class="tqext">${value}</span>`;
}

export function Person({ person, count = false }) {
    const name = person.known ? person.name : person.name.slice(0, 8);
    return html`<span class="tqwho" data-tone=${person.live ? person.status : "gone"} title=${person.name}>
        ${person.live && html`<i aria-hidden="true"></i>`}${name}
        ${count && html`<span class="tqconversations">${person.conversations.length} ${person.conversations.length === 1 ? "conversation" : "conversations"}</span>`}
    </span>`;
}

export function TaskRow({ task, onOpen }) {
    return html`
        <button class=${`tqrow${task.status === "cancelled" ? " cancelled" : ""}`} type="button"
                aria-label=${`#${task.number}: ${task.title}, ${task.project}`} onClick=${() => onOpen(task)}>
            <${Priority} value=${task.priority} />
            <span class="tqmain">
                <span class="tqt"><span class="tqid">#${task.number}</span> ${task.title}</span>
                <span class="tqmeta">
                    <span class="tqproj">${task.project}</span>
                    ${(task.externalRefs || []).map((ref) => html`<${Issue} value=${ref} key=${ref} />`)}
                    <span class="tqwho">${task.lastActorName || "session unknown"}</span>
                </span>
            </span>
            <span class="tqage">${age(task.lastEventAt || task.updatedAt)}</span>
        </button>
    `;
}

export function TaskRows({ tasks, onOpen }) {
    return html`<div class="pjblock tqgroup">${tasks.map((task) => html`<${TaskRow} key=${`${task.projectId}:${task.number}`} task=${task} onOpen=${onOpen} />`)}</div>`;
}

export function Heading({ children, count }) {
    return html`<div class="grouphead pjsection">${children}<span class="pjcount">${` ${count}`}</span></div>`;
}

export function PrioritySplit({ tasks }) {
    return html`<span class="tqsplit" aria-hidden="true">${["HIGH", "MEDIUM", "LOW"].map((priority) => html`
        <i data-pr=${priority} style=${`flex:${tasks.filter((task) => task.priority === priority).length}`} key=${priority}></i>
    `)}</span>`;
}

export function Empty({ name }) {
    return html`<div class="tqempty">
        <span class="tqemptyicon" aria-hidden="true">${Icon.plan()}</span>
        <h2>No tasks in ${name} yet</h2>
        <p>Sessions of this contour file them as they work.</p>
    </div>`;
}
