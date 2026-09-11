// The state of the profile map: the map itself, what is open on it and what is
// being edited.
import { useCallback, useEffect, useState } from "preact/hooks";

import { useAction } from "../../actions/gate.js";
import { useProfilePage } from "../sessions/pages.js";
import { moveIds } from "./move.js";

export function useProfileMap() {
    const [profiles, setProfiles] = useState(null);
    const [catalog, setCatalog] = useState(null);
    const [disk, setDisk] = useState(null);
    const [error, setError] = useState("");
    const [open, setOpen] = useState(() => new Set());
    const [form, setForm] = useState(null);
    const [loose, setLoose] = useState(false);
    const run = useAction();

    const names = (profiles || []).map((p) => p.name);
    const [current, pick] = useProfilePage(names);

    const load = useCallback(async () => {
        try {
            const response = await fetch("/api/profiles", { credentials: "same-origin" });
            if (!response.ok) throw new Error(await message(response));
            const body = await response.json();
            setProfiles(body.profiles || []);
            setCatalog(body.models || null);
            setDisk(body.disk || null);
            setError("");
        } catch (err) {
            setError(err.message);
        }
    }, []);

    useEffect(() => {
        load();
    }, [load]);

    const apply = useCallback((data) => {
        if (!data || !Array.isArray(data.profiles)) {
            load();
            return;
        }
        setProfiles(data.profiles);
        setCatalog(data.models || null);
        if (data.disk) setDisk(data.disk);
        setError("");
        if (data.profile) pick(data.profile.name);
        const reveal = [];
        if (data.group) reveal.push(`g:${data.group.id}`);
        if (data.project) reveal.push(`g:${data.project.groupId}`, `r:${data.project.id}`);
        if (reveal.length > 0) setOpen((prev) => new Set([...prev, ...reveal]));
    }, [load, pick]);

    const toggle = useCallback((key) => {
        setOpen((prev) => {
            const next = new Set(prev);
            if (next.has(key)) next.delete(key);
            else next.add(key);
            return next;
        });
    }, []);

    const remove = useCallback(async (spec) => {
        const result = await drop(run, spec);
        if (result && result.ok) apply(result.data);
        return result;
    }, [run, apply]);

    const reorder = useCallback(async (id, target, params) => {
        const result = await run(id, target, params);
        if (result && result.ok) apply(result.data);
    }, [run, apply]);

    const gone = new Set((disk && disk.missing) || []);
    const profile = (profiles || []).find((p) => p.name === current) || null;

    return {
        profiles, catalog, disk, error, gone,
        names, current, pick, profile,
        open, toggle,
        form, setForm,
        loose, setLoose,
        apply, remove, reorder,
    };
}

// orderOf returns where a row stands among its neighbours and how to move it.
export function orderOf(profiles, form, reorder) {
    if (!profiles || form.mode !== "edit") return null;

    let items = null;
    let id = 0;
    let kind = "";
    let params = null;
    if (form.kind === "profile") {
        items = profiles;
        id = form.profile.id;
        kind = "profile.reorder";
        params = {};
    } else if (form.kind === "group") {
        const own = profiles.find((p) => p.id === form.profile.id);
        items = own && own.groups;
        id = form.group.id;
        kind = "group.reorder";
        params = { profileId: form.profile.id };
    } else {
        const own = profiles.find((p) => p.id === form.profile.id);
        const group = own && (own.groups || []).find((g) => g.id === form.group.id);
        items = group && group.projects;
        id = form.project.id;
        kind = "project.reorder";
        params = { groupId: form.group.id };
    }

    const list = items || [];
    const index = list.findIndex((it) => it.id === id);
    if (index < 0 || list.length < 2) return null;
    return {
        index,
        total: list.length,
        name: list[index].name,
        move: (dir) => {
            const ids = moveIds(list, index, dir);
            if (ids) reorder(kind, list[index].name, { ...params, ids });
        },
    };
}

function drop(run, spec) {
    if (spec.kind === "profile") {
        const groups = spec.profile.groups || [];
        const projects = groups.reduce((n, group) => n + (group.projects || []).length, 0);
        return run("profile.remove", spec.profile.name, {
            id: spec.profile.id,
            groups: groups.length,
            projects,
            cascade: projects > 0,
        });
    }
    if (spec.kind === "group") {
        const projects = (spec.group.projects || []).length;
        return run("group.remove", spec.group.name, {
            id: spec.group.id,
            profile: spec.profile.name,
            projects,
            cascade: projects > 0,
        });
    }
    return run("project.remove", spec.project.name, {
        id: spec.project.id,
        path: spec.project.path,
        group: spec.group.name,
    });
}

async function message(response) {
    const text = (await response.text()).trim();
    try {
        const parsed = JSON.parse(text);
        if (parsed && parsed.error) return parsed.error;
    } catch (err) {
    }
    if (response.status === 401) return "you need to sign in again";
    if (response.status === 404) return "the profile map is not wired up on the server yet";
    if (response.status === 503) return "the database is unavailable — the map is kept in it";
    return text || `the server answered ${response.status}`;
}
