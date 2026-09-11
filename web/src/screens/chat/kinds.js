// What to show an opened file with: the decisions, without markup and without DOM.

const MARKDOWN = /\.(md|markdown)$/i;

const PAGE = /\.(html?|xhtml)$/i;

const SHEET = /\.(csv|tsv)$/i;

// isExec reports whether the file is a program.
export function isExec(state) {
    return Boolean(state) && (state.form === "exec" || state.kind === "exec");
}

// pickView returns the name of the view to show a file with.
export function pickView(state, name) {
    if (!state) return "code";
    if (isExec(state)) return "exec";
    const form = state.form || "";
    if (state.tooBig) return "toobig";
    if (form === "image" || form === "video" || form === "audio" || form === "pdf") {
        return form;
    }
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
