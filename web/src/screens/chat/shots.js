// Pictures of the feed as tiles: a strip beside a message or a call, a thumbnail
// on the row of a call. A tile opens the gallery of its message.
import { useEffect, useRef, useState } from "preact/hooks";

import { html } from "../../html.js";
import { useMenu } from "../../ui/menu.js";
import { idParam } from "./api.js";
import { ShotMenu } from "./feedmenu.js";
import { Photo, shotName } from "./photo.js";

// shotSrc returns the address of one picture of the feed: a file the panel
// sent goes by its name, a picture a call returned by the place of its
// result, a picture pasted into a prompt by the record it came in.
export function shotSrc(session, id, pos, shot) {
    if (shot.src) return shot.src;
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
export function usePicture(src, box, eager = false) {
    const [picture, setPicture] = useState({ src: "", url: "", error: "" });
    const active = useRef(null);
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
        if (!src || (!want && !eager)) return undefined;
        active.current = src;
        let alive = true;
        let object = "";
        (async () => {
            try {
                const r = await fetch(src);
                if (!r.ok) throw new Error((await r.text()).trim() || `response ${r.status}`);
                const blob = await r.blob();
                if (!alive) return;
                object = URL.createObjectURL(blob);
                setPicture({ src, url: object, error: "" });
            } catch (e) {
                if (alive) setPicture({ src, url: "", error: String(e.message || e) });
            }
        })();
        return () => {
            alive = false;
            active.current = null;
            if (object) URL.revokeObjectURL(object);
        };
    }, [src, want, eager]);

    return picture.src === src && active.current === src ? picture : { url: "", error: "" };
}

export function pictureFiles(shots, session, id, pos) {
    return shots.map((shot) => ({
        name: shotName(shot, pos), media: shot.media,
        shot: { src: shotSrc(session, id, pos, shot) },
    }));
}

// Shots renders the pictures of one row as a strip of tiles.
export function Shots({ shots, session, id, pos, files, onFile }) {
    const [open, setOpen] = useState(null);
    if (!shots || !shots.length) return null;
    const pictures = files && pictureFiles(shots, session, id, pos);
    return html`
        <div class="mshots">
            ${shots.map((shot, index) => html`<${Shot} key=${shotKey(shot)} src=${shotSrc(session, id, pos, shot)}
                name=${shotName(shot, pos)} onOpen=${(picture) => files && onFile
                    ? onFile(pictures[index], files) : setOpen({ index, ...picture })} />`)}
        </div>
        ${open && html`<${ShotGallery} shots=${shots} session=${session} id=${id} pos=${pos}
            open=${open} onPick=${(index) => setOpen({ index })} onClose=${() => setOpen(null)} />`}
    `;
}

function ShotGallery({ shots, session, id, pos, open, onPick, onClose }) {
    const box = useRef(null);
    const index = Math.min(open.index, shots.length - 1);
    const shot = shots[index], src = shotSrc(session, id, pos, shot);
    const held = open.src === src ? open : null;
    const picture = usePicture(src, box, !held?.url);
    return html`<${Photo} url=${held?.url || picture.url} error=${held?.error || picture.error}
        name=${shotName(shot, pos)} onClose=${onClose} index=${index} total=${shots.length}
        onPrevious=${index > 0 ? () => onPick(index - 1) : undefined}
        onNext=${index + 1 < shots.length ? () => onPick(index + 1) : undefined} />`;
}

// Shot is one tile; at a desk its menu opens the picture as a press does,
// and saves it as the viewer does.
function Shot({ src, name, onOpen }) {
    const menu = useMenu();
    const box = useRef(null);
    const { url, error } = usePicture(src, box);
    const show = () => onOpen({ src, url, error });

    return html`
        <button class="mshot" ref=${box} type="button" onClick=${show}
                onContextMenu=${menu && menu(ShotMenu, { url, name, error, onOpen: show }, `picture ${name}`)}
                aria-label=${`open attachment ${name}`}>
            ${url && html`<img src=${url} decoding="async" alt="attachment in the message" />`}
            ${error && html`<span class="mshotbad">✕</span>`}
        </button>
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
