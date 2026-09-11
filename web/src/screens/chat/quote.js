// A quote from the feed: the selected piece of a message goes to the composer
// as a markdown quote.

export const QUOTE_LINES = 2;
export const QUOTE_CHARS = 240;

export const QUOTABLE = ".msg, .mmbody, .callpre";

// quoteOf returns a markdown quote of the selected text, or an empty string.
export function quoteOf(raw) {
    const lines = String(raw || "").split("\n").map((line) => line.trim()).filter(Boolean);
    if (!lines.length) return "";
    let cut = lines.length > QUOTE_LINES;
    const out = [];
    let total = 0;
    for (const line of lines.slice(0, QUOTE_LINES)) {
        if (total + line.length > QUOTE_CHARS) {
            const room = QUOTE_CHARS - total;
            if (room > 0) out.push(line.slice(0, room).trimEnd());
            cut = true;
            break;
        }
        out.push(line);
        total += line.length;
    }
    if (cut) out[out.length - 1] += "…";
    return out.map((line) => "> " + line).join("\n");
}

// withQuote puts the quote at the start of the message, what was typed below it.
export function withQuote(quote, text) {
    const rest = String(text || "").replace(/^\s+/, "");
    return `${quote}\n\n${rest}`;
}
