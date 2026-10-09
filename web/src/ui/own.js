// A word for a value a project sets otherwise than its contour: the row of a
// project on a contour's page says these and nothing it merely inherits.

const ON_OFF = {
    remoteControl: ["RC", "no RC"],
    autoRestart: ["auto restart", "no auto restart"],
    panelTools: ["panel tools", "no panel tools"],
};

export function ownLabel(v) {
    const value = v.value;
    if (ON_OFF[v.key]) return value ? ON_OFF[v.key][0] : ON_OFF[v.key][1];
    switch (v.key) {
    case "agent": return value === "codex" ? "Codex" : "Claude Code";
    case "transport": return value === "stream" ? "stream" : "tmux";
    case "intent": return value === "" ? "no first message" : "first message";
    case "restartIntent": return "message after a restart";
    case "contextCap": return `cap ${value}%`;
    case "env": return "environment";
    case "args": return "extra arguments";
    default: return String(value);
    }
}
