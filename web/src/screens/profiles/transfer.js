import { useState } from "preact/hooks";
import { html } from "../../html.js";
import { useAction } from "../../actions/gate.js";
import { knows, useExec, whyNot } from "../../exec.js";

// A transfer is outside the settings draft: its file work and map update are
// one host action, and the gate is the only confirmation it gets.
export function ProjectTransfer({ project, contour, profiles, dirty, onDone, onClose }) {
    const run = useAction();
    const exec = useExec();
    const [to, setTo] = useState("");
    const [busy, setBusy] = useState(false);
    const choices = (profiles || []).filter((p) => p.id !== contour.id)
        .flatMap((p) => (p.groups || []).map((g) => ({ id: g.id, label: `${p.name} · ${g.name}`, contour: p.name })));
    const picked = choices.find((g) => String(g.id) === to);
    const ready = knows(exec, "project.move");
    const reason = !ready ? whyNot(exec, "project.move") : dirty ? "save or discard the settings draft first"
        : !choices.length ? "add a group to another contour first" : "";
    return html`
        <div class="pffield">
            <span class="pflabel">Move to another contour</span>
            <span class="pfhelp">carry this project's Claude conversations and session files with its map entry</span>
            <select class="search" aria-label="destination contour and group" value=${to}
                    disabled=${busy || Boolean(reason)} onChange=${(e) => setTo(e.target.value)}>
                <option value="">Choose a contour and group</option>
                ${choices.map((g) => html`<option key=${g.id} value=${String(g.id)}>${g.label}</option>`)}
            </select>
            ${reason && html`<span class="pfhelp">${reason}</span>`}
            <button class="btn" type="button" disabled=${busy || Boolean(reason) || !picked}
                    onClick=${async () => {
                        if (!picked || busy || reason) return;
                        setBusy(true);
                        try {
                            const result = await run("project.move", String(project.id), {
                                name: project.name, from: contour.name, to: picked.label, group: picked.id,
                            });
                            if (result && result.ok) {
                                onDone(result.data);
                                onClose();
                            }
                        } finally { setBusy(false); }
                    }}>Move project</button>
        </div>
    `;
}
