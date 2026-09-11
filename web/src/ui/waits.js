// What a session is waiting for, in words.
const WAITS = {
    "dialog open": "waiting: a dialog is open",
    "input needed": "waiting for input",
    "sandbox request": "waiting: the sandbox is asking for access",
    "goal proposal": "waiting: a goal was proposed",
    "worker request": "waiting: a worker request",
};

// waitText returns the wait reason as a line for the screen.
export function waitText(reason) {
    if (!reason) return "waiting for an answer";
    return WAITS[reason] || `waiting: ${reason}`;
}

// waitTip returns the same line capitalised for a tooltip.
export function waitTip(reason) {
    const say = waitText(reason);
    return say.charAt(0).toUpperCase() + say.slice(1);
}
