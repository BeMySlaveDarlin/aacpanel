import { useEffect, useRef, useState } from "preact/hooks";

const ANSI = /\u001b\[[0-9;?]*[ -\/]*[@-~]/g;

const MAX_LINES = 500;

function clock(at) {
    if (!at) return "";
    const when = new Date(at);
    if (Number.isNaN(when.getTime())) return "";
    return when.toLocaleTimeString([], {
        hourCycle: "h23", hour: "2-digit", minute: "2-digit", second: "2-digit",
    });
}

export function useLogs(id, tail = 200) {
    const [lines, setLines] = useState([]);
    const [state, setState] = useState("idle");
    const [error, setError] = useState(null);
    const counter = useRef(0);

    useEffect(() => {
        if (!id) {
            setLines([]);
            setState("idle");
            return undefined;
        }

        setLines([]);
        setError(null);
        setState("connecting");
        counter.current = 0;

        const source = new EventSource(`/api/logs?id=${encodeURIComponent(id)}&tail=${tail}`);

        source.addEventListener("log", (event) => {
            const { stream, line, at } = JSON.parse(event.data);
            counter.current += 1;
            const entry = { key: counter.current, stream, time: clock(at), line: line.replace(ANSI, "") };
            setLines((prev) => (prev.length >= MAX_LINES ? [...prev.slice(1), entry] : [...prev, entry]));
            setState("live");
        });

        source.addEventListener("failed", (event) => {
            setError(JSON.parse(event.data).error);
            setState("failed");
            source.close();
        });

        source.addEventListener("eof", () => {
            setState("ended");
            source.close();
        });

        source.onerror = () => {
            if (source.readyState === EventSource.CLOSED) {
                setState("failed");
                setError("the connection was closed");
            } else {
                setState("reconnecting");
            }
        };

        return () => source.close();
    }, [id, tail]);

    return { lines, state, error };
}
