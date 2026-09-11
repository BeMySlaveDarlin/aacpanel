// Settings in words: what a change costs, where the value came from and what can
// be said about a secret at all.

export const COST = {
    live: { text: "Applies immediately", tone: "now" },
    session: { text: "Applies to the next session", tone: "next" },
    exec: { text: "After the executor restarts", tone: "restart" },
    agent: { text: "After the collector restarts", tone: "restart" },
    service: { text: "After the panel container restarts", tone: "restart" },
    recreate: { text: "Only by recreating the container", tone: "heavy" },
    never: { text: "Editing changes nothing", tone: "locked" },
};

// costOf returns the cost of a change by key, always in words.
export function costOf(cost) {
    const known = COST[cost];
    if (known) return known;
    return { text: `unknown cost: ${cost}`, tone: "heavy" };
}

export const SOURCE = {
    env: "from the process environment",
    file: "from the machine description",
    default: "not set anywhere",
};

export const GROUPS = [
    { id: "machine", title: "Machine", hint: "what this host is and how sessions open on it" },
    { id: "access", title: "Access", hint: "who gets in, from where, and over what" },
    { id: "session", title: "Sessions", hint: "how long a session lives and under whom it runs" },
    { id: "store", title: "Database", hint: "the names the panel connects with" },
    { id: "secrets", title: "Secrets", hint: "each one is shown as set or not set, never by value" },
];

// OTHER is the section a group the panel does not know falls into.
export const OTHER = { id: "", title: "Other", hint: "the panel does not know this group yet" };

// valueText returns what to print in place of the value.
export function valueText(item) {
    if (item.secret) return item.set ? "set" : "not set";
    if (!item.set) return "not set";
    return item.value;
}

// valueTone returns how the value should be set: as code or as plain words.
export function valueTone(item) {
    return !item.secret && item.set ? "code" : "word";
}

// clash returns the machine-description value when it differs from the environment.
export function clash(item) {
    if (item.secret) return "";
    return item.fileValue || "";
}

// byGroup returns the settings laid out by section, in the order of GROUPS.
export function byGroup(items) {
    const out = [];
    const seen = new Set();
    for (const group of GROUPS) {
        const own = (items || []).filter((item) => item.group === group.id);
        own.forEach((item) => seen.add(item.key));
        if (own.length > 0) out.push({ group, items: own });
    }
    const rest = (items || []).filter((item) => !seen.has(item.key));
    if (rest.length > 0) out.push({ group: OTHER, items: rest });
    return out;
}
