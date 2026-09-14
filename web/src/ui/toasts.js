import { createContext } from "preact";
import { useCallback, useContext, useEffect, useMemo, useRef, useState } from "preact/hooks";

import { html } from "../html.js";
import { Toast } from "./toast.js";

// How long a toast stays: enough to read two lines, short enough that the
// one about an action just taken is gone before the next screen.
export const LIFETIME = 5000;

const ToastContext = createContext(null);

// ToastHost mounts once above the whole application and owns the one toast
// and its clock. The clock lives here, not in an effect of the toast: an
// effect is re-armed by whatever re-renders around it, and a clock that is
// re-armed never rings.
export function ToastHost({ children }) {
    const [toast, setToast] = useState(null);
    const timer = useRef(null);

    const hide = useCallback(() => {
        clearTimeout(timer.current);
        timer.current = null;
        setToast(null);
    }, []);

    // A repeated show restarts the clock: the last toast gets its full time.
    const show = useCallback((text, sub, bad = false) => {
        clearTimeout(timer.current);
        timer.current = setTimeout(hide, LIFETIME);
        setToast({ text, sub, bad });
    }, [hide]);

    // A page put away has had its moment: whoever comes back to it later
    // should not find a note about something long done.
    useEffect(() => {
        const away = () => {
            if (document.visibilityState === "hidden") hide();
        };
        document.addEventListener("visibilitychange", away);
        return () => {
            document.removeEventListener("visibilitychange", away);
            clearTimeout(timer.current);
        };
    }, [hide]);

    const api = useMemo(() => ({ show, hide }), [show, hide]);

    return html`
        <${ToastContext.Provider} value=${api}>
            ${children}
            <${Toast} toast=${toast} />
        <//>
    `;
}

function useToastApi() {
    const api = useContext(ToastContext);
    if (!api) throw new Error("useToast outside ToastHost");
    return api;
}

export function useToast() {
    return useToastApi().show;
}

// useToastHide returns the way to take the toast down early: a screen that
// goes away takes the note about its action with it.
export function useToastHide() {
    return useToastApi().hide;
}
