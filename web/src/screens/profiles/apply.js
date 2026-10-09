// After a Save: what of the saved values a running session of the project
// can take right away, and by which action — the model, the effort, the mode
// on the stream, Remote Control, a move between tmux and the stream — and
// what it takes only at its next start, said in words. Nothing is applied
// unasked: the page offers, the person presses.
import { html } from "../../html.js";
import { useState } from "preact/hooks";
import { useAction } from "../../actions/gate.js";
import { label, valueOf } from "./draft.js";

const SET = { model: "model", effort: "effort", permissionMode: "mode" };

// offers returns, for one running session, what each saved key comes to.
export function offers(keys, effective, params, session) {
    const onStream = session.transport === "stream";
    const out = [];
    for (const key of keys) {
        const param = params.find((p) => p.key === key);
        // A running session is claude's: a key of codex reaches none of it.
        if (!param || param.agent === "codex") continue;
        const value = valueOf(effective, key).value;
        const said = value === null ? param.unset : label(param, value);
        const live = (param.live || {})[onStream ? "stream" : "tmux"];
        if (key === "transport") {
            const to = value === "stream" ? "stream" : "console";
            if ((to === "stream") === onStream) continue;
            out.push({ key, text: `${to === "stream" ? "Move to the stream" : "Move to tmux"}`,
                action: "session.switch", params: { to } });
        } else if (key === "remoteControl" && live === "now") {
            if (Boolean(value) === Boolean(session.remote)) continue;
            out.push({ key, text: `Remote Control ${value ? "on" : "off"}`, action: "session.remote", params: { on: Boolean(value) } });
        } else if (SET[key] && live === "now" && value !== null) {
            out.push({
                key, text: `${param.label} ${said}`, action: "session.set", params: { [SET[key]]: value },
                note: !onStream && key !== "permissionMode" ? "in tmux claude also keeps it as the account's default for new sessions" : "",
            });
        } else if (live === "now" && param.host) {
            out.push({ key, text: `${param.label} ${said}`, later: "the host reads it already" });
        } else {
            out.push({ key, text: `${param.label} ${said}`, later: live === "on move" ? "at the next start or on a move" : "at the next start" });
        }
    }
    return out;
}

// Apply is the card that offers a Save to the running sessions of a project.
export function Apply({ saved, params, sessions, onDone, onClose }) {
    const run = useAction();
    const [done, setDone] = useState({});
    if (!saved || sessions.length === 0) return null;
    return html`
        <div class="pzapply">
            <div class="pzapplyhead">
                <span>${sessions.length === 1 ? `${sessions[0].session} runs now` : `${sessions.length} sessions run now`}</span>
                <button class="pzx" type="button" aria-label="close" onClick=${onClose}>✕</button>
            </div>
            ${sessions.map((s) => {
                const list = offers(saved.keys, saved.effective, params, s);
                const now = list.filter((o) => o.action);
                const later = list.filter((o) => !o.action);
                return html`
                    <div class="pzapplyrow" key=${s.session}>
                        ${sessions.length > 1 && html`<span class="pzapplywho">${s.session}</span>`}
                        ${now.length > 0 && html`
                            <div class="pzopts">
                                ${now.map((o) => {
                                    const id = `${s.session}:${o.key}`;
                                    return html`
                                        <button key=${o.key} class="pzopt" type="button" disabled=${Boolean(done[id])}
                                                aria-pressed=${done[id] ? "true" : "false"}
                                                onClick=${async () => {
                                                    const result = await run(o.action, s.session, o.params);
                                                    if (result && result.ok) {
                                                        setDone((d) => ({ ...d, [id]: true }));
                                                        if (onDone) onDone(o);
                                                    }
                                                }}>${o.text}</button>
                                    `;
                                })}
                            </div>
                            ${now.filter((o) => o.note).map((o) => html`<span class="pzhelp" key=${`n${o.key}`}>${o.text}: ${o.note}</span>`)}
                        `}
                        ${later.map((o) => html`<span class="pzhelp" key=${`l${o.key}`}>${o.text} — ${o.later}</span>`)}
                    </div>
                `;
            })}
        </div>
    `;
}
