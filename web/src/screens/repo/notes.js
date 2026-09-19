// The notes of a reading: what was written on a line, where it is kept, and
// the two pieces that draw it — the box under the line and the list of them.
//
// A reading is not held in the tab. The panel keeps it, the way it keeps the
// answers of a brief: a reload, a locked phone, a move from the sofa to the
// desk, and what was written down is still there. So every act that changes a
// note — writing one, editing it, taking it back — goes to the panel at once
// rather than waiting for a button nobody knew to press.
//
// The requests live here beside the screen that makes them rather than with
// the reading side of the viewer: this is the one part of it that writes, and
// what writes is easier to find when it stands apart from what only reads.

import { useCallback, useEffect, useRef, useState } from "preact/hooks";

import { ago } from "../../format.js";
import { html } from "../../html.js";
import { Icon } from "../../ui/icons.js";

// What a note may carry. The ceilings are the service's; holding them here as
// well means a note is stopped where it is written rather than quietly cut on
// its way out.
const TEXT_MAX = 4000;
const NOTES_MAX = 200;

async function body(r, what) {
    if (!r.ok) throw new Error((await r.text()).trim() || `${what} (${r.status})`);
    return r.json();
}

// readings returns the index of one conversation, newest first, with the notes
// of each: one answer rather than a reading opened per name.
async function readings(session) {
    const r = await fetch(`/api/reviews?session=${encodeURIComponent(session)}`);
    const got = await body(r, "the readings did not open");
    return got.reviews || [];
}

// start names a reading. The name is the service's to give — it becomes the
// name of a file on a shelf.
async function start(session, cwd, base) {
    const r = await fetch("/api/reviews", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ session, cwd, base }),
    });
    return body(r, "the reading did not start");
}

async function keep(id, draft) {
    const r = await fetch(`/api/reviews/${encodeURIComponent(id)}`, {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(draft),
    });
    return body(r, "the notes were not saved");
}

async function drop(id) {
    const r = await fetch(`/api/reviews/${encodeURIComponent(id)}`, { method: "DELETE" });
    return body(r, "the reading was not put down");
}

function noteId() {
    return `n${Date.now().toString(36)}${Math.random().toString(36).slice(2, 7)}`;
}

function blank(kind) {
    return { kind, id: "", notes: [], sentAt: null, at: "" };
}

// noteAt finds the note standing on a line. A line carries one: a second note
// on the same line is the first one said again, and what the writer meant was
// to change what they wrote.
export function noteAt(notes, path, line, quote) {
    return (notes || []).find(
        (n) => n.path === path && n.line === line && n.quote === quote,
    ) || null;
}

// useReview holds the reading of this directory and this conversation.
//
// It is started by the first note rather than by opening the screen: a reading
// with nothing written on it is a name on a shelf that says a review happened
// when none did. For the same reason the last note taken back puts the reading
// down again.
export function useReview(cwd, session, base) {
    const [state, setState] = useState(() => blank("loading"));
    const [trouble, setTrouble] = useState("");
    const box = useRef({
        id: "", sent: false, notes: [], base: "",
        pending: null, queue: Promise.resolve(), trouble: "",
    });
    box.current.base = base;

    const complain = useCallback((why) => {
        box.current.trouble = why;
        setTrouble(why);
    }, []);

    // step writes down where the reading has got to. It runs one at a time:
    // two notes written a moment apart would otherwise start two readings and
    // leave the second one holding the first one's note.
    const step = useCallback(async () => {
        const at = box.current;
        const notes = at.pending;
        if (notes === null) return;
        at.pending = null;
        try {
            if (!notes.length) {
                if (at.id) await drop(at.id);
                at.id = "";
                setState({ kind: "ready", id: "", notes: [], sentAt: null, at: "" });
                complain("");
                return;
            }
            if (!at.id) {
                const made = await start(session, cwd, at.base);
                at.id = made.id;
                at.sent = false;
            }
            const saved = await keep(at.id, { session, cwd, base: at.base, notes });
            at.notes = saved.notes || notes;
            at.sent = Boolean(saved.sentAt);
            setState({
                kind: "ready", id: at.id, notes: at.notes,
                sentAt: saved.sentAt || null, at: saved.updatedAt || "",
            });
            complain("");
        } catch (e) {
            // What did not reach the panel is held: the next write, and the
            // press of the send button, try it again rather than going out
            // with the note missing.
            if (at.pending === null) at.pending = notes;
            complain(String((e && e.message) || e));
        }
    }, [cwd, session, complain]);

    const write = useCallback((notes) => {
        const at = box.current;
        at.notes = notes;
        setState((was) => ({ ...was, kind: "ready", notes }));
        at.pending = notes;
        at.queue = at.queue.then(step, step);
        return at.queue;
    }, [step]);

    // What was written and has not reached the panel goes now. The send
    // carries the file the panel builds out of the saved reading, and a note
    // still in flight is a note the session never sees.
    const flush = useCallback(async () => {
        const at = box.current;
        if (at.pending !== null) at.queue = at.queue.then(step, step);
        await at.queue;
        return !at.trouble;
    }, [step]);

    useEffect(() => {
        let gone = false;
        const at = box.current;
        at.id = "";
        at.sent = false;
        at.notes = [];
        at.pending = null;
        complain("");
        setState(blank("loading"));
        if (!session) {
            setState(blank("ready"));
            return undefined;
        }
        readings(session)
            .then((list) => {
                if (gone) return;
                // The newest reading of this directory that has not gone yet:
                // the index arrives newest first, and a sent one is finished
                // business rather than the draft in hand.
                const mine = list.find((r) => r && r.cwd === cwd && !r.sentAt);
                if (!mine) {
                    setState(blank("ready"));
                    return;
                }
                at.id = mine.id;
                at.notes = mine.notes || [];
                setState({
                    kind: "ready", id: mine.id, notes: at.notes,
                    sentAt: null, at: mine.updatedAt || "",
                });
            })
            .catch((e) => {
                if (gone) return;
                setState(blank("ready"));
                complain(String((e && e.message) || e));
            });
        return () => { gone = true; };
    }, [cwd, session, complain]);

    const add = useCallback((path, line, quote, text) => {
        const at = box.current;
        const written = String(text || "").trim();
        if (!written || !path || !(line > 0)) return;
        // A reading that has gone is settled. What is written after it starts
        // the next one rather than changing what the session was handed.
        if (at.sent) {
            at.id = "";
            at.sent = false;
            at.notes = [];
            setState((was) => ({ ...was, id: "", notes: [], sentAt: null }));
        }
        const held = noteAt(at.notes, path, line, quote);
        if (held) {
            write(at.notes.map((n) => (n.id === held.id ? { ...n, text: written.slice(0, TEXT_MAX) } : n)));
            return;
        }
        if (at.notes.length >= NOTES_MAX) {
            complain(`a reading holds ${NOTES_MAX} notes, and this one is full`);
            return;
        }
        write([...at.notes, {
            id: noteId(), path, line,
            quote: String(quote == null ? "" : quote),
            text: written.slice(0, TEXT_MAX),
            at: new Date().toISOString(),
        }]);
    }, [write, complain]);

    // What the panel says about the reading, once it has left this screen. The
    // send happens outside the viewer, and a screen that went on taking notes
    // would be writing into a file the session is already reading.
    const recheck = useCallback(async () => {
        const at = box.current;
        if (!at.id) return;
        try {
            const r = await fetch(`/api/reviews/${encodeURIComponent(at.id)}`);
            const got = await body(r, "the reading did not open");
            at.sent = Boolean(got.sentAt);
            at.notes = got.notes || at.notes;
            setState({
                kind: "ready", id: got.id || at.id, notes: at.notes,
                sentAt: got.sentAt || null, at: got.updatedAt || "",
            });
            complain("");
        } catch (e) {
            complain(String((e && e.message) || e));
        }
    }, [complain]);

    const remove = useCallback((id) => {
        const at = box.current;
        if (at.sent) return;
        write(at.notes.filter((n) => n.id !== id));
    }, [write]);

    return {
        id: state.id,
        notes: state.notes,
        sentAt: state.sentAt,
        at: state.at,
        loading: state.kind === "loading",
        trouble,
        add, remove, flush, recheck,
    };
}

// NoteBox is the box under the line: the line itself quoted back, and the room
// to say what is wrong with it. It stands under the line rather than in a sheet
// over the code — what a note is about is the line above it, and a note written
// with the line hidden is written from memory.
export function NoteBox({ note, quote, onSave, onRemove, onClose }) {
    const [text, setText] = useState((note && note.text) || "");
    const area = useRef(null);

    useEffect(() => {
        if (area.current) area.current.focus();
    }, []);

    const written = text.trim();
    const save = () => {
        if (!written) return;
        onSave(written);
    };
    const keys = (e) => {
        if (e.key === "Escape") {
            e.preventDefault();
            onClose();
            return;
        }
        if (e.key === "Enter" && (e.ctrlKey || e.metaKey)) {
            e.preventDefault();
            save();
        }
    };

    return html`
        <div class="cdnote">
            <div class="cdnoteline">${(quote || "").trim() || "an empty line"}</div>
            <textarea
                class="cdnotein"
                ref=${area}
                rows="3"
                maxlength=${TEXT_MAX}
                aria-label="a note on this line"
                placeholder="what this line does wrong, or what it should say instead"
                value=${text}
                onKeyDown=${keys}
                onInput=${(e) => setText(e.currentTarget.value)}
            ></textarea>
            <div class="cdnoteact">
                <button class="cdnotekeep" type="button" disabled=${!written} onClick=${save}>
                    ${note ? "Save" : "Note it"}
                </button>
                <button class="cdnotecut" type="button" onClick=${onClose}>Cancel</button>
                ${note && html`
                    <button class="cdnotegone" type="button" onClick=${onRemove}>Remove</button>
                `}
            </div>
        </div>
    `;
}

// NotesPane is the reading as a list: every note with the line it stands on,
// and the way back to that line. It is the same list on both screens — a panel
// beside the code at a desk, a page of the stack on a phone.
export function NotesPane({ review, onOpen, onSend, wide }) {
    const [sending, setSending] = useState(false);
    // Why the reading did not go. It is kept apart from the trouble of saving
    // one: a note that never reached the panel and a reading the session would
    // not take are two different things to do something about.
    const [unsent, setUnsent] = useState("");
    const notes = review.notes || [];
    const sent = Boolean(review.sentAt);
    const canSend = Boolean(onSend) && notes.length > 0 && !sent && !sending;

    const send = async () => {
        if (!canSend) return;
        setSending(true);
        setUnsent("");
        const ok = await review.flush();
        if (!ok) {
            setSending(false);
            return;
        }
        try {
            await onSend({ id: review.id, notes });
        } catch (e) {
            setUnsent(String((e && e.message) || e));
        } finally {
            await review.recheck();
            setSending(false);
        }
    };

    return html`
        <div class="cdnotes">
            <div class="cdnhead">
                <span class="cdncount">${notes.length} ${notes.length === 1 ? "note" : "notes"}</span>
                ${sent
                    ? html`<span class="cdnstate cdngone">sent ${ago(review.sentAt)}</span>`
                    : review.at && html`<span class="cdnstate">kept ${ago(review.at)}</span>`}
            </div>

            ${review.trouble && html`<p class="hint crit">${review.trouble}</p>`}
            ${unsent && html`<p class="hint crit">${unsent}</p>`}
            ${sent && html`
                <p class="hint">This reading has gone to the session and stands as it was read. A note written now starts the next one.</p>
            `}

            ${!notes.length && !review.loading && html`
                <p class="hint">
                    Nothing is noted yet. ${wide ? "Click" : "Tap"} the number of a line to say what is wrong with it.
                </p>
            `}

            <div class="cdnlist">
                ${notes.map((n) => html`
                    <div class="cdnrow" key=${n.id}>
                        <button class="cdnat" type="button" title=${n.path}
                                onClick=${() => onOpen(n.path, n.line, n.quote)}>
                            <span class="cdnwhere">
                                <span class="cdnfile">${n.path.split("/").pop()}</span>
                                <span class="cdnline">:${n.line}</span>
                            </span>
                            <span class="cdnquote">${(n.quote || "").trim()}</span>
                            <span class="cdntext">${n.text}</span>
                        </button>
                        ${!sent && html`
                            <button class="cdnx" type="button"
                                    aria-label=${`remove the note on ${n.path} line ${n.line}`}
                                    onClick=${() => review.remove(n.id)}>
                                ${Icon.close ? Icon.close() : "×"}
                            </button>
                        `}
                    </div>
                `)}
            </div>

            <div class="cdnfoot">
                <button class="cdnsend" type="button" disabled=${!canSend} onClick=${send}>
                    ${Icon.send ? Icon.send() : ""}
                    <span>${sending ? "Sending…" : sent ? "Sent" : "Send the notes"}</span>
                </button>
            </div>
        </div>
    `;
}
