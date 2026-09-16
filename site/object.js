/* The object under the page: one canvas, seven shapes, one galaxy.
   Sprites and the galaxy layers are drawn once into offscreen canvases;
   a frame only moves them. No library, no shadowBlur, no ctx.filter. */

const cv = document.getElementById("object");

// id|name|arm(s/x/c)|kind records, packed to fit the line budget; order feeds
// pts[i] below. Each node's prose lives in <dl id="legend">, read by say().
const NODES = `service|the service|s|core;exec|the executor|x|core;agent|the collector|c|core;pg|Postgres|s|infra;proxy|docker socket proxy|s|infra;edge|the edge|s|infra;phone|the phone|s|infra;desk|the desktop|s|infra;push|Web Push|s|infra;passkeys|passkeys|s|infra;
docker|docker|x|infra;tmux|tmux + claude|x|infra;actions|named actions|x|infra;transcripts|transcripts|c|infra;snapshot|the snapshot|c|infra;hist|history and charts|s|f;alerts|alerts and rules|s|f;devices|devices|s|f;settings|settings|s|f;map|the profile map|s|f;
contours|contours|s|f;stacks|containers by stack|s|f;logs|logs|s|f;updown|bring up / down|x|f;window|the window|x|f;stop|stop|x|f;composer|the composer|x|f;files|files both ways|x|f;term|the terminal|x|f;slash|slash commands|x|f;
permit|permission prompts|x|f;ask|questions|x|f;journal|the journal|x|f;sessions|sessions|c|f;feed|the feed|c|f;archive|session archive|c|f;machine|the machine|c|f;probes|probes|c|f;usage|usage|c|f`
    .split(";").map((rec, i) => { const [id, name, arm, kind] = rec.trim().split("|"); return { id, name, arm, kind, i, near: [] }; });
const BY = Object.fromEntries(NODES.map((n) => [n.id, n]));
// Edge pairs for the hover graph; both ends learn the other.
`service-exec service-agent exec-agent pg-service proxy-service proxy-docker edge-service phone-edge desk-edge push-service push-phone passkeys-service docker-exec tmux-exec tmux-agent actions-service
actions-exec transcripts-agent transcripts-tmux snapshot-agent snapshot-service hist-service hist-pg alerts-service alerts-push devices-service devices-push settings-service map-service map-exec contours-map stacks-service
stacks-proxy logs-service logs-proxy updown-exec updown-docker window-exec stop-exec composer-exec composer-tmux files-exec files-service term-exec term-service slash-exec permit-exec permit-service
ask-agent ask-exec ask-service journal-exec journal-pg sessions-agent sessions-service feed-agent feed-service archive-agent archive-transcripts machine-agent machine-service probes-agent usage-agent usage-transcripts`
    .trim().split(/\s+/).forEach((pair) => { const [a, b] = pair.split("-"); BY[a].near.push(BY[b]); BY[b].near.push(BY[a]); });

const TAU = Math.PI * 2, FLAT = 0.55, ARMS = 3, SPIN = 1.4, SPREAD = 0.22, RM = matchMedia("(prefers-reduced-motion: reduce)").matches;
const DPR = Math.min(devicePixelRatio || 1, innerWidth < 720 ? 1.5 : 2);   // soft stars survive 1.5 on a phone; the fill does not survive 2
// warm core → ink → cold rim → violet; a star picks one by its temperature
const TEMP = [[255, 236, 200], [255, 244, 222], [238, 241, 255], [219, 230, 255], [168, 200, 255], [99, 168, 255], [143, 157, 255], [122, 104, 214]];
const clamp = (v, a = 0, b = 1) => (v < a ? a : v > b ? b : v), lerp = (a, b, t) => a + (b - a) * t;
const ease = (t) => (t < .5 ? 4 * t * t * t : 1 - Math.pow(-2 * t + 2, 3) / 2), rgba = (c, a) => `rgba(${c[0]},${c[1]},${c[2]},${a})`;
function prng(a) { return () => { a |= 0; a = a + 0x6D2B79F5 | 0; let t = Math.imul(a ^ a >>> 15, 1 | a); t = t + Math.imul(t ^ t >>> 7, 61 | t) ^ t; return ((t ^ t >>> 14) >>> 0) / 4294967296; }; }

const SPR = 64, sprites = new Map();
function sprite(temp, rays) {                              // one radial star, cached by temperature
    const key = (temp << 1) | (rays ? 1 : 0);
    if (sprites.has(key)) return sprites.get(key);
    const c = document.createElement("canvas"); c.width = c.height = SPR;
    const g = c.getContext("2d"), h = SPR / 2, col = TEMP[temp]; g.globalCompositeOperation = "lighter";
    if (rays) for (const vert of [0, 1]) {                  // diffraction: two thin strokes, fading to the ends
        const lg = vert ? g.createLinearGradient(h, 0, h, SPR) : g.createLinearGradient(0, h, SPR, h);
        lg.addColorStop(0, rgba(col, 0)); lg.addColorStop(.5, rgba(col, .33)); lg.addColorStop(1, rgba(col, 0));
        g.fillStyle = lg;
        if (vert) g.fillRect(h - .7, 2, 1.4, SPR - 4); else g.fillRect(2, h - .7, SPR - 4, 1.4);
    }
    const rg = g.createRadialGradient(h, h, 0, h, h, h);    // white core → colour → nothing, falling off sharply
    rg.addColorStop(0, "rgba(255,255,255,1)"); rg.addColorStop(.05, rgba([lerp(col[0], 255, .6) | 0, lerp(col[1], 255, .6) | 0, lerp(col[2], 255, .6) | 0], .92));
    rg.addColorStop(.10, rgba(col, .42)); rg.addColorStop(.24, rgba(col, .085));
    rg.addColorStop(.55, rgba(col, .016)); rg.addColorStop(1, rgba(col, 0));
    g.fillStyle = rg; g.fillRect(0, 0, SPR, SPR); sprites.set(key, c); return c;
}
function dustSprite(col) {
    const c = document.createElement("canvas"); c.width = c.height = 128;
    const g = c.getContext("2d"), rg = g.createRadialGradient(64, 64, 0, 64, 64, 64);
    rg.addColorStop(0, rgba(col, .5)); rg.addColorStop(.45, rgba(col, .16)); rg.addColorStop(1, rgba(col, 0));
    g.fillStyle = rg; g.fillRect(0, 0, 128, 128); return c;
}

const POOL = 420, pts = [];
const r0 = prng(20260914);
for (let i = 0; i < POOL; i++) {
    const n = NODES[i];
    pts.push({
        i, n, u: r0(), v: r0(), w: r0(), ph: r0(), m: i % 10,
        sz: n ? (n.kind === "core" ? 2.8 : n.kind === "infra" ? 2.2 : 1.9) + r0() * .3 : .4 + Math.pow(r0(), 7) * 2.2,
        tm: n ? (n.kind === "core" ? 2 : 3 + ((i * 7) % 3)) : 0, tw: i % 10 === 3 && !n,
        x: 0, y: 0, s: 0, a: 0, t: 2, fx: 0, fy: 0, fs: 0, fa: 0, ft: 2, d: r0() * .3,
    });
}
function armPoint(r, k, out) {                              // Bruno Simon's arms: r^1.6, spin by radius, cubed spread
    const rr = .14 + Math.pow(r(), 1.6) * .86;             // the arms start outside the bulge, or the middle burns out
    const a = (k % ARMS) / ARMS * TAU + rr * SPIN * Math.PI + (r() + r() + r() - 1.5) * .032 * (1 - rr * .45);
    out.r = rr;
    out.x = Math.cos(a) * rr + Math.pow(r(), 3) * (r() < .5 ? 1 : -1) * SPREAD * rr;
    out.y = Math.sin(a) * rr + Math.pow(r(), 3) * (r() < .5 ? 1 : -1) * SPREAD * rr;
    return out;
}
// the 39 stars of the first magnitude, laid on the same three arms
const r = prng(7717), AI = { s: 0, x: 1, c: 2 }, put = (n, rr, j) => {
    const a = AI[n.arm] / ARMS * TAU + rr * SPIN * Math.PI + j;
    n.gx = Math.cos(a) * rr; n.gy = Math.sin(a) * rr;
};
for (const arm of ["s", "x", "c"]) {
    const list = NODES.filter((n) => n.arm === arm && n.kind !== "core")
        .sort((a, b) => (a.kind === "infra" ? 0 : 1) - (b.kind === "infra" ? 0 : 1));
    list.forEach((n, i) => put(n, .28 + .64 * (i / (list.length - 1)) + (r() - .5) * .07, (r() - .5) * .13));
    NODES.filter((n) => n.arm === arm && n.kind === "core").forEach((n) => put(n, .3, 0));
}
const r2 = prng(4242), o = {};
for (const p of pts) {
    if (p.n) { p.gx = p.n.gx; p.gy = p.n.gy; continue; }
    armPoint(r2, p.i, o); p.gx = o.x; p.gy = o.y; p.gt = clamp(o.r * 1.25 + (r2() - .5) * .3);
}
const view = { W: 0, H: 0, S: 0, cx: 0, cy: 0, gr: 0 };
let layers = [], ctx = null, T = RM ? 4.2 : 0, cur = "hero", prev = "hero", mStart = -1e6, galFrom = 0, galA = 0, hover = null;
const DUR = 1050;

function buildGalaxy() {                                    // 5200 stars and 30 dust clouds, drawn once per size
    const R = view.gr;
    if (!R) return;
    layers = [[2900, .5, .0080, 1, .8], [2300, .75, .0095, 1.05, .78], [1100, 1, .0110, 1.1, .7]].map(([count, q, spd, sc, al], li) => {
        const r = prng(1337 + li * 101), side = Math.round(2.4 * R), px = Math.min(DPR, q < 1 ? 1 : 1.5);
        const c = document.createElement("canvas");
        c.width = c.height = Math.round(side * px);
        const g = c.getContext("2d"), h = side / 2, o = {};
        g.scale(px, px); g.globalCompositeOperation = "lighter";
        if (li === 0) for (let i = 0; i < 34; i++) {         // dust along the arms, barely there
            armPoint(r, i, o);
            const rad = 40 + r() * 80, col = TEMP[o.r < .35 ? 1 : 4 + ((i * 3) % 4)], sp = dustSprite(col);
            g.globalAlpha = .03 + r() * .03;
            g.drawImage(sp, h + o.x * R - rad, h + o.y * R - rad, rad * 2, rad * 2);
        }
        if (li === 1) {                                     // the hot core: a wide soft glow over the crowded middle
            for (const [rad, stops] of [[R * .44, [[0, [255, 247, 228], .17], [.3, [255, 222, 170], .07], [1, [255, 200, 140], 0]]],
                [R * .95, [[0, [120, 150, 255], .028], [1, [90, 110, 220], 0]]]]) {
                const rg = g.createRadialGradient(h, h, 0, h, h, rad);
                for (const [st, col, a] of stops) rg.addColorStop(st, rgba(col, a));
                g.globalAlpha = 1; g.fillStyle = rg; g.fillRect(0, 0, side, side);
            }
        }
        const clumps = Array.from({ length: 11 }, (_, i) => ({ ...armPoint(r, i * 2 + 1, {}) }));
        for (let i = 0; i < count; i++) {
            armPoint(r, i, o);
            let x = o.x, y = o.y;
            if (i % 7 === 0) {                              // the bulge: crowded, round, no arms
                const rr = Math.pow(r(), 2.2) * .46, a = r() * TAU;
                x = Math.cos(a) * rr; y = Math.sin(a) * rr; o.r = rr; o.small = 1;
            } else if (i % 7 === 1) {                       // clusters: knots of stars off the smooth arms
                const cl = clumps[i % clumps.length];
                x = cl.x + (r() + r() + r() - 1.5) * .05; y = cl.y + (r() + r() + r() - 1.5) * .05; o.r = Math.hypot(x, y);
            }
            const small = o.small; o.small = 0;
            const s = (.4 + Math.pow(r(), 11) * 3.8) * (.6 + q * .7) * (small ? .8 : 1);   // power law: thousands tiny, a few large
            const temp = clamp(Math.round((clamp(o.r * 1.3 + (r() - .5) * .5) * 7)), 0, 7) | 0;
            // the arms crowd towards the middle: dim each star there, or the sum burns out flat white
            g.globalAlpha = clamp((.13 + Math.pow(r(), 2) * .38 + (s > 2 ? .2 : 0)) * (small ? .3 : 1) * (small ? clamp(.4 + o.r * 2.4) : clamp(.1 + (o.r - .12) * 1.9)), .04, .85);
            const half = Math.max(s * 6, 1.1);
            g.drawImage(sprite(temp, s > 3.2), h + x * R - half, h + y * R - half, half * 2, half * 2);
        }
        return { c, spd, sc, al, r: side / 2 };
    });
}

// Every shape writes the point's target into tx/ty (centred), ts, ta, tt. Time makes them live.
function field(p, lo, hi) {
    const a = p.u * TAU, r = view.S * (lo + (hi - lo) * Math.pow(p.v, .6));
    p.tx = Math.cos(a) * r; p.ty = Math.sin(a) * r * .8; p.ts = p.sz * .7; p.ta = .12 + .26 * p.w; p.tt = 2 + (p.w * 4 | 0);
}
// Radius and height of each coil, base to tip.
const COILS = [[.27, .2], [.195, .04], [.13, -.085], [.045, -.175]];
const SHAPES = {
    hero(p) {                                               // the orbit mark: a ring and a tilted ellipse
        const S = view.S;
        if (p.m < 4) { const a = p.u * TAU + T * .05, r = S * .2 * (1 + (p.v - .5) * .03); p.tx = Math.cos(a) * r; p.ty = Math.sin(a) * r; }
        else if (p.m < 8) {
            const a = p.u * TAU - T * .1, ca = Math.cos(-.4189), sa = Math.sin(-.4189);
            const x = Math.cos(a) * S * .38 * (1 + (p.v - .5) * .03), y = Math.sin(a) * S * .16;
            p.tx = x * ca - y * sa; p.ty = x * sa + y * ca;
        } else { field(p, .3, 1); return; }
        p.ts = p.sz; p.ta = .5 + .5 * p.w; p.tt = p.n ? p.tm : 2 + (p.v * 4 | 0);
    },
    see(p) {                                                // a sphere of points, breathing
        if (p.m === 9) { field(p, .38, 1); return; }
        const y = 1 - 2 * (p.i + .5) / POOL, rr = Math.sqrt(Math.max(0, 1 - y * y));
        const a = p.i * 2.3999 + T * .22, R = view.S * .29 * (1 + .045 * Math.sin(T * .85)) * (.93 + p.v * .12);
        const d = (Math.sin(a) * rr + 1) / 2;
        p.tx = Math.cos(a) * rr * R; p.ty = y * R;
        p.ts = p.sz * (.45 + .9 * d); p.ta = .16 + .8 * d; p.tt = p.n ? p.tm : 2 + (d * 3 | 0);
    },
    ring(p) {                                               // stacks around a ring, their indicators blinking
        if (p.i >= 189) { field(p, .2, 1); return; }
        const g = p.i % 7, k = p.i / 7 | 0, S = view.S, a = g / 7 * TAU - Math.PI / 2 + T * .045;
        p.tx = Math.cos(a) * S * .33 + ((k % 3) - 1) * S * .022;
        p.ty = Math.sin(a) * S * .33 * .72 + ((k / 3 | 0) - 4) * S * .016;
        p.ts = p.sz * .95; p.tt = p.n ? p.tm : (p.w < .18 ? 5 : 3);
        p.ta = p.w < .18 ? .3 + .7 * (.5 + .5 * Math.sin(T * 2.6 + p.ph * TAU)) : .75;
    },
    gate(p) {                                               // four coils stacked, lit from the base up every seven seconds
        const S = view.S;
        if (p.m === 9) { field(p, .35, 1); return; }
        const k = p.m < 4 ? 0 : p.m < 6 ? 1 : p.m < 8 ? 2 : 3;
        const c = COILS[k], a = p.u * TAU + T * .12 + k * 1.1;
        const r = S * c[0] * (k === 3 ? Math.sqrt(p.w) : 1 - p.w * .22);   // the tip is a point, the coils are rings
        p.tx = Math.cos(a) * r + (k * k - 3.5) * S * .012;                 // and the stack leans as it rises
        p.ty = S * c[1] + Math.sin(a) * r * .44;
        const ph = (T % 7) / 7, h = clamp((S * .3 - p.ty) / (S * .56));
        p.ts = p.sz; p.tt = p.n ? p.tm : 1 + (p.v * 3 | 0);
        p.ta = clamp((.42 + .45 * p.w) * (1 + 1.6 * Math.exp(-Math.pow(h - ph, 2) / .012)), 0, 1);
    },
    built(p) {                                              // the galaxy: the same arms the layers were drawn on
        const a = T * .0095, c = Math.cos(a), s = Math.sin(a), R = view.gr;
        p.tx = (p.gx * c - p.gy * s) * R; p.ty = (p.gx * s + p.gy * c) * R * FLAT;
        p.ts = p.n ? p.sz : p.sz * .55; p.tt = p.n ? p.tm : clamp(p.gt * 7, 0, 7) | 0;
        p.ta = p.n ? 1 : clamp(.14 + p.sz * .2);
    },
    install(p) {                                            // a stretched stream of points
        if (p.m > 7) { field(p, .35, 1); return; }
        const t = (p.u + T * .07) % 1, S = view.S;
        p.tx = (t - .5) * view.W * 1.15;
        p.ty = (p.v - .5) * S * .035 + Math.sin(t * TAU * 1.5 + p.ph) * S * .02;
        p.ts = p.sz; p.ta = clamp(Math.sin(t * Math.PI) * 1.9) * .95; p.tt = 3 + (p.w * 3 | 0);
    },
    status(p) { field(p, .15, 1.1); p.ts = p.sz * .8; p.ta = .1 + .3 * p.w; },
};
// Chapters share the seven shapes so that no two neighbours stand on the same one.
SHAPES.session = SHAPES.cards = SHAPES.machine = SHAPES.ring; SHAPES.sheets = SHAPES.more = SHAPES.see; SHAPES.terminal = SHAPES.gate;
function setChapter(name) {
    if (!SHAPES[name] || name === cur) return;
    for (const p of pts) { p.fx = p.x; p.fy = p.y; p.fs = p.s; p.fa = p.a; p.ft = p.t; }
    galFrom = galA; prev = cur; cur = name; mStart = performance.now();
    if (name === "built" && !layers.length) buildGalaxy();
    svg.classList.toggle("on", name === "built");
    if (name === "built" && cap && !cap.textContent) say(NODES[0]);
    if (RM) draw(); else if (!raf) loop();
}
function draw() {
    const { W, H, cx, cy } = view;
    const raw = RM ? 1 : clamp((performance.now() - mStart) / DUR);
    ctx.clearRect(0, 0, W, H);
    ctx.globalCompositeOperation = "lighter";
    galA = lerp(galFrom, cur === "built" ? 1 : 0, ease(raw));
    if (galA > .003 && layers.length) for (const L of layers) {
        ctx.save();
        ctx.translate(cx, cy); ctx.scale(1, FLAT); ctx.rotate(T * L.spd); ctx.scale(L.sc, L.sc);
        ctx.globalAlpha = galA * L.al;
        ctx.drawImage(L.c, -L.r, -L.r, L.r * 2, L.r * 2);
        ctx.restore();
    }
    for (const p of pts) {
        SHAPES[cur](p);
        const k = raw >= 1 ? 1 : ease(clamp((raw - p.d) / .7));
        p.x = lerp(p.fx, cx + p.tx, k); p.y = lerp(p.fy, cy + p.ty, k);
        p.s = lerp(p.fs, p.ts, k); p.a = lerp(p.fa, p.ta, k); p.t = Math.round(lerp(p.ft, p.tt, k));
    }
    if (hover && galA > .5) {                               // only on hover: the node's edges, fading to both ends
        const h = pts[hover.i];
        ctx.lineWidth = 1;
        for (const o of hover.near) {
            const q = pts[o.i], g = ctx.createLinearGradient(h.x, h.y, q.x, q.y);
            g.addColorStop(0, "rgba(99,168,255,0)"); g.addColorStop(.5, `rgba(127,227,212,${.5 * galA})`); g.addColorStop(1, "rgba(99,168,255,0)");
            ctx.strokeStyle = g; ctx.beginPath(); ctx.moveTo(h.x, h.y); ctx.lineTo(q.x, q.y); ctx.stroke();
        }
    }
    for (const p of pts) {
        let a = p.a;
        if (p.tw && !RM) a *= .55 + .45 * Math.sin(T * 3.1 + p.ph * TAU);
        if (hover && p.n && p.n !== hover && galA > .5) a *= .55;
        if (a < .004 || p.s < .02) continue;
        ctx.globalAlpha = clamp(a);
        const h = p.s * 6;
        ctx.drawImage(sprite(clamp(p.t, 0, 7), p.s > 2.9), p.x - h, p.y - h, h * 2, h * 2);
    }
    const satA = (cur === "hero" ? ease(raw) : prev === "hero" ? 1 - ease(raw) : 0);
    if (satA > .01) {                                       // the satellite riding the tilted ellipse
        const a = -T * .55, ca = Math.cos(-.4189), sa = Math.sin(-.4189), S = view.S;
        const x = Math.cos(a) * S * .38, y = Math.sin(a) * S * .16, h = 34;
        ctx.globalAlpha = satA;
        ctx.drawImage(sprite(2, true), cx + x * ca - y * sa - h, cy + x * sa + y * ca - h, h * 2, h * 2);
    }
    ctx.globalAlpha = 1;
    if (galA > .5) placeLabels();
}

const NS = "http://www.w3.org/2000/svg", svg = document.createElementNS(NS, "svg");
const gHit = document.createElementNS(NS, "g"), gTxt = document.createElementNS(NS, "g"), cap = document.getElementById("object-caption"), labels = new Map();
svg.id = "object-nodes"; svg.setAttribute("aria-hidden", "true"); svg.append(gHit, gTxt);
for (const n of NODES) {
    const c = document.createElementNS(NS, "circle");
    c.setAttribute("class", "hit"); c.setAttribute("r", 15); c.dataset.i = n.i; gHit.appendChild(c); n.hit = c;
    if (n.kind === "core") { const t = document.createElementNS(NS, "text"); gTxt.appendChild(t); labels.set(n, t); }
}
// The description is real markup, not JS data, so a screen reader can reach it
// without the canvas; the hover caption borrows the same text by id.
const say = (n) => { const w = document.getElementById("w-" + n.id); if (cap && w) cap.innerHTML = `<b>${n.name}</b> — ${w.textContent}`; };
function placeLabels() {
    const seen = new Set();
    for (const n of [...labels.keys(), hover].filter(Boolean)) {
        let t = labels.get(n);
        if (!t) { t = document.createElementNS(NS, "text"); gTxt.appendChild(t); labels.set(n, t); }
        const p = pts[n.i];
        t.setAttribute("x", Math.round(p.x)); t.setAttribute("y", Math.round(p.y - p.s * 6 - 6)); t.setAttribute("class", n === hover ? "on" : ""); t.textContent = n.name;
        seen.add(n);
    }
    for (const [n, t] of labels) if (!seen.has(n)) { t.remove(); labels.delete(n); }
}
function setHover(n) {
    if (hover === n) return;
    hover = n; if (n) say(n); if (RM) draw();
}
svg.addEventListener("pointerover", (e) => { const i = e.target.dataset?.i; if (i) setHover(NODES[+i]); });
svg.addEventListener("pointerout", (e) => { if (e.target.dataset?.i && !svg.contains(e.relatedTarget)) setHover(null); });
svg.addEventListener("pointerdown", (e) => { const i = e.target.dataset?.i; if (i) setHover(NODES[+i]); });
document.addEventListener("pointerdown", (e) => { if (!e.target.dataset?.i) setHover(null); }, true);
function resize() {
    const r = cv.getBoundingClientRect(), W = Math.round(r.width) || innerWidth, H = Math.round(r.height) || innerHeight;
    if (W === view.W && H === view.H) return;               // the address bar came or went: the canvas is the same size
    view.W = W; view.H = H; view.S = Math.min(view.W, view.H); view.cx = view.W / 2; view.cy = view.H / 2;
    view.gr = Math.min(view.S * .72, (view.W / 2 - 24) / .92);   // every node has to land on the screen
    cv.width = Math.round(view.W * DPR); cv.height = Math.round(view.H * DPR);
    ctx = cv.getContext("2d", { alpha: true }); ctx.setTransform(DPR, 0, 0, DPR, 0, 0);
    svg.setAttribute("width", view.W); svg.setAttribute("height", view.H); gHit.setAttribute("transform", `translate(${view.cx} ${view.cy}) scale(1 ${FLAT})`);
    for (const n of NODES) { n.hit.setAttribute("cx", (n.gx * view.gr).toFixed(1)); n.hit.setAttribute("cy", (n.gy * view.gr).toFixed(1)); }
    if (layers.length) buildGalaxy();
    if (RM) draw();
}
let raf = 0, last = 0, visible = true;
function loop(now = performance.now()) {
    raf = requestAnimationFrame(loop);
    if (now - last < 30) return;                            // 30 fps is the target; the frame is cheap enough
    T += Math.min(.06, (now - last) / 1000); last = now;
    draw();
}
function pump() {
    const on = visible && !document.hidden;
    if (on && !raf && !RM) { last = performance.now(); loop(); }
    if (!on && raf) { cancelAnimationFrame(raf); raf = 0; }
}
function boot() {
    (cv.parentElement || document.body).appendChild(svg);
    resize();
    for (const p of pts) { field(p, .3, 1.1); p.x = view.cx + p.tx; p.y = view.cy + p.ty; p.s = p.ts; p.a = 0; p.t = p.tt; }
    mStart = performance.now(); cur = document.documentElement.dataset.chapter || "hero";
    if (cur === "built") buildGalaxy(); else if (!RM) setTimeout(() => layers.length || buildGalaxy(), 1200);   // drawn once, while the page is still quiet
    svg.classList.toggle("on", cur === "built");
    addEventListener("resize", () => { clearTimeout(boot.t); boot.t = setTimeout(resize, 180); });
    document.addEventListener("chapterchange", (e) => setChapter(e.detail?.chapter));
    document.addEventListener("visibilitychange", pump);
    new IntersectionObserver((es) => { visible = es[0].isIntersecting; pump(); }, { threshold: 0 }).observe(cv);
    if (RM) requestAnimationFrame(draw); else pump();
}
if (cv) boot();
export { setChapter };
