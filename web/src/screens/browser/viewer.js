import { useCallback, useEffect, useRef, useState } from "preact/hooks";
import { html } from "../../html.js";

const special = new Set([
    "Enter", "Backspace", "Delete", "Tab", "Escape", "ArrowLeft", "ArrowRight",
    "ArrowUp", "ArrowDown", "Home", "End", "PageUp", "PageDown",
]);
const modifiers = (e) => (e.altKey ? 1 : 0) | (e.ctrlKey ? 2 : 0) | (e.metaKey ? 4 : 0) | (e.shiftKey ? 8 : 0);

// BrowserViewer owns one controller and releases its images when hidden or closed.
export function BrowserViewer({ profile, limits, wide, onBack }) {
    const wrap = useRef(null);
    const viewport = useRef(null);
    const picture = useRef(null);
    const keyboard = useRef(null);
    const connection = useRef(null);
    const queue = useRef([]);
    const sending = useRef(false);
    const size = useRef(null);
    const drawn = useRef(null);
    const gesture = useRef(null);
    const generation = useRef(0);
    const composing = useRef(false);
    const [state, setState] = useState({ tabs: [], current: "" });
    const [address, setAddress] = useState("");
    const [message, setMessage] = useState("Connecting…");
    const [online, setOnline] = useState(false);
    const [attempt, setAttempt] = useState(0);
    const [keys, setKeys] = useState(false);
    const send = useCallback((input) => {
        const current = connection.current;
        if (!current || !current.id) return;
        const payload = { ...input, generation: generation.current };
        const last = queue.current[queue.current.length - 1];
        // Motion can arrive faster than a WAN round trip. Keep its final
        // position and total scroll, while keys and presses retain their order.
        const sameQueue = last && last.current === current && last.payload.generation === payload.generation;
        if (sameQueue && payload.phase === "move" && last.payload.phase === "move" &&
            (payload.kind === "mouse" || payload.kind === "touch") && last.payload.kind === payload.kind) {
            last.payload = payload;
        } else if (sameQueue && payload.kind === "wheel" && last.payload.kind === "wheel" &&
            Math.abs(payload.dx + last.payload.dx) <= 10000 && Math.abs(payload.dy + last.payload.dy) <= 10000) {
            payload.dx += last.payload.dx;
            payload.dy += last.payload.dy;
            last.payload = payload;
        } else {
            if (queue.current.length >= 128) {
                setMessage("The connection cannot keep up with input. Wait before typing more.");
                return;
            }
            queue.current.push({ current, payload });
        }
        if (sending.current) return;
        sending.current = true;
        const flush = async () => {
            try {
                while (queue.current.length) {
                    const batch = queue.current.splice(0, 32).filter((item) => item.current === connection.current);
                    if (!batch.length) continue;
                    const r = await fetch(`/api/browser/input?id=${batch[0].current.id}`, {
                        method: "POST",
                        headers: { "Content-Type": "application/json" },
                        body: JSON.stringify({ events: batch.map((item) => item.payload) }),
                    });
                    if (!r.ok) throw new Error("The browser refused this input");
                }
            } catch (e) {
                queue.current = [];
                setMessage(e.message);
            } finally {
                sending.current = false;
            }
        };
        flush();
    }, []);
    useEffect(() => {
        let source, timer, imageBusy = false, nextFrame = null, loadingFrame = null;
        let imageURL = "", loadingURL = "", pull = null;
        let stopped = false;
        const current = { id: "" };
        const measure = () => {
            const box = viewport.current.getBoundingClientRect();
            const width = Math.max(100, Math.min(limits.width, Math.floor(box.width)));
            const height = Math.max(100, Math.min(limits.height, Math.floor(box.height)));
            const dpr = wide ? 1 : Math.max(1, Math.min(
                limits.dpr, window.devicePixelRatio || 1, Math.sqrt(limits.pixels / (width * height)),
            ));
            return { width, height, dpr, mobile: wide ? 0 : 1 };
        };
        const releaseImages = () => {
            if (pull) pull.abort();
            pull = null;
            picture.current.removeAttribute("src");
            for (const url of [imageURL, loadingURL]) {
                if (url) URL.revokeObjectURL(url);
            }
            imageURL = loadingURL = "";
            imageBusy = false;
            nextFrame = null;
            drawn.current = null;
        };
        const close = () => {
            if (source) source.close();
            source = null;
            current.id = "";
            connection.current = null;
            releaseImages();
            setOnline(false);
        };
        const fail = (why) => {
            close();
            setMessage(why);
        };
        const loadFrame = async (frame) => {
            if (imageBusy) {
                nextFrame = frame;
                return;
            }
            imageBusy = true;
            loadingFrame = frame;
            const request = new AbortController();
            pull = request;
            try {
                // fetch follows the selected panel endpoint and its bearer.
                // An img URL would bypass that router and require the origin.
                const response = await fetch(`/api/browser/frame?id=${current.id}&sequence=${frame.sequence}`, {
                    cache: "no-store", signal: request.signal,
                });
                if (!response.ok) throw new Error("The browser image could not be loaded");
                const blob = await response.blob();
                if (request.signal.aborted || stopped || !source) return;
                loadingURL = URL.createObjectURL(blob);
                picture.current.src = loadingURL;
            } catch (error) {
                if (!request.signal.aborted && !stopped) fail(error.message);
            }
        };
        picture.current.onload = () => {
            if (!source || !loadingFrame) return;
            if (imageURL) URL.revokeObjectURL(imageURL);
            imageURL = loadingURL;
            loadingURL = "";
            generation.current = loadingFrame.generation;
            drawn.current = loadingFrame.size;
            imageBusy = false;
            if (nextFrame) {
                const frame = nextFrame;
                nextFrame = null;
                loadFrame(frame);
            }
        };
        picture.current.onerror = () => {
            if (source) fail("The browser image could not be loaded");
        };
        const connect = () => {
            if (stopped || document.hidden || source) return;
            size.current = measure();
            setMessage("Connecting…");
            source = new EventSource(`/api/browser/stream?profile=${profile.id}&${new URLSearchParams(size.current)}`);
            source.addEventListener("ready", (e) => {
                current.id = JSON.parse(e.data).id;
                connection.current = current;
                setOnline(true);
                setMessage("");
            });
            source.addEventListener("state", (e) => setState(JSON.parse(e.data)));
            source.addEventListener("frame", (e) => loadFrame(JSON.parse(e.data)));
            source.addEventListener("refused", (e) => setMessage(JSON.parse(e.data).reason));
            source.addEventListener("end", (e) => fail(JSON.parse(e.data).reason || "The browser closed"));
            source.onerror = () => fail("Disconnected. Reconnect, or reopen this profile if it has closed.");
        };
        const visibility = () => {
            if (document.hidden) {
                close();
                setMessage("Paused while this page is hidden");
            } else {
                connect();
            }
        };
        const resize = new ResizeObserver(() => {
            if (!current.id || JSON.stringify(measure()) === JSON.stringify(size.current)) return;
            clearTimeout(timer);
            timer = setTimeout(async () => {
                if (!current.id) return;
                const next = measure();
                if (JSON.stringify(next) === JSON.stringify(size.current)) return;
                size.current = next;
                try {
                    const r = await fetch(`/api/browser/size?id=${current.id}&${new URLSearchParams(next)}`, { method: "POST" });
                    if (!r.ok) fail("The browser could not resize");
                } catch (e) {
                    fail("The browser connection ended");
                }
            }, 150);
        });
        resize.observe(viewport.current);
        document.addEventListener("visibilitychange", visibility);
        connect();
        return () => {
            stopped = true;
            close();
            resize.disconnect();
            clearTimeout(timer);
            document.removeEventListener("visibilitychange", visibility);
        };
    }, [profile.id, wide, attempt, limits.width, limits.height, limits.dpr, limits.pixels]);
    useEffect(() => {
        const view = window.visualViewport;
        if (!view || wide) return;
        const fit = () => {
            const top = wrap.current.getBoundingClientRect().top;
            wrap.current.style.height = `${Math.max(260, view.height + view.offsetTop - top)}px`;
        };
        fit();
        view.addEventListener("resize", fit);
        view.addEventListener("scroll", fit);
        return () => {
            view.removeEventListener("resize", fit);
            view.removeEventListener("scroll", fit);
        };
    }, [wide]);
    const tab = state.tabs.find((t) => t.id === state.current);
    useEffect(() => { setAddress(tab ? tab.url : ""); }, [tab && tab.url]);
    const point = (e, clamp = false) => {
        const frame = drawn.current;
        if (!frame) return null;
        const box = picture.current.getBoundingClientRect();
        if (!box.width || !box.height) return null;
        const x = (e.clientX - box.left) * frame.width / box.width;
        const y = (e.clientY - box.top) * frame.height / box.height;
        if (!clamp && (x < 0 || y < 0 || x > frame.width || y > frame.height)) return null;
        return {
            x: Math.max(0, Math.min(frame.width, x)),
            y: Math.max(0, Math.min(frame.height, y)),
        };
    };
    const down = (e) => {
        const p = point(e);
        if (!online || !p || gesture.current) return;
        e.preventDefault();
        viewport.current.focus({ preventScroll: true });
        viewport.current.setPointerCapture(e.pointerId);
        const touch = e.pointerType !== "mouse";
        const button = e.button === 2 ? "right" : e.button === 1 ? "middle" : "left";
        gesture.current = { pointer: e.pointerId, touch, button };
        send({ kind: touch ? "touch" : "mouse", phase: touch ? "start" : "down", ...p, button, modifiers: modifiers(e) });
    };
    const move = (e) => {
        const g = gesture.current;
        const p = point(e, !!g);
        if (!p) return;
        if (!g) {
            if (online && e.pointerType === "mouse") {
                send({ kind: "mouse", phase: "move", ...p, button: "none", modifiers: modifiers(e) });
            }
            return;
        }
        if (g.pointer !== e.pointerId) return;
        send({ kind: g.touch ? "touch" : "mouse", phase: "move", ...p, button: g.button, modifiers: modifiers(e) });
    };
    const up = (e) => {
        const g = gesture.current;
        if (!g || g.pointer !== e.pointerId) return;
        gesture.current = null;
        const p = point(e, true);
        if (!p) return;
        const phase = g.touch ? (e.type === "pointercancel" ? "cancel" : "end") : "up";
        send({ kind: g.touch ? "touch" : "mouse", phase, ...p, button: g.button, modifiers: modifiers(e) });
    };
    const key = (e) => {
        if (e.isComposing) return;
        if (special.has(e.key) || ((e.ctrlKey || e.metaKey) && "acxz".includes(e.key.toLowerCase()))) {
            e.preventDefault();
            send({ kind: "key", key: e.key, modifiers: modifiers(e) });
        } else if (e.currentTarget === viewport.current && e.key.length === 1 && !e.ctrlKey && !e.metaKey && !e.altKey) {
            e.preventDefault();
            send({ kind: "text", text: e.key });
        }
    };
    const typed = (e) => {
        if (composing.current || e.isComposing) return;
        if (e.currentTarget.value) send({ kind: "text", text: e.currentTarget.value });
        else if (e.inputType === "deleteContentBackward") send({ kind: "key", key: "Backspace" });
        e.currentTarget.value = "";
    };
    useEffect(() => {
        const el = viewport.current;
        const wheel = (e) => {
            const p = point(e);
            if (!connection.current || !p) return;
            e.preventDefault();
            send({ kind: "wheel", ...p, dx: e.deltaX, dy: e.deltaY });
        };
        el.addEventListener("wheel", wheel, { passive: false });
        return () => el.removeEventListener("wheel", wheel);
    }, [send]);
    const showKeyboard = () => {
        setKeys(!keys);
        if (!keys) requestAnimationFrame(() => keyboard.current.focus());
    };
    const navigate = (event) => {
        event.preventDefault();
        send({ kind: "navigate", url: address.trim() });
        keyboard.current.blur();
    };
    const paste = (event) => {
        event.preventDefault();
        send({ kind: "text", text: event.clipboardData.getData("text/plain") });
    };
    return html`
        <section class=${`browser-view${wide ? " browser-wide" : ""}`} ref=${wrap}>
            <header class="browser-heading">
                <button onClick=${onBack}>‹ Profiles</button>
                <strong>${profile.name}</strong>
                <button aria-pressed=${keys} onClick=${showKeyboard}>Keyboard</button>
            </header>
            <nav class="browser-tabs" aria-label="Browser tabs">
                ${state.tabs.map((t) => html`
                    <div class=${t.id === state.current ? "selected" : ""} key=${t.id}>
                        <button onClick=${() => send({ kind: "select-tab", tab: t.id })}>${t.title || "New tab"}</button>
                        <button aria-label=${`Close ${t.title || "tab"}`}
                                onClick=${() => send({ kind: "close-tab", tab: t.id })}>×</button>
                    </div>
                `)}
                <button aria-label="New tab" onClick=${() => send({ kind: "new-tab" })}>＋</button>
            </nav>
            <form class="browser-address" onSubmit=${navigate}>
                <button type="button" aria-label="Back" disabled=${!online || !state.back}
                        onClick=${() => send({ kind: "back" })}>←</button>
                <button type="button" aria-label="Forward" disabled=${!online || !state.forward}
                        onClick=${() => send({ kind: "forward" })}>→</button>
                <button type="button" aria-label="Reload" disabled=${!online}
                        onClick=${() => send({ kind: "reload" })}>↻</button>
                <input aria-label="Page address" placeholder="https://example.com" inputMode="url" autoCapitalize="off"
                       autoCorrect=${false} spellcheck=${false} value=${address}
                       onInput=${(e) => setAddress(e.currentTarget.value)} />
                <button type="submit" disabled=${!online}>Go</button>
            </form>
            ${message && html`
                <div class="browser-status" role="status">
                    ${message}
                    ${!online && html`<button onClick=${() => setAttempt(attempt + 1)}>Reconnect</button>`}
                </div>
            `}
            <div class="browser-viewport" ref=${viewport} tabIndex="0" role="application" aria-label="Remote browser page"
                 onPointerDown=${down} onPointerMove=${move} onPointerUp=${up} onPointerCancel=${up}
                 onKeyDown=${key} onContextMenu=${(e) => e.preventDefault()} onPaste=${paste}>
                <img ref=${picture} alt="Remote page" draggable="false" />
            </div>
            <div class=${`browser-keyboard${keys ? " shown" : ""}`}>
                <textarea ref=${keyboard} rows="1" aria-label="Type on the remote page" placeholder="Type on the page…"
                          autoComplete="off" autoCapitalize="off" spellcheck=${false}
                          onKeyDown=${key} onInput=${typed} onCompositionStart=${() => { composing.current = true; }}
                          onCompositionEnd=${(e) => { composing.current = false; typed(e); }} />
                <button onClick=${() => send({ kind: "key", key: "Tab" })}>Tab</button>
                <button onClick=${() => send({ kind: "key", key: "Enter" })}>Enter</button>
            </div>
        </section>
    `;
}
