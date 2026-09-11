function toBuf(s) {
    const pad = s.replace(/-/g, "+").replace(/_/g, "/");
    const raw = atob(pad + "=".repeat((4 - (pad.length % 4)) % 4));
    return Uint8Array.from(raw, (c) => c.charCodeAt(0));
}

function toB64(buffer) {
    return btoa(String.fromCharCode(...new Uint8Array(buffer)))
        .replace(/\+/g, "-")
        .replace(/\//g, "_")
        .replace(/=+$/, "");
}

async function post(url, body) {
    const response = await fetch(url, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        credentials: "same-origin",
        body: JSON.stringify(body || {}),
    });

    const text = await response.text();
    let data = null;
    try {
        data = JSON.parse(text);
    } catch (err) {
        data = null;
    }
    if (!response.ok) throw new Error((data && data.error) || serverSaid(response.status));
    return data;
}

function serverSaid(status) {
    if (status === 401) return "you need to sign in again";
    if (status === 403) return "the device no longer has access";
    if (status === 404) return "the server does not know this address";
    if (status === 503) return "the device database is unavailable — signing in is impossible for now";
    return `the server answered ${status}`;
}

function packCreate(c) {
    return {
        id: c.id,
        rawId: toB64(c.rawId),
        type: c.type,
        authenticatorAttachment: c.authenticatorAttachment || undefined,
        response: {
            clientDataJSON: toB64(c.response.clientDataJSON),
            attestationObject: toB64(c.response.attestationObject),
            transports: c.response.getTransports ? c.response.getTransports() : [],
        },
    };
}

function packGet(c) {
    return {
        id: c.id,
        rawId: toB64(c.rawId),
        type: c.type,
        authenticatorAttachment: c.authenticatorAttachment || undefined,
        response: {
            clientDataJSON: toB64(c.response.clientDataJSON),
            authenticatorData: toB64(c.response.authenticatorData),
            signature: toB64(c.response.signature),
            userHandle: c.response.userHandle ? toB64(c.response.userHandle) : "",
        },
    };
}

export function supported() {
    return typeof window !== "undefined" && Boolean(window.PublicKeyCredential);
}

export async function login() {
    const options = (await post("/auth/passkey/login/begin")).publicKey;
    options.challenge = toBuf(options.challenge);
    (options.allowCredentials || []).forEach((c) => {
        c.id = toBuf(c.id);
    });

    const credential = await navigator.credentials.get({ publicKey: options });
    await post("/auth/passkey/login/finish", packGet(credential));
}

export async function register(code, name) {
    const options = (await post("/auth/passkey/register/begin", { code })).publicKey;
    options.challenge = toBuf(options.challenge);
    options.user.id = toBuf(options.user.id);
    (options.excludeCredentials || []).forEach((c) => {
        c.id = toBuf(c.id);
    });

    const credential = await navigator.credentials.create({ publicKey: options });
    await post("/auth/passkey/register/finish", { name, credential: packCreate(credential) });
}

export async function methods() {
    const response = await fetch("/auth/methods", { credentials: "same-origin" });
    if (!response.ok) throw new Error(serverSaid(response.status));
    const data = await response.json();
    return { passkey: Boolean(data.passkey), token: Boolean(data.token), home: typeof data.home === "string" ? data.home : "" };
}

export async function loginToken(token) {
    await post("/auth/token", { token });
}

export async function logout() {
    try {
        await fetch("/logout", { method: "POST", credentials: "same-origin" });
    } finally {
        location.href = "/login";
    }
}

export function human(err) {
    if (!err) return "";
    if (err.name === "NotAllowedError") return "The sign-in was cancelled or timed out.";
    if (err.name === "InvalidStateError") return "This device is already registered.";
    if (err.name === "SecurityError") return "The domain does not match the one the key is bound to.";
    if (err.name === "AbortError") return "The operation was aborted.";
    return err.message || "It did not work.";
}
