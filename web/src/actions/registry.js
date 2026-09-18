// Registry of state-changing actions: what to tell the person before it happens.

let hostName = "";

export function setHostName(name) {
    if (name) hostName = name;
}

// hostLabel returns the name to call the host by in texts.
export function hostLabel() {
    return hostName || "this machine";
}

function count(n, one, many) {
    return `${n} ${n === 1 ? one : many}`;
}

const KEPT = "Directories on disk stay where they are: what disappears from the map is the entry, not the code. "
    + "Live consoles will not close — they live on the host, and they are closed on the sessions screen.";

// COMMANDS lists the slash commands the panel can send into a session.
export const COMMANDS = {
    clear: {
        name: "Clear the conversation",
        effect: "The console will forget the whole conversation: the context starts from zero. The transcript on disk stays, but the only way back into it is resuming the session.",
        danger: true,
    },
    compact: {
        name: "Compact the context",
        effect: "The conversation collapses into a retelling: the details of its beginning are lost, and the compaction itself costs a request to the model.",
        danger: true,
    },
    finalize: {
        name: "Wrap up",
        effect: "The session will put together a summary and update its notes. That is minutes of work and a noticeable share of the limit.",
    },
    model: {
        name: "Model",
        args: ["default", "fable", "opus", "opus[1m]", "sonnet", "haiku"],
        effect: "The model changes starting with the next request. The context stays where it is, while the price and the speed of the answer change.",
    },
    effort: {
        name: "Effort",
        args: ["low", "medium", "high", "xhigh", "max"],
        effect: "The effort changes starting with the next request: the higher it is, the longer the model thinks over an answer and the faster the limits melt away.",
    },
};

function command(params) {
    return (params && COMMANDS[params.command]) || {};
}

// commandLine renders how a command looks in the session composer.
export function commandLine(params) {
    if (!params || !COMMANDS[params.command]) return "";
    return params.arg ? `/${params.command} ${params.arg}` : `/${params.command}`;
}

// parseCommand reads what is typed in the composer as a slash command.
export function parseCommand(text) {
    const line = String(text || "").trim();
    if (!line.startsWith("/")) return null;
    const space = line.search(/\s/);
    const name = space < 0 ? line.slice(1) : line.slice(1, space);
    const spec = COMMANDS[name];
    if (!spec) return null;
    const arg = space < 0 ? "" : line.slice(space + 1).trim();
    const args = spec.args || [];
    if (args.length === 0) return { command: name, arg: "", ready: arg === "" };
    return { command: name, arg, ready: args.includes(arg) };
}

// commandHints returns the suggestions to show under the line being typed.
export function commandHints(text) {
    const line = String(text || "");
    if (!line.startsWith("/")) return [];
    const space = line.search(/\s/);
    const name = space < 0 ? line.slice(1) : line.slice(1, space);
    const spec = space < 0 ? null : COMMANDS[name];
    if (spec && spec.args) {
        const typed = line.slice(space + 1).trim().toLowerCase();
        return spec.args
            .filter((value) => value.toLowerCase().startsWith(typed))
            .map((value) => ({ value: `/${name} ${value}`, label: value, hint: "" }));
    }
    if (spec) return [];
    const typed = name.toLowerCase();
    return Object.keys(COMMANDS)
        .filter((id) => id.startsWith(typed))
        .map((id) => ({
            value: COMMANDS[id].args ? `/${id} ` : `/${id}`,
            label: `/${id}`,
            hint: COMMANDS[id].name,
        }));
}

export const ACTIONS = {
    "container.stop": {
        title: (target) => `Stop ${target}?`,
        effect: "Dependent containers of the stack will not hear about it.",
        done: (target) => `${target} stopped`,
        ok: "Stop",
        danger: true,
    },
    "container.restart": {
        title: (target) => `Restart ${target}?`,
        effect: "Connections will drop. For a database that can mean open transactions rolled back.",
        done: (target) => `${target} restarted`,
        ok: "Restart",
        danger: true,
    },
    "container.start": {
        title: (target) => `Start ${target}?`,
        effect: "The container comes up with its current image and the variables from compose.",
        done: (target) => `${target} started`,
        ok: "Start",
    },
    "stack.up": {
        title: (target) => `Bring up stack ${target}?`,
        effect: "Every container of the stack comes up, in the dependency order from compose.",
        done: (target) => `Stack ${target} is coming up`,
        ok: "Bring up",
    },
    "stack.down": {
        title: (target) => `Bring down stack ${target}?`,
        effect: "Every container of the stack goes down, in the order reverse to bringing it up. Bringing it back means the \"Bring up stack\" button or doing it by hand from the host.",
        done: (target) => `Stack ${target} is going down`,
        ok: "Bring down",
        danger: true,
    },
    "session.close": {
        watch: "close",
        title: (target) => `Close session ${target}?`,
        effect: "We wait up to 15 seconds for the agent to finish writing the transcript.",
        done: (target) => `Session ${target} is closing`,
        ok: "Close gently",
        danger: true,
        escalate: "session.kill",
        escalateLabel: "Kill right now (kill -9)",
    },
    "session.restart": {
        watch: "restart",
        title: (target) => `Restart ${target} from scratch?`,
        effect: "The conversation ends and a new one starts with an empty context; the old transcript stays in the archive.",
        done: (target) => `Session ${target} is restarting`,
        ok: "Restart",
        danger: true,
    },
    "session.send": {
        instant: true,
        done: (target) => `Sent to ${target}`,
    },
    "session.answer": {
        instant: true,
        done: (target) => `Answer sent to ${target}`,
    },
    "session.permit": {
        instant: true,
        done: (target) => `Permission answer sent to ${target}`,
    },
    "session.dismiss": {
        title: (target) => `Dismiss the question in ${target}?`,
        effect: "the question disappears unanswered: the model gets neither a choice nor words, "
            + "and the session goes back to waiting for an ordinary message. There is nothing to bring the question back with",
        done: (target) => `Question dismissed in ${target}`,
        ok: "Dismiss the question",
    },
    "task.stop": {
        title: (target, params) => `Stop the background work in ${target}?`,
        effect: "The command breaks off wherever it has got to: a half-written file stays "
            + "half-written, and the output stays whatever has piled up by then. There is nothing "
            + "on the panel to start it again, only the session itself will do that",
        done: (target) => `Background work in ${target} stopped`,
        ok: "Stop",
    },
    "agent.stop": {
        title: (target, params) => `Stop subagent${params && params.id ? ` ${params.id}` : ""}?`,
        effect: "Everything it has done and not yet handed over in a message is lost: the session "
            + "gets nothing instead of a report. Silence does not mean it has finished — claude does "
            + "not tell an interim report from the final one",
        done: (target, params) => `Subagent${params && params.id ? ` ${params.id}` : ""} stopped in ${target}`,
        ok: "Stop subagent",
        danger: true,
    },
    "session.stop": {
        instant: true,
        done: (target) => `${target} stopped`,
    },
    "session.escape": {
        instant: true,
        done: (target) => `The composer of ${target} is free`,
    },
    "session.file": {
        instant: true,
        done: (target, params) => {
            const n = (params && params.files && params.files.length) || 1;
            return n > 1 ? `${count(n, "file", "files")} sent to ${target}`
                : `File sent to ${target}`;
        },
    },
    "session.command": {
        title: (target, params) => `Send ${commandLine(params)} to ${target}?`,
        effect: (params) => command(params).effect,
        done: (target, params) => `${commandLine(params)} sent to ${target}`,
        ok: "Send",
        danger: (params) => Boolean(command(params).danger),
    },
    "session.kill": {
        watch: "close",
        title: (target) => `Kill ${target} without warning?`,
        effect: "The transcript may not survive — the session history will not come back through /resume.",
        done: (target) => `Session ${target} killed`,
        ok: "Kill",
        danger: true,
        second: {
            title: (target) => `Really kill ${target}?`,
            effect: "The gentle close was not tried. Only file edits already written to disk will survive.",
            ok: "Kill with kill -9",
        },
    },
    "window.open": {
        title: (target) => `Open a window to ${target} on ${hostLabel()}?`,
        effect: "A terminal window attached to this session appears on the desktop of the machine. "
            + "The conversation itself is not touched: it lives in tmux, and the window only attaches to it.",
        done: (target) => `Window to ${target} opened`,
        ok: "Open window",
    },
    "window.close": {
        title: (target) => `Close window ${target} on ${hostLabel()}?`,
        effect: "The window on the desktop closes, the conversation goes on — the session lives in tmux. "
            + "Everything attached to it detaches, including a console opened over ssh; "
            + "the terminal inside the panel itself is not touched.",
        done: (target) => `Window ${target} closed`,
        ok: "Close window",
    },
    "device.revoke": {
        title: (target) => `Revoke access for "${target}"?`,
        effect: "The device will not sign in any more, and its push subscription goes dark. Access comes back only through a new registration by code.",
        done: (target) => `Device "${target}" revoked`,
        ok: "Revoke",
        danger: true,
        journaled: false,
        send: { verb: "DELETE", path: (target, params) => `/api/devices/${params.id}` },
    },
    "push.disable": {
        title: () => "Turn off notifications on this device?",
        effect: "Alerts will stop arriving. You will learn about failures only by opening the panel yourself.",
        done: () => "Notifications turned off",
        ok: "Turn off",
        danger: true,
        journaled: false,
        send: { verb: "DELETE", path: () => "/api/push/subscription" },
    },
    "push.test": {
        title: () => "Send a test notification?",
        effect: "It will arrive on every subscribed device — that is how delivery is checked without waiting for a real failure.",
        done: () => "Test notification sent",
        ok: "Send",
        journaled: false,
        send: { verb: "POST", path: () => "/api/push/test" },
    },
    "device.revokeSelf": {
        title: (target) => `Revoke this device ("${target}")?`,
        effect: "You will be signed out right now. Getting back in will only be possible from another registered device or by a code from the host — the aacpanel -enroll command.",
        done: () => "Access revoked",
        ok: "Revoke and sign out",
        danger: true,
        journaled: false,
        send: { verb: "DELETE", path: (target, params) => `/api/devices/${params.id}` },
    },
    "alert.ack": {
        instant: true,
        done: () => "Marked as seen",
        journaled: false,
        send: { verb: "POST", path: (target) => `/api/alerts/${target}/ack` },
    },
    "device.rename": {
        title: (target) => `Rename "${target}"?`,
        effect: "The name is visible only in this list — it has no effect on access.",
        done: (target) => `Now it is "${target}"`,
        ok: "Rename",
        journaled: false,
        send: {
            verb: "PATCH",
            path: (target, params) => `/api/devices/${params.id}`,
            body: (target) => ({ name: target }),
        },
    },
    "enroll.issue": {
        title: () => "Issue a registration code?",
        effect: "The code is good for five minutes and goes out after the first registration. Whoever enters it gets access to the panel.",
        done: () => "Code issued",
        ok: "Issue",
        journaled: false,
        send: { verb: "POST", path: () => "/api/enroll/code" },
    },
    "session.open": {
        watch: "open",
        title: (target) => `Open console ${target}?`,
        effect: () => `The console comes up on ${hostLabel()}. The limits it spends come out of the shared quota.`,
        done: (target) => `Console ${target} is up`,
        ok: "Open",
    },
    "session.resume": {
        watch: "open",
        title: (target) => `Resume ${target}?`,
        effect: "A terminal window comes up with the previous conversation: the context returns in full and takes its share of the limit right away.",
        done: (target) => `Conversation ${target} resumed`,
        ok: "Resume",
    },
    "profile.add": {
        title: (target) => `Create profile "${target}"?`,
        effect: "The profile appears on the map. While it has no groups and no projects, there is nothing to launch from it.",
        done: (target) => `Profile "${target}" created`,
        ok: "Add",
        journaled: false,
        fieldConflict: true,
        send: {
            verb: "POST",
            path: () => "/api/profiles",
            body: (target, params) => params.fields,
        },
    },
    "profile.edit": {
        title: (target) => `Save profile "${target}"?`,
        effect: "The new values go into the launch of the next sessions. Consoles already up stay the way they were — they read their settings at start.",
        done: (target) => `Profile "${target}" saved`,
        ok: "Save",
        journaled: false,
        fieldConflict: true,
        send: {
            verb: "PATCH",
            path: (target, params) => `/api/profiles/${params.id}`,
            body: (target, params) => params.fields,
        },
    },
    "profile.remove": {
        title: (target) => `Delete profile "${target}"?`,
        effect: (params) => (params.projects > 0
            ? `The profile leaves the map whole: ${count(params.groups, "group", "groups")} `
              + `and ${count(params.projects, "project", "projects")} go with it. ` + KEPT
            : `The profile leaves the map together with its empty groups (${count(params.groups, "group", "groups")}). `
              + "It has no projects — there is nothing to lose."),
        done: (target) => `Profile "${target}" deleted`,
        ok: "Delete",
        danger: true,
        journaled: false,
        send: {
            verb: "DELETE",
            path: (target, params) => `/api/profiles/${params.id}${params.cascade ? "?cascade=1" : ""}`,
        },
    },
    "group.add": {
        title: (target) => `Create group "${target}"?`,
        effect: (params) => `The group appears in profile "${params.profile}". Projects are put into it separately — that is a different job.`,
        done: (target) => `Group "${target}" created`,
        ok: "Add",
        journaled: false,
        fieldConflict: true,
        send: {
            verb: "POST",
            path: (target, params) => `/api/profiles/${params.profileId}/groups`,
            body: (target, params) => params.fields,
        },
    },
    "group.edit": {
        title: (target) => `Save group "${target}"?`,
        effect: (params) => (params.toProfile
            ? `The group moves to profile "${params.toProfile}" together with its projects`
              + (params.projects ? ` (${count(params.projects, "project", "projects")})` : "")
              + `: their sessions will come up with its token and spend its subscription, not the one of "${params.fromProfile}". `
            : "")
            + "The name is visible on the map and in the launch list. The projects stay in this same group.",
        done: (target) => `Group "${target}" saved`,
        ok: "Save",
        journaled: false,
        fieldConflict: true,
        send: {
            verb: "PATCH",
            path: (target, params) => `/api/groups/${params.id}`,
            body: (target, params) => params.fields,
        },
    },
    "group.remove": {
        title: (target) => `Delete group "${target}"?`,
        effect: (params) => (params.projects > 0
            ? `The group leaves profile "${params.profile}" together with its projects `
              + `(${count(params.projects, "project", "projects")}). ` + KEPT
            : `The empty group leaves profile "${params.profile}". Projects and directories are not affected.`),
        done: (target) => `Group "${target}" deleted`,
        ok: "Delete",
        danger: true,
        journaled: false,
        send: {
            verb: "DELETE",
            path: (target, params) => `/api/groups/${params.id}${params.cascade ? "?cascade=1" : ""}`,
        },
    },
    "project.add": {
        title: (target) => `Create project "${target}"?`,
        effect: (params) => `The project goes into group "${params.group}" and appears in the launch list. `
            + "If there is no such directory on the host, it is created, and the panel grants it trust "
            + "so that the first session opens straight away instead of stopping at the trust question. "
            + "A directory that is already there is left as it is — permissions, owner and contents alike. "
            + "A path the host does not accept as its own is refused, and then nothing is written at all.",
        done: (target) => `Project "${target}" created`,
        ok: "Add",
        journaled: false,
        fieldConflict: true,
        entails: "project.create",
        send: {
            verb: "POST",
            path: (target, params) => `/api/groups/${params.groupId}/projects`,
            body: (target, params) => params.fields,
        },
    },
    "project.edit": {
        title: (target) => `Save project "${target}"?`,
        effect: (params) => (params.moveTo
            ? `The project moves to group "${params.moveTo}" and lands at its end. `
            : "")
            + "The new values go into the launch of the next sessions. Live consoles of this project will not change.",
        done: (target) => `Project "${target}" saved`,
        ok: "Save",
        journaled: false,
        fieldConflict: true,
        send: {
            verb: "PATCH",
            path: (target, params) => `/api/projects/${params.id}`,
            body: (target, params) => params.fields,
        },
    },
    "project.remove": {
        title: (target) => `Delete project "${target}"?`,
        effect: (params) => `The project disappears from the launch list of group "${params.group}" — it will not be possible to open it from the phone. `
            + `The directory ${params.path} and everything inside it stays where it is. `
            + "A live console of this project will not close: it lives on the host, and what leaves the map is the entry. "
            + "It will still be visible in the same place, on the sessions screen — and that is where it is closed.",
        done: (target) => `Project "${target}" deleted`,
        ok: "Delete",
        danger: true,
        journaled: false,
        send: {
            verb: "DELETE",
            path: (target, params) => `/api/projects/${params.id}`,
        },
    },
    "profile.reorder": {
        instant: true,
        done: () => "Order saved",
        journaled: false,
        send: {
            verb: "PUT",
            path: () => "/api/profiles/order",
            body: (target, params) => ({ ids: params.ids }),
        },
    },
    "group.reorder": {
        instant: true,
        done: () => "Order saved",
        journaled: false,
        send: {
            verb: "PUT",
            path: (target, params) => `/api/profiles/${params.profileId}/groups/order`,
            body: (target, params) => ({ ids: params.ids }),
        },
    },
    "disk.hide": {
        instant: true,
        done: (target) => `${target} taken out of the review queue`,
        journaled: false,
        send: {
            verb: "POST",
            path: () => "/api/disk/hidden",
            body: (target) => ({ path: target }),
        },
    },
    "disk.show": {
        instant: true,
        done: (target) => `${target} is back in the review queue`,
        journaled: false,
        send: {
            verb: "DELETE",
            path: () => "/api/disk/hidden",
            body: (target) => ({ path: target }),
        },
    },
    "project.reorder": {
        instant: true,
        done: () => "Order saved",
        journaled: false,
        send: {
            verb: "PUT",
            path: (target, params) => `/api/groups/${params.groupId}/projects/order`,
            body: (target, params) => ({ ids: params.ids }),
        },
    },
};

// known reports whether the identifier is a registered action.
export function known(id) {
    return Object.hasOwn(ACTIONS, id);
}

const NAMES = {
    "container.stop": "Stop",
    "container.restart": "Restart",
    "container.start": "Start",
    "stack.up": "Bring up stack",
    "stack.down": "Bring down stack",
    "session.close": "Close session",
    "session.restart": "Restart session",
    "session.kill": "Kill session",
    "session.open": "Open console",
    "session.resume": "Resume session",
    "session.send": "Write to session",
    "session.answer": "Answer the question",
    "session.dismiss": "Dismiss the question",
    "session.permit": "Answer the permission request",
    "session.stop": "Stop the work",
    "session.escape": "Free the composer",
    "task.stop": "Stop background work",
    "agent.stop": "Stop subagent",
    "session.file": "Send file",
    "session.command": "Slash command",
    "window.open": "Open window",
    "window.close": "Close window",
    "device.revoke": "Revoke device",
    "device.revokeSelf": "Revoke device",
    "device.rename": "Rename device",
    "enroll.issue": "Issue code",
    "push.disable": "Turn off notifications",
    "push.test": "Test notification",
    "profile.add": "Create profile",
    "profile.edit": "Edit profile",
    "profile.remove": "Delete profile",
    "group.add": "Create group",
    "group.edit": "Edit group",
    "group.remove": "Delete group",
    "project.add": "Create project",
    "project.edit": "Edit project",
    "project.remove": "Delete project",
    "disk.hide": "Hide directory",
    "disk.show": "Restore directory",
    "profile.reorder": "Reorder profile",
    "group.reorder": "Reorder group",
    "project.reorder": "Reorder project",
};

export function actionName(kind) {
    return NAMES[kind] || kind;
}
