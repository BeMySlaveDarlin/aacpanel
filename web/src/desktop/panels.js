// The right-hand panel: what belongs to the open section but not to the centre.
import { html } from "../html.js";
import { Icon } from "../ui/icons.js";
import { Archive } from "./panels/archive.js";
import { Journal } from "./panels/journal.js";
import { Logs } from "./panels/logs.js";
import { Procs } from "./panels/procs.js";

export function RightPanel({ tab, title, profiles, names, archPicks, setArchPicks, container, exec, onOpen, onClose }) {
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
        </aside>
    `;
}

// The buttons of a section beside its sections. Most open a panel at the
// right; one marked layer opens over the whole shell instead — the map is
// settings in three columns, too wide for a panel.
export const PANELS = {
    sessions: [
        { id: "archive", label: "Session archive", icon: Icon.clock },
        { id: "projects", label: "Projects", icon: Icon.cube, layer: true },
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
    devices: [],
    home: [],
};
