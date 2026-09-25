// Who wrote a file: each line with the commit that last wrote it, and the
// commit with the conversation it was written in.

import { useState } from "preact/hooks";

import { html } from "../../html.js";
import { since } from "../../format.js";
import { commitOf, useAsk } from "./data.js";
import { Painted } from "./lines.js";

// The name a line nobody has committed goes by: the working tree wrote it.
const UNCOMMITTED = "worktree";

// BlameLines is a window of a file with the commit of each line beside it. A
// run of lines from one commit is labelled once, at its top, and runs take
// turns in tone, so where one commit ends and the next begins reads without a
// label on every line. A tap on a line opens its commit right under it.
export function BlameLines({ cwd, first, lines, spans, blame, commits, head, onConversation }) {
    const [picked, setPicked] = useState(null);
    let run = 0;
    return html`
        <div class="cdhunk cdblame">
            <div class="cdhead"><span class="cdpath">${head}</span></div>
            <div class="cdlines">
                ${lines.map((text, i) => {
                    const sha = blame[i] || "";
                    const top = i === 0 || (blame[i - 1] || "") !== sha;
                    if (top && i > 0) run += 1;
                    const key = sha || UNCOMMITTED;
                    const at = picked && picked.index === i;
                    const commit = commits[sha];
                    return [
                        html`
                            <div key=${i} class=${`cdln blln${run % 2 ? " odd" : ""}${top ? " top" : ""}${picked && picked.key === key ? " picked" : ""}`}>
                                <button class="blgut" type="button"
                                        aria-label=${`line ${first + i}: ${sha ? `commit ${sha.slice(0, 7)}` : "not committed"}`}
                                        onClick=${() => setPicked(at ? null : { index: i, key })}>
                                    <span class="blwho">${top ? whoSays(sha, commit) : ""}</span>
                                    <span class="cdno">${first + i}</span>
                                </button>
                                <span class="cdsrc"><${Painted} text=${text} spans=${spans && spans[i]} /></span>
                            </div>
                        `,
                        at ? html`<${CommitCard} key=${`card-${i}`} cwd=${cwd} sha=${sha} commit=${commit}
                                                 onConversation=${onConversation}
                                                 onClose=${() => setPicked(null)} />` : null,
                    ];
                })}
            </div>
        </div>
    `;
}

function whoSays(sha, commit) {
    if (!sha) return "not committed";
    return commit && commit.at ? `${sha.slice(0, 7)} · ${since(commit.at)}` : sha.slice(0, 7);
}

// CommitCard says who wrote a commit and when, what it says it did, and where
// it was written: the conversation its trailer leads to, or that it has none.
// The conversation is looked up only for a commit that names one — a commit
// written by hand is said to be one, and nothing is looked for.
export function CommitCard({ cwd, sha, commit, onConversation, onClose }) {
    const names = Boolean(sha && commit && commit.session);
    const [found] = useAsk(() => commitOf(cwd, sha), [cwd, sha], names);
    const talk = found.kind === "ready" ? found.data.conversation : null;
    return html`
        <div class="blcard" role="group" aria-label="the commit of this line">
            ${!sha
                ? html`<p class="blsay">Not committed yet: the working tree wrote this line.</p>`
                : html`
                    <div class="blhead">
                        <code class="blsha">${sha.slice(0, 7)}</code>
                        <span class="blby">${(commit && commit.author) || "unknown author"}</span>
                        ${commit && commit.at && html`<span class="blat">${since(commit.at)} ago</span>`}
                    </div>
                    ${commit && commit.subject && html`<p class="blsubj">${commit.subject}</p>`}
                    ${!names && html`<p class="blsay">Written without a session: there is no conversation to open.</p>`}
                    ${names && found.kind === "loading" && html`<p class="blsay">Looking for the conversation…</p>`}
                    ${names && found.kind === "failed" && html`<p class="blsay crit">${found.error}</p>`}
                    ${names && found.kind === "ready" && (talk
                        ? html`
                            <button class="btn blopen" type="button" disabled=${!onConversation}
                                    onClick=${() => onConversation(talk)}>
                                Open the conversation ${talk.name ? `«${talk.name}»` : ""}
                                <span class="blstate">${talk.live ? "live" : "closed"}</span>
                            </button>
                        `
                        : html`
                            <p class="blsay">It names a conversation this machine does not keep —
                                <a href=${commit.session} target="_blank" rel="noopener">open it on claude.ai</a>.</p>
                        `)}
                `}
            <button class="blclose" type="button" aria-label="close the commit" onClick=${onClose}>×</button>
        </div>
    `;
}
