// A second attempt at a message the session did not take.

import { useState } from "preact/hooks";

import { useAction } from "../../actions/gate.js";
import { knows, useExec, whyNot } from "../../exec.js";

// The words the host writes into a refusal, read here to tell one refusal from
// another. Only one of them promises the message went nowhere at all, and only
// that promise makes a second attempt safe: after any other refusal it may
// have arrived, and sending it again says everything twice. A file the message
// carried is named in the same line — it already lies on the host, so the
// second attempt sends the path it landed at instead of the bytes, and nothing
// travels from the phone a second time. The phrases belong to the executor:
// they are written there and read here, and they move together.
const NOTHING_TYPED = "nothing was typed";
const ONE_FILE = "the file itself was saved to ";
const MANY_FILES = "what was saved to disk: ";

// resend says what a second attempt at this message would send, and nothing at
// all when there is no safe second attempt to make.
export function resend(from, error) {
    if (!from || !from.name) return null;
    const said = String(error || "");
    if (!said.includes(NOTHING_TYPED)) return null;
    const text = String(from.text || "");
    if (!from.files) return text.trim() ? { name: from.name, text } : null;
    const paths = savedPaths(said);
    if (!paths.length) return null;
    return { name: from.name, text: [text, ...paths].filter((line) => line.trim()).join("\n") };
}

// savedPaths reads where the files of the message landed on the host. The names
// come from the phone through a gate that lets nothing but letters, digits, a
// dash, an underscore and a dot through, so a comma in the line separates two
// paths and never stands inside one.
export function savedPaths(error) {
    const said = String(error || "");
    const one = said.lastIndexOf(ONE_FILE);
    if (one >= 0) return [said.slice(one + ONE_FILE.length).trim()].filter(Boolean);
    const many = said.lastIndexOf(MANY_FILES);
    if (many < 0) return [];
    return said.slice(many + MANY_FILES.length).split(",").map((path) => path.trim()).filter(Boolean);
}

// useSendAgain makes the second attempt the way it is made: the two steps the
// refusal asks for, Esc and then the same message once more. They stay two
// steps, because the second one is the ordinary send — the panel has one way
// of sending a message, not two.
//
// The Esc is not a blind key. The host reads the screen first, presses nothing
// while the composer is free, and refuses when what it found is a screen Esc
// does not close — and that refusal is said in fail instead of being followed
// by a message typed into whatever stands there now. Both steps are asked for
// before the attempt offers itself (ready, or why not): a host that knows one
// and not the other would press Esc and type nothing after it, leaving the
// session on a screen the person never opened.
export function useSendAgain(again, onDone) {
    const run = useAction();
    const exec = useExec();
    const [going, setGoing] = useState(false);
    const [fail, setFail] = useState("");
    const ready = knows(exec, "session.escape") && knows(exec, "session.send");
    const why = whyNot(exec, "session.escape") || whyNot(exec, "session.send");

    const press = async () => {
        setGoing(true);
        setFail("");
        const freed = await run("session.escape", again.name, {});
        if (!freed.ok) {
            setGoing(false);
            setFail(freed.error || "the composer did not come back");
            return;
        }
        const sent = await run("session.send", again.name, { text: again.text });
        setGoing(false);
        if (!sent.ok) {
            setFail(sent.error || "it did not go out the second time either");
            return;
        }
        onDone({ state: "queued", error: undefined });
    };

    return { ready, why, going, fail, press };
}
