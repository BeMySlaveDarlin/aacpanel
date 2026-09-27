// The command a project's next launch runs, as the service built it with the
// launcher's own code: each word marked by where its value came from — the
// project's own, the contour's — and the plumbing of the stream folded into
// one word, since nobody reads it and everybody reads past it.
import { html } from "../html.js";

const STREAM = "transport";

// fold returns the words to show: a run of the stream's own words is one.
export function fold(words) {
    const out = [];
    for (const w of words || []) {
        const last = out[out.length - 1];
        if (w.key === STREAM && last && last.key === STREAM && last.folded) continue;
        out.push(w.key === STREAM ? { ...w, text: "⟨stream⟩", folded: true } : w);
    }
    return out;
}

const FAMILY = /(opus|sonnet|haiku|fable)/i;

function family(model) {
    const found = FAMILY.exec(String(model || ""));
    return found ? found[1].toLowerCase() : "";
}

function titled(word) {
    return word ? word[0].toUpperCase() + word.slice(1) : word;
}

// drift says, a line each, where a live session of the project runs otherwise
// than the project's launch says: the console against the feed, or another
// family of model where the launch names one. What the account picks is not
// in the line, and a session that picks for itself is not held against it.
export function drift(line, live) {
    const out = [];
    if (!line || !live) return out;
    const stream = onStream(line);
    const liveStream = live.transport === "stream";
    if (stream !== liveStream) {
        out.push(`${live.session} runs in the ${liveStream ? "feed" : "console"} — the project says the ${stream ? "feed" : "console"}`);
    }
    const words = line.words || [];
    const at = words.findIndex((w) => w.key === "model" && w.text === "--model");
    const want = at >= 0 && words[at + 1] ? family(words[at + 1].text) : "";
    const runs = family(live.model);
    if (want && runs && want !== runs) {
        out.push(`${live.session} runs ${titled(runs)} — the project says ${titled(want)}`);
    }
    return out;
}

// onStream says whether the line starts a session on the stream.
export function onStream(line) {
    return Boolean(line && (line.words || []).some((w) => w.key === STREAM));
}

export function LaunchLine({ line }) {
    if (!line || !(line.words || []).length) return null;
    const then = line.then || [];
    return html`
        <div class="lline">
            <code class="lwords">
                ${fold(line.words).map((w, i) => html`
                    <span class="lw" key=${i} data-layer=${w.layer || ""}
                          title=${w.key ? `${w.key}${w.layer ? ` · ${w.layer}` : ""}` : undefined}>${w.text}</span>
                `)}
            </code>
            ${then.length > 0 && html`
                <span class="lthen">then: ${then.map((w) => (w.key === "intent" ? "the first message" : w.text)).join(" · ")}</span>
            `}
        </div>
    `;
}
