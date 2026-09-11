// The sign-in screen: a fingerprint for a known device, a registration code for a
// new one, and a long-lived token where passkey does not work at all.
import { useEffect, useRef, useState } from "preact/hooks";

import { html } from "../html.js";
import { human, login, loginToken, methods as askMethods, register, supported } from "../auth.js";
import { span } from "../format.js";

const REASONS = {
    idle: (after) => after
        ? `The session closed: ${after} without action. Sign in again.`
        : "The session closed: too long without action. Sign in again.",
    expired: (after) => after
        ? `The session lived ${after} — that is the limit, sign in again.`
        : "The session lived its full term — sign in again.",
    revoked: () => "This device no longer has access. A new registration code is needed.",
    none: () => "",
};

const DOORS = { passkey: true, token: false, home: "" };

function notice(reason, afterSec) {
    const text = REASONS[reason];
    return text ? text(span(afterSec)) : "";
}

function TokenForm({ busy, value, onInput, onSubmit }) {
    const field = useRef(null);
    useEffect(() => {
        if (field.current) field.current.focus();
    }, []);

    return html`
        <div class="gate-enroll">
            <p class="hint">
                The token is set at install time — the service variable <code>AACP_TOKEN</code>.
                It lets you in the same way a fingerprint does, so it has to be kept the same way.
            </p>
            <input
                ref=${field}
                class="search"
                type="password"
                placeholder="Access token"
                autocomplete="current-password"
                value=${value}
                onInput=${(e) => onInput(e.target.value)}
            />
            <button class="btn primary" type="button" disabled=${busy || value.trim() === ""} onClick=${onSubmit}>
                Sign in with the token
            </button>
        </div>
    `;
}

export function Login({ next = "/app", reason = "", afterSec = 0 }) {
    const [busy, setBusy] = useState(false);
    const [error, setError] = useState("");
    const [open, setOpen] = useState("");
    const [code, setCode] = useState("");
    const [name, setName] = useState("phone");
    const [token, setToken] = useState("");
    const [doors, setDoors] = useState(DOORS);
    const codeField = useRef(null);

    useEffect(() => {
        if (open === "enroll" && codeField.current) codeField.current.focus();
    }, [open]);

    useEffect(() => {
        let alive = true;
        askMethods().then((got) => alive && setDoors(got)).catch(() => {});
        return () => {
            alive = false;
        };
    }, []);

    const run = async (action) => {
        setBusy(true);
        setError("");
        try {
            await action();
            location.href = next;
        } catch (err) {
            setError(human(err));
        } finally {
            setBusy(false);
        }
    };

    const tokenForm = html`
        <${TokenForm}
            busy=${busy}
            value=${token}
            onInput=${setToken}
            onSubmit=${() => run(() => loginToken(token.trim()))}
        />
    `;

    if (!supported()) {
        return html`
            <div class="gate">
                <img class="gate-logo" src="/static/icons/icon.svg" alt="" />
                <h1>aacpanel</h1>
                ${doors.token
                    ? html`<p class="gate-sub">the browser cannot do passkey — sign in with the token</p>${tokenForm}`
                    : html`<p class="gate-error">The browser cannot do passkey — there is no way in from here.</p>`}
                ${error && html`<p class="gate-error" role="alert">${error}</p>`}
            </div>
        `;
    }

    return html`
        <div class="gate">
            <img class="gate-logo" src="/static/icons/icon.svg" alt="" />
            <h1>aacpanel</h1>
            <p class="gate-sub">the host at hand</p>

            ${notice(reason, afterSec) && html`<p class="gate-notice">${notice(reason, afterSec)}</p>`}

            ${doors.home && !doors.passkey && html`
                <p class="gate-sub">passkey sign-in lives on the domain</p>
                <a class="btn primary big" href=${`${doors.home}/login?next=${encodeURIComponent(next)}`}>Sign in through the domain</a>
            `}

            ${doors.passkey && html`
                <button class="btn primary big" type="button" disabled=${busy} onClick=${() => run(login)}>
                    ${busy ? "…" : "Sign in with a fingerprint"}
                </button>

                <button class="ghost" type="button" onClick=${() => setOpen(open === "enroll" ? "" : "enroll")}>
                    ${open === "enroll" ? "I already have access" : "new device"}
                </button>
            `}

            ${doors.token && html`
                <button class="ghost" type="button" onClick=${() => setOpen(open === "token" ? "" : "token")}>
                    ${open === "token" ? "hide the token" : "sign in with a token"}
                </button>
            `}

            ${open === "enroll" && html`
                <div class="gate-enroll">
                    <p class="hint">
                        The code is issued by a device that already has access — in the “devices” section.
                        If there is none, <code>aacpanel -enroll</code> prints it on the host.
                    </p>
                    <input
                        ref=${codeField}
                        class="search"
                        placeholder="Registration code"
                        autocomplete="one-time-code"
                        inputmode="latin"
                        value=${code}
                        onInput=${(e) => setCode(e.target.value)}
                    />
                    <input
                        class="search"
                        placeholder="Device name"
                        value=${name}
                        onInput=${(e) => setName(e.target.value)}
                    />
                    <button
                        class="btn primary"
                        type="button"
                        disabled=${busy || code.trim() === ""}
                        onClick=${() => run(() => register(code.trim(), name.trim() || "device"))}
                    >Add the device</button>
                </div>
            `}

            ${open === "token" && tokenForm}

            ${error && html`<p class="gate-error" role="alert">${error}</p>`}
        </div>
    `;
}
