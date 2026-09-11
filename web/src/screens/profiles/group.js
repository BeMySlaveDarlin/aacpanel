// A group on the contour page: a card row that unfolds into projects.
import { html } from "../../html.js";
import { plural } from "../../format.js";
import { Icon } from "../../ui/icons.js";
import { ProjectRow } from "./project.js";
import { dotOf } from "./pick.js";

// countOf renders a project count as a line.
export function countOf(n) {
    return `${n} ${plural(n, "project", "projects")}`;
}

export function GroupCard({ profile, group, open, onToggle, onForm, gone, searching }) {
    const projects = group.projects || [];
    const expanded = searching || open.has(`g:${group.id}`);

    return html`
        <section class="card pfgroup" data-open=${expanded ? "1" : "0"}>
            <div class="pfghead">
                <button class="pfgtap" type="button" onClick=${() => onToggle(`g:${group.id}`)}>
                    <span class="dot ${dotOf(group, gone)}"></span>
                    <span class="pfgname">${group.name}</span>
                    <span class="count">${countOf(projects.length)}</span>
                </button>
                <button
                    class="pfact"
                    type="button"
                    aria-label=${`add a project to group ${group.name}`}
                    onClick=${() => onForm({ kind: "project", mode: "add", profile, group })}
                >${Icon.plus()}</button>
                <button
                    class="pfact"
                    type="button"
                    aria-label=${`edit group ${group.name}`}
                    onClick=${() => onForm({ kind: "group", mode: "edit", profile, group })}
                >${Icon.pencil()}</button>
            </div>

            ${expanded && (projects.length === 0
                ? html`<p class="hint pfhint">The group has no projects — there is nothing to launch from it.</p>`
                : projects.map((project) => html`
                    <${ProjectRow}
                        key=${project.id}
                        project=${project}
                        profile=${profile}
                        group=${group}
                        gone=${Boolean(gone && gone.has(project.id))}
                        expanded=${open.has(`r:${project.id}`)}
                        onToggle=${() => onToggle(`r:${project.id}`)}
                        onForm=${onForm}
                    />
                `))}
        </section>
    `;
}
