// The details sheet: background command output, subagent letters, a project file.

import { useEffect, useState } from "preact/hooks";

import { html } from "../../html.js";
import { BackHead, useBackClose } from "../../ui/back.js";
import { Icon } from "../../ui/icons.js";
import { useToast } from "../../ui/toasts.js";
import * as copy from "./copy.js";
import { render } from "../../md.js";
import { FileBody } from "./filebody.js";
import { bytes } from "../../format.js";
import { idParam } from "./api.js";
import { stampText } from "./labels.js";

export const LOOK_NAMES = {
    tasks: "background work",
    agents: "subagents",
    workflows: "workflow runs",
    workflow: "workflow run",
    arts: "published pages",
    briefs: "briefs",
    task: "command output",
    agent: "agent letters",
    file: "project file",
    artifact: "published page",
    brief: "brief",
    command: "command output",
    mcp: "MCP servers",
    status: "session info",
    commands: "commands",
};

export const WORK_LISTS = new Set(["tasks", "agents", "arts", "briefs", "workflows"]);

function fileURL(base, path, offset) {
    const at = offset > 0 ? `&offset=${offset}` : "";
    return `/api/chat/file?${base}&path=${encodeURIComponent(path)}${at}`;
}

function saveURL(base, path) {
    return `/api/chat/file/download?${base}&path=${encodeURIComponent(path)}`;
}

// openURL is the file as the browser shows it rather than saves it: the same
// route as saving, so the same check of the path against the directory of the
// conversation, and the service worker lets it past the same way.
function openURL(base, path) {
    return `${saveURL(base, path)}&inline=1`;
}

export function Look({ session, id, look, onBack }) {
    const toast = useToast();
    const [state, setState] = useState({ kind: "loading" });
    const [more, setMore] = useState({ busy: false, error: "" });
    const task = look.kind === "task";
    const file = look.kind === "file";
    const base = `session=${encodeURIComponent(session)}${idParam(id)}`;

    useBackClose(Boolean(onBack), onBack);

    useEffect(() => {
        let alive = true;
        setState({ kind: "loading" });
        setMore({ busy: false, error: "" });
        const ask = async () => {
            const url = task
                ? `/api/chat/task?${base}&task=${encodeURIComponent(look.id)}`
                : file
                    ? fileURL(base, look.path, 0)
                    : `/api/chat/agent?${base}&name=${encodeURIComponent(look.name)}`;
            try {
                const r = await fetch(url);
                if (!r.ok) throw new Error((await r.text()).trim() || `response ${r.status}`);
                const data = await r.json();
                if (alive) setState({ ...data, kind: "ready", form: data.kind || "" });
            } catch (e) {
                if (alive) setState({ kind: "failed", error: String(e.message || e) });
            }
        };
        ask();
        if (!task) return () => { alive = false; };
        const timer = setInterval(() => {
            if (document.visibilityState === "visible") ask();
        }, 4000);
        return () => { alive = false; clearInterval(timer); };
    }, [session, id, look.kind, look.id, look.name, look.path]);

    const loadMore = async () => {
        if (!state.next || more.busy) return;
        setMore({ busy: true, error: "" });
        try {
            const r = await fetch(fileURL(base, look.path, state.next));
            if (!r.ok) throw new Error((await r.text()).trim() || `response ${r.status}`);
            const data = await r.json();
            setState((was) => ({
                ...was,
                text: (was.text || "") + (data.text || ""),
                next: data.next || 0,
                cut: Boolean(data.next),
            }));
            setMore({ busy: false, error: "" });
        } catch (e) {
            setMore({ busy: false, error: String(e.message || e) });
        }
    };

    const letters = state.letters || [];
    const who = html`
        <div class="chatwho">
                <h2>${task ? "Background command" : (file ? (state.name || look.path) : look.name)}</h2>
            <div class="chatsub">
                <span>${file ? (look.text || look.path) : (look.text || (task ? "output" : "agent letters"))}</span>
                ${file && state.size > 0 && html`<span class="sep">·</span><span>${bytes(state.size)}</span>`}
            </div>
        </div>
    `;
    const pick = state.kind === "ready" && file ? copy.filePick(state) : null;
    // Every kind of file is saved, an executable and a binary included: the one
    // the screen has nothing to show with is the one there is a reason to save.
    const save = state.kind === "ready" && file;
    const tools = (pick || save) && html`
        ${pick && html`
            <button type="button" class="mdcopy filecopy"
                    title="Copy the contents" aria-label="Copy the contents"
                    onClick=${() => copy.fileCopy(state, toast)}>${Icon.copy()}</button>
        `}
        ${save && html`
            <a class="mdcopy filecopy" href=${saveURL(base, look.path)}
               download=${state.name || look.path}
               title="Save the file" aria-label="Save the file">${Icon.download()}</a>
        `}
    `;
    return html`
        ${onBack
            ? html`<${BackHead} onBack=${onBack} label="to the list" tools=${tools}>${who}<//>`
            : html`<div class="sheethead">${who}${tools}</div>`}

        <div class="callbody" onClick=${(event) => copy.fromClick(event, toast)}>
            ${state.kind === "loading" && html`<p class="hint">Reading…</p>`}
            ${state.kind === "failed" && html`<p class="hint crit">${state.error}</p>`}
            ${state.kind === "ready" && task && html`
                ${state.cut && html`<p class="hint warn">Showing the tail: the output is longer.</p>`}
                <pre class="callpre">${state.text || "nothing yet"}</pre>
            `}
            ${state.kind === "ready" && file && html`<${FileBody} state=${state} look=${look}
                open=${openURL(base, look.path)} more=${more} onMore=${loadMore} />`}
            ${state.kind === "ready" && !task && !file && (letters.length === 0
                ? html`<p class="hint">The agent is working and has not reported yet.</p>`
                : letters.map((letter, n) => html`
                    <div class="letter" key=${n}>
                        ${letter.at && html`<div class="lat">${stampText(letter.at)}</div>`}
                        <div class="mmbody">${render(letter.text)}</div>
                    </div>
                `))}
        </div>
    `;
}
