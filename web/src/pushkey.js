// The VAPID key travels from the server as base64url text, and the browser
// takes it as bytes. Both the page and the service worker subscribe, and the
// worker is bundled on its own — so the conversion lives here, apart from the
// hooks the page needs and the worker must not pull in.
export function toKey(base64) {
    const pad = base64.replace(/-/g, "+").replace(/_/g, "/");
    const raw = atob(pad + "=".repeat((4 - (pad.length % 4)) % 4));
    return Uint8Array.from(raw, (c) => c.charCodeAt(0));
}
