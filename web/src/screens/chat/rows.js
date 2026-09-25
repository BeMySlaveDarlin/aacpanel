// Feed entries: what a row of the conversation looks like.

import { useEffect, useRef, useState } from "preact/hooks";

import { html } from "../../html.js";
import { useAction } from "../../actions/gate.js";
import { knows, useExec, whyNot } from "../../exec.js";
import { Icon } from "../../ui/icons.js";
import { render } from "../../md.js";
import { plural, stopwatch } from "../../format.js";
import { resend } from "./again.js";
import { idParam } from "./api.js";
import { CommandCard } from "./command.js";
import { FileAtts, SentCard } from "./files.js";
import { Photo, shotName } from "./photo.js";
import { callWord, KIND_NAMES, kindIcon, shortTokens, stampText, tokenWord } from "./labels.js";

// Row renders one row of the feed.
export function Row({ item, session, id, onCalls, onFile, onBrief, onCommand, copies, onPage }) {
    if (item.role === "shots") {
        const shots = item.shots || [];
        if (!shots.length) return null;
        return html`
            <div class="mshots">
                ${shots.map((shot) => {
                    const src = `/api/chat/image?session=${encodeURIComponent(session)}${idParam(id)}&pos=${item.pos}&i=${shot.index}`;
                    return html`<${Shot} key=${shot.index} src=${src}
                                         name=${shotName(shot, item.pos)} />`;
                })}
            </div>
        `;
    }

    if (item.role === "note") {
        return html`<div class="mnote">${item.text}</div>`;
    }
    if (item.role === "taskdone" || item.role === "notice") {
        return html`<${Line} item=${item} />`;
    }
    if (item.role === "turn") {
        return html`
            <div class="mturn">
                <span>worked ${stopwatch(item.ms / 1000)}</span>
                ${item.at && html`<span class="mturnat">${stampText(item.at)}</span>`}
            </div>
        `;
    }
    if (item.role === "mind") {
        return html`
            <div class="msg mmind">
                <span class="mmicon" role="img" aria-label="thinking">${Icon.thinking()}</span>
                ${render(item.text)}
                ${item.cut && html`<p class="hint warn">The thinking is longer than shown — cut.</p>`}
            </div>
        `;
    }
    if (item.role === "toolrow") {
        const groups = (item.groups || []).filter((group) => (group.calls || []).length);
        const think = item.think;
        if (!groups.length && !think) return null;
        return html`
            <div class="mrow">
                ${think && html`
                    <button class="mtools mthink" type="button" onClick=${onCalls}
                          title=${`thinking: ${think.count}${think.tokens ? ` · ${shortTokens(think.tokens)} ${tokenWord(think.tokens)}` : ""}`}
                          aria-label=${`thinking blocks: ${think.count}`}>
                        <span class="mticon">${Icon.thinking()}</span>
                        <span class="mtnum">${think.count}</span>
                    </button>
                `}
                ${groups.map((group) => {
                    const label = KIND_NAMES[group.kind] || KIND_NAMES.other;
                    const count = group.calls.length;
                    return html`
                        <button class=${`mtools k-${group.kind}`} type="button" key=${group.kind} onClick=${onCalls}
                                title=${label}
                                aria-label=${`${label}: ${count} ${callWord(count)}`}>
                            <span class="mticon">${kindIcon(group.kind)}</span>
                            <span class="mtnum">${count}</span>
                        </button>
                    `;
                })}
            </div>
            ${(item.lines || []).map((line) => html`<${Line} key=${`${line.role}-${line.pos}`} item=${line} under />`)}
        `;
    }
    if (item.role === "mail") {
        return html`<${Mail} item=${item} />`;
    }

    if (item.role === "wake") {
        return html`<${Wake} item=${item} />`;
    }

    if (item.role === "artifact") {
        return html`<${ArtifactCard} item=${item} copy=${copies && copies.of(item)} onOpen=${onPage} />`;
    }

    if (item.role === "brief") {
        return html`<${BriefCard} item=${item} onOpen=${onBrief} />`;
    }

    if (item.role === "asked") {
        return html`<${AskedCard} item=${item} />`;
    }

    if (item.role === "permitted") {
        return html`<${PermittedCard} item=${item} />`;
    }

    if (item.role === "sent") {
        return html`<${SentCard} item=${item} onOpen=${onFile} />`;
    }

    if (item.role === "command") {
        return html`<${CommandCard} item=${item} onOpen=${onCommand} />`;
    }

    if (item.role === "shell") {
        return html`
            <div class="mshell">
                <span class="mshellmark" role="img" aria-label="shell command">!</span>
                <code class="mshellcmd">${item.text}</code>
            </div>
        `;
    }
    if (item.role === "shellout") {
        if (!item.text && !item.err) return null;
        return html`
            ${item.text && html`<pre class="mshellout">${item.text}</pre>`}
            ${item.err && html`<pre class="mshellerr">${item.err}</pre>`}
            ${item.cut && html`<p class="hint warn">The output is longer than shown — cut.</p>`}
        `;
    }

    const failed = item.state === "failed";
    const gone = item.state === "withdrawn";
    const mine = item.role === "me";
    // A message on its way says so where its stamp will be: the bubble keeps
    // its height when the transcript echoes it, and only the line under it
    // changes from the state to the time.
    const wait = onTheWay(item.state);
    const again = failed ? resend(item.from, item.error) : null;
    return html`
        <div class=${`msg ${mine ? "me" : "ai"}${wait && !gone ? " queued" : ""}${failed ? " failed" : ""}${gone ? " withdrawn" : ""}`}>
            ${render(item.text, { breaks: mine })}
            ${!mine && html`<${FileAtts} files=${item.files} onOpen=${onFile} />`}
            ${item.cut && html`<p class="hint warn">The message is longer than shown — cut.</p>`}
            ${failed && html`<p class="mwait crit">did not go out: ${item.error}</p>`}
            ${again && item.done && html`<${SendAgain} again=${again} onDone=${item.done} />`}
        </div>
        ${mine && (wait || item.at) && html`<div class="mstamp">${wait || stampText(item.at)}</div>`}
    `;
}

// SendAgain stands under a message a session refused because a screen of its
// own held the keyboard. It does the two steps the refusal asks for: Esc, and
// then the same message once more. They stay two steps, because the second one
// is the ordinary send — the panel has one way of sending a message, not two.
//
// The Esc is not a blind key. The host reads the screen first, presses nothing
// while the composer is free, and refuses when what it found is a screen Esc
// does not close — and that refusal is shown here instead of being followed by
// a message typed into whatever stands there now.
function SendAgain({ again, onDone }) {
    const run = useAction();
    const exec = useExec();
    const [going, setGoing] = useState(false);
    const [fail, setFail] = useState("");
    // Both steps are asked for before the button offers itself: a host that
    // knows one and not the other would press Esc and type nothing after it,
    // leaving the session on a screen the person never opened.
    const ready = knows(exec, "session.escape") && knows(exec, "session.send");
    const why = whyNot(exec, "session.escape") || whyNot(exec, "session.send");

    const press = async () => {
        setGoing(true);
        setFail("");
        const freed = await run("session.escape", again.name, {});
        if (!freed.ok) {
            setGoing(false);
            setFail(freed.error || "the composer did not come back");
            return;
        }
        const sent = await run("session.send", again.name, { text: again.text });
        setGoing(false);
        if (!sent.ok) {
            setFail(sent.error || "it did not go out the second time either");
            return;
        }
        onDone({ state: "queued", error: undefined });
    };

    return html`
        <div class="magain">
            <button class="btn" type="button" disabled=${going || !ready}
                    title=${ready ? "press Esc in the session and send this message again" : why}
                    onClick=${press}>${going ? "sending again…" : "Esc and send again"}</button>
            ${fail && html`<p class="mwait crit">${fail}</p>`}
        </div>
    `;
}

// onTheWay names the state of a message that has not reached the transcript yet.
function onTheWay(state) {
    if (state === "sending") return html`<span class="mclock">${Icon.clock()}</span> going out`;
    if (state === "queued") return "queued";
    if (state === "held") return "will go out when the session is free";
    if (state === "withdrawn") return "taken back — the session did not read it";
    return null;
}

function Shot({ src, name }) {
    const box = useRef(null);
    const [url, setUrl] = useState("");
    const [error, setError] = useState("");
    const [want, setWant] = useState(typeof IntersectionObserver === "undefined");
    const [open, setOpen] = useState(false);

    useEffect(() => {
        if (want || !box.current) return undefined;
        const eye = new IntersectionObserver((entries) => {
            if (entries.some((e) => e.isIntersecting)) setWant(true);
        }, { rootMargin: "300px" });
        eye.observe(box.current);
        return () => eye.disconnect();
    }, [want]);

    useEffect(() => {
        if (!want) return undefined;
        let alive = true;
        let object = "";
        (async () => {
            try {
                const r = await fetch(src);
                if (!r.ok) throw new Error((await r.text()).trim() || `response ${r.status}`);
                const blob = await r.blob();
                if (!alive) return;
                object = URL.createObjectURL(blob);
                setUrl(object);
            } catch (e) {
                if (alive) setError(String(e.message || e));
            }
        })();
        return () => {
            alive = false;
            if (object) URL.revokeObjectURL(object);
        };
    }, [src, want]);

    return html`
        <button class="mshot" ref=${box} type="button" onClick=${() => setOpen(true)}
                aria-label=${`open attachment ${name}`}>
            ${url && html`<img src=${url} decoding="async" alt="attachment in the message" />`}
            ${error && html`<span class="mshotbad">✕</span>`}
        </button>
        ${open && html`<${Photo} url=${url} name=${name} error=${error}
                                 onClose=${() => setOpen(false)} />`}
    `;
}

function Wake({ item }) {
    const [open, setOpen] = useState(false);
    return html`
        <div class=${`mmail wake${open ? " open" : ""}`}>
            <button class="mmhead" type="button" onClick=${() => setOpen(!open)}
                    aria-expanded=${open ? "true" : "false"}>
                <span class="mmico">${Icon.alerts()}</span>
                <span class="mmfrom">wake-up</span>
                ${!open && html`<span class="mmpeek">${peek(item.text)}</span>`}
                ${item.at && html`<span class="mmat">${stampText(item.at)}</span>`}
            </button>
            ${open && html`
                <div class="mmbody">${render(item.text)}</div>
                ${item.cut && html`<p class="hint warn">The prompt is longer than shown — cut.</p>`}
            `}
        </div>
    `;
}

// What a letter is: a session next door, a subagent of this one, or a hook of
// the session speaking at the end of a turn. All three arrive among the
// prompts wrapped in a preamble nobody reads twice, so all three are drawn the
// same way — a line that says who, and the words themselves under it.
const MAIL_KINDS = { session: "session", agent: "agent", hook: "hook" };

const MAIL_WHO = { session: "neighbour session", agent: "subagent", hook: "stop hook" };

function Mail({ item }) {
    const [open, setOpen] = useState(false);
    const kind = MAIL_KINDS[item.source] || "agent";
    const out = item.dir === "out";
    const who = item.from || MAIL_WHO[kind];
    return html`
        <div class=${`mmail ${kind}${open ? " open" : ""}${out ? " out" : ""}`}>
            <button class="mmhead" type="button" onClick=${() => setOpen(!open)}
                    aria-expanded=${open ? "true" : "false"}>
                <span class="mmico">${Icon.envelope()}</span>
                <span class="mmdir">${out ? "to:" : "from:"}</span>
                <span class="mmfrom">${who}</span>
                ${!out && html`<span class="mmkind">${kind}</span>`}
                ${!open && html`<span class="mmpeek">${peek(item.text)}</span>`}
                ${item.at && html`<span class="mmat">${stampText(item.at)}</span>`}
            </button>
            ${open && html`
                <div class="mmbody">${render(item.text)}</div>
                ${item.cut && html`<p class="hint warn">The letter is longer than shown — cut.</p>`}
            `}
        </div>
    `;
}

// The tone and the mark of a background task by how it ended.
const DONE_TONES = { completed: "ok", failed: "crit", killed: "faint", stopped: "faint" };
const DONE_MARKS = { completed: "✓", failed: "✗" };

// doneName is what a finished task was called: the name in the quotes of
// claude's sentence about it, or the sentence when it has none.
function doneName(summary) {
    const quoted = /"(.+)"/.exec(summary || "");
    return quoted ? quoted[1] : (summary || "a background task");
}

// doneExit is the exit code a command ended with, when it is not a clean one.
function doneExit(summary) {
    const code = /exit code (\d+)/.exec(summary || "");
    return code && code[1] !== "0" ? `exit ${code[1]}` : "";
}

// Line is what arrives beside the conversation. A background task done is a
// pill: the name it ran under, how it ended, how long it took and what an
// agent spent. A warning of claude or the recap after an absence is a line,
// since its words do not fit a pill.
function Line({ item, under = false }) {
    const where = under ? " under" : "";
    if (item.role === "taskdone") {
        const aside = [
            item.ms > 0 && stopwatch(item.ms / 1000),
            item.tokens > 0 && `${shortTokens(item.tokens)} ${tokenWord(item.tokens)}`,
            doneExit(item.summary),
        ].filter(Boolean);
        return html`
            <div class=${`mdone s-${DONE_TONES[item.status] || "faint"}${where}`}
                 role="note" aria-label=${item.summary || "a background task ended"}>
                <span class="mdonemark" aria-hidden="true">${DONE_MARKS[item.status] || "–"}</span>
                <span class="mdonename">${doneName(item.summary)}</span>
                ${aside.length > 0 && html`<span class="mdoneaside">${aside.join(" · ")}</span>`}
            </div>
        `;
    }
    return html`
        <div class=${`mside s-${item.level || "info"}${where}`}>
            <span class="msidedot" aria-hidden="true"></span>
            <span class="msidetext">
                ${item.from && html`<b class="msidefrom">${item.from}</b>`}${item.text}
            </span>
        </div>
    `;
}

const ROUND_NAMES = {
    rejected: "rejected",
    afk: "nobody answered",
    failed: "the call never happened",
};

function AskedCard({ item }) {
    const rows = item.asked || [];
    const note = ROUND_NAMES[item.status];
    return html`
        <div class=${`asked${item.status ? " off" : ""}`}>
            <div class="askedhead">
                <span class="askedico">${Icon.ask()}</span>
                <span class="askedlabel">${rows.length > 1 ? "questions" : "question"}</span>
                ${note && html`<span class="askedwhy">${note}</span>`}
                ${item.at && html`<span class="askedat">${stampText(item.at)}</span>`}
            </div>
            ${rows.map((row, n) => html`
                <div class="askedrow" key=${n}>
                    ${row.header && html`<span class="askedtop">${row.header}</span>`}
                    <span class="askedq">${row.text}</span>
                    ${(row.answer || []).length
                        ? html`<span class="askeda">${row.answer.join(" · ")}</span>`
                        : !item.status && html`<span class="askeda skip">skipped</span>`}
                </div>
            `)}
        </div>
    `;
}

// permitAnswer says what a person answered, in the words of the dialog they
// answered in.
function permitAnswer(row) {
    if (row.decision === "deny") return "Denied";
    return row.lasting ? "Allowed, not asked again" : "Allowed";
}

// PermittedCard is what a person answered to the permissions of the calls
// above it: the transcript has the calls and nothing of the question.
function PermittedCard({ item }) {
    const rows = item.rows || [];
    return html`
        <div class="asked permitted">
            <div class="askedhead">
                <span class="askedico">${Icon.hand()}</span>
                <span class="askedlabel">${rows.length > 1 ? "permissions" : "permission"}</span>
                ${item.at && html`<span class="askedat">${stampText(item.at)}</span>`}
            </div>
            ${rows.map((row, n) => html`
                <div class="askedrow" key=${n}>
                    <span class="askedtop">${row.tool}</span>
                    ${row.subject && html`<code class="askedsubj">${row.subject}</code>`}
                    <span class=${`askeda${row.decision === "deny" ? " denied" : ""}`}>${permitAnswer(row)}</span>
                </div>
            `)}
        </div>
    `;
}

// ArtifactCard renders a published artifact as a card.
//
// A page goes out into the account its session works under, and the reader of
// the panel is signed into one account at a time. Where the panel kept a copy
// the card opens that, here, for anyone who can see the panel; the address it
// was published at stays available to whoever matches the account.
export function ArtifactCard({ item, copy, onOpen, onSeen }) {
    const body = html`
        <span class="arico">${item.icon || "📄"}</span>
        <span class="arbody">
            <span class="artitle">${item.again && item.label ? item.label : item.title}</span>
            ${(item.again ? item.note : item.desc) && html`
                <span class="ardesc">${item.again ? item.note : item.desc}</span>
            `}
            <span class="armeta">
                ${item.again ? html`<span class="arnew">update</span>` : ""}
                <span>${item.file}</span>
                ${item.count > 1 && html`
                    <span>${item.count} ${plural(item.count, "version", "versions")}</span>
                `}
            </span>
        </span>
        ${(copy || item.url) && html`<span class="crgo">${Icon.chevron()}</span>`}
    `;
    // Opened is opened, whether the page was read here or followed out to the
    // account it was published into: the count is of what the reader has not
    // looked at, not of what they looked at in one particular way.
    const seen = () => { if (onSeen) onSeen(); };
    if (copy && onOpen) {
        return html`
            <button type="button" class="artifact arhere"
                    onClick=${() => { seen(); onOpen(copy); }}>${body}</button>
        `;
    }
    if (!item.url) return html`<div class="artifact dead">${body}</div>`;
    return html`
        <a class="artifact" href=${item.url} target="_blank" rel="noopener noreferrer"
           onClick=${seen}>${body}</a>
    `;
}

// BriefCard renders a brief the session published: a document that waits for
// the person rather than a message they read in passing.
export function BriefCard({ item, onOpen }) {
    const asks = item.questions > 0;
    // A card listed after the document was answered says where it stands, not
    // only how much it asked: "5 of 5" and "sent" are different states, and a
    // row that says neither sends the reader in to find out.
    const mark = item.mark;
    const body = html`
        <span class="brico">${Icon.file()}</span>
        <span class="arbody">
            <span class="artitle">${item.title}</span>
            ${item.eyebrow && html`<span class="ardesc">${item.eyebrow}</span>`}
            <span class="armeta">
                <span class="arnew">brief</span>
                <span>${asks
                    ? `${item.questions} ${plural(item.questions, "question", "questions")}`
                    : "nothing to answer"}</span>
                ${mark && mark.word && html`
                    <span class=${`brstate is-${mark.tone}`}>${mark.word}</span>
                `}
            </span>
        </span>
        ${onOpen && html`<span class="crgo">${Icon.chevron()}</span>`}
    `;
    if (!onOpen) return html`<div class="artifact arbrief dead">${body}</div>`;
    return html`
        <button type="button" class="artifact arbrief" onClick=${() => onOpen(item.id)}>${body}</button>
    `;
}

function peek(text) {
    const line = (text || "").split("\n").map((s) => s.replace(/^[#>*\-\s]+/, "").trim())
        .find((s) => s.length > 0) || "";
    return line.length > 90 ? `${line.slice(0, 90)}…` : line;
}
