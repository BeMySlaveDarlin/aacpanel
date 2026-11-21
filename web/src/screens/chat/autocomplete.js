import { useEffect, useRef, useState } from "preact/hooks";

import { completionSources } from "../../actions/registry.js";
import { agentName, shownName } from "../../agent.js";
import { sessionsOf } from "../sessions/of.js";

export const COMPLETION_DELAY = 180;

export function completionAt(text, start, end = start) {
    if (start !== end) return null;
    const before = text.slice(0, start);
    const match = /(?:^|[\s([{])([@#])([^\s()[\]{}@#]*)$/.exec(before);
    if (!match) return null;
    const from = start - match[2].length - 1;
    const tail = /^[^\s()[\]{}@#]*/.exec(text.slice(start))[0];
    return { kind: match[1], query: match[2].toLowerCase(), from, to: start + tail.length };
}

function nameHints(query, profiles, sessions) {
    const rows = [];
    const add = (label, tag) => {
        if (label && label.toLowerCase().includes(query)) rows.push({ label, tag, value: `${label} ` });
    };
    for (const session of sessions) add(shownName(session), `session · ${agentName(session)}`);
    for (const contour of profiles || []) {
        add(contour.profile || contour.name, "contour");
        for (const group of contour.groups || []) {
            add(group.name, "group");
            for (const project of group.projects || []) add(project.name, "project");
        }
    }
    return rows.sort((a, b) => Number(!a.label.toLowerCase().startsWith(query)) - Number(!b.label.toLowerCase().startsWith(query))
        || a.label.localeCompare(b.label));
}

export function useCompletions(name, at) {
    const sources = completionSources();
    const [fallback, setFallback] = useState(null);
    const mapRequest = useRef(null);
    const profiles = sources.profiles || fallback;
    const want = Boolean(at);
    useEffect(() => {
        if (!want || profiles !== null || mapRequest.current) return;
        const controller = new AbortController();
        mapRequest.current = controller;
        (async () => {
            try {
                const response = await fetch("/api/profiles", { credentials: "same-origin", signal: controller.signal });
                if (response.ok) {
                    const body = await response.json();
                    if (!controller.signal.aborted) setFallback(body.profiles || []);
                }
            } catch {
            }
        })();
    }, [want, profiles]);
    useEffect(() => () => mapRequest.current?.abort(), []);

    const live = sources.sessions.find((s) => s.session === name);
    let project = live?.project?.id || null;
    if (live && live.project === undefined) {
        for (const contour of profiles || []) {
            for (const group of contour.groups || []) {
                const found = (group.projects || []).find((p) => sessionsOf(p, [live]).length);
                if (found) project = found.id;
            }
        }
    }
    const query = at?.kind === "#" && project ? at.query : null;
    const key = query === null ? "" : `${project}:${query}`;
    const [tasks, setTasks] = useState({ key: "", rows: [] });
    useEffect(() => {
        if (query === null) return;
        const controller = new AbortController();
        const timer = setTimeout(async () => {
            try {
                const read = async (status) => {
                    const params = new URLSearchParams({ project, q: query, status, limit: 30 });
                    const response = await fetch(`/api/tasks?${params}`, { credentials: "same-origin", signal: controller.signal });
                    if (!response.ok) throw new Error("The task list is unavailable.");
                    return (await response.json()).tasks || [];
                };
                // Separate pages keep closed high-priority tasks from displacing open ones at the limit.
                const pages = await Promise.all([read("open,deferred"), ...(query ? [read("done,cancelled")] : [])]);
                const rows = pages.flat().filter((task) => String(task.number).includes(query) || task.title.toLowerCase().includes(query))
                    .map((task) => ({ label: `#${task.number} [${task.title}]`, tag: task.status, value: `#${task.number} [${task.title}] ` }));
                if (!controller.signal.aborted) setTasks({ key, rows });
            } catch {
                if (!controller.signal.aborted) setTasks({ key, rows: [] });
            }
        }, COMPLETION_DELAY);
        return () => { clearTimeout(timer); controller.abort(); };
    }, [project, query]);

    if (!at) return [];
    return at.kind === "@" ? nameHints(at.query, profiles, sources.sessions) : tasks.key === key ? tasks.rows : [];
}
