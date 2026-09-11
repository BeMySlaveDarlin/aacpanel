// Input into a session: bytes go out while the bridge is alive, and not one byte more.

// sender returns the input sender of one session.
export function sender({ fetch: f = fetch, path = "/api/term/input" } = {}) {
    let id = "";
    let gone = false;

    const send = async (bytes) => {
        if (gone || !id) return;
        try {
            const response = await f(`${path}?id=${encodeURIComponent(id)}`, {
                method: "POST",
                headers: { "Content-Type": "application/octet-stream" },
                body: bytes,
            });
            if (response && response.status === 410) gone = true;
        } catch (err) {
        }
    };

    return {
        send,
        open(next) {
            id = next || "";
            gone = !id;
        },
        stop() {
            gone = true;
        },
        get closed() {
            return gone;
        },
    };
}
