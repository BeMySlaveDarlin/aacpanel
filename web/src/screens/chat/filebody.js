// The viewer body: the contents of an open file — as a picture, a player, a page,
// a table or code.
import { useEffect, useState } from "preact/hooks";

import { html } from "../../html.js";
import { render } from "../../md.js";
import { highlight } from "../../code.js";
import { bytes } from "../../format.js";
import { Photo } from "./photo.js";
import { frameDoc, pickView, sheet } from "./kinds.js";

// FileBody renders the contents of an open file.
export function FileBody({ state, look, more, onMore }) {
    const name = state.name || look.path || "";
    const view = pickView(state, name);
    if (view === "exec") return html`<${ExecPlate} state=${state} name=${name} />`;
    if (view === "toobig") {
        return html`
            <p class="hint warn">Too large to show in the panel: ${bytes(state.size)}.</p>
        `;
    }
    if (view === "image") {
        return html`<${Picture} state=${state} name=${name} />`;
    }
    if (view === "video" || view === "audio") {
        return html`<${Player} state=${state} name=${name} sound=${view === "audio"} />`;
    }
    if (view === "pdf") return html`<${Paper} state=${state} name=${name} />`;
    if (view === "binary") {
        return html`<p class="hint warn">The file is binary — there is nothing to show it with.</p>`;
    }
    const text = state.text || "";
    return html`
        ${view === "page" && html`<${Page} text=${text} name=${name} />`}
        ${view === "doc" && html`<div class="mmbody doc">${render(text || "the file is empty")}</div>`}
        ${view === "sheet" && html`<${Sheet} text=${text} name=${name} />`}
        ${view === "code" && html`
            <pre class="callpre code">${text ? highlight(text, name) : "the file is empty"}</pre>
        `}
        ${state.next > 0 && html`
            <div class="filemore">
                <button class="btn" type="button" onClick=${onMore} disabled=${more.busy}>
                    ${more.busy ? "Reading…" : "Show more"}
                </button>
                <span class="hint">${bytes(state.next)} of ${bytes(state.size)}</span>
            </div>
        `}
        ${more.error && html`<p class="hint crit">${more.error}</p>`}
    `;
}

function ExecPlate({ state, name }) {
    return html`
        <div class="fileexec">
            <p class="hint warn">An executable: the panel neither runs it nor shows it.</p>
            <dl class="fileface">
                <div><dt>name</dt><dd>${name}</dd></div>
                <div><dt>size</dt><dd>${bytes(state.size || 0)}</dd></div>
                ${state.mode && html`<div><dt>mode</dt><dd>${state.mode}</dd></div>`}
            </dl>
        </div>
    `;
}

function useBytes(data, media) {
    const [url, setUrl] = useState("");
    useEffect(() => {
        if (!data) {
            setUrl("");
            return undefined;
        }
        let object = "";
        try {
            const raw = atob(data);
            const buf = new Uint8Array(raw.length);
            for (let i = 0; i < raw.length; i += 1) buf[i] = raw.charCodeAt(i);
            object = URL.createObjectURL(new Blob([buf], { type: media || "application/octet-stream" }));
            setUrl(object);
        } catch {
            setUrl("");
        }
        return () => { if (object) URL.revokeObjectURL(object); };
    }, [data, media]);
    return url;
}

function Save({ url, name }) {
    if (!url) return null;
    return html`
        <div class="filemore">
            <a class="fileget" href=${url} download=${name || "file"}>Save</a>
        </div>
    `;
}

function Picture({ state, name }) {
    const url = useBytes(state.data, state.media || "image/png");
    const [open, setOpen] = useState(false);
    return html`
        <button class="filepic" type="button" onClick=${() => setOpen(true)}
                aria-label=${`open ${name} full screen`}>
            ${url && html`<img src=${url} alt=${name} decoding="async" />`}
        </button>
        <${Save} url=${url} name=${name} />
        ${open && html`<${Photo} url=${url} name=${name} head=${name} onClose=${() => setOpen(false)} />`}
    `;
}

function Player({ state, name, sound }) {
    const url = useBytes(state.data, state.media);
    if (!url) return html`<p class="hint">Reading…</p>`;
    return html`
        <div class=${sound ? "fileaudio" : "filevideo"}>
            ${sound
                ? html`<audio src=${url} controls preload="metadata"></audio>`
                : html`<video src=${url} controls playsinline preload="metadata"></video>`}
        </div>
        <p class="hint">If it does not play, the phone has no codec for it — save the file.</p>
        <${Save} url=${url} name=${name} />
    `;
}

function Paper({ state, name }) {
    const url = useBytes(state.data, state.media || "application/pdf");
    if (!url) return html`<p class="hint">Reading…</p>`;
    return html`
        <iframe class="filepdf" src=${url} title=${name}></iframe>
        <${Save} url=${url} name=${name} />
    `;
}

function Page({ text, name }) {
    const [source, setSource] = useState(false);
    return html`
        <div class="filetabs">
            <button type="button" class="chip" aria-pressed=${source ? "false" : "true"}
                    onClick=${() => setSource(false)}>page</button>
            <button type="button" class="chip" aria-pressed=${source ? "true" : "false"}
                    onClick=${() => setSource(true)}>source</button>
        </div>
        ${source
            ? html`<pre class="callpre code">${highlight(text, name)}</pre>`
            : html`<iframe class="filehtml" sandbox="" referrerpolicy="no-referrer"
                           title=${name} srcdoc=${frameDoc(text)}></iframe>`}
    `;
}

function Sheet({ text, name }) {
    const rows = sheet(text, name);
    if (!rows.length) return html`<p class="hint">the file is empty</p>`;
    const [head, ...body] = rows;
    return html`
        <div class="filesheet">
            <table>
                <thead><tr>${head.map((cell, n) => html`<th key=${n}>${cell}</th>`)}</tr></thead>
                <tbody>
                    ${body.map((line, n) => html`
                        <tr key=${n}>${line.map((cell, k) => html`<td key=${k}>${cell}</td>`)}</tr>
                    `)}
                </tbody>
            </table>
        </div>
    `;
}
