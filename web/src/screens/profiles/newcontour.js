// A new contour is taken from an account of the host the map has no contour
// for yet: the account itself — its directory, its token, the router's line —
// is set up on the host, and the map only stands for it. By hand, with paths,
// only on a machine without the router.
import { useState } from "preact/hooks";

import { html } from "../../html.js";
import { BackHead, useBackClose } from "../../ui/back.js";
import { useAction } from "../../actions/gate.js";
import { authState } from "./pick.js";
import { Layer } from "./kit.js";

// routed says whether the host routes accounts by directory: then an account
// is the host's, and a contour typed by hand would stand for nothing there.
export function routed(profiles, accounts) {
    return (profiles || []).some((p) => p.route) || (accounts || []).some((a) => a.route);
}

export function NewContour({ profiles, accounts, onClose, onDone, onManual }) {
    useBackClose(true, onClose);
    const run = useAction();
    const [problem, setProblem] = useState("");
    const list = accounts || [];
    const byHand = !routed(profiles, accounts);

    const take = async (a) => {
        setProblem("");
        const prefix = a.route && a.route.prefix !== "*" ? a.route.prefix : "";
        const result = await run("profile.add", a.name, { fields: { name: a.name, configDir: a.configDir, prefix } });
        if (result && result.ok) {
            onDone(result.data);
            onClose();
        } else if (result && result.status === 409) {
            setProblem(result.error);
        }
    };

    return html`
        <${BackHead} onBack=${onClose} label="back">
            <h2>New contour</h2>
            <span class="where">an account of this host the map has no contour for</span>
        <//>
        ${problem && html`<p class="hint warn">${problem}</p>`}
        ${list.length === 0
            ? html`<p class="empty">Every account of this host is on the map. An account is set up on the host — its config
                directory, its token${byHand ? "" : ", its line in the router's registry"} — and then it shows here.</p>`
            : list.map((a) => {
                const auth = authState(a);
                return html`
                    <button class="pzaccount" type="button" key=${a.configDir} onClick=${() => take(a)}>
                        <span class="pzaccountname">${a.name}</span>
                        <span class="pzhelp pfpath">${a.configDir}</span>
                        <span class="pzhelp">${auth.text}${a.route
                            ? ` · routed by ${a.route.prefix === "*" ? "every directory no other contour takes" : a.route.prefix}`
                            : " · not in the router's registry"}</span>
                    </button>
                `;
            })}
        ${byHand && html`
            <button class="btn" type="button" onClick=${onManual}>Add by hand</button>
            <p class="pfhelp">this machine has no router: the paths of a contour are typed in</p>
        `}
    `;
}

export function NewContourLayer(props) {
    return html`<${Layer} label="new contour"><${NewContour} ...${props} /><//>`;
}
