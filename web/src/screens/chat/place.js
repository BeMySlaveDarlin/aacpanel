import { createContext } from "preact";
import { useContext } from "preact/hooks";

import { html } from "../../html.js";
import { Icon } from "../../ui/icons.js";

export const PlaceContext = createContext(null);

const CALL = /^> \[(.+?) · (.+?)( · failed)? · use (\S+) · pos (\d+)\.(\d+)\]$/;
const FILE = /^> \[file (\/.+)\]$/;

function short(text, limit) {
    const chars = Array.from(text);
    return chars.length > limit ? chars.slice(0, limit - 1).join("") + "…" : text;
}

export function callPlace(call, result) {
    return {
        kind: "call", tool: call.name,
        arg: short(String(call.arg || "").replace(/\s+/g, " ").trim(), 80) || "—",
        use: call.use, pos: call.pos, index: call.index || 0,
        failed: Boolean(result.failed),
        excerpt: String(result.result || "").split(/\r?\n/).filter((line) => line.trim())
            .slice(-3).map((line) => short(line, 160)),
    };
}

export function filePlace(file) {
    return { kind: "file", path: file.path };
}

export function placeText(place, text = "") {
    const head = place.kind === "file" ? `file ${place.path}`
        : `${place.tool} · ${place.arg}${place.failed ? " · failed" : ""} · use ${place.use} · pos ${place.pos}.${place.index}`;
    const quote = [`> [${head}]`, ...(place.excerpt || []).map((line) => `> ${line}`)].join("\n");
    return text ? `${quote}\n\n${text}` : quote;
}

// Only a heading at the start of a message identifies a place. A quoted
// example or a heading with more words on its line remains ordinary Markdown.
export function readPlace(text) {
    const lines = String(text || "").split("\n");
    const call = CALL.exec(lines[0]);
    const file = FILE.exec(lines[0]);
    if (!call && !file) return null;
    const place = call ? {
        kind: "call", tool: call[1], arg: call[2], failed: Boolean(call[3]),
        use: call[4], pos: Number(call[5]), index: Number(call[6]), excerpt: [],
    } : { kind: "file", path: file[1] };
    let next = 1;
    while (next < lines.length && lines[next].startsWith("> ")) {
        if (call) place.excerpt.push(lines[next].slice(2));
        next += 1;
    }
    if (next < lines.length && lines[next] !== "") return null;
    return { place, text: lines.slice(next + 1).join("\n") };
}

export function PlaceCard({ place, draft = false }) {
    const actions = useContext(PlaceContext);
    const open = !draft && actions && actions.open;
    const Tag = open ? "button" : "div";
    const arg = place.arg && place.arg !== "—" ? ` · ${place.arg}` : "";
    const label = place.kind === "file" ? place.path.split("/").pop() || place.path
        : `${place.tool}${arg}${place.failed ? " · failed" : ""}`;
    return html`<${Tag} class=${`placecard${place.failed ? " failed" : ""}`} type=${open ? "button" : undefined}
                      onClick=${open ? () => actions.open(place) : undefined}>
        <span class="placehead"><span class="placeico">${place.kind === "file" ? Icon.file() : Icon.terminal()}</span>
            <span>${label}</span></span>
        ${place.excerpt && place.excerpt.length > 0 && html`<pre class="placeexcerpt">${place.excerpt.join("\n")}</pre>`}
    <//>`;
}
