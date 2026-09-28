// Feed entries: what a row of the conversation looks like.

import { useEffect, useLayoutEffect, useRef, useState } from "preact/hooks";

import { html } from "../../html.js";
import { useAction } from "../../actions/gate.js";
import { knows, useExec, whyNot } from "../../exec.js";
import { Icon } from "../../ui/icons.js";
import { dedent, leadOf, render } from "../../md.js";
import { plural, stopwatch } from "../../format.js";
import { resend } from "./again.js";
import { idParam } from "./api.js";
import { CommandCard } from "./command.js";
import { FileAtts, SentCard } from "./files.js";
import { Photo, shotName } from "./photo.js";
import { ShellCommand, ShellOutput } from "./shell.js";
import { shortTokens, stampText, tokenWord } from "./labels.js";

// Row renders one row of the feed: what is said and what arrives. A run of
// calls and the end of a turn are not rows — they stand on the timeline
// beside the feed (see timeline.js).
export function Row({ item, session, id, onFile, onBrief, onCommand, onShell, copies, onPage, onTask }) {
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
    // A background task done is a card of the build of the files sent to the
    // person; tasks that ended side by side come as one card with a row each.
    if (item.role === "tasks" || item.role === "taskdone") {
        return html`<${TaskCard} list=${item.list || [item]} onTask=${onTask} />`;
    }
    if (item.role === "notice") {
        return html`<${Line} item=${item} />`;
    }
    // A thought is set as an answer is: the same face, size and ink. What
    // marks it is a hairline on its left, and the word over the first thought
    // of a run of them.
    if (item.role === "mind") {
        return html`
            <div class="msg ai mmind">
                ${item.head !== false && html`<div class="mmtag">thinking</div>`}
                ${render(item.text)}
                ${item.cut && html`<p class="hint warn">The thinking is longer than shown — cut.</p>`}
            </div>
        `;
    }
    if (item.role === "mail") {
        return html`<${Letter} item=${item} />`;
    }

    if (item.role === "wake") {
        return html`<${Letter} item=${{ ...item, source: "wake" }} />`;
    }

    if (item.role === "artifact") {
        return html`<${PageCard} item=${item} copy=${copies && copies.of(item)} onOpen=${onPage} />`;
    }

    if (item.role === "brief") {
        return html`<${BriefDoc} item=${item} onOpen=${onBrief} />`;
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
        return html`<${ShellCommand} item=${item} />`;
    }
    if (item.role === "shellout") {
        return html`<${ShellOutput} item=${item} onOpen=${onShell} />`;
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

// What a letter is: a session next door, a subagent of this one, a hook of the
// session speaking at the end of a turn, or an alarm of the session waking it
// up. All four arrive among the prompts, the first three wrapped in a preamble
// nobody reads twice, so all four are drawn the same way — a card of the build
// of the files sent to the person: who and when on its head, the first lines
// of the letter, and the rest opened by a row under them.
const MAIL_KINDS = { session: "session", agent: "agent", hook: "hook", wake: "wake" };

const MAIL_WHO = { session: "neighbour session", agent: "subagent", hook: "stop hook" };

const MAIL_LABEL = {
    agent: ["from subagent", "to subagent"],
    session: ["from session", "to session"],
    hook: ["stop hook", "stop hook"],
    wake: ["wake-up", "wake-up"],
};

const MAIL_ICONS = { session: Icon.envelope, agent: Icon.envelope, hook: Icon.hook, wake: Icon.alerts };

// What the row under a letter calls it: a wake-up is the prompt the session
// set itself, not a letter anyone wrote.
const MAIL_WORD = { session: "letter", agent: "letter", hook: "letter", wake: "prompt" };

function sizeOf(text) {
    const n = text.length;
    return n >= 1000 ? `${(n / 1000).toFixed(1)}k chars` : `${n} chars`;
}

// A letter sent is drawn when it is sent, and whether it reached anyone comes
// later, from claude's answer: one that reached nobody says so on its head and
// gives claude's reason once opened.
function Letter({ item }) {
    const [open, setOpen] = useState(false);
    const cap = useRef(null);
    const [clamped, setClamped] = useState(false);
    const kind = MAIL_KINDS[item.source] || "agent";
    const out = item.dir === "out";
    const lost = out && item.undelivered;
    const text = dedent(item.text);
    const lead = leadOf(text);
    // A letter of one long paragraph has nothing after its lead and still
    // more than the lines the card shows closed.
    useLayoutEffect(() => {
        const el = cap.current;
        if (el && !open) setClamped(el.scrollHeight > el.clientHeight + 1);
    }, [text, open]);
    // A letter that went nowhere always opens: its reason is inside.
    const more = text.trim() !== lead.trim() || clamped || Boolean(lost);
    const who = kind === "hook" || kind === "wake" ? "" : (item.whoName || item.from || MAIL_WHO[kind]);
    const word = MAIL_WORD[kind];
    return html`
        <div class=${`sent mletter k-${kind}${out ? " out" : ""}${open ? " open" : ""}`}>
            <div class="senthead">
                <span class="sentico">${MAIL_ICONS[kind]()}</span>
                <span class="sentlabel">${MAIL_LABEL[kind][out ? 1 : 0]}</span>
                ${lost && html`<span class="mletterlost" title=${item.undelivered}>not delivered</span>`}
                ${item.at && html`<span class="sentat">${stampText(item.at)}</span>`}
            </div>
            ${who && html`<div class="mletterwho" title=${item.from || ""}>${who}</div>`}
            ${open && lost && html`<p class="hint warn mletterwhy">${item.undelivered}</p>`}
            <div class=${`sentcap mletterbody${open ? "" : " closed"}`} ref=${cap}>${render(open ? text : lead)}</div>
            ${open && item.cut && html`<p class="hint warn">The ${word} is longer than shown — cut.</p>`}
            ${more && html`
                <div class="mflist">
                    <button class="mfile mlettermore" type="button" onClick=${() => setOpen(!open)}
                            aria-expanded=${open ? "true" : "false"}>
                        <span class="mfico">${open ? Icon.close() : Icon.file()}</span>
                        <span class="mfname">${open ? `Fold the ${word}` : lost ? "Why it was not delivered" : `The whole ${word}`}</span>
                        <span class="mfsize">${sizeOf(text)}</span>
                    </button>
                </div>
            `}
        </div>
    `;
}

// The tone of a background task by how it ended, and its mark: a tick for one
// that did its work, a cross for one that did not.
const DONE_TONES = { completed: "ok", failed: "crit", killed: "faint", stopped: "faint" };
const doneMark = (status) => (status === "completed" ? "✓" : "✗");

// doneName is what a finished task was called: the name in the quotes of
// claude's sentence about it, or the sentence when it has none.
function doneName(summary) {
    const quoted = /"(.+)"/.exec(summary || "");
    return quoted ? quoted[1] : (summary || "a background task");
}

// isAgent tells an agent done from a command done: an agent reports what it
// spent, and claude names it so.
function isAgent(item) {
    return item.tokens > 0 || /^Agent\b/.test(item.summary || "");
}

// doneExit is the exit code a command ended with, when it is not a clean one.
function doneExit(summary) {
    const code = /exit code (\d+)/.exec(summary || "");
    return code && code[1] !== "0" ? `exit ${code[1]}` : "";
}

// The words a head names tasks by, one of a kind and several.
const TASK_NOUNS = {
    agent: ["agent", "agents"],
    command: ["command", "commands"],
    monitor: ["monitor", "monitors"],
    other: ["task", "tasks"],
};

// How a task ended, in the word of the head. A monitor watches until its
// source runs dry: it ends rather than finishes.
const TASK_ENDS = { completed: "finished", failed: "failed", killed: "stopped", stopped: "stopped" };

function endWord(kind, status) {
    if (kind === "monitor" && status === "completed") return "ended";
    return TASK_ENDS[status] || "ended";
}

const TASK_ICONS = { agent: Icon.robot, command: Icon.terminal, monitor: Icon.monitor, other: Icon.tools };

// taskKind is what ran in the background: claude names an agent, a command
// and a monitor in the first word of its sentence about them.
function taskKind(item) {
    const said = item.summary || "";
    if (isAgent(item)) return "agent";
    if (/^Monitor\b/.test(said)) return "monitor";
    if (/^Background command\b/.test(said)) return "command";
    return "other";
}

// kindsOf counts the tasks of a card by kind, in the order their rows stand.
function kindsOf(list) {
    const kinds = new Map();
    for (const item of list) {
        const kind = taskKind(item);
        kinds.set(kind, (kinds.get(kind) || 0) + 1);
    }
    return kinds;
}

// taskHead names what ended. Tasks of one kind are named by it, counted when
// there are several, with how they ended: the one ending they share, or
// "ended" when they ended apart. Tasks of several kinds are the kinds counted
// alone — the marks of the rows say how each ended, and a phone has no room
// on the head for more beside the time. A head that still does not fit wraps
// between the kinds, never between a number and its kind.
function taskHead(list, kinds) {
    if (kinds.size > 1) {
        return [...kinds].map(([kind, n]) => `${n}\u00a0${TASK_NOUNS[kind][n > 1 ? 1 : 0]}`).join(", ");
    }
    const [kind] = kinds.keys();
    const alike = list.every((t) => t.status === list[0].status);
    const verb = alike ? endWord(kind, list[0].status) : "ended";
    return list.length > 1 ? `${list.length} ${TASK_NOUNS[kind][1]} ${verb}` : `${TASK_NOUNS[kind][0]} ${verb}`;
}

// TaskCard is the background tasks that ended side by side: the head names
// what ended, a row per task marks how it ended and says what it was and
// what it took. A task that names itself opens what it left behind: an agent
// its conversation, a command its output.
function TaskCard({ list, onTask }) {
    const kinds = kindsOf(list);
    const [kind] = kinds.keys();
    const failed = list.some((t) => t.status === "failed");
    const icon = kinds.size > 1 ? Icon.list() : TASK_ICONS[kind]();
    const at = list[list.length - 1].at;
    return html`
        <div class=${`sent mtasks${failed ? " failed" : ""}`}>
            <div class="senthead">
                <span class="sentico">${icon}</span>
                <span class="sentlabel">${taskHead(list, kinds)}</span>
                ${at && html`<span class="sentat">${stampText(at)}</span>`}
            </div>
            <div class="mflist">
                ${list.map((item) => {
                    const aside = [
                        item.ms > 0 && stopwatch(item.ms / 1000),
                        item.tokens > 0 && `${shortTokens(item.tokens)} ${tokenWord(item.tokens)}`,
                        doneExit(item.summary),
                    ].filter(Boolean).join(" · ");
                    const name = doneName(item.summary);
                    const said = item.summary || "a background task ended";
                    const body = html`
                        <span class="mftag">${doneMark(item.status)}</span>
                        <span class="mfname">
                            ${name}
                            ${aside && html`<span class="mfnote">${aside}</span>`}
                        </span>
                    `;
                    const cls = `mfile tagged mtask s-${DONE_TONES[item.status] || "faint"}`;
                    if (item.task && onTask) {
                        return html`
                            <button class=${cls} type="button" key=${item.pos} aria-label=${`${said} — open`}
                                    onClick=${() => onTask({ id: item.task, name, agent: isAgent(item) })}>
                                ${body}<span class="crgo">${Icon.chevron()}</span>
                            </button>
                        `;
                    }
                    return html`<div class=${cls} key=${item.pos} role="note" aria-label=${said}>${body}</div>`;
                })}
            </div>
        </div>
    `;
}

// Line is a warning of claude or the recap after an absence: a line of its
// own, its dot saying how loud it is.
function Line({ item }) {
    return html`
        <div class=${`mside s-${item.level || "info"}`}>
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

// AskedCard is a round of questions the person answered, a card of the build
// of the files sent to them: the head says what it was and when, and each
// question is a row on the plate — its header as the tag, the question, and
// what was picked. A question left without an answer has its tag dimmed, so
// the round reads at a glance.
function AskedCard({ item }) {
    const rows = item.asked || [];
    const note = ROUND_NAMES[item.status];
    return html`
        <div class=${`sent asked${item.status ? " off" : ""}`}>
            <div class="senthead">
                <span class="sentico">${Icon.ask()}</span>
                <span class="sentlabel">${rows.length > 1 ? "questions" : "question"}</span>
                ${note && html`<span class="askedwhy">${note}</span>`}
                ${item.at && html`<span class="sentat">${stampText(item.at)}</span>`}
            </div>
            <div class="mflist">
                ${rows.map((row, n) => {
                    const answer = row.answer || [];
                    return html`
                        <div class=${`mfile askedrow${answer.length ? "" : " s-faint"}`} key=${n}>
                            ${row.header && html`<span class="mftag">${row.header}</span>`}
                            <span class="askedq">${row.text}</span>
                            ${answer.length
                                ? html`<span class="askeda">${answer.join(" · ")}</span>`
                                : !item.status && html`<span class="askeda skip">skipped</span>`}
                        </div>
                    `;
                })}
            </div>
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
// above it: the transcript has the calls and nothing of the question. It is
// drawn as a round of questions is, the tool as the tag of its row, and the
// tag takes the tone of the answer, as the tag of a task done takes the tone
// of its end.
function PermittedCard({ item }) {
    const rows = item.rows || [];
    return html`
        <div class="sent asked permitted">
            <div class="senthead">
                <span class="sentico">${Icon.hand()}</span>
                <span class="sentlabel">${rows.length > 1 ? "permissions" : "permission"}</span>
                ${item.at && html`<span class="sentat">${stampText(item.at)}</span>`}
            </div>
            <div class="mflist">
                ${rows.map((row, n) => {
                    const denied = row.decision === "deny";
                    return html`
                        <div class=${`mfile askedrow s-${denied ? "crit" : "ok"}`} key=${n}>
                            <span class="mftag">${row.tool}</span>
                            ${row.subject && html`<code class="askedsubj">${row.subject}</code>`}
                            <span class=${`askeda${denied ? " denied" : ""}`}>${permitAnswer(row)}</span>
                        </div>
                    `;
                })}
            </div>
        </div>
    `;
}

// PageCard is a page the session published, a card of the build of the files
// sent to the person: the head says a page went out, or that one already out
// was published again, and the page is the row on the plate that opens it —
// the same row the shelf of pages lists it by.
function PageCard({ item, copy, onOpen }) {
    return html`
        <div class="sent mpage">
            <div class="senthead">
                <span class="sentico">${Icon.artifact()}</span>
                <span class="sentlabel">${item.again ? "page updated" : "page"}</span>
                ${item.at && html`<span class="sentat">${stampText(item.at)}</span>`}
            </div>
            <div class="mflist">
                <${ArtifactCard} item=${item} copy=${copy} onOpen=${onOpen} />
            </div>
        </div>
    `;
}

// BriefDoc is a brief the session published: the head says so, and the
// document is the row on the plate that opens it, as the shelf of briefs has
// it.
function BriefDoc({ item, onOpen }) {
    return html`
        <div class="sent mbrief">
            <div class="senthead">
                <span class="sentico">${Icon.plan()}</span>
                <span class="sentlabel">brief</span>
                ${item.at && html`<span class="sentat">${stampText(item.at)}</span>`}
            </div>
            <div class="mflist">
                <${BriefCard} item=${item} onOpen=${onOpen} named=${false} />
            </div>
        </div>
    `;
}

// ArtifactCard renders a published artifact as the row that opens it: on the
// plate of its card in the feed and on the shelf of pages.
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
// the person rather than a message they read in passing. On the plate of its
// card in the feed the head already names it a brief, and the row drops the
// word (named false).
export function BriefCard({ item, onOpen, named = true }) {
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
                ${named && html`<span class="arnew">brief</span>`}
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
