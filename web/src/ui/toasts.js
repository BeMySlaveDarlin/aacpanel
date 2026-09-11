import { createContext } from "preact";
import { useCallback, useContext, useMemo, useState } from "preact/hooks";

import { html } from "../html.js";
import { Toast } from "./toast.js";

const ToastContext = createContext(null);

export function ToastHost({ children }) {
    const [toast, setToast] = useState(null);

    const show = useCallback((text, sub, bad = false) => {
        setToast({ text, sub, bad, at: Date.now() });
    }, []);

    const api = useMemo(() => ({ show }), [show]);

    return html`
        <${ToastContext.Provider} value=${api}>
            ${children}
            <${Toast} toast=${toast} onHide=${() => setToast(null)} />
        <//>
    `;
}

export function useToast() {
    const api = useContext(ToastContext);
    if (!api) throw new Error("useToast outside ToastHost");
    return api.show;
}
