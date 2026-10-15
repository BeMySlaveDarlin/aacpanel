// Where a session stands in the graph of the home screen, and how the graph is
// seen. Numbers in, numbers out: nothing here draws or reads the page.
//
// A contour is a system of its own: a star, and round it an orbit for every
// project with a live session, in the order of the map. The sessions of a
// project are the planets of its orbit, each at an angle that says how long
// ago it last asked the model — the meridian of now runs to the front of
// every system alike, the time goes clockwise from it. A session another one
// opened is that one's moon, at the angle of its own last request, and its
// own sessions are moons of the moon. The systems stand in depth, a galaxy:
// one at the front, the others behind it.

export const TAU = Math.PI * 2;

// The dial is logarithmic: the first minutes after a request are where a
// session is told from its neighbours, a quiet afternoon is one place. Five
// minutes, half an hour, an hour, six hours and a day all have room, and the
// day takes 300 degrees; older stands at the end of the scale. The last 60
// degrees stay empty, so the oldest session is never read as the newest one
// beside the meridian.
export const DAY = 1440;
const STEP = 2;
export const SPAN = (TAU * 300) / 360;

// dial is the angle of a session that last asked min minutes ago.
export function dial(min) {
    const m = Math.max(0, min || 0);
    return SPAN * Math.min(1, Math.log(1 + m / STEP) / Math.log(1 + DAY / STEP));
}

// idleOf is how many minutes ago a session last asked the model, counted from
// now in milliseconds. A session that has not asked yet counts from its start;
// one that names neither stands at the end of the scale.
export function idleOf(s, now) {
    const at = Date.parse((s && s.lastRequestAt) || "") || Date.parse((s && s.startedAt) || "");
    if (!Number.isFinite(at)) return DAY;
    return Math.max(0, (now - at) / 60000);
}

// The orbits of a system, in the units of the scene; the camera fits them to
// the canvas, so only their proportions count. The first orbit keeps STAR
// clear for the star and its name; the next stands a STEP out at least, and
// further where the moons of two neighbouring orbits would meet. ROOM is what
// a planet needs along its orbit for its mark and its name.
const STAR = 40;
const ORBIT_STEP = 58;
const ORBIT_GAP = 30;
const ROOM = 70;

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

// orbitOf is the radius of the orbit a node of depth d keeps its moons on.
export function orbitOf(depth) {
    return MOON * MOON_SHRINK ** depth;
}

// layout places the nodes of every system in the plane of its system.
// systems lists the contours, each with its orbits from the innermost out:
// { name, orbits: [{ key, name, keys }] }, keys being the planets of the
// orbit; nodes maps the key of every session to { parent, idle }, parent the
// key of the session that opened it within the same contour. It answers the
// systems that have a planet, each with its radius R and its orbits with
// theirs, and for every node its point in its system's plane (w), its angle,
// its depth among moons, its moons and the radius of their orbit.
export function layout(systems, nodes) {
    const placed = new Map();
    for (const [key, n] of nodes) {
        placed.set(key, { key, parent: n.parent || null, idle: n.idle, ang: dial(n.idle), kids: [], depth: 0, w: null });
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
        let r = 0;
        orbits.forEach((o, i) => {
            for (const p of o.planets) depthOf(p, 0);
            o.reach = Math.max(...o.planets.map(reach));
            r = i === 0 ? STAR + o.reach + 14 : r + Math.max(ORBIT_STEP, orbits[i - 1].reach + o.reach + ORBIT_GAP);
            o.R = r;
            spread(o.planets, (a, b) => Math.max(ROOM + reach(a), ROOM + reach(b)) / o.R);
            for (const p of o.planets) {
                p.w = at(p.ang, o.R);
                moons(p);
            }
        });
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
    for (const k of n.kids) depthOf(k, depth + 1);
}

// reach is how far a node's moons, and theirs, stand from it.
function reach(n) {
    if (n.kids.length === 0) return 0;
    return n.orbit + Math.max(...n.kids.map(reach));
}

function moons(p) {
    if (p.kids.length === 0) return;
    spread(p.kids, () => MOON_ROOM / p.orbit);
    for (const k of p.kids) {
        k.w = add(p.w, at(k.ang, p.orbit));
        moons(k);
    }
}

// spread pulls apart the nodes that would stand on one another, and only
// toward older: a session is never drawn fresher than it is. gap(a, b) is the
// angle two neighbours need between them. When the orbit cannot hold every
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

// The tilts of the systems, by their place in the map: every plane leans its
// own way, so two systems read as two even when they stand close.
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
// system standing at place { c, tilt }.
export function worldOf(place, p) {
    const q = tilted(place.tilt, p);
    return [place.c[0] + q[0], place.c[1] + q[1], place.c[2] + q[2]];
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

// The places of the galaxy, in radii of the largest system: the front one in
// the middle and nearest, then one behind on the left, one behind on the
// right, one far behind in the middle; any more go round further back.
const PLACES = [[0, 0, -0.65], [-2.4, 0.5, 1.4], [2.3, 0.3, 0.9], [0, 0.9, 2.3]];

function placeOf(k) {
    if (k < PLACES.length) return PLACES[k];
    const a = k * 2.4;
    const ring = 2.6 + 0.4 * Math.floor(k / PLACES.length);
    return [Math.sin(a) * ring, 0.6 + 0.2 * k, 1.5 + 0.5 * k];
}

// slotsFor gives count places for systems of radius up to R, spread out
// sideways until, seen from the home view, no two of them cover each other:
// any system fits any place, so the front can change hands without a
// system landing on another.
export function slotsFor(count, R) {
    let wide = 1;
    let slots = [];
    for (let i = 0; i < 40; i++) {
        slots = Array.from({ length: count }, (_, k) => {
            const p = placeOf(k);
            return [p[0] * R * wide, p[1] * R, p[2] * R];
        });
        if (apart(slots, R)) break;
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

// A gap of a twentieth of the larger box between two systems.
function apart(slots, R) {
    const boxes = slots.map((c) => boxOf(c, R));
    for (let i = 0; i < boxes.length; i++) {
        for (let j = i + 1; j < boxes.length; j++) {
            const a = boxes[i];
            const b = boxes[j];
            const g = Math.max(a.w, b.w) / 20;
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

// fit picks the lens that keeps every point inside the canvas, less pad
// [top, right, bottom, left], for every view of views.
export function fit(points, views, W, H, pad) {
    let x0 = Infinity;
    let x1 = -Infinity;
    let y0 = Infinity;
    let y1 = -Infinity;
    const lens = { f: 1, dist: DIST, ox: 0, oy: 0 };
    for (const v of views) {
        const pr = camera(v, 0, 0, lens);
        for (const p of points) {
            const q = pr(p);
            x0 = Math.min(x0, q.x);
            x1 = Math.max(x1, q.x);
            y0 = Math.min(y0, q.y);
            y1 = Math.max(y1, q.y);
        }
    }
    const f = Math.max(0.01, Math.min((W - pad[1] - pad[3]) / (x1 - x0), (H - pad[0] - pad[2]) / (y1 - y0)));
    return {
        f,
        dist: DIST,
        ox: pad[3] + (W - pad[1] - pad[3]) / 2 - W / 2 - (f * (x0 + x1)) / 2,
        oy: pad[0] + (H - pad[0] - pad[2]) / 2 - H / 2 - (f * (y0 + y1)) / 2,
    };
}

// floorOf is the height of the floor the stalks of the stars stand on: under
// the lowest place, by three quarters of the largest system.
export function floorOf(slots, R) {
    return Math.min(...slots.map((c) => c[1])) - 0.75 * R;
}

// lensFor fits every place of the galaxy, a circle of R round each and its
// stalk down to the floor, to a canvas, from the home view and a little
// either side of it: the first view holds every system whichever is in
// front, and a short turn does not push one out.
export function lensFor(slots, R, W, H, pad) {
    const points = [];
    const floor = floorOf(slots, R);
    for (const c of slots) {
        for (let i = 0; i < 24; i++) points.push(worldOf({ c, tilt: { tx: 0, tz: 0 } }, at((i / 24) * TAU, R + 26)));
        points.push([c[0], floor - 10, c[2]]);
    }
    const views = [HOME, { ...HOME, yaw: HOME.yaw - 0.2 }, { ...HOME, yaw: HOME.yaw + 0.2 }];
    return fit(points, views, W, H, pad);
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
