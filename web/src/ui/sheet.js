// Bottom sheet and the scrim under it, shared by every popup panel.
import { useEffect, useRef, useState } from "preact/hooks";

import { html } from "../html.js";
import { Icon } from "./icons.js";
import { useBackClose } from "./back.js";
import { useWide } from "./wide.js";

const STOPS = ["peek", "half", "full"];

const FLING = 500;

const DRAG_MIN = 60;
const TAP_MAX = 6;

// What the finger asked for. The axis it travelled along decides the owner of the
// gesture: a sideways move belongs to the content under it, and without that answer
// a swipe across a wide table reaches the sheet as a tap and changes its height.
export function gestureKind(dx, dy) {
    const sideways = Math.abs(dx);
    const vertical = Math.abs(dy);
    if (sideways <= TAP_MAX && vertical <= TAP_MAX) return "tap";
    return sideways > vertical ? "sideways" : "drag";
}

// Whether the finger landed inside a box that scrolls sideways — a wide table, a row
// of tabs. Such a box owns the gesture from the first pixel: the sheet following the
// hand and only then letting go would still have moved under a person reading a table.
function scrollsSideways(node, sheet) {
    for (let el = node; el && el !== sheet; el = el.parentElement) {
        if (!(el.scrollWidth - el.clientWidth > 1)) continue;
        const how = getComputedStyle(el).overflowX;
        if (how === "auto" || how === "scroll") return true;
    }
    return false;
}

// Sheet decides only how to show the content, never what. A sheet carrying a
// document says so: on a wide screen the rest of them are dialogs the width of
// a question, and a piece read for forty minutes in that window is a column of
// four words.
export function Sheet({ open, onClose, children, label, inner = false, side = false, doc = false }) {
    const wide = useWide();
    const [stop, setStop] = useState("half");
    const [dragging, setDragging] = useState(false);
    const drag = useRef(null);
    const sheetRef = useRef(null);
    const trackingRef = useRef(null);

    useEffect(() => {
        if (open) setStop("half");
    }, [open]);

    const stopTracking = () => {
        const handlers = trackingRef.current;
        if (!handlers) return;
        document.removeEventListener("pointermove", handlers.move);
        document.removeEventListener("pointerup", handlers.up);
        document.removeEventListener("pointercancel", handlers.up);
        trackingRef.current = null;
    };
    useEffect(() => stopTracking, []);

    const onDown = (event) => {
        if (wide) return;
        const body = event.target.closest?.(".sheetbody");
        const scrolls = Boolean(body) && body.scrollHeight - body.clientHeight > 1;
        if (scrolls && body.scrollTop > 0) return;
        if (scrollsSideways(event.target, sheetRef.current)) return;

        const onControl = Boolean(
            event.target.closest?.("button, a, input, textarea, select, label, [role='button']"),
        );
        drag.current = {
            x: event.clientX, y: event.clientY, at: event.timeStamp, dx: 0, dy: 0,
            guard: scrolls ? body : null, onControl,
        };
        setDragging(true);
        stopTracking();
        const move = (e) => onMove(e);
        const up = (e) => {
            stopTracking();
            setDragging(false);
            onUp(e);
        };
        document.addEventListener("pointermove", move);
        document.addEventListener("pointerup", up);
        document.addEventListener("pointercancel", up);
        trackingRef.current = { move, up };
    };
    const onMove = (event) => {
        const state = drag.current;
        if (!state || !sheetRef.current) return;
        if (state.guard && event.clientY < state.y) {
            drag.current = null;
            sheetRef.current.style.transform = "";
            return;
        }
        const dx = event.clientX - state.x;
        const dy = event.clientY - state.y;
        // Once the finger has gone sideways the gesture is the content's for good:
        // taking it back halfway would jump the sheet under a hand that is still moving.
        if (gestureKind(dx, dy) === "sideways") {
            drag.current = null;
            sheetRef.current.style.transform = "";
            return;
        }
        state.dx = dx;
        state.dy = dy;
        const shift = state.dy < 0 ? Math.max(state.dy / 3, -70) : state.dy;
        sheetRef.current.style.transform = `translateY(${shift}px)`;
    };
    const onUp = (event) => {
        const state = drag.current;
        drag.current = null;
        if (!state || !sheetRef.current) return;
        sheetRef.current.style.transform = "";

        const speed = (Math.abs(state.dy) / Math.max(event.timeStamp - state.at, 1)) * 1000;
        const at = STOPS.indexOf(stop);
        const kind = gestureKind(state.dx, state.dy);
        if (kind === "sideways") return;
        if (kind === "tap") {
            if (!state.onControl) setStop(STOPS[(at + 1) % STOPS.length]);
            return;
        }
        if (speed < FLING && Math.abs(state.dy) < DRAG_MIN) return;
        if (state.dy > 0) {
            if (at === 0) onClose();
            else setStop(STOPS[at - 1]);
            return;
        }
        setStop(STOPS[Math.min(at + 1, STOPS.length - 1)]);
    };
    useEffect(() => {
        if (!open) return undefined;
        const onKey = (event) => {
            if (event.key === "Escape") onClose();
        };
        document.addEventListener("keydown", onKey);
        return () => document.removeEventListener("keydown", onKey);
    }, [open, onClose]);

    const closeStack = useBackClose(open, onClose);

    return html`
        <div class=${`scrim${open && (wide || stop !== "peek") ? " on" : ""}${inner ? " inner" : ""}${side ? " side" : ""}`}
             onClick=${closeStack}></div>
        <div
            ref=${sheetRef}
            class=${`sheet${open ? " on" : ""}${inner ? " inner" : ""}${doc ? " doc" : ""} ${wide ? `win${side ? " side" : ""}` : `draggable ${stop}${dragging ? " dragging" : ""}`}`}
            role="dialog"
            aria-modal=${open ? "true" : "false"}
            aria-label=${label || "action"}
            aria-hidden=${open ? "false" : "true"}
            onPointerDown=${onDown}
        >
            ${!wide && html`<div class="grab"><div class="grip"></div></div>`}
            <button class="sheetclose" type="button" aria-label="close" onClick=${onClose}>
                ${Icon.close()}
            </button>
            <div class="sheetbody">${open && children}</div>
        </div>
    `;
}
