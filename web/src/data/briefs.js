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

// drop takes a brief off the shelf of the host, and the answers with it.
export async function drop(id) {
    const r = await fetch(`/api/briefs/${encodeURIComponent(id)}`, { method: "DELETE" });
    return body(r, "the brief was not removed");
}

// state is the one thing a card of a brief has to say: what this document wants
// from you. It lives here and not in a screen because the shelf and the shelf
// of a conversation both draw it, and two wordings of "3 of 5" would be two
// answers to the same question.
export function state(card) {
    if (!card) return { word: "", tone: "", share: 0 };
    if (card.sent) return { word: "sent", tone: "sent", share: 1 };
    if (!card.questions) return { word: "to read", tone: "read", share: 0 };
    const of = `${card.answered || 0} of ${card.questions}`;
    if ((card.answered || 0) >= card.questions) return { word: of, tone: "ready", share: 1 };
    if ((card.answered || 0) > 0) return { word: of, tone: "part", share: (card.answered || 0) / card.questions };
    return { word: of, tone: "fresh", share: 0 };
}

// waiting counts the briefs that want something: answered and unsent, or not
// answered at all. It is what a chip outside the shelf shows a number for.
export function waiting(cards) {
    let n = 0;
    for (const card of cards || []) {
        if (!card || card.sent) continue;
        n += 1;
    }
    return n;
}
