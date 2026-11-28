import { Icon } from "../../ui/icons.js";
import { html } from "../../html.js";
import { knows, whyNot } from "../../exec.js";
import { useAction } from "../../actions/gate.js";

export function backgroundItems({ live, exec, run, onTerm }) {
    const attach = knows(exec, "session.bgattach") && Boolean(onTerm);
    const stop = knows(exec, "session.bgstop");
    const adopt = knows(exec, "session.bgadopt");
    const open = async () => {
        const result = await run("session.bgattach", live.session, {});
        if (result?.ok && result.data?.id) onTerm({ id: result.data.id, place: live.cwd });
    };
    return [
        { key: "bgattach", icon: Icon.terminal, word: "Terminal", label: "Open in terminal",
            off: attach ? "" : whyNot(exec, "session.bgattach") || "open the terminal-enabled panel", act: open },
        { key: "bgstop", icon: Icon.stopsquare, word: "Stop", label: "Stop background session",
            off: stop ? "" : whyNot(exec, "session.bgstop"), act: () => run("session.bgstop", live.session, {}) },
        { key: "bgadopt", icon: Icon.feed, word: "To panel", label: "Move to panel",
            off: adopt ? "" : whyNot(exec, "session.bgadopt"), act: () => run("session.bgadopt", live.session, {}) },
    ];
}

export function BackgroundActions({ live, exec, onTerm }) {
    const run = useAction();
    return html`<div class="backgroundsession">
        <p class="outsidenote">Claude Code background session</p>
        <div class="toolsegs">
            ${backgroundItems({ live, exec, run, onTerm }).map((it) => html`
                <button type="button" class="btn" disabled=${Boolean(it.off)} title=${it.off || undefined}
                        onClick=${it.act}>${it.label}</button>`)}
        </div>
    </div>`;
}
