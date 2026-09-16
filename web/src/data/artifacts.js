// Client for the /api/artifacts/* endpoints behind the artifact viewer.
//
// An artifact is published into the account its session works under, and the
// person reading the panel is signed into one account at a time. The panel
// keeps a copy of the page as it went out, and these are the requests that
// reach it.

async function body(r, what) {
    if (!r.ok) throw new Error((await r.text()) || `${what} (${r.status})`);
    return r.json();
}

// shelf returns the card of every kept page, of one conversation or of the
// whole machine.
export async function shelf(session) {
    const url = session ? `/api/artifacts?session=${encodeURIComponent(session)}` : "/api/artifacts";
    const r = await fetch(url);
    const got = await body(r, "the published pages did not open");
    return got.pages || [];
}

// pageURL is the address the viewer loads into its frame. The page is somebody
// else's code, and the answer carries its own sandbox, so this is never fetched
// and inlined here.
export function pageURL(id) {
    return `/api/artifacts/${encodeURIComponent(id)}/page`;
}

// key is what one published page is known by in both lists: the address it
// went out at, or the file it was made from when it has no address.
export function key(card) {
    if (!card) return "";
    if (card.url) return card.url;
    return card.file || (card.path || "").split("/").pop() || "";
}

// merge puts the feed and the shelf into one list of what this conversation
// published. The feed reaches only as far back as its window, and the shelf
// keeps what fell out of it: together they are the whole of it, newest first.
export function merge(fromFeed, fromShelf) {
    const out = new Map();
    for (const art of fromFeed || []) {
        const id = key(art);
        if (id) out.set(id, { ...art, kept: null });
    }
    for (const card of fromShelf || []) {
        const id = key(card);
        if (!id) continue;
        const was = out.get(id);
        out.set(id, was
            ? { ...was, kept: card }
            : {
                // A page the window no longer reaches is drawn from the copy
                // alone: the shelf carries what the card needs to say.
                title: card.title, desc: card.desc, file: card.file, icon: card.icon,
                url: card.url, count: card.versions, at: card.at, kept: card,
            });
    }
    return Array.from(out.values())
        .sort((a, b) => String(b.at || "").localeCompare(String(a.at || "")));
}

// index turns the shelf into what a card can look itself up in. A page is
// found by the address it was published at, and a page published without one
// by the file it was made from: both are what the feed already knows about an
// artifact.
export function index(cards) {
    const byURL = new Map();
    const byPath = new Map();
    for (const card of cards || []) {
        if (card.url) byURL.set(card.url, card);
        if (card.file) byPath.set(card.file, card);
    }
    return {
        of(art) {
            if (!art) return null;
            if (art.url && byURL.has(art.url)) return byURL.get(art.url);
            const file = art.file || (art.path || "").split("/").pop();
            if (file && byPath.has(file)) return byPath.get(file);
            return null;
        },
        size: (cards || []).length,
    };
}
