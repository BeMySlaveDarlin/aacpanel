// What a session is waiting for, in words: the line a screen says it with, and
// the word and subject a session card opens its state's line with. A dialog
// open is a tool waiting for the person's leave to run.
const WAITS = {
    "dialog open": { line: "waiting: a dialog is open", word: "allow", subject: "a tool", permission: true },
    "input needed": { line: "waiting for input", word: "needs", subject: "input" },
    "sandbox request": { line: "waiting: the sandbox is asking for access", word: "sandbox", subject: "asks access" },
    "goal proposal": { line: "waiting: a goal was proposed", word: "goal", subject: "proposed" },
    "worker request": { line: "waiting: a worker request", word: "worker", subject: "request" },
};

// waitText returns the wait reason as a line for the screen.
export function waitText(reason) {
    if (!reason) return "waiting for an answer";
    return WAITS[reason] ? WAITS[reason].line : `waiting: ${reason}`;
}

// waitTip returns the same line capitalised for a tooltip.
export function waitTip(reason) {
    const say = waitText(reason);
    return say.charAt(0).toUpperCase() + say.slice(1);
}

// waitWords returns how a card says a wait reason: the word, its subject and
// whether it is a permission.
export function waitWords(reason) {
    if (!reason) return { word: "waits", subject: "for an answer", permission: false };
    return WAITS[reason] || { word: "waits", subject: reason, permission: false };
}
