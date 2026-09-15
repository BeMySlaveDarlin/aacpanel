// The feed window: the first read, paging upwards and the stream of new items.

import { useEffect, useLayoutEffect, useRef, useState } from "preact/hooks";

import { html } from "../../html.js";
import { Icon } from "../../ui/icons.js";
import { merge } from "./feed.js";
import { idParam } from "./api.js";

export const PAGE = 40;

const FRESH_RETRY_MS = 3000;

const CLOSED = 2;

// The feed counts as being at its end within this many pixels of the bottom: a
// reader who nudged the last message a line up is still carried along by the
// next one, and the jump button does not appear for that nudge either.
const END_SLACK = 120;

// nearEnd reports whether the box is scrolled to its end, with the slack. It
// is the one rule behind both the stickiness of the feed and the jump button:
// two rules here would leave a feed that follows new messages while the button
// says it does not, or the other way round.
export function nearEnd(box) {
    return box.scrollHeight - box.scrollTop - box.clientHeight < END_SLACK;
}

// left keeps where every feed was last seen, for as long as the page lives. The box
// of the feed is thrown away whenever the screen shows the terminal or an agent
// instead, and built anew on the way back: a position kept inside it would die with
// it, and the reader would come back to the top of the conversation every time.
const left = new Map();

// feedKey names a feed: the conversation, and for an agent its own feed inside it.
export function feedKey(name, id) {
    return `${name}\u0000${id || ""}`;
}

// keepAt writes down where the box stands: at the end of the feed, or at this offset.
// Following the end is kept as an answer of its own, not as the offset it happened to
// have: messages arrive while the feed is away, and the end moves with them.
export function keepAt(key, box) {
    if (!box) return;
    left.set(key, { end: nearEnd(box), top: box.scrollTop });
}

// leftAt returns where the feed was left, or null for a feed never seen scrolled.
export function leftAt(key) {
    return left.get(key) || null;
}

// wakeNeeded reports whether the feed stream should be reopened.
export function wakeNeeded(visibility, readyState) {
    if (visibility !== "visible") return false;
    return readyState == null || readyState === CLOSED;
}

// JumpToEnd renders the button that takes a scrolled-up feed to its end. It
// lives inside the feed as its last child: the feed's own bottom edge is the
// one place that stays above the composer, the chips and whatever else stands
// under the feed, on every screen and with nothing under it at all.
export function JumpToEnd({ onJump }) {
    return html`
        <button class="feedjump" type="button" aria-label="to the end of the conversation"
                onClick=${onJump}>${Icon.chevron()}</button>
    `;
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
    const seenRef = useRef(null);
    const putRef = useRef(null);
    const watchRef = useRef(null);
    const [atEnd, setAtEnd] = useState(true);

    const base = `session=${encodeURIComponent(name)}${idParam(id)}`;

    // The key of the feed is read out of a ref: an observer or a listener made for one
    // box outlives the render that made it, and would go on writing the position of a
    // feed the reader has already left.
    const keyRef = useRef("");
    keyRef.current = feedKey(name, id);

    // settle reads the position of the box once and tells both sides of it, and the
    // feed is written down where it stands — every scroll of the box comes through
    // here, the reader's own and the ones the feed makes for itself.
    const settle = (box) => {
        const near = nearEnd(box);
        stickRef.current = near;
        setAtEnd(near);
        keepAt(keyRef.current, box);
    };

    // put returns a box to where the feed was left: to the end if the feed was
    // following new messages there — the end as it is now, not as it was on leaving —
    // and to the same offset if it was scrolled up. A feed nobody has scrolled counts
    // as being at its end, which is where a feed opens. A box with nothing in it yet
    // cannot be scrolled anywhere, and the arriving items try again.
    const put = (box) => {
        if (box.scrollHeight <= box.clientHeight) return;
        const at = leftAt(keyRef.current);
        putRef.current = box;
        box.scrollTop = !at || at.end ? box.scrollHeight : at.top;
        settle(box);
    };

    useEffect(() => {
        const at = leftAt(feedKey(name, id));
        stickRef.current = !at || at.end;
        putRef.current = null;
        // A feed left scrolled up gets its button back from the reading of the box it
        // is put into, not before: until then there is nothing to jump away from.
        setAtEnd(true);
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

    // The box of the feed comes and goes under a window that stays: the screen builds
    // it anew every time it returns from the terminal or from an agent. A new box
    // starts at the top of the conversation, and an observer left on the old one
    // watches a node nobody can see — so every render looks for the box, and a box
    // that is new is watched and put back where the reader left the feed.
    useLayoutEffect(() => {
        const box = feedRef.current;
        if (box !== seenRef.current) {
            seenRef.current = box;
            if (watchRef.current) {
                watchRef.current.disconnect();
                watchRef.current = null;
            }
            // A box that grows while the feed is scrolled up may now reach the end
            // by itself; without a scroll event nobody would notice, and the button
            // would stay for an end already in view.
            if (box && typeof ResizeObserver === "function") {
                const ro = new ResizeObserver(() => {
                    if (stickRef.current) box.scrollTop = box.scrollHeight;
                    else settle(box);
                });
                ro.observe(box);
                watchRef.current = ro;
            }
        }
        if (box && putRef.current !== box) put(box);
    });

    useEffect(() => () => {
        if (watchRef.current) watchRef.current.disconnect();
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

    const onScroll = (event) => settle(event.currentTarget);

    // toEnd takes the feed to its end and makes it follow new messages again.
    const toEnd = () => {
        const box = feedRef.current;
        if (!box) return;
        box.scrollTop = box.scrollHeight;
        settle(box);
    };

    return { state, more, feedRef, topRef, onScroll, atEnd, toEnd };
}
