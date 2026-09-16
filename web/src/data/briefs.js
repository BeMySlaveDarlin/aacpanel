// Client for the /api/briefs/* endpoints behind the brief screens.
//
// The requests live here and not in the screens because a changing request is
// allowed past the action gate only from a named place: everything that writes
// is easier to find when it is in one file per notion.

async function body(r, what) {
    if (!r.ok) throw new Error((await r.text()) || `${what} (${r.status})`);
    return r.json();
}

// shelf returns the card of every brief on the host, newest first, or of one
// conversation when it is named.
export async function shelf(session) {
    const url = session ? `/api/briefs?session=${encodeURIComponent(session)}` : "/api/briefs";
    const r = await fetch(url);
    const got = await body(r, "the shelf did not open");
    return got.briefs || [];
}

// one returns a brief with the draft of its answers and the text that would go
// into the session right now.
export async function one(id) {
    const r = await fetch(`/api/briefs/${encodeURIComponent(id)}`);
    return body(r, "the brief did not open");
}

// saveDraft stores the answers as they stand and returns the text they make.
export async function saveDraft(id, answers) {
    const r = await fetch(`/api/briefs/${encodeURIComponent(id)}`, {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ answers }),
    });
    return body(r, "the draft was not saved");
}

// markSent records that the answers have gone into the session. The sending
// itself is an action and goes through the gate, not through here.
export async function markSent(id) {
    const r = await fetch(`/api/briefs/${encodeURIComponent(id)}/sent`, { method: "POST" });
    return body(r, "the mark was not saved");
}
