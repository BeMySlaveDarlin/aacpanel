// The secrets the host keeps for sessions: the files the notepad of a card
// saved, by name, size and time. A secret is filled in from its card in the
// conversation; this list is where one is taken off the host. Nothing here
// reads a file: the list knows that a file is there, not what it says.
import { useCallback, useEffect, useState } from "preact/hooks";

import { html } from "../html.js";
import { useAction } from "../actions/gate.js";
import { knows, whyNot } from "../exec.js";
import { bytes } from "../format.js";
import { list } from "../data/secrets.js";
import { Trouble } from "../ui/trouble.js";
import { stampText } from "./chat/labels.js";

export function SecretsShelf({ exec }) {
    const run = useAction();
    const [shelf, setShelf] = useState(null);
    const [error, setError] = useState("");
    const can = knows(exec, "secret.drop");

    const load = useCallback(async () => {
        try {
            setShelf(await list());
            setError("");
        } catch (err) {
            setError(String((err && err.message) || err));
        }
    }, []);

    useEffect(() => {
        load();
    }, [load]);

    const drop = async (name) => {
        const result = await run("secret.drop", name);
        if (result && result.ok) load();
    };

    const rows = (shelf && shelf.secrets) || [];
    return html`
        <div class="grouphead">Secrets of sessions</div>
        <p class="hint sethead">
            Files a session asked for and you filled in from its card. The session uses one by its path and
            never reads it; the panel shows the name, the size and the time, never the text.
        </p>
        ${shelf && shelf.dir && html`<p class="skdir"><code>${shelf.dir}</code></p>`}

        ${!shelf && !error && html`<p class="hint">Loading…</p>`}

        ${error && html`
            <${Trouble} what="secrets" error=${error}
                hint="the secrets themselves stay where they are: this is only the list of them." />
        `}

        ${shelf && rows.length === 0 && html`
            <p class="empty skempty">
                No secrets on the host. A session asks for one with a card in its conversation, and the
                notepad of the card saves it here.
            </p>
        `}

        ${rows.map((secret) => html`
            <section class="card setrow skrow" key=${secret.name}>
                <div class="setline">
                    <span class="setkey">${secret.name}</span>
                    <button class="btn danger skdrop" type="button" disabled=${!can}
                            aria-label=${`remove the secret ${secret.name}`}
                            onClick=${() => drop(secret.name)}>Remove</button>
                </div>
                <div class="setmeta">
                    <span>${bytes(secret.bytes)}</span>
                    ${stampText(secret.at) && html`<span>saved ${stampText(secret.at)}</span>`}
                </div>
            </section>
        `)}
        ${rows.length > 0 && !can && html`<p class="hint">${whyNot(exec, "secret.drop")}</p>`}
    `;
}
