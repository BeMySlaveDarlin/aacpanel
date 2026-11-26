import { useEffect, useLayoutEffect, useRef, useState } from "preact/hooks";

import { html } from "./html.js";
import { Icon } from "./ui/icons.js";

let loading = null;
let drawing = Promise.resolve();
let serial = 0;

function engine() {
    if (!loading) {
        loading = new Promise((resolve, reject) => {
            const script = document.createElement("script");
            script.src = "/dist/mermaid.js";
            script.onload = () => resolve(globalThis.mermaid);
            script.onerror = () => {
                script.remove();
                loading = null;
                reject(new Error("The diagram engine could not be loaded"));
            };
            document.head.append(script);
        });
    }
    return loading;
}

function palette(el) {
    const css = getComputedStyle(el);
    const canvas = document.createElement("canvas");
    canvas.width = canvas.height = 1;
    const ctx = canvas.getContext("2d");
    // Mermaid derives colours from hex. Composite translucent surface tokens
    // on the panel's background before handing them to its theme engine.
    const color = (name) => {
        ctx.fillStyle = css.getPropertyValue("--bg").trim();
        ctx.fillRect(0, 0, 1, 1);
        ctx.fillStyle = css.getPropertyValue(name).trim();
        ctx.fillRect(0, 0, 1, 1);
        return "#" + [...ctx.getImageData(0, 0, 1, 1).data].slice(0, 3).map((n) => n.toString(16).padStart(2, "0")).join("");
    };
    const ink = color("--ink"), line = color("--line"), surface = color("--surface-2"), bg = color("--bg"), accent = color("--accent");
    return { darkMode: !document.documentElement.classList.contains("sky"), fontFamily: css.fontFamily, fontSize: css.fontSize,
        primaryColor: surface, primaryTextColor: ink, primaryBorderColor: line, secondaryColor: surface, tertiaryColor: bg,
        textColor: ink, lineColor: accent, background: bg, mainBkg: surface, nodeTextColor: ink,
        edgeLabelBackground: bg, clusterBkg: surface, clusterBorder: line, actorBkg: surface, actorTextColor: ink };
}

function diagram(text, el) {
    // initialize changes global configuration; keep it with its render so
    // simultaneous blocks and theme changes cannot borrow each other's theme.
    const next = drawing.then(async () => {
        const mermaid = await engine();
        mermaid.initialize({ startOnLoad: false, securityLevel: "strict", htmlLabels: false,
            suppressErrorRendering: true, theme: "base", themeVariables: palette(el),
            flowchart: { htmlLabels: false, useMaxWidth: false },
            secure: ["secure", "securityLevel", "startOnLoad", "maxTextSize", "maxEdges", "suppressErrorRendering",
                "htmlLabels", "flowchart", "theme", "themeVariables", "themeCSS", "dompurifyConfig"] });
        const { svg } = await mermaid.render(`mdmermaid-${++serial}`, text);
        // Strict mode sanitizes the SVG; interaction binders are deliberately
        // not installed for diagrams supplied by files or messages.
        return svg;
    });
    drawing = next.catch(() => {});
    return next;
}

export function MermaidBlock({ text }) {
    const box = useRef(null);
    const view = useRef(null);
    const drawn = useRef("");
    const [visible, setVisible] = useState(false);
    const [theme, setTheme] = useState(0);
    const [source, setSource] = useState(false);
    const [result, setResult] = useState({ text: "", svg: "", error: "" });
    const svg = result.text === text ? result.svg : "";
    const error = result.text === text ? result.error : "";
    useEffect(() => {
        if (typeof IntersectionObserver === "undefined") { setVisible(true); return undefined; }
        const eye = new IntersectionObserver((entries) => setVisible(entries.some((entry) => entry.isIntersecting)), { rootMargin: "240px 0px" });
        eye.observe(box.current);
        return () => eye.disconnect();
    }, []);
    useEffect(() => {
        if (!visible) return undefined;
        const watch = new MutationObserver(() => setTheme((n) => n + 1));
        watch.observe(document.documentElement, { attributes: true, attributeFilter: ["class", "style"] });
        return () => watch.disconnect();
    }, [visible]);
    useEffect(() => {
        if (!visible) return undefined;
        const css = getComputedStyle(box.current);
        const key = JSON.stringify([text, document.documentElement.className, css.fontSize, css.fontFamily]);
        if (drawn.current === key) return undefined;
        let alive = true;
        diagram(text, box.current).then((rendered) => {
            if (alive) {
                drawn.current = key;
                setResult({ text, svg: rendered, error: "" });
            }
        }).catch((err) => {
            if (alive) {
                drawn.current = key;
                setResult({ text, svg: "", error: String(err.message || err).replace(/\s+/g, " ").trim() });
            }
        });
        return () => { alive = false; };
    }, [text, visible, theme]);
    useLayoutEffect(() => {
        if (!view.current) return;
        if (!svg) { view.current.replaceChildren(); return; }
        const parsed = new DOMParser().parseFromString(svg, "image/svg+xml");
        const node = document.importNode(parsed.documentElement, true);
        if (node.viewBox.baseVal.width) {
            node.style.width = `${node.viewBox.baseVal.width}px`;
            node.style.maxWidth = `${node.viewBox.baseVal.width}px`;
        }
        view.current.replaceChildren(node);
    }, [svg]);
    return html`<div class="mdcode mdmermaid" ref=${box}>
        <pre hidden=${Boolean(svg) && !source}><code>${text}</code></pre>
        <div class="mdmermaidview" ref=${view} hidden=${!svg || source}></div>
        ${error && html`<p class="mdmermaiderror" title=${error}>${error}</p>`}
        <button type="button" class="mdrun mdmode" disabled=${!svg} aria-label=${source ? "Show diagram" : "Show code"}
                onClick=${() => setSource((v) => !v)}>${source ? "Diagram" : "Code"}</button>
        <button type="button" class="mdcopy" data-md=${text} title="Copy the code" aria-label="Copy the code">${Icon.copy()}</button>
    </div>`;
}
