// The pure logic of the profile map: whose directory is whose, and what a search
// leaves of the map.

// folderOf returns the top-level folder under the scan root and the relative path.
export function folderOf(path, roots) {
    let best = "";
    for (const root of roots || []) {
        if ((path === root || path.startsWith(`${root}/`)) && root.length > best.length) best = root;
    }
    if (!best) return { folder: path, rel: path };
    const rel = path.slice(best.length + 1);
    const cut = rel.indexOf("/");
    return { folder: cut < 0 ? rel : rel.slice(0, cut), rel };
}

// contourOf returns whose contour a directory found on disk falls into.
export function contourOf(path, profiles) {
    let best = null;
    for (const profile of profiles || []) {
        const prefix = (profile.prefix || "").replace(/\/+$/, "");
        if (!prefix) continue;
        if (path !== prefix && !path.startsWith(`${prefix}/`)) continue;
        if (!best || prefix.length > best.prefix.length) best = { name: profile.name, prefix };
    }
    if (best) return best.name;
    const rest = (profiles || []).find((profile) => !profile.prefix) || (profiles || [])[0];
    return rest ? rest.name : "";
}

// looseOf returns what of the disk findings belongs to this contour.
export function looseOf(disk, profiles, name) {
    if (!disk || disk.state !== "ok") return [];
    return (disk.dirs || []).filter((dir) => contourOf(dir.path, profiles) === name);
}

// hiddenOf returns what of the hidden directories belongs to this contour.
export function hiddenOf(disk, profiles, name) {
    if (!disk) return [];
    return (disk.hidden || []).filter((path) => contourOf(path, profiles) === name);
}

// guessGroup returns which group to offer for a directory found on disk.
export function guessGroup(profile, path, roots) {
    const { folder } = folderOf(path, roots);
    let best = null;
    for (const group of (profile.groups || [])) {
        let n = 0;
        for (const project of (group.projects || [])) {
            if (folderOf(project.path, roots).folder === folder) n += 1;
        }
        if (n > 0 && (!best || n > best.n)) best = { id: group.id, n };
    }
    return best ? String(best.id) : "";
}

// authState returns how the contour is authorized.
export function authState(profile) {
    if (profile.auth === "builtin") return { dot: "ok", text: "built-in authorization" };
    if (profile.auth === "token") return { dot: "ok", text: "own token" };
    if (profile.auth === "missing") return { dot: "warn", text: "token not found" };
    return { dot: "off", text: "authorization unknown" };
}

// dotOf returns what the group dot says.
export function dotOf(group, gone) {
    const projects = group.projects || [];
    if (projects.length === 0) return "off";
    if (gone && projects.some((project) => gone.has(project.id))) return "warn";
    return "ok";
}

// filterGroups returns what is left of the contour map under a search query.
export function filterGroups(groups, query) {
    const needle = (query || "").trim().toLowerCase();
    if (!needle) return groups || [];
    const out = [];
    for (const group of groups || []) {
        if ((group.name || "").toLowerCase().includes(needle)) {
            out.push(group);
            continue;
        }
        const projects = (group.projects || []).filter((project) =>
            `${project.name} ${project.path}`.toLowerCase().includes(needle));
        if (projects.length > 0) out.push({ ...group, projects });
    }
    return out;
}
