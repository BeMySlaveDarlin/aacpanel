// The one menu of the right click on the wide screen. The shell mounts the
// host once around everything it draws; an object asks for its menu through
// useMenu (ui/menu.js), and the host decides, in one place for every object,
// whether the right click is the panel's or stays the browser's, opens the
// menu at the click, keeps it inside the window, moves through it by the
// keys and takes it down.
//
// The browser keeps its menu where the panel has nothing better to offer:
// with Shift held, in a field, in a terminal, over a selection made inside
// the object — copy is what a right click on selected text is for — and on a
// screen too narrow for the shell.
import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "preact/hooks";

import { html } from "../html.js";
import { Sheet } from "../ui/sheet.js";
import { WIDE } from "../ui/wide.js";
import { MenuContext } from "../ui/menu.js";
import { placeMenu } from "./tip.js";

// NATIVE is what a right click inside keeps the browser's menu for: a field,
// where it pastes and spells, and a terminal, which reads the click itself.
const NATIVE = "input, textarea, select, [contenteditable], .termwrap, .xterm";

// KEYBOARD is the button of a menu the keyboard asks for — the menu key,
// Shift+F10. Chrome sends it as a contextmenu like the mouse does, at the
// middle of the focused element, with no Shift whatever was held, and tells
// it apart by this button alone: the right button is 2.
const KEYBOARD = -1;

// native says whether a right click stays the browser's.
function native(event) {
    if (event.shiftKey) return true;
    if (!window.matchMedia(WIDE).matches) return true;
    const target = event.target instanceof Element ? event.target : null;
    if (target && target.closest(NATIVE)) return true;
    const sel = document.getSelection();
    return Boolean(sel && !sel.isCollapsed && sel.rangeCount > 0 && sel.getRangeAt(0).intersectsNode(event.currentTarget));
}

// anchorOf says where the menu opens from: the point of a click, or the
// object the keyboard stands on — the middle of it, where the browser puts
// the point, would lay the menu over the very thing it is about.
function anchorOf(event, from) {
    if (event.button === KEYBOARD) return from.getBoundingClientRect();
    const x = event.clientX;
    const y = event.clientY;
    return { left: x, right: x, top: y, bottom: y };
}

function items(node) {
    return Array.from(node.querySelectorAll('[role="menuitem"]'));
}

function available(item) {
    return item.getAttribute("aria-disabled") !== "true";
}

export function MenuHost({ children }) {
    const [menu, setMenu] = useState(null);
    const [sheet, setSheet] = useState(null);
    const [spot, setSpot] = useState(null);
    const node = useRef(null);
    // What the menu was opened from, the focus goes back to as it closes: a
    // menu the keyboard opened leaves the keyboard where it was.
    const from = useRef(null);
    const seq = useRef(0);

    const back = useCallback(() => {
        const el = from.current;
        if (el && el.isConnected) el.focus({ preventScroll: true });
    }, []);

    const close = useCallback(() => {
        setMenu(null);
        setSpot(null);
        back();
    }, [back]);

    const open = useCallback((event, Body, props, label) => {
        if (event.defaultPrevented || native(event)) return;
        event.preventDefault();
        const el = event.currentTarget;
        from.current = el;
        seq.current += 1;
        setSpot(null);
        setMenu({ Body, props, label, at: anchorOf(event, el), key: seq.current });
    }, []);

    const menuOf = useCallback((Body, props, label) => (Body
        ? (event) => open(event, Body, props, label)
        : undefined), [open]);

    const showSheet = useCallback((label, draw) => setSheet({ label, draw }), []);
    const closeSheet = useCallback(() => {
        setSheet(null);
        back();
    }, [back]);

    const host = useMemo(() => ({ menuOf, done: close, sheet: showSheet }), [menuOf, close, showSheet]);

    // The menu is measured and placed before it is first painted, and again
    // when what it holds changes size — an item that waited on the host goes
    // or changes its words.
    useLayoutEffect(() => {
        const el = node.current;
        if (!menu || !el) return undefined;
        const place = () => setSpot(placeMenu(menu.at, el.getBoundingClientRect(), window.innerWidth, window.innerHeight));
        place();
        const first = items(el).find(available);
        (first || el).focus({ preventScroll: true });
        const sized = new ResizeObserver(place);
        sized.observe(el);
        return () => sized.disconnect();
    }, [menu]);

    useEffect(() => {
        if (!menu) return undefined;
        const away = (event) => {
            if (!node.current || !node.current.contains(event.target)) close();
        };
        const keys = (event) => {
            const el = node.current;
            if (!el) return;
            const list = items(el);
            const at = list.indexOf(document.activeElement);
            const go = (i) => {
                const item = list[(i + list.length) % list.length];
                if (item) item.focus({ preventScroll: true });
            };
            const fire = (item) => {
                if (item && available(item)) item.click();
            };
            const digit = /^[1-9]$/.test(event.key) && !event.altKey && !event.ctrlKey && !event.metaKey;
            if (event.key === "Escape" || event.key === "Tab") close();
            else if (event.key === "ArrowDown") go(at + 1);
            else if (event.key === "ArrowUp") go(at < 0 ? -1 : at - 1);
            else if (event.key === "Home") go(0);
            else if (event.key === "End") go(-1);
            else if (event.key === "Enter" || event.key === " ") fire(list[at]);
            else if (digit) fire(list[Number(event.key) - 1]);
            else return;
            // The menu has the keys while it is open: an arrow is not the
            // next session, Escape is not the panel at the right.
            event.preventDefault();
            event.stopPropagation();
        };
        document.addEventListener("pointerdown", away, true);
        window.addEventListener("wheel", away, { capture: true, passive: true });
        window.addEventListener("scroll", away, true);
        window.addEventListener("keydown", keys, true);
        window.addEventListener("blur", close);
        window.addEventListener("resize", close);
        return () => {
            document.removeEventListener("pointerdown", away, true);
            window.removeEventListener("wheel", away, { capture: true, passive: true });
            window.removeEventListener("scroll", away, true);
            window.removeEventListener("keydown", keys, true);
            window.removeEventListener("blur", close);
            window.removeEventListener("resize", close);
        };
    }, [menu, close]);

    // A press follows the pointer, as in the browser's own menu: the item
    // under it is the one the keys go on from.
    const follow = (event) => {
        const item = event.target instanceof Element ? event.target.closest('[role="menuitem"]') : null;
        if (item && item !== document.activeElement) item.focus({ preventScroll: true });
    };

    const Body = menu && menu.Body;
    return html`
        <${MenuContext.Provider} value=${host}>
            ${children}
            ${menu && html`
                <div key=${menu.key} class="pkmenu ctxmenu" role="menu" aria-label=${menu.label}
                     tabindex="-1" ref=${node}
                     style=${spot ? `transform: translate(${spot.x}px, ${spot.y}px)` : ""}
                     onPointerMove=${follow}
                     onContextMenu=${(event) => event.preventDefault()}>
                    <${Body} ...${menu.props} />
                </div>
            `}
            <${Sheet} open=${Boolean(sheet)} onClose=${closeSheet} label=${sheet ? sheet.label : ""}>
                ${sheet && sheet.draw(closeSheet)}
            <//>
        <//>
    `;
}
