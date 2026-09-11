// The project row on the map: what will launch and where it lies.
import { html } from "../../html.js";
import { Icon } from "../../ui/icons.js";
import { LaunchView } from "./launch.js";

export function ProjectRow({ project, profile, group, gone, expanded, onToggle, onForm }) {
    return html`
        <div class="pfproj" data-open=${expanded ? "1" : "0"} data-gone=${gone ? "1" : "0"}>
            <div class="prow">
                <button class="pmain" type="button" onClick=${onToggle}>
                    <div class="r1"><span class="nm">${project.name}</span></div>
                    <span class="chev">${Icon.chevron()}</span>
                    <div class="meta">
                        <div class="mline">
                            <span class="path">${project.path}</span>
                            ${gone && html`<span class="warn">the directory is not on disk</span>`}
                        </div>
                    </div>
                </button>
                <button
                    class="pfact"
                    type="button"
                    aria-label=${`edit project ${project.name}`}
                    onClick=${() => onForm({ kind: "project", mode: "edit", profile, group, project })}
                >${Icon.pencil()}</button>
            </div>

            ${expanded && html`
                <div class="pfdetails">
                    <div class="pfprops">
                        <div class="kv pfline">
                            <span class="k">path</span>
                            <span class="v pfpath">${project.path}</span>
                        </div>
                        <div class="kv">
                            <span class="k">session name</span>
                            <span class="v">${project.session || html`<span class="pfnone">after the directory name</span>`}</span>
                        </div>
                    </div>
                    <${LaunchView} launch=${project.launch} profile=${profile.launch} />
                </div>
            `}
        </div>
    `;
}
