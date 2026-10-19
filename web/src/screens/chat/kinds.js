// What to show an opened file with: the decisions, without markup and without DOM.

const MARKDOWN = /\.(md|markdown)$/i;

const PAGE = /\.(html?|xhtml)$/i;

// PAGE_WHOLE is the largest page the panel draws: a page is read whole before
// it is drawn, and past this it is saved and opened rather than waited for.
export const PAGE_WHOLE = 8 << 20;

// PAGE_WINDOW is how much of a page one read asks for: the largest window the
// collector gives when asked, so that a page up to PAGE_WHOLE is a handful of
// reads. Every other file is read by the default window, a press at a time.
export const PAGE_WINDOW = 1 << 20;

// isPage reports whether a file is a page the panel draws as a document.
export function isPage(name) {
    return PAGE.test(String(name || ""));
}

const SHEET = /\.(csv|tsv)$/i;

// isExec reports whether the file is a program.
export function isExec(state) {
    return Boolean(state) && (state.form === "exec" || state.kind === "exec");
}

// DRAWN are the pictures every browser draws from an address. The rest of
// what the collector calls a picture — HEIC and TIFF, which Chrome does not
// decode, and SVG, which the panel does not show in place since it is a page —
// has no view past the ceiling of the viewer.
const DRAWN = new Set([
    "image/png", "image/jpeg", "image/gif", "image/webp", "image/avif",
    "image/bmp", "image/x-icon", "image/apng",
]);

// pickView returns the name of the view to show a file with. A sound, a clip,
// a PDF and a picture a browser draws are shown whatever their size: what the
// viewer does not bring whole, the view asks for at the file's address.
export function pickView(state, name) {
    if (!state) return "code";
    if (isExec(state)) return "exec";
    const form = state.form || "";
    if (form === "video" || form === "audio" || form === "pdf") return form;
    if (state.tooBig && !(form === "image" && DRAWN.has(state.media))) return "toobig";
    if (form === "image") return form;
    if (form === "binary" || state.binary) return "binary";
    if (state.data) return "image";
    const file = String(name || "");
    if (PAGE.test(file)) return "page";
    if (MARKDOWN.test(file)) return "doc";
    if (SHEET.test(file)) return "sheet";
    return "code";
}

const FRAME_CSP = [
    "default-src 'none'",
    "img-src data:",
    "font-src data:",
    "style-src 'unsafe-inline'",
    "base-uri 'none'",
    "form-action 'none'",
].join("; ");

// frameDoc returns the document for the sandbox srcdoc.
export function frameDoc(text) {
    return "<!doctype html><meta charset=\"utf-8\">"
        + `<meta http-equiv="Content-Security-Policy" content="${FRAME_CSP}">`
        + String(text == null ? "" : text);
}

export const MAX_ROWS = 400;

// sheet returns the rows and cells of a separated-values table.
export function sheet(text, name) {
    const apart = /\.tsv$/i.test(String(name || "")) ? "\t" : ",";
    const rows = [];
    let cell = "";
    let row = [];
    let quoted = false;
    const body = String(text == null ? "" : text);
    const endRow = () => {
        row.push(cell);
        cell = "";
        rows.push(row);
        row = [];
    };
    for (let i = 0; i < body.length && rows.length < MAX_ROWS; i += 1) {
        const ch = body[i];
        if (quoted) {
            if (ch === "\"" && body[i + 1] === "\"") {
                cell += "\"";
                i += 1;
            } else if (ch === "\"") {
                quoted = false;
            } else {
                cell += ch;
            }
            continue;
        }
        if (ch === "\"" && cell === "") quoted = true;
        else if (ch === apart) { row.push(cell); cell = ""; }
        else if (ch === "\n") endRow();
        else if (ch !== "\r") cell += ch;
    }
    if (rows.length < MAX_ROWS && (cell !== "" || row.length)) endRow();
    if (rows.length && rows[rows.length - 1].length === 1 && rows[rows.length - 1][0] === "") {
        rows.pop();
    }
    return rows;
}
