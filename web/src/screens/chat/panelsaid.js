// What the panel said to a session on the person's behalf. Three messages are
// written by the panel rather than typed: a secret saved, the answers to a
// brief and a reading of the branch waiting. The transcript keeps them as
// prompts of the person, and the text the session got carries no mark of the
// panel — a mark would be read by the model as well. So a message of the panel
// is told by the shape of its whole text, and a text that only resembles one
// stays a message of the person.
//
// The writers are secretMessage in internal/executor/secret.go, briefReply in
// cmd/aacpanel/briefs.go and signal in screens/repo/notes.js. Their words and
// the shapes below are held together by the samples in
// web/check/testdata/panel-said.json: a writer that changes a word turns a
// test red instead of leaving the feed to draw a bubble.

const KEY = "[A-Za-z_][A-Za-z0-9_]*";
const KEYS = `${KEY}(?:, ${KEY})*`;

const SECRET = new RegExp(
    '^The secret "([a-z0-9][a-z0-9._-]{0,63})" is saved: (/.+?) \\(0600, (\\d+) (lines?)\\)\\.'
    + `(?: Keys: (${KEYS})(?:; left empty: (${KEYS}))?\\.)?`
    + " Use it by its path and never read or print it: whatever you read goes into the transcript\\.$",
);

const READING = new RegExp(
    "^A reading of this branch is waiting for you: (/.+?) — (\\d+) (notes?), "
    + "each with the line it stands on\\. Read the file and answer here\\.$",
);

// The head of the answers to a brief: its title as Go quotes it, its name and
// the count. What follows is a block a question.
const BRIEF = /^Brief ("(?:[^"\\\n]|\\.)*") · ([a-z0-9][a-z0-9-]{0,63})\nAnswered (\d+) of (\d+)(?:\n|$)/;
const QUESTION = /^(\S*) (.+)$/;
const PICK = /^ {3}(\S+) · (.*)$/;
const NOTE = "   note: ";

// A reading is fetched back by the name of its file.
const READING_ID = /^([A-Za-z0-9_-]{1,120})\.json$/;

// one says whether a count agrees with the word after it: "1 line", "2 lines".
const one = (n, word) => (n === 1) === !word.endsWith("s");

function secret(text) {
    const m = SECRET.exec(text);
    if (!m) return null;
    const [, name, path, count, unit, keyList, emptyList] = m;
    const lines = Number(count);
    const keys = keyList ? keyList.split(", ") : [];
    const empty = emptyList ? emptyList.split(", ") : [];
    if (!one(lines, unit) || !path.endsWith(`/${name}`)) return null;
    if (empty.some((key) => !keys.includes(key))) return null;
    return { kind: "secret", name, path, dir: path.slice(0, -name.length), lines, keys, empty };
}

function reading(text) {
    const m = READING.exec(text);
    if (!m) return null;
    const notes = Number(m[2]);
    if (!one(notes, m[3])) return null;
    const file = READING_ID.exec(m[1].slice(m[1].lastIndexOf("/") + 1));
    return { kind: "reading", path: m[1], id: file ? file[1] : "", notes };
}

// unquote reads the title the way Go wrote it with %q. JSON reads the same
// escapes for any title a person types; one it does not is shown as written.
function unquote(quoted) {
    try {
        return JSON.parse(quoted);
    } catch {
        return quoted.slice(1, -1);
    }
}

const answered = (row) => row.skipped || row.picks.length > 0 || row.note !== "";

// A question is said whole once something stands under it.
const settled = (row) => row.none || answered(row);

// brief reads the answers back question by question: a blank line, the number
// and the question, then what it got — skipped, no answer, or the picks — and
// the note last. A note may run over several lines with no indent; they are
// the note's until the blank line before the next question. Whatever does not
// fall into that order, or does not add up to the count of the head, is not
// the panel's text.
function brief(text) {
    const m = BRIEF.exec(text);
    if (!m) return null;
    const rows = [];
    let row = null;
    let head = false;
    let inNote = false;
    const rest = text.slice(m[0].length);
    for (const line of rest ? rest.split("\n") : []) {
        if (head) {
            const q = QUESTION.exec(line);
            if (!q) return null;
            row = { n: q[1], title: q[2], picks: [], skipped: false, none: false, note: "" };
            rows.push(row);
            head = false;
            inNote = false;
            continue;
        }
        if (line === "") {
            if (row && !settled(row)) return null;
            head = true;
            continue;
        }
        if (!row) return null;
        if (inNote) {
            row.note += `\n${line}`;
            continue;
        }
        if (line.startsWith(NOTE)) {
            if (row.none) return null;
            row.note = line.slice(NOTE.length);
            inNote = true;
            continue;
        }
        if (row.skipped || row.none) return null;
        if (line === "   skipped" && !row.picks.length) {
            row.skipped = true;
            continue;
        }
        if (line === "   no answer" && !row.picks.length) {
            row.none = true;
            continue;
        }
        const pick = PICK.exec(line);
        if (!pick) return null;
        row.picks.push({ key: pick[1], label: pick[2] });
    }
    if (head || (row && !settled(row))) return null;
    const done = Number(m[3]);
    const total = Number(m[4]);
    if (rows.filter(answered).length !== done || rows.length > total) return null;
    return {
        kind: "brief",
        title: unquote(m[1]),
        id: m[2],
        answered: done,
        total,
        skipped: rows.filter((r) => r.skipped).length,
        none: rows.filter((r) => r.none).length,
        rows,
    };
}

// panelSaid returns what a message of the person says when the panel wrote
// it, or null when it is the person's own.
export function panelSaid(text) {
    const said = String(text || "").trim();
    if (said.startsWith('The secret "')) return secret(said);
    if (said.startsWith('Brief "')) return brief(said);
    if (said.startsWith("A reading of this branch ")) return reading(said);
    return null;
}
