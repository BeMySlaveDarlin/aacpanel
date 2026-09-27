// The badges of calls: one kind of call, or the thinking of a run, as a chip
// that opens the calls it counts. A run draws its own; a piece of work made of
// several runs draws their sum the same way.

import { html } from "../../html.js";
import { Icon } from "../../ui/icons.js";
import { callWord, KIND_NAMES, kindIcon, shortTokens, tokenWord } from "./labels.js";

// press opens the calls of a badge and keeps the tap to itself: a badge stands
// inside rows that open something of their own on a tap.
function press(onCalls) {
    return (event) => {
        event.stopPropagation();
        if (onCalls) onCalls();
    };
}

// failedOf counts the calls the host reported as failed.
export function failedOf(calls) {
    return (calls || []).filter((call) => call && call.failed).length;
}

// ThinkBadge counts the thinking blocks a run left without text.
export function ThinkBadge({ count, tokens, onCalls }) {
    if (!count) return null;
    return html`
        <button class="mtools mthink" type="button" onClick=${press(onCalls)}
                title=${`thinking: ${count}${tokens ? ` · ${shortTokens(tokens)} ${tokenWord(tokens)}` : ""}`}
                aria-label=${`thinking blocks: ${count}`}>
            <span class="mticon">${Icon.thinking()}</span>
            <span class="mtnum">${count}</span>
        </button>
    `;
}

// KindBadge counts the calls of one kind. A call the host reported as failed
// marks the badge and is counted in its title: the list it opens says which.
export function KindBadge({ kind, count, failed = 0, onCalls }) {
    const label = KIND_NAMES[kind] || KIND_NAMES.other;
    const broke = failed > 0 ? ` · ${failed} failed` : "";
    return html`
        <button class=${`mtools k-${kind}${failed > 0 ? " mtfail" : ""}`} type="button" onClick=${press(onCalls)}
                title=${`${label}${broke}`}
                aria-label=${`${label}: ${count} ${callWord(count)}${broke}`}>
            <span class="mticon">${kindIcon(kind)}</span>
            <span class="mtnum">${count}</span>
        </button>
    `;
}

// SumBadges draws a sum of runs: the thinking first, then one badge a kind.
// Every badge of the sum opens the calls of all the runs it adds up.
export function SumBadges({ sum, onCalls, cls = "" }) {
    if (!sum || (!sum.think && !sum.kinds.length)) return null;
    return html`
        <span class=${`epchips${cls ? ` ${cls}` : ""}`}>
            <${ThinkBadge} count=${sum.think} tokens=${sum.thinkTokens} onCalls=${onCalls} />
            ${sum.kinds.map((k) => html`
                <${KindBadge} key=${k.kind} kind=${k.kind} count=${k.count} failed=${k.failed} onCalls=${onCalls} />
            `)}
        </span>
    `;
}
