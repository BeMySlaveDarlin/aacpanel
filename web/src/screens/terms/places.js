// Where terminals live: the home directory and the projects of the map. A
// place is shown when it has a terminal, and home is shown always, first. The
// wide screen lays the places out one by one, the phone by contour.
import { shortPath } from "../chat/head.js";
import { samePlace } from "../../data/terms.js";

export const HOME = "Home";

// Which place the wide screen stands on, and which places it shows, are kept
// on the device.
export const PLACE_KEY = "aacpanel.terms.place";

export const SHOW_KEY = "aacpanel.terms.shown";

// Which contour the phone pages to is kept on the device as well.
export const CONTOUR_KEY = "aacpanel.terms.contour";

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

// contoursOf lays the places out the way the phone pages them: a page per
// contour of the map in its order, keyed by its name, with the projects of the
// contour that have terminals in the order the map lists them. The personal
// contour is the one the map marks default — kept in the default config
// directory of the owner — or the first where none is, and every other place
// with terminals stands on it: home ahead of its projects, the rest after them
// in the order the host lists them. A place two contours list stands on the
// first. paths are the places a page answers for, with terminals or without,
// so a terminal just started finds its page before the list catches up. A map
// without contours gives one page for every place.
export function contoursOf(places, profileMap, home) {
    const taken = new Set();
    const pages = [];
    for (const profile of profileMap || []) {
        const name = contourName(profile);
        if (!name) continue;
        const paths = projectsOf(profile, home).map((project) => project.path);
        const own = [];
        for (const path of paths) {
            const entry = places.find((p) => samePlace(p.place, path));
            if (!entry || taken.has(entry.place) || entry.terms.length === 0) continue;
            taken.add(entry.place);
            own.push(entry);
        }
        pages.push({ key: name, label: name, contour: name, paths, places: own, personal: Boolean(profile.default) });
    }
    if (pages.length === 0) pages.push({ key: "", label: "Terminals", contour: "", paths: [], places: [] });
    const personal = personalOf(pages);
    const rest = places.filter((p) => !taken.has(p.place) && p.terms.length > 0);
    const atHome = (p) => Boolean(home) && samePlace(p.place, home);
    personal.places = [...rest.filter(atHome), ...personal.places, ...rest.filter((p) => !atHome(p))];
    for (const page of pages) page.count = page.places.reduce((n, p) => n + p.terms.length, 0);
    return pages;
}

// personalOf is the page of the personal contour.
function personalOf(pages) {
    return pages.find((p) => p.personal) || pages[0];
}

// pageOf is the key of the page a place stands on: the contour that lists it,
// or the personal one.
export function pageOf(pages, place) {
    const found = pages.find((p) => p.paths.some((path) => samePlace(path, place))) || personalOf(pages);
    return found ? found.key : "";
}

// pickOf lists where a new terminal can start: home at the top, then the
// projects of the map by contour. A project of the map in the home directory
// is home already and is not offered twice.
export function pickOf(profileMap, home) {
    const out = [];
    if (home) out.push({ contour: "", items: [{ place: home, name: HOME, path: "~" }] });
    for (const profile of profileMap || []) {
        const items = projectsOf(profile, home).map((project) => ({
            place: project.path, name: project.name || baseName(project.path), path: shortPath(project.path),
        }));
        if (items.length) out.push({ contour: contourName(profile), items });
    }
    return out;
}

function contourName(profile) {
    return profile.profile || profile.name || "";
}

// projectsOf is the projects of a contour, one per directory, the home
// directory left out: it is home, not a project.
function projectsOf(profile, home) {
    const out = [];
    for (const group of profile.groups || []) {
        for (const project of group.projects || []) {
            if (!project.path || (home && samePlace(project.path, home))) continue;
            if (!out.some((it) => samePlace(it.path, project.path))) out.push(project);
        }
    }
    return out;
}

function baseName(path) {
    const parts = String(path || "").replace(/\/+$/, "").split("/");
    return parts[parts.length - 1] || String(path || "");
}
