// Taking back a message that waits in the queue of a session on the stream.

import { useState } from "preact/hooks";

import { html } from "../../html.js";
import { useAction } from "../../actions/gate.js";
import { knows } from "../../exec.js";

// TakeBack stands under a message the session has not read yet. Editing is
// taking it back and putting its words into the composer: the queue has no
// editing of its own. A message the session read before the press cannot be
// taken back, and the line under it says so.
export function TakeBack({ row, name, exec, onGone, onEdit }) {
    const run = useAction();
    const [busy, setBusy] = useState(false);
    const [fail, setFail] = useState("");
    if (!row.messageId || !knows(exec, "session.unqueue")) return null;

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

    return html`
        <div class="mqueue">
            <button class="btn" type="button" disabled=${busy} onClick=${() => take(true)}>edit</button>
            <button class="btn" type="button" disabled=${busy} onClick=${() => take(false)}>take back</button>
        </div>
        ${fail && html`<p class="mwait crit mqueuefail">${fail}</p>`}
    `;
}
