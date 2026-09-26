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
