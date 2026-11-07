// One project picker for the contour's plus, in a popover or a phone sheet.
import { html } from "../../html.js";
import { homeProject, homeSession } from "../../ui/home.js";
import { useAction } from "../../actions/gate.js";
import { knows, whyNot } from "../../exec.js";

export function newSessionGroups(entry, snapshot) {
    const home = homeSession(snapshot) ? homeProject(snapshot) : null;
    return ((entry && entry.groups) || [])
        .map((g) => ({ name: g.name, projects: (g.projects || []).filter((p) => !home || p.id !== home.id) }))
        .filter((g) => g.projects.length > 0);
}

export function NewSessionList({ entry, snapshot, exec, onClose, desktop = false }) {
    const run = useAction();
    const groups = newSessionGroups(entry, snapshot);
    const ready = knows(exec, "session.open");
    const live = new Set(((snapshot && snapshot.sessions) || []).map((s) => s.project && s.project.id).filter(Boolean));
    return html`
        ${!ready && html`<p class=${desktop ? "dklimpopnote" : "hint"}>${whyNot(exec, "session.open")}</p>`}
        ${groups.length === 0 && html`<p class="empty">There are no projects to open.</p>`}
        ${groups.map((g) => html`
            <div class=${desktop ? "dknewgroup" : "newgroup"} key=${g.name}>${g.name}</div>
            ${g.projects.map((p) => html`
                <button class=${desktop ? "dknewrow" : "newproject"} type="button" key=${p.id} disabled=${!ready}
                        onClick=${async () => {
                            onClose();
                            await run("session.open", p.session || p.name, { project: p.id });
                        }}>
                    <span class=${desktop ? "dkname" : "newname"}>${p.name}</span>
                    ${live.has(p.id) && html`<span class=${desktop ? "dknewlive" : "newlive"}>live</span>`}
                </button>
            `)}
        `)}
    `;
}
