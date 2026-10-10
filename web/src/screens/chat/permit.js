// A session permission: the “Do you want to proceed?” dialog from the phone.
import { useCallback, useEffect, useLayoutEffect, useRef, useState } from "preact/hooks";

import { html } from "../../html.js";
import { Icon } from "../../ui/icons.js";
import { useAction } from "../../actions/gate.js";
import { knows, whyNot } from "../../exec.js";
import { SAFE_LINK } from "../../md.js";
import { waitText } from "../../ui/waits.js";

// The titles of the requests of codex's whose tool is a word of the panel's
// rather than a tool a person knows: a grant of more than the sandbox gives,
// and what an MCP server asks through codex — a page to open, a yes or a no.
// The tool keeps its word everywhere else, since what the row of a session
// says it waits for is read off it; and a command, a change to files and a
// call of a tool of a server are titled by their tool here too, as claude's
// are.
const CODEX_TITLES = new Map([
    ["Permissions", "More access than the sandbox gives"],
    ["MCP", "An MCP server asks"],
]);

// Permit renders the permission the session is asking for. Codex says the
// session is codex's: its requests have titles of their own.
export function Permit({ name, exec, waitingFor, codex, onAnswered }) {
    const [state, setState] = useState({ state: "load" });
    const [sending, setSending] = useState(0);
    const [fail, setFail] = useState("");
    const run = useAction();

    // Reading what the session is asking. It is read again after a keypress the
    // executor refused: the dialog it compares against is the one on the screen
    // now, and telling a person to look again is worth nothing while the card
    // in front of them still shows what was there a minute ago.
    const look = useCallback(async () => {
        try {
            const r = await fetch(`/api/session/permission?name=${encodeURIComponent(name)}`,
                { credentials: "same-origin" });
            return await r.json();
        } catch (err) {
            return { state: "unknown", reason: err.message };
        }
    }, [name]);

    useEffect(() => {
        let alive = true;
        setState({ state: "load" });
        look().then((body) => { if (alive) setState(body); });
        return () => { alive = false; };
    }, [name, look]);

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
            tail: perm.tail,
        });
        setSending(0);
        if (!result.ok) {
            setFail(result.error || "the keypress did not go through");
            setState(await look());
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
                <p class="hint">it can be answered in the terminal itself — the dialog is there</p>
            </div>
        `;
    }

    if (!perm) {
        const canStop = knows(exec, "session.stop");
        // The panel could not parse the dialog, but the lines it stands on are still an
        // answer to "what is it asking" — unmarked text beats sending a person to the terminal.
        const raw = (shown && shown.raw) || [];
        return html`
            <div class="permit">
                ${foreign
                    ? html`<p class="hint warn">${raw.length > 0
                        ? "there is a dialog on the session screen that the panel does not know — here it is as it stands there"
                        : "there is a dialog on the session screen that the panel does not know — what it asks cannot be seen from here"}</p>`
                    : html`<p class="hint">the session is ${waitText(waitingFor)} — what is there, the panel did
                        not parse</p>`}
                ${raw.length > 0 && html`<${Request} lines=${raw} note=${[]} />`}
                ${state.escaped
                    ? html`<p class="hint">Esc sent — if there was a dialog, it is closed</p>`
                    : html`<p class="hint">it can only be answered in the terminal; from here — close the dialog
                        without confirming anything</p>`}
                <div class="askopts">
                    <div class=${`askopt${sending > 0 || !canStop ? " off" : ""}`}>
                        <button
                            class="askpick"
                            type="button"
                            disabled=${sending > 0 || !canStop}
                            title=${canStop ? "" : whyNot(exec, "session.stop")}
                            onClick=${escape}
                        >
                            <span class="asktick permitn">${Icon.close()}</span>
                            <span class="askbody permittext"><span class="askname">Close the dialog — Esc</span></span>
                        </button>
                    </div>
                </div>
                <p class="hint warn">Esc cancels: the permission is not given, a held letter is not
                    delivered, and queued messages are dropped</p>
                ${fail && html`<p class="hint crit">${fail}</p>`}
            </div>
        `;
    }

    return html`
        <div class="permit">
            <div class="askhead">
                <span class="asklabel">Permission</span>
            </div>
            <p class="asktext permittool">${(codex && CODEX_TITLES.get(perm.tool)) || perm.tool || "Do you want to proceed?"}</p>

            <${Request} lines=${perm.action || []} note=${perm.note || []} />

            ${perm.url && html`
                <p class="permiturl">${SAFE_LINK.test(perm.url)
                    ? html`<a href=${perm.url} target="_blank" rel="noopener noreferrer">${perm.url}</a>`
                    : perm.url}</p>
            `}

            ${perm.cut && html`
                <p class="hint warn">the command is longer than shown — its beginning is above the terminal
                    screen and cut off. Read the tail before pressing anything</p>
            `}

            ${perm.partial && html`
                <p class="hint warn">not everything is visible: one of the dialog lines was not parsed by the panel.
                    If the item you need is not here — answer in the terminal</p>
            `}

            <div class="askopts">
                ${(perm.options || []).map((o) => html`
                    <div
                        key=${o.n}
                        class=${`askopt${sending === o.n ? " on" : sending > 0 || !ready ? " off" : ""}`}
                    >
                        <button
                            class="askpick"
                            type="button"
                            disabled=${sending > 0 || !ready}
                            title=${ready ? "" : whyNot(exec, "session.permit")}
                            onClick=${() => press(o.n)}
                        >
                            <span class="asktick permitn">${o.n}</span>
                            <span class="askbody permittext"><span class="askname">${o.text}</span></span>
                            ${o.lasting && html`<span class="permitlast">from now on</span>`}
                        </button>
                    </div>
                `)}
            </div>

            ${fail && html`<p class="hint crit">${fail}</p>`}
            ${!ready && html`<p class="hint warn">${whyNot(exec, "session.permit")}</p>`}
        </div>
    `;
}

// Request is what the session asks to do and why it asks, folded to the first
// lines of each. The answers under it are what a person came to the dock for,
// and a request can be a whole edit: drawn whole, it pushes them to the bottom
// of the screen under a wall of code. The fold shows its button only when it
// hides something, and opened, the request scrolls in a box of its own. What
// the fold hides is measured again when the screen turns: text that fit across
// a phone held sideways does not fit across one held upright.
function Request({ lines, note }) {
    const [open, setOpen] = useState(false);
    const [hides, setHides] = useState({ request: false, note: false });
    const box = useRef(null);
    const text = lines.join("\n");
    const why = note.join("\n");

    useLayoutEffect(() => {
        const el = box.current;
        if (!el || open) return undefined;
        const over = (pre) => Boolean(pre) && pre.scrollHeight > pre.clientHeight + 1;
        const measure = () => setHides({
            request: over(el.querySelector(".permitaction")),
            note: over(el.querySelector(".permitnote")),
        });
        measure();
        window.addEventListener("resize", measure);
        return () => window.removeEventListener("resize", measure);
    }, [text, why, open]);

    return html`
        <div ref=${box} class=${`permitsaid${open ? " open" : hides.request ? " hides" : ""}`}>
            <pre class="permitaction">${text}</pre>
            ${why && html`<pre class="permitnote">${why}</pre>`}
            ${(hides.request || hides.note || open) && html`
                <button class="mfmore" type="button" aria-expanded=${open ? "true" : "false"}
                        onClick=${() => setOpen(!open)}>
                    ${open ? "collapse" : "show all"}
                </button>
            `}
        </div>
    `;
}
