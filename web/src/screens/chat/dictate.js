// Dictation into the composer: the browser listens, the words land in the field.
//
// The recognition is the browser's own, and in Chrome that means the recording
// goes to Google. Nothing else in the panel leaves the machine, so this is off
// until the person turns it on, and the switch says so in as many words.

const KEY = "aacpanel.dictation";

// HOLD_MS is how long the send button is held before it starts listening. Short
// enough that the gesture does not feel stuck, long enough that an ordinary
// press on send never opens the microphone by accident.
export const HOLD_MS = 350;

// speech returns the recognition constructor of this browser, or null.
export function speech() {
    if (typeof window === "undefined") return null;
    return window.SpeechRecognition || window.webkitSpeechRecognition || null;
}

// dictation reports whether the person has turned dictation on for this device.
// The microphone belongs to the device, not to the account: a phone with
// dictation and a desk without it is an ordinary arrangement.
export function dictation(storage = safeStorage()) {
    try {
        return storage.getItem(KEY) === "on";
    } catch {
        return false;
    }
}

export function setDictation(on, storage = safeStorage()) {
    try {
        storage.setItem(KEY, on ? "on" : "off");
    } catch {
        // A browser that refuses storage keeps dictation off, which is the safe way round.
    }
    return on;
}

function safeStorage() {
    try {
        return window.localStorage;
    } catch {
        return { getItem: () => null, setItem: () => {} };
    }
}

// askMicrophone asks for the microphone once, at the moment the switch goes on,
// and lets go of it immediately. Asking here rather than on the first hold keeps
// the gesture clean: a question from the browser in the middle of a press eats
// the press, and the person sees a button that did nothing.
export async function askMicrophone(media = navigator.mediaDevices) {
    if (!media || !media.getUserMedia) return { ok: false, why: "this browser has no microphone to ask for" };
    try {
        const stream = await media.getUserMedia({ audio: true });
        for (const track of stream.getTracks()) track.stop();
        return { ok: true, why: "" };
    } catch (err) {
        return { ok: false, why: refusal(err) };
    }
}

function refusal(err) {
    const name = err && err.name;
    if (name === "NotAllowedError") return "the microphone was refused; the browser asks again from its own settings";
    if (name === "NotFoundError") return "this device has no microphone";
    return `the microphone did not open: ${(err && err.message) || name || "no reason given"}`;
}

// listen starts the browser's recognition and returns a handle with one stop.
// It reports the whole of what this dictation has heard so far, not the piece
// that just arrived: a phone does not hand over the pieces of a sentence, it
// hands over the sentence again each time it hears more of it, and a reader
// that adds them up writes the beginning over and over.
export function listen({ lang, onSaid, onEnd, make = speech() } = {}) {
    if (!make) return null;
    const rec = new make();
    rec.lang = lang || navigator.language || "en-US";
    rec.interimResults = true;
    rec.continuous = true;

    let done = false;
    const finish = (why) => {
        if (done) return;
        done = true;
        if (onEnd) onEnd(why);
    };

    // What this dictation has heard, and how far down the list it has read.
    let heard = "";
    let taken = 0;

    rec.onresult = (e) => {
        for (let i = taken; i < e.results.length; i++) {
            const piece = e.results[i];
            // Pieces come in order and the unfinished ones are last: the first
            // one still being made out ends this round, so nothing lands in the
            // field while the person is still saying it.
            if (!piece.isFinal) break;
            taken = i + 1;
            heard = merge(heard, String(piece[0].transcript || "").trim());
        }
        if (onSaid) onSaid(heard);
    };
    rec.onerror = (e) => finish(trouble(e && e.error));
    rec.onend = () => finish("");

    try {
        rec.start();
    } catch (err) {
        finish(`dictation did not start: ${(err && err.message) || err}`);
        return null;
    }
    return { stop: () => { try { rec.stop(); } catch { finish(""); } } };
}

// merge puts a finished piece into what the dictation has heard. A piece that
// carries everything already heard and goes further is the same sentence heard
// better, and takes the place of it; one that stops short of it is the same
// sentence heard worse, and is dropped. Anything else is new words.
export function merge(heard, said) {
    if (!said) return heard;
    if (!heard) return said;
    if (said === heard || said.startsWith(`${heard} `)) return said;
    if (heard.startsWith(`${said} `)) return heard;
    return `${heard} ${said}`;
}

// trouble turns the browser's one-word reason into something a person can act on.
function trouble(reason) {
    switch (reason) {
        case "no-speech": return "";
        case "aborted": return "";
        case "not-allowed":
        case "service-not-allowed":
            return "the microphone is closed for this page; the browser opens it from its own settings";
        case "network": return "dictation needs the network: the recognition is not done on this machine";
        case "audio-capture": return "the microphone did not open";
        default: return reason ? `dictation stopped: ${reason}` : "";
    }
}

// join puts a dictated piece into what is already written, without gluing words
// together and without a space in front of an empty field.
export function join(text, said) {
    const was = String(text || "").replace(/\s+$/, "");
    const add = String(said || "").trim();
    if (!add) return text || "";
    return was ? `${was} ${add}` : add;
}
