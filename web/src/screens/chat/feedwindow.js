// The feed window: the first read, paging upwards and the stream of new items,
// and a window around one item away from the end of the conversation.

import { useEffect, useLayoutEffect, useRef, useState } from "preact/hooks";

import { html } from "../../html.js";
import { Icon } from "../../ui/icons.js";
import { merge } from "./feed.js";
import { idParam } from "./api.js";

export const PAGE = 40;

// A window around an item asks for this many items on either side of it: one
// page in all, the item in the middle of it.
const AROUND = PAGE / 2;

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
// under the feed, on every screen and with nothing under it at all. Over a
// window away from the end it says so in words: the end it goes back to is not
// below the rows on screen, and a bare arrow reads as one more page down.
export function JumpToEnd({ onJump, away }) {
    return html`
        <button class=${`feedjump${away ? " feedaway" : ""}`} type="button"
                aria-label=${away ? "back to the end of the conversation" : "to the end of the conversation"}
                onClick=${onJump}>${away && html`<span>Back to the end</span>`}${Icon.chevron()}</button>
    `;
}

// useFeedWindow returns the feed and everything needed to show it.
//
// The feed shows one of two lists. The tail is the end of the conversation: the
// first read, the pages above it and whatever the stream brings. A window away
// from the end is a piece around one item, asked for by show(pos) when the item
// is not among the rows on screen; it pages up as the tail does and down past
// its own last row. While the window is on screen the tail goes on taking the
// stream out of sight — the rows the reader is looking at do not move under new
// messages — and toEnd puts the tail back, at its end, with everything that
// arrived in between.
export function useFeedWindow({ name, id, live }) {
    const [state, setState] = useState({ kind: "loading", items: [] });
    const [more, setMore] = useState(false);
    const feedRef = useRef(null);
    const topRef = useRef(null);
    const bottomRef = useRef(null);
    const busyRef = useRef(false);
    const lastRef = useRef(null);
    const firstRef = useRef(null);
    const stickRef = useRef(true);
    const seenRef = useRef(null);
    const putRef = useRef(null);
    const watchRef = useRef(null);
    const [atEnd, setAtEnd] = useState(true);
    // The window away from the end, or null while the tail is on screen. The
    // ref is what callbacks made in an earlier render read; trip tells one
    // window from the next, so a page asked for one is not sewn onto another.
    const [away, setAway] = useState(null);
    const awayRef = useRef(null);
    const tripRef = useRef(0);
    const tailRef = useRef(state.items);
    tailRef.current = state.items;
    // drawnRef holds what show() waits on: called once the window it asked for
    // is on screen. homeRef says the window was left for the end of the tail.
    const drawnRef = useRef(null);
    const homeRef = useRef(false);

    const base = `session=${encodeURIComponent(name)}${idParam(id)}`;
    const baseRef = useRef(base);
    baseRef.current = base;

    // The key of the feed is read out of a ref: an observer or a listener made for one
    // box outlives the render that made it, and would go on writing the position of a
    // feed the reader has already left.
    const keyRef = useRef("");
    keyRef.current = feedKey(name, id);

    // settle reads the position of the box once and tells both sides of it, and the
    // feed is written down where it stands — every scroll of the box comes through
    // here, the reader's own and the ones the feed makes for itself. The bottom of a
    // window away from the end is not the end: there the feed neither follows new
    // messages nor hides the way back.
    const settle = (box) => {
        const near = !awayRef.current && nearEnd(box);
        stickRef.current = near;
        setAtEnd(near);
        keepAt(keyRef.current, box);
    };

    // leave puts a window on screen in place of the tail, or the tail back in
    // place of a window.
    const leave = (win) => {
        awayRef.current = win;
        stickRef.current = !win;
        setAway(win);
        if (win) setAtEnd(false);
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
        awayRef.current = null;
        homeRef.current = false;
        setAway(null);
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

    // A window put on screen: show() goes on once its rows are drawn. The tail
    // put back by toEnd: the box goes to its end — the end as it is now, with
    // what the stream brought while the window was on screen.
    useLayoutEffect(() => {
        const drawn = drawnRef.current;
        if (away && drawn) {
            drawnRef.current = null;
            drawn(true);
        }
        if (away || !homeRef.current) return;
        homeRef.current = false;
        const box = feedRef.current;
        if (!box) return;
        box.scrollTop = box.scrollHeight;
        settle(box);
    }, [away]);

    // ask reads a piece of the conversation: what comes before a position or after it.
    const ask = async (where, limit) => {
        const r = await fetch(`/api/chat?${baseRef.current}&limit=${limit}&${where}`);
        if (!r.ok) throw new Error((await r.text()).trim() || `response ${r.status}`);
        return r.json();
    };

    // trip names the list on screen: the tail, or one window away from the end.
    const tripOf = () => (awayRef.current ? awayRef.current.trip : 0);

    const loadUp = async () => {
        const box = feedRef.current;
        const win = awayRef.current;
        const before = win ? win.first : firstRef.current;
        if (before == null || !box || busyRef.current) return;
        busyRef.current = true;
        const trip = tripOf();
        const wasHeight = box.scrollHeight;
        const wasTop = box.scrollTop;
        try {
            const data = await ask(`before=${before}`, PAGE);
            if (win) {
                if (tripOf() !== trip) return;
                const now = awayRef.current;
                leave({ ...now, items: merge(data.items || [], now.items),
                        first: data.first ?? before, moreBefore: Boolean(data.moreBefore) });
            } else {
                firstRef.current = data.first ?? before;
                setMore(Boolean(data.moreBefore));
                setState((prev) => ({ ...prev, items: merge(data.items || [], prev.items) }));
            }
            // The page grows over the rows the reader is on, and the box keeps them
            // where they were — unless another list took the screen meanwhile.
            requestAnimationFrame(() => {
                if (tripOf() !== trip) return;
                box.scrollTop = wasTop + (box.scrollHeight - wasHeight);
            });
        } catch (e) {
            if (win) {
                if (tripOf() === trip) leave({ ...awayRef.current, moreBefore: false });
            } else {
                setMore(false);
            }
            setState((prev) => ({ ...prev, note: String(e.message || e) }));
        } finally {
            busyRef.current = false;
        }
    };

    // loadDown takes a window away from the end a page further down. A page
    // after a position has no word for "there is more": a full page says there
    // may be, a short one that the end of the conversation is reached.
    const loadDown = async () => {
        const box = feedRef.current;
        const win = awayRef.current;
        if (!win || !win.moreAfter || !box || busyRef.current) return;
        busyRef.current = true;
        try {
            const data = await ask(`after=${win.last}`, PAGE);
            if (tripOf() !== win.trip) return;
            const now = awayRef.current;
            const items = data.items || [];
            leave({ ...now, items: merge(now.items, items), last: data.last ?? now.last,
                    moreAfter: items.filter((item) => item.pos > now.last).length >= PAGE });
        } catch (e) {
            if (tripOf() === win.trip) leave({ ...awayRef.current, moreAfter: false });
            setState((prev) => ({ ...prev, note: String(e.message || e) }));
        } finally {
            busyRef.current = false;
        }
    };

    // show makes the item at a position part of the list on screen, and resolves
    // once it is drawn: at once for an item already there, otherwise after a
    // window around it has been read and has taken the place of the list. The
    // window is two reads side by side — up to the item and past it — since a
    // position names a record, not a count of rows to step back by. Resolves
    // false when the conversation changed under the reads.
    const show = async (pos) => {
        const shown = awayRef.current ? awayRef.current.items : tailRef.current;
        if (shown.some((item) => item.pos === pos)) return true;
        const key = keyRef.current;
        const [up, down] = await Promise.all([ask(`before=${pos + 1}`, AROUND), ask(`after=${pos}`, AROUND)]);
        if (keyRef.current !== key) return false;
        const later = (down.items || []).filter((item) => item.pos > pos).length;
        tripRef.current += 1;
        // A window asked for while another was still on its way takes its place:
        // the one before it will not be drawn.
        if (drawnRef.current) drawnRef.current(false);
        const drawn = new Promise((resolve) => { drawnRef.current = resolve; });
        leave({
            trip: tripRef.current,
            items: merge(up.items || [], down.items || []),
            first: up.first ?? pos,
            last: down.last ?? pos,
            moreBefore: Boolean(up.moreBefore),
            moreAfter: later >= AROUND,
        });
        return drawn;
    };

    const shownItems = away ? away.items : state.items;
    const upMore = away ? away.moreBefore : more;
    const downMore = Boolean(away && away.moreAfter);

    useEffect(() => {
        if (!upMore || state.kind !== "ready") return undefined;
        const box = feedRef.current;
        const sentinel = topRef.current;
        if (!box || !sentinel || typeof IntersectionObserver !== "function") return undefined;

        const io = new IntersectionObserver((entries) => {
            if (entries.some((e) => e.isIntersecting)) loadUp();
        }, { root: box, rootMargin: "300px 0px 0px 0px" });
        io.observe(sentinel);
        return () => io.disconnect();
    }, [upMore, state.kind, shownItems.length, away]);

    useEffect(() => {
        if (!downMore || state.kind !== "ready") return undefined;
        const box = feedRef.current;
        const sentinel = bottomRef.current;
        if (!box || !sentinel || typeof IntersectionObserver !== "function") return undefined;

        const io = new IntersectionObserver((entries) => {
            if (entries.some((e) => e.isIntersecting)) loadDown();
        }, { root: box, rootMargin: "0px 0px 300px 0px" });
        io.observe(sentinel);
        return () => io.disconnect();
    }, [downMore, state.kind, shownItems.length, away]);

    const onScroll = (event) => settle(event.currentTarget);

    // toEnd takes the feed to its end and makes it follow new messages again.
    // From a window away from the end that is the tail put back, and the box
    // goes to its end once the tail is drawn.
    const toEnd = () => {
        const box = feedRef.current;
        if (!box) return;
        if (awayRef.current) {
            homeRef.current = true;
            leave(null);
            return;
        }
        box.scrollTop = box.scrollHeight;
        settle(box);
    };

    return {
        state, shown: shownItems, away: Boolean(away), more: upMore, later: downMore,
        feedRef, topRef, bottomRef, onScroll, atEnd, toEnd, show,
    };
}
