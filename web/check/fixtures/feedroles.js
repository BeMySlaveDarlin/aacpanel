// A feed with a row of every role the collector hands over, and a few of the
// ways they meet: a report with work after it, lines inside a run and beside
// it, a note before any work and after it, a message still in the queue, one
// taken back, a role this screen does not know. Every row carries a mark of
// its own — in its text, or in the name of its call — so a check can tell
// whether it reached the screen.

const base = Date.parse("2026-09-26T10:00:00Z");
const at = (sec) => new Date(base + sec * 1000).toISOString();
const call = (pos, name, extra = {}) => ({ name, arg: `${name.toLowerCase()}-arg`, at: at(pos), seq: 0, pos, index: 0, use: `u${pos}`, ...extra });

export const NOW = at(1030);

const report = "MARK-report — the first half is done. " + "The check ran twice and the bundle stayed the same. ".repeat(6)
    + "\n\nWhat is left is the second half.";

export const items = [
    // An exchange the window begins inside: work, a report, and work after it.
    { role: "tools", kind: "bash", run: 1, pos: 1, at: at(1), calls: [call(1, "MARK-call-1")] },
    { role: "think", run: 1, pos: 2, at: at(2), count: 1, tokens: 40, spots: [{ seq: 1, at: at(2), tokens: 40 }] },
    { role: "ai", pos: 3, at: at(3), text: report },
    { role: "mind", pos: 4, at: at(4), text: "MARK-mind-after-report" },
    { role: "tools", kind: "files", run: 5, pos: 5, at: at(5), calls: [call(5, "MARK-call-5")] },
    { role: "ai", pos: 6, at: at(6), text: "MARK-ai-6 and that settles the second half." },
    { role: "turn", pos: 7, at: at(7), ms: 7000 },

    // A message with pictures, a note before any work, and the work of it.
    { role: "me", pos: 10, at: at(10), text: "MARK-me-10 please look at the screenshots" },
    { role: "shots", pos: 11, at: at(10), shots: [{ index: 0, media: "image/png", bytes: 10 }] },
    { role: "note", pos: 12, at: at(12), text: "MARK-note-12 the context was compacted" },
    { role: "mind", pos: 13, at: at(13), text: "MARK-mind-13" },
    { role: "tools", kind: "bash", run: 14, pos: 14, at: at(14), calls: [call(14, "MARK-call-14", { failed: true })] },
    { role: "notice", pos: 15, at: at(15), level: "warn", text: "MARK-notice-15" },
    { role: "ai", pos: 16, at: at(16), text: "MARK-ai-16 narration on the way" },
    { role: "taskdone", pos: 17, at: at(17), use: "bg17", status: "completed", summary: "Background command \"MARK-taskdone-17\" completed" },
    { role: "tools", kind: "web", run: 18, pos: 18, at: at(18), calls: [call(18, "MARK-call-18")] },
    { role: "artifact", pos: 19, at: at(19), use: "u19", title: "MARK-artifact-19", file: "page.html", icon: "📄" },
    { role: "artifactlink", pos: 20, at: at(20), use: "u19", url: "https://claude.ai/artifact/MARK-link-20" },
    { role: "sent", pos: 21, at: at(21), text: "MARK-sent-21", files: [{ path: "/srv/proj/out.html", name: "out.html", size: 10 }] },
    { role: "brief", pos: 22, at: at(22), id: "b22", title: "MARK-brief-22", questions: 2 },
    { role: "mail", dir: "out", pos: 23, at: at(23), from: "neighbour", source: "session", text: "MARK-mailout-23" },
    { role: "ai", pos: 24, at: at(24), text: "MARK-ai-24 the outcome of the pictures",
      files: [{ path: "/srv/proj/web/a.css", name: "a.css", size: 10 }] },
    { role: "note", pos: 25, at: at(25), text: "MARK-note-25 the human interrupted the answer" },
    { role: "turn", pos: 26, at: at(26), ms: 16000 },

    // A shell command and its output.
    { role: "shell", pos: 30, at: at(30), text: "MARK-shell-30" },
    { role: "shellout", pos: 31, at: at(31), text: "MARK-shellout-31" },
    { role: "ai", pos: 32, at: at(32), text: "MARK-ai-32" },

    // A permission, then the call it let through.
    { role: "permitted", pos: 40, at: at(40), rows: [{ tool: "MARK-permitted-40", subject: "rm x", decision: "allow" }] },
    { role: "tools", kind: "bash", run: 41, pos: 41, at: at(41), calls: [call(41, "MARK-call-41")] },
    { role: "ai", pos: 42, at: at(42), text: "MARK-ai-42" },

    // A question answered, and one nobody answered.
    { role: "asked", pos: 50, at: at(50), use: "q50", asked: [{ text: "which one?", header: "pick", answer: ["MARK-asked-50"] }] },
    { role: "asked", pos: 51, at: at(51), use: "q51", status: "afk", asked: [{ text: "MARK-asked-51 and this one?" }] },
    { role: "ai", pos: 52, at: at(52), text: "MARK-ai-52" },

    // A wake-up, a letter from a neighbour, a command and its card.
    { role: "wake", pos: 60, at: at(60), text: "MARK-wake-60" },
    { role: "ai", pos: 61, at: at(61), text: "MARK-ai-61" },
    { role: "mail", pos: 70, at: at(70), from: "MARK-mail-70", source: "session", text: "a letter" },
    { role: "ai", pos: 71, at: at(71), text: "MARK-ai-71" },
    { role: "me", pos: 79, at: at(79), text: "/context MARK-me-79" },
    { role: "command", pos: 80, at: at(80), name: "context", data: { model: "claude", max: 1000, categories: [] } },
    { role: "turn", pos: 85, at: at(85), ms: 1000 },

    // A background task done after the turn: it opens an exchange of its own.
    { role: "taskdone", pos: 90, at: at(90), use: "bg90", status: "failed", summary: "Background command \"MARK-taskdone-90\" failed with exit code 2" },
    { role: "ai", pos: 91, at: at(91), text: "MARK-ai-91" },
    { role: "hologram", pos: 92, at: at(92), text: "MARK-unknown-92" },
    { role: "me", pos: 93, at: at(93), text: "MARK-withdrawn-93", state: "withdrawn" },

    // The exchange going on now, and a message waiting in the queue.
    { role: "me", pos: 100, at: at(100), text: "MARK-me-100 now this" },
    { role: "mind", pos: 101, at: at(1001), text: "MARK-mind-101" },
    { role: "tools", kind: "bash", run: 102, pos: 102, at: at(1002), calls: [call(102, "MARK-call-102", { at: at(1019), open: true })] },
    { role: "me", pos: 103, at: at(1020), text: "MARK-queued-103", state: "queued" },
];

// roleItems hands the feed over to a check run outside a page.
export function roleItems() {
    return items;
}
