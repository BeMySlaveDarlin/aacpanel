// A session question and the answer to it.
import { useEffect, useState } from "preact/hooks";

import { html } from "../html.js";
import { useAction } from "../actions/gate.js";
import { knows, whyNot } from "../exec.js";
import { Sheet } from "../ui/sheet.js";
import { Icon } from "../ui/icons.js";

const OWN_MAX = 4000;

// Ask renders the whole bottom of the screen while the session is asking.
// On the stream an answer is structure rather than keys, so the limits a
// terminal dialog puts on a layout do not hold there, and a note can go beside
// a pick.
export function Ask({ ask, name, exec, stream, onAnswered }) {
    const run = useAction();
    const [picks, setPicks] = useState([]);
    const [step, setStep] = useState(0);
    const [review, setReview] = useState(false);
    const [sending, setSending] = useState(false);
    const [open, setOpen] = useState(true);
    const [preview, setPreview] = useState(null);
    const [texts, setTexts] = useState([]);
    const [notes, setNotes] = useState([]);
    const [writing, setWriting] = useState(-1);
    const [fail, setFail] = useState("");

    const id = ask && ask.toolUseId;
    const questions = (ask && ask.questions) || [];

    useEffect(() => {
        setPicks(questions.map(() => []));
        setStep(0);
        setReview(false);
        setSending(false);
        setOpen(true);
        setPreview(null);
        setTexts(questions.map(() => ""));
        setNotes(questions.map(() => ""));
        setWriting(-1);
        setFail("");
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [id]);

    if (!questions.length) return null;

    const many = questions.length;
    const stepped = many > 1;
    const hasPreview = questions.some((q) => (q.options || []).some((o) => o.preview));
    const instant = many === 1 && !questions[0].multi && !hasPreview;
    const ready = Boolean(id) && knows(exec, "session.answer");
    const why = !id
        ? "the question came without an id — there is nothing to match it against on the host"
        : whyNot(exec, "session.answer");

    const dropReady = Boolean(id) && knows(exec, "session.dismiss");
    const dropWhy = !id
        ? "the question came without an id — there is nothing to match it against on the host"
        : whyNot(exec, "session.dismiss");

    const ownReady = ready && dropReady;

    const dropBlocked = !stream && (questions[0].options || []).some((o) => o.preview)
        ? "the options have previews, and in that layout the “discuss” item is drawn without a number"
        : "";

    const chosen = (n) => picks[n] || [];
    const own = (n) => texts[n] || "";
    const ownBlocked = (n) => {
        if (stream) return ownReady ? "" : dropWhy;
        if (questions[n].multi) {
            return "this one takes several choices, and a free answer in the console is a checkbox, not a field";
        }
        if ((questions[n].options || []).some((o) => o.preview)) {
            return "the options have previews, and in that layout there is no free answer at all";
        }
        if (!ownReady) return dropWhy;
        return "";
    };
    const answered = (n) => chosen(n).length > 0 || own(n).trim() !== "";

    const send = async (all, words) => {
        if (sending) return;
        setSending(true);
        setFail("");
        setOpen(false);
        const params = { ask: id, picks: all };
        if (words.some((text) => text !== "")) params.texts = words;
        const noted = all.map((list, n) => (stream && list.length ? (notes[n] || "").trim() : ""));
        if (noted.some((note) => note !== "")) params.notes = noted;
        const result = await run("session.answer", name, params);
        setSending(false);
        if (!result.ok) {
            setFail(result.error || "the answer did not go out");
            setOpen(true);
            return;
        }
        if (onAnswered) onAnswered(id);
    };

    const drop = async () => {
        if (!dropReady || dropBlocked || sending) return;
        setSending(true);
        setFail("");
        const result = await run("session.dismiss", name, { ask: id });
        setSending(false);
        if (!result.ok) {
            if (!result.cancelled) setFail(result.error || "the question was not dismissed");
            return;
        }
        setOpen(false);
        if (onAnswered) onAnswered(id);
    };

    const words = (n, text) => {
        const next = questions.map((_, i) => (i === n ? text : own(i)));
        setTexts(next);
    };

    const openOwn = (n) => {
        if (ownBlocked(n) || sending) return;
        if (writing === n) {
            words(n, "");
            setWriting(-1);
            return;
        }
        const next = questions.map((_, i) => (i === n ? [] : chosen(i)));
        setPicks(next);
        setWriting(n);
    };

    const pick = (n, k) => {
        if (!ready || sending) return;
        if (writing === n) {
            words(n, "");
            setWriting(-1);
        }
        if (instant) return send([[k + 1]], questions.map(() => ""));

        const next = questions.map((_, i) => [...chosen(i)]);
        const list = next[n];
        const at = list.indexOf(k + 1);
        if (at >= 0) list.splice(at, 1);
        else if (questions[n].multi) list.push(k + 1);
        else next[n] = [k + 1];
        setPicks(next);

        if (stepped && !questions[n].multi && next[n].length) {
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
        ${!open && html`
            <button
                class=${`waitbar${fail ? " bad" : sending ? " sent" : ""}`}
                type="button"
                onClick=${() => setOpen(true)}
            >
                <span class="askdot"></span>
                <span class="waittext">
                    ${sending ? "Sending the answer…" : fail ? `Did not go out: ${fail}` : "The session is waiting for an answer"}
                </span>
                <span class="waitgo">open</span>
            </button>
        `}

        <${Sheet} open=${open} onClose=${() => setOpen(false)} label="session question" inner>
            <div class="askhead">
                <span class="asklabel">
                    ${review ? "almost done" : (questions[shown[0]].header || `${name} asks`)}
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

                            <${OwnWords}
                                open=${writing === n}
                                text=${own(n)}
                                stepped=${stepped}
                                disabled=${Boolean(ownBlocked(n)) || sending}
                                why=${ownBlocked(n)}
                                onOpen=${() => openOwn(n)}
                                onText=${(text) => words(n, text)}
                            />
                        </div>
                        ${stream && !instant && chosen(n).length > 0 && html`
                            <textarea
                                class="askinput asknote"
                                rows="2"
                                maxLength=${OWN_MAX}
                                placeholder="a note beside your pick — optional, the session reads it with the answer"
                                value=${notes[n] || ""}
                                disabled=${sending}
                                onInput=${(e) => setNotes(questions.map((_, i) => (i === n ? e.target.value : notes[i] || "")))}
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
                        onClick=${stepped && !review
                            ? forward
                            : () => send(questions.map((_, i) => chosen(i)), questions.map((_, i) => own(i)))}
                    >
                        ${label({ sending, stepped, review, step, many, nothing, writing, answered })}
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

function label({ sending, stepped, review, step, many, nothing, writing, answered }) {
    if (sending) return "Sending…";
    if (writing >= 0 && !answered(writing)) return "Write the answer";
    if (!stepped || review) return nothing ? "Pick an option" : "Send";
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

function OwnWords({ open, text, stepped, disabled, why, onOpen, onText }) {
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
        ${open && html`
            <textarea
                class="askinput"
                rows="3"
                autofocus
                maxLength=${OWN_MAX}
                placeholder="what to answer the session"
                value=${text}
                disabled=${disabled}
                onInput=${(e) => onText(e.target.value)}
            ></textarea>
        `}
    `;
}

function Review({ questions, picks, texts, onEdit }) {
    return html`
        <p class="asktext">Check and send</p>
        <div class="askreview">
            ${questions.map((q, n) => {
                const labels = (picks[n] || []).map((pick) => (q.options[pick - 1] || {}).label).filter(Boolean);
                const said = (texts[n] || "").trim();
                return html`
                    <div class="askrow" key=${n}>
                        <span class="askrowq">${q.header || q.text}</span>
                        <span class=${`askrowa${labels.length || said ? "" : " skip"}`}>
                            ${said || (labels.length ? labels.join(", ") : "skipped")}
                        </span>
                        <button class="askedit" type="button" onClick=${() => onEdit(n)}>change</button>
                    </div>
                `;
            })}
        </div>
    `;
}
