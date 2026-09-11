// Screen width as the layout signal.
import { useEffect, useState } from "preact/hooks";

export const WIDE = "(min-width: 1100px)";

export function useWide() {
    const [wide, setWide] = useState(() => window.matchMedia(WIDE).matches);

    useEffect(() => {
        const mq = window.matchMedia(WIDE);
        const on = (e) => setWide(e.matches);
        mq.addEventListener("change", on);
        setWide(mq.matches);
        return () => mq.removeEventListener("change", on);
    }, []);

    return wide;
}
