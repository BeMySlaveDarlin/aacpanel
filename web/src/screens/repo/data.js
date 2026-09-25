// What the viewer asks the panel for. Every answer carries the revision it was
// read at, and a window asked for under one the repository has moved past comes
// back stale: the screen asks again rather than drawing a diff stitched out of
// two states.

import { useCallback, useEffect, useState } from "preact/hooks";

async function ask(path, params) {
    const query = new URLSearchParams(params);
    const r = await fetch(`${path}?${query}`);
    if (!r.ok) {
        const text = (await r.text()).trim();
        const err = new Error(text || `response ${r.status}`);
        err.status = r.status;
        throw err;
    }
    return r.json();
}

export const changesOf = (cwd, base) => ask("/api/repo/changes", clean({ cwd, base }));
export const treeOf = (cwd, path) => ask("/api/repo/tree", clean({ cwd, path }));
export const findOf = (cwd, q) => ask("/api/repo/find", clean({ cwd, q }));
export const fileOf = (cwd, path, rev, first, lines) =>
    ask("/api/repo/file", clean({ cwd, path, rev, first, lines }));
export const diffOf = (cwd, path, base, rev, layer) =>
    ask("/api/repo/diff", clean({ cwd, path, base, rev, layer }));
export const refsOf = (cwd) => ask("/api/repo/refs", { cwd });
export const commitOf = (cwd, hash) => ask("/api/repo/commit", { cwd, hash });
export const blameOf = (cwd, path, rev, first, lines) =>
    ask("/api/repo/blame", clean({ cwd, path, rev, first, lines }));

function clean(params) {
    const out = {};
    for (const [k, v] of Object.entries(params)) {
        if (v !== undefined && v !== null && v !== "") out[k] = v;
    }
    return out;
}

// useAsk keeps one request and what came back of it. The state is a kind rather
// than three booleans: a screen drawing "loading" and "failed" at once is a
// screen that read two of them.
export function useAsk(run, deps, ready = true) {
    const [state, setState] = useState({ kind: ready ? "loading" : "idle" });
    const reload = useCallback(() => setState({ kind: "loading" }), []);

    useEffect(() => {
        if (!ready) return undefined;
        let alive = true;
        setState((was) => (was.kind === "ready" ? was : { kind: "loading" }));
        run()
            .then((data) => alive && setState({ kind: "ready", data }))
            .catch((e) => alive && setState({ kind: "failed", error: String(e.message || e) }));
        return () => {
            alive = false;
        };
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [...deps, ready]);

    return [state, reload];
}
