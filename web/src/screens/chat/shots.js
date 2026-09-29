// Pictures of the feed as tiles: a strip beside a message or a call, a thumbnail
// on the row of a call. A tile opens its picture full screen (see photo.js).
import { useEffect, useRef, useState } from "preact/hooks";

import { html } from "../../html.js";
import { idParam } from "./api.js";
import { Photo, shotName } from "./photo.js";

// shotSrc returns the address of one picture of the feed: a file the panel
// sent goes by its name, a picture a call returned by the place of its
// result, a picture pasted into a prompt by the record it came in.
export function shotSrc(session, id, pos, shot) {
    const base = `/api/chat/image?session=${encodeURIComponent(session)}${idParam(id)}`;
    if (shot.upload) return `${base}&upload=${encodeURIComponent(shot.upload)}`;
    if (shot.part != null) return `${base}&pos=${shot.pos}&i=${shot.index}&part=${shot.part}`;
    return `${base}&pos=${pos}&i=${shot.index}`;
}

function shotKey(shot) {
    if (shot.upload) return shot.upload;
    return shot.part != null ? `${shot.pos}-${shot.index}-${shot.part}` : `${shot.index}`;
}

// usePicture fetches a picture once its tile comes near the screen, and
// returns its address in the page or why it did not come.
function usePicture(src, box) {
    const [url, setUrl] = useState("");
    const [error, setError] = useState("");
    const [want, setWant] = useState(typeof IntersectionObserver === "undefined");

    useEffect(() => {
        if (want || !box.current) return undefined;
        const eye = new IntersectionObserver((entries) => {
            if (entries.some((e) => e.isIntersecting)) setWant(true);
        }, { rootMargin: "300px" });
        eye.observe(box.current);
        return () => eye.disconnect();
    }, [want]);

    useEffect(() => {
        if (!want) return undefined;
        let alive = true;
        let object = "";
        (async () => {
            try {
                const r = await fetch(src);
                if (!r.ok) throw new Error((await r.text()).trim() || `response ${r.status}`);
                const blob = await r.blob();
                if (!alive) return;
                object = URL.createObjectURL(blob);
                setUrl(object);
            } catch (e) {
                if (alive) setError(String(e.message || e));
            }
        })();
        return () => {
            alive = false;
            if (object) URL.revokeObjectURL(object);
        };
    }, [src, want]);

    return { url, error };
}

// Shots renders the pictures of one row as a strip of tiles.
export function Shots({ shots, session, id, pos }) {
    if (!shots || !shots.length) return null;
    return html`
        <div class="mshots">
            ${shots.map((shot) => html`<${Shot} key=${shotKey(shot)} src=${shotSrc(session, id, pos, shot)}
                                                name=${shotName(shot, pos)} />`)}
        </div>
    `;
}

function Shot({ src, name }) {
    const box = useRef(null);
    const { url, error } = usePicture(src, box);
    const [open, setOpen] = useState(false);

    return html`
        <button class="mshot" ref=${box} type="button" onClick=${() => setOpen(true)}
                aria-label=${`open attachment ${name}`}>
            ${url && html`<img src=${url} decoding="async" alt="attachment in the message" />`}
            ${error && html`<span class="mshotbad">✕</span>`}
        </button>
        ${open && html`<${Photo} url=${url} name=${name} error=${error}
                                 onClose=${() => setOpen(false)} />`}
    `;
}

// Thumb is a picture on a row that opens something else: it says what the row
// holds, and the row itself is what a tap opens.
export function Thumb({ src }) {
    const box = useRef(null);
    const { url, error } = usePicture(src, box);
    return html`
        <span class="cnshot" ref=${box} aria-hidden="true">
            ${url && html`<img src=${url} decoding="async" alt="" />`}
            ${error && html`<span class="mshotbad">✕</span>`}
        </span>
    `;
}
