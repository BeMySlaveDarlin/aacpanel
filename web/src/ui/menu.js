// Objects share one menu host per shell (desktop/menu.js), so placement,
// focus and dismissal stay the same for a right click and a held press.
import { createContext } from "preact";
import { useContext } from "preact/hooks";

import { html } from "../html.js";
import { useHold } from "./pie.js";
import { useWide } from "./wide.js";

// MenuContext carries the host: menuOf for a right click, holdOf for an
// object's long press, done once an item is chosen, and sheet,
// which shows what an item opens once the menu is down.
export const MenuContext = createContext(null);

// useMenu returns menuOf(Body, props, label) — the handler that opens the
// menu Body draws with props, or undefined for an object with no menu — and
// null where no host stands, where the browser keeps its own menu. The
// handler takes the event, and the element of the object where it is not the
// one the handler is set on.
export function useMenu() {
    const host = useContext(MenuContext);
    return host ? host.menuOf : null;
}

// Only objects with their own hold gesture opt into a menu on the phone;
// messages keep the browser's text selection and context menu.
export function useHoldMenu(ref, Body, props, label) {
    const host = useContext(MenuContext);
    const wide = useWide();
    const hold = useHold(ref, !wide && host && Body
        ? (press, event) => host.holdOf(Body, props, label, ref.current, press, event) : null);
    return wide ? { onContextMenu: host && host.menuOf(Body, props, label) } : hold;
}

// useMenuSheet returns how an item opens a sheet of its own once the menu is
// down: show(label, draw), where draw(close) gives what the sheet holds.
export function useMenuSheet() {
    const host = useContext(MenuContext);
    return host ? host.sheet : null;
}

// A ring or other self-contained layer outlives the menu without gaining a
// sheet around it. Its anchor is the menu item that opened it.
export function useMenuLayer() {
    const host = useContext(MenuContext);
    return host ? host.layer : null;
}

// MenuItem is one thing the menu does. One that cannot be done now stays in
// its place, dimmed, and says why on its tip, as the buttons of the panel
// do: aria-disabled rather than disabled, since a disabled button takes
// neither the pointer nor the focus its tip is read by. note is a short word
// under the label; tip is the longer one an item that can be pressed shows.
export function MenuItem({ label, note = "", tip = "", off = "", onPick }) {
    const host = useContext(MenuContext);
    return html`
        <button type="button" role="menuitem" class="pkrow" aria-disabled=${off ? "true" : undefined}
                data-tip=${off || tip || undefined} data-tipside="left"
                onClick=${(event) => {
                    if (off) return;
                    host.done();
                    onPick(event);
                }}>
            <span class="pkbody">
                <span class="pkname">${label}</span>
                ${note && html`<span class="pkdesc">${note}</span>`}
            </span>
            <span class="pknum" aria-hidden="true"></span>
        </button>
    `;
}

// MenuList lays the items of a menu out in their groups, a line between two
// groups; an item left out is false, and a group with nothing left in it
// takes no line.
export function MenuList({ groups }) {
    const shown = groups.map((group) => group.filter(Boolean)).filter((group) => group.length > 0);
    return shown.flatMap((group, i) => (i > 0
        ? [html`<div key=${`sep${i}`} class="pkmenusep" role="separator"></div>`, ...group]
        : group));
}
