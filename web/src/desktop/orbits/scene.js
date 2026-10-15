// The drawing of the session graph on a canvas: an engraving on the empty
// page. The rings are hairlines, the sessions are exact small marks, the
// type is quiet but for the scale and the host, which are large and thin.
// One warm colour — the session waiting for the person — and one soft light
// behind the session that needs the person most.
import { CAP_DEFAULT as CAP } from "../../screens/profiles/launch.js";
import { DAY, SPAN, TAU, add, at, dial } from "./space.js";

// NEAR is where the context arc lights up and the frame says the number: a
// session past it is a turn or two of a long task from the cap.
const NEAR = 62;

// The marks of the scale the frame names; the rest of the dial is read by
// the angle.
const SCALE = [[60, "an hour"], [DAY, "a day"]];

// Sizes of the marks in units of the scene: a unit is a pixel at the scale
// the interface opens on, and grows with it as the type does. A mark is large
// enough to aim the pointer at and to read its arc without leaning in; the
// rings stay hairlines at any scale.
const NODE_R = 5;
const MOON_R = NODE_R * 0.75;
const ARC_GAP = 7;
const BEADS = 8;
export const HALO = 6;
const FAR = 0.35;
const GLOW = 170;
const REACH = [12, 28, 46];
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

function seg(ctx, a, b) {
    ctx.moveTo(a.x, a.y);
    ctx.lineTo(b.x, b.y);
}

function circle(ctx, x, y, r) {
    ctx.beginPath();
    ctx.arc(x, y, Math.max(0.3, r), 0, TAU);
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
// halo erases the rings and the light under the letters rather than painting
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

// caption is a fixed line: centred on x, or from it, and kept as taken.
function caption(ctx, text, f, colour, x, y, align, taken) {
    setFont(ctx, f);
    const w = ctx.measureText(text).width;
    const x0 = align === "c" ? x - w / 2 : x;
    write(ctx, text, f, colour, x0, y);
    taken.push({ x: x0 - 3, y: y - 2, w: w + 6, h: f.size * 1.3 + 3 });
}

// The sides a name may stand on round its mark: the eight points of the
// compass, fixed on the screen, at three distances. A side kept from one frame
// to the next is the same side however the scene turns or moves.
const SIDES = Array.from({ length: 8 }, (_, i) => [Math.cos((i * Math.PI) / 4), -Math.sin((i * Math.PI) / 4)]);

// A name keeps its side while it covers no more than TOUCH of what is drawn
// and named (in the weighted area clash counts), and leaves it only for a side
// that clashes less than SWITCH of what it does now: a side a little better is
// not worth the name jumping across its mark.
export const TOUCH = 32;
const SWITCH = 0.5;

// clash is how much a name at rect covers: the edges of the canvas, the names
// and captions placed before it, the marks.
function clash(rect, W, H, taken, discs) {
    let c = 0;
    if (rect.x < 3 || rect.y < 3 || rect.x + rect.w > W - 3 || rect.y + rect.h > H - 3) c += 1e6;
    for (const t of taken) c += overlap(rect, t) * 8;
    for (const q of discs) c += overlap(rect, { x: q.x - q.r, y: q.y - q.r, w: q.r * 2, h: q.r * 2 }) * 5;
    return c;
}

// tags places the names of the nodes, the most wanted first, each beside its
// node or on a hairline leader further off. sides keeps where every name stood
// the frame before: a name stays there while it covers nothing, and the side
// is chosen anew — outward from the host first — only for a new name or one
// its side has come to cover. Where nothing turns (settle false: the scene
// only moved or grew) no name changes its side at all, and the names ride
// with their marks. It answers every name with the side it took and how much
// that covers, and, where it left one, the side it left and how much that
// side covered.
function tags(ctx, W, H, items, taken, discs, core, leader, u, sides, settle) {
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
                // Outward from the host is where a name reads as its mark's.
                const out = Math.atan2(-(it.y - core.y), it.x - core.x);
                let best = null;
                let cost = Infinity;
                for (let r = 0; r < reach.length; r++) {
                    for (let dir = 0; dir < SIDES.length; dir++) {
                        const turn = Math.abs(Math.atan2(Math.sin(out - (dir * Math.PI) / 4), Math.cos(out - (dir * Math.PI) / 4)));
                        const cand = { dir, reach: r };
                        const c = clash(rectOf(cand), W, H, taken, discs);
                        const score = c + (turn / (Math.PI / 4)) * 3 + reach[r] * 0.6;
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

// The context of a session is a thin arc round its mark, from the top
// clockwise, with a tick at the cap.
function contextArc(ctx, q, n, r, f, pal, u = 1) {
    const a0 = -Math.PI / 2;
    const near = n.pct >= NEAR;
    ctx.lineCap = "butt";
    ctx.strokeStyle = rgba(pal.ink, 0.12 * f);
    ctx.lineWidth = 1.2;
    circle(ctx, q.x, q.y, r);
    ctx.stroke();
    ctx.strokeStyle = near ? rgba(pal.cap, f) : rgba(pal.ink, (n.pct >= 50 ? 0.8 : 0.45) * f);
    ctx.lineWidth = near ? 2.8 : 2;
    ctx.beginPath();
    ctx.arc(q.x, q.y, r, a0, a0 + (TAU * Math.min(100, n.pct)) / 100);
    ctx.stroke();
    const c = a0 + (TAU * CAP) / 100;
    ctx.strokeStyle = rgba(near ? pal.cap : pal.ink, (near ? 1 : 0.7) * f);
    ctx.lineWidth = 1.4;
    ctx.beginPath();
    ctx.moveTo(q.x + Math.cos(c) * (r - 3.5 * u), q.y + Math.sin(c) * (r - 3.5 * u));
    ctx.lineTo(q.x + Math.cos(c) * (r + 4.5 * u), q.y + Math.sin(c) * (r + 4.5 * u));
    ctx.stroke();
}

// The background work of a session is beads on a ring round its mark, one
// shape per kind: an agent a dot, a task a square, an observer a ring, an
// alarm a triangle.
function beads(ctx, q, n, r, f, pal, u) {
    n.work.forEach((kind, i) => {
        const a = Math.PI * 0.62 + i * 0.4;
        bead(ctx, kind, q.x + Math.cos(a) * r, q.y + Math.sin(a) * r, f, pal, u);
    });
}

function bead(ctx, kind, x, y, f, pal, u = 1) {
    ctx.fillStyle = rgba(pal.ink, 0.85 * f);
    ctx.strokeStyle = rgba(pal.ink, 0.85 * f);
    ctx.lineWidth = 1.3;
    if (kind === "agents") {
        circle(ctx, x, y, 2.6 * u);
        ctx.fill();
    } else if (kind === "tasks") {
        ctx.fillRect(x - 2.4 * u, y - 2.4 * u, 4.8 * u, 4.8 * u);
    } else if (kind === "observers") {
        circle(ctx, x, y, 2.8 * u);
        ctx.stroke();
    } else {
        ctx.beginPath();
        ctx.moveTo(x, y - 3.4 * u);
        ctx.lineTo(x + 3.1 * u, y + 2.2 * u);
        ctx.lineTo(x - 3.1 * u, y + 2.2 * u);
        ctx.closePath();
        ctx.fill();
    }
}

function mark(ctx, q, n, r, f, pal, u = 1) {
    if (n.tone === "wait") {
        circle(ctx, q.x, q.y, r + 0.8 * u);
        ctx.fillStyle = pal.wait;
        ctx.fill();
        circle(ctx, q.x, q.y, r + 15 * u);
        ctx.strokeStyle = rgba(pal.wait, 0.75);
        ctx.lineWidth = 1.2;
        ctx.stroke();
        return;
    }
    if (n.tone === "busy") {
        circle(ctx, q.x, q.y, r + 0.4 * u);
        ctx.fillStyle = pal.busy;
        ctx.fill();
        return;
    }
    circle(ctx, q.x, q.y, r - 0.6 * u);
    ctx.fillStyle = rgba(pal.ink, 0.55 * f);
    ctx.fill();
}

// drawKey draws the mark the legend names kind on a square canvas of size,
// with the code the scene draws it with, so the legend cannot part from it.
// The square is sixteen units across, whatever the size, and the marks are
// drawn to the size of an icon rather than their size on the scene.
export function drawKey(ctx, kind, size, pal) {
    ctx.save();
    ctx.scale(size / 16, size / 16);
    const q = { x: 8, y: 8 };
    if (kind === "busy" || kind === "idle") {
        mark(ctx, q, { tone: kind }, 3.4, 1, pal);
    } else if (kind === "wait") {
        // The ring of the scene stands further out; here it is drawn in.
        circle(ctx, q.x, q.y, 2.6);
        ctx.fillStyle = pal.wait;
        ctx.fill();
        circle(ctx, q.x, q.y, 6);
        ctx.strokeStyle = rgba(pal.wait, 0.8);
        ctx.lineWidth = 0.9;
        ctx.stroke();
    } else if (kind === "angle") {
        ctx.strokeStyle = rgba(pal.ink, 0.6);
        ctx.lineWidth = 1;
        ctx.setLineDash([2, 2]);
        ctx.beginPath();
        ctx.moveTo(8, 14);
        ctx.lineTo(8, 2);
        ctx.stroke();
        ctx.setLineDash([]);
        ctx.strokeStyle = pal.ink;
        ctx.beginPath();
        ctx.arc(8, 8, 5.5, Math.PI / 2, Math.PI * 1.05);
        ctx.stroke();
    } else if (kind === "context") {
        contextArc(ctx, q, { pct: 66 }, 5.2, 1, pal, 0.6);
    } else if (kind === "work") {
        ["agents", "tasks", "observers"].forEach((k, i) => bead(ctx, k, 3 + i * 5, 8, 1, pal, 0.65));
    } else if (kind === "moons") {
        circle(ctx, 6.5, 9.5, 4.5);
        ctx.strokeStyle = rgba(pal.ink, 0.35);
        ctx.lineWidth = 1;
        ctx.stroke();
        circle(ctx, 10.4, 6.5, 1.9);
        ctx.fillStyle = rgba(pal.ink, 0.8);
        ctx.fill();
        circle(ctx, 6.5, 9.5, 2);
        ctx.fill();
    }
    ctx.restore();
}

// priority orders the names: the waiting first, then the busy, then those
// near the cap, the freshest before the quiet.
function priority(n) {
    return (n.tone === "wait" ? 1000 : 0) + (n.tone === "busy" ? 300 : 0) + (n.pct >= NEAR ? 200 : 0) - n.idle / 100;
}

// drawScene draws the graph seen through pr on a canvas of W×H and answers
// where every node stands on it, for the pointer to find (hits), where the
// words are (taken), for the tip to keep off, and where each name went
// (labels). scene holds the rings and the nodes of a layout, each node
// carrying what it shows (name, tone, pct, work, note), the host's name, the
// palette and the type, the sides the names stood on the frame before
// (sides, which it updates) and whether a name may change its side (settle).
export function drawScene(ctx, W, H, pr, scene) {
    const { rings, nodes, host, pal, type } = scene;
    const u = type.unit;
    const taken = [];
    const discs = [];
    const items = [];
    const hits = [];
    const core = pr([0, 0, 0]);
    const zmax = Math.max(...rings.map((c) => c.R)) * 1.05;
    // Depth is told by how faint a line is, never by how large a mark is.
    const fade = (z) => clamp(1 - ((z + zmax) / (2 * zmax)) * (1 - FAR), FAR, 1);

    // The one light: behind the session that has waited for the person
    // longest, or the freshest at work.
    const glow = scene.glow;
    if (glow) {
        const q = pr(glow.p.w);
        const warm = glow.tone === "wait";
        const reach = GLOW * u;
        const g = ctx.createRadialGradient(q.x, q.y, 0, q.x, q.y, reach);
        g.addColorStop(0, rgba(warm ? pal.wait : pal.ink, warm ? 0.2 : 0.08));
        g.addColorStop(1, rgba(warm ? pal.wait : pal.ink, 0));
        ctx.fillStyle = g;
        ctx.fillRect(q.x - reach, q.y - reach, reach * 2, reach * 2);
    }

    // The rings, fainter where the dial ends.
    ctx.lineWidth = 0.7;
    for (const c of rings) {
        const N = 160;
        let prev = pr(at(0, c.R));
        for (let i = 1; i <= N; i++) {
            const a = (i / N) * TAU;
            const q = pr(at(a, c.R));
            const mid = ((i - 0.5) / N) * TAU;
            ctx.strokeStyle = rgba(pal.ink, (mid > SPAN ? 0.12 : 0.34) * fade((prev.z + q.z) / 2));
            ctx.beginPath();
            seg(ctx, prev, q);
            ctx.stroke();
            prev = q;
        }
    }

    // The meridian of now, from the host to the front of the outer ring, and
    // the scale named on the outer ring.
    const outer = rings[rings.length - 1];
    const front = pr(at(0, outer.R + 26));
    ctx.strokeStyle = rgba(pal.ink, 0.5);
    ctx.lineWidth = 0.7;
    ctx.beginPath();
    seg(ctx, core, front);
    ctx.stroke();
    caption(ctx, "now", type.big, rgba(pal.ink, 0.9), front.x, front.y + 2, "c", taken);
    for (const [min, text] of SCALE) {
        const q = pr(at(dial(min), outer.R + 30));
        caption(ctx, text, type.big, rgba(pal.ink, 0.55), q.x, q.y - type.big.size * 0.56, "c", taken);
    }

    // A contour is named at the right end of its ring.
    for (const c of rings) {
        let best = null;
        for (let i = 0; i < 72; i++) {
            const q = pr(at((i / 72) * TAU, c.R));
            if (!best || q.x > best.x) best = q;
        }
        caption(ctx, c.name, type.tag, rgba(pal.ink, 0.5), best.x + 12 * u, best.y - type.tag.size * 0.64, "l", taken);
    }

    // The orbits of the moons.
    ctx.lineWidth = 0.8;
    for (const n of nodes) {
        if (n.p.kids.length === 0) continue;
        let prev = pr(add(n.p.w, at(0, n.p.orbit)));
        for (let i = 1; i <= 48; i++) {
            const q = pr(add(n.p.w, at((i / 48) * TAU, n.p.orbit)));
            ctx.strokeStyle = rgba(pal.ink, 0.2 * fade(q.z));
            ctx.beginPath();
            seg(ctx, prev, q);
            ctx.stroke();
            prev = q;
        }
    }

    // The nodes, the far ones first.
    const order = nodes.map((n) => ({ n, q: pr(n.p.w) })).sort((a, b) => b.q.z - a.q.z);
    for (const { n, q } of order) {
        const f = fade(q.z);
        const r = (n.p.depth > 0 ? MOON_R : NODE_R) * u;
        mark(ctx, q, n, r, f, pal, u);
        contextArc(ctx, q, n, r + ARC_GAP * u, f, pal, u);
        beads(ctx, q, n, r + (ARC_GAP + BEADS) * u, f, pal, u);
        const d = { x: q.x, y: q.y, r: r + (ARC_GAP + 3) * u };
        discs.push(d);
        hits.push({ x: q.x, y: q.y, r: d.r + 4 * u, node: n });
        items.push({
            key: n.s.session, x: q.x, y: q.y, r: d.r, pri: priority(n),
            leader: n.tone === "wait" ? rgba(pal.wait, 0.8) : null,
            lines: linesOf(n, pal, type),
        });
    }

    // The host: a cross, and its name under it.
    ctx.strokeStyle = rgba(pal.ink, 0.8);
    ctx.lineWidth = 0.8;
    ctx.beginPath();
    seg(ctx, { x: core.x - 7 * u, y: core.y }, { x: core.x + 7 * u, y: core.y });
    seg(ctx, { x: core.x, y: core.y - 7 * u }, { x: core.x, y: core.y + 7 * u });
    ctx.stroke();
    discs.push({ x: core.x, y: core.y, r: 10 * u });
    caption(ctx, host, type.big, rgba(pal.ink, 0.85), core.x, core.y + 10 * u, "c", taken);

    const labels = tags(ctx, W, H, items, taken, discs, core, rgba(pal.ink, 0.3), u, scene.sides, scene.settle);
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
