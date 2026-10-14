// The menus of the feed at a desk: of a message, a letter, a path in the
// words of a message, a file and a picture. The rows are drawn on the phone
// as well, where nothing asks for a menu and a right click stays the
// browser's. An item does what the row's own button does, through the same
// code: a message has what a person does with words — copy them, quote them,
// write them again — one in the queue what the buttons under it do, and one
// that did not go out what its row offers.

import { html } from "../../html.js";
import { useToast } from "../../ui/toasts.js";
import { MenuItem, MenuList, useMenu } from "../../ui/menu.js";
import { useSendAgain } from "./again.js";
import { copyText } from "./copy.js";
import { saveAs } from "./look.js";
import { CUT_EDIT, useTakeBack } from "./takeback.js";

// MessageMenu is the menu of a bubble: md is the message as markdown, quote
// and edit put it into the composer, where there is one; queue is what takes
// a message back from the queue (see useTakeBack), again the second attempt
// at one that did not go out (see useSendAgain).
export function MessageMenu(props) {
    if (props.queue) return html`<${QueuedMenu} ...${props} />`;
    if (props.again) return html`<${FailedMenu} ...${props} />`;
    return html`<${WordsMenu} ...${props} />`;
}

function WordsMenu({ md, quote, edit }) {
    const toast = useToast();
    return html`<${MenuList} groups=${[[
        html`<${MenuItem} key="copy" label="Copy" onPick=${() => copyText(md, toast)} />`,
        quote && html`<${MenuItem} key="quote" label="Quote in the composer" onPick=${quote} />`,
        edit && html`<${MenuItem} key="edit" label="Edit" note="into the composer" onPick=${edit} />`,
    ]]} />`;
}

function QueuedMenu({ queue }) {
    const { take } = useTakeBack(queue);
    return html`<${MenuList} groups=${[[
        html`<${MenuItem} key="edit" label="Edit" note="takes it back into the composer" off=${queue.row.cut ? CUT_EDIT : ""}
                          onPick=${() => take(true)} />`,
        html`<${MenuItem} key="back" label="Take back" onPick=${() => take(false)} />`,
    ]]} />`;
}

function FailedMenu({ md, again }) {
    const toast = useToast();
    const second = useSendAgain(again.again, again.onDone);
    return html`<${MenuList} groups=${[[
        md && html`<${MenuItem} key="copy" label="Copy" onPick=${() => copyText(md, toast)} />`,
        html`<${MenuItem} key="again" label="Esc and send again" off=${second.ready ? "" : second.why}
                          onPick=${second.press} />`,
    ]]} />`;
}

// LetterMenu copies a letter whole, as the copy of its head does.
export function LetterMenu({ text }) {
    const toast = useToast();
    return html`<${MenuList} groups=${[[html`<${MenuItem} key="copy" label="Copy" onPick=${() => copyText(text, toast)} />`]]} />`;
}

function PathMenu({ path, onOpen }) {
    const toast = useToast();
    return html`<${MenuList} groups=${[[
        html`<${MenuItem} key="open" label="Open" onPick=${onOpen} />`,
        html`<${MenuItem} key="copy" label="Copy the path" onPick=${() => copyText(path, toast)} />`,
    ]]} />`;
}

// usePathMenu is how a feed opens the menu of a path in the words of a
// message. A path is not a component of its own but a piece of the words, so
// the feed finds it under the click, and does so on the way down, before the
// message it stands in: the menu of the message would take the click
// otherwise. onOpen opens a path as a press on it does.
export function usePathMenu(onOpen) {
    const menu = useMenu();
    if (!menu) return undefined;
    return (event) => {
        const hit = event.target instanceof Element ? event.target.closest(".path") : null;
        if (!hit) return;
        const path = hit.dataset.path;
        menu(PathMenu, { path, onOpen: () => onOpen(path) }, `path ${path}`)(event, hit);
    };
}

// FileMenu is the menu of a file row: opened as a press opens it, its path,
// and saved by the route the viewer saves it by (onSave), where the row knows
// its conversation.
export function FileMenu({ file, onOpen, onSave }) {
    const toast = useToast();
    return html`<${MenuList} groups=${[
        [
            html`<${MenuItem} key="open" label="Open" onPick=${() => onOpen(file)} />`,
            html`<${MenuItem} key="copy" label="Copy the path" onPick=${() => copyText(file.path, toast)} />`,
        ],
        [onSave && html`<${MenuItem} key="save" label="Download" onPick=${() => onSave(file)} />`],
    ]} />`;
}

// ShotMenu is the menu of a picture: opened as a press opens it, and saved
// under the name the viewer saves it by once it has come.
export function ShotMenu({ url, name, error, onOpen }) {
    return html`<${MenuList} groups=${[[
        html`<${MenuItem} key="open" label="Open" onPick=${onOpen} />`,
        html`<${MenuItem} key="save" label="Download" off=${error || (url ? "" : "the picture has not come yet")}
                          onPick=${() => saveAs(url, name)} />`,
    ]]} />`;
}
