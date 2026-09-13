// A file as an attachment: the row in the feed and in the call details, and
// the card of the files a call delivered to the human.
import { useLayoutEffect, useRef, useState } from "preact/hooks";

import { html } from "../../html.js";
import { Icon } from "../../ui/icons.js";
import { bytes } from "../../format.js";
import { render } from "../../md.js";
import { stampText } from "./labels.js";

const IMAGE = /\.(png|jpe?g|gif|webp|avif|bmp|ico|svg)$/i;

const HEAD = 3;

const PATH_KEYS = ["file_path", "notebook_path"];

// fileOfCall returns the file the call was about, or null.
export function fileOfCall(args) {
    if (!args) return null;
    let data;
    try {
        data = JSON.parse(args);
    } catch {
        return null;
    }
    if (!data || typeof data !== "object") return null;
    for (const key of PATH_KEYS) {
        const value = data[key];
        if (typeof value === "string" && value.trim()) {
            const path = value.trim();
            return { path, name: path.split("/").pop() || path };
        }
    }
    return null;
}

// isImage reports whether the file is drawn as a picture. A file that comes
// with its type is judged by the type: a delivered PDF carries one too, and
// its presence alone would make a picture of it.
export function isImage(file) {
    if (!file) return false;
    if (file.media) return /^image\//.test(file.media);
    return IMAGE.test(file.name || file.path || "");
}

const TAG = /^[a-z0-9]{1,5}$/i;

// fileTag returns the type of a file the way the human picks one: by the
// extension, and by the media type when the name has none.
export function fileTag(file) {
    const name = String((file && file.name) || "");
    const dot = name.lastIndexOf(".");
    const ext = dot > 0 ? name.slice(dot + 1) : "";
    if (TAG.test(ext)) return ext.toUpperCase();
    const sub = String((file && file.media) || "").split("/")[1] || "";
    return TAG.test(sub) ? sub.toUpperCase() : "FILE";
}

// FileCard renders an attachment as one row. With a tag the type stands where
// the icon would: a PDF and a text file are one icon, and the type is what
// the human tells them apart by.
//
// A file the reader cannot open — one a session sent from outside the
// directory of the conversation — is not a button: the reader holds every
// file against that directory, and a tap would end in a refusal. The row
// says so under the name instead of promising an opening.
export function FileCard({ file, onOpen, tag }) {
    const image = isImage(file);
    const mark = tag
        ? html`<span class="mftag">${tag}</span>`
        : html`<span class="mfico">${image ? Icon.photo() : Icon.file()}</span>`;
    if (file.outside) {
        return html`
            <div class=${`mfile${tag ? " tagged" : ""} outside`}>
                ${mark}
                <span class="mfname">
                    ${file.name}
                    <span class="mfnote">outside the conversation directory</span>
                </span>
                ${file.size > 0 && html`<span class="mfsize">${bytes(file.size)}</span>`}
            </div>
        `;
    }
    return html`
        <button class=${`mfile${tag ? " tagged" : ""}`} type="button"
                onClick=${() => onOpen && onOpen(file)}
                data-path=${file.path}
                aria-label=${`open ${file.name}`}>
            ${mark}
            <span class="mfname">${file.name}</span>
            ${file.size > 0 && html`<span class="mfsize">${bytes(file.size)}</span>`}
        </button>
    `;
}

// SentCard renders the files a call delivered to the human: the caption the
// session gave them, then one row per file. Every row opens the file the same
// way an attachment named in a reply does.
export function SentCard({ item, onOpen }) {
    const files = item.files || [];
    return html`
        <div class="sent">
            <div class="senthead">
                <span class="sentico">${Icon.clip()}</span>
                <span class="sentlabel">${files.length > 1 ? "files for you" : "file for you"}</span>
                ${item.at && html`<span class="sentat">${stampText(item.at)}</span>`}
            </div>
            ${item.text && html`<div class="sentcap">${render(item.text)}</div>`}
            ${item.cut && html`<p class="hint warn">The caption is longer than shown — cut.</p>`}
            <div class="mflist">
                ${files.map((file) => html`
                    <${FileCard} key=${file.path} file=${file} onOpen=${onOpen} tag=${fileTag(file)} />
                `)}
            </div>
        </div>
    `;
}

// FileAtts renders the attachments of one message as a capped list of rows.
export function FileAtts({ files, onOpen }) {
    const list = files || [];
    const [all, setAll] = useState(false);
    const box = useRef(null);
    const [more, setMore] = useState(false);
    useLayoutEffect(() => {
        const el = box.current;
        if (!el || !all) {
            setMore(false);
            return;
        }
        setMore(el.scrollHeight > el.clientHeight + 1);
    }, [all, list.length]);
    if (!list.length) return null;
    const shown = all ? list : list.slice(0, HEAD);
    const rest = list.length - HEAD;
    return html`
        <div class="mfiles">
            <div ref=${box} class=${`mflist${all ? " mfopen" : ""}${more ? " mfmask" : ""}`}>
                ${shown.map((file) => html`<${FileCard} key=${file.path} file=${file} onOpen=${onOpen} />`)}
            </div>
            ${rest > 0 && html`
                <button class="mfmore" type="button" onClick=${() => setAll(!all)}
                        aria-expanded=${all ? "true" : "false"}>
                    ${all ? "collapse" : `${rest} more`}
                </button>
            `}
        </div>
    `;
}
