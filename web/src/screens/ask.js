// A session question and the answer to it.
import { useEffect, useRef, useState } from "preact/hooks";

import { html } from "../html.js";
import { useAction } from "../actions/gate.js";
import { knows, whyNot } from "../exec.js";
import { Sheet } from "../ui/sheet.js";
import { Icon } from "../ui/icons.js";

const OWN_MAX = 4000;

// Ask renders the whole bottom of the screen while the session is asking.
// On the stream an answer is structure rather than keys, so the limits a
// terminal dialog puts on a layout do not hold there, and a note can go beside
// a pick. A codex thread is answered by its protocol wherever it lives, and
// what an MCP server asks through it is a form rather than a round of
// questions.
export function Ask(props) {
    const { ask, codex } = props;
    return codex && ask && (ask.server || ask.message)
        ? html`<${Form} ...${props} />`
        : html`<${Round} ...${props} />`;
}

// askLabel names who asks over a question: its header for claude, and for
// codex the header beside the word that the question is codex's — a question
// of codex comes from plan mode.
function askLabel(q, name, codex) {
    if (codex) return `${q.header || "Plan"} · codex asks`;
    return q.header || `${name} asks`;
}

// useAnswer keeps an answer as the inputs leave it. Two inputs can come before
// the card is drawn again — two options of a multiple choice, a pick and a tap
// on Send — and the second has to build on what the first left: built on the
// values of the last drawing, it puts back what was there before the first
// and drops it. The state draws the card; an input reads and changes latest,
// and the answer goes out of it.
function useAnswer(blank) {
    const [given, setGiven] = useState(blank);
    const latest = useRef(given);
    const change = (fn) => {
        latest.current = { ...latest.current, ...fn(latest.current) };
        setGiven(latest.current);
    };
    const reset = (fresh) => {
        latest.current = fresh;
        setGiven(fresh);
    };
    return { given, latest, change, reset };
}

// useSending says whether an answer or a dismissal is on its way. A second tap
// on Send can come before the card is drawn again, and the state of the last
// drawing still says nothing is going: what the tap asks is kept beside it, so
// one card sends one answer.
function useSending() {
    const [sending, setSending] = useState(false);
    const going = useRef(false);
    const begin = () => {
        if (going.current) return false;
        going.current = true;
        setSending(true);
        return true;
    };
    const end = () => {
        going.current = false;
        setSending(false);
    };
    return { sending, begin, end };
}

// blankAnswer is a round answered in nothing: no picks, no words, no notes and
// no field of one's own words open.
function blankAnswer(questions) {
    return {
        picks: questions.map(() => []),
        texts: questions.map(() => ""),
        notes: questions.map(() => ""),
        writing: -1,
    };
}

// Round is a round of questions: one at a time when there are several, an
// option picked or words of the person's own.
//
// A question of codex says itself whether it takes words beside its options
// (other), and one without options takes nothing else; words it asks to keep
// out of sight (secret) are typed into a field that hides them.
function Round({ ask, name, exec, stream, codex, onAnswered }) {
    const run = useAction();
    const [step, setStep] = useState(0);
    const [review, setReview] = useState(false);
    const { sending, begin, end } = useSending();
    const [open, setOpen] = useState(true);
    const [preview, setPreview] = useState(null);
    const [fail, setFail] = useState("");

    const id = ask && ask.toolUseId;
    const questions = (ask && ask.questions) || [];

    // The picks, the words, the notes and the question whose own words are
    // open, as the inputs leave them.
    const { given, latest, change, reset } = useAnswer(() => blankAnswer(questions));
    const { picks, texts, notes, writing } = given;

    useEffect(() => {
        reset(blankAnswer(questions));
        setStep(0);
        setReview(false);
        end();
        setOpen(true);
        setPreview(null);
        setFail("");
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [id]);

    if (!questions.length) return null;

    // The answer goes as structure: on the stream, and for codex always.
    const proto = Boolean(stream || codex);
    const many = questions.length;
    const stepped = many > 1;
    const free = (n) => !(questions[n].options || []).length;
    const hasPreview = questions.some((q) => (q.options || []).some((o) => o.preview));
    const instant = many === 1 && !questions[0].multi && !hasPreview && !free(0);
    const ready = Boolean(id) && knows(exec, "session.answer");
    const why = !id
        ? "the question came without an id — there is nothing to match it against on the host"
        : whyNot(exec, "session.answer");

    const dropReady = Boolean(id) && knows(exec, "session.dismiss");
    const dropWhy = !id
        ? "the question came without an id — there is nothing to match it against on the host"
        : whyNot(exec, "session.dismiss");

    const ownReady = ready && dropReady;

    const dropBlocked = !proto && (questions[0].options || []).some((o) => o.preview)
        ? "the options have previews, and in that layout the “discuss” item is drawn without a number"
        : "";

    const chosen = (n) => picks[n] || [];
    const own = (n) => texts[n] || "";
    // Words of codex's own question go with the answer and need nothing else
    // of the host.
    const offersOwn = (n) => !codex || Boolean(questions[n].other);
    const ownBlocked = (n) => {
        if (codex) return ready ? "" : why;
        if (proto) return ownReady ? "" : dropWhy;
        if (questions[n].multi) {
            return "this one takes several choices, and a free answer in tmux is a checkbox, not a field";
        }
        if ((questions[n].options || []).some((o) => o.preview)) {
            return "the options have previews, and in that layout there is no free answer at all";
        }
        if (!ownReady) return dropWhy;
        return "";
    };
    const answered = (n) => chosen(n).length > 0 || own(n).trim() !== "";

    const send = async (all, words) => {
        if (!begin()) return;
        setFail("");
        setOpen(false);
        const params = { ask: id, picks: all };
        if (words.some((text) => text !== "")) params.texts = words;
        const noted = all.map((list, n) => (proto && list.length ? (latest.current.notes[n] || "").trim() : ""));
        if (noted.some((note) => note !== "")) params.notes = noted;
        const result = await run("session.answer", name, params);
        end();
        if (!result.ok) {
            setFail(result.error || "the answer did not go out");
            setOpen(true);
            return;
        }
        if (onAnswered) onAnswered(id);
    };

    const drop = async () => {
        if (!dropReady || dropBlocked || !begin()) return;
        setFail("");
        const result = await run("session.dismiss", name, { ask: id });
        end();
        if (!result.ok) {
            if (!result.cancelled) setFail(result.error || "the question was not dismissed");
            return;
        }
        setOpen(false);
        if (onAnswered) onAnswered(id);
    };

    // one puts value in place of question n's in a list of the answer.
    const one = (list, n, value, blank) => questions.map((_, i) => (i === n ? value : list[i] || blank));
    const words = (n, text) => change((a) => ({ texts: one(a.texts, n, text, "") }));
    const setNote = (n, text) => change((a) => ({ notes: one(a.notes, n, text, "") }));
    // sendAll sends the answer as the inputs left it, whatever was drawn last.
    const sendAll = () => {
        const a = latest.current;
        send(questions.map((_, i) => a.picks[i] || []), questions.map((_, i) => a.texts[i] || ""));
    };

    const openOwn = (n) => {
        if (ownBlocked(n) || sending) return;
        if (latest.current.writing === n) {
            change((a) => ({ texts: one(a.texts, n, "", ""), writing: -1 }));
            return;
        }
        change((a) => ({ picks: one(a.picks, n, [], []), writing: n }));
    };

    const pick = (n, k) => {
        if (!ready || sending) return;
        if (latest.current.writing === n) {
            change((a) => ({ texts: one(a.texts, n, "", ""), writing: -1 }));
        }
        if (instant) return send([[k + 1]], questions.map(() => ""));

        const list = [...(latest.current.picks[n] || [])];
        const at = list.indexOf(k + 1);
        if (at >= 0) list.splice(at, 1);
        else if (questions[n].multi) list.push(k + 1);
        else list.splice(0, list.length, k + 1);
        change((a) => ({ picks: one(a.picks, n, list, []) }));

        if (stepped && !questions[n].multi && list.length) {
            setTimeout(() => {
                if (n < many - 1) setStep(n + 1);
                else setReview(true);
            }, 180);
        }
    };

    const forward = () => {
        if (step < many - 1) setStep(step + 1);
        else setReview(true);
    };
    const backward = () => {
        if (review) setReview(false);
        else if (step > 0) setStep(step - 1);
    };

    const nothing = !questions.some((_, n) => answered(n));
    const shown = stepped && !review ? [step] : questions.map((_, n) => n);

    return html`
        ${!open && html`<${WaitBar} sending=${sending} fail=${fail} onOpen=${() => setOpen(true)} />`}

        <${Sheet} open=${open} onClose=${() => setOpen(false)} label="session question" inner>
            <div class="askhead">
                <span class="asklabel">
                    ${review ? "almost done" : askLabel(questions[shown[0]], name, codex)}
                </span>
                ${!ready && html`<span class="askwhy">${why}</span>`}
            </div>

            ${stepped && html`
                <div class="asksteps">
                    ${questions.map((_, n) => html`
                        <i key=${n} class=${!review && n === step ? "now" : chosen(n).length ? "done" : ""}></i>
                    `)}
                </div>
            `}

            ${review
                ? html`<${Review}
                    questions=${questions}
                    picks=${picks}
                    texts=${texts}
                    onEdit=${(n) => { setReview(false); setStep(n); }}
                />`
                : shown.map((n) => html`
                    <div class="askone" key=${n}>
                        ${!stepped && many > 1 && html`
                            <span class="asklabel">${questions[n].header || "question"}</span>
                        `}
                        <p class="asktext">${questions[n].text}</p>
                        ${questions[n].multi && html`<p class="askhint">several can be picked</p>`}
                        ${free(n) ? html`
                            <${Words}
                                text=${own(n)}
                                secret=${Boolean(questions[n].secret)}
                                disabled=${Boolean(ownBlocked(n)) || sending}
                                why=${ownBlocked(n)}
                                onText=${(text) => words(n, text)}
                            />
                        ` : html`
                        <div class="askopts">
                            ${(questions[n].options || []).map((opt, k) => html`
                                <${Option}
                                    key=${k}
                                    opt=${opt}
                                    on=${chosen(n).includes(k + 1)}
                                    disabled=${!ready || sending}
                                    onPick=${() => pick(n, k)}
                                    onPreview=${() => setPreview([n, k])}
                                />
                            `)}

                            ${offersOwn(n) && html`
                                <${OwnWords}
                                    open=${writing === n}
                                    text=${own(n)}
                                    stepped=${stepped}
                                    secret=${Boolean(questions[n].secret)}
                                    disabled=${Boolean(ownBlocked(n)) || sending}
                                    why=${ownBlocked(n)}
                                    onOpen=${() => openOwn(n)}
                                    onText=${(text) => words(n, text)}
                                />
                            `}
                        </div>
                        `}
                        ${proto && !instant && chosen(n).length > 0 && html`
                            <textarea
                                class="askinput asknote"
                                rows="2"
                                maxLength=${OWN_MAX}
                                placeholder="a note beside your pick — optional, the session reads it with the answer"
                                value=${notes[n] || ""}
                                disabled=${sending}
                                onInput=${(e) => setNote(n, e.target.value)}
                            ></textarea>
                        `}
                    </div>
                `)}

            <button
                class="askdrop"
                type="button"
                disabled=${!dropReady || Boolean(dropBlocked) || sending}
                onClick=${drop}
            >
                <span class="askdropname">Dismiss the question and discuss</span>
                <span class="askdropwhy">
                    ${sending
                        ? "dismissing the question…"
                        : dropBlocked || (dropReady ? "the session will wait for an ordinary message" : dropWhy)}
                </span>
            </button>

            ${fail && html`
                <p class="askfail">${fail}</p>
            `}

            ${(!instant || writing >= 0) && html`
                <div class="askfoot">
                    ${stepped && (step > 0 || review) && html`
                        <button class="btn" type="button" onClick=${backward}>Back</button>
                    `}
                    <button
                        class="btn primary"
                        type="button"
                        disabled=${!ready || sending || (!stepped ? nothing : false) || (review && nothing)}
                        onClick=${stepped && !review ? forward : sendAll}
                    >
                        ${label({ sending, stepped, review, step, many, nothing, writing, answered,
                                  wordsOnly: questions.every((_, i) => free(i)) })}
                    </button>
                </div>
            `}
        <//>

        ${preview && html`
            <div class="askover">
                <${Sheet} open onClose=${() => setPreview(null)} label="what it looks like" inner>
                    <div class="askhead">
                        <span class="asklabel">${questions[preview[0]].options[preview[1]].label}</span>
                    </div>
                    <${Mockup} text=${questions[preview[0]].options[preview[1]].preview} />
                <//>
            </div>
        `}
    `;
}

// WaitBar stands where the card was while it is put down: what became of the
// answer, and the way back to the card.
function WaitBar({ sending, fail, onOpen }) {
    return html`
        <button class=${`waitbar${fail ? " bad" : sending ? " sent" : ""}`} type="button" onClick=${onOpen}>
            <span class="askdot"></span>
            <span class="waittext">
                ${sending ? "Sending the answer…" : fail ? `Did not go out: ${fail}` : "The session is waiting for an answer"}
            </span>
            <span class="waitgo">open</span>
        </button>
    `;
}

function label({ sending, stepped, review, step, many, nothing, writing, answered, wordsOnly }) {
    if (sending) return "Sending…";
    if (writing >= 0 && !answered(writing)) return "Write the answer";
    if (!stepped || review) return nothing ? (wordsOnly ? "Write the answer" : "Pick an option") : "Send";
    if (!answered(step)) return "Skip";
    return step === many - 1 ? "To the review" : "Next";
}

// columns is the width of a drawing: the longest of its lines, in characters.
function columns(text) {
    let most = 0;
    for (const line of String(text || "").split("\n")) {
        most = Math.max(most, [...line].length);
    }
    return most || 1;
}

// Mockup shows the drawing an option carries. A drawing is made of characters
// standing in columns and is read whole: a column past the right edge takes the
// shape with it, and the box it is dropped into is the width of a phone. So the
// box is told how many columns it has to hold and fits its type to them; only a
// drawing too wide to stay readable at all is left to be scrolled.
function Mockup({ text }) {
    return html`
        <div class="askshow" style=${`--cols:${columns(text)}`}>
            <pre class="askpreview">${text}</pre>
        </div>
    `;
}

function Option({ opt, on, disabled, onPick, onPreview }) {
    return html`
        <div class=${`askopt${on ? " on" : ""}${disabled ? " off" : ""}`}>
            <button class="askpick" type="button" disabled=${disabled} onClick=${onPick}>
                <span class="asktick"></span>
                <span class="askbody">
                    <span class="askname">${opt.label}</span>
                    ${opt.description && html`<span class="askdesc">${opt.description}</span>`}
                </span>
            </button>
            ${opt.preview && html`
                <button class="askinfo" type="button" aria-label="show the mockup" onClick=${onPreview}>
                    ${Icon.info()}
                </button>
            `}
        </div>
    `;
}

function OwnWords({ open, text, stepped, secret, disabled, why, onOpen, onText }) {
    return html`
        <div class=${`askopt askown${open ? " on" : ""}${disabled ? " off" : ""}`}>
            <button class="askpick" type="button" disabled=${disabled} onClick=${onOpen}>
                <span class="asktick"></span>
                <span class="askbody">
                    <span class="askname">Answer in your own words</span>
                    <span class="askdesc">
                        ${why || (stepped ? "an answer to this question, not to the whole round" : "your own text instead of an option")}
                    </span>
                </span>
            </button>
        </div>
        ${open && html`<${Field} text=${text} secret=${secret} disabled=${disabled} onText=${onText} focus />`}
    `;
}

// Words are the answer to a question that has no options to pick: the field
// stands open, and what stops it is said under it.
function Words({ text, secret, disabled, why, onText }) {
    return html`
        <div class="askopts">
            <${Field} text=${text} secret=${secret} disabled=${disabled} onText=${onText} />
            ${why && html`<p class="askhint">${why}</p>`}
        </div>
    `;
}

// Field is where the person writes an answer. Words the question asks to keep
// out of sight go into a field that shows dots, a line rather than a box: a
// password is not written in paragraphs.
function Field({ text, secret, disabled, onText, focus }) {
    if (secret) {
        return html`
            <input
                class="askinput"
                type="password"
                autocomplete="off"
                autofocus=${focus}
                maxLength=${OWN_MAX}
                placeholder="what to answer the session — it is not shown"
                value=${text}
                disabled=${disabled}
                onInput=${(e) => onText(e.target.value)}
            />
        `;
    }
    return html`
        <textarea
            class="askinput"
            rows="3"
            autofocus=${focus}
            maxLength=${OWN_MAX}
            placeholder="what to answer the session"
            value=${text}
            disabled=${disabled}
            onInput=${(e) => onText(e.target.value)}
        ></textarea>
    `;
}

function Review({ questions, picks, texts, onEdit }) {
    return html`
        <p class="asktext">Check and send</p>
        <div class="askreview">
            ${questions.map((q, n) => {
                const labels = (picks[n] || []).map((pick) => (q.options[pick - 1] || {}).label).filter(Boolean);
                const said = (texts[n] || "").trim();
                const shown = said && q.secret ? "•••••• (hidden)" : said;
                return html`
                    <div class="askrow" key=${n}>
                        <span class="askrowq">${q.header || q.text}</span>
                        <span class=${`askrowa${labels.length || said ? "" : " skip"}`}>
                            ${shown || (labels.length ? labels.join(", ") : "skipped")}
                        </span>
                        <button class="askedit" type="button" onClick=${() => onEdit(n)}>change</button>
                    </div>
                `;
            })}
        </div>
    `;
}

// The kinds of field a form is drawn with. The host words every field as a
// question: a field with nothing to pick takes words, a list takes several
// picks, and a yes or a no is a switch. A pick of a few is a row of segments,
// and of more a list that opens.
const SEGMENTS_MAX = 4;

function fieldKind(q) {
    const options = q.options || [];
    if (!options.length) return "words";
    if (q.multi) return "many";
    if (options.length === 2 && options[0].label === "Yes" && options[1].label === "No") return "switch";
    return options.length <= SEGMENTS_MAX ? "segments" : "list";
}

// startPicks are what a form shows before a hand touches it: a switch stands
// off, which is an answer of its own — no — and every other field is empty.
function startPicks(fields) {
    return fields.map((q) => (fieldKind(q) === "switch" ? [2] : []));
}

// Form is what an MCP server asks through codex: its name and its words over
// the fields of its form. The answer goes to the server, not into the
// conversation, so there is no note beside a field and no "discuss": the form
// is sent or declined. A field the server requires holds the form back until
// it is given.
function Form({ ask, name, exec, onAnswered }) {
    const run = useAction();
    const id = ask.toolUseId;
    const fields = ask.questions || [];
    const blank = () => ({ picks: startPicks(fields), texts: fields.map(() => "") });
    const { given, latest, change, reset } = useAnswer(blank);
    const { picks, texts } = given;
    const { sending, begin, end } = useSending();
    const [open, setOpen] = useState(true);
    const [fail, setFail] = useState("");

    useEffect(() => {
        reset(blank());
        end();
        setOpen(true);
        setFail("");
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [id]);

    const ready = Boolean(id) && knows(exec, "session.answer");
    const why = !id
        ? "the form came without an id — there is nothing to match it against on the host"
        : whyNot(exec, "session.answer");
    const declineReady = Boolean(id) && knows(exec, "session.dismiss");

    const filled = (a, n) => (a.picks[n] || []).length > 0 || (a.texts[n] || "").trim() !== "";
    const missingIn = (a) => fields.filter((q, n) => q.required && !filled(a, n)).map((q) => q.text);
    const missing = missingIn(given);

    // A pick is a change of the field's picks as they stand, not a list made of
    // the last drawing: two taps on one field before it is drawn again keep
    // both.
    const setPick = (n, pick) => change((a) => ({
        picks: fields.map((_, i) => (i === n ? pick(a.picks[i] || []) : a.picks[i] || [])),
    }));
    const setText = (n, text) => change((a) => ({ texts: fields.map((_, i) => (i === n ? text : a.texts[i] || "")) }));

    const send = async () => {
        const a = latest.current;
        if (missingIn(a).length || !begin()) return;
        setFail("");
        setOpen(false);
        const params = { ask: id, picks: fields.map((_, n) => a.picks[n] || []) };
        if (a.texts.some((text) => text.trim() !== "")) params.texts = fields.map((_, n) => (a.texts[n] || "").trim());
        const result = await run("session.answer", name, params);
        end();
        if (!result.ok) {
            setFail(result.error || "the form did not go out");
            setOpen(true);
            return;
        }
        if (onAnswered) onAnswered(id);
    };

    const decline = async () => {
        if (!declineReady || !begin()) return;
        setFail("");
        const result = await run("session.dismiss", name, { ask: id });
        end();
        if (!result.ok) {
            if (!result.cancelled) setFail(result.error || "the form was not declined");
            return;
        }
        setOpen(false);
        if (onAnswered) onAnswered(id);
    };

    const off = !ready || sending;
    return html`
        ${!open && html`<${WaitBar} sending=${sending} fail=${fail} onOpen=${() => setOpen(true)} />`}

        <${Sheet} open=${open} onClose=${() => setOpen(false)} label="a form from an MCP server" inner>
            <div class="askhead">
                <span class="asklabel">${ask.server ? `${ask.server} · MCP server asks` : "An MCP server asks"}</span>
                ${!ready && html`<span class="askwhy">${why}</span>`}
            </div>
            <p class="asktext">${ask.message || "Fill in the form"}</p>

            ${fields.map((q, n) => html`
                <${FormField}
                    key=${n}
                    q=${q}
                    picks=${picks[n] || []}
                    text=${texts[n] || ""}
                    disabled=${off}
                    onPick=${(pick) => setPick(n, pick)}
                    onText=${(text) => setText(n, text)}
                />
            `)}

            <p class="askformnote">
                ${missing.length
                    ? `Still to fill in: ${missing.join(", ")}.`
                    : "The answer goes to the server, not into the conversation."}
            </p>

            ${fail && html`<p class="askfail">${fail}</p>`}

            <div class="askfoot">
                <button class="btn" type="button" disabled=${!declineReady || sending}
                        title=${declineReady ? "the server hears that the form was declined" : whyNot(exec, "session.dismiss")}
                        onClick=${decline}>Decline</button>
                <button class="btn primary" type="button" disabled=${off || missing.length > 0} onClick=${send}>
                    ${sending ? "Sending…" : "Send"}
                </button>
            </div>
        <//>
    `;
}

// FormField is one field of a form: its name over it, marked when the server
// requires it, and the control of its kind. A pick goes to onPick as a change
// of the field's picks, to be made over what the field holds when it lands.
function FormField({ q, picks, text, disabled, onPick, onText }) {
    const kind = fieldKind(q);
    const options = q.options || [];
    if (kind === "switch") {
        const on = picks[0] === 1;
        return html`
            <button class="nfrow askswitch" type="button" role="switch" aria-checked=${on ? "true" : "false"}
                    disabled=${disabled} onClick=${() => onPick((was) => [was[0] === 1 ? 2 : 1])}>
                <span class="nfbody"><span class="nftitle">${q.text}</span></span>
                <span class="nfsw" aria-hidden="true"></span>
            </button>
        `;
    }
    const head = html`
        <span class="askfieldname">
            ${q.text}${q.required && html`<span class="askreq">required</span>`}
        </span>
    `;
    if (kind === "words") {
        return html`
            <label class="askfield">
                ${head}
                <input class="askinput" type=${q.secret ? "password" : "text"} autocomplete="off"
                       maxLength=${OWN_MAX} value=${text} disabled=${disabled}
                       onInput=${(e) => onText(e.target.value)} />
            </label>
        `;
    }
    if (kind === "list") {
        return html`
            <label class="askfield">
                ${head}
                <select class="askinput askselect" disabled=${disabled} value=${String(picks[0] || 0)}
                        onChange=${(e) => { const v = e.target.value; onPick(() => (v === "0" ? [] : [Number(v)])); }}>
                    <option value="0">${q.required ? "Pick one" : "Nothing picked"}</option>
                    ${options.map((o, k) => html`<option key=${k} value=${String(k + 1)}>${o.label}</option>`)}
                </select>
            </label>
        `;
    }
    if (kind === "segments") {
        return html`
            <div class="askfield">
                ${head}
                <div class="pkscope askseg" role="radiogroup">
                    ${options.map((o, k) => html`
                        <button key=${k} type="button" role="radio" aria-checked=${picks[0] === k + 1 ? "true" : "false"}
                                class=${`pkseg${picks[0] === k + 1 ? " on" : ""}`} disabled=${disabled}
                                onClick=${() => onPick((was) => (was[0] === k + 1 && !q.required ? [] : [k + 1]))}>${o.label}</button>
                    `)}
                </div>
            </div>
        `;
    }
    return html`
        <div class="askfield">
            ${head}
            <div class="askopts">
                ${options.map((o, k) => html`
                    <${Option}
                        key=${k}
                        opt=${o}
                        on=${picks.includes(k + 1)}
                        disabled=${disabled}
                        onPick=${() => onPick((was) => (was.includes(k + 1) ? was.filter((p) => p !== k + 1) : [...was, k + 1]))}
                        onPreview=${() => {}}
                    />
                `)}
            </div>
        </div>
    `;
}
