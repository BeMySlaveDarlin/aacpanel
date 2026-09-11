// The home session button in the centre of the bottom menu.
import { html } from "../html.js";
import { Icon } from "./icons.js";
import { useAction } from "../actions/gate.js";
import { knows, whyNot } from "../exec.js";
import { useToast } from "./toasts.js";

// homeSession returns the host home session among the live ones.
export function homeSession(snapshot) {
    return ((snapshot && snapshot.sessions) || []).find((s) => s.home) || null;
}

// homeProject returns the project on the map whose path is the home directory.
export function homeProject(snapshot) {
    const home = ((snapshot && snapshot.host) || {}).home;
    if (!home) return null;
    const want = String(home).replace(/\/+$/, "");
    for (const profile of (snapshot && snapshot.profileMap) || []) {
        for (const group of profile.groups || []) {
            for (const project of group.projects || []) {
                if (String(project.path || "").replace(/\/+$/, "") === want) return project;
            }
        }
    }
    return null;
}

// HomeButton renders the button itself, carrying the session state in its look.
export function HomeButton({ snapshot, exec, onChat, onOpened }) {
    const run = useAction();
    const toast = useToast();

    const live = homeSession(snapshot);
    const project = homeProject(snapshot);
    const ready = knows(exec, "session.open");
    const why = whyNot(exec, "session.open");

    const state = !live ? "" : live.waitingFor || live.status === "waiting" ? " waiting" : live.status === "busy" ? " busy" : "";

    const label = live
        ? `open conversation ${live.session}${live.status === "busy" ? " · handling the request" : ""}`
        : "start the home session";

    const press = async () => {
        if (live) {
            onChat(live.session, live.sessionId || null);
            return;
        }
        if (!ready) {
            toast("There is nothing to start a session with", why, true);
            return;
        }
        if (!project) {
            toast("There is no home project on the map", "add a project with the path of the home directory", true);
            return;
        }
        const result = await run("session.open", project.session || project.name, { project: project.id });
        if (result.ok && onOpened) onOpened();
    };

    return html`
        <button
            class=${`homebtn${live ? " live" : ""}${state}`}
            type="button"
            aria-label=${label}
            title=${label}
            onClick=${press}
        >${Icon.orbit()}</button>
    `;
}
