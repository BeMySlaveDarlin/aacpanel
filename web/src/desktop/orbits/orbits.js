// The graph of the live sessions at the top of the home screen, a galaxy:
// every contour a system round a star of its own, every project with a live
// session an orbit round the star — the small and quiet inside, the large and
// lively outside, every orbit leaning its own way — every session a planet
// going round on the orbit of its project, the sessions it opened its moons.
// The systems stand in depth, the liveliest in front when the page opens; a
// click on a star, or the wheel with Shift, brings another forward. A click on
// a planet opens the session as its row in the column opens it, a right click
// opens the same menu.
//
// It is handled as a map is: a drag moves the picture, the wheel scales it
// round the pointer, and a drag with Shift or with the middle button turns
// the scene. The wheel over the graph is the graph's, so the page does not
// scroll under it there; Reset view brings back the turn, the place and the
// scale it opened with, and the order of the systems by how alive they are.
//
// With Motion on the planets go round their orbits, frame after frame but no
// more than FPS frames a second, and not at all while the page is hidden or
// the graph is off the screen: a home screen left open all day costs little.
// Motion is on unless the system asks for less motion, and the person's own
// choice is kept. With it off the scene is drawn only when what it shows
// changes — the snapshot, the theme, the size, a turn, a move. A system
// coming forward, or an orbit a snapshot moves in or out, glides there in a
// moment, or stands there at once where the system asks for less motion.
import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "preact/hooks";

import { html } from "../../html.js";
import { plural } from "../../format.js";
import { ownName } from "../../catchup.js";
import { shownName } from "../../agent.js";
import { kinOf } from "../../screens/sessions/kin.js";
import { liveBlocksOf, stateOf, useAnswers } from "../../screens/sessions/blocks.js";
import { contourOfLive } from "../../screens/sessions/map.js";
import { pageNames } from "../../screens/sessions/pages.js";
import { useMenu } from "../../ui/menu.js";
import { liveMenuOf } from "../sessionmenu.js";
import { BACK, FLAT, HOME, PITCH, camera, floorOf, idleOf, layout, lensAt, lensFor, placeOrbit, slotsFor, tiltOf, turnBy, zoomAt } from "./space.js";
import { HALO, drawKey, drawScene, overlap } from "./scene.js";

// PAD keeps the scene off the edges of its canvas: [top, right, bottom, left].
const PAD = [26, 24, 30, 24];

// The hues of the systems, by their place in the map: tokens of the panel no
// state of a session wears — a session at work is the accent, one waiting the
// gold, a context near the cap the top of the context scale.
const HUES = ["--ok", "--accent-2", "--cat-agents", "--cat-tools"];
const HUE_FALLBACK = ["#6b9c83", "#7fe3d4", "#b98cff", "#8f9dff"];

// The kinds of background work a session reports, in the order its beads go.
const WORK = ["agents", "tasks", "observers", "alarms"];

// How fast a drag turns and tips the scene, in radians a pixel.
const DRAG_YAW = 0.006;
const DRAG_PITCH = 0.004;

// The most frames a second the orbits turn at: the eye reads a slow turn as
// smooth at it, and it is half of what a screen would draw.
const FPS = 30;

// Where the choice of Motion is kept, "on" or "off"; with none kept the
// system's wish for less motion decides.
const MOTION_KEY = "aacpanel.desktop.motion";

function motionAtStart() {
    try {
        const kept = localStorage.getItem(MOTION_KEY);
        if (kept === "on" || kept === "off") return kept === "on";
    } catch {
    }
    return !window.matchMedia(REDUCE).matches;
}

// A press that travels further than this is a drag, not a click.
const SLOP = 3;

// How much a pixel of the wheel scales the picture: a notch of a mouse wheel,
// a hundred pixels, is about a seventh.
const WHEEL = 0.0015;

// How far the wheel with Shift travels for the next system to come forward:
// a notch of a mouse wheel; a touchpad gets there over a short stroke.
const NEXT = 40;

// How long the wheel rests before the zoom counts as done. While a move or a
// zoom goes on no name changes its side; once it is done, a name the move
// has brought onto another moves off it, once — as an answer to the gesture
// rather than a jump some seconds later, with the next snapshot.
const WHEEL_DONE = 250;

// How long a system takes to come forward.
const GLIDE = 450;

// The scale of the type against the root size of the interface: the names a
// step under the body text, read from across the desk without leaning in, the
// names of the stars in the same size and heavier. The marks are drawn for a
// root of UNIT_PX — the size the interface opens on — and grow with it.
const TAG_REM = 0.86;
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

// graphOf lays out the live sessions of a snapshot. A contour is a system,
// its sessions as the column of sessions holds them: an orbit for every
// project with a live session, and the sessions of no project on an orbit of
// their own; on an orbit the sessions nobody opened, and every other session
// a moon of the one that opened it. The orbits go out from the star by their
// weight — the sessions of a project with their moons, a busy one pulling
// once more and a waiting one twice — and orbits of equal weight keep the
// order before had them in (from orbitsIn of the graph before), new ones the
// order of the map: a snapshot moves an orbit only when the weights reorder.
// Every session carries what its mark says — its name, its context, its
// background work, and its state in the words of its row: stateOf, so the
// graph waits on a question exactly when the row does.
export function graphOf(snapshot, before = null) {
    const sessions = (snapshot && snapshot.sessions) || [];
    const map = (snapshot && snapshot.profileMap) || [];
    const now = snapshot && snapshot.at ? snapshot.at * 1000 : Date.now();
    const contourOf = contourOfLive(map, sessions);
    const lists = new Map(pageNames(map, snapshot && snapshot.limits).map((name) => [name, []]));
    for (const s of sessions) {
        const contour = contourOf(s);
        if (!lists.has(contour)) lists.set(contour, []);
        lists.get(contour).push(s);
    }
    const facts = new Map();
    const plan = [];
    const index = new Map();
    for (const [name, list] of lists) {
        index.set(name, index.size);
        if (list.length === 0) continue;
        // A parent in another contour is not one here: the child is a planet
        // of its own contour.
        const here = new Set(list.map((s) => s.session));
        for (const s of list) {
            const parent = s.parent && here.has(s.parent.session) ? s.parent.session : null;
            const tone = stateOf(s).tone;
            facts.set(s.session, { parent, idle: idleOf(s, now), pull: tone === "wait" ? 2 : tone === "busy" ? 1 : 0 });
        }
        const { rootOf } = kinOf(list);
        const orbits = [];
        const outside = [];
        for (const block of liveBlocksOf({ profile: map.find((p) => p.profile === name) || null, sessions: list })) {
            const tops = block.live.filter((s) => rootOf(s) === s).map((s) => s.session);
            if (!block.project) {
                outside.push(...tops);
                continue;
            }
            const own = block.project.session || block.project.name;
            orbits.push({
                key: `p${block.project.id}`, name: block.project.name, keys: tops,
                label: !tops.some((t) => ownName(t.toLowerCase(), String(own).toLowerCase())),
            });
        }
        if (outside.length) orbits.push({ key: "outside", name: "outside the map", keys: outside, label: true });
        const was = (before && before.get(name)) || [];
        const rank = (o) => (was.includes(o.key) ? was.indexOf(o.key) : was.length);
        plan.push({ name, orbits: orbits.sort((a, b) => rank(a) - rank(b)) });
    }
    const laid = layout(plan, facts);
    const orbitName = new Map();
    for (const sys of laid.systems) for (const o of sys.orbits) for (const p of o.planets) orbitName.set(p.key, o.name);
    const nameOfOrbit = (p) => {
        let at = p;
        while (at.parent) at = laid.nodes.get(at.parent);
        return orbitName.get(at.key) || "";
    };
    const nodes = sessions.filter((s) => laid.nodes.get(s.session).w).map((s) => {
        const state = stateOf(s);
        const p = laid.nodes.get(s.session);
        return {
            s,
            p,
            name: shownName(s),
            contour: contourOf(s),
            orbit: nameOfOrbit(p),
            tone: state.tone,
            state,
            pct: s.pct || 0,
            idle: p.idle,
            work: WORK.flatMap((kind) => Array((s.work && s.work[kind]) || 0).fill(kind)),
            note: `${state.text}, ${short(p.idle)}`,
        };
    });
    const systems = laid.systems.map((sys) => {
        const mine = nodes.filter((n) => n.p.system === sys.name);
        const busy = mine.filter((n) => n.tone === "busy").length;
        const wait = mine.filter((n) => n.tone === "wait").length;
        const count = mine.length;
        return {
            ...sys,
            index: index.get(sys.name),
            busy,
            wait,
            live: busy + 2 * wait,
            summary: [`${count} ${plural(count, "session", "sessions")}`, busy > 0 && `${busy} busy`, wait > 0 && `${wait} waiting`]
                .filter(Boolean).join(" · "),
        };
    });
    return { systems, nodes };
}

// orbitsIn is the order of the orbits of every system of a graph, from the
// star out: what the next snapshot keeps for orbits of equal weight.
export function orbitsIn(graph) {
    return new Map(graph.systems.map((s) => [s.name, s.orbits.map((o) => o.key)]));
}

// orderOf is the order of the systems in depth when the page opens and on
// Reset view: the liveliest — at work, and waiting twice — in front, a larger
// one before a smaller at a tie, then the order of the map. It is not
// rearranged as the contours quiet down or wake up: only the person moves a
// system forward.
export function orderOf(graph) {
    return graph.systems.slice()
        .sort((a, b) => b.live - a.live || b.R - a.R || a.index - b.index)
        .map((s) => s.name);
}

// placesOf is where every system stands with front in front: the systems go
// round the places of the galaxy in their order, starting from front. The one
// in front is drawn at the size of the largest and square to the view, the
// others at BACK of their own size, leaning their own ways.
export function placesOf(graph, order, front) {
    const R = Math.max(...graph.systems.map((s) => s.R));
    const slots = slotsFor(order.length, R);
    const first = Math.max(0, order.indexOf(front));
    const out = new Map();
    order.forEach((name, i) => {
        const sys = graph.systems.find((s) => s.name === name);
        if (!sys) return;
        const k = (i - first + order.length) % order.length;
        out.set(name, { c: slots[k].c.slice(), tilt: k === 0 ? FLAT : tiltOf(sys.index), k: k === 0 ? R / sys.R : BACK });
    });
    return out;
}

// between is a place a share e of the way from a to b: where a system stands,
// how large it is drawn and how it leans, so the one coming forward grows and
// straightens as it comes and the one going back shrinks as it goes.
export function between(a, b, e) {
    const by = (u, v) => u + (v - u) * e;
    return {
        c: a.c.map((v, i) => by(v, b.c[i])),
        tilt: { tx: by(a.tilt.tx, b.tilt.tx), tz: by(a.tilt.tz, b.tilt.tz) },
        k: by(a.k, b.k),
    };
}

// lensOf fits the galaxy of a graph to a canvas of W×H: the near plan large,
// every place behind inside the canvas.
export function lensOf(graph, W, H) {
    const R = Math.max(...graph.systems.map((s) => s.R));
    return lensFor(slotsFor(graph.systems.length, R), W, H, PAD);
}

// sceneOf is what the drawing takes of a graph with its systems at places:
// the systems with their places and hues, the sessions, the floor.
export function sceneOf(graph, places, hues) {
    const R = Math.max(...graph.systems.map((s) => s.R));
    return {
        systems: graph.systems.map((s) => ({ ...s, place: places.get(s.name), tint: hues[s.index % hues.length] })),
        nodes: graph.nodes,
        floor: floorOf(R),
    };
}

// countsOf is the line of the header: how many live, where, and how many need
// nothing and how many need the person.
function countsOf(graph) {
    const live = graph.nodes.length;
    const contours = graph.systems.length;
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
    const sky = v("--void", "#06070f");
    const [r, g, b] = [0, 2, 4].map((i) => parseInt(sky.replace("#", "").slice(i, i + 2), 16) || 0);
    return {
        ink: v("--ink", "#eef1ff"),
        wait: v("--wait", "#e3c97a"),
        busy: v("--accent", "#63a8ff"),
        cap: v("--ctx-4", "#b98cff"),
        hues: HUES.map((name, i) => v(name, HUE_FALLBACK[i])),
        // The sky behind the scene, and whether it is a light one: there the
        // shadows are soft and no far stars are drawn.
        void: sky,
        sky: 0.2126 * r + 0.7152 * g + 0.0722 * b > 128,
    };
}

// typeOf is the type of the scene at the scale the interface is drawn at.
function typeOf(el) {
    const rem = parseFloat(getComputedStyle(document.documentElement).fontSize) || 16;
    const family = getComputedStyle(el).fontFamily || "Inter, system-ui, sans-serif";
    const tag = Math.round(TAG_REM * rem * 10) / 10;
    const unit = rem / UNIT_PX;
    return {
        unit,
        tag: { font: `${tag}px ${family}`, size: tag, sp: 0.4, halo: HALO * unit },
        star: { font: `600 ${tag}px ${family}`, size: tag, sp: 0.8, halo: HALO * unit },
    };
}

// hitAt is the mark under a point of the stage, the nearest of those the
// point is on.
function hitAt(hits, x, y) {
    let best = null;
    let bd = Infinity;
    for (const h of hits) {
        const d = Math.hypot(h.x - x, h.y - y);
        if (d < h.r + 4 && d < bd) {
            bd = d;
            best = h;
        }
    }
    return best;
}

// behindAt is a system behind the one in front whose orbits a point of the
// stage falls among, the one whose star is nearest where two boxes overlap:
// a click there brings it forward as a click on its star does. The system in
// front has no such box — it is already forward.
function behindAt(areas, front, x, y) {
    let best = null;
    let bd = Infinity;
    for (const a of areas || []) {
        if (a.star.name === front || x < a.x0 || x > a.x1 || y < a.y0 || y > a.y1) continue;
        const d = Math.hypot(a.cx - x, a.cy - y);
        if (d < bd) {
            bd = d;
            best = { star: a.star };
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

function Tip({ tip, front, onPlaced }) {
    const ref = useRef(null);
    useLayoutEffect(() => {
        if (ref.current) onPlaced(ref.current);
    }, [tip]);
    if (tip.star) {
        return html`
            <div class="dkorbtip" ref=${ref} role="tooltip">
                <b>${tip.star.name}</b>
                <span>${tip.star.summary}</span>
                <em>${tip.star.name === front ? "the contour in front" : "click — bring it forward"}</em>
            </div>
        `;
    }
    const n = tip.node;
    const parent = n.p.parent ? ` · opened by ${n.p.parent}` : "";
    return html`
        <div class="dkorbtip" ref=${ref} role="tooltip">
            <b>${n.name}</b>
            <span>${`${n.contour} · ${n.orbit} · context ${Math.round(n.pct)}% · ${lastAsked(n)}${parent}`}</span>
            <span class=${n.tone === "wait" ? "dkorbwait" : ""}>
                ${n.state.text}${n.state.work && n.tone !== "wait" ? ` · ${n.state.work}` : ""}
            </span>
            <em>click — open the session · right-click — its menu</em>
        </div>
    `;
}

// Key is the mark the legend names, drawn as the scene draws it: on a canvas
// of its own, again when the theme or the scale changes it.
const KEY_MAX = 32;
function Key({ kind, theme }) {
    const ref = useRef(null);
    useEffect(() => {
        const el = ref.current;
        const draw = () => {
            // No larger than KEY_MAX: a key its styles do not size takes the
            // size of its pixels, and would grow with every time it is drawn.
            const size = Math.min(el.getBoundingClientRect().width, KEY_MAX);
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
    ["phase", "lit part — context"],
    ["work", "background work"],
    ["moons", "moons — sessions it opened"],
];

// Legend names what the marks mean.
function Legend({ theme }) {
    return html`
        <div class="dkorbleg">
            ${KEYS.map(([kind, label]) => html`
                <span key=${kind} class=${kind === "wait" ? "dkorbwait" : ""}><${Key} kind=${kind} theme=${theme} />${label}</span>
            `)}
            <span class="dkorbhelp">drag to move · Shift-drag to turn · wheel to zoom · Shift-wheel or a star — another contour forward</span>
        </div>
    `;
}

// keepOrder carries the order of the systems over to a new snapshot: those
// still there keep their places, a new one goes to the back, and the one in
// front stays in front while it is there.
function keepOrder(st, graph) {
    const names = graph.systems.map((s) => s.name);
    if (!st.order) {
        st.order = orderOf(graph);
        st.front = st.order[0] || null;
        return;
    }
    st.order = st.order.filter((n) => names.includes(n)).concat(orderOf(graph).filter((n) => !st.order.includes(n)));
    if (!st.order.includes(st.front)) st.front = st.order[0] || null;
}

export function Orbits({ snapshot, exec, wait, theme, onSession, onTerm }) {
    // A question answered from this page stops the session waiting here as it
    // does in its row, before the next snapshot says so.
    useAnswers();
    const menu = useMenu();
    const tones = ((snapshot && snapshot.sessions) || []).map((s) => stateOf(s).tone).join();
    const orbitsBefore = useRef(null);
    const graph = useMemo(() => {
        const next = graphOf(snapshot, orbitsBefore.current);
        orbitsBefore.current = orbitsIn(next);
        return next;
    }, [snapshot, tones]);
    const graphRef = useRef(graph);
    graphRef.current = graph;
    const counts = countsOf(graph);

    const stage = useRef(null);
    const canvas = useRef(null);
    // What the drawing works from lives in a ref: a drag or a frame of the
    // turn redraws without a render of the component.
    const live = useRef({
        view: { ...HOME }, zoom: 1, pan: { x: 0, y: 0 }, size: null, lens: null,
        hits: [], taken: [], sides: new Map(), settle: true, drag: null, frame: 0, motion: 0, seen: false, turns: new Map(), zoomed: 0,
        order: null, front: null, places: new Map(), glide: 0, next: 0, shapes: new Map(), reshape: 0,
    });
    const [tip, setTip] = useState(null);
    const [motion, setMotion] = useState(motionAtStart);
    const toggleMotion = () => {
        const next = !motion;
        setMotion(next);
        try {
            localStorage.setItem(MOTION_KEY, next ? "on" : "off");
        } catch {
        }
    };

    // paint draws the scene as it stands. settle says the scene turned or
    // changed, so a name its side has come to cover may move; a frame of a
    // move, a zoom or a glide alone keeps every name where it was beside its
    // mark.
    const paint = (settle = true) => {
        const st = live.current;
        st.frame = 0;
        const turned = settle || st.settle;
        st.settle = false;
        const el = canvas.current;
        if (!el || !st.size) return;
        const { W, H, dpr } = st.size;
        const ctx = el.getContext("2d");
        ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
        ctx.clearRect(0, 0, W, H);
        if (graph.systems.length === 0) return;
        // A frame asked for before a new snapshot came may be drawn after it.
        keepOrder(st, graph);
        // The lens is fitted to the galaxy and the box: a new system or a new
        // size fits it again.
        if (!st.lens) st.lens = lensOf(graph, W, H);
        // While a system glides forward the others glide with it; one that
        // turns up meanwhile takes its place at once.
        const target = placesOf(graph, st.order, st.front);
        if (!st.glide) st.places = target;
        else for (const [n, p] of target) if (!st.places.has(n)) st.places.set(n, p);
        const pal = paletteOf(stage.current);
        const pr = camera(st.view, W, H, lensAt(st.lens, st.zoom, st.pan));
        const drawn = drawScene(ctx, W, H, pr, {
            ...sceneOf(graph, st.places, pal.hues),
            pal,
            type: typeOf(stage.current),
            front: st.front,
            sides: st.sides,
            settle: turned,
            view: st.view,
            pan: st.pan,
        });
        st.hits = drawn.hits;
        st.areas = drawn.areas;
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

    // bring moves a system to the front, the others round the places after it.
    // It glides there — the one in front going back and growing smaller, the
    // new one coming forward and growing — or stands there at once where less
    // motion is asked for.
    const bring = (name) => {
        const st = live.current;
        if (!st.order || !st.order.includes(name)) return;
        cancelAnimationFrame(st.glide);
        st.glide = 0;
        const from = new Map(st.places);
        st.front = name;
        const to = placesOf(graph, st.order, name);
        if (window.matchMedia(REDUCE).matches || from.size === 0) {
            st.places = to;
            request(true);
            return;
        }
        const t0 = performance.now();
        const step = (t) => {
            const k = Math.min(1, (t - t0) / GLIDE);
            const e = 1 - (1 - k) ** 3;
            const now = new Map();
            for (const [n, p] of to) now.set(n, between(from.get(n) || p, p, e));
            st.places = now;
            if (k < 1) {
                st.glide = requestAnimationFrame(step);
                paintRef.current(false);
            } else {
                st.glide = 0;
                paintRef.current(true);
            }
        };
        st.glide = requestAnimationFrame(step);
    };
    const bringRef = useRef(bring);
    bringRef.current = bring;

    // reshape carries the orbits of a new snapshot from where they were drawn
    // to where it puts them — an orbit that moves out or in, a band that
    // widens — gliding there, or at once where less motion is asked for.
    const reshape = (next) => {
        const st = live.current;
        cancelAnimationFrame(st.reshape);
        st.reshape = 0;
        const all = next.systems.flatMap((sys) => sys.orbits.map((o) => ({ key: `${sys.name}/${o.key}`, o })));
        const kept = () => {
            st.shapes = new Map(all.map(({ key, o }) => [key, { R: o.R, tilt: o.tilt }]));
        };
        const moves = all
            .map(({ key, o }) => ({ o, from: st.shapes.get(key), to: { R: o.R, tilt: o.tilt } }))
            .filter((m) => m.from && (m.from.R !== m.to.R || m.from.tilt.inc !== m.to.tilt.inc || m.from.tilt.node !== m.to.tilt.node));
        if (moves.length === 0 || window.matchMedia(REDUCE).matches) {
            kept();
            return;
        }
        const t0 = performance.now();
        const step = (t) => {
            const k = Math.min(1, (t - t0) / GLIDE);
            const e = 1 - (1 - k) ** 3;
            const by = (u, v) => u + (v - u) * e;
            for (const m of moves) {
                m.o.R = by(m.from.R, m.to.R);
                m.o.tilt = { inc: by(m.from.tilt.inc, m.to.tilt.inc), node: by(m.from.tilt.node, m.to.tilt.node) };
                placeOrbit(m.o);
            }
            kept();
            if (k < 1) {
                st.reshape = requestAnimationFrame(step);
                paintRef.current(false);
            } else {
                st.reshape = 0;
                paintRef.current(true);
            }
        };
        step(t0);
    };

    // The canvas follows its box and the density of the screen.
    useEffect(() => {
        const el = stage.current;
        const sized = () => {
            const box = el.getBoundingClientRect();
            const dpr = window.devicePixelRatio || 1;
            const W = Math.max(10, box.width);
            const H = Math.max(10, box.height);
            // The canvas is held to the box it was measured from: where the
            // styles of the wide screen do not size it, its pixels would be
            // its size, and the box would grow with every frame drawn in it.
            const cv = canvas.current;
            cv.style.display = "block";
            cv.style.width = `${W}px`;
            cv.style.height = `${H}px`;
            cv.width = Math.round(W * dpr);
            cv.height = Math.round(H * dpr);
            live.current.size = { W, H, dpr };
            live.current.lens = null;
            paintRef.current();
        };
        const watch = new ResizeObserver(sized);
        watch.observe(el);
        return () => watch.disconnect();
    }, []);

    // A new snapshot keeps the order of the systems. On the next frame rather
    // than now: the theme reaches the page in an effect of the application,
    // which runs after this one, and its colours are read off the page.
    useEffect(() => {
        keepOrder(live.current, graph);
        live.current.lens = null;
        turnBy(graph, live.current.turns, 0);
        reshape(graph);
        request();
    }, [graph, theme]);

    // The names of the stars are heavier type, drawn again once it comes.
    useEffect(() => {
        let alive = true;
        const type = typeOf(stage.current);
        document.fonts.load(type.star.font).then(() => alive && paintRef.current(), () => {});
        return () => { alive = false; };
    }, []);

    // Motion: the orbits turn a frame at a time, no more than FPS a second,
    // while it is on, the page is seen and the graph is on the screen. A
    // frame skipped or a pause counts no time: the planets go on from where
    // they stood.
    useEffect(() => {
        if (!motion) return undefined;
        const st = live.current;
        let last = 0;
        const step = (t) => {
            st.motion = requestAnimationFrame(step);
            if (last && t - last < 1000 / FPS - 2) return;
            const dt = last ? Math.min(0.1, (t - last) / 1000) : 0;
            last = t;
            turnBy(graphRef.current, st.turns, dt);
            paintRef.current(true);
        };
        const run = () => {
            cancelAnimationFrame(st.motion);
            st.motion = 0;
            last = 0;
            if (!document.hidden && st.seen) st.motion = requestAnimationFrame(step);
        };
        const seen = new IntersectionObserver((entries) => {
            st.seen = entries[entries.length - 1].isIntersecting;
            run();
        });
        seen.observe(stage.current);
        run();
        document.addEventListener("visibilitychange", run);
        return () => {
            seen.disconnect();
            document.removeEventListener("visibilitychange", run);
            cancelAnimationFrame(st.motion);
            st.motion = 0;
        };
    }, [motion]);

    useEffect(() => () => {
        cancelAnimationFrame(live.current.frame);
        cancelAnimationFrame(live.current.glide);
        cancelAnimationFrame(live.current.reshape);
    }, []);

    // The wheel scales the picture round the pointer; with Shift it brings
    // the next system forward, up the one behind, down the one before, round
    // and round. It is listened to by hand: a wheel the page may not cancel
    // would scroll the page under the graph with every step.
    useEffect(() => {
        const el = stage.current;
        const wheel = (e) => {
            const st = live.current;
            e.preventDefault();
            if (!st.size || !st.lens) return;
            const px = e.deltaMode === 1 ? 16 : e.deltaMode === 2 ? st.size.H : 1;
            if (e.shiftKey) {
                // With Shift held the browser may turn the wheel sideways.
                st.next += (e.deltaY || e.deltaX) * px;
                if (Math.abs(st.next) < NEXT || !st.order || st.order.length < 2) return;
                const by = st.next < 0 ? 1 : -1;
                st.next = 0;
                const n = st.order.length;
                hideRef.current();
                bringRef.current(st.order[(st.order.indexOf(st.front) + by + n) % n]);
                return;
            }
            const box = el.getBoundingClientRect();
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
    const show = (h, at) => setTip(h ? { node: h.node || null, star: h.star || null, x: at.x, y: at.y } : null);
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
        const was = tip && (tip.node || tip.star);
        if ((h && (h.node || h.star)) !== was) show(h, at);
    };
    // A click on a planet opens its session; a click on a star, or among the
    // orbits of a system behind, brings that system forward.
    const up = (e) => {
        const st = live.current;
        const drag = st.drag;
        st.drag = null;
        stage.current.classList.remove("drag");
        if (drag && drag.moved && !drag.turn) request(true);
        if (!drag || drag.moved || drag.button !== 0) return;
        const at = point(e);
        const h = hitAt(st.hits, at.x, at.y) || behindAt(st.areas, st.front, at.x, at.y);
        if (!h) return;
        if (h.star) {
            setTip(null);
            bring(h.star.name);
            return;
        }
        onSession({ name: h.node.s.session, id: h.node.s.sessionId });
    };
    const context = (e) => {
        if (!menu) return;
        const at = point(e);
        const h = hitAt(live.current.hits, at.x, at.y);
        if (!h || !h.node) return;
        const s = h.node.s;
        const open = () => onSession({ name: s.session, id: s.sessionId });
        setTip(null);
        menu(...liveMenuOf(s, { exec, snapshot, wait, onOpen: open, onTerm }))(e, stage.current);
    };
    // Reset view brings back the turn, the place, the scale and the order of
    // the systems by how alive they are now.
    const reset = () => {
        const st = live.current;
        cancelAnimationFrame(st.glide);
        st.glide = 0;
        st.view = { ...HOME };
        st.zoom = 1;
        st.pan = { x: 0, y: 0 };
        st.order = orderOf(graph);
        st.front = st.order[0] || null;
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
                    <button class="dkorbbtn" type="button" aria-pressed=${motion ? "true" : "false"} onClick=${toggleMotion}>
                        <i class="dkorbsw"></i>Motion
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
                ${tip && html`<${Tip} tip=${tip} front=${live.current.front} onPlaced=${placed} />`}
            </div>
            <${Legend} theme=${theme} />
        </section>
    `;
}
