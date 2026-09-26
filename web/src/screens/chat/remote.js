// Remote Control of a session: the bridge that makes it reachable from the
// Claude app and claude.ai, how it stands and how it is switched.

import { useEffect, useState } from "preact/hooks";

import { useAction } from "../../actions/gate.js";
import { knows, whyNot } from "../../exec.js";

// How long a switch that went through stands on the button before the
// snapshot has caught up with it.
const SETTLE_MS = 30000;

// remoteContour names the contour whose account the bridge would open in when
// it is not the account of the machine itself, or nothing when it is. The
// Claude app and claude.ai are signed into the machine's own account; a
// contour on a token of its own is another account, where the session would
// appear and the phone would not see it.
export function remoteContour(live, snapshot) {
    const profiles = (snapshot && snapshot.profiles) || [];
    const mine = profiles.find((p) => p.name === live.profile);
    if (!mine || mine.auth === "builtin") return "";
    return mine.name;
}

// useRemote says how Remote Control of a live session stands, why it cannot
// be switched when it cannot, and switches it. A switch that went through
// stands the new way until the snapshot catches up with it.
export function useRemote({ name, live, exec, snapshot }) {
    const run = useAction();
    const [want, setWant] = useState(null);
    const on = Boolean(live.remote);

    useEffect(() => {
        if (want === null) return undefined;
        if (want === on) {
            setWant(null);
            return undefined;
        }
        const timer = setTimeout(() => setWant(null), SETTLE_MS);
        return () => clearTimeout(timer);
    }, [want, on]);

    const shown = want === null ? on : want;
    const contour = remoteContour(live, snapshot);
    const why = contour
        ? `Remote Control of this session would open in the claude.ai account of contour ${contour}, `
            + "not the one the Claude app is signed into"
        : knows(exec, "session.remote") ? "" : whyNot(exec, "session.remote");
    const off = Boolean(why) || want !== null;
    const say = shown
        ? `Remote Control is on${live.remote ? `: ${live.remote.replace(/^https:\/\//, "")}` : ""} · switch it off`
        : "Switch Remote Control on, to go on from the Claude app or claude.ai";
    const press = async () => {
        const result = await run("session.remote", name, { on: !shown });
        if (result && result.ok) setWant(!shown);
    };
    return { shown, contour, why, off, settling: want !== null, say, press };
}
