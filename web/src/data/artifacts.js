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
