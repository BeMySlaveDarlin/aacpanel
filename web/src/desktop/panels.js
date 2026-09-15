// The right-hand panel: what belongs to the open section but not to the centre.
import { html } from "../html.js";
import { Icon } from "../ui/icons.js";
import { Archive } from "./panels/archive.js";
import { Journal } from "./panels/journal.js";
import { Logs } from "./panels/logs.js";
import { Procs } from "./panels/procs.js";
import { Projects } from "./panels/projects.js";

export function RightPanel({ tab, title, profiles, picks, names, archPicks, setArchPicks, container, exec, onOpen, onClose, onOpened }) {
    return html`
        <aside class="dkright">
            <div class="dkpanelhead">
                <span class="dkpaneltitle">${title}</span>
                <button class="dkclose" type="button" onClick=${onClose}><${Icon.close} /></button>
            </div>
            ${tab === "archive" && html`<${Archive}
                profiles=${profiles}
                names=${names}
                picks=${archPicks}
                setPicks=${setArchPicks}
                onOpen=${onOpen}
                exec=${exec}
            />`}
            ${tab === "journal" && html`<${Journal} />`}
            ${tab === "logs" && html`<${Logs} container=${container} />`}
            ${tab === "procs" && html`<${Procs} />`}
            ${tab === "projects" && html`<${Projects} picks=${picks} exec=${exec} onOpened=${onOpened} />`}
        </aside>
    `;
}

export const PANELS = {
    sessions: [
        { id: "archive", label: "Session archive", icon: Icon.clock },
        { id: "projects", label: "Projects", icon: Icon.cube },
        { id: "journal", label: "Action journal", icon: Icon.list },
    ],
    containers: [
        { id: "logs", label: "Container logs", icon: Icon.terminal },
        { id: "journal", label: "Action journal", icon: Icon.list },
    ],
    machine: [
        { id: "procs", label: "Processes", icon: Icon.tools },
        { id: "journal", label: "Action journal", icon: Icon.list },
    ],
    profiles: [
        { id: "journal", label: "Action journal", icon: Icon.list },
    ],
    devices: [],
    home: [],
};
