// Taking back a message that waits in the queue of a session on the stream.

import { useState } from "preact/hooks";

import { html } from "../../html.js";
import { useAction } from "../../actions/gate.js";
import { knows } from "../../exec.js";

// takesBack says whether a message in the queue can be taken back from it:
// the queue knows it by its id, and the host has to know the action.
export function takesBack(row, exec) {
    return Boolean(row.messageId) && knows(exec, "session.unqueue");
}

// useTakeBack takes a message out of the queue: take(true) puts its words
// back into the composer as well, take(false) only withdraws it. A message
// the session read before the press cannot be taken back, and fail says so.
export function useTakeBack({ row, name, onGone, onEdit }) {
    const run = useAction();
    const [busy, setBusy] = useState(false);
    const [fail, setFail] = useState("");

    const take = async (edit) => {
        setBusy(true);
        setFail("");
        const result = await run("session.unqueue", name, { messageId: row.messageId });
        setBusy(false);
        if (!result.ok) {
            if (!result.cancelled) setFail(result.error || "the message was not taken back");
            return;
        }
        onGone(row.key);
        if (edit) onEdit(row.sent || row.text || "");
    };

    return { busy, fail, take };
}

// The words of an edit that cannot be made: the queue holds the whole
// message, the screen only its head.
export const CUT_EDIT = "The full message is not loaded; take it back to withdraw it.";

// TakeBack stands under a message the session has not read yet. Editing is
// taking it back and putting its words into the composer: the queue has no
// editing of its own.
export function TakeBack({ row, name, exec, onGone, onEdit }) {
    const { busy, fail, take } = useTakeBack({ row, name, onGone, onEdit });
    if (!takesBack(row, exec)) return null;

    return html`
        <div class="mqueue">
            <button class="btn" type="button" disabled=${busy || row.cut}
                    title=${row.cut ? CUT_EDIT : undefined}
                    onClick=${() => take(true)}>edit</button>
            <button class="btn" type="button" disabled=${busy} onClick=${() => take(false)}>take back</button>
        </div>
        ${fail && html`<p class="mwait crit mqueuefail">${fail}</p>`}
    `;
}
