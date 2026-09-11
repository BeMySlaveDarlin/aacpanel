// The feed window: the first read, paging upwards and the stream of new items.

import { useEffect, useRef, useState } from "preact/hooks";

import { merge } from "./feed.js";
import { idParam } from "./api.js";

export const PAGE = 40;

const FRESH_RETRY_MS = 3000;

const CLOSED = 2;

// wakeNeeded reports whether the feed stream should be reopened.
export function wakeNeeded(visibility, readyState) {
    if (visibility !== "visible") return false;
    return readyState == null || readyState === CLOSED;
}

// useFeedWindow returns the feed and everything needed to show it.
export function useFeedWindow({ name, id, live }) {
    const [state, setState] = useState({ kind: "loading", items: [] });
    const [more, setMore] = useState(false);
    const feedRef = useRef(null);
    const topRef = useRef(null);
    const busyRef = useRef(false);
    const lastRef = useRef(null);
    const firstRef = useRef(null);
    const stickRef = useRef(true);

    const base = `session=${encodeURIComponent(name)}${idParam(id)}`;

    useEffect(() => {
        stickRef.current = true;
    }, [name, id]);

    useEffect(() => {
        let alive = true;
        let waiting = null;
        const running = Boolean(live);

        const load = () => fetch(`/api/chat?${base}&limit=${PAGE}`)
            .then(async (r) => {
                if (!r.ok) {
                    const err = new Error((await r.text()).trim() || `response ${r.status}`);
                    err.status = r.status;
                    throw err;
                }
                return r.json();
            })
            .then((data) => {
                if (!alive) return;
                if (waiting) {
                    clearInterval(waiting);
                    waiting = null;
                }
                firstRef.current = data.first;
                lastRef.current = data.last;
                setMore(Boolean(data.moreBefore));
                setState({ kind: "ready", items: data.items || [], total: data.total || 0,
                           work: data.state || null });
            })
            .catch((e) => {
                if (!alive) return;
                if (running && e.status === 404) {
                    setState({ kind: "fresh", items: [] });
                    if (!waiting) waiting = setInterval(load, FRESH_RETRY_MS);
                    return;
                }
                setState({ kind: "failed", items: [], error: String(e.message || e) });
            });

        setState({ kind: "loading", items: [] });
        load();
        return () => {
            alive = false;
            if (waiting) clearInterval(waiting);
        };
    }, [name, id]);

    const isLive = Boolean(live);
    useEffect(() => {
        if (!isLive || state.kind !== "ready") return undefined;
        let es = null;
        const open = () => {
            const from = lastRef.current == null ? "" : `&after=${lastRef.current}`;
            es = new EventSource(`/api/chat/stream?${base}${from}`);
            wire(es);
            return es;
        };
        const wire = (es) => {
            es.addEventListener("state", (ev) => {
                try {
                    setState((prev) => ({ ...prev, work: JSON.parse(ev.data) }));
                } catch {
                }
            });
            es.addEventListener("chat", (ev) => {
                let payload;
                try {
                    payload = JSON.parse(ev.data);
                } catch {
                    return;
                }
                const items = payload.items || [];
                if (!items.length) return;
                lastRef.current = items.reduce((max, i) => (i.pos > max ? i.pos : max), lastRef.current ?? 0);
                setState((prev) => ({ ...prev, items: merge(prev.items, items), total: payload.total || prev.total }));
            });
        };

        open();

        const wake = () => {
            if (wakeNeeded(document.visibilityState, es && es.readyState)) open();
        };
        document.addEventListener("visibilitychange", wake);

        return () => {
            document.removeEventListener("visibilitychange", wake);
            if (es) es.close();
        };
    }, [name, id, isLive, state.kind]);

    useEffect(() => {
        const box = feedRef.current;
        if (!box || state.kind !== "ready" || !stickRef.current) return;
        box.scrollTop = box.scrollHeight;
    }, [state.items, state.kind]);

    useEffect(() => {
        const box = feedRef.current;
        if (!box || typeof ResizeObserver !== "function") return undefined;
        const ro = new ResizeObserver(() => {
            if (stickRef.current) box.scrollTop = box.scrollHeight;
        });
        ro.observe(box);
        return () => ro.disconnect();
    }, []);

    const loadUp = async () => {
        const box = feedRef.current;
        const before = firstRef.current;
        if (before == null || !box || busyRef.current) return;
        busyRef.current = true;
        const wasHeight = box.scrollHeight;
        const wasTop = box.scrollTop;
        try {
            const r = await fetch(`/api/chat?${base}&limit=${PAGE}&before=${before}`);
            if (!r.ok) throw new Error((await r.text()).trim() || `response ${r.status}`);
            const data = await r.json();
            firstRef.current = data.first ?? before;
            setMore(Boolean(data.moreBefore));
            setState((prev) => ({ ...prev, items: merge(data.items || [], prev.items) }));
            requestAnimationFrame(() => {
                box.scrollTop = wasTop + (box.scrollHeight - wasHeight);
            });
        } catch (e) {
            setMore(false);
            setState((prev) => ({ ...prev, note: String(e.message || e) }));
        } finally {
            busyRef.current = false;
        }
    };

    useEffect(() => {
        if (!more || state.kind !== "ready") return undefined;
        const box = feedRef.current;
        const sentinel = topRef.current;
        if (!box || !sentinel || typeof IntersectionObserver !== "function") return undefined;

        const io = new IntersectionObserver((entries) => {
            if (entries.some((e) => e.isIntersecting)) loadUp();
        }, { root: box, rootMargin: "300px 0px 0px 0px" });
        io.observe(sentinel);
        return () => io.disconnect();
    }, [more, state.kind, state.items.length]);

    const onScroll = (event) => {
        const box = event.currentTarget;
        stickRef.current = box.scrollHeight - box.scrollTop - box.clientHeight < 120;
    };

    return { state, more, feedRef, topRef, onScroll };
}
