// An attachment full screen: the layer a picture is examined in.
import { useEffect, useRef, useState } from "preact/hooks";

import { html } from "../../html.js";
import { BackHead, useBackClose } from "../../ui/back.js";
import { FIT, mid, nextScale, panBy, span, zoomAt } from "./zoom.js";

const TAP_MS = 300;
const TAP_MOVE = 12;

const EXT = {
    "image/png": "png",
    "image/jpeg": "jpg",
    "image/jpg": "jpg",
    "image/webp": "webp",
    "image/gif": "gif",
    "image/avif": "avif",
    "image/heic": "heic",
    "image/svg+xml": "svg",
};

// shotName returns the name an attachment is saved to the device under.
export function shotName(shot, pos) {
    const ext = EXT[(shot && shot.media) || ""] || "png";
    return `attachment-${pos}-${(shot && shot.index) || 0}.${ext}`;
}

// Photo renders the layer one picture is viewed in.
export function Photo({ url, name, error, onClose, head: title }) {
    const stage = useRef(null);
    const [view, setView] = useState(FIT);
    const [image, setImage] = useState({ w: 0, h: 0 });
    const [box, setBox] = useState({ w: 0, h: 0 });
    const [snap, setSnap] = useState(false);

    const boxAt = useRef(box);
    const imageAt = useRef(image);
    const viewAt = useRef(view);
    boxAt.current = box;
    imageAt.current = image;
    viewAt.current = view;

    useBackClose(true, onClose);

    useEffect(() => setView(FIT), [url]);

    useEffect(() => {
        const el = stage.current;
        if (!el) return undefined;
        const measure = () => setBox({ w: el.clientWidth, h: el.clientHeight });
        measure();
        if (typeof ResizeObserver === "undefined") return undefined;
        const eye = new ResizeObserver(measure);
        eye.observe(el);
        return () => eye.disconnect();
    }, []);

    const points = useRef(new Map());
    const start = useRef(null);
    const tap = useRef({ at: 0, x: 0, y: 0 });
    const watching = useRef(null);

    const spot = (event) => {
        const el = stage.current;
        if (!el) return { x: 0, y: 0 };
        const rect = el.getBoundingClientRect();
        return {
            x: event.clientX - (rect.left + rect.width / 2),
            y: event.clientY - (rect.top + rect.height / 2),
        };
    };

    const pair = () => {
        const both = [...points.current.values()];
        return both.length >= 2 ? [both[0], both[1]] : null;
    };

    const unwatch = () => {
        const on = watching.current;
        if (!on) return;
        document.removeEventListener("pointermove", on.move);
        document.removeEventListener("pointerup", on.up);
        document.removeEventListener("pointercancel", on.up);
        watching.current = null;
    };
    useEffect(() => unwatch, []);

    const onMove = (event) => {
        if (!points.current.has(event.pointerId)) return;
        const was = points.current.get(event.pointerId);
        const now = spot(event);
        points.current.set(event.pointerId, now);
        const two = pair();
        if (two && start.current && start.current.span > 0) {
            const k = span(two[0], two[1]) / start.current.span;
            setView(zoomAt(start.current.view, start.current.view.scale * k,
                mid(two[0], two[1]), boxAt.current, imageAt.current));
            return;
        }
        if (points.current.size !== 1) return;
        setView((v) => panBy(v, now.x - was.x, now.y - was.y, boxAt.current, imageAt.current));
    };

    const onUp = (event) => {
        const from = points.current.get(event.pointerId);
        const pinched = Boolean(start.current && start.current.span > 0);
        points.current.delete(event.pointerId);
        if (points.current.size > 0) {
            start.current = { view: viewAt.current, span: 0 };
            return;
        }
        unwatch();
        start.current = null;
        const now = spot(event);
        const moved = from ? Math.hypot(now.x - from.x, now.y - from.y) : 0;
        if (pinched || moved > TAP_MOVE) {
            tap.current = { at: 0, x: 0, y: 0 };
            return;
        }
        const twice = event.timeStamp - tap.current.at < TAP_MS
            && Math.hypot(now.x - tap.current.x, now.y - tap.current.y) <= TAP_MOVE;
        if (!twice) {
            tap.current = { at: event.timeStamp, x: now.x, y: now.y };
            return;
        }
        tap.current = { at: 0, x: 0, y: 0 };
        setSnap(true);
        setView((v) => zoomAt(v, nextScale(v, boxAt.current, imageAt.current),
            now, boxAt.current, imageAt.current));
    };

    const onDown = (event) => {
        setSnap(false);
        points.current.set(event.pointerId, spot(event));
        const two = pair();
        start.current = {
            view: viewAt.current,
            span: two ? span(two[0], two[1]) : 0,
        };
        if (watching.current) return;
        const move = (e) => onMove(e);
        const up = (e) => onUp(e);
        document.addEventListener("pointermove", move);
        document.addEventListener("pointerup", up);
        document.addEventListener("pointercancel", up);
        watching.current = { move, up };
    };

    const zoomed = view.scale > 1.01;
    const head = html`
        <div class="chatwho">
            <h2>${title || "Attachment"}</h2>
            <div class="chatsub">
                <span>${image.w > 0 ? `${image.w} × ${image.h}` : "image"}</span>
                ${zoomed && html`<span class="sep">·</span><span>${Math.round(view.scale * 100)}%</span>`}
            </div>
        </div>
    `;
    return html`
        <div class="pv">
            <${BackHead} onBack=${onClose} label="close" tools=${url && html`
                <a class="pvget" href=${url} download=${name || "attachment"}>Save</a>
            `}>${head}<//>
            <div class="pvstage" ref=${stage} onPointerDown=${onDown}>
                ${error && html`<p class="hint crit">${error}</p>`}
                ${!url && !error && html`<p class="hint">Reading the attachment…</p>`}
                ${url && html`
                    <img class=${`pvpic${zoomed ? " zoomed" : ""}${snap ? " snap" : ""}`} src=${url}
                         alt=${name || "attachment"} decoding="async" draggable="false"
                         style=${`transform: translate(${view.x}px, ${view.y}px) scale(${view.scale})`}
                         onLoad=${(e) => setImage({
                             w: e.currentTarget.naturalWidth,
                             h: e.currentTarget.naturalHeight,
                         })} />
                `}
            </div>
            ${url && !error && html`<p class="pvhint">double tap to zoom in, two fingers to scale</p>`}
        </div>
    `;
}
