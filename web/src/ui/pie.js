// The pie: a menu of a few large sectors, an icon and a word in each, that
// stands in one place whatever was pressed — low in the middle of the screen,
// in the reach of the thumb, always of one size — so an item is found where it
// was found the time before. A sector chosen moves out of the ring, and the
// plate over the ring says what it does, or why it cannot be done now. The hub
// in the middle closes the pie, or goes back from a second ring to the first.
//
// It opens on a long press (useHold) and is chosen from by a tap. A press that
// slides on to a sector and lifts there chooses too, except an item that asks
// for a tap: lifting over one of those only points at it. What was pressed is
// raised over the dimmed screen, above the pie where the pie would cover it,
// with its own place hatched.
//
// An item is { key, icon, word, lines, title, note, off, short, tap, on,
// danger, items, pick }: the word under the icon (lines, where it takes two),
// what the plate says of it, why it cannot be done now (off) and the word
// under a dimmed one (short), whether only a tap does it, whether it is on,
// whether it ends something, the second ring it opens (items) or what it does
// (pick). A slot left empty (null) keeps the places of the others.
import { useEffect, useLayoutEffect, useRef, useState } from "preact/hooks";

import { html } from "../html.js";
import { useBackClose } from "./back.js";

// How long a press is held before it opens a pie, and how far the finger may
// wander meanwhile: further is a scroll or a swipe, not a press.
const HOLD_MS = 450;
const HOLD_SLOP = 10;

// The ring: its radius, the hub's, how far a chosen sector moves out and how
// much it grows, and the gap between two sectors.
const RING = 140;
const HUB = 48;
const PUSH = 9;
const GROW = 6;
const GAP = 3;
// Where the ring stands: its bottom this far over the bottom of the screen,
// the plate this far over the ring and the thing raised this far over a plate
// of one line.
const LOW = 74;
const PLATE_GAP = 12;
const PLATE_LINE = 46;
const LIFT_GAP = 8;
// How far past its ring a sliding finger still points at a sector.
const REACH = 48;
// A ring of more items than this draws their icons smaller.
const MANY = 8;

const HINT = "tap an action, or slide to it and lift";
const RAD = Math.PI / 180;

const f1 = (n) => n.toFixed(1);
const point = (x, y, r, deg) => ({ x: x + r * Math.cos(deg * RAD), y: y + r * Math.sin(deg * RAD) });

// wedge outlines a sector of a ring centred at x, y between radii r0 and r1,
// from angle a0 to a1 in degrees, a gap wide along both of its edges.
function wedge(x, y, r0, r1, a0, a1) {
    const g0 = GAP / 2 / r0 / RAD;
    const g1 = GAP / 2 / r1 / RAD;
    const p1 = point(x, y, r1, a0 + g1);
    const p2 = point(x, y, r1, a1 - g1);
    const p3 = point(x, y, r0, a1 - g0);
    const p4 = point(x, y, r0, a0 + g0);
    const big1 = a1 - a0 - 2 * g1 > 180 ? 1 : 0;
    const big0 = a1 - a0 - 2 * g0 > 180 ? 1 : 0;
    return `M${f1(p1.x)},${f1(p1.y)} A${r1},${r1} 0 ${big1} 1 ${f1(p2.x)},${f1(p2.y)} `
        + `L${f1(p3.x)},${f1(p3.y)} A${r0},${r0} 0 ${big0} 0 ${f1(p4.x)},${f1(p4.y)} Z`;
}

// placeOf lays the pie out on a screen of w by h: the centre of the ring and
// its radius, the box the ring is drawn in, where the plate ends and the
// highest a thing pressed may stand to stay clear of a plate of one line.
function placeOf(w, h) {
    const ring = Math.max(96, Math.min(RING, w / 2 - 8 - PUSH - GROW));
    const side = 2 * (ring + PUSH + GROW + 12);
    const x = w / 2;
    const y = Math.max(ring + PLATE_GAP + PLATE_LINE + 24, h - LOW - ring);
    const plate = y - ring - PLATE_GAP;
    return { w, h, x, y, ring, side, left: x - side / 2, top: y - side / 2, plate, clear: plate - PLATE_LINE - LIFT_GAP };
}

// sectorsOf draws the slots of a ring in its box, clockwise from the top: the
// outline of each and the middle its icon and word stand at. The chosen one
// moves out and grows, unless it cannot be done.
function sectorsOf(items, sel, at) {
    const c = at.side / 2;
    const step = 360 / items.length;
    return items.map((it, i) => {
        if (!it) return null;
        const mid = -90 + i * step;
        const out = it.key === sel && !it.off;
        const o = out ? point(c, c, PUSH, mid) : { x: c, y: c };
        const r1 = out ? at.ring + GROW : at.ring;
        // How high and wide the sector reaches, for the light of its glass.
        const edge = [];
        for (let k = 0; k <= 8; k += 1) {
            const deg = mid - step / 2 + (step * k) / 8;
            edge.push(point(o.x, o.y, HUB + 4, deg), point(o.x, o.y, r1, deg));
        }
        const xs = edge.map((p) => p.x);
        const ys = edge.map((p) => p.y);
        return {
            it, out, slot: i, on: it.key === sel,
            d: wedge(o.x, o.y, HUB + 4, r1, mid - step / 2, mid + step / 2),
            mid: point(o.x, o.y, (HUB + r1) / 2 + 4, mid),
            box: { left: Math.min(...xs), right: Math.max(...xs), top: Math.min(...ys), bottom: Math.max(...ys) },
        };
    });
}

// slotAt is the slot of a ring of n under a point of the screen, "hub" over
// the hub, and -1 off the ring.
function slotAt(at, n, x, y) {
    const dx = x - at.x;
    const dy = y - at.y;
    const far = Math.hypot(dx, dy);
    if (far <= HUB + 4) return "hub";
    if (far > at.ring + REACH) return -1;
    const deg = Math.atan2(dy, dx) / RAD + 90;
    return ((Math.round(deg / (360 / n)) % n) + n) % n;
}

// liftOf says where the thing pressed is raised to: where it stands, or over
// the plate where the pie would cover it, from is its own place then.
function liftOf(el, at) {
    if (!el || !el.isConnected) return null;
    const r = el.getBoundingClientRect();
    const box = { left: r.left, top: r.top, width: r.width, height: r.height };
    if (r.bottom <= at.clear) return { box, from: null };
    return { box: { ...box, top: Math.max(8, at.clear - r.height) }, from: box };
}

const px = (box) => `left:${f1(box.left)}px;top:${f1(box.top)}px;width:${f1(box.width)}px;height:${f1(box.height)}px`;

// Lift is a copy of what was pressed over the dimmed screen, drawn again as
// the original changes; the original keeps its place in the list.
function Lift({ el, box }) {
    const ref = useRef(null);
    useLayoutEffect(() => {
        const holder = ref.current;
        if (!holder || !el || !el.isConnected) return;
        const copy = el.cloneNode(true);
        copy.removeAttribute("data-name");
        const style = getComputedStyle(el);
        holder.style.fontSize = style.fontSize;
        holder.replaceChildren(copy);
    });
    return html`<div class="pielift" ref=${ref} style=${px(box)} aria-hidden="true" inert></div>`;
}

// Plate says what the chosen item does, why it cannot be done now, or — with
// nothing chosen — whose menu it is and how it is used. A finger held over an
// item lifting does not do, or lifted over one, is told so.
function Plate({ at, item, name, lifting }) {
    const why = item && item.off ? item.off : "";
    const tap = Boolean(item) && !why && lifting && item.tap;
    const words = !item ? [name, HINT]
        : why ? [item.title, lifting ? `${why} — lifting here does nothing` : why]
        : tap ? [item.title, `${item.note ? `${item.note} · ` : ""}tap it: lifting here does nothing`]
        : [item.title, item.note || ""];
    return html`
        <div class=${`pieplate${why ? " pieoff" : ""}${item && item.danger && !why ? " piedanger" : ""}`}
             style=${`bottom:${f1(at.h - at.plate)}px`} aria-live="polite">
            <b>${words[0]}</b>${words[1] && html`<span class=${why || tap ? "piewhy" : ""}>${words[1]}</span>`}
        </div>
    `;
}

// tint gives a colour of the theme — a hex or an rgb() of the tokens — at an
// opacity of its own.
function tint(color, alpha) {
    const hex = /^#([0-9a-f]{6})$/i.exec(color);
    const rgb = hex ? [0, 2, 4].map((i) => parseInt(hex[1].slice(i, i + 2), 16)) : (color.match(/[\d.]+/g) || [0, 0, 0]).slice(0, 3);
    return `rgba(${rgb.join(",")},${alpha})`;
}

// The room the canvas of the ring keeps around it for the glow of the chosen
// sector and the shadow of the whole ring.
const BLEED = 36;

// Ring draws the sectors and the hub on a canvas, as the panel draws its
// other pictures: each sector of glass, lit from its top; the chosen one in
// the accent — in the colour of an end for an item that ends something —
// glowing; a chosen one that cannot be done in a dashed line; the whole ring
// over a shadow of its own. The blur of the glass is the backdrop of an
// element clipped to the sectors.
function Ring({ at, sectors }) {
    const ref = useRef(null);
    const drawn = sectors.filter(Boolean);
    // The drawing changes with the ring and the chosen sector, not with every
    // redraw of the screen under it.
    const shape = `${at.side}:${drawn.map((s) => `${s.d}${s.on ? "+" : ""}${s.out ? "*" : ""}${s.it.danger ? "!" : ""}`).join("|")}`;
    useLayoutEffect(() => {
        const canvas = ref.current;
        if (!canvas) return;
        const scale = window.devicePixelRatio || 1;
        const room = at.side + 2 * BLEED;
        canvas.width = Math.round(room * scale);
        canvas.height = Math.round(room * scale);
        const ctx = canvas.getContext("2d");
        ctx.setTransform(scale, 0, 0, scale, BLEED * scale, BLEED * scale);
        const css = getComputedStyle(canvas);
        const token = (name) => css.getPropertyValue(name).trim();
        const c = at.side / 2;
        const hub = new Path2D();
        hub.arc(c, c, HUB, 0, 2 * Math.PI);
        const shapes = drawn.map((s) => ({ s, path: new Path2D(s.d) }));

        // The shadow of the ring is cast by its outline drawn out of sight:
        // only the shadow lands, under the ring as one piece.
        const away = room * 4;
        ctx.save();
        ctx.translate(-away, 0);
        ctx.shadowColor = "rgba(0,0,0,.5)";
        ctx.shadowBlur = 26 * scale;
        ctx.shadowOffsetX = away * scale;
        ctx.shadowOffsetY = 10 * scale;
        for (const { path } of shapes) ctx.fill(path);
        ctx.fill(hub);
        ctx.restore();

        const glass = (s, path) => {
            const light = ctx.createLinearGradient(0, s.box.top, 0, s.box.bottom);
            light.addColorStop(0, token("--pie-top"));
            light.addColorStop(1, token("--pie-foot"));
            ctx.fillStyle = light;
            ctx.fill(path);
        };
        // The chosen sector is drawn last, over its neighbours.
        for (const { s, path } of [...shapes.filter((x) => !x.s.on), ...shapes.filter((x) => x.s.on)]) {
            glass(s, path);
            if (s.out) {
                const tone = token(s.it.danger ? "--crit" : "--accent");
                const lit = ctx.createLinearGradient(s.box.left, s.box.top, s.box.right, s.box.bottom);
                lit.addColorStop(0, tint(tone, .46));
                lit.addColorStop(.7, tint(tone, .24));
                lit.addColorStop(1, tint(tone, .24));
                ctx.save();
                ctx.shadowColor = tint(tone, .55);
                ctx.shadowBlur = 14 * scale;
                ctx.fillStyle = lit;
                ctx.fill(path);
                ctx.restore();
                ctx.strokeStyle = tint(tone, .85);
                ctx.lineWidth = 1;
                ctx.stroke(path);
            } else if (s.on) {
                ctx.save();
                ctx.setLineDash([4, 3]);
                ctx.strokeStyle = tint(token("--ink"), .5);
                ctx.lineWidth = 1;
                ctx.stroke(path);
                ctx.restore();
            } else {
                ctx.strokeStyle = token("--pie-edge");
                ctx.lineWidth = .75;
                ctx.stroke(path);
            }
        }
        ctx.fillStyle = token("--pie-hub");
        ctx.fill(hub);
        ctx.strokeStyle = token("--pie-edge");
        ctx.lineWidth = .75;
        ctx.stroke(hub);
    }, [shape]);
    const room = at.side + 2 * BLEED;
    return html`
        <div class="pieglass" style=${`left:${f1(at.left)}px;top:${f1(at.top)}px;width:${at.side}px;height:${at.side}px;clip-path:path('${drawn.map((s) => s.d).join(" ")}')`}></div>
        <canvas class="piering" ref=${ref} aria-hidden="true"
                style=${`left:${f1(at.left - BLEED)}px;top:${f1(at.top - BLEED)}px;width:${room}px;height:${room}px`}></canvas>
    `;
}

// Pie is the menu. label names it for a reader, name is what the hub and the
// plate call the thing it is for, lift the element pressed, press the pointer
// still held from the press that opened it and where it went down (null when
// the keyboard or a reader opened it), onClose takes it down.
export function Pie({ label, name, items, lift = null, press = null, onClose }) {
    const [size, setSize] = useState(() => ({ w: window.innerWidth, h: window.innerHeight }));
    const [more, setMore] = useState(false);
    const [sel, setSel] = useState("");
    const [held, setHeld] = useState(Boolean(press));
    // The item the press lifted over without doing it.
    const [lifted, setLifted] = useState("");
    const [raised] = useState(() => liftOf(lift, placeOf(window.innerWidth, window.innerHeight)));
    const menu = useRef(null);
    // The element a press on the pie went down on: a click counts only where
    // its press went down, so the click that ends the press opening the pie
    // chooses nothing.
    const armed = useRef(null);

    const at = placeOf(size.w, size.h);
    const parent = items.find((it) => it && it.items);
    const ring = more && parent ? parent.items : items;
    const sectors = sectorsOf(ring, sel, at);
    const chosen = ring.find((it) => it && it.key === sel) || null;

    // What the listeners of the held press read: the pie as it is drawn now.
    const now = useRef(null);
    now.current = { ring, at, more };

    const closer = useBackClose(true, onClose);

    // choose does an item: one that cannot be done says why on the plate, the
    // item of a second ring opens it, any other takes the pie down first —
    // what it opens is a layer of its own — and then does what it does.
    const choose = (it, keys = false) => {
        if (!it) return;
        if (it.off) {
            setSel(it.key);
            return;
        }
        if (it.items) {
            setMore(true);
            setSel("");
            requestAnimationFrame(() => focusFirst(keys));
            return;
        }
        onClose();
        it.pick();
    };
    const hub = (keys = false) => {
        if (!more) {
            closer();
            return;
        }
        setMore(false);
        setSel("");
        requestAnimationFrame(() => focusFirst(keys));
    };
    const focusFirst = (keys) => {
        const el = menu.current;
        if (!el) return;
        const first = keys ? el.querySelector('.piesec:not([aria-disabled="true"])') : null;
        (first || el).focus({ preventScroll: true });
    };

    useEffect(() => {
        const resized = () => setSize({ w: window.innerWidth, h: window.innerHeight });
        const keys = (event) => {
            if (event.key !== "Escape") return;
            event.preventDefault();
            event.stopPropagation();
            closer();
        };
        window.addEventListener("resize", resized);
        document.addEventListener("keydown", keys, true);
        focusFirst(!press);
        return () => {
            window.removeEventListener("resize", resized);
            document.removeEventListener("keydown", keys, true);
        };
    }, []);

    // The press that opened the pie is followed until it lifts: once it has
    // slid off the place it went down, the sector under it is chosen as it
    // goes, and lifting over one does it, unless it asks for a tap or cannot
    // be done. A press that lifts where it went down chooses nothing, though
    // the pie may stand over that place.
    useEffect(() => {
        if (!press) return undefined;
        let slid = false;
        const over = (event) => {
            const { ring: list, at: place } = now.current;
            const slot = slotAt(place, list.length, event.clientX, event.clientY);
            return typeof slot === "number" && slot >= 0 ? list[slot] : null;
        };
        const move = (event) => {
            if (event.pointerId !== press.pointerId) return;
            slid = slid || Math.hypot(event.clientX - press.x, event.clientY - press.y) > HOLD_SLOP;
            if (!slid) return;
            const it = over(event);
            setSel(it ? it.key : "");
        };
        const up = (event) => {
            if (event.pointerId !== press.pointerId) return;
            stop();
            setHeld(false);
            const it = event.type === "pointerup" && slid ? over(event) : null;
            if (!it) {
                setSel("");
                return;
            }
            setSel(it.key);
            if (it.off || it.tap) setLifted(it.key);
            else choose(it);
        };
        const stop = () => {
            document.removeEventListener("pointermove", move, true);
            document.removeEventListener("pointerup", up, true);
            document.removeEventListener("pointercancel", up, true);
        };
        document.addEventListener("pointermove", move, true);
        document.addEventListener("pointerup", up, true);
        document.addEventListener("pointercancel", up, true);
        return stop;
    }, []);

    const under = (event) => Boolean(armed.current) && event.currentTarget.contains(armed.current);
    const fair = (event) => event.detail === 0 || under(event);
    const pressed = (handler) => (event) => {
        const ok = fair(event);
        armed.current = null;
        if (ok) handler(event);
    };
    // The keyboard chooses a sector by standing on it; a pointer chooses by
    // its press, and the focus its press brings chooses nothing: a sector
    // chosen moves out, and could move out from under the press.
    const stand = (key) => (event) => {
        if (!under(event)) setSel(key);
    };

    // The arrows go round the ring, Home and End to its ends; Enter and Space
    // are the buttons' own, Tab leaves the pie.
    const onKey = (event) => {
        const list = [...menu.current.querySelectorAll(".piesec, .piehub")];
        const here = list.indexOf(document.activeElement);
        const go = (i) => list[(i + list.length) % list.length].focus({ preventScroll: true });
        if (event.key === "ArrowRight" || event.key === "ArrowDown") go(here + 1);
        else if (event.key === "ArrowLeft" || event.key === "ArrowUp") go(here < 0 ? -1 : here - 1);
        else if (event.key === "Home") go(0);
        else if (event.key === "End") go(-1);
        else if (event.key === "Tab") closer();
        else return;
        event.preventDefault();
        event.stopPropagation();
    };

    const many = ring.length > MANY;
    const reach = at.ring + PUSH + GROW;
    return html`
        <div class="pie" onPointerDownCapture=${(event) => { armed.current = event.target; }}
             onContextMenu=${(event) => event.preventDefault()}>
            <div class="piescrim" onClick=${pressed(() => closer())}></div>
            ${raised && raised.from && html`<div class="piewas" style=${px(raised.from)}></div>`}
            ${raised && html`<${Lift} el=${lift} box=${raised.box} />`}
            <${Plate} at=${at} item=${chosen} name=${name} lifting=${held || (Boolean(sel) && lifted === sel)} />
            <${Ring} at=${at} sectors=${sectors} />
            <div class=${`piemenu${many ? " piemany" : ""}`} role="menu" aria-label=${label} tabindex="-1" ref=${menu}
                 style=${`left:${f1(at.left)}px;top:${f1(at.top)}px;width:${at.side}px;height:${at.side}px;clip-path:circle(${reach}px)`}
                 onKeyDown=${onKey}>
                ${sectors.map((s) => s && html`
                    <button key=${s.it.key} type="button" role=${s.it.on === undefined ? "menuitem" : "menuitemcheckbox"}
                            class=${`piesec${s.on ? " pieon" : ""}${s.it.danger ? " piedanger" : ""}`}
                            style=${`clip-path:path('${s.d}')`} aria-label=${s.it.title}
                            aria-disabled=${s.it.off ? "true" : undefined} aria-description=${s.it.off || undefined}
                            aria-haspopup=${s.it.items ? "menu" : undefined}
                            aria-checked=${s.it.on === undefined ? undefined : String(Boolean(s.it.on))}
                            data-key=${s.it.key} data-slot=${s.slot} onFocus=${stand(s.it.key)}
                            onClick=${pressed((event) => choose(s.it, event.detail === 0))}>
                        <span class="pieword" style=${`left:${f1(s.mid.x)}px;top:${f1(s.mid.y)}px`} aria-hidden="true">
                            ${s.it.icon()}
                            <span>${(s.it.lines || [s.it.word]).map((line, i) => html`${i > 0 && html`<br />`}${line}`)}</span>
                            ${s.it.off && s.it.short && html`<i>${s.it.short}</i>`}
                        </span>
                    </button>
                `)}
                <button type="button" role="menuitem" class=${`piehub${more ? " pieback" : ""}`}
                        style=${`left:${f1(at.side / 2 - HUB)}px;top:${f1(at.side / 2 - HUB)}px;width:${2 * HUB}px;height:${2 * HUB}px`}
                        aria-label=${more ? "Back to the first ring" : "Close the menu"}
                        onFocus=${stand("")} onClick=${pressed((event) => hub(event.detail === 0))}>
                    <b aria-hidden="true">${more ? "Back" : name}</b>
                    <span aria-hidden="true">${more ? "to the first ring" : "tap here to close"}</span>
                </button>
            </div>
        </div>
    `;
}

// useHold makes a long press of the element of ref open what onHold opens,
// with the pointer of the press, which is still held, and where it went down;
// a right click, the menu key and a reader's long press — the contextmenu of
// the element — open it with none. A press that moves or scrolls before its
// time is a scroll, not a press, and the click that ends a press which opened
// something does not go on to the element. onHold is null for an element with
// nothing to open: it keeps its own contextmenu, and the press is the
// browser's.
export function useHold(ref, onHold) {
    const state = useRef({ timer: 0, fire: null, off: null, fired: false });
    const stop = () => {
        const s = state.current;
        clearTimeout(s.timer);
        s.timer = 0;
        s.fire = null;
        if (s.off) s.off();
        s.off = null;
    };
    useEffect(() => stop, []);
    // A finger that held and slides on is choosing, not scrolling the list
    // or turning its page. The browser asks whether a move scrolls before it
    // scrolls, so the listener is there from the start, and not passive.
    const active = Boolean(onHold);
    useEffect(() => {
        const el = ref.current;
        if (!active || !el) return undefined;
        const keep = (event) => {
            if (state.current.fired && event.cancelable) event.preventDefault();
        };
        el.addEventListener("touchmove", keep, { passive: false });
        return () => el.removeEventListener("touchmove", keep);
    }, [active]);
    if (!onHold) return {};

    const down = (event) => {
        const s = state.current;
        stop();
        s.fired = false;
        if (!event.isPrimary || event.button !== 0) return;
        const { pointerId, clientX: x, clientY: y } = event;
        const moved = (e) => {
            if (e.pointerId === pointerId && Math.hypot(e.clientX - x, e.clientY - y) > HOLD_SLOP) stop();
        };
        const ended = (e) => {
            if (e.pointerId === pointerId) stop();
        };
        document.addEventListener("pointermove", moved, true);
        document.addEventListener("pointerup", ended, true);
        document.addEventListener("pointercancel", ended, true);
        window.addEventListener("scroll", stop, true);
        s.off = () => {
            document.removeEventListener("pointermove", moved, true);
            document.removeEventListener("pointerup", ended, true);
            document.removeEventListener("pointercancel", ended, true);
            window.removeEventListener("scroll", stop, true);
        };
        s.fire = () => {
            stop();
            s.fired = true;
            if (navigator.vibrate) navigator.vibrate(12);
            onHold({ pointerId, x, y });
        };
        s.timer = setTimeout(() => s.fire && s.fire(), HOLD_MS);
    };
    // A phone sends its own contextmenu for a long press, sooner or later than
    // the pie's time: during a press it is that press.
    const menu = (event) => {
        event.preventDefault();
        const s = state.current;
        if (s.fire) {
            s.fire();
            return;
        }
        onHold(null);
    };
    const click = (event) => {
        if (state.current.fired && event.detail > 0) {
            event.preventDefault();
            event.stopPropagation();
        }
    };
    return { onPointerDown: down, onContextMenu: menu, onClickCapture: click };
}
