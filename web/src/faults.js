// Whether the agent snapshot reaches the history.
import { useEffect, useState } from "preact/hooks";

const FAULTS_MS = 60000;

export function useFaults() {
    const [faults, setFaults] = useState([]);

    useEffect(() => {
        let alive = true;

        const load = () => {
            fetch("/api/faults", { credentials: "same-origin" })
                .then((response) => (response.ok ? response.json() : null))
                .then((data) => {
                    if (!alive || !data) return;
                    setFaults(data.faults || []);
                })
                .catch(() => {});
        };

        load();
        const timer = setInterval(() => {
            if (document.visibilityState === "visible") load();
        }, FAULTS_MS);
        return () => {
            alive = false;
            clearInterval(timer);
        };
    }, []);

    return faults;
}
