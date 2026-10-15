// Where a session stands in the graph of the home screen, and how the graph is
// seen. Numbers in, numbers out: nothing here draws or reads the page.
//
// A contour is a ring around the host. A session stands on the ring of its
// contour at an angle that says how long ago it last asked the model: the
// meridian of now runs to the front, the time goes clockwise from it. A
// session another one opened is its moon, on a small orbit around it, at the
// angle of its own last request.

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

// The rings, in the units of the scene: the camera fits them to the canvas,
// so only their proportions count. ROOM is what a node needs along its ring
// for its mark and its name.
const RING = 150;
const RING_STEP = 97.5;
const ROOM = 64;

// MOON is the orbit of a session's moons; a moon's own moons go on an orbit
// smaller by MOON_SHRINK, so a grandchild stays inside its parent's reach.
export const MOON = 34;
const MOON_SHRINK = 0.6;
const MOON_ROOM = 22;

// at is a point of the dial plane: angle a clockwise from the front, at radius
// r and height y.
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

// layout places the nodes of the graph. contours lists the rings from the
// innermost out, each with the keys of its sessions; nodes maps a key to
// { parent, idle }, parent being the key of the session that opened it.
// It answers with a ring for every contour that has a session, and for
// every node its point (w), its angle (ang), its depth among moons, its
// moons (kids) and the radius of their orbit (orbit).
export function layout(contours, nodes) {
    const placed = new Map();
    for (const [key, n] of nodes) {
        placed.set(key, { key, parent: n.parent || null, idle: n.idle, ang: dial(n.idle), kids: [], depth: 0, w: null });
    }
    // A parent that is not live, or a chain of parents that comes back to the
    // node itself, leaves the node on its ring: it has nobody to orbit.
    for (const n of placed.values()) {
        if (!n.parent || !placed.has(n.parent) || loops(n, placed)) n.parent = null;
    }
    for (const n of placed.values()) if (n.parent) placed.get(n.parent).kids.push(n);

    const rings = [];
    for (const c of contours) {
        const members = c.keys.map((k) => placed.get(k)).filter(Boolean);
        if (members.length === 0) continue;
        const R = RING + rings.length * RING_STEP;
        rings.push({ name: c.name, R, members });
        const roots = members.filter((n) => !n.parent);
        for (const n of roots) depthOf(n, 0);
        spread(roots, (a, b) => Math.max(ROOM + reach(a), ROOM + reach(b)) / R);
        for (const n of roots) {
            n.w = at(n.ang, R);
            moons(n);
        }
    }
    return { rings, nodes: placed };
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
// angle two neighbours need between them. When the ring cannot hold every
// node at its full gap, the gaps shrink all together until the last node
// stops short of the first one round the ring.
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

// The view the graph opens with and comes back to on Reset: the front a
// little to the right, the plane tilted enough for the rings to read as
// rings and flat enough for the names to stand apart.
export const HOME = { yaw: 0.12, pitch: 0.42 };

// How far a drag may tip the plane: past these it is seen edge on, or from
// below.
export const PITCH = [-0.2, 0.95];

// DIST is how far the eye stands from the host, in the units of the scene:
// near enough for the front to read as nearer, far enough not to bend.
export const DIST = 1500;

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
// [top, right, bottom, left], for every view of wide in width and of tall in
// height: a turn of the scene never zooms it.
export function fit(points, wide, tall, W, H, pad) {
    let x0 = Infinity;
    let x1 = -Infinity;
    let y0 = Infinity;
    let y1 = -Infinity;
    const lens = { f: 1, dist: DIST, ox: 0, oy: 0 };
    for (const v of wide) {
        const pr = camera(v, 0, 0, lens);
        for (const p of points) {
            const q = pr(p);
            x0 = Math.min(x0, q.x);
            x1 = Math.max(x1, q.x);
        }
    }
    for (const v of tall) {
        const pr = camera(v, 0, 0, lens);
        for (const p of points) {
            const q = pr(p);
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

// lensFor fits the rings of a layout to a canvas: in width over a whole turn,
// in height over the tilts a little either side of the home view, so the
// scene neither zooms as it turns nor shrinks for a tilt nobody makes.
export function lensFor(rings, W, H, pad) {
    const points = [];
    for (const r of rings) for (let i = 0; i < 16; i++) points.push(at((i / 16) * TAU, r.R + 48));
    points.push([0, 30, 0], [0, -30, 0]);
    const wide = [];
    for (let i = 0; i < 16; i++) wide.push({ yaw: HOME.yaw + (i * TAU) / 16, pitch: HOME.pitch });
    const tall = [];
    for (let i = 0; i < 7; i++) tall.push({ yaw: HOME.yaw + (i / 6 - 0.5) * 1.2, pitch: HOME.pitch });
    return fit(points, wide, tall, W, H, pad);
}
