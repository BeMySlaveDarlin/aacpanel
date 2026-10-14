// The menu of an object, opened by a right click on the wide screen: what can
// be done to a session, a conversation, a container, in place of the
// browser's own menu. An object asks for its menu here and draws nothing of
// it: the one host the desktop shell mounts opens it, places it and takes it
// down (desktop/menu.js). Outside that host there is no menu to ask for, and
// the object leaves the right click to the browser, so a component drawn on
// both screens asks the same way on either.
import { createContext } from "preact";
import { useContext } from "preact/hooks";

import { html } from "../html.js";

// MenuContext carries the host: menuOf, which makes the handler of a right
// click, done, which takes the menu down once an item is chosen, and sheet,
// which shows what an item opens once the menu is down.
export const MenuContext = createContext(null);

// useMenu returns menuOf(Body, props, label) — the handler that opens the
// menu Body draws with props, or undefined for an object with no menu — and
// null where no host stands, where the browser keeps its own menu.
export function useMenu() {
    const host = useContext(MenuContext);
    return host ? host.menuOf : null;
}

// useMenuSheet returns how an item opens a sheet of its own once the menu is
// down: show(label, draw), where draw(close) gives what the sheet holds.
export function useMenuSheet() {
    const host = useContext(MenuContext);
    return host ? host.sheet : null;
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
                onClick=${() => {
                    if (off) return;
                    host.done();
                    onPick();
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
