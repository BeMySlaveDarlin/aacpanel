import { useEffect, useRef, useState } from "preact/hooks";

import { html } from "../../html.js";
import { useAction } from "../../actions/gate.js";
import { commandHints, parseCommand } from "../../actions/registry.js";
import { knows, whyNot } from "../../exec.js";
import { mayFocus, regain } from "../../ui/focus.js";
import { Icon } from "../../ui/icons.js";
import { useToast } from "../../ui/toasts.js";
import { WIDE } from "../../ui/wide.js";
import { clipName, FILES_MAX, intake, mb, PickFile } from "./tools.js";
import { withQuote } from "./quote.js";

const MAX_COMPOSER = 112;

const DRAFTS_KEY = "aacpanel.drafts";

const DRAFT_TTL = 7 * 24 * 60 * 60 * 1000;

const DRAFT_MAX = 20;

function drafts() {
    try {
        const all = JSON.parse(localStorage.getItem(DRAFTS_KEY) || "{}");
        return all && typeof all === "object" ? all : {};
    } catch {
        return {};
    }
}

function keep(all) {
    const edge = Date.now() - DRAFT_TTL;
    const live = Object.entries(all)
        .filter(([, one]) => one && typeof one.text === "string" && (one.at || 0) > edge)
        .sort((a, b) => (b[1].at || 0) - (a[1].at || 0));
    return Object.fromEntries(live.slice(0, DRAFT_MAX));
}

function useDraft(id) {
    const [text, setText] = useState(() => saved(id));
    useEffect(() => { setText(saved(id)); }, [id]);

    const write = (value) => {
        setText(value);
        if (!id) return;
        const all = keep(drafts());
        if (value.trim()) all[id] = { text: value, at: Date.now() };
        else delete all[id];
        try {
            localStorage.setItem(DRAFTS_KEY, JSON.stringify(all));
        } catch {
        }
    };

    return [text, write];
}

function saved(id) {
    const one = id ? drafts()[id] : null;
    return (one && one.text) || "";
}

// deliver sends a message into a session, with files or without them.
export function deliver(run, name, { text, files }) {
    const pack = files || [];
    return pack.length
        ? run("session.file", name, {
            text,
            files: pack.map((f) => ({ name: f.name, data: f.data })),
        })
        : run("session.send", name, { text });
}

// outcome turns a send result into the state of the local message row.
export function outcome(result) {
    return result.ok
        ? { state: "queued" }
        : { state: "failed", error: result.error || "did not go out" };
}

// asksSend reports whether this key press asks to send what is typed.
export function asksSend(e, wide) {
    if (e.key !== "Enter" || e.repeat || e.isComposing) return false;
    if (e.shiftKey || e.altKey) return false;
    return Boolean(wide || e.ctrlKey || e.metaKey);
}

// Composer writes into a live session.
export function Composer({ name, id, exec, busy, hold, files, onFiles, onDropFile, onDropFiles, onLocal, onLocalDone, insert, focus, onAsk }) {
    const run = useAction();
    const toast = useToast();
    const area = useRef(null);
    const [text, setText] = useDraft(id);
    const [sending, setSending] = useState(false);
    useEffect(() => {
        if (!insert || !insert.text) return;
        setText(withQuote(insert.text, text));
        requestAnimationFrame(() => {
            const el = area.current;
            if (!el) return;
            el.focus();
            grow(el);
            el.setSelectionRange(el.value.length, el.value.length);
        });
    }, [insert && insert.key]);
    useEffect(() => {
        if (!mayFocus()) return;
        const el = area.current;
        if (!el) return;
        el.focus();
        el.setSelectionRange(el.value.length, el.value.length);
    }, [focus]);
    const wasSending = useRef(false);
    useEffect(() => {
        const back = wasSending.current && !sending;
        wasSending.current = sending;
        if (back) regain(area.current);
    }, [sending]);
    // Two presses can land in the same frame — a phone answers one touch with a
    // click of its own — and both run the closure of the draw they were made in,
    // where the flag that disables the button is still off and the draft is
    // still whole. The latch is taken before anything leaves and dropped only
    // once the screen has been drawn without that draft.
    const taken = useRef(false);
    const ready = knows(exec, "session.send");
    const why = whyNot(exec, "session.send");
    const canStop = knows(exec, "session.stop");
    const stopWhy = whyNot(exec, "session.stop");
    const canFile = knows(exec, "session.file");
    const fileWhy = whyNot(exec, "session.file");
    const canCmd = knows(exec, "session.command");
    const pack = files || [];
    useEffect(() => { taken.current = false; }, [text, pack.length, sending]);
    const cmd = canCmd && !pack.length ? parseCommand(text) : null;
    const hints = canCmd && !pack.length && !(cmd && cmd.ready) ? commandHints(text) : [];
    const cantSend = !ready || sending || (cmd ? !cmd.ready : (pack.length ? !canFile : !text.trim()));
    const stopping = busy && !text.trim() && !pack.length;

    const stop = async () => {
        if (sending || taken.current) return;
        taken.current = true;
        setSending(true);
        await run("session.stop", name, {});
        setSending(false);
    };

    const pick = (value) => {
        setText(value);
        const el = area.current;
        if (!el) return;
        el.focus();
        grow(el);
    };

    const sendCommand = async () => {
        if (!cmd.ready || sending) return;
        setSending(true);
        const result = await run("session.command", name, { command: cmd.command, arg: cmd.arg });
        setSending(false);
        if (result.ok) setText("");
    };

    const paste = async (event) => {
        const data = event.clipboardData;
        if (!data) return;
        const picked = Array.from(data.files || []);
        if (!picked.length || data.getData("text/plain")) return;
        event.preventDefault();
        if (!canFile) {
            toast("The file cannot be attached", fileWhy, true);
            return;
        }
        const got = await intake(picked, pack, toast, clipName);
        if (got && onFiles) onFiles(got);
    };

    const send = async () => {
        const body = text.trim();
        if ((!body && !pack.length) || sending || taken.current) return;
        taken.current = true;
        if (cmd) return sendCommand();
        const key = `${Date.now()}-${Math.random().toString(36).slice(2, 7)}`;
        const named = pack.map((f) => f.name).join(", ");
        const row = {
            role: "me", key, at: new Date().toISOString(),
            text: body || named,
            sent: body,
            file: named || undefined,
        };
        setText("");
        if (pack.length && onDropFiles) onDropFiles();
        if (hold) {
            if (onLocal) onLocal({ ...row, state: "held", hold: { text: body, files: pack } });
            return;
        }
        setSending(true);
        if (onLocal) onLocal({ ...row, state: "sending" });
        const result = await deliver(run, name, { text: body, files: pack });
        setSending(false);
        if (onLocalDone) onLocalDone(key, outcome(result));
    };

    const keys = (e) => {
        if (!asksSend(e, window.matchMedia(WIDE).matches)) return;
        e.preventDefault();
        if (cantSend) return;
        send();
    };

    return html`
        <div class="composer">
            ${hints.length > 0 && html`
                <div class="slashlist">
                    ${hints.map((item) => html`
                        <button class="slashitem" type="button" onClick=${() => pick(item.value)}>
                            <span class="slashname">${item.label}</span>
                            ${item.hint && html`<span class="slashhint">${item.hint}</span>`}
                        </button>
                    `)}
                </div>
            `}
            ${pack.length > 0 && html`
                <div class="clippack">
                    ${pack.map((one, i) => html`
                        <div class="clipped" key=${`${one.name}-${i}`}>
                            ${Icon.clip()}
                            <span class="clipname">${one.name}</span>
                            <span class="clipsize">${mb(one.size)}</span>
                            <button class="clipoff" type="button" aria-label=${`remove file ${one.name}`}
                                    disabled=${sending} onClick=${() => onDropFile && onDropFile(i)}>
                                ${Icon.close()}
                            </button>
                        </div>
                    `)}
                </div>
            `}
            <textarea
                rows="1"
                ref=${(el) => { area.current = el; grow(el); }}
                placeholder=${placeholder(ready, why, name, pack)}
                disabled=${!ready || sending}
                value=${text}
                onInput=${(e) => { setText(e.target.value); grow(e.target); }}
                onPaste=${paste}
                onKeyDown=${keys}
            ></textarea>
            ${pack.length < FILES_MAX && html`<${PickFile} exec=${exec} onAsk=${onAsk} />`}
            ${stopping
                ? html`
                    <button
                        class="iconbtn danger"
                        type="button"
                        aria-label=${`stop the work of session ${name}`}
                        title=${canStop ? "interrupt the answer and drop the queue" : stopWhy}
                        disabled=${!canStop || sending}
                        onClick=${stop}
                    >${Icon.stopsquare()}</button>
                `
                : html`
                    <button
                        class="iconbtn accent"
                        type="button"
                        aria-label=${cmd ? `send a command to session ${name}` : `send to session ${name}`}
                        title=${hold ? "will go out when the session is free" : sendTitle(ready, why, canFile, fileWhy, pack, cmd)}
                        disabled=${cantSend}
                        onClick=${send}
                    >${Icon.send()}</button>
                `}
        </div>
    `;
}

function grow(el) {
    if (!el) return;
    el.style.height = "auto";
    el.style.height = Math.min(el.scrollHeight, MAX_COMPOSER) + "px";
}

function placeholder(ready, why, name, pack) {
    if (!ready) return why;
    if (!pack.length) return `Write to ${name}`;
    return pack.length > 1 ? "A caption for the files — optional" : "A caption for the file — optional";
}

function sendTitle(ready, why, canFile, fileWhy, pack, cmd) {
    if (cmd) return cmd.ready ? "send the command" : "pick a command option";
    if (pack.length) {
        if (!canFile) return fileWhy;
        return pack.length > 1 ? `send ${pack.length} files — as one message` : "send the file";
    }
    return ready ? "send" : why;
}
