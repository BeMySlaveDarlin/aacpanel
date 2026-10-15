// The graph of the live sessions at the top of the home screen: every contour
// a ring round the host, every session a mark on its ring at the angle of its
// last request, the sessions it opened its moons. A click opens the session as
// its row in the column opens it, a right click opens the same menu.
//
// It is handled as a map is: a drag moves the picture, the wheel scales it
// round the pointer, and a drag with Shift or with the middle button turns
// the scene. The wheel over the graph is the graph's, so the page does not
// scroll under it there; Reset view brings back the turn, the place and the
// scale it opened with.
//
// The scene is drawn when what it shows changes — the snapshot, the theme,
// the size, a turn, a move — and not on every frame: a home screen left open
// all day should cost nothing while nothing moves. Only the turning of its
// own, switched on by the person, draws frame after frame, and it stops while
// the page is hidden. Where the system asks for less motion it starts off.
import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "preact/hooks";

import { html } from "../../html.js";
import { plural } from "../../format.js";
import { shownName } from "../../agent.js";
import { stateOf, useAnswers } from "../../screens/sessions/blocks.js";
import { contourOfLive } from "../../screens/sessions/map.js";
import { pageNames } from "../../screens/sessions/pages.js";
import { useMenu } from "../../ui/menu.js";
import { liveMenuOf } from "../sessionmenu.js";
import { HOME, PITCH, camera, idleOf, layout, lensAt, lensFor, zoomAt } from "./space.js";
import { HALO, drawKey, drawScene, overlap } from "./scene.js";

// PAD keeps the scene off the edges of its canvas: [top, right, bottom, left].
export const PAD = [30, 40, 26, 40];

// The kinds of background work a session reports, in the order its beads go.
const WORK = ["agents", "tasks", "observers", "alarms"];

// How fast a drag turns and tips the scene, in radians a pixel, and how fast
// it turns by itself, in radians a millisecond: a turn in a minute and a half.
const DRAG_YAW = 0.006;
const DRAG_PITCH = 0.004;
const SPIN = 0.00007;

// A press that travels further than this is a drag, not a click.
const SLOP = 3;

// How much a pixel of the wheel scales the picture: a notch of a mouse wheel,
// a hundred pixels, is about a seventh.
const WHEEL = 0.0015;

// How long the wheel rests before the zoom counts as done. While a move or a
// zoom goes on no name changes its side; once it is done, a name the move
// has brought onto another moves off it, once — as an answer to the gesture
// rather than a jump some seconds later, with the next snapshot.
const WHEEL_DONE = 250;

// The scale of the type against the root size of the interface: the names a
// step under the body text, read from across the desk without leaning in; the
// scale and the host about twice as large and thin. The marks are drawn for a
// root of UNIT_PX — the size the interface opens on — and grow with it.
const TAG_REM = 0.86;
const BIG_REM = 1.6;
const UNIT_PX = 18;

const REDUCE = "(prefers-reduced-motion: reduce)";

// short says a span of minutes in the words of the frame, rounded to the
// unit it is said in.
function short(min) {
    if (min < 1) return "just now";
    if (Math.round(min) < 60) return `${Math.round(min)} min`;
    if (Math.round(min / 60) < 24) return `${Math.round(min / 60)} h`;
    return `${Math.round(min / 1440)} d`;
}

// graphOf lays out the live sessions of a snapshot: the rings in the order of
// the map, the contours the map does not know after them, and every session
// with what its mark says — its name, its context, its background work, and
// its state in the words of its row: stateOf, so the graph waits on a
// question exactly when the row does.
export function graphOf(snapshot) {
    const sessions = (snapshot && snapshot.sessions) || [];
    const map = (snapshot && snapshot.profileMap) || [];
    const now = snapshot && snapshot.at ? snapshot.at * 1000 : Date.now();
    const contourOf = contourOfLive(map, sessions);
    const rings = new Map(pageNames(map, snapshot && snapshot.limits).map((name) => [name, []]));
    const facts = new Map();
    for (const s of sessions) {
        const contour = contourOf(s);
        if (!rings.has(contour)) rings.set(contour, []);
        rings.get(contour).push(s.session);
        facts.set(s.session, { parent: (s.parent && s.parent.session) || null, idle: idleOf(s, now) });
    }
    const placed = layout([...rings].map(([name, keys]) => ({ name, keys })), facts);
    const nodes = sessions.map((s) => {
        const state = stateOf(s);
        const p = placed.nodes.get(s.session);
        return {
            s,
            p,
            name: shownName(s),
            contour: contourOf(s),
            tone: state.tone,
            state,
            pct: s.pct || 0,
            idle: p.idle,
            work: WORK.flatMap((kind) => Array((s.work && s.work[kind]) || 0).fill(kind)),
            note: `${state.text}, ${short(p.idle)}`,
        };
    });
    return { rings: placed.rings, nodes };
}

// glowOf is the session the one light stands behind: the one that has waited
// for the person longest, or else the freshest at work.
function glowOf(nodes) {
    const waiting = nodes.filter((n) => n.tone === "wait").sort((a, b) => b.idle - a.idle);
    if (waiting.length) return waiting[0];
    const busy = nodes.filter((n) => n.tone === "busy").sort((a, b) => a.idle - b.idle);
    return busy[0] || null;
}

// countsOf is the line of the header: how many live, where, and how many need
// nothing and how many need the person.
function countsOf(graph) {
    const live = graph.nodes.length;
    const contours = graph.rings.length;
    const busy = graph.nodes.filter((n) => n.tone === "busy").length;
    const wait = graph.nodes.filter((n) => n.tone === "wait").length;
    return { live, contours, busy, wait };
}

// paletteOf reads the colours of the scene from the panel's own tokens, so a
// theme recolours the graph as it does every other screen: the wait is the
// panel's gold, the busy its accent, and a context near the cap takes the top
// step of the context scale.
function paletteOf(el) {
    const css = getComputedStyle(el);
    const v = (name, fallback) => css.getPropertyValue(name).trim() || fallback;
    return {
        ink: v("--ink", "#eef1ff"),
        wait: v("--wait", "#e3c97a"),
        busy: v("--accent", "#63a8ff"),
        cap: v("--ctx-4", "#b98cff"),
    };
}

// typeOf is the type of the scene at the scale the interface is drawn at.
function typeOf(el) {
    const rem = parseFloat(getComputedStyle(document.documentElement).fontSize) || 16;
    const family = getComputedStyle(el).fontFamily || "Inter, system-ui, sans-serif";
    const tag = Math.round(TAG_REM * rem * 10) / 10;
    const big = Math.round(BIG_REM * rem * 10) / 10;
    const unit = rem / UNIT_PX;
    return {
        unit,
        tag: { font: `${tag}px ${family}`, size: tag, sp: 0.5, halo: HALO * unit },
        big: { font: `200 ${big}px ${family}`, size: big, sp: 1, halo: HALO * unit },
    };
}

// hitAt is the node under a point of the stage, the nearest of those whose
// mark the point is on.
function hitAt(hits, x, y) {
    let best = null;
    let bd = Infinity;
    for (const h of hits) {
        const d = Math.hypot(h.x - x, h.y - y);
        if (d < h.r + 6 && d < bd) {
            bd = d;
            best = h;
        }
    }
    return best;
}

// placeTip finds the corner of the tip near the pointer that covers the
// fewest names and marks, inside the stage.
function placeTip(x, y, tw, th, W, H, taken, hits) {
    const spots = [];
    for (const dy of [18, -th - 14, 64, -th - 60, 110, -th / 2]) {
        for (const dx of [18, -tw - 14, -tw / 2, 60, -tw - 60]) spots.push([x + dx, y + dy]);
    }
    let best = spots[0];
    let cost = Infinity;
    spots.forEach(([sx, sy], i) => {
        const r = { x: sx, y: sy, w: tw, h: th };
        let c = i;
        if (sx < 4 || sy < 4 || sx + tw > W - 4 || sy + th > H - 4) c += 1e6;
        for (const q of taken) c += overlap(r, q) * 4;
        for (const q of hits) c += overlap(r, { x: q.x - q.r, y: q.y - q.r, w: q.r * 2, h: q.r * 2 }) * 4;
        if (c < cost) {
            cost = c;
            best = [sx, sy];
        }
    });
    return { left: Math.max(4, best[0]), top: Math.max(4, best[1]) };
}

function lastAsked(n) {
    if (n.s.noRequests) return "no requests yet";
    return n.idle < 1 ? "asked just now" : `last request ${short(n.idle)} ago`;
}

function Tip({ tip, onPlaced }) {
    const ref = useRef(null);
    useLayoutEffect(() => {
        if (ref.current) onPlaced(ref.current);
    }, [tip]);
    const n = tip.node;
    const parent = n.p.parent ? ` · opened by ${n.p.parent}` : "";
    return html`
        <div class="dkorbtip" ref=${ref} role="tooltip">
            <b>${n.name}</b>
            <span>${`${n.contour} · context ${Math.round(n.pct)}% · ${lastAsked(n)}${parent}`}</span>
            <span class=${n.tone === "wait" ? "dkorbwait" : ""}>
                ${n.state.text}${n.state.work && n.tone !== "wait" ? ` · ${n.state.work}` : ""}
            </span>
            <em>click — open the session · right-click — its menu</em>
        </div>
    `;
}

// Key is the mark the legend names, drawn as the scene draws it: on a canvas
// of its own, again when the theme or the scale changes it.
function Key({ kind, theme }) {
    const ref = useRef(null);
    useEffect(() => {
        const el = ref.current;
        const draw = () => {
            const size = el.getBoundingClientRect().width;
            const dpr = window.devicePixelRatio || 1;
            el.width = Math.round(size * dpr);
            el.height = Math.round(size * dpr);
            const ctx = el.getContext("2d");
            ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
            drawKey(ctx, kind, size, paletteOf(el));
        };
        const sized = new ResizeObserver(draw);
        sized.observe(el);
        // A new theme is drawn on the next frame, once the page has it.
        const frame = requestAnimationFrame(draw);
        return () => {
            sized.disconnect();
            cancelAnimationFrame(frame);
        };
    }, [kind, theme]);
    return html`<canvas class="dkorbkey" ref=${ref} aria-hidden="true"></canvas>`;
}

const KEYS = [
    ["busy", "busy"],
    ["idle", "idle"],
    ["wait", "waiting for an answer"],
    ["angle", "angle — since the last request"],
    ["context", "context, tick — the cap"],
    ["work", "background work"],
    ["moons", "child sessions"],
];

// Legend names what the marks mean.
function Legend({ theme }) {
    return html`
        <div class="dkorbleg">
            ${KEYS.map(([kind, label]) => html`
                <span key=${kind} class=${kind === "wait" ? "dkorbwait" : ""}><${Key} kind=${kind} theme=${theme} />${label}</span>
            `)}
            <span class="dkorbhelp">drag to move · Shift-drag to turn · wheel to zoom · click opens · right-click — menu</span>
        </div>
    `;
}

export function Orbits({ snapshot, exec, wait, theme, onSession, onTerm }) {
    // A question answered from this page stops the session waiting here as it
    // does in its row, before the next snapshot says so.
    useAnswers();
    const menu = useMenu();
    const tones = ((snapshot && snapshot.sessions) || []).map((s) => stateOf(s).tone).join();
    const graph = useMemo(() => graphOf(snapshot), [snapshot, tones]);
    const counts = countsOf(graph);

    const stage = useRef(null);
    const canvas = useRef(null);
    // What the drawing works from lives in a ref: a drag or a frame of the
    // turn redraws without a render of the component.
    const live = useRef({
        view: { ...HOME }, zoom: 1, pan: { x: 0, y: 0 }, size: null, lens: null,
        hits: [], taken: [], sides: new Map(), settle: true, drag: null, frame: 0, spin: 0, zoomed: 0,
    });
    const [tip, setTip] = useState(null);
    const [auto, setAuto] = useState(() => !window.matchMedia(REDUCE).matches);

    // paint draws the scene as it stands. settle says the scene turned or
    // changed, so a name its side has come to cover may move; a frame of a
    // move or a zoom alone keeps every name where it was beside its mark.
    const paint = (settle = true) => {
        const st = live.current;
        st.frame = 0;
        const turned = settle || st.settle;
        st.settle = false;
        const el = canvas.current;
        if (!el || !st.size || graph.rings.length === 0) return;
        const { W, H, dpr } = st.size;
        // The lens is fitted to the rings and the box: a new contour or a new
        // size fits it again.
        if (!st.lens) st.lens = lensFor(graph.rings, W, H, PAD);
        const ctx = el.getContext("2d");
        ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
        ctx.clearRect(0, 0, W, H);
        const pr = camera(st.view, W, H, lensAt(st.lens, st.zoom, st.pan));
        const drawn = drawScene(ctx, W, H, pr, {
            rings: graph.rings,
            nodes: graph.nodes,
            glow: glowOf(graph.nodes),
            host: (snapshot && snapshot.hostName) || "host",
            pal: paletteOf(stage.current),
            type: typeOf(stage.current),
            sides: st.sides,
            settle: turned,
        });
        st.hits = drawn.hits;
        st.taken = drawn.taken;
    };
    const paintRef = useRef(paint);
    paintRef.current = paint;
    // A frame asked for draws what stands when it comes, not what stood when
    // it was asked: a new snapshot between the two is drawn, not lost.
    const request = (settle = true) => {
        const st = live.current;
        st.settle = st.settle || settle;
        if (!st.frame) st.frame = requestAnimationFrame(() => paintRef.current(false));
    };
    const requestRef = useRef(request);
    requestRef.current = request;

    // The canvas follows its box and the density of the screen.
    useEffect(() => {
        const el = stage.current;
        const sized = () => {
            const box = el.getBoundingClientRect();
            const dpr = window.devicePixelRatio || 1;
            const W = Math.max(10, box.width);
            const H = Math.max(10, box.height);
            canvas.current.width = Math.round(W * dpr);
            canvas.current.height = Math.round(H * dpr);
            live.current.size = { W, H, dpr };
            live.current.lens = null;
            paintRef.current();
        };
        const watch = new ResizeObserver(sized);
        watch.observe(el);
        return () => watch.disconnect();
    }, []);

    // On the next frame rather than now: the theme reaches the page in an
    // effect of the application, which runs after this one, and its colours
    // are read off the page.
    useEffect(() => {
        live.current.lens = null;
        request();
    }, [graph, theme]);

    // The thin face of the scale comes with the first frame that asks for it;
    // the frame drawn before it came is drawn again.
    useEffect(() => {
        let alive = true;
        const type = typeOf(stage.current);
        document.fonts.load(type.big.font).then(() => alive && paintRef.current(), () => {});
        return () => { alive = false; };
    }, []);

    // The turn of its own: a frame at a time while it is on and the page is
    // seen.
    useEffect(() => {
        if (!auto) return undefined;
        const st = live.current;
        let last = 0;
        const step = (t) => {
            const dt = last ? Math.min(64, t - last) : 16;
            last = t;
            if (!st.drag) st.view = { ...st.view, yaw: st.view.yaw + dt * SPIN };
            paintRef.current();
            st.spin = requestAnimationFrame(step);
        };
        const run = () => {
            cancelAnimationFrame(st.spin);
            st.spin = 0;
            last = 0;
            if (!document.hidden) st.spin = requestAnimationFrame(step);
        };
        run();
        document.addEventListener("visibilitychange", run);
        return () => {
            document.removeEventListener("visibilitychange", run);
            cancelAnimationFrame(st.spin);
            st.spin = 0;
        };
    }, [auto]);

    useEffect(() => () => cancelAnimationFrame(live.current.frame), []);

    // The wheel scales the picture round the pointer. It is listened to by
    // hand: a wheel the page may not cancel would scroll the page under the
    // graph with every step of the zoom.
    useEffect(() => {
        const el = stage.current;
        const wheel = (e) => {
            const st = live.current;
            e.preventDefault();
            if (!st.size || !st.lens) return;
            const box = el.getBoundingClientRect();
            const px = e.deltaMode === 1 ? 16 : e.deltaMode === 2 ? st.size.H : 1;
            const m = { x: e.clientX - box.left, y: e.clientY - box.top };
            const c = { x: st.size.W / 2 + st.lens.ox, y: st.size.H / 2 + st.lens.oy };
            const next = zoomAt(st.zoom, st.pan, Math.exp(-e.deltaY * px * WHEEL), m, c);
            if (next.zoom === st.zoom) return;
            st.zoom = next.zoom;
            st.pan = next.pan;
            hideRef.current();
            requestRef.current(false);
            clearTimeout(st.zoomed);
            st.zoomed = setTimeout(() => requestRef.current(true), WHEEL_DONE);
        };
        el.addEventListener("wheel", wheel, { passive: false });
        return () => {
            el.removeEventListener("wheel", wheel);
            clearTimeout(live.current.zoomed);
        };
    }, []);

    const point = (e) => {
        const box = stage.current.getBoundingClientRect();
        return { x: e.clientX - box.left, y: e.clientY - box.top };
    };
    const show = (h, at) => setTip(h ? { node: h.node, x: at.x, y: at.y } : null);
    const hideRef = useRef(null);
    hideRef.current = () => { if (tip) setTip(null); };

    // A drag with the left button moves the picture; with Shift, or with the
    // middle button, it turns the scene. The middle button's own scrolling of
    // the page is turned down on the graph.
    const down = (e) => {
        if (e.button !== 0 && e.button !== 1) return;
        if (e.button === 1) e.preventDefault();
        const st = live.current;
        st.drag = {
            x: e.clientX, y: e.clientY, button: e.button, turn: e.button === 1 || e.shiftKey,
            view: { ...st.view }, pan: { ...st.pan }, moved: false,
        };
        try {
            stage.current.setPointerCapture(e.pointerId);
        } catch {
            // A pointer the page made up has nothing to capture.
        }
    };
    const move = (e) => {
        const st = live.current;
        const at = point(e);
        if (st.drag) {
            const dx = e.clientX - st.drag.x;
            const dy = e.clientY - st.drag.y;
            if (!st.drag.moved && Math.abs(dx) + Math.abs(dy) <= SLOP) return;
            st.drag.moved = true;
            stage.current.classList.add("drag");
            if (st.drag.turn) {
                const pitch = Math.max(PITCH[0], Math.min(PITCH[1], st.drag.view.pitch + dy * DRAG_PITCH));
                st.view = { yaw: st.drag.view.yaw + dx * DRAG_YAW, pitch };
            } else {
                st.pan = { x: st.drag.pan.x + dx, y: st.drag.pan.y + dy };
            }
            if (tip) setTip(null);
            request(st.drag.turn);
            return;
        }
        const h = hitAt(st.hits, at.x, at.y);
        if ((h && h.node) !== (tip && tip.node)) show(h, at);
    };
    const up = (e) => {
        const st = live.current;
        const drag = st.drag;
        st.drag = null;
        stage.current.classList.remove("drag");
        if (drag && drag.moved && !drag.turn) request(true);
        if (!drag || drag.moved || drag.button !== 0) return;
        const at = point(e);
        const h = hitAt(st.hits, at.x, at.y);
        if (h) onSession({ name: h.node.s.session, id: h.node.s.sessionId });
    };
    const context = (e) => {
        if (!menu) return;
        const at = point(e);
        const h = hitAt(live.current.hits, at.x, at.y);
        if (!h) return;
        const s = h.node.s;
        const open = () => onSession({ name: s.session, id: s.sessionId });
        setTip(null);
        menu(...liveMenuOf(s, { exec, snapshot, wait, onOpen: open, onTerm }))(e, stage.current);
    };
    const reset = () => {
        const st = live.current;
        st.view = { ...HOME };
        st.zoom = 1;
        st.pan = { x: 0, y: 0 };
        request();
    };
    const placed = (el) => {
        if (!tip) return;
        const st = live.current;
        const spot = placeTip(tip.x, tip.y, el.offsetWidth, el.offsetHeight, st.size.W, st.size.H, st.taken, st.hits);
        el.style.left = `${spot.left}px`;
        el.style.top = `${spot.top}px`;
    };

    const said = `${counts.live} live in ${counts.contours} ${plural(counts.contours, "contour", "contours")}`;
    return html`
        <section class="dkwidget dkorbits">
            <header class="dkorbhead">
                <span class="dkorbtitle">Sessions</span>
                <span class="dkorbnote">
                    ${said}
                    ${counts.busy > 0 && html` · <b class="dkorbbusy">${counts.busy} busy</b>`}
                    ${counts.wait > 0 && html` · <b class="dkorbwait">${counts.wait} waiting for an answer</b>`}
                </span>
                <span class="dkorbacts">
                    <button class="dkorbbtn" type="button" aria-pressed=${auto ? "true" : "false"} onClick=${() => setAuto(!auto)}>
                        <i class="dkorbsw"></i>Auto-rotate
                    </button>
                    <button class="dkorbbtn" type="button" onClick=${reset}>Reset view</button>
                </span>
            </header>
            <div
                class=${`dkorbstage${tip ? " over" : ""}`}
                ref=${stage}
                onPointerDown=${down}
                onPointerMove=${move}
                onPointerUp=${up}
                onPointerCancel=${() => { live.current.drag = null; stage.current.classList.remove("drag"); }}
                onPointerLeave=${() => { if (!live.current.drag) setTip(null); }}
                onContextMenu=${context}
            >
                <canvas ref=${canvas} role="img" aria-label=${`Sessions: ${said}`}></canvas>
                ${snapshot && counts.live === 0 && html`<p class="dkorbempty">There are no live sessions.</p>`}
                ${tip && html`<${Tip} tip=${tip} onPlaced=${placed} />`}
            </div>
            <${Legend} theme=${theme} />
        </section>
    `;
}
