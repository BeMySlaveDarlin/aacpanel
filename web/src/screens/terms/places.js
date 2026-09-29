// Where terminals live: the home directory and the projects of the map. A
// place is shown when it has a terminal, and home is shown always, first.
import { shortPath } from "../chat/head.js";
import { samePlace } from "../../data/terms.js";

export const HOME = "Home";

// Which place a pager stands on, and which places the wide screen shows,
// are kept on the device: the phone and the desk share them.
export const PLACE_KEY = "aacpanel.terms.place";

export const SHOW_KEY = "aacpanel.terms.shown";

// homeOf is the home directory of the host: the snapshot names it, and a
// terminal the host keeps there names it too.
export function homeOf(snapshot, terms) {
    const said = snapshot && snapshot.host && snapshot.host.home;
    if (said) return String(said);
    const kept = (terms || []).find((t) => t.label === HOME);
    return kept ? kept.place : "";
}

// placesOf lays the places out in the order the screen reads them: home
// first, then every other place with a terminal in the order the host lists
// them. Each carries its terminals.
export function placesOf(terms, home) {
    const out = [];
    const at = new Map();
    const add = (place, label) => {
        const key = String(place).replace(/\/+$/, "") || place;
        if (!at.has(key)) {
            at.set(key, out.length);
            out.push({ place: key, label, terms: [] });
        }
        return out[at.get(key)];
    };
    if (home) add(home, HOME);
    for (const t of terms || []) {
        const atHome = Boolean(home) && samePlace(t.place, home);
        const entry = add(atHome ? home : t.place, atHome ? HOME : t.label || baseName(t.place));
        entry.terms.push(t);
    }
    return out;
}

// tabsOf is what a terminal opened in a place stands among: the place, its
// tabs and the open one. A terminal just started may be ahead of the list, and
// it stands as a tab of its own until the list catches up.
export function tabsOf(places, open) {
    const entry = places.find((p) => samePlace(p.place, open.place))
        || { place: open.place, label: baseName(open.place), terms: [] };
    const tabs = entry.terms.some((t) => t.id === open.id)
        ? entry.terms
        : [...entry.terms, { id: open.id, place: open.place, name: "" }];
    return { entry, tabs, t: tabs.find((it) => it.id === open.id) };
}

// afterClose is the tab that gives way to a closed one: the one of the place
// typed into last, or null when it was the last.
export function afterClose(entry, gone) {
    const rest = entry.terms.filter((it) => it.id !== gone.id);
    if (rest.length === 0) return null;
    return rest.reduce((a, b) => ((b.activity || 0) > (a.activity || 0) ? b : a));
}

// placeLabel names a place by its key among the places laid out.
export function placeLabel(places, place) {
    const found = places.find((p) => p.place === place);
    return found ? found.label : baseName(place);
}

// pickOf lists where a new terminal can start: home at the top, then the
// projects of the map by contour. A project of the map in the home directory
// is home already and is not offered twice.
export function pickOf(profileMap, home) {
    const out = [];
    if (home) out.push({ contour: "", items: [{ place: home, name: HOME, path: "~" }] });
    for (const profile of profileMap || []) {
        const items = [];
        for (const group of profile.groups || []) {
            for (const project of group.projects || []) {
                if (!project.path || (home && samePlace(project.path, home))) continue;
                if (items.some((it) => samePlace(it.place, project.path))) continue;
                items.push({ place: project.path, name: project.name || baseName(project.path), path: shortPath(project.path) });
            }
        }
        if (items.length) out.push({ contour: profile.profile || profile.name || "", items });
    }
    return out;
}

function baseName(path) {
    const parts = String(path || "").replace(/\/+$/, "").split("/");
    return parts[parts.length - 1] || String(path || "");
}
