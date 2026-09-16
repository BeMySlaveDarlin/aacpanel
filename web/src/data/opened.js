// What this device has already opened.
//
// The chip over the deck counts what is still standing: a page nobody has
// looked at yet. Whether it was looked at is a fact about this reader on this
// device, not about the host — a page opened on the phone stays open on the
// phone — so it is kept in the browser and never travels.
//
// Everything here survives the storage being blocked or wiped: the reader then
// sees the number it started with, which is the safe way to be wrong.

const KEY = "aacpanel.opened";

// A ceiling, so a year of publishing does not grow without end. The oldest
// marks go first, and a page that falls out is counted as new again — a
// harmless mistake in the direction of showing more rather than less.
const MAX = 400;

function read() {
    try {
        const raw = JSON.parse(localStorage.getItem(KEY) || "[]");
        return Array.isArray(raw) ? raw.filter((k) => typeof k === "string") : [];
    } catch {
        return [];
    }
}

function write(list) {
    try {
        localStorage.setItem(KEY, JSON.stringify(list.slice(-MAX)));
    } catch {
        // A private window, blocked storage, a full quota: the count is not
        // worth an error in the reader's face.
    }
}

// wasOpened says whether this device has opened that page before.
export function wasOpened(key) {
    if (!key) return false;
    return read().includes(key);
}

// markOpened records that it has, and returns whether that was new.
export function markOpened(key) {
    if (!key) return false;
    const list = read();
    if (list.includes(key)) return false;
    list.push(key);
    write(list);
    return true;
}

// unopened counts the ones this device has not seen yet.
export function unopened(keys) {
    const seen = new Set(read());
    let n = 0;
    for (const key of keys || []) {
        if (key && !seen.has(key)) n += 1;
    }
    return n;
}
