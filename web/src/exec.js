// Executor availability and the live container tree.
import { useEffect, useState } from "preact/hooks";

import { same } from "./ui/pure.js";

const STATUS_MS = 60000;

export function useExec() {
    const [state, setState] = useState({ available: false, reason: "checking the executor" });

    useEffect(() => {
        let alive = true;
        // The executor answers the same minute after minute, and an answer
        // that changes nothing keeps the one on the screens: a new one would
        // draw every row of an open conversation again.
        const put = (next) => setState((was) => (same(was, next) ? was : next));

        const ask = async () => {
            try {
                const response = await fetch("/api/exec", { credentials: "same-origin" });
                if (!alive) return;
                if (!response.ok) {
                    put({ available: false, reason: `the server answered ${response.status}` });
                    return;
                }
                const body = await response.json();
                put({
                    available: Boolean(body.available),
                    reason: body.reason || "",
                    kinds: Array.isArray(body.kinds) && body.kinds.length ? body.kinds : null,
                });
            } catch (err) {
                if (alive) put({ available: false, reason: "the network is unavailable" });
            }
        };

        ask();
        const timer = setInterval(() => {
            if (document.visibilityState === "visible") ask();
        }, STATUS_MS);
        return () => {
            alive = false;
            clearInterval(timer);
        };
    }, []);

    return state;
}

export function knows(exec, kind) {
    if (!exec || !exec.available) return false;
    if (!exec.kinds) return true;
    return exec.kinds.includes(kind);
}

export function whyNot(exec, kind) {
    if (!exec || !exec.available) return (exec && exec.reason) || "the executor is unavailable";
    if (exec.kinds && !exec.kinds.includes(kind)) {
        return `the host does not know the action “${kind}” — update aacpanel-exec on the host ` +
            "or deliver what it is missing (sessions need tmux)";
    }
    return "";
}

export function useTreeStream(onTree, onError) {
    useEffect(() => {
        let source = null;
        let alive = true;
        let retry = null;
        // A frame word for word the one before it is the same tree again —
        // the stream starts over with its last one when it reconnects — and
        // taking it would draw the whole panel again for nothing.
        let last = "";

        const connect = () => {
            if (!alive) return;
            source = new EventSource("/api/stream");

            source.addEventListener("tree", (event) => {
                if (event.data === last) return;
                last = event.data;
                try {
                    onTree(JSON.parse(event.data));
                } catch (err) {
                    console.error("aacpanel: the tree frame did not parse", err);
                }
            });

            source.onerror = () => {
                if (source.readyState !== EventSource.CLOSED) return;
                source.close();
                if (onError) onError();
                if (alive) retry = setTimeout(connect, 5000);
            };
        };

        connect();
        return () => {
            alive = false;
            if (retry) clearTimeout(retry);
            if (source) source.close();
        };
    }, [onTree, onError]);
}
