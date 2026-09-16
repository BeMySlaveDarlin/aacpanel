// A brief: a long piece the session published, read and answered here.
import { useCallback, useEffect, useMemo, useRef, useState } from "preact/hooks";

import { html } from "../html.js";
import { inline } from "../md.js";
import { useAction } from "../actions/gate.js";
import { knows, whyNot } from "../exec.js";
import { Sheet } from "../ui/sheet.js";
import { markSent, one, saveDraft } from "../data/briefs.js";

// How long after the last keystroke the draft goes to the panel. Short enough
// that a phone put down mid-sentence keeps the sentence, long enough not to
// send a request per letter.
const SAVE_MS = 900;

function paras(text) {
    return String(text || "").split(/\n{2,}/).map((p) => p.trim()).filter(Boolean);
}

function answered(a) {
    return Boolean(a && (a.skip || (a.picks && a.picks.length) || (a.note || "").trim()));
}

// routes finds where the answers can go. The answer travels to a name, and a
// brief outlives the conversation that wrote it: by the time it is answered the
// author is usually gone. A session sitting in the same directory is the one
// carrying that work on, so it stands in; a session elsewhere never does, the
// answers belong to their project.
function routes(snapshot, doc) {
    const list = (snapshot && snapshot.sessions) || [];
    const author = list.find((s) => s && s.sessionId === (doc && doc.sessionId));
    const cwd = (doc && doc.cwd) || "";
    const near = cwd ? list.filter((s) => s && s.cwd === cwd && s !== author) : [];
    return { author: author ? author.session : "", near: near.map((s) => s.session) };
}

function Facts({ items }) {
    if (!items || !items.length) return null;
    return html`
        <div class="bblk">
            <div class="bblk-h">what this rests on</div>
            <ul class="bfacts">
                ${items.map((f, i) => html`
                    <li key=${i}>
                        <span class=${f.flag ? "bflag" : ""}>${inline(f.text)}</span>
                        ${f.src && html`<span class="bsrc">${f.src}</span>`}
                    </li>
                `)}
            </ul>
        </div>
    `;
}

function Options({ q, picks, onPick }) {
    if (!q.options || !q.options.length) return null;
    return html`
        <div class="bblk">
            <div class="bblk-h">${q.kind === "multi" ? "options · several may be picked" : "options"}</div>
            <ol class="bopts">
                ${q.options.map((opt) => html`
                    <li key=${opt.key}>
                        <button
                            type="button"
                            class="bopt"
                            aria-pressed=${picks.includes(opt.key) ? "true" : "false"}
                            onClick=${() => onPick(opt.key)}
                        >
                            <span class="bopt-k">${opt.key}</span>
                            <span>
                                <span class="bopt-l">${inline(opt.label)}</span>
                                ${opt.note && html`<span class="bopt-c">${inline(opt.note)}</span>`}
                            </span>
                        </button>
                    </li>
                `)}
            </ol>
        </div>
    `;
}

function Question({ q, answer, onAnswer }) {
    const picks = (answer && answer.picks) || [];
    const note = (answer && answer.note) || "";
    const skip = Boolean(answer && answer.skip);

    const pick = (key) => {
        if (q.kind === "multi") {
            const next = picks.includes(key) ? picks.filter((k) => k !== key) : [...picks, key];
            onAnswer({ ...answer, picks: next, skip: false });
            return;
        }
        onAnswer({ ...answer, picks: picks[0] === key ? [] : [key], skip: false });
    };

    const said = skip ? "skipped" : picks.length ? picks.join(", ") : "";

    return html`
        <article class=${`bq${answered(answer) ? " is-done" : ""}`} id=${`bq-${q.id}`}>
            <div class="bq-n">${q.n}</div>
            ${q.chips && q.chips.length ? html`
                <div class="bchips">
                    ${q.chips.map((c, i) => html`<span key=${i} class=${`bchip ${c.tone || "plain"}`}>${c.text}</span>`)}
                </div>
            ` : null}
            <h3>${inline(q.title)}</h3>
            ${q.ask && paras(q.ask).map((p, i) => html`<p key=${i} class="bask">${inline(p)}</p>`)}

            <${Facts} items=${q.facts} />
            <${Options} q=${q} picks=${picks} onPick=${pick} />

            ${q.read && q.read.length ? html`
                <div class="bread">
                    <div class="bread-h">the session's reading — a proposal, not a fact</div>
                    ${q.read.map((p, i) => html`<p key=${i}>${inline(p)}</p>`)}
                </div>
            ` : null}

            ${q.kind === "none" ? null : html`
                <div class="bcap">
                    <div class="bcap-h">
                        <span class="bcap-l">what you decided</span>
                        <span class="bcap-s">${said}</span>
                    </div>
                    <textarea
                        value=${note}
                        placeholder=${(q.capture && q.capture.note && q.capture.note.placeholder) || "A note, if there is more to say"}
                        onInput=${(e) => onAnswer({ ...answer, note: e.target.value })}
                    ></textarea>
                    <div class="bcap-act">
                        <button type="button" class="blnk" onClick=${() => onAnswer({})}>clear</button>
                        <button
                            type="button"
                            class="blnk"
                            onClick=${() => onAnswer(skip ? { ...answer, skip: false } : { note, skip: true, picks: [] })}
                        >${skip ? "unskip" : "skip"}</button>
                    </div>
                </div>
            `}
        </article>
    `;
}

export function Brief({ id, snapshot, exec, onBack, onSession }) {
    const run = useAction();
    const [doc, setDoc] = useState(null);
    const [answers, setAnswers] = useState({});
    const [reply, setReply] = useState("");
    const [sentAt, setSentAt] = useState(null);
    const [error, setError] = useState("");
    const [peek, setPeek] = useState(false);
    const [sending, setSending] = useState(false);
    const [picked, setPicked] = useState("");
    const timer = useRef(null);

    useEffect(() => {
        let gone = false;
        setDoc(null);
        setError("");
        one(id)
            .then((body) => {
                if (gone) return;
                setDoc(body.brief);
                setAnswers((body.draft && body.draft.answers) || {});
                setReply(body.reply || "");
                setSentAt((body.draft && body.draft.sentAt) || null);
            })
            .catch((e) => { if (!gone) setError(String(e.message || e)); });
        return () => { gone = true; if (timer.current) clearTimeout(timer.current); };
    }, [id]);

    // The draft is saved by the panel, not kept in the tab: a brief is answered
    // over hours and from more than one device.
    const save = useCallback((next) => {
        if (timer.current) clearTimeout(timer.current);
        timer.current = setTimeout(() => {
            saveDraft(id, next)
                .then((body) => { if (body && typeof body.reply === "string") setReply(body.reply); })
                .catch(() => { /* the next keystroke tries again */ });
        }, SAVE_MS);
    }, [id]);

    const answer = useCallback((qid, value) => {
        setAnswers((prev) => {
            const next = { ...prev };
            if (value && (value.skip || (value.picks && value.picks.length) || (value.note || "").trim())) {
                next[qid] = value;
            } else {
                delete next[qid];
            }
            save(next);
            return next;
        });
    }, [save]);

    const questions = (doc && doc.questions) || [];
    const asking = questions.filter((q) => q.kind !== "none");
    const done = asking.filter((q) => answered(answers[q.id])).length;

    const route = useMemo(() => routes(snapshot, doc), [snapshot, doc]);
    const name = route.author || (route.near.includes(picked) ? picked : route.near[0] || "");
    const standIn = Boolean(!route.author && name);
    const canSend = Boolean(name) && knows(exec, "session.send");
    const why = !name
        ? "the session that wrote this brief is not running, and nothing else is working in its directory: the answers wait here until one is"
        : whyNot(exec, "session.send");

    const send = async () => {
        if (!canSend || sending || !done) return;
        setSending(true);
        const result = await run("session.send", name, { text: reply });
        setSending(false);
        if (!result.ok) return;
        markSent(id).catch(() => { /* the mark is a label on the screen, not the send */ });
        setSentAt(new Date().toISOString());
        // The answers are a message to a session, and a message is the start of
        // a conversation: the screen follows them in rather than leaving the
        // person on a document that has nothing left to do.
        if (onSession) onSession(name);
    };

    if (error) {
        return html`
            <div class="brief">
                <div class="brief-page">
                    <p class="bask">${error}</p>
                    <button type="button" class="bbtn" onClick=${onBack}>back</button>
                </div>
            </div>
        `;
    }
    if (!doc) return html`<div class="brief"><div class="brief-page"><p class="bask">opening…</p></div></div>`;

    return html`
        <div class="brief">
            <div class="brief-page">
                <header class="bmast">
                    ${doc.eyebrow && html`<div class="beyebrow">${doc.eyebrow}</div>`}
                    <h1>${doc.title}</h1>
                    ${doc.lede && paras(doc.lede).map((p, i) => html`<p key=${i} class="blede">${inline(p)}</p>`)}
                    ${doc.lineage && doc.lineage.length ? html`
                        <div class="blin">
                            ${doc.lineage.map((row, i) => html`
                                <span key=${`f${i}`} class="bfrom">${row.from}</span>
                                <span key=${`t${i}`}><span class="barrow">→</span> ${inline(row.to)}</span>
                            `)}
                        </div>
                    ` : null}
                </header>

                ${doc.summary && doc.summary.length ? html`
                    <section class="bsum">
                        ${doc.summary.map((c, i) => html`
                            <div key=${i}><span class="bsum-n">${c.n}</span><span class="bsum-l">${c.label}</span></div>
                        `)}
                    </section>
                ` : null}

                ${(doc.sections || []).map((s, i) => html`
                    <section key=${i} class="bsection">
                        ${s.title && html`<div class="bblk-h">${s.title}</div>`}
                        ${(s.body || []).map((p, n) => html`<p key=${n}>${inline(p)}</p>`)}
                    </section>
                `)}

                <main>
                    ${questions.map((q) => html`
                        <${Question}
                            key=${q.id}
                            q=${q}
                            answer=${answers[q.id]}
                            onAnswer=${(value) => answer(q.id, value)}
                        />
                    `)}
                </main>

                ${doc.closing && doc.closing.length ? html`
                    <div class="bclose">
                        ${doc.closing.map((p, i) => html`<p key=${i}>${inline(p)}</p>`)}
                    </div>
                ` : null}

                ${asking.length ? html`
                    <div class="bdock">
                        <div class="bdock-say">
                            ${sentAt
                                ? html`the answers have gone into <b>${name || "the session"}</b>`
                                : !done
                                ? "nothing answered yet · the draft saves itself"
                                : done < asking.length
                                ? html`answered <b>${done}</b> of <b>${asking.length}</b> · the rest may stay empty`
                                : html`all <b>${asking.length}</b> answered`}
                            ${!canSend && why ? html`<br />${why}` : null}
                            ${standIn ? html`
                                <br />the session that wrote this is gone · the answers go into
                                ${route.near.length > 1 ? html`
                                    <select
                                        class="bwho"
                                        value=${name}
                                        onChange=${(e) => setPicked(e.target.value)}
                                    >
                                        ${route.near.map((s) => html`<option key=${s} value=${s}>${s}</option>`)}
                                    </select>
                                ` : html` <b>${name}</b>`}, working in the same directory
                            ` : null}
                        </div>
                        <button type="button" class="bbtn" onClick=${() => setPeek(true)}>What goes</button>
                        <button
                            type="button"
                            class="bbtn go"
                            disabled=${!canSend || !done || sending}
                            onClick=${send}
                        >${sending ? "Sending…" : sentAt ? "Send again" : "Send answers"}</button>
                    </div>
                ` : null}
            </div>

            <${Sheet} open=${peek} onClose=${() => setPeek(false)} label="what goes into the session">
                <div class="shead">
                    <div><div class="stitle">What goes into the session</div>
                    <div class="ssub">${reply.length} characters</div></div>
                </div>
                <pre class="breply">${reply}</pre>
            </${Sheet}>
        </div>
    `;
}
