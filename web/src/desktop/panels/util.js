// Shared helpers for the right-column panels.
import { useEffect, useState } from "preact/hooks";

export const PAGE = 40;

export function useJSON(url) {
    const [data, setData] = useState(null);
    const [error, setError] = useState("");
    useEffect(() => {
        let alive = true;
        setError("");
        fetch(url, { credentials: "same-origin" })
            .then((r) => (r.ok ? r.json() : Promise.reject(new Error(String(r.status)))))
            .then((d) => alive && setData(d))
            .catch((e) => alive && setError(String(e.message || e)));
        return () => { alive = false; };
    }, [url]);
    return { data, error };
}

export function ago(iso) {
    if (!iso) return "";
    const sec = Math.max(0, Math.round((Date.now() - new Date(iso).getTime()) / 1000));
    if (sec < 60) return `${sec} s`;
    const min = Math.round(sec / 60);
    if (min < 60) return `${min} min`;
    const h = Math.round(min / 60);
    return h < 48 ? `${h} h` : `${Math.round(h / 24)} d`;
}

export function modelShort(model) {
    if (!model) return "";
    return String(model).replace(/^claude-/, "").replace(/-\d{8}$/, "");
}
