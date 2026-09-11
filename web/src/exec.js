// Executor availability and the live container tree.
import { useEffect, useState } from "preact/hooks";

const STATUS_MS = 60000;

export function useExec() {
    const [state, setState] = useState({ available: false, reason: "checking the executor" });

    useEffect(() => {
        let alive = true;

        const ask = async () => {
            try {
                const response = await fetch("/api/exec", { credentials: "same-origin" });
                if (!alive) return;
                if (!response.ok) {
                    setState({ available: false, reason: `the server answered ${response.status}` });
                    return;
                }
                const body = await response.json();
                setState({
                    available: Boolean(body.available),
                    reason: body.reason || "",
                    kinds: Array.isArray(body.kinds) && body.kinds.length ? body.kinds : null,
                });
            } catch (err) {
                if (alive) setState({ available: false, reason: "the network is unavailable" });
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

        const connect = () => {
            if (!alive) return;
            source = new EventSource("/api/stream");

            source.addEventListener("tree", (event) => {
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
