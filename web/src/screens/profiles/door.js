// The door to a project's settings from outside the map — the project screen
// of Sessions: the map is loaded for it, and the page is the map's own edit
// layer, the same one the Profiles tab opens.
import { html } from "../../html.js";
import { BackHead, useBackClose } from "../../ui/back.js";
import { EditLayer } from "./forms.js";
import { locate } from "./settings.js";
import { useProfileMap } from "./state.js";

export function ProjectDoor({ id, sessions, onClose }) {
    const map = useProfileMap();
    const found = locate(map.profiles, id);
    // A layer of its own only for the page that says the project is gone:
    // while the map loads there is nothing to go back from yet, and once it
    // has come the settings page holds the back gesture itself.
    useBackClose(Boolean(map.profiles) && !found.project, onClose);
    if (!found.project) {
        return html`
            <${BackHead} onBack=${onClose} label="back"><h2>Settings</h2><//>
            <p class=${map.error ? "hint crit" : "empty"}>${map.error || (map.profiles ? "The project is not on the map any more." : "Loading…")}</p>
        `;
    }
    return html`<${EditLayer}
        form=${{ kind: "project", mode: "edit", profile: found.contour, group: found.group, project: found.project }}
        profiles=${map.profiles}
        catalog=${map.catalog}
        disk=${map.disk}
        accounts=${map.accounts}
        sessions=${sessions}
        order=${null}
        onClose=${onClose}
        onDone=${map.apply}
        onRemove=${map.remove}
    />`;
}

// ContourDoor opens a contour's settings from its page in Sessions.
export function ContourDoor({ id, onClose }) {
    const map = useProfileMap();
    const contour = (map.profiles || []).find((p) => p.id === id);
    useBackClose(Boolean(map.profiles) && !contour, onClose);
    if (!contour) {
        return html`
            <${BackHead} onBack=${onClose} label="back"><h2>Settings</h2><//>
            <p class=${map.error ? "hint crit" : "empty"}>${map.error || (map.profiles ? "The contour is not on the map any more." : "Loading…")}</p>
        `;
    }
    return html`<${EditLayer}
        form=${{ kind: "profile", mode: "edit", profile: contour }}
        profiles=${map.profiles}
        catalog=${map.catalog}
        disk=${map.disk}
        accounts=${map.accounts}
        order=${null}
        onClose=${onClose}
        onDone=${map.apply}
        onRemove=${map.remove}
    />`;
}
