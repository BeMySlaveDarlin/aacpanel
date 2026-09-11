// A file as an attachment: the row in the feed and in the call details.
import { useLayoutEffect, useRef, useState } from "preact/hooks";

import { html } from "../../html.js";
import { Icon } from "../../ui/icons.js";
import { bytes } from "../../format.js";

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

// isImage reports whether the file is drawn as a picture.
export function isImage(file) {
    return Boolean(file && (file.media || IMAGE.test(file.name || file.path || "")));
}

// FileCard renders an attachment as one row.
export function FileCard({ file, onOpen }) {
    const image = isImage(file);
    return html`
        <button class="mfile" type="button"
                onClick=${() => onOpen && onOpen(file)}
                data-path=${file.path}
                aria-label=${`open ${file.name}`}>
            <span class="mfico">${image ? Icon.photo() : Icon.file()}</span>
            <span class="mfname">${file.name}</span>
            ${file.size > 0 && html`<span class="mfsize">${bytes(file.size)}</span>`}
        </button>
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
