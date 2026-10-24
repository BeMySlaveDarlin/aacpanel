// A command the person ran: a card in the feed, and its whole output
// in a sheet.
//
// A session in tmux writes the command and what it printed into the
// conversation as two rows, and so does a session on the stream once the
// command has ended, with the exit code besides. Until then the command is the
// page's own row: the card says it is starting, and then how long it has been
// running.

import { html } from "../../html.js";
import { knows } from "../../exec.js";
import { Icon } from "../../ui/icons.js";
import { useToast } from "../../ui/toasts.js";
import { copyText } from "./copy.js";
import { clock, useTick } from "./now.js";

// How many lines of the output the card shows: the last ones, where a command
// says how it went. The whole of it is a tap away.
export const TAIL_LINES = 6;

// shellOf reads what is typed as a shell command — the words after a leading
// "!" — or returns null for anything else.
export function shellOf(text) {
    const body = String(text || "").trim();
    if (!body.startsWith("!")) return null;
    return { command: body.slice(1).trim() };
}

export function canRunShell(exec, stream, shell) {
    return Boolean(stream && shell && knows(exec, "session.shell"));
}

// A fence keeps its indentation; only empty edge lines and the composer's
// leading bang leave it. Python goes through a quoted heredoc so the shell
// cannot expand its words, with an end marker the code cannot close early.
export function blockCommand(text, lang) {
    const code = text.replace(/^(?:[ \t]*\r?\n)+|(?:\r?\n[ \t]*)+$/g, "").replace(/^[ \t]*![ \t]*/, "");
    if (!/^(python|py|python3)$/i.test(lang)) return code;
    const lines = code.split(/\r?\n/);
    let end = "PY";
    for (let n = 1; lines.includes(end); n += 1) end = `PY_${n}`;
    return `python3 - <<'${end}'\n${code}\n${end}`;
}

// The page owns the row until the transcript takes it over. A fence waits
// for the gate before that row appears; a typed bang goes out at once.
export async function sendShell(run, name, { command, sent, messageId, onLocal, onLocalDone, confirm = false }) {
    const key = `${Date.now()}-${Math.random().toString(36).slice(2, 7)}`;
    const result = await run("session.shell", name, messageId ? { text: command, messageId } : { text: command }, {
        confirm,
        onSend: () => {
            if (onLocal) onLocal({ role: "shell", key, at: new Date().toISOString(), text: command,
                                   sent, messageId, state: "sending" });
        },
    });
    if (!result.cancelled && onLocalDone) {
        onLocalDone(key, result.ok
            ? { state: "running", since: new Date().toISOString() }
            : { state: "failed", error: result.error || "the session did not take it" });
    }
    return result;
}

// tailOf keeps the last lines of an output and counts the ones left above.
export function tailOf(text, n = TAIL_LINES) {
    const lines = String(text || "").split("\n");
    if (lines.length <= n) return { shown: lines.join("\n"), above: 0, lines: lines.length };
    return { shown: lines.slice(-n).join("\n"), above: lines.length - n, lines: lines.length };
}

// Stand says where a command stands: going out, running and for how long, or
// how it ended. A session in tmux says nothing of how a command ended, and
// neither does its card.
function Stand({ item }) {
    if (item.state === "sending") {
        return html`<span class="mshellat"><span class="mclock">${Icon.clock()}</span>starting</span>`;
    }
    if (item.state === "running") return html`<${Running} since=${item.since || item.at} />`;
    if (typeof item.code !== "number") return null;
    const ok = item.code === 0;
    return html`
        <span class=${`mshellat ${ok ? "ok" : "bad"}`} title=${ok ? "the command succeeded" : "the command failed"}>
            ${ok ? Icon.check() : Icon.close()}exit ${item.code}
        </span>
    `;
}

// Running is the clock of a command the host runs, by the second.
function Running({ since }) {
    useTick();
    const from = Date.parse(since);
    const took = Number.isFinite(from) ? clock((Date.now() - from) / 1000) : "";
    return html`<span class="mshellat run"><i class="mshelldot"></i>running ${took}</span>`;
}

// shellNow is the command the person ran last while it is still the last
// thing in the conversation: the page's own row on its way, or the command
// the transcript ended with, followed by nothing but its output. Anything said
// after it — a message, the session's next turn — takes the bar away.
export function shellNow(feed, pending) {
    if (pending.length > 0) {
        const last = pending[pending.length - 1];
        return last.role === "shell" ? last : null;
    }
    for (let i = feed.length - 1; i >= 0; i--) {
        if (feed[i].role === "shellout") continue;
        return feed[i].role === "shell" ? feed[i] : null;
    }
    return null;
}

// ShellBar says above the composer where the command the person ran stands,
// the way its card says it in the feed: a command started from a fence far up
// the conversation is followed from here, and a press takes the feed down to
// its card, the last one there is.
export function ShellBar({ item, onOpen }) {
    if (!item) return null;
    return html`
        <div class="workbar status">
            <button class="nowbar nowshell" type="button" onClick=${onOpen}
                    aria-label=${`the command ${item.text} — go to its card`}>
                <span class="nowtop">
                    <span class="mshellmark" aria-hidden="true">!</span>
                    <code class="nowshellcmd">${item.text}</code>
                    <${Stand} item=${item} />
                </span>
            </button>
        </div>
        ${item.state === "failed" && html`<p class="mwait crit">did not start: ${item.error}</p>`}
    `;
}

// ShellCommand is the head of the card: the mark, the command as typed, and
// where it stands.
export function ShellCommand({ item }) {
    const failed = item.state === "failed";
    return html`
        <div class=${`mshell${failed ? " failed" : ""}`}>
            <span class="mshellmark" role="img" aria-label="shell command">!</span>
            <code class="mshellcmd">${item.text}</code>
            <${Stand} item=${item} />
        </div>
        ${failed && html`<p class="mwait crit">did not start: ${item.error}</p>`}
    `;
}

// ShellOutput is the body of the card: the last lines of what the command
// printed, and the way to the whole of it.
export function ShellOutput({ item, onOpen }) {
    if (!item.text && !item.err) return null;
    const out = tailOf(item.text);
    const err = tailOf(item.err);
    const lines = (item.text ? out.lines : 0) + (item.err ? err.lines : 0);
    const more = out.above > 0 || err.above > 0 || item.cut;
    return html`
        ${item.text && html`<pre class="mshellout">${out.above > 0 && html`<span class="mshellabove">… ${out.above} more above</span>`}${out.shown}</pre>`}
        ${item.err && html`<pre class="mshellerr">${err.above > 0 && html`<span class="mshellabove">… ${err.above} more above</span>`}${err.shown}</pre>`}
        ${more && onOpen && html`
            <button class="mshellmore" type="button" onClick=${() => onOpen(item)}>
                the whole output · ${lines} ${lines === 1 ? "line" : "lines"}${Icon.chevron()}
            </button>
        `}
        ${more && !onOpen && item.cut && html`<p class="hint warn">The output is longer than shown — cut.</p>`}
    `;
}

// ShellSheet is the whole output of a command, as much of it as the feed keeps.
export function ShellSheet({ item }) {
    const toast = useToast();
    if (!item) return null;
    const all = [item.text, item.err].filter(Boolean).join("\n");
    return html`
        <div class="shellsheet">
            ${item.command && html`
                <div class="mshell">
                    <span class="mshellmark" role="img" aria-label="shell command">!</span>
                    <code class="mshellcmd">${item.command}</code>
                    <${Stand} item=${item} />
                </div>
            `}
            ${item.text && html`<pre class="shellwhole">${item.text}</pre>`}
            ${item.err && html`<pre class="shellwhole err">${item.err}</pre>`}
            ${item.cut && html`<p class="hint warn">The output is longer than the feed keeps — cut.</p>`}
            <button class="btn" type="button" onClick=${() => copyText(all, toast, "Output copied")}>
                ${Icon.copy()} Copy the output
            </button>
        </div>
    `;
}
