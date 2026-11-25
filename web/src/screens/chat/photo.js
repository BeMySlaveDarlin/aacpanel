// A picture in the attachment gallery, with its own zoom and pan gestures.
import { useEffect, useLayoutEffect, useRef, useState } from "preact/hooks";

import { html } from "../../html.js";
import { Gallery } from "../../ui/gallery.js";
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

// shotName returns the name an attachment is saved to the device under. A
// file the panel sent keeps the name it was sent under, and the smaller copy
// the phone drew of it is a JPEG named after it; a picture a call returned is
// named by the place of its result.
export function shotName(shot, pos) {
    const name = shot && (shot.upload || shot.name);
    if (name) return shot.preview ? `${name.replace(/\.[^.]*$/, "")}-preview.jpg` : name;
    const ext = EXT[(shot && shot.media) || ""] || "png";
    if (shot && shot.part != null) return `attachment-${shot.pos}-${shot.index || 0}-${shot.part}.${ext}`;
    return `attachment-${pos}-${(shot && shot.index) || 0}.${ext}`;
}

export function Photo({ url, name, error, onClose, head: title, index, total, onPrevious, onNext }) {
    const [image, setImage] = useState({ w: 0, h: 0 });
    const [scale, setScale] = useState(1);
    return html`
        <${Gallery} label=${title || name || "Attachment"} onClose=${onClose}
                    index=${index} total=${total} onPrevious=${onPrevious} onNext=${onNext}>
            <div class="pvhead">
                <div class="chatwho">
                    <h2>${title || name || "Attachment"}</h2>
                    <div class="chatsub">
                        <span>${image.w > 0 ? `${image.w} × ${image.h}` : "image"}</span>
                        ${scale > 1.01 && html`<span class="sep">·</span><span>${Math.round(scale * 100)}%</span>`}
                    </div>
                </div>
                ${url && html`<a class="pvget" href=${url} download=${name || "attachment"}>Save</a>`}
            </div>
            <${PhotoView} url=${url} name=${name} error=${error} onClose=${onClose}
                onPrevious=${onPrevious} onNext=${onNext} onImage=${setImage} onScale=${setScale} />
        <//>
    `;
}

export function PhotoView({ url, name, error, onClose, onPrevious, onNext, onImage, onScale }) {
    const stage = useRef(null);
    const [view, setView] = useState(FIT);
    const [image, setImage] = useState({ w: 0, h: 0 });
    const [box, setBox] = useState({ w: 0, h: 0 });
    const [snap, setSnap] = useState(false);
    const boxAt = useRef(box), imageAt = useRef(image), viewAt = useRef(view);
    boxAt.current = box;
    imageAt.current = image;
    viewAt.current = view;

    const points = useRef(new Map());
    const gesture = useRef(null);
    const pinch = useRef(null);
    const tap = useRef({ at: 0, x: 0, y: 0 });
    const watching = useRef(null);
    const unwatch = () => {
        const on = watching.current;
        if (!on) return;
        document.removeEventListener("pointermove", on.move);
        document.removeEventListener("pointerup", on.up);
        document.removeEventListener("pointercancel", on.cancel);
        watching.current = null;
    };
    const cancel = () => {
        points.current.clear();
        gesture.current = null;
        pinch.current = null;
        tap.current = { at: 0, x: 0, y: 0 };
        unwatch();
    };
    useEffect(() => cancel, []);
    useLayoutEffect(() => {
        cancel();
        setView(FIT);
        setImage({ w: 0, h: 0 });
    }, [url]);
    useEffect(() => { onImage?.(image); }, [image, onImage]);
    useEffect(() => { onScale?.(view.scale); }, [view.scale, onScale]);
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

    const spot = (event) => {
        const rect = stage.current.getBoundingClientRect();
        return { x: event.clientX - rect.left - rect.width / 2, y: event.clientY - rect.top - rect.height / 2 };
    };
    const pair = () => [...points.current.values()].slice(0, 2);
    const onMove = (event) => {
        if (!points.current.has(event.pointerId)) return;
        const was = points.current.get(event.pointerId), now = spot(event);
        points.current.set(event.pointerId, now);
        const both = pair();
        if (both.length === 2 && pinch.current?.span > 0) {
            const start = pinch.current;
            setView(zoomAt(start.view, start.view.scale * span(...both) / start.span,
                mid(...both), boxAt.current, imageAt.current));
            return;
        }
        if (points.current.size === 1 && viewAt.current.scale > 1.01) {
            setView((v) => panBy(v, now.x - was.x, now.y - was.y, boxAt.current, imageAt.current));
        }
    };
    const onUp = (event) => {
        if (!points.current.has(event.pointerId)) return;
        points.current.delete(event.pointerId);
        pinch.current = null;
        if (points.current.size > 0) return;
        unwatch();
        const start = gesture.current;
        gesture.current = null;
        if (!start) return;
        const now = spot(event), dx = now.x - start.x, dy = now.y - start.y;
        if (!start.pinched && start.scale <= 1.01 && event.pointerType === "touch") {
            if (dy > 60 && dy > Math.abs(dx)) { tap.current.at = 0; onClose?.(); return; }
            if (Math.abs(dx) > 60 && Math.abs(dx) > Math.abs(dy)) {
                tap.current.at = 0;
                (dx > 0 ? onPrevious : onNext)?.();
                return;
            }
        }
        if (start.pinched || Math.hypot(dx, dy) > TAP_MOVE) { tap.current.at = 0; return; }
        const twice = tap.current.at > 0 && event.timeStamp - tap.current.at < TAP_MS
            && Math.hypot(now.x - tap.current.x, now.y - tap.current.y) <= TAP_MOVE;
        if (!twice) { tap.current = { at: event.timeStamp, ...now }; return; }
        tap.current.at = 0;
        setSnap(true);
        setView((v) => zoomAt(v, nextScale(v, boxAt.current, imageAt.current), now, boxAt.current, imageAt.current));
    };
    const onDown = (event) => {
        if (event.button > 0) return;
        event.stopPropagation();
        setSnap(false);
        const now = spot(event);
        points.current.set(event.pointerId, now);
        if (!gesture.current) gesture.current = { ...now, scale: viewAt.current.scale, pinched: false };
        const both = pair();
        if (both.length === 2) {
            gesture.current.pinched = true;
            pinch.current = { view: viewAt.current, span: span(...both) };
        }
        if (watching.current) return;
        const move = (e) => onMove(e), up = (e) => onUp(e), stop = () => cancel();
        document.addEventListener("pointermove", move);
        document.addEventListener("pointerup", up);
        document.addEventListener("pointercancel", stop);
        watching.current = { move, up, cancel: stop };
    };
    const zoomed = view.scale > 1.01;
    return html`
        <div class="pvstage" ref=${stage} onPointerDown=${onDown}>
            ${error && html`<p class="hint crit">${error}</p>`}
            ${!url && !error && html`<p class="hint">Reading the attachment…</p>`}
            ${url && html`<img class=${`pvpic${zoomed ? " zoomed" : ""}${snap ? " snap" : ""}`} src=${url}
                alt=${name || "attachment"} decoding="async" draggable="false"
                style=${`transform: translate(${view.x}px, ${view.y}px) scale(${view.scale})`}
                onLoad=${(event) => setImage({ w: event.currentTarget.naturalWidth, h: event.currentTarget.naturalHeight })} />`}
        </div>
        ${url && !error && html`<p class="pvhint">double tap to zoom in, two fingers to scale</p>`}
    `;
}
