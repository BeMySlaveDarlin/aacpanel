// Scale and offset of a picture in the viewer: pure arithmetic, no DOM.

// MAX is the ceiling of the scale.
export const MAX = 6;

// DOUBLE is the scale a double tap zooms to.
export const DOUBLE = 2.5;

const MIN = 1;

// FIT is the starting position: fitted and centred.
export const FIT = { scale: MIN, x: 0, y: 0 };

function hold(value, edge) {
    if (!(edge > 0)) return 0;
    return Math.min(Math.max(value, -edge), edge);
}

// drawn returns the size of the picture fitted into the stage by contain.
export function drawn(box, image) {
    if (!box || !image || !(image.w > 0) || !(image.h > 0)) return { w: 0, h: 0 };
    const k = Math.min(box.w / image.w, box.h / image.h);
    return { w: image.w * k, h: image.h * k };
}

// limits returns how far a picture of this size may travel from the centre.
export function limits(box, size) {
    return {
        x: Math.max(0, (size.w - box.w) / 2),
        y: Math.max(0, (size.h - box.h) / 2),
    };
}

// clamp brings the view back into what is allowed.
export function clamp(view, box, image) {
    const scale = Math.min(Math.max(view.scale || MIN, MIN), MAX);
    const fit = drawn(box, image);
    const edge = limits(box, { w: fit.w * scale, h: fit.h * scale });
    return { scale, x: hold(view.x || 0, edge.x), y: hold(view.y || 0, edge.y) };
}

// zoomAt changes the scale, keeping the point under the finger in place.
export function zoomAt(view, scale, point, box, image) {
    const was = view.scale || MIN;
    const next = Math.min(Math.max(scale, MIN), MAX);
    const k = next / was;
    const at = point || { x: 0, y: 0 };
    return clamp({
        scale: next,
        x: at.x - (at.x - (view.x || 0)) * k,
        y: at.y - (at.y - (view.y || 0)) * k,
    }, box, image);
}

// panBy shifts the picture by what the finger travelled.
export function panBy(view, dx, dy, box, image) {
    return clamp({ scale: view.scale, x: (view.x || 0) + dx, y: (view.y || 0) + dy }, box, image);
}

// nextScale returns where a double tap leads: zoom in or back as it was.
export function nextScale(view, box, image) {
    if ((view.scale || MIN) > MIN + 0.01) return MIN;
    const fit = drawn(box, image);
    const wide = fit.w > 0 ? box.w / fit.w : 1;
    return Math.min(Math.max(DOUBLE, wide), MAX);
}

// span returns the distance between two fingers.
export function span(a, b) {
    return Math.hypot(a.x - b.x, a.y - b.y);
}

export function mid(a, b) {
    return { x: (a.x + b.x) / 2, y: (a.y + b.y) / 2 };
}
