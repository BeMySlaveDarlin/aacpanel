// The pie of a card on the phone, as a fixture works it: opened by the
// card's contextmenu — the way the keyboard and a reader reach it — or by a
// press held on the card, its sectors pressed as the keyboard presses them,
// and closed with Escape. Every way out waits for the step back of the
// history to land: a layer that leaves it on its way stalls the next page
// of the run. Nothing here waits for the page to fall quiet: a fixture may
// hold an answer of its fake host back on purpose while it reads the pie.

export const pie = () => document.querySelector(".pie");
export const sector = (key) => document.querySelector(`.pie .piesec[data-key="${key}"]`);
export const hub = () => document.querySelector(".pie .piehub");
// plate reads the plate over the ring: its title and its note, a dot between.
export const plate = () => {
    const el = document.querySelector(".pie .pieplate");
    return el ? [...el.children].map((part) => part.textContent.replace(/\s+/g, " ").trim()).join(" · ") : "";
};

export async function until(ok, what = "the pie", ms = 4000) {
    const end = performance.now() + ms;
    while (!ok() && performance.now() < end) await new Promise((r) => requestAnimationFrame(r));
    if (!ok()) throw new Error(`${what} did not come`);
}

// depth is how many layers the history stands on; floor is how many stand
// under every pie of the page — a conversation open beside the list is one.
const depth = () => (history.state && history.state.overlay ? Number(history.state.depth) || 1 : 0);
let floor = 0;

// underPies takes the layers the history stands on now for the floor under
// the pies the page opens next: called once the page has come to rest.
export function underPies() {
    floor = depth();
}

// settled waits for the pie to go and for the history to stand where it
// stands under the pies.
export async function settled() {
    await until(() => !pie() && depth() <= floor, "the history after the pie");
}

// openPie opens the pie of a card through its contextmenu, and says whether
// the menu of the browser was kept from it. It is open once it stands as a
// layer and the focus is inside it.
export async function openPie(card) {
    await settled();
    const event = new MouseEvent("contextmenu", { bubbles: true, cancelable: true });
    card.dispatchEvent(event);
    await until(() => pie() && depth() > floor && pie().contains(document.activeElement), "the pie of the card");
    return event.defaultPrevented;
}

export async function closePie() {
    document.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", bubbles: true, cancelable: true }));
    await settled();
}

// said is what the plate says of a sector the keyboard stands on.
export async function said(key) {
    const el = sector(key);
    const title = el.getAttribute("aria-label");
    el.focus();
    await until(() => plate() === title || plate().startsWith(`${title} · `), `the plate of ${key}`);
    return plate();
}

// item reads a sector of the ring shown: its key, what it says to a reader,
// whether it is off and why, whether it is on.
export function item(el) {
    return {
        key: el.dataset.key,
        slot: Number(el.dataset.slot),
        label: el.getAttribute("aria-label"),
        role: el.getAttribute("role"),
        off: el.getAttribute("aria-disabled") === "true",
        why: el.getAttribute("aria-description") || "",
        on: el.getAttribute("aria-checked") === "true",
        word: el.textContent.replace(/\s+/g, " ").trim(),
    };
}

// ring reads the ring shown, each sector with the slot it stands in.
export function ring() {
    const el = pie();
    if (!el) return null;
    return [...el.querySelectorAll(".piesec")].map(item);
}

// press sends the pointer events of a finger on an element, at a point of
// the screen (its middle when none), with the pointer id of the press.
export function press(type, el, at = null, id = 1) {
    const r = el.getBoundingClientRect();
    const point = at || { x: r.left + r.width / 2, y: r.top + r.height / 2 };
    const event = new PointerEvent(type, {
        bubbles: true, cancelable: true, composed: true, pointerId: id, isPrimary: true, pointerType: "touch",
        button: type === "pointermove" ? -1 : 0, buttons: type === "pointerup" ? 0 : 1, clientX: point.x, clientY: point.y,
    });
    el.dispatchEvent(event);
    return point;
}

// middle is the middle of the word of a sector, where a finger points at it.
export function middle(key) {
    const r = sector(key).querySelector(".pieword").getBoundingClientRect();
    return { x: r.left + r.width / 2, y: r.top + r.height / 2 };
}

// tap is a finger's tap on an element of the pie: down, up and the click.
export function tap(el) {
    press("pointerdown", el, null, 2);
    press("pointerup", el, null, 2);
    el.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true, detail: 1 }));
}
