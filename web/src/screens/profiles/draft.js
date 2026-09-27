// The draft of a project's settings: what the person changed since the page
// opened and nothing else — so Save sends only what was touched, and a value
// put back as it was is no change at all. A launch key set to null is a key
// the draft removes: the project goes back to what its contour and account say.

const EMPTY = {};

export function emptyDraft() {
    return { fields: {}, launch: {} };
}

function same(a, b) {
    return JSON.stringify(a ?? null) === JSON.stringify(b ?? null);
}

function stored(project, key) {
    const launch = (project && project.launch) || EMPTY;
    return Object.prototype.hasOwnProperty.call(launch, key) ? launch[key] : null;
}

// put returns the draft with a launch key changed: null removes the project's
// own value, and a value equal to the stored one takes the key out of the draft.
export function put(draft, project, key, value) {
    const launch = { ...draft.launch };
    if (same(value, stored(project, key))) delete launch[key];
    else launch[key] = value;
    return { ...draft, launch };
}

// field returns the draft with a project field changed.
export function field(draft, project, name, value) {
    const fields = { ...draft.fields };
    const was = project ? project[name] : undefined;
    if (same(value, was ?? (name === "groupId" ? null : ""))) delete fields[name];
    else fields[name] = value;
    return { ...draft, fields };
}

// revert takes a key or a field out of the draft.
export function revert(draft, key) {
    const fields = { ...draft.fields };
    const launch = { ...draft.launch };
    delete fields[key];
    delete launch[key];
    return { fields, launch };
}

export function count(draft) {
    return Object.keys(draft.fields).length + Object.keys(draft.launch).length;
}

// touched says whether a key or a field is in the draft.
export function touched(draft, key) {
    return key in draft.launch || key in draft.fields;
}

// own returns the project's own value of a launch key as the draft leaves it:
// null where the project says nothing of it.
export function own(draft, project, key) {
    if (key in draft.launch) return draft.launch[key];
    return stored(project, key);
}

// fieldOf returns a project field as the draft leaves it.
export function fieldOf(draft, project, name) {
    if (name in draft.fields) return draft.fields[name];
    return project ? project[name] : undefined;
}

// body returns what a Save sends: the fields changed and the launch keys set
// or removed, each only when there is one.
export function body(draft) {
    const out = { ...draft.fields };
    const set = {};
    const unset = [];
    for (const [key, value] of Object.entries(draft.launch)) {
        if (value === null) unset.push(key);
        else set[key] = value;
    }
    if (Object.keys(set).length > 0) out.launchSet = set;
    if (unset.length > 0) out.launchUnset = unset;
    return out;
}

// weighty says whether a Save needs its own sheet: a new directory leaves the
// conversations of the old one behind, a new group moves the project.
export function weighty(draft) {
    return "path" in draft.fields || "groupId" in draft.fields;
}

// valueOf returns an effective value of a key, as the service laid it.
export function valueOf(effective, key) {
    return (effective || []).find((v) => v.key === key) || { key, value: null, layer: "claude" };
}

// overlay returns the effective values the draft leads to, as far as the page
// can tell without the service: a key the draft sets is the project's own, a
// key it removes is what the contour gives. It stands only while the service's
// answer is on its way — that answer is the one the launch is built by.
export function overlay(effective, draft, contourEffective) {
    return (effective || []).map((v) => {
        if (!(v.key in draft.launch)) return v;
        const value = draft.launch[v.key];
        if (value === null) return valueOf(contourEffective, v.key);
        return { key: v.key, value, layer: "project" };
    });
}

const SOURCES = {
    account: "the account",
    contour: "the contour",
    panel: "the panel's default",
    project: "this project",
};

export function sourceOf(layer) {
    return SOURCES[layer] || "";
}

// label returns how a value reads on the page: the option's own label where
// the schema has one, on and off for a switch, the number with its unit.
export function label(param, value) {
    if (value === null || value === undefined) return "";
    if (param.kind === "bool") return value ? "On" : "Off";
    if (param.kind === "int") return `${value}${param.unit || ""}`;
    const option = (param.options || []).find((o) => o.value === value);
    if (option) return option.label;
    if (Array.isArray(value)) return value.join(" ");
    if (typeof value === "object") return Object.keys(value).join(", ");
    return String(value);
}

// outcome says what a parameter comes to where the project says nothing of
// it: the value the layers below give with where it came from, or what the
// schema says an absent value leaves to — never "not set".
export function outcome(param, eff) {
    if (eff.layer === "claude" || eff.value === null || eff.value === undefined) {
        return param.unset;
    }
    const text = param.kind === "text" && eff.value === "" ? "none" : label(param, eff.value);
    return `${text} — ${sourceOf(eff.layer)}`;
}

// liveOf returns when a running session takes a change of the parameter,
// where the session lives as the draft leaves it.
export function liveOf(param, transport) {
    const live = param.live || EMPTY;
    return live[transport || "tmux"] || "";
}

// pins returns the owner's own values that repeat what the contour or the
// account would give anyway: pinned, they stop following a change down there,
// and the page offers to take the pin out. A value equal to the panel's own
// default is not offered: that default moves only with a release, and a value
// set against it is a choice, not a copy.
export function pins(params, draft, project, contourEffective) {
    const out = [];
    for (const param of params) {
        const mine = own(draft, project, param.key);
        if (mine === null) continue;
        const below = valueOf(contourEffective, param.key);
        if (below.layer === "claude" || below.layer === "panel" || !same(mine, below.value)) continue;
        out.push({
            key: param.key,
            text: `${param.label} ${label(param, mine)} here is the same as ${sourceOf(below.layer)} — remove the pin?`,
        });
    }
    return out;
}

// consequences lists what living in one place means for this project: what
// the option itself says, which parameters a running session then takes at
// another time, and what the project's own values turn into there.
export function consequences(params, transport, eff) {
    const out = [];
    const where = transport === "stream" ? "stream" : "tmux";
    const other = where === "stream" ? "tmux" : "stream";
    for (const param of params) {
        if (param.key === "transport") continue;
        const here = liveOf(param, where);
        const there = liveOf(param, other);
        if (here && there && here !== there) {
            out.push(`${param.label}: ${here === "now" ? "changes from the panel on a running session" : `takes effect ${here === "next start" ? "at the next start" : "on a move"}`}`);
        }
    }
    const rc = valueOf(eff, "remoteControl").value === true;
    if (where === "stream" && rc) out.push("Remote Control is raised by the panel once claude answers");
    const args = valueOf(eff, "args").value;
    if (where === "stream" && Array.isArray(args) && args.length > 0) {
        out.push(`the extra arguments go to claude -p: one only the terminal knows stops the session at its start`);
    }
    return out;
}

// exit returns the one press that takes a problem away: undoing the change
// when the draft made it, removing the project's value when it was stored.
export function exit(draft, param, key) {
    const name = param ? param.label : key;
    if (touched(draft, key)) return { text: "Undo the change", run: (d) => revert(d, key) };
    return { text: `Remove ${name}`, remove: key };
}

// fieldProblems returns what in the project's own fields Save must not send.
export function fieldProblems(draft, project) {
    const out = [];
    const name = String(fieldOf(draft, project, "name") || "").trim();
    const path = String(fieldOf(draft, project, "path") || "").trim();
    if (!name) out.push({ key: "name", why: "without a caption the project cannot be found in the launch list" });
    if (!path) out.push({ key: "path", why: "the directory is required: the session is brought up in it" });
    else if (!path.startsWith("/") || path.split("/").includes("..")) {
        out.push({ key: "path", why: "the directory has to be an absolute path, without “..”" });
    }
    return out;
}

// tokens reads extra arguments typed into a field: a word each, split on
// spaces — there are no quotes and no escaping.
export function tokens(text) {
    return String(text || "").split(/\s+/).filter(Boolean);
}
