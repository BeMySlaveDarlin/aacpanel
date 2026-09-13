// Feed entries: what a row of the conversation looks like.

import { useEffect, useRef, useState } from "preact/hooks";

import { html } from "../../html.js";
import { Icon } from "../../ui/icons.js";
import { render } from "../../md.js";
import { plural } from "../../format.js";
import { idParam } from "./api.js";
import { FileAtts, SentCard } from "./files.js";
import { Photo, shotName } from "./photo.js";
import { callWord, KIND_NAMES, kindIcon, shortTokens, stampText, tokenWord } from "./labels.js";

// Row renders one row of the feed.
export function Row({ item, session, id, onCalls, onFile }) {
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
                        <button class=${`mtools ${group.kind}`} type="button" key=${group.kind} onClick=${onCalls}
                                title=${label}
                                aria-label=${`${label}: ${count} ${callWord(count)}`}>
                            <span class="mticon">${kindIcon(group.kind)}</span>
                            <span class="mtnum">${count}</span>
                        </button>
                    `;
                })}
            </div>
        `;
    }
    if (item.role === "mail") {
        return html`<${Mail} item=${item} />`;
    }

    if (item.role === "wake") {
        return html`<${Wake} item=${item} />`;
    }

    if (item.role === "artifact") {
        return html`<${ArtifactCard} item=${item} />`;
    }

    if (item.role === "asked") {
        return html`<${AskedCard} item=${item} />`;
    }

    if (item.role === "sent") {
        return html`<${SentCard} item=${item} onOpen=${onFile} />`;
    }

    const queued = item.state === "queued";
    const sending = item.state === "sending";
    const failed = item.state === "failed";
    const held = item.state === "held";
    const mine = item.role === "me";
    return html`
        <div class=${`msg ${mine ? "me" : "ai"}${queued || sending || held ? " queued" : ""}${failed ? " failed" : ""}`}>
            ${render(item.text, { breaks: mine })}
            ${!mine && html`<${FileAtts} files=${item.files} onOpen=${onFile} />`}
            ${item.cut && html`<p class="hint warn">The message is longer than shown — cut.</p>`}
            ${sending && html`<p class="mwait"><span class="mclock">${Icon.clock()}</span> going out</p>`}
            ${queued && html`<p class="mwait">queued</p>`}
            ${held && html`<p class="mwait">will go out when the session is free</p>`}
            ${failed && html`<p class="mwait crit">did not go out: ${item.error}</p>`}
        </div>
        ${mine && item.at && html`<div class="mstamp">${stampText(item.at)}</div>`}
    `;
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

function Mail({ item }) {
    const [open, setOpen] = useState(false);
    const peer = item.source === "session";
    const out = item.dir === "out";
    const who = item.from || (peer ? "neighbour session" : "subagent");
    return html`
        <div class=${`mmail${open ? " open" : ""}${out ? " out" : ""}`}>
            <button class="mmhead" type="button" onClick=${() => setOpen(!open)}
                    aria-expanded=${open ? "true" : "false"}>
                <span class="mmico">${Icon.envelope()}</span>
                <span class="mmdir">${out ? "to:" : "from:"}</span>
                <span class="mmfrom">${who}</span>
                ${!out && html`<span class="mmkind">${peer ? "session" : "agent"}</span>`}
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

// ArtifactCard renders a published artifact as a card.
export function ArtifactCard({ item }) {
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
        ${item.url && html`<span class="crgo">${Icon.chevron()}</span>`}
    `;
    if (!item.url) return html`<div class="artifact dead">${body}</div>`;
    return html`
        <a class="artifact" href=${item.url} target="_blank" rel="noopener noreferrer">${body}</a>
    `;
}

function peek(text) {
    const line = (text || "").split("\n").map((s) => s.replace(/^[#>*\-\s]+/, "").trim())
        .find((s) => s.length > 0) || "";
    return line.length > 90 ? `${line.slice(0, 90)}…` : line;
}
