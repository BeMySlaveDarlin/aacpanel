// Where a session stands in the graph of the home screen, and how the graph is
// seen. Numbers in, numbers out: nothing here draws or reads the page.
//
// A contour is a system of its own: a star, and round it an orbit for every
// project with a live session — the small and quiet ones inside, the large
// and lively further out, each orbit as wide as the widest family on it and
// leaning its own way. The sessions of a project are the planets of its
// orbit and go round their star, the inner orbits faster than the outer, the
// idle ones slowly; a session another one opened is that one's moon and goes
// round it faster still, and its own sessions are moons of the moon. Where a
// planet stands on its orbit says nothing of its own: the tip tells how long
// ago it last asked the model. The systems stand in depth, a galaxy: one in
// front, drawn large, the others smaller behind it.

export const TAU = Math.PI * 2;

// A day, in minutes: how long ago a session that names no time last asked.
export const DAY = 1440;

// idleOf is how many minutes ago a session last asked the model, counted from
// now in milliseconds. A session that has not asked yet counts from its start;
// one that names neither counts a day.
export function idleOf(s, now) {
    const at = Date.parse((s && s.lastRequestAt) || "") || Date.parse((s && s.startedAt) || "");
    if (!Number.isFinite(at)) return DAY;
    return Math.max(0, (now - at) / 60000);
}

// The orbits of a system, in the units of the scene; the camera fits them to
// the canvas, so only their proportions count. An orbit is a band as wide as
// the widest family on it — a planet and its moons of every depth across —
// and BAND for the marks and the names beside it; the first band starts STAR
// out, clear of the star and its name, and every orbit runs down the middle
// of its band. ROOM is what a planet needs along its orbit for its mark and
// its name.
const STAR = 25;
const BAND = 58;
const ROOM = 70;

// Every orbit leans its own way, the orbit of a planet's moons too: by an
// inclination within INCLINE about a line through the star at an angle of its
// own, both drawn from the name of the orbit — the same in every snapshot, and
// not two alike. The moons lean within MOON_INCLINE: a moon on a steeper orbit
// round a planet of the outer orbit would stand past the height the system is
// given, as the planets go round.
export const INCLINE = [(5 * Math.PI) / 180, (25 * Math.PI) / 180];
export const MOON_INCLINE = [(5 * Math.PI) / 180, (12 * Math.PI) / 180];

// MOON is the orbit of a planet's moons; a moon's own moons go on an orbit
// smaller by MOON_SHRINK, so a grandchild stays inside its parent's reach.
export const MOON = 54;
const MOON_SHRINK = 0.6;
const MOON_ROOM = 30;

// at is a point of a system's plane: angle a clockwise from the front, at
// radius r and height y.
export function at(a, r, y = 0) {
    return [-Math.sin(a) * r, y, -Math.cos(a) * r];
}

export function add(p, q) {
    return [p[0] + q[0], p[1] + q[1], p[2] + q[2]];
}

// hashOf is a number in [0, 1) that a name and a salt always give.
export function hashOf(name, salt) {
    let h = 2166136261 ^ salt;
    for (let i = 0; i < name.length; i++) h = Math.imul(h ^ name.charCodeAt(i), 16777619);
    h = Math.imul(h ^ (h >>> 16), 0x85ebca6b);
    h = Math.imul(h ^ (h >>> 13), 0xc2b2ae35);
    return ((h ^ (h >>> 16)) >>> 0) / 4294967296;
}

// leanOf is how the orbit of a name leans: its inclination inc, within range,
// and the angle of the line it leans about, node.
export function leanOf(name, range = INCLINE) {
    return { inc: range[0] + (range[1] - range[0]) * hashOf(name, 1), node: TAU * hashOf(name, 2) };
}

// inclined turns a point of an orbit's flat circle about the line of the
// lean by its inclination.
export function inclined(t, p) {
    const nx = Math.cos(t.node);
    const nz = Math.sin(t.node);
    const c = Math.cos(t.inc);
    const s = Math.sin(t.inc);
    const dot = nx * p[0] + nz * p[2];
    return [
        p[0] * c - nz * p[1] * s + nx * dot * (1 - c),
        p[1] * c + (nz * p[0] - nx * p[2]) * s,
        p[2] * c + nx * p[1] * s + nz * dot * (1 - c),
    ];
}

// onOrbit is the point of orbit o at angle a, in the plane of its system.
export function onOrbit(o, a) {
    return inclined(o.tilt, at(a, o.R));
}

// The orbits turn, their planets going round the star: the innermost orbit of
// a system once in TURN seconds, every orbit further out slower, as the
// planets of a sun go — the time of a turn as the radius to the power of one
// and a half — and an orbit where every session idles at IDLE of that, so the
// live ones turn and the sleeping ones all but stand. Moons go round their
// planet MOONS times as fast as the planet goes round its star.
export const TURN = 180;
export const IDLE = 0.25;
export const MOONS = 3;

// spinOf is how fast an orbit of radius R turns, in radians a second, in a
// system whose innermost orbit has radius inner; live says a session on it
// works or waits.
export function spinOf(R, inner, live) {
    return (TAU / TURN) * (inner / R) ** 1.5 * (live ? 1 : IDLE);
}

// orbitOf is the radius of the orbit a node of depth d keeps its moons on.
export function orbitOf(depth) {
    return MOON * MOON_SHRINK ** depth;
}

// layout places the nodes of every system in the plane of its system, each
// planet at a place of its own on its orbit, drawn from its name and pulled
// clear of its neighbours, and sets every orbit and every planet with moons
// turning.
// systems lists the contours, each with its orbits: { name, orbits: [{ key,
// name, keys }] }, keys being the planets of the orbit; nodes maps the key of
// every session to { parent, idle, pull }, parent the key of the session that
// opened it within the same contour, pull how hard it draws its orbit out — a
// session at work or waiting. An orbit weighs as many as the sessions of its
// families, moons of every depth among them, and their pull; the lighter
// stand inside, and orbits of equal weight keep the order they came in. It
// answers the systems that have a planet, each with its radius R and its
// orbits with theirs, their bands and leans, and for every node its point in
// its system's plane (w), its angle, its depth among moons, its moons, the
// radius of their orbit and its lean; and how fast every orbit turns (spin)
// and the moons of every planet (its spin).
export function layout(systems, nodes) {
    const placed = new Map();
    for (const [key, n] of nodes) {
        placed.set(key, { key, parent: n.parent || null, idle: n.idle, pull: n.pull || 0, ang: TAU * hashOf(key, 3), kids: [], depth: 0, w: null });
    }
    // A parent that is not live, or a chain of parents that comes back to the
    // node itself, leaves the node a planet: it has nobody to orbit.
    for (const n of placed.values()) {
        if (!n.parent || !placed.has(n.parent) || loops(n, placed)) n.parent = null;
    }
    for (const n of placed.values()) if (n.parent) placed.get(n.parent).kids.push(n);

    const out = [];
    for (const sys of systems) {
        const orbits = sys.orbits
            .map((o) => ({ ...o, planets: o.keys.map((k) => placed.get(k)).filter((n) => n && !n.parent) }))
            .filter((o) => o.planets.length > 0);
        if (orbits.length === 0) continue;
        for (const o of orbits) {
            for (const p of o.planets) depthOf(p, 0);
            o.reach = Math.max(...o.planets.map(reach));
            o.weight = o.planets.reduce((sum, p) => sum + weightOf(p), 0);
        }
        orbits.sort((a, b) => a.weight - b.weight);
        let edge = STAR;
        for (const o of orbits) {
            o.band = 2 * o.reach + BAND;
            o.R = edge + o.band / 2;
            edge += o.band;
            o.tilt = leanOf(`${sys.name}/${o.key}`);
            o.spin = spinOf(o.R, orbits[0].R, o.planets.some(lively));
            for (const p of o.planets) spinMoons(p, o.spin * MOONS);
            spread(o.planets, (a, b) => Math.max(ROOM + reach(a), ROOM + reach(b)) / o.R);
            for (const p of o.planets) spreadMoons(p);
            placeOrbit(o);
        }
        const members = [];
        const gather = (n) => {
            n.system = sys.name;
            members.push(n);
            n.kids.forEach(gather);
        };
        for (const o of orbits) {
            for (const p of o.planets) {
                p.orbitKey = o.key;
                gather(p);
            }
        }
        const last = orbits[orbits.length - 1];
        out.push({ name: sys.name, orbits, members, R: last.R + last.reach + 18 });
    }
    return { systems: out, nodes: placed };
}

function loops(n, placed) {
    const seen = new Set([n.key]);
    for (let p = placed.get(n.parent); p && p.parent; p = placed.get(p.parent)) {
        if (seen.has(p.parent)) return true;
        seen.add(p.key);
    }
    return false;
}

function depthOf(n, depth) {
    n.depth = depth;
    n.orbit = orbitOf(depth);
    n.lean = leanOf(`moons/${n.key}`, MOON_INCLINE);
    for (const k of n.kids) depthOf(k, depth + 1);
}

// lively says a session of a family works or waits.
function lively(n) {
    return n.pull > 0 || n.kids.some(lively);
}

// spinMoons sets how fast the moons of a node go round it, and theirs round
// them, each depth MOONS times as fast as the one above.
function spinMoons(n, spin) {
    n.spin = spin;
    for (const k of n.kids) spinMoons(k, spin * MOONS);
}

// weightOf is what a family weighs: a session apiece and their pull.
function weightOf(n) {
    return 1 + n.pull + n.kids.reduce((sum, k) => sum + weightOf(k), 0);
}

// reach is how far a node's moons, and theirs, stand from it.
function reach(n) {
    if (n.kids.length === 0) return 0;
    return n.orbit + Math.max(...n.kids.map(reach));
}

function spreadMoons(p) {
    if (p.kids.length === 0) return;
    spread(p.kids, () => MOON_ROOM / p.orbit);
    for (const k of p.kids) spreadMoons(k);
}

function moons(p) {
    for (const k of p.kids) {
        k.w = add(p.w, inclined(p.lean, at(k.ang + (p.turn || 0), p.orbit)));
        moons(k);
    }
}

// placeOrbit puts the planets of an orbit, and their moons, where the
// orbit's radius, lean and turn say: on a new snapshot, on every frame of an
// orbit gliding to a new place and of the orbits turning.
export function placeOrbit(o) {
    for (const p of o.planets) {
        p.w = onOrbit(o, p.ang + (o.turn || 0));
        moons(p);
    }
}

// turnBy turns the orbits of a graph, and the moons round their planets, by
// dt seconds from where turns has them — a map from every orbit and every
// planet with moons to how far it has turned, which outlives the snapshots —
// and puts every node where that leaves it. A dt of 0 only puts them there.
export function turnBy(graph, turns, dt) {
    const turn = (key, spin) => {
        const now = ((turns.get(key) || 0) + spin * dt) % TAU;
        turns.set(key, now);
        return now;
    };
    const kin = (n) => {
        if (n.kids.length === 0) return;
        n.turn = turn(`moons/${n.key}`, n.spin);
        n.kids.forEach(kin);
    };
    for (const sys of graph.systems) {
        for (const o of sys.orbits) {
            o.turn = turn(`${sys.name}/${o.key}`, o.spin);
            o.planets.forEach(kin);
            placeOrbit(o);
        }
    }
}

// spread pulls apart the nodes that would stand on one another, forward
// round the orbit. gap(a, b) is the angle two neighbours need between them. When the orbit cannot hold every
// node at its full gap, the gaps shrink all together until the last node
// stops short of the first one round the orbit.
export function spread(list, gap) {
    if (list.length === 0) return;
    list.sort((a, b) => a.ang - b.ang || (a.key < b.key ? -1 : 1));
    const from = list.map((n) => n.ang);
    const push = (k) => {
        let last = from[0];
        list[0].ang = last;
        for (let i = 1; i < list.length; i++) {
            last = Math.max(from[i], last + k * gap(list[i - 1], list[i]));
            list[i].ang = last;
        }
        return last;
    };
    const fits = (k) => list.length < 2 || push(k) <= from[0] + TAU - k * gap(list[list.length - 1], list[0]);
    if (fits(1)) return;
    let lo = 0;
    let hi = 1;
    for (let i = 0; i < 24; i++) {
        const mid = (lo + hi) / 2;
        if (fits(mid)) lo = mid;
        else hi = mid;
    }
    push(lo);
}

// The tilts of the systems behind the front, by their place in the map: every
// plane leans its own way, so two systems read as two even when they stand
// close. The system in front faces the view square (FLAT), so the near plan is
// as tall as the lens means it to be whichever system it is.
export const FLAT = { tx: 0, tz: 0 };
const TILTS = [
    { tx: 0.14, tz: -0.08 },
    { tx: 0, tz: 0.05 },
    { tx: -0.12, tz: 0.09 },
    { tx: 0.08, tz: 0.12 },
    { tx: -0.06, tz: -0.1 },
];

export function tiltOf(index) {
    return TILTS[index % TILTS.length];
}

// tilted turns a point of a system's plane by the tilt of the system.
export function tilted(t, p) {
    const cx = Math.cos(t.tx);
    const sx = Math.sin(t.tx);
    const cz = Math.cos(t.tz);
    const sz = Math.sin(t.tz);
    const y1 = p[1] * cx - p[2] * sx;
    const z1 = p[1] * sx + p[2] * cx;
    return [p[0] * cz - y1 * sz, p[0] * sz + y1 * cz, z1];
}

// worldOf is where a point of a system's plane stands in the scene, the
// system standing at place { c, tilt, k }: at c, tilted, scaled by k.
export function worldOf(place, p) {
    const q = tilted(place.tilt, p);
    const k = place.k || 1;
    return [place.c[0] + q[0] * k, place.c[1] + q[1] * k, place.c[2] + q[2] * k];
}

// The view the graph opens with and comes back to on Reset: the galaxy seen
// a little from the right and from above, so the systems behind the front
// one stand clear of it and the depth between them reads.
export const HOME = { yaw: 0.28, pitch: 0.5 };

// How far a drag may tip the scene: past these it is seen edge on, or from
// below.
export const PITCH = [-0.2, 0.95];

// DIST is how far the eye stands from the front of the galaxy, in the units
// of the scene: far enough for a system three deep not to shrink to a dot.
export const DIST = 2200;

// The galaxy is a near plan and a back row. The system in front is drawn
// at the size of the largest system whatever its own — a small contour
// brought forward is drawn up to it, its marks and names keeping their size —
// and the systems behind are BACK of their own size, in a row further back
// and above it, by turns to the left and to the right of the middle, further
// out the more of them there are. The places are the same whichever system
// stands in front, so bringing another forward never moves the picture.
export const BACK = 0.4;

// slotsFor gives count places for a galaxy whose largest system has radius
// R: the first in front, the rest behind, each { c, R } with the radius a
// system drawn there takes at most. A place behind is raised until, seen from
// the home view, it stands clear above the system in front, and the row is
// spread sideways until no two of its places cover each other.
export function slotsFor(count, R) {
    const back = BACK * R;
    const front = boxOf([0, 0, 0], R);
    const slots = [{ c: [0, 0, 0], R }];
    let wide = 1;
    for (let tries = 0; tries < 40; tries++) {
        slots.length = 1;
        for (let j = 0; j < count - 1; j++) {
            const side = j % 2 ? 1 : -1;
            const rank = Math.floor(j / 2);
            const c = [side * R * (0.85 + 0.95 * rank) * wide, 0, R * (0.9 + 0.4 * rank)];
            for (let up = 0; up < 60; up++) {
                const b = boxOf(c, back);
                if (b.y + b.h <= front.y - 0.05 * front.h) break;
                c[1] += 0.05 * R;
            }
            slots.push({ c, R: back });
        }
        if (apart(slots)) break;
        wide *= 1.08;
    }
    return slots;
}

// boxOf is the box a circle of radius R round c takes on the screen of the
// home view, before any lens.
export function boxOf(c, R, tilt = { tx: 0, tz: 0 }) {
    const pr = camera(HOME, 0, 0, { f: 1, dist: DIST, ox: 0, oy: 0 });
    let x0 = Infinity;
    let x1 = -Infinity;
    let y0 = Infinity;
    let y1 = -Infinity;
    for (let i = 0; i < 32; i++) {
        const q = pr(worldOf({ c, tilt }, at((i / 32) * TAU, R)));
        x0 = Math.min(x0, q.x);
        x1 = Math.max(x1, q.x);
        y0 = Math.min(y0, q.y);
        y1 = Math.max(y1, q.y);
    }
    return { x: x0, y: y0, w: x1 - x0, h: y1 - y0 };
}

// A gap of a tenth of the smaller box between two places.
function apart(slots) {
    const boxes = slots.map((s) => boxOf(s.c, s.R));
    for (let i = 0; i < boxes.length; i++) {
        for (let j = i + 1; j < boxes.length; j++) {
            const a = boxes[i];
            const b = boxes[j];
            const g = Math.min(a.w, b.w) / 10;
            if (a.x < b.x + b.w + g && b.x < a.x + a.w + g && a.y < b.y + b.h + g && b.y < a.y + a.h + g) return false;
        }
    }
    return true;
}

// camera projects a point of the scene onto a canvas of W×H for a view
// { yaw, pitch } and a lens k = { f, dist, ox, oy }. z grows away from the eye.
export function camera(view, W, H, k) {
    const cy = Math.cos(view.yaw);
    const sy = Math.sin(view.yaw);
    const cp = Math.cos(view.pitch);
    const sp = Math.sin(view.pitch);
    return (p) => {
        const x = p[0] * cy + p[2] * sy;
        const z0 = -p[0] * sy + p[2] * cy;
        const y = p[1] * cp + z0 * sp;
        const z = -p[1] * sp + z0 * cp;
        const s = k.f / (k.dist + z);
        return { x: W / 2 + k.ox + x * s, y: H / 2 + k.oy - y * s, z, s };
    };
}

// floorOf is the height of the floor the stalks of the stars stand on: a
// little under the plane of the system in front.
export function floorOf(R) {
    return -0.3 * R;
}

// FRONT is how much of the height of the canvas the system in front takes
// at most: the near plan is the subject of the widget, the row behind its
// setting, and a lone system is not blown up to the edges.
const FRONT = 0.7;

// lensFor fits the galaxy to a canvas from the home view: every place in it,
// less pad [top, right, bottom, left], the system in front in the middle
// across and taking no more than FRONT of the height.
export function lensFor(slots, W, H, pad) {
    const boxes = slots.map((s) => boxOf(s.c, s.R * 1.08));
    const x0 = Math.min(...boxes.map((b) => b.x));
    const x1 = Math.max(...boxes.map((b) => b.x + b.w));
    const y0 = Math.min(...boxes.map((b) => b.y));
    const y1 = Math.max(...boxes.map((b) => b.y + b.h));
    const front = boxes[0];
    const half = Math.max(front.x + front.w / 2 - x0, x1 - (front.x + front.w / 2));
    const f = Math.min((W - pad[1] - pad[3]) / (2 * half), (H - pad[0] - pad[2]) / (y1 - y0), (FRONT * H) / front.h);
    return {
        f,
        dist: DIST,
        ox: -(front.x + front.w / 2) * f,
        oy: pad[0] + (H - pad[0] - pad[2]) / 2 - H / 2 - (f * (y0 + y1)) / 2,
    };
}

// ZOOM bounds the scale the wheel sets: out to where the whole scene is a
// little smaller than its box, in to where a cluster of moons reads mark by
// mark. Past either the scene is a speck or a single mark.
export const ZOOM = [0.6, 4];

// zoomAt is the zoom and the pan that scale the picture by `by` round the
// point m of the canvas, so what stood under m stays under it, the zoom held
// to ZOOM. c is the centre of the lens with no pan, the point the zoom of a
// lens scales the picture from.
export function zoomAt(zoom, pan, by, m, c) {
    const z = Math.min(ZOOM[1], Math.max(ZOOM[0], zoom * by));
    const k = z / zoom;
    return {
        zoom: z,
        pan: { x: m.x - c.x - k * (m.x - c.x - pan.x), y: m.y - c.y - k * (m.y - c.y - pan.y) },
    };
}

// lensAt is a fitted lens scaled by zoom and moved by pan: the picture grows
// round its centre and slides, while the marks and the names keep their size
// in pixels, as on a map.
export function lensAt(lens, zoom, pan) {
    return { ...lens, f: lens.f * zoom, ox: lens.ox + pan.x, oy: lens.oy + pan.y };
}
