import { useEffect, useRef, useState } from "preact/hooks";

import { html } from "../../html.js";
import { mayFocus } from "../../ui/focus.js";
import { useToast } from "../../ui/toasts.js";
import { useWide, WIDE } from "../../ui/wide.js";
import { sender } from "./input.js";

const SCROLLBACK = 5000;

// useTermAvailable reports whether this installation has a terminal at all.
export function useTermAvailable() {
    const [state, setState] = useState({ ok: false, reason: "" });
    useEffect(() => {
        let live = true;
        fetch("/api/term")
            .then((r) => (r.ok ? r.json() : { available: false, reason: "" }))
            .then((d) => live && setState({ ok: Boolean(d.available), reason: d.reason || "" }))
            .catch(() => live && setState({ ok: false, reason: "" }));
        return () => {
            live = false;
        };
    }, []);
    return state;
}

const ANSI = [
    ["black", "--term-black", "#1c2333"],
    ["red", "--term-red", "#cb9490"],
    ["green", "--term-green", "#6b9c83"],
    ["yellow", "--term-yellow", "#cbab7f"],
    ["blue", "--term-blue", "#7d99bd"],
    ["magenta", "--term-magenta", "#a68cc4"],
    ["cyan", "--term-cyan", "#79a8ad"],
    ["white", "--term-white", "#b6c0d4"],
    ["brightBlack", "--term-bright-black", "#7c88a0"],
    ["brightRed", "--term-bright-red", "#e3aca7"],
    ["brightGreen", "--term-bright-green", "#8bbca2"],
    ["brightYellow", "--term-bright-yellow", "#e5c79a"],
    ["brightBlue", "--term-bright-blue", "#9ab6da"],
    ["brightMagenta", "--term-bright-magenta", "#c1a8dd"],
    ["brightCyan", "--term-bright-cyan", "#95c4c8"],
    ["brightWhite", "--term-bright-white", "#e6ecf8"],
];

function palette(el) {
    const css = getComputedStyle(el);
    const pick = (name, fallback) => css.getPropertyValue(name).trim() || fallback;
    const bg = pick("--termbg", "#05060b");
    const theme = {
        background: bg,
        foreground: pick("--termink", "#cfd8ee"),
        cursor: pick("--term-cursor", "#7d99bd"),
        cursorAccent: bg,
        selectionBackground: pick("--term-sel", "rgba(125, 153, 189, .32)"),
    };
    for (const [name, token, fallback] of ANSI) theme[name] = pick(token, fallback);
    return theme;
}

const SWIPE_STEP_ROWS = 1;

function swipeAsWheel(term, screen) {
    let lastY = null;
    let carry = 0;
    let rowPx = 0;

    const rowHeight = () => {
        const rows = term.element && term.element.querySelector(".xterm-screen");
        const h = rows ? rows.getBoundingClientRect().height / Math.max(term.rows, 1) : 0;
        return h > 0 ? h : 16;
    };
    const start = (ev) => {
        lastY = ev.touches[0].clientY;
        carry = 0;
        rowPx = rowHeight() * SWIPE_STEP_ROWS;
    };
    const move = (ev) => {
        if (lastY === null || term.modes.mouseTrackingMode === "none") return;
        const touch = ev.touches[0];
        carry += lastY - touch.clientY;
        lastY = touch.clientY;
        while (Math.abs(carry) >= rowPx) {
            const dir = carry > 0 ? 1 : -1;
            carry -= dir * rowPx;
            term.element.dispatchEvent(new WheelEvent("wheel", {
                deltaY: dir,
                deltaMode: WheelEvent.DOM_DELTA_LINE,
                clientX: touch.clientX,
                clientY: touch.clientY,
                bubbles: true,
                cancelable: true,
            }));
        }
    };
    const end = () => {
        lastY = null;
    };
    screen.addEventListener("touchstart", start, { passive: true });
    screen.addEventListener("touchmove", move, { passive: true });
    screen.addEventListener("touchend", end, { passive: true });
    screen.addEventListener("touchcancel", end, { passive: true });
    return () => {
        screen.removeEventListener("touchstart", start);
        screen.removeEventListener("touchmove", move);
        screen.removeEventListener("touchend", end);
        screen.removeEventListener("touchcancel", end);
    };
}

// fitOn recomputes the screen size and registers the events that trigger it.
export function fitOn(fit, screen, fonts, font, Observer) {
    let live = true;
    const refit = () => {
        if (!live) return;
        try {
            fit.fit();
        } catch {
        }
    };
    const observer = typeof Observer === "function" ? new Observer(refit) : null;
    if (observer) observer.observe(screen);
    if (fonts && typeof fonts.load === "function") fonts.load(font).then(refit, refit);
    return () => {
        live = false;
        if (observer) observer.disconnect();
    };
}

const MIN_ROOM = 120;

const SCALE_SLACK = 0.01;

// roomFor returns how much room the terminal has inside the visible viewport.
export function roomFor(top, view) {
    if (!view || Math.abs(view.scale - 1) > SCALE_SLACK) return 0;
    const room = Math.floor(view.offsetTop + view.height - top);
    return room >= MIN_ROOM ? room : 0;
}

// capToView keeps the terminal inside the visible part of the page.
export function capToView(wrap, view) {
    if (!view || typeof view.addEventListener !== "function") return () => {};
    const apply = () => {
        const room = roomFor(wrap.getBoundingClientRect().top, view);
        if (room) wrap.style.maxHeight = `${room}px`;
    };
    apply();
    view.addEventListener("resize", apply);
    view.addEventListener("scroll", apply);
    return () => {
        view.removeEventListener("resize", apply);
        view.removeEventListener("scroll", apply);
        wrap.style.maxHeight = "";
    };
}

const KEYS = [
    { id: "esc", label: "Esc", bytes: "\x1b", danger: true },
    { id: "ctrl", label: "Ctrl", modifier: true },
    { id: "tab", label: "Tab", bytes: "\t" },
    { id: "left", label: "←", bytes: "\x1b[D", repeat: true },
    { id: "up", label: "↑", bytes: "\x1b[A", repeat: true },
    { id: "down", label: "↓", bytes: "\x1b[B", repeat: true },
    { id: "right", label: "→", bytes: "\x1b[C", repeat: true },
    { id: "enter", label: "Enter", bytes: "\r" },
];

const REPEAT_AFTER = 400;
const REPEAT_EVERY = 90;

// Ctrl on a phone is a key that sticks for one character, the way shift does
// on the keyboard of the phone itself: there is no second hand to hold it
// with. What follows it becomes a control code — Ctrl+C, Ctrl+D, Ctrl+R — and
// the key lets go by itself.
function ctrlHeld(data) {
    const ch = data.charCodeAt(0);
    if (ch >= 97 && ch <= 122) return String.fromCharCode(ch - 96) + data.slice(1);
    if (ch >= 64 && ch <= 95) return String.fromCharCode(ch - 64) + data.slice(1);
    if (ch === 32) return "\x00" + data.slice(1);
    return data;
}

function TermKeys({ send, ctrl, onCtrl }) {
    const hold = useRef({ delay: 0, tick: 0 });
    const stop = () => {
        clearTimeout(hold.current.delay);
        clearInterval(hold.current.tick);
        hold.current = { delay: 0, tick: 0 };
    };
    useEffect(() => stop, []);

    const press = (key) => (event) => {
        event.preventDefault();
        if (key.modifier) {
            onCtrl(!ctrl);
            return;
        }
        send(key.bytes);
        if (!key.repeat) return;
        stop();
        hold.current.delay = setTimeout(() => {
            hold.current.tick = setInterval(() => send(key.bytes), REPEAT_EVERY);
        }, REPEAT_AFTER);
    };

    return html`
        <div class="termkeys">
            ${KEYS.map((key) => html`
                <button
                    class=${`termkey${key.danger ? " danger" : ""}${key.modifier ? " mod" : ""}${key.modifier && ctrl ? " on" : ""}`}
                    aria-pressed=${key.modifier ? (ctrl ? "true" : "false") : undefined}
                    type="button"
                    key=${key.id}
                    aria-label=${key.id}
                    onPointerDown=${press(key)}
                    onPointerUp=${stop}
                    onPointerLeave=${stop}
                    onPointerCancel=${stop}
                >${key.label}</button>
            `)}
        </div>
    `;
}

export function Term({ name }) {
    const box = useRef(null);
    const wrap = useRef(null);
    const termRef = useRef(null);
    const sendRef = useRef(null);
    const toast = useToast();
    const wide = useWide();
    const [state, setState] = useState({ kind: "loading" });
    const [attempt, setAttempt] = useState(0);
    // The modifier lives here rather than in the row of keys: what it changes
    // is the next character typed on the phone's own keyboard, and that goes
    // straight from the emulator to the session.
    const [ctrl, setCtrl] = useState(false);
    const ctrlRef = useRef(false);
    const holdCtrl = (on) => {
        ctrlRef.current = on;
        setCtrl(on);
    };

    useEffect(() => {
        if (!box.current) return undefined;

        let alive = true;
        let close = null;
        let term = null;
        let unfit = null;
        let uncap = null;
        let themeWatch = null;
        let swipe = null;
        let unfocus = null;
        const input = sender();

        (async () => {
            let mod;
            try {
                mod = await import("/dist/term.js");
            } catch (e) {
                if (alive) setState({ kind: "failed", error: `the terminal emulator did not load: ${e}` });
                return;
            }
            if (!alive) return;

            const screen = box.current;
            const css = getComputedStyle(screen);
            const fontFamily = css.getPropertyValue("--code").trim() || "monospace";
            const fontSize = window.matchMedia(WIDE).matches ? 17 : 13;
            term = new mod.Terminal({
                allowTransparency: false,
                convertEol: false,
                cursorBlink: false,
                fontFamily,
                fontSize,
                scrollback: SCROLLBACK,
                theme: palette(screen),
            });
            const fit = new mod.FitAddon();
            term.loadAddon(fit);
            term.open(screen);
            fit.fit();
            termRef.current = term;
            swipe = swipeAsWheel(term, screen);

            themeWatch = new MutationObserver(() => {
                term.options.theme = palette(screen);
            });
            themeWatch.observe(document.documentElement, { attributes: true, attributeFilter: ["class"] });

            const keepFocus = (event) => {
                if (event.relatedTarget && event.relatedTarget !== document.body) return;
                if (!mayFocus()) return;
                requestAnimationFrame(() => {
                    if (!alive) return;
                    if (document.activeElement && document.activeElement !== document.body) return;
                    term.focus();
                });
            };

            const es = new EventSource(
                `/api/term/stream?name=${encodeURIComponent(name)}&cols=${term.cols}&rows=${term.rows}`,
            );
            close = () => es.close();

            let id = "";
            const encoder = new TextEncoder();
            const send = (bytes) => input.send(bytes);

            es.addEventListener("ready", (ev) => {
                if (!alive) return;
                const info = JSON.parse(ev.data);
                id = info.id;
                input.open(id);
                setState({ kind: "live", detail: info.detail || "" });

                const typed = (data) => {
                    if (!ctrlRef.current) return data;
                    ctrlRef.current = false;
                    setCtrl(false);
                    return ctrlHeld(data);
                };
                term.onData((data) => send(encoder.encode(typed(data))));
                sendRef.current = (data) => send(encoder.encode(data));
                term.onBinary((data) => send(Uint8Array.from(data, (ch) => ch.charCodeAt(0) & 255)));

                term.onResize(({ cols, rows }) => {
                    if (input.closed) return;
                    fetch(`/api/term/size?id=${id}&cols=${cols}&rows=${rows}`, { method: "POST" }).catch(() => {});
                });
                unfit = fitOn(fit, screen, document.fonts, `${fontSize}px ${fontFamily}`, window.ResizeObserver);
                uncap = capToView(wrap.current, window.visualViewport);

                screen.addEventListener("focusout", keepFocus);
                unfocus = () => screen.removeEventListener("focusout", keepFocus);
            });

            es.onmessage = (ev) => {
                if (!alive) return;
                const raw = atob(ev.data);
                const bytes = new Uint8Array(raw.length);
                for (let i = 0; i < raw.length; i += 1) bytes[i] = raw.charCodeAt(i);
                term.write(bytes);
            };

            es.addEventListener("end", (ev) => {
                es.close();
                input.stop();
                if (!alive) return;
                let reason = "";
                try {
                    reason = JSON.parse(ev.data).reason || "";
                } catch {
                }
                setState(reason ? { kind: "failed", error: reason } : { kind: "closed" });
            });

            es.onerror = () => {
                if (es.readyState === EventSource.CLOSED) input.stop();
                if (es.readyState === EventSource.CLOSED && alive) {
                    setState({ kind: "closed" });
                }
            };
            if (alive) setState({ kind: "closed" });
        })();

        return () => {
            alive = false;
            input.stop();
            if (close) close();
            if (unfit) unfit();
            if (uncap) uncap();
            if (themeWatch) themeWatch.disconnect();
            if (swipe) swipe();
            if (unfocus) unfocus();
            if (term) term.dispose();
            termRef.current = null;
            sendRef.current = null;
        };
    }, [name, attempt]);

    useEffect(() => {
        if (state.kind !== "live") return;
        if (!mayFocus()) return;
        const term = termRef.current;
        if (term) term.focus();
    }, [state.kind]);

    useEffect(() => {
        const el = wrap.current;
        if (!el) return undefined;
        const refuse = (event) => {
            const data = event.clipboardData;
            if (!data) return;
            if (!(data.files && data.files.length) || data.getData("text/plain")) return;
            event.preventDefault();
            event.stopPropagation();
            toast("A file cannot be pasted into the terminal",
                "only keyboard bytes go into the session — attach it in the feed", true);
        };
        el.addEventListener("paste", refuse, true);
        return () => el.removeEventListener("paste", refuse, true);
    }, [toast]);

    const type = (data) => {
        const send = sendRef.current;
        if (send) send(data);
    };

    const again = () => {
        setState({ kind: "loading" });
        setAttempt((n) => n + 1);
    };

    return html`
        <div class=${`termwrap${state.kind === "live" ? "" : " off"}`} ref=${wrap}>
            <div class="termscreen" ref=${box}></div>
            ${state.kind === "live" && !wide && html`
                <${TermKeys} send=${type} ctrl=${ctrl} onCtrl=${holdCtrl} />`}
            ${state.kind === "loading" && html`<p class="hint">Opening the terminal…</p>`}
            ${state.kind === "failed" && html`
                <div class="termnote">
                    <p class="hint crit">${state.error}</p>
                    <button class="btn" type="button" onClick=${again}>Try again</button>
                </div>
            `}
            ${state.kind === "closed" && html`
                <div class="termnote">
                    <p class="hint">The terminal detached — the session is closed or detached at the machine.</p>
                    <button class="btn" type="button" onClick=${again}>Connect again</button>
                </div>
            `}
        </div>
    `;
}
