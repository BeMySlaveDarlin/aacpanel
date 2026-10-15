// The drawing of the galaxy of sessions on a canvas. Every contour is a
// system in a hue of its own: a star, the orbits of its projects as hairlines
// barely there, a dotted stalk down to the floor that tells how deep it
// stands.
// A session is a planet lit in part, as a moon in its phase: the lit share is
// how much of its context it has used. The marks keep their colours of state
// — the accent of a session at work in a small steady glow, the gold of a
// session waiting for the person, the top of the context scale near the cap
// — and the one wide light stands behind the session that has waited longest.
import { TAU, at, add, inclined, onOrbit, worldOf } from "./space.js";

// NEAR is where the lit part takes the tone of the cap and the frame says the
// number: a session past it is a turn or two of a long task from the cap.
const NEAR = 62;

// Sizes in units of the scene: a unit is a pixel at the scale the interface
// opens on, and grows with it as the type does. The orbits stay hairlines at
// any scale.
const NODE_R = 8;
const MOON_R = 6;
const BUSY_GLOW = 13;
const WAIT_RING = 12;
const BEADS = 9;
export const HALO = 6;
const GLOW = 150;
const REACH = [8, 22, 40, 62, 90];

// The orbits are barely there: ORBIT[1] on the half of an orbit nearer the
// eye, ORBIT[0] on the far half — enough to follow an orbit round and see
// which way it leans, never a line that rivals a mark.
export const ORBIT = [0.05, 0.08];

// Where on its orbit the name of an orbit stands: a fixed point at the back
// and to the left, past which the planets go.
const ORBIT_NAME_AT = (TAU * 330) / 360;
const TAG_PAD = [2, 5];

// rgba is a colour of the palette at an opacity. The palette is written in
// hex in the styles, so this is all the parsing it takes.
function rgba(hex, a) {
    const h = hex.replace("#", "");
    const n = h.length === 3 ? h.split("").map((c) => parseInt(c + c, 16)) : [0, 2, 4].map((i) => parseInt(h.slice(i, i + 2), 16));
    return `rgba(${n[0]},${n[1]},${n[2]},${a})`;
}

function clamp(v, a, b) {
    return Math.max(a, Math.min(b, v));
}

function circle(ctx, x, y, r) {
    ctx.beginPath();
    ctx.arc(x, y, Math.max(0.3, r), 0, TAU);
}

// line strokes a path through points already on the screen, a segment at a
// time, each in the opacity alpha gives it by its depth.
function line(ctx, points, colour, alpha) {
    for (let i = 1; i < points.length; i++) {
        ctx.strokeStyle = rgba(colour, alpha(points[i - 1], points[i], i));
        ctx.beginPath();
        ctx.moveTo(points[i - 1].x, points[i - 1].y);
        ctx.lineTo(points[i].x, points[i].y);
        ctx.stroke();
    }
}

// overlap is the area two rectangles share.
export function overlap(a, b) {
    const w = Math.min(a.x + a.w, b.x + b.w) - Math.max(a.x, b.x);
    const h = Math.min(a.y + a.h, b.y + b.h) - Math.max(a.y, b.y);
    return w > 0 && h > 0 ? w * h : 0;
}

function setFont(ctx, f) {
    ctx.font = f.font;
    ctx.letterSpacing = `${f.sp}px`;
}

// write puts a line of text on the canvas cut out of what lies under it: the
// halo erases the orbits and the light under the letters rather than painting
// a background of its own, so the text stands clear on any theme.
function write(ctx, text, f, colour, x, y) {
    setFont(ctx, f);
    ctx.textBaseline = "top";
    ctx.save();
    ctx.globalCompositeOperation = "destination-out";
    ctx.lineJoin = "round";
    ctx.strokeStyle = "#000";
    ctx.lineWidth = f.halo;
    ctx.strokeText(text, x, y);
    ctx.restore();
    ctx.fillStyle = colour;
    ctx.fillText(text, x, y);
    ctx.letterSpacing = "0px";
}

// The sides a name may stand on round its mark: the eight points of the
// compass, fixed on the screen, at five distances. A side kept from one frame
// to the next is the same side however the scene turns or moves.
const SIDES = Array.from({ length: 8 }, (_, i) => [Math.cos((i * Math.PI) / 4), -Math.sin((i * Math.PI) / 4)]);

// A name keeps its side while it covers no more than TOUCH of what is drawn
// and named (in the weighted area clash counts), and leaves it only for a side
// that clashes less than SWITCH of what it does now: a side a little better is
// not worth the name jumping across its mark.
export const TOUCH = 32;
const SWITCH = 0.5;

// clash is how much a name at rect covers: the edges of the canvas, the names
// placed before it, the marks.
function clash(rect, W, H, taken, discs) {
    let c = 0;
    if (rect.x < 3 || rect.y < 3 || rect.x + rect.w > W - 3 || rect.y + rect.h > H - 3) c += 1e6;
    for (const t of taken) c += overlap(rect, t) * 8;
    for (const q of discs) c += overlap(rect, { x: q.x - q.r, y: q.y - q.r, w: q.r * 2, h: q.r * 2 }) * 5;
    return c;
}

// tags places the names, the most wanted first, each beside its mark or on a
// hairline leader further off; a quiet one only where it stands clear. sides keeps where every name stood the frame
// before: a name stays there while it covers nothing, and the side is chosen
// anew only for a new name or one its side has come to cover — outward from
// the star of its system first, or in the order of the sides an item prefers.
// Where nothing turns (settle false: the scene only moved or grew) no name
// changes its side at all, and the names ride with their marks. It answers
// every name with the side it took and how much that covers, and, where it
// left one, the side it left and how much that side covered.
function tags(ctx, W, H, items, taken, discs, leader, u, sides, settle) {
    const pad = TAG_PAD.map((v) => v * u);
    const reach = REACH.map((v) => v * u);
    const labels = [];
    items.sort((a, b) => b.pri - a.pri);
    for (const it of items) {
        let w = 0;
        let h = 0;
        for (const l of it.lines) {
            setFont(ctx, l.f);
            l.w = ctx.measureText(l.t).width;
            w = Math.max(w, l.w);
            h += l.f.size * 1.3;
        }
        const bw = w + pad[1] * 2;
        const bh = h + pad[0] * 2;
        const rectOf = (side) => {
            const [dx, dy] = SIDES[side.dir];
            const px = it.x + dx * (it.r + reach[side.reach]);
            const py = it.y + dy * (it.r + reach[side.reach]);
            const x = dx >= 0.25 ? px : dx <= -0.25 ? px - bw : px - bw / 2;
            const y = dy >= 0.6 ? py : dy <= -0.6 ? py - bh : py - bh / 2;
            return { x, y, w: bw, h: bh };
        };
        const kept = sides.get(it.key) || null;
        let side = kept;
        let left = null;
        let covers = 0;
        if (!kept || settle) {
            const now = kept ? clash(rectOf(kept), W, H, taken, discs) : Infinity;
            covers = now;
            if (now > TOUCH) {
                const out = Math.atan2(-(it.y - it.from.y), it.x - it.from.x);
                let best = null;
                let cost = Infinity;
                for (let r = 0; r < reach.length; r++) {
                    for (let dir = 0; dir < SIDES.length; dir++) {
                        const turn = it.prefer
                            ? (it.prefer.includes(dir) ? it.prefer.indexOf(dir) : 6)
                            : Math.abs(Math.atan2(Math.sin(out - (dir * Math.PI) / 4), Math.cos(out - (dir * Math.PI) / 4))) / (Math.PI / 4);
                        const cand = { dir, reach: r };
                        const c = clash(rectOf(cand), W, H, taken, discs);
                        const score = c + turn * 3 + reach[r] * 0.6;
                        if (score < cost) {
                            cost = score;
                            best = { side: cand, clash: c };
                        }
                    }
                }
                if (!kept || best.clash < now * SWITCH) {
                    if (kept) left = { dir: kept.dir, reach: kept.reach, clash: now };
                    side = best.side;
                    covers = best.clash;
                }
            }
        }
        sides.set(it.key, side);
        const R = rectOf(side);
        // A quiet name — of a system behind the front one — is written only
        // where it stands clear; its mark keeps its tip.
        if (it.quiet && clash(R, W, H, taken, discs) > TOUCH) {
            labels.push({ key: it.key, dir: side.dir, reach: side.reach, rect: R, clash: covers, left, hidden: true });
            continue;
        }
        taken.push(R);
        labels.push({ key: it.key, dir: side.dir, reach: side.reach, rect: R, clash: covers, left });
        if (side.reach > 0) {
            const ex = clamp(it.x, R.x, R.x + R.w);
            const ey = clamp(it.y, R.y, R.y + R.h);
            const vx = ex - it.x;
            const vy = ey - it.y;
            const vl = Math.hypot(vx, vy) || 1;
            ctx.strokeStyle = it.leader || leader;
            ctx.lineWidth = 0.8;
            ctx.beginPath();
            ctx.moveTo(it.x + (vx / vl) * (it.r + 2 * u), it.y + (vy / vl) * (it.r + 2 * u));
            ctx.lineTo(ex, ey);
            ctx.stroke();
        }
        let y = R.y + pad[0];
        for (const l of it.lines) {
            write(ctx, l.t, l.f, l.c, R.x + pad[1], y + 0.5);
            y += l.f.size * 1.3;
        }
    }
    return labels;
}

function toneOf(n, pal) {
    return n.tone === "wait" ? pal.wait : n.tone === "busy" ? pal.busy : pal.ink;
}

// phase draws a planet lit in part, as the moon in its phase: the lit share
// of the disc is the share of the context the session has used, the lit side
// toward light (an angle on the screen). Near the cap the lit part takes the
// tone of the cap.
function phase(ctx, q, r, n, light, f, pal) {
    const k = clamp(n.pct / 100, 0, 1);
    const col = toneOf(n, pal);
    circle(ctx, q.x, q.y, r);
    ctx.fillStyle = rgba(col, (n.tone === "idle" ? 0.13 : 0.22) * f);
    ctx.fill();
    ctx.strokeStyle = rgba(col, (n.tone === "idle" ? 0.45 : 0.75) * f);
    ctx.lineWidth = 1;
    ctx.stroke();
    if (k < 0.01) return;
    // The lit part is a half disc and the half of an ellipse whose width is
    // |1 − 2k| of the disc: hollowed out of the half below half the context,
    // added to it above, so its area is the share k of the disc.
    ctx.save();
    ctx.translate(q.x, q.y);
    ctx.rotate(light);
    ctx.beginPath();
    ctx.arc(0, 0, r, -Math.PI / 2, Math.PI / 2, false);
    const rx = r * Math.abs(1 - 2 * k);
    if (k < 0.5) ctx.ellipse(0, 0, rx, r, 0, Math.PI / 2, -Math.PI / 2, true);
    else ctx.ellipse(0, 0, rx, r, 0, Math.PI / 2, Math.PI * 1.5, false);
    ctx.closePath();
    ctx.fillStyle = n.pct >= NEAR ? rgba(pal.cap, f) : rgba(col, (n.tone === "idle" ? 0.78 : 1) * f);
    ctx.fill();
    ctx.restore();
}

// The background work of a session is beads round its mark, one shape per
// kind: an agent a dot, a task a square, an observer a ring, an alarm a
// triangle.
function beads(ctx, q, n, r, f, pal, u) {
    n.work.forEach((kind, i) => {
        const a = Math.PI * 0.62 + i * 0.42;
        bead(ctx, kind, q.x + Math.cos(a) * r, q.y + Math.sin(a) * r, f, pal, u);
    });
}

function bead(ctx, kind, x, y, f, pal, u = 1) {
    ctx.fillStyle = rgba(pal.ink, 0.85 * f);
    ctx.strokeStyle = rgba(pal.ink, 0.85 * f);
    ctx.lineWidth = 1.3;
    if (kind === "agents") {
        circle(ctx, x, y, 2.4 * u);
        ctx.fill();
    } else if (kind === "tasks") {
        ctx.fillRect(x - 2.2 * u, y - 2.2 * u, 4.4 * u, 4.4 * u);
    } else if (kind === "observers") {
        circle(ctx, x, y, 2.6 * u);
        ctx.stroke();
    } else {
        ctx.beginPath();
        ctx.moveTo(x, y - 3.1 * u);
        ctx.lineTo(x + 2.9 * u, y + 2 * u);
        ctx.lineTo(x - 2.9 * u, y + 2 * u);
        ctx.closePath();
        ctx.fill();
    }
}

// busyGlow is the small steady light of a session at work: never the size of
// the light of a wait, and not a pulse — six at work would ripple the whole
// widget.
function busyGlow(ctx, q, r, u, pal) {
    const g = ctx.createRadialGradient(q.x, q.y, r * 0.6, q.x, q.y, r + BUSY_GLOW * u);
    g.addColorStop(0, rgba(pal.busy, 0.42));
    g.addColorStop(1, rgba(pal.busy, 0));
    ctx.fillStyle = g;
    circle(ctx, q.x, q.y, r + BUSY_GLOW * u);
    ctx.fill();
}

// starSize is the radius of a star by how alive its contour is: the sessions
// at work, and those waiting for the person twice. A quiet contour keeps a
// small star; a lively one is larger, and brighter.
export function starSize(live) {
    return 3 + 2.2 * Math.sqrt(live + 0.3);
}

// drawKey draws the mark the legend names kind on a square canvas of size,
// with the code the scene draws it with, so the legend cannot part from it.
// The square is sixteen units across, whatever the size, and the marks are
// drawn to the size of an icon rather than their size on the scene.
export function drawKey(ctx, kind, size, pal) {
    ctx.save();
    ctx.scale(size / 16, size / 16);
    const q = { x: 8, y: 8 };
    if (kind === "busy") {
        busyGlow(ctx, q, 3.4, 0.35, pal);
        phase(ctx, q, 3.6, { tone: "busy", pct: 60 }, Math.PI, 1, pal);
    } else if (kind === "idle") {
        phase(ctx, q, 3.6, { tone: "idle", pct: 60 }, Math.PI, 1, pal);
    } else if (kind === "wait") {
        phase(ctx, q, 3, { tone: "wait", pct: 60 }, Math.PI, 1, pal);
        circle(ctx, q.x, q.y, 6.5);
        ctx.strokeStyle = rgba(pal.wait, 0.8);
        ctx.lineWidth = 1.1;
        ctx.stroke();
    } else if (kind === "phase") {
        phase(ctx, q, 5.5, { tone: "idle", pct: 40 }, 0, 1, pal);
    } else if (kind === "work") {
        ["agents", "tasks", "observers"].forEach((k, i) => bead(ctx, k, 3 + i * 5, 8, 1, pal, 0.75));
    } else if (kind === "moons") {
        circle(ctx, 6.5, 9.5, 4.5);
        ctx.strokeStyle = rgba(pal.ink, 0.35);
        ctx.lineWidth = 1;
        ctx.stroke();
        circle(ctx, 10.4, 6.5, 1.9);
        ctx.fillStyle = rgba(pal.ink, 0.8);
        ctx.fill();
        circle(ctx, 6.5, 9.5, 2.2);
        ctx.fill();
    }
    ctx.restore();
}

// priority orders the names: the waiting first, wherever they are, then the
// system in front, then the busy, then those near the cap, planets before
// moons, the freshest before the quiet.
function priority(n, front) {
    return (n.tone === "wait" ? 4000 : 0) + (front ? 2000 : 0) + (n.tone === "busy" ? 300 : 0) + (n.pct >= NEAR ? 200 : 0)
        - n.p.depth * 50 - n.idle / 100;
}

// The sides the name of a star prefers, in order: under it, over it, under
// to the left, under to the right — off the orbits, which run to its sides.
const STAR_SIDES = [6, 2, 5, 7];

// drawScene draws the galaxy seen through pr on a canvas of W×H and answers
// where every planet and star stands on it, for the pointer to find (hits),
// where the words are (taken), for the tip to keep off, and where each name
// went (labels). scene holds the systems — each with its place, its hue, its
// orbits, its members and how alive it is — and the nodes, each carrying what
// it shows (name, tone, pct, work, note) and its place in its system (p), the
// floor under the stars, the system in front (front), the palette and the
// type, the sides the names stood on the frame before (sides, which it
// updates) and whether a name may change its side (settle).
export function drawScene(ctx, W, H, pr, scene) {
    const { systems, nodes, floor, pal, type } = scene;
    const u = type.unit;
    const taken = [];
    const discs = [];
    const items = [];
    const hits = [];
    const bySystem = new Map(systems.map((s) => [s.name, s]));
    // The names of the system in front are all written; those of the systems
    // behind it, smaller with distance, only where they stand clear, but for
    // a session waiting for the person.
    const behind = (name) => Boolean(scene.front) && name !== scene.front;
    const spot = new Map(nodes.map((n) => [n, pr(worldOf(bySystem.get(n.p.system).place, n.p.w))]));
    const zs = [...spot.values()].map((q) => q.z);
    const z0 = Math.min(...zs);
    const z1 = Math.max(...zs);
    // Depth is told by how faint a line is, never by how large a mark is.
    const fade = (z) => (z1 > z0 ? clamp(1 - ((z - z0) / (z1 - z0)) * 0.55, 0.45, 1) : 1);

    // The one wide light, behind the session that has waited for the person
    // longest.
    const waiting = nodes.filter((n) => n.tone === "wait").sort((a, b) => b.idle - a.idle)[0];
    if (waiting) {
        const q = spot.get(waiting);
        const reach = GLOW * u;
        const g = ctx.createRadialGradient(q.x, q.y, 0, q.x, q.y, reach);
        g.addColorStop(0, rgba(pal.wait, 0.2));
        g.addColorStop(1, rgba(pal.wait, 0));
        ctx.fillStyle = g;
        ctx.fillRect(q.x - reach, q.y - reach, reach * 2, reach * 2);
    }

    // The systems, the far ones first.
    const stars = new Map();
    const order = systems.map((s) => ({ s, c: pr(s.place.c) })).sort((a, b) => b.c.z - a.c.z);
    for (const { s, c: star } of order) {
        const plane = (p) => pr(worldOf(s.place, p));
        ctx.lineWidth = 0.8;
        // The stalk to the floor and its foot: how deep the system stands.
        const foot = [s.place.c[0], floor, s.place.c[2]];
        ctx.setLineDash([1, 4]);
        line(ctx, [star, pr(foot)], s.tint, () => 0.28);
        ctx.setLineDash([]);
        const ring = [];
        for (let i = 0; i <= 32; i++) {
            ring.push(pr([foot[0] + Math.cos((i / 32) * TAU) * 22, floor, foot[2] + Math.sin((i / 32) * TAU) * 22]));
        }
        line(ctx, ring, s.tint, () => 0.28);
        // The orbits, each in its lean.
        const orbit = (points, centre, colour) => line(ctx, points, colour, (a, b) => ((a.z + b.z) / 2 < centre.z ? ORBIT[1] : ORBIT[0]));
        for (const o of s.orbits) {
            const points = [];
            for (let i = 0; i <= 120; i++) points.push(plane(onOrbit(o, (i / 120) * TAU)));
            orbit(points, star, s.tint);
        }
        // The orbits of the moons.
        for (const n of s.members) {
            if (n.kids.length === 0) continue;
            const points = [];
            for (let i = 0; i <= 40; i++) points.push(plane(add(n.w, inclined(n.lean, at((i / 40) * TAU, n.orbit)))));
            orbit(points, plane(n.w), pal.ink);
        }
        // The star: larger and brighter the more alive its contour is.
        const sr = starSize(s.live) * u;
        if (s.live > 0) {
            const g = ctx.createRadialGradient(star.x, star.y, 0, star.x, star.y, sr * 6);
            g.addColorStop(0, rgba(s.tint, 0.1 + 0.05 * Math.min(s.live, 5)));
            g.addColorStop(1, rgba(s.tint, 0));
            ctx.fillStyle = g;
            ctx.fillRect(star.x - sr * 6, star.y - sr * 6, sr * 12, sr * 12);
        }
        circle(ctx, star.x, star.y, sr);
        ctx.fillStyle = s.tint;
        ctx.fill();
        circle(ctx, star.x, star.y, sr + 5 * u);
        ctx.strokeStyle = rgba(s.tint, 0.45);
        ctx.lineWidth = 1;
        ctx.stroke();
        stars.set(s.name, star);
        discs.push({ x: star.x, y: star.y, r: sr + 6 * u });
        hits.push({ x: star.x, y: star.y, r: sr + 10 * u, star: s });
        items.push({
            key: `*${s.name}`, x: star.x, y: star.y, r: sr + 6 * u, pri: 5000, prefer: STAR_SIDES, from: star,
            lines: [{ t: s.name, f: type.star, c: s.tint }, { t: s.summary, f: type.tag, c: rgba(pal.ink, 0.5) }],
        });
        // An orbit says its project only where its planets do not: outside
        // the map, and a project whose sessions are named otherwise.
        for (const o of s.orbits) {
            if (!o.label) continue;
            const q = plane(onOrbit(o, ORBIT_NAME_AT));
            items.push({ key: `${s.name}/${o.key}`, x: q.x, y: q.y, r: 2 * u, pri: -100, from: star, quiet: behind(s.name),
                lines: [{ t: o.name, f: type.tag, c: rgba(s.tint, 0.75) }] });
        }
    }

    // The planets and the moons, the far ones first.
    const far = nodes.slice().sort((a, b) => spot.get(b).z - spot.get(a).z);
    for (const n of far) {
        const q = spot.get(n);
        const f = fade(q.z);
        const r = (n.p.depth > 0 ? MOON_R : NODE_R) * u;
        const star = stars.get(n.p.system);
        const light = Math.atan2(star.y - q.y, star.x - q.x);
        if (n.tone === "busy") busyGlow(ctx, q, r, u, pal);
        phase(ctx, q, r, n, light, f, pal);
        if (n.tone === "wait") {
            circle(ctx, q.x, q.y, r + WAIT_RING * u);
            ctx.strokeStyle = rgba(pal.wait, 0.8);
            ctx.lineWidth = 1.3;
            ctx.stroke();
        }
        beads(ctx, q, n, r + BEADS * u, f, pal, u);
        const d = { x: q.x, y: q.y, r: r + (n.tone === "wait" ? WAIT_RING + 1 : 6) * u };
        discs.push(d);
        hits.push({ x: q.x, y: q.y, r: d.r + 3 * u, node: n });
        items.push({
            key: n.s.session, x: q.x, y: q.y, r: d.r, pri: priority(n, !behind(n.p.system)), from: star,
            quiet: behind(n.p.system) && n.tone !== "wait",
            leader: n.tone === "wait" ? rgba(pal.wait, 0.8) : null,
            lines: linesOf(n, pal, type),
        });
    }

    const labels = tags(ctx, W, H, items, taken, discs, rgba(pal.ink, 0.3), u, scene.sides, scene.settle);
    return { hits, taken, labels };
}

// linesOf is what the frame says beside a node: its name, and a number only
// where it asks for the person — the wait, or the context near the cap.
function linesOf(n, pal, type) {
    const colour = n.tone === "wait" ? pal.wait : n.tone === "busy" ? pal.busy : rgba(pal.ink, 0.62);
    const lines = [{ t: n.name, f: type.tag, c: colour }];
    if (n.tone === "wait") lines.push({ t: n.note, f: type.tag, c: rgba(pal.wait, 0.9) });
    else if (n.pct >= NEAR) lines.push({ t: `${Math.round(n.pct)}% of context`, f: type.tag, c: pal.cap });
    return lines;
}
