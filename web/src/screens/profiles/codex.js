// The models codex offers, as the daemon of a codex home lists them: the
// Codex tab picks its model from them, and offers the efforts the chosen one
// takes. They are asked once the tab is first open — a page that never shows
// it does not wake the daemon.
import { useEffect, useState } from "preact/hooks";

import { valueOf } from "./draft.js";

export function useCodexModels(wanted) {
    const [got, setGot] = useState(null);
    useEffect(() => {
        if (!wanted || got) return undefined;
        let live = true;
        (async () => {
            try {
                const response = await fetch("/api/codex/models", { credentials: "same-origin" });
                const body = response.ok ? await response.json() : { reason: `the server answered ${response.status}` };
                if (live) setGot(body);
            } catch {
                if (live) setGot({ reason: "the network is unavailable" });
            }
        })();
        return () => {
            live = false;
        };
    }, [wanted]);
    return got;
}

function codexModels(codex) {
    return (codex && codex.state === "ok" && codex.models) || [];
}

// codexRows returns what the list of codex models offers: each model by the
// name codex gives it, with the efforts it takes.
function codexRows(codex) {
    return codexModels(codex).map((m) => ({
        value: m.model,
        name: m.name || m.model,
        meaning: [(m.efforts || []).join(" · "), m.effort ? `${m.effort} by default` : ""].filter(Boolean).join(", "),
    }));
}

// codexNote says where the list came from, or why there is none.
function codexNote(codex) {
    if (!codex) return "asking the codex daemon for its models…";
    if (codex.state === "ok") return "the models the codex daemon lists";
    return `the models of codex were not read: ${codex.reason || "the panel did not answer"}`;
}

// pickList returns what the list of a model's row is drawn from: codex's
// from what its daemon lists; claude's from the catalogue of the account,
// which the list reads itself.
export function pickList(key, codex) {
    return key === "codexModel" ? { rows: codexRows(codex), note: codexNote(codex) } : {};
}

// effortOffer returns the efforts the chosen codex model takes, offered in
// place of all of them — null where the model is not on the list, and then
// nothing is known to leave out — and what is said of a chosen effort the
// model does not take.
export function effortOffer(codex, effective) {
    const model = valueOf(effective, "codexModel").value;
    const effort = valueOf(effective, "codexEffort").value;
    const found = codexModels(codex).find((m) => m.model === model);
    if (!found) return { efforts: null, strike: "" };
    const efforts = found.efforts || [];
    const strike = effort && !efforts.includes(effort) ? `${found.name || found.model} does not take ${effort}` : "";
    return { efforts, strike };
}
