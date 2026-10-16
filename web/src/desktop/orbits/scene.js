// The drawing of the galaxy of sessions on a canvas. Every contour is a system
// in a hue of its own: a star with a dense core and a soft corona, the orbits
// of its projects as hairlines barely there, a sparse field of far stars
// behind on the dark theme. A session is a planet lit by its star, as a moon
// in its phase: the side toward the star is lit, the lit share is how much of
// its context it has used, its surface its own, its night side a little lit by
// the sky. The marks keep their colours of state — the accent of a session at
// work in a small steady glow, the gold of a session waiting for the person,
// the top of the context scale near the cap — and the one wide light stands
// behind the session that has waited longest. What stands further back is
// fainter and hazier.
import { TAU, at, add, hashOf, inclined, onOrbit, worldOf } from "./space.js";

// NEAR is where the lit part takes the tone of the cap: a session past it is
// a turn or two of a long task from the cap.
const NEAR = 62;

// Sizes in units of the scene: a unit is a pixel at the scale the interface
// opens on, and grows with it as the type does. The orbits stay hairlines at
// any scale.
const NODE_R = 9.5;
const MOON_R = 5;
const BUSY_GLOW = 13;
const WAIT_RING = 12;
const BEADS = 9;
export const HALO = 6;
const GLOW = 150;
const REACH = [8, 22, 40, 62, 90];

// BEHIND is how large the marks of a system behind the front one are drawn:
// its orbits shrink there, and planets of full size would crowd each other
// and its star. The share its orbits shrink by would leave them dots, so the
// marks stop at half; a system on its way forward or back passes between.
const BEHIND = 0.5;

// The orbits are faint: ORBIT[1] on the half of an orbit nearer the eye,
// ORBIT[0] on the far half — enough to follow an orbit round and see which
// way it leans, never a line that rivals a mark.
export const ORBIT = [0.11, 0.18];

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
// hairline leader further off; a quiet one is not written, and its mark keeps
// its tip. sides keeps where every name stood the frame before: a name stays
// there while it covers nothing, and the side is chosen anew only for a new
// name or one its side has come to cover — outward from the star of its system
// first, or in the order of the sides an item prefers. Where nothing turns
// (settle false: the scene only moved or grew) no name changes its side at
// all, and the names ride with their marks. It answers every name with the
// side it took and how much that covers, and, where it left one, the side it
// left and how much that side covered; a quiet one only as hidden.
function tags(ctx, W, H, items, taken, discs, leader, u, sides, settle) {
    const pad = TAG_PAD.map((v) => v * u);
    const reach = REACH.map((v) => v * u);
    const labels = [];
    items.sort((a, b) => b.pri - a.pri);
    for (const it of items) {
        if (it.quiet) {
            labels.push({ key: it.key, hidden: true });
            continue;
        }
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

// A planet is drawn from a picture made once and kept: its surface from its
// name, lit from the right by the share k of its context, its night side the
// sky behind seen through NIGHT of its own light, darker toward its edge,
// with a thin rim of air on the lit limb and no gloss. Each frame turns the
// picture toward its star; a new one is made only for a new size, state or
// share. The lit part is where light falls on the sphere, so its area is the
// share k exactly, the terminator soft by SOFT either side.
const SPRITES = new Map();
const SPRITES_KEPT = 400;
let made = 0;
const SOFT = 0.1;
const NIGHT = 0.08;
const NIGHT_SKY = 0.3;
const GREY = [150, 152, 158];

// spritesMade is how many pictures of planets have been made so far.
export function spritesMade() {
    return made;
}

function rgbOf(hex) {
    const h = hex.replace("#", "");
    return h.length === 3 ? h.split("").map((c) => parseInt(c + c, 16)) : [0, 2, 4].map((i) => parseInt(h.slice(i, i + 2), 16));
}

function smooth(a, b, x) {
    const t = clamp((x - a) / (b - a), 0, 1);
    return t * t * (3 - 2 * t);
}

// surfaceOf is how light the surface of a planet is at a point of its disc,
// from its name: the bands of a gas giant or the spots of a mottled one, and
// for a moon the craters of rock.
function surfaceOf(key, rocky) {
    const h = (salt) => hashOf(key, 10 + salt);
    if (rocky) {
        const craters = Array.from({ length: 4 + Math.floor(h(0) * 4) }, (_, i) => ({
            x: h(1 + i * 3) * 1.6 - 0.8, y: h(2 + i * 3) * 1.6 - 0.8, r: 0.14 + h(3 + i * 3) * 0.22,
        }));
        const grain = h(40) * TAU;
        return (x, y) => {
            let a = 0.96 + 0.05 * Math.sin(9 * x + 7 * y + grain);
            for (const c of craters) {
                const dd = Math.hypot(x - c.x, y - c.y) / c.r;
                if (dd < 1) a -= 0.18 * (1 - dd * dd);
                else if (dd < 1.3) a += 0.08 * (1.3 - dd);
            }
            return a;
        };
    }
    if (h(0) < 0.65) {
        const f1 = 6 + h(1) * 5;
        const f2 = 15 + h(2) * 8;
        const p1 = h(3) * TAU;
        const p2 = h(4) * TAU;
        const p3 = h(5) * TAU;
        const a1 = 0.09 + h(6) * 0.07;
        const a2 = 0.04 + h(7) * 0.04;
        const wob = 0.05 + h(8) * 0.08;
        return (x, y) => {
            const yy = y + wob * Math.sin(3.2 * x + p3);
            return 1 + a1 * Math.sin(f1 * yy + p1) + a2 * Math.sin(f2 * yy + p2);
        };
    }
    const spots = Array.from({ length: 3 + Math.floor(h(1) * 3) }, (_, i) => ({
        x: h(2 + i * 4) * 1.4 - 0.7, y: h(3 + i * 4) * 1.4 - 0.7, r: 0.15 + h(4 + i * 4) * 0.25,
        d: (h(5 + i * 4) < 0.5 ? -1 : 1) * (0.08 + h(6 + i * 4) * 0.1),
    }));
    const grain = h(40) * TAU;
    return (x, y) => {
        let a = 1 + 0.04 * Math.sin(5 * x + 4 * y + grain);
        for (const sp of spots) {
            const dd = Math.hypot(x - sp.x, y - sp.y) / sp.r;
            if (dd < 1) a += sp.d * (1 - dd * dd) ** 1.5;
        }
        return a;
    };
}

// lookOf is what the picture of a node is made of: the colour of its state
// (the cap's on the lit part near the cap), a moon greyer, an idle one a
// little dimmer, and the sky its night side shows.
function lookOf(n, moon, pal) {
    const near = n.pct >= NEAR;
    let rgb = rgbOf(near ? pal.cap : toneOf(n, pal));
    if (moon) rgb = rgb.map((v, i) => v * 0.4 + GREY[i] * 0.6);
    const sky = rgbOf(pal.void || "#06070f");
    const night = pal.sky ? NIGHT_SKY : NIGHT;
    return {
        k: clamp(Math.round(n.pct) / 100, 0, 1),
        rgb,
        day: n.tone === "idle" ? 0.9 : 1,
        sky: sky.map((v) => v * (1 - night)),
        night,
        rim: rgb.map((v) => v + (255 - v) * 0.5),
        rocky: moon,
        id: `${n.tone}|${near ? "cap" : ""}|${Math.round(n.pct)}|${moon ? "moon" : ""}|${pal.void}|${rgb.join(",")}`,
    };
}

function spriteOf(key, r, scale, look) {
    const id = `${key}|${Math.round(r * scale * 10)}|${look.id}`;
    const kept = SPRITES.get(id);
    if (kept) return kept;
    const R = r * scale;
    const size = Math.ceil(2 * (R + 1.5));
    const cv = typeof OffscreenCanvas === "function" ? new OffscreenCanvas(size, size)
        : Object.assign(document.createElement("canvas"), { width: size, height: size });
    const g = cv.getContext("2d");
    const img = g.createImageData(size, size);
    const d = img.data;
    const c = size / 2;
    const cosT = 2 * look.k - 1;
    const sinT = Math.sqrt(Math.max(0, 1 - cosT * cosT));
    const surface = surfaceOf(key, look.rocky);
    for (let py = 0; py < size; py++) {
        for (let px = 0; px < size; px++) {
            const x = (px + 0.5 - c) / R;
            const y = (py + 0.5 - c) / R;
            const rho = Math.hypot(x, y);
            const edge = clamp((1 - rho) * R + 0.5, 0, 1);
            if (edge <= 0) continue;
            const z = Math.sqrt(Math.max(0, 1 - rho * rho));
            const sun = x * sinT + z * cosT;
            const lit = smooth(-SOFT, SOFT, sun);
            const albedo = surface(x, y, z);
            const day = look.day * albedo * (0.7 + 0.3 * Math.max(0, sun) ** 0.6) * (0.78 + 0.22 * z);
            const air = rho > 1 - 2 / R ? smooth(1 - 2 / R, 1, rho) * Math.max(0, x / (rho || 1)) * lit * 0.55 : 0;
            const i = (py * size + px) * 4;
            for (let ch = 0; ch < 3; ch++) {
                const lightC = look.rgb[ch] * day;
                const nightC = look.sky[ch] + look.rgb[ch] * albedo * look.night * (0.6 + 0.4 * z);
                const v = nightC + (lightC - nightC) * lit;
                d[i + ch] = clamp(Math.round(v + (look.rim[ch] - v) * air), 0, 255);
            }
            d[i + 3] = Math.round(255 * edge);
        }
    }
    g.putImageData(img, 0, 0);
    made += 1;
    SPRITES.set(id, cv);
    if (SPRITES.size > SPRITES_KEPT) SPRITES.delete(SPRITES.keys().next().value);
    return cv;
}

// planet draws a node at q, of radius r, lit from the angle light on the
// screen, at the opacity f its depth leaves it, behind a haze as far as it
// stands back.
function planet(ctx, q, r, n, key, moon, light, f, pal) {
    const scale = ctx.getTransform().a || 1;
    const sprite = spriteOf(key, r, scale, lookOf(n, moon, pal));
    const half = sprite.width / scale / 2;
    ctx.save();
    ctx.translate(q.x, q.y);
    ctx.rotate(light);
    ctx.globalAlpha *= f;
    ctx.drawImage(sprite, -half, -half, half * 2, half * 2);
    ctx.restore();
    if (f < 1) {
        circle(ctx, q.x, q.y, r);
        ctx.fillStyle = rgba(pal.void || "#06070f", (1 - f) * 0.45);
        ctx.fill();
    }
}

// starField is the sparse field of far stars behind the scene on the dark
// theme — one star to some seven thousand pixels — the same every time,
// moving a little as the scene moves and turns.
function starField(ctx, W, H, view, pan, pal) {
    const T = Math.max(W, H) * 1.4;
    const count = Math.round((T * T) / 7000);
    const ox = view.yaw * 60 + pan.x * 0.08;
    const oy = view.pitch * 60 + pan.y * 0.08;
    for (let i = 0; i < count; i++) {
        const x = ((((hashOf(`far${i}`, 1) * T + ox) % T) + T) % T) - (T - W) / 2;
        const y = ((((hashOf(`far${i}`, 2) * T + oy) % T) + T) % T) - (T - H) / 2;
        if (x < 0 || y < 0 || x > W || y > H) continue;
        const size = 0.5 + hashOf(`far${i}`, 3) * 0.8;
        ctx.fillStyle = rgba(pal.ink, 0.12 + hashOf(`far${i}`, 4) * 0.38);
        ctx.fillRect(x - size / 2, y - size / 2, size, size);
    }
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
        planet(ctx, q, 3.6, { tone: "busy", pct: 60 }, "key/busy", false, Math.PI, 1, pal);
    } else if (kind === "idle") {
        planet(ctx, q, 3.6, { tone: "idle", pct: 60 }, "key/idle", false, Math.PI, 1, pal);
    } else if (kind === "wait") {
        planet(ctx, q, 3, { tone: "wait", pct: 60 }, "key/wait", false, Math.PI, 1, pal);
        circle(ctx, q.x, q.y, 6.5);
        ctx.strokeStyle = rgba(pal.wait, 0.8);
        ctx.lineWidth = 1.1;
        ctx.stroke();
    } else if (kind === "phase") {
        planet(ctx, q, 5.5, { tone: "idle", pct: 40 }, "key/phase", false, 0, 1, pal);
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
// it shows (name, tone, pct, work, note) and its place in its system (p),
// the system in front (front), the palette and the type, the sides the names
// stood on the frame before (sides, which it updates), whether a name may
// change its side (settle), and the view and pan the far stars move with.
export function drawScene(ctx, W, H, pr, scene) {
    const { systems, nodes, pal, type } = scene;
    const u = type.unit;
    const taken = [];
    const discs = [];
    const items = [];
    const hits = [];
    const areas = [];
    const bySystem = new Map(systems.map((s) => [s.name, s]));
    // The names of the system in front are all written; a system behind it is
    // one target, and says only its own name and that of a session waiting
    // for the person.
    const behind = (name) => Boolean(scene.front) && name !== scene.front;
    const R = Math.max(...systems.map((s) => s.R));
    const marks = new Map(systems.map((s) => [s.name, markOf(s, R)]));
    const spot = new Map(nodes.map((n) => [n, pr(worldOf(bySystem.get(n.p.system).place, n.p.w))]));
    const zs = [...spot.values()].map((q) => q.z);
    const z0 = Math.min(...zs);
    const z1 = Math.max(...zs);
    // Depth within the scene is told by how faint a line is; a mark is smaller
    // only for standing in a system behind the front one.
    const fade = (z) => (z1 > z0 ? clamp(1 - ((z - z0) / (z1 - z0)) * 0.55, 0.45, 1) : 1);

    // The far stars, on the dark theme only.
    if (!pal.sky) starField(ctx, W, H, scene.view || { yaw: 0, pitch: 0 }, scene.pan || { x: 0, y: 0 }, pal);

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
        // The orbits, each in its lean.
        const orbit = (points, centre, colour) => line(ctx, points, colour, (a, b) => ((a.z + b.z) / 2 < centre.z ? ORBIT[1] : ORBIT[0]));
        // The box the orbits take on the stage: a click there, off every
        // mark, is a click on the system.
        const box = { x0: star.x, y0: star.y, x1: star.x, y1: star.y, cx: star.x, cy: star.y, star: s };
        for (const o of s.orbits) {
            const points = [];
            for (let i = 0; i <= 120; i++) points.push(plane(onOrbit(o, (i / 120) * TAU)));
            orbit(points, star, s.tint);
            for (const q of points) {
                box.x0 = Math.min(box.x0, q.x);
                box.y0 = Math.min(box.y0, q.y);
                box.x1 = Math.max(box.x1, q.x);
                box.y1 = Math.max(box.y1, q.y);
            }
        }
        areas.push(box);
        // The orbits of the moons.
        for (const n of s.members) {
            if (n.kids.length === 0) continue;
            const points = [];
            for (let i = 0; i <= 40; i++) points.push(plane(add(n.w, inclined(n.lean, at((i / 40) * TAU, n.orbit)))));
            orbit(points, plane(n.w), pal.ink);
        }
        // The star: a dense bright core in its hue, a corona that fades
        // softly and a little bloom, larger and brighter the more alive its
        // contour is, fainter the further back it stands.
        const sr = starSize(s.live) * u;
        const glow = (0.3 + 0.07 * Math.min(s.live, 5)) * (pal.sky ? 0.6 : 1) * fade(star.z);
        const bloom = ctx.createRadialGradient(star.x, star.y, sr * 0.5, star.x, star.y, sr * 10);
        bloom.addColorStop(0, rgba(s.tint, glow * 0.5));
        bloom.addColorStop(0.25, rgba(s.tint, glow * 0.16));
        bloom.addColorStop(1, rgba(s.tint, 0));
        ctx.fillStyle = bloom;
        circle(ctx, star.x, star.y, sr * 10);
        ctx.fill();
        const tint = rgbOf(s.tint);
        const core = ctx.createRadialGradient(star.x, star.y, 0, star.x, star.y, sr * 1.15);
        const white = pal.sky ? 0.3 : 0.75;
        core.addColorStop(0, `rgb(${tint.map((v) => Math.round(v + (255 - v) * white)).join(",")})`);
        core.addColorStop(0.45, s.tint);
        core.addColorStop(0.85, rgba(s.tint, 0.9));
        core.addColorStop(1, rgba(s.tint, 0));
        ctx.fillStyle = core;
        circle(ctx, star.x, star.y, sr * 1.15);
        ctx.fill();
        stars.set(s.name, star);
        discs.push({ x: star.x, y: star.y, r: sr + 6 * u });
        hits.push({ x: star.x, y: star.y, r: sr + 10 * u, star: s });
        items.push({
            key: `*${s.name}`, x: star.x, y: star.y, r: sr + 6 * u, pri: 5000, prefer: STAR_SIDES, from: star,
            lines: [{ t: s.name, f: type.star, c: s.tint }],
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
        const m = u * marks.get(n.p.system);
        const r = (n.p.depth > 0 ? MOON_R : NODE_R) * m;
        const star = stars.get(n.p.system);
        const light = Math.atan2(star.y - q.y, star.x - q.x);
        if (n.tone === "busy") busyGlow(ctx, q, r, m, pal);
        planet(ctx, q, r, n, n.s.session, n.p.depth > 0, light, f, pal);
        if (n.tone === "wait") {
            circle(ctx, q.x, q.y, r + WAIT_RING * m);
            ctx.strokeStyle = rgba(pal.wait, 0.8);
            ctx.lineWidth = 1.3;
            ctx.stroke();
        }
        beads(ctx, q, n, r + BEADS * m, f, pal, m);
        const d = { x: q.x, y: q.y, r: r + (n.tone === "wait" ? WAIT_RING + 1 : 6) * m };
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
    return { hits, taken, labels, areas };
}

// markOf is the share of their size the marks of a system are drawn at: whole
// in front, BEHIND behind, and between on the way, by how large the system is
// drawn against its size in front (k, which in front is R over its own
// radius). Kept to twentieths, so a system on its way makes a few pictures of
// its planets rather than one a frame.
function markOf(s, R) {
    const k = s.place.k || R / s.R;
    return clamp(Math.round(((k * s.R) / R) * 20) / 20, BEHIND, 1);
}

// linesOf is what the frame says beside a node: its name, and under it what
// the session asks the person while it waits.
function linesOf(n, pal, type) {
    const colour = n.tone === "wait" ? pal.wait : n.tone === "busy" ? pal.busy : rgba(pal.ink, 0.62);
    const lines = [{ t: n.name, f: type.tag, c: colour }];
    if (n.tone === "wait") lines.push({ t: n.note, f: type.tag, c: rgba(pal.wait, 0.9) });
    return lines;
}
