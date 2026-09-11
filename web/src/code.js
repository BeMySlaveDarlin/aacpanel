// Syntax highlighting for file contents opened from the feed.
import { html } from "./html.js";

export const MAX_PAINT = 384 * 1024;

const DQ = String.raw`"(?:\\.|[^"\\\n])*"`;
const SQ = String.raw`'(?:\\.|[^'\\\n])*'`;
const TICK = "`(?:\\\\.|[^`\\\\])*`";

const SLASH = String.raw`//[^\n]*`;
const BLOCK = String.raw`/\*[\s\S]*?\*/`;
const HASH = String.raw`#[^\n]*`;
const DASH = String.raw`--[^\n]*`;
const TAGCM = "<!--[\\s\\S]*?-->";

const YAMLKEY = String.raw`^[ \t]*-?[ \t]*[\w.$-]+(?=\s*:)`;
const INIKEY = String.raw`^[ \t]*\[[^\]\n]+\]|^[ \t]*[\w.-]+(?=\s*=)`;

const WORDS = {
    js: "const let var function return if else for while class extends new async await import export from default try catch finally throw typeof instanceof of in null true false undefined this switch case break continue",
    go: "func package import type struct interface return if else for range var const map chan go defer select switch case break continue nil true false string int int64 bool byte error make new append len cap",
    py: "def class return if elif else for while import from as try except finally with lambda None True False and or not in is pass raise yield self async await global nonlocal assert del",
    sh: "if then fi else elif for while until do done case esac function local export readonly return echo exit set unset source shift trap",
    sql: "select from where insert into update delete join left right inner outer on group by order having limit offset create table index view alter drop primary key foreign references not null default as and or in is distinct union values returning",
    php: "function class return if else elseif foreach for while echo print new use namespace public private protected static const try catch finally throw null true false array isset unset require include",
};

const LANGS = {
    js: { cm: [SLASH, BLOCK], str: [DQ, SQ, TICK], kw: WORDS.js },
    ts: { cm: [SLASH, BLOCK], str: [DQ, SQ, TICK], kw: WORDS.js },
    jsx: { cm: [SLASH, BLOCK], str: [DQ, SQ, TICK], kw: WORDS.js },
    tsx: { cm: [SLASH, BLOCK], str: [DQ, SQ, TICK], kw: WORDS.js },
    go: { cm: [SLASH, BLOCK], str: [DQ, TICK], kw: WORDS.go },
    py: { cm: [HASH], str: [DQ, SQ], kw: WORDS.py },
    sh: { cm: [HASH], str: [DQ, SQ], kw: WORDS.sh },
    bash: { cm: [HASH], str: [DQ, SQ], kw: WORDS.sh },
    php: { cm: [SLASH, HASH, BLOCK], str: [DQ, SQ], kw: WORDS.php },
    sql: { cm: [DASH, BLOCK], str: [DQ, SQ], kw: WORDS.sql, nocase: true },
    css: { cm: [BLOCK], str: [DQ, SQ], extra: [String.raw`@[\w-]+`, String.raw`[-\w]+(?=\s*:)`] },
    json: { cm: [], str: [DQ], extra: [String.raw`\b(?:true|false|null)\b`] },
    jsonl: { cm: [], str: [DQ], extra: [String.raw`\b(?:true|false|null)\b`] },
    yaml: { cm: [HASH], str: [DQ, SQ], extra: [YAMLKEY], multiline: true },
    yml: { cm: [HASH], str: [DQ, SQ], extra: [YAMLKEY], multiline: true },
    toml: { cm: [HASH], str: [DQ, SQ], extra: [INIKEY], multiline: true },
    ini: { cm: [HASH, String.raw`;[^\n]*`], str: [DQ, SQ], extra: [INIKEY], multiline: true },
    conf: { cm: [HASH], str: [DQ, SQ], extra: [INIKEY], multiline: true },
    env: { cm: [HASH], str: [DQ, SQ], extra: [INIKEY], multiline: true },
    html: { cm: [TAGCM], str: [DQ, SQ], extra: [String.raw`</?[\w-]+`] },
    xml: { cm: [TAGCM], str: [DQ, SQ], extra: [String.raw`</?[\w-]+`] },
    sum: { cm: [], str: [], extra: [] },
};

const BY_NAME = {
    dockerfile: "sh", makefile: "sh", ".env": "env", ".gitignore": "conf",
    "docker-compose.yml": "yaml", "go.mod": "sum", "go.sum": "sum",
};

export function langOf(name) {
    const clean = String(name || "").split("/").pop().split("?")[0];
    const low = clean.toLowerCase();
    if (BY_NAME[low]) return BY_NAME[low];
    const dot = low.lastIndexOf(".");
    if (dot <= 0) return "";
    const ext = low.slice(dot + 1);
    return LANGS[ext] ? ext : "";
}

const built = new Map();

function ruleOf(lang) {
    if (built.has(lang)) return built.get(lang);
    const spec = LANGS[lang];
    if (!spec) return null;
    const kw = spec.kw ? [`\\b(?:${spec.kw.split(" ").join("|")})\\b`] : [];
    const words = kw.concat(spec.extra || []);
    const groups = [
        spec.cm.length ? spec.cm.join("|") : null,
        spec.str.length ? spec.str.join("|") : null,
        words.length ? words.join("|") : null,
        String.raw`\b\d[\d_]*(?:\.\d+)?(?:[eE][+-]?\d+)?\b`,
    ];
    const source = groups.map((part) => `(${part || "(?!)"})`).join("|");
    const flags = `g${spec.multiline ? "m" : ""}${spec.nocase ? "i" : ""}`;
    const rule = new RegExp(source, flags);
    built.set(lang, rule);
    return rule;
}

const CLASS = ["cdcm", "cdstr", "cdkw", "cdnum"];

export function highlight(text, name) {
    const body = String(text == null ? "" : text);
    const lang = langOf(name);
    const rule = lang && body.length <= MAX_PAINT ? ruleOf(lang) : null;
    if (!rule) return body;
    rule.lastIndex = 0;
    const out = [];
    let last = 0;
    for (const m of body.matchAll(rule)) {
        if (m.index > last) out.push(body.slice(last, m.index));
        const which = CLASS.findIndex((_, i) => m[i + 1] !== undefined);
        out.push(html`<span class=${CLASS[which] || "cdnum"}>${m[0]}</span>`);
        last = m.index + m[0].length;
    }
    if (!out.length) return body;
    if (last < body.length) out.push(body.slice(last));
    return out;
}
