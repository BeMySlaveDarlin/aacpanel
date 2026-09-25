import { useEffect, useRef, useState } from "preact/hooks";

import { html } from "../../html.js";
import { useAction } from "../../actions/gate.js";
import { commandHints, parseCommand, parseSide, sessionHints } from "../../actions/registry.js";
import { knows, whyNot } from "../../exec.js";
import { mayFocus, regain } from "../../ui/focus.js";
import { Icon } from "../../ui/icons.js";
import { useToast } from "../../ui/toasts.js";
import { WIDE } from "../../ui/wide.js";
import { dictation, HOLD_MS, join, listen, speech } from "./dictate.js";
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

// useDictate hangs dictation off the send button, and no second button appears.
// An empty field has nothing to send, so the button is a switch there: one press
// starts listening, the next stops it, and what was heard stays in the field. A
// field with words in it is the send button it has always been — a press sends,
// and only a press held past HOLD_MS talks, until the finger comes off. Either
// way the dictation lands after what is already written.
function useDictate(text, setText, toast) {
    const [live, setLive] = useState(false);
    const on = dictation() && Boolean(speech());
    const timer = useRef(null);
    const ear = useRef(null);
    const how = useRef("");
    const spoke = useRef(false);
    const latest = useRef(text);
    latest.current = text;

    useEffect(() => () => {
        clearTimeout(timer.current);
        if (ear.current) ear.current.stop();
    }, []);

    // start opens the microphone either way it was asked for. What stood in the
    // field at that moment is kept: the dictation reports the whole of what it
    // has heard each time, so the field is written anew from that rather than
    // added to, and the same words arriving twice change nothing. Words already
    // written stay where they are and the dictation lands after them.
    const start = (mode) => {
        timer.current = null;
        how.current = mode;
        // Only a held press leaves a click behind to be swallowed: the press
        // that ends a switched-on dictation is that click.
        spoke.current = mode === "hold";
        const base = latest.current;
        ear.current = listen({
            onSaid: (heard) => {
                latest.current = join(base, heard);
                setText(latest.current);
            },
            onEnd: (why) => {
                ear.current = null;
                how.current = "";
                setLive(false);
                if (why) toast("Dictation", why, true);
            },
        });
        if (ear.current) setLive(true);
        else spoke.current = false;
    };

    const stop = () => { if (ear.current) ear.current.stop(); };

    return {
        on,
        live,
        // tap reports whether a press should be answered by the microphone
        // rather than by sending, and does it. An empty field is a switch: one
        // press starts, the next stops, and what was heard stays in the field.
        // A field with words in it is the send button, and only a held press
        // talks.
        tap: (empty) => {
            if (!on) return false;
            if (ear.current) {
                // A press held to talk is ended by the release, not by the
                // click that comes after it.
                if (how.current === "tap") stop();
                return true;
            }
            if (spoke.current) {
                spoke.current = false;
                return true;
            }
            if (!empty) return false;
            start("tap");
            return true;
        },
        down: (e, empty) => {
            if (!on || ear.current || empty || (e.button !== undefined && e.button > 0)) return;
            if (e.currentTarget.setPointerCapture) e.currentTarget.setPointerCapture(e.pointerId);
            clearTimeout(timer.current);
            timer.current = setTimeout(() => start("hold"), HOLD_MS);
        },
        up: () => {
            clearTimeout(timer.current);
            timer.current = null;
            if (!ear.current || how.current !== "hold") return;
            stop();
            // The click that follows a release is swallowed by the mark, but a
            // release does not always bring one — a touch cancelled, a finger
            // dragged off. Dropping the mark in the next task lets the click
            // through if it comes and forgets it if it does not, so a dictation
            // can never swallow the send after it.
            setTimeout(() => { spoke.current = false; }, 0);
        },
    };
}

// The commands a session on the stream takes, asked for once per session the
// first time the list is opened: a skill installed since is picked up by the
// next session, as it is by claude.
const sessionLists = new Map();

function useSessionCommands(name, id, want) {
    const key = `${name}|${id || ""}`;
    const [, redraw] = useState(0);
    useEffect(() => {
        if (!want || !name || sessionLists.has(key)) return;
        sessionLists.set(key, { state: "loading" });
        (async () => {
            let got = { state: "unknown" };
            try {
                const r = await fetch(`/api/session/commands?name=${encodeURIComponent(name)}`, { credentials: "same-origin" });
                const body = r.ok ? await r.json() : null;
                if (body && body.state === "ok" && body.transport === "stream" && Array.isArray(body.list)) {
                    got = { state: "ok", list: body.list };
                }
            } catch {
            }
            sessionLists.set(key, got);
            redraw((n) => n + 1);
        })();
    }, [want, key]);
    const held = sessionLists.get(key);
    return held && held.state === "ok" ? held.list : null;
}

function saved(id) {
    const one = id ? drafts()[id] : null;
    return (one && one.text) || "";
}

// deliver sends a message into a session, with files or without them.
export function deliver(run, name, { text, files, messageId }) {
    const pack = files || [];
    return pack.length
        ? run("session.file", name, {
            text,
            files: pack.map((f) => ({ name: f.name, data: f.data })),
        })
        : run("session.send", name, messageId ? { text, messageId } : { text });
}

// messageID names a message to a session on the stream, so that it can be
// taken back while it waits in the queue. A terminal session gets none: its
// queue is on its screen.
function messageID(stream, pack) {
    if (!stream || pack.length || typeof crypto === "undefined" || !crypto.randomUUID) return undefined;
    return crypto.randomUUID();
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
export function Composer({ name, id, exec, busy, stream, hold, files, onFiles, onDropFile, onDropFiles, onLocal, onLocalDone, insert, focus, onAsk, onPicker, onScreen, onSide, strip }) {
    const run = useAction();
    const toast = useToast();
    const area = useRef(null);
    const [text, setText] = useDraft(id);
    const [sending, setSending] = useState(false);
    useEffect(() => {
        if (!insert || !insert.text) return;
        // A message taken back comes as it was written, ahead of whatever the
        // composer already holds; a command goes in front of it, so what is
        // typed becomes what the command is about; a quote comes as a quote.
        if (insert.message) setText(text.trim() ? `${insert.text}\n\n${text}` : insert.text);
        else if (insert.command) setText(`${insert.text}${text.trimStart()}`);
        else setText(withQuote(insert.text, text));
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
    const hear = useDictate(text, setText, toast);
    const ready = knows(exec, "session.send");
    const why = whyNot(exec, "session.send");
    const canStop = knows(exec, "session.stop");
    const stopWhy = whyNot(exec, "session.stop");
    const canFile = knows(exec, "session.file");
    const fileWhy = whyNot(exec, "session.file");
    const canCmd = knows(exec, "session.command");
    const pack = files || [];
    useEffect(() => { taken.current = false; }, [text, pack.length, sending]);
    const cmd = canCmd && !pack.length ? parseCommand(text, stream) : null;
    // /model and /effort with nothing after them are a request for the list,
    // as they are in a terminal: sending them opens it instead.
    const lists = cmd && !cmd.arg && onPicker && (cmd.command === "model" || cmd.command === "effort")
        ? cmd.command : "";
    // A command the panel answers with a screen of its own opens it.
    const opens = cmd && cmd.screen && onScreen ? cmd.command : "";
    // A question aside on the stream is the panel's to ask: it goes to the side
    // chat and never into the conversation.
    const side = stream && onSide && !pack.length ? parseSide(text) : null;
    const listing = canCmd && !pack.length && !(cmd && cmd.ready) && !lists && !opens && !(side && side.question);
    // On the stream the list is the session's own, every command and skill it
    // takes; until it has come, and in a console, the panel's own list stands.
    const theirs = useSessionCommands(name, id, stream && listing && text.startsWith("/"));
    const hints = listing ? (stream && theirs ? sessionHints(text, theirs) : commandHints(text, stream)) : [];
    const [hot, setHot] = useState(0);
    useEffect(() => { setHot(0); }, [text]);
    const wideNow = typeof window !== "undefined" && window.matchMedia(WIDE).matches;
    // With dictation on, an empty field shows the microphone rather than the
    // arrow: there is nothing to send yet, and a disabled button takes no press
    // to hold. The moment there are words it is the send button again. The
    // emptiness is folded into cantSend rather than kept beside it, so the
    // button and the key that sends keep asking one question.
    const canTalk = hear.on && !cmd && !pack.length;
    const asMic = canTalk && !text.trim();
    const cantSend = !ready || sending
        || (side ? false : cmd ? !(cmd.ready || lists || opens) : (pack.length ? !canFile : (!text.trim() && !canTalk)));
    const stopping = busy && !text.trim() && !pack.length;

    const stop = async () => {
        if (sending || taken.current) return;
        taken.current = true;
        setSending(true);
        await run("session.stop", name, {});
        setSending(false);
    };

    const pick = (item) => {
        if (!item || item.off) return;
        const value = item.value;
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
        if (!body && !pack.length) return;
        if (sending || taken.current) return;
        if (lists) {
            setText("");
            onPicker(lists);
            return;
        }
        if (opens) {
            setText("");
            onScreen(opens);
            return;
        }
        if (side) {
            setText("");
            onSide(side.question);
            return;
        }
        taken.current = true;
        if (cmd) return sendCommand();
        const key = `${Date.now()}-${Math.random().toString(36).slice(2, 7)}`;
        const named = pack.map((f) => f.name).join(", ");
        const messageId = messageID(stream, pack);
        // A row that did not go out is drawn among the rows of the feed, and
        // those are written from the transcript — nothing is handed to them.
        // So the row carries what a second attempt takes: where the message
        // goes, what it says, whether it had files, and where to report how the
        // attempt went. Without that report a message that did arrive the
        // second time would stand in the feed twice — once as the failed
        // bubble, once as its echo.
        const row = {
            role: "me", key, at: new Date().toISOString(),
            text: body || named,
            sent: body,
            file: named || undefined,
            from: { name, text: body, files: pack.length },
            messageId,
            done: (patch) => { if (onLocalDone) onLocalDone(key, patch); },
        };
        setText("");
        if (pack.length && onDropFiles) onDropFiles();
        if (hold) {
            if (onLocal) onLocal({ ...row, state: "held", hold: { text: body, files: pack, messageId } });
            return;
        }
        setSending(true);
        if (onLocal) onLocal({ ...row, state: "sending" });
        const result = await deliver(run, name, { text: body, files: pack, messageId });
        setSending(false);
        if (onLocalDone) onLocalDone(key, outcome(result));
    };

    const keys = (e) => {
        // The list under the line is walked with the arrows and taken with Tab,
        // the way a terminal's is; Enter keeps sending what is typed.
        if (hints.length > 0 && (e.key === "ArrowDown" || e.key === "ArrowUp") && !e.shiftKey && !e.altKey) {
            e.preventDefault();
            const step = e.key === "ArrowDown" ? 1 : -1;
            setHot((at) => (at + step + hints.length) % hints.length);
            return;
        }
        if (hints.length > 0 && e.key === "Tab" && !e.shiftKey) {
            e.preventDefault();
            pick(hints[Math.min(hot, hints.length - 1)]);
            return;
        }
        if (!asksSend(e, window.matchMedia(WIDE).matches)) return;
        e.preventDefault();
        if (cantSend) return;
        send();
    };

    return html`
        <div class="composer">
            ${hints.length > 0 && html`<${SlashList} hints=${hints} hot=${Math.min(hot, hints.length - 1)}
                                                   theirs=${Boolean(stream && theirs)} wide=${wideNow}
                                                   onHot=${setHot} onPick=${pick} />`}
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
            ${!strip && pack.length < FILES_MAX && html`<${PickFile} exec=${exec} onAsk=${onAsk} />`}
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
                        class=${`iconbtn accent sendbtn${hear.live ? " hearing" : ""}`}
                        type="button"
                        aria-label=${micLabel(asMic, hear.live, cmd, name, side)}
                        title=${micTitle(hear, asMic, hold, ready, why, canFile, fileWhy, pack, cmd)}
                        disabled=${cantSend}
                        onClick=${() => { if (!hear.tap(asMic)) send(); }}
                        onPointerDown=${(e) => hear.down(e, asMic)}
                        onPointerUp=${hear.up}
                        onPointerCancel=${hear.up}
                        onContextMenu=${(e) => hear.on && e.preventDefault()}
                    >${asMic || hear.live ? Icon.mic() : Icon.arrowup()}</button>
                `}
            ${strip && html`<div class="cstrip">
                ${pack.length < FILES_MAX && html`<${PickFile} exec=${exec} onAsk=${onAsk} />`}
                ${strip}
            </div>`}
        </div>
    `;
}

// SlashList is the list of commands under the line being typed. The session's
// own list is long and its descriptions are long: on a phone each row carries
// its description on a second line, cut; on a wide screen the rows carry the
// names alone, and the description of the row under the pointer stands beside
// the list, whole, the way the native client shows it.
function SlashList({ hints, hot, theirs, wide, onHot, onPick }) {
    const box = useRef(null);
    const [tip, setTip] = useState(null);
    const item = hints[hot];
    const beside = theirs && wide;
    // place puts the description beside the row it belongs to, where the row
    // is now: the list scrolls under a pointer that stays where it is.
    const place = () => {
        const list = box.current;
        const row = list && list.children[hot];
        if (!beside || !row) {
            setTip(null);
            return;
        }
        setTip({ top: row.offsetTop - list.scrollTop, left: list.offsetWidth + 8 });
    };
    useEffect(() => {
        const list = box.current;
        const row = list && list.children[hot];
        if (row && (row.offsetTop < list.scrollTop
            || row.offsetTop + row.offsetHeight > list.scrollTop + list.clientHeight)) {
            list.scrollTop = row.offsetTop < list.scrollTop
                ? row.offsetTop
                : row.offsetTop + row.offsetHeight - list.clientHeight;
        }
        place();
    }, [hot, beside, hints.length]);
    return html`
        <div class=${`slashbox${theirs ? " theirs" : ""}`}>
            <div class="slashlist" ref=${box} onScroll=${place}>
                ${hints.map((one, n) => html`
                    <button class=${`slashitem${n === hot ? " hot" : ""}${one.off ? " off" : ""}`} type="button"
                            key=${one.label + one.value}
                            aria-disabled=${one.off ? "true" : "false"}
                            title=${one.off ? one.hint : ""}
                            onPointerEnter=${() => onHot(n)}
                            onClick=${() => onPick(one)}>
                        <span class="slashname">${one.label}</span>
                        ${one.screen && html`<span class="slashtag">screen</span>`}
                        ${one.hint && !beside && html`<span class="slashhint">${one.hint}</span>`}
                    </button>
                `)}
            </div>
            ${beside && tip && item && item.hint && html`
                <div class=${`slashtip${item.off ? " off" : ""}`} role="tooltip"
                     style=${`top:${tip.top}px;left:${tip.left}px`}>${item.hint}</div>
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

function micLabel(asMic, live, cmd, name, side) {
    if (live) return `listening to what goes to session ${name} — press again to stop`;
    if (asMic) return `talk to session ${name}`;
    if (side) return `ask session ${name} aside`;
    return cmd ? `send a command to session ${name}` : `send to session ${name}`;
}

function micTitle(hear, asMic, hold, ready, why, canFile, fileWhy, pack, cmd) {
    if (hear.live) return "listening — press again to stop, and the words stay in the field";
    if (asMic) return "press to talk";
    const plain = hold ? "will go out when the session is free" : sendTitle(ready, why, canFile, fileWhy, pack, cmd);
    return hear.on ? `${plain} · hold to talk` : plain;
}

function sendTitle(ready, why, canFile, fileWhy, pack, cmd) {
    if (cmd) return cmd.ready ? "send the command" : "pick a command option";
    if (pack.length) {
        if (!canFile) return fileWhy;
        return pack.length > 1 ? `send ${pack.length} files — as one message` : "send the file";
    }
    return ready ? "send" : why;
}
