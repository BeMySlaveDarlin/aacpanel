// Which session's question is on screen right now.
import { useEffect } from "preact/hooks";

let seen = "";

export function viewing() {
    return seen;
}

export function useViewing(name) {
    useEffect(() => {
        seen = name || "";
        return () => {
            if (seen === name) seen = "";
        };
    }, [name]);
}
