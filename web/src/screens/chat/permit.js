// A session permission: the “Do you want to proceed?” dialog from the phone.
import { useEffect, useState } from "preact/hooks";

import { html } from "../../html.js";
import { Icon } from "../../ui/icons.js";
import { useAction } from "../../actions/gate.js";
import { knows, whyNot } from "../../exec.js";
import { waitText } from "../../ui/waits.js";

// Permit renders the permission the session is asking for.
export function Permit({ name, exec, waitingFor, onAnswered }) {
    const [state, setState] = useState({ state: "load" });
    const [sending, setSending] = useState(0);
    const [fail, setFail] = useState("");
    const run = useAction();

    useEffect(() => {
        let alive = true;
        setState({ state: "load" });
        (async () => {
            try {
                const r = await fetch(`/api/session/permission?name=${encodeURIComponent(name)}`,
                    { credentials: "same-origin" });
                const body = await r.json();
                if (alive) setState(body);
            } catch (err) {
                if (alive) setState({ state: "unknown", reason: err.message });
            }
        })();
        return () => { alive = false; };
    }, [name]);

    const shown = state.state === "ok" ? state.permission : null;
    const foreign = !!(shown && shown.unknown);
    const perm = foreign ? null : shown;
    const ready = knows(exec, "session.permit");

    const press = async (option) => {
        setSending(option);
        setFail("");
        const result = await run("session.permit", name, {
            option,
            dialog: perm.fingerprint,
        });
        setSending(0);
        if (!result.ok) {
            setFail(result.error || "the keypress did not go through");
            return;
        }
        setState({ state: "none" });
        if (onAnswered) onAnswered();
    };

    const escape = async () => {
        setSending(-1);
        setFail("");
        const result = await run("session.stop", name, {});
        setSending(0);
        if (!result.ok) {
            setFail(result.error || "Esc did not get through");
            return;
        }
        setState({ state: "none", escaped: true });
        if (onAnswered) onAnswered();
    };

    if (state.state === "load") {
        return html`<div class="permit"><p class="hint">looking at what the session is asking…</p></div>`;
    }
    if (state.state === "unknown") {
        return html`
            <div class="permit">
                <p class="hint warn">the permission was not read: ${state.reason || "the executor did not answer"}</p>
                <p class="hint">it can be answered in the console itself — the dialog is there</p>
            </div>
        `;
    }

    if (!perm) {
        const canStop = knows(exec, "session.stop");
        // The panel could not parse the dialog, but the lines it stands on are still an
        // answer to "what is it asking" — unmarked text beats sending a person to the console.
        const raw = (shown && shown.raw) || [];
        return html`
            <div class="permit">
                ${foreign
                    ? html`<p class="hint warn">${raw.length > 0
                        ? "there is a dialog on the session screen that the panel does not know — here it is as it stands there"
                        : "there is a dialog on the session screen that the panel does not know — what it asks cannot be seen from here"}</p>`
                    : html`<p class="hint">the session is ${waitText(waitingFor)} — what is there, the panel did
                        not parse</p>`}
                ${raw.length > 0 && html`<pre class="permitaction">${raw.join("\n")}</pre>`}
                ${state.escaped
                    ? html`<p class="hint">Esc sent — if there was a dialog, it is closed</p>`
                    : html`<p class="hint">it can only be answered in the console; from here — close the dialog
                        without confirming anything</p>`}
                <div class="permitopts">
                    <button
                        class="permitopt"
                        type="button"
                        disabled=${sending > 0 || !canStop}
                        title=${canStop ? "" : whyNot(exec, "session.stop")}
                        onClick=${escape}
                    >
                        <span class="permitn">${Icon.close()}</span>
                        <span class="permittext">Close the dialog — Esc</span>
                    </button>
                </div>
                <p class="hint warn">Esc cancels: the permission is not given, a held letter is not
                    delivered, and queued messages are dropped</p>
                ${fail && html`<p class="hint crit">${fail}</p>`}
            </div>
        `;
    }

    return html`
        <div class="permit">
            <div class="permithead">
                <span class="permittool">${perm.tool || "permission"}</span>
                <span class="permitwhat">the console asks for permission</span>
            </div>

            <pre class="permitaction">${(perm.action || []).join("\n")}</pre>

            ${perm.partial && html`
                <p class="hint warn">not everything is visible: one of the dialog lines was not parsed by the panel.
                    If the item you need is not here — answer in the console</p>
            `}

            <div class="permitopts">
                ${(perm.options || []).map((o) => html`
                    <button
                        key=${o.n}
                        class=${`permitopt${o.lasting ? " lasting" : ""}${sending === o.n ? " on" : ""}`}
                        type="button"
                        disabled=${sending > 0 || !ready}
                        title=${ready ? "" : whyNot(exec, "session.permit")}
                        onClick=${() => press(o.n)}
                    >
                        <span class="permitn">${o.n}</span>
                        <span class="permittext">${o.text}</span>
                        ${o.lasting && html`<span class="permitlast">from now on</span>`}
                    </button>
                `)}
            </div>

            ${fail && html`<p class="hint crit">${fail}</p>`}
            ${!ready && html`<p class="hint warn">${whyNot(exec, "session.permit")}</p>`}
        </div>
    `;
}
