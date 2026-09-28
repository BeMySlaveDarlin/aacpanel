// Client for /api/push/prefs: which pushes the person wants, kept once for
// every device, and what the choice can name.
//
// The requests live here and not in the screen because a changing request is
// allowed past the action gate only from a named place.

const PREFS_URL = "/api/push/prefs";

async function body(r, what) {
    if (!r.ok) throw new Error((await r.text()).trim() || `${what} (${r.status})`);
    return r.json();
}

// load returns the choice and its sources: the contours with their projects,
// the stacks, the rules and the probes.
export async function load() {
    const got = await body(await fetch(PREFS_URL), "the choice of pushes did not open");
    return { prefs: got.prefs || {}, sources: got.sources || {} };
}

// save stores the choice whole and returns it as the service keeps it: sorted,
// without repeats and without what cannot be chosen.
export async function save(prefs) {
    const r = await fetch(PREFS_URL, {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(prefs),
    });
    const got = await body(r, "the choice of pushes was not saved");
    return got.prefs || prefs;
}
