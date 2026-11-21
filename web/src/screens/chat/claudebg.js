import { html } from "../../html.js";
import { knows, whyNot } from "../../exec.js";
import { useAction } from "../../actions/gate.js";

export function BackgroundActions({ live, exec, onTerm }) {
    const run = useAction();
    const attach = knows(exec, "session.bgattach") && Boolean(onTerm);
    const stop = knows(exec, "session.bgstop");
    const adopt = knows(exec, "session.bgadopt");
    const open = async () => {
        const result = await run("session.bgattach", live.session, {});
        if (result?.ok && result.data?.id) onTerm({ id: result.data.id, place: live.cwd });
    };
    return html`<div class="backgroundsession">
        <p class="outsidenote">Claude Code background session</p>
        <div class="toolsegs">
            <button type="button" class="btn" disabled=${!attach}
                    title=${attach ? undefined : whyNot(exec, "session.bgattach") || "open the terminal-enabled panel"}
                    onClick=${open}>Open in terminal</button>
            <button type="button" class="btn" disabled=${!stop} title=${stop ? undefined : whyNot(exec, "session.bgstop")}
                    onClick=${() => run("session.bgstop", live.session, {})}>Stop background session</button>
            <button type="button" class="btn" disabled=${!adopt} title=${adopt ? undefined : whyNot(exec, "session.bgadopt")}
                    onClick=${() => run("session.bgadopt", live.session, {})}>Move to panel</button>
        </div>
    </div>`;
}
