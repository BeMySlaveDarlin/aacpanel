// Client for GET /api/secrets: the files the host keeps in its secrets
// directory, by name, size and time. The text of a secret never comes back
// this way — it leaves the panel once, in the body of secret.put, and the
// list knows only that a file is there.

// list returns the directory and its secrets sorted by name. A host without
// the directory answers an empty list.
export async function list() {
    const r = await fetch("/api/secrets", { credentials: "same-origin" });
    if (!r.ok) throw new Error((await r.text()).trim() || `the list did not open (${r.status})`);
    const got = await r.json();
    return { dir: got.dir || "", secrets: got.secrets || [] };
}

// pathOf is where a secret of that name lies on the host, or the name alone
// when the directory is not known yet.
export function pathOf(shelf, name) {
    return shelf && shelf.dir ? `${shelf.dir}/${name}` : name;
}

// named returns the entry of that name, or null.
export function named(shelf, name) {
    return ((shelf && shelf.secrets) || []).find((s) => s.name === name) || null;
}

// savedAfter returns the entry of the card's name when it was written after
// the card was: the person has filled the notepad in since the session asked.
// An older file of the same name is an answer to an earlier question.
export function savedAfter(shelf, card) {
    const entry = named(shelf, card.name);
    if (!entry) return null;
    const saved = Date.parse(entry.at);
    const asked = Date.parse(card.at);
    if (!Number.isFinite(saved)) return null;
    if (Number.isFinite(asked) && saved <= asked) return null;
    return entry;
}
