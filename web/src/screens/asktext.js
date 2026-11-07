import { html } from "../html.js";
import { render } from "../md.js";

// Inline plans are prose until at least two consecutive numbered items
// follow punctuation. Code and isolated numbers must keep their spelling.
export function splitQuestionLists(value) {
    const text = String(value || "");
    const items = [...text.matchAll(/(```[\s\S]*?(?:```|$)|`[^`\n]*`)|([:.;])([ \t]+)([1-9]\d*)\)(?=[ \t]+\S)/g)]
        .filter((m) => m[2]);
    const cuts = [];
    for (let i = 0; i < items.length; i++) {
        if (Number(items[i][4]) !== 1) continue;
        let end = i + 1;
        while (end < items.length && Number(items[end][4]) === end - i + 1
               && !/\n[ \t]*\n/.test(text.slice(items[end - 1].index, items[end].index))) end++;
        if (end - i > 1) cuts.push(...items.slice(i, end));
        i = end - 1;
    }
    let out = text;
    for (const item of cuts.reverse()) {
        const start = item.index + item[2].length;
        out = out.slice(0, start) + "\n" + out.slice(start + item[3].length);
    }
    return out;
}

export function AskText({ text }) {
    return html`<div class="asktext msg">${render(splitQuestionLists(text), { breaks: true })}</div>`;
}
