// The single gate every state-changing action goes through.
import { createContext } from "preact";
import { useCallback, useContext, useMemo, useRef, useState } from "preact/hooks";

import { ACTIONS, known } from "./registry.js";
import { noteAction } from "../catchup.js";
import { html } from "../html.js";
import { Sheet } from "../ui/sheet.js";
import { useToast } from "../ui/toasts.js";

const ENDPOINT = "/api/actions";

const GateContext = createContext(null);

async function send(id, target, params) {
    const spec = ACTIONS[id].send;
    const url = spec ? spec.path(target, params) : ENDPOINT;

    const request = spec
        ? {
            method: spec.verb,
            credentials: "same-origin",
            ...(spec.body
                ? { headers: { "Content-Type": "application/json" }, body: JSON.stringify(spec.body(target, params)) }
                : {}),
        }
        : {
            method: "POST",
            credentials: "same-origin",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ kind: id, target, params }),
        };

    let response;
    try {
        response = await fetch(url, request);
    } catch (err) {
        return { ok: false, error: "the network is unavailable" };
    }

    if (response.status === 401) return { ok: false, error: "the session has ended", status: 401 };
    if (response.status === 404 || response.status === 405) {
        return { ok: false, error: "the executor is not connected yet", status: response.status };
    }
    if (!response.ok) {
        const text = (await response.text()).trim();
        return { ok: false, error: explain(text, response.status), status: response.status };
    }

    noteAction(ACTIONS[id].watch, target);

    const body = await response.text();
    try {
        return { ok: true, data: body ? JSON.parse(body) : null };
    } catch (err) {
        return { ok: true, data: null };
    }
}

function explain(text, status) {
    try {
        const parsed = JSON.parse(text);
        if (parsed && parsed.error) return parsed.error;
    } catch (err) {
    }
    if (!text || /^\s*</.test(text)) return byStatus(status);
    return text.length > 300 ? `${text.slice(0, 300)}…` : text;
}

function byStatus(status) {
    if (status === 413) return "too large: the proxy in front of the host rejected the whole request";
    if (status >= 500) return `the host did not answer (${status})`;
    return `the server answered ${status}`;
}

// GateHost mounts once above the whole application and holds the confirmation.
export function GateHost({ children }) {
    const [pending, setPending] = useState(null);
    const fired = useRef(null);
    const toast = useToast();

    const run = useCallback(async (id, target, params = {}) => {
        if (!known(id)) return Promise.reject(new Error(`unknown action: ${id}`));

        if (ACTIONS[id].instant) {
            const result = await send(id, target, params);
            if (!result.ok) toast("Not done", result.error, true);
            return result;
        }

        return new Promise((resolve) => {
            setPending({ id, target, params, step: 1, resolve });
        });
    }, [toast]);

    const cancel = useCallback(() => {
        if (!pending) return;
        setPending(null);
        pending.resolve({ ok: false, cancelled: true });
    }, [pending]);

    const escalate = useCallback(() => {
        if (!pending) return;
        const next = ACTIONS[pending.id].escalate;
        if (!next) return;
        setPending({ ...pending, id: next, step: 1 });
    }, [pending]);

    const confirm = useCallback(async () => {
        if (!pending || fired.current === pending) return;
        const action = ACTIONS[pending.id];

        if (action.second && pending.step === 1) {
            setPending({ ...pending, step: 2 });
            return;
        }

        fired.current = pending;
        const { id, target, params, resolve } = pending;
        setPending(null);

        const result = await send(id, target, params);
        let journal = null;
        if (action.journaled !== false) {
            const logged = !result.data || result.data.logged !== false;
            journal = logged ? "written to the journal" : "not written to the journal: the database is unavailable";
        }
        const detail = result.data && result.data.detail;
        const hideToast = !result.ok && action.fieldConflict && result.status === 409;
        const note = result.ok
            ? {
                text: action.done(target, params),
                sub: detail && journal ? `${detail} · ${journal}` : (detail || journal || undefined),
            }
            : hideToast ? null : { text: "Not done", sub: result.error, bad: true };
        if (note) toast(note.text, note.sub, note.bad);
        resolve(result);
    }, [pending, toast]);

    const api = useMemo(() => ({ run }), [run]);
    const action = pending ? ACTIONS[pending.id] : null;
    const screen = pending && pending.step === 2 ? action.second : action;
    const danger = action && (typeof action.danger === "function"
        ? action.danger(pending.params)
        : action.danger);

    return html`
        <${GateContext.Provider} value=${api}>
            ${children}
            <${Sheet} open=${Boolean(pending)} onClose=${cancel} label="action confirmation">
                ${pending && html`
                    <div class="shead">
                        <span class="dot ${danger ? "crit" : "warn"}"></span>
                        <div>
                            <div class="stitle">${screen.title(pending.target, pending.params)}</div>
                            <div class="ssub">${action.journaled === false ? "the action is not journalled" : "the action goes into the journal"}</div>
                        </div>
                    </div>
                    <div class="warnline">
                        ${typeof screen.effect === "function" ? screen.effect(pending.params) : screen.effect}
                    </div>
                    <div class="btnrow">
                        <button class="btn" type="button" onClick=${cancel}>Cancel</button>
                        <button
                            class="btn ${danger ? "danger" : "primary"}"
                            type="button"
                            onClick=${confirm}
                        >${screen.ok}</button>
                    </div>
                    ${action.escalate && pending.step === 1 && html`
                        <button class="item danger" type="button" onClick=${escalate}>
                            ${action.escalateLabel}
                        </button>
                    `}
                `}
            <//>
        <//>
    `;
}

// useAction returns the only way to run an action: run(id, target, params).
export function useAction() {
    const api = useContext(GateContext);
    if (!api) throw new Error("useAction outside GateHost: there is nobody to confirm the action");
    return api.run;
}
