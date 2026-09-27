// A light highlighter for the code blocks of the feed: sh, js, go, python,
// yaml, json and diff. It is regular expressions and nothing else — no
// grammar, no nesting — which is what a chat needs: the eye finds the
// strings, the comments and the words of the language, and a wrong colour
// on an odd line costs nothing.
//
// What it makes is a list of pieces, never markup: a piece is plain text or
// a pair of a class and the text it colours, and the caller builds nodes of
// them. The feed calls it for a block that has come near the screen, and it
// keeps what it made: a block scrolled past twice is coloured once.

const words = (list) => new RegExp(`\\b(?:${list.split(" ").join("|")})\\b`, "y");

const STR_DQ = /"(?:[^"\\\n]|\\.)*"?/y;
const STR_SQ = /'(?:[^'\\\n]|\\.)*'?/y;
const STR_BT = /`(?:[^`\\]|\\.)*`?/y;
const NUM = /\b(?:0x[\da-fA-F_]+|\d[\d_]*(?:\.\d+)?(?:e[+-]?\d+)?)\b/y;
const SPACE = /\s+/y;
const WORD = /[A-Za-z_$][\w$]*/y;

// A rule is [pattern, class]; the first pattern that matches at the cursor
// wins. Patterns are sticky, so a scan is one pass over the text.
const RULES = {
    js: [
        [/\/\/[^\n]*/y, "com"], [/\/\*[\s\S]*?(?:\*\/|$)/y, "com"],
        [STR_DQ, "str"], [STR_SQ, "str"], [STR_BT, "str"],
        [words("true false null undefined NaN Infinity this"), "lit"],
        [words("const let var function return if else for while do switch case break continue new class extends import from export default async await try catch finally throw typeof instanceof in of yield delete void"), "kw"],
        [NUM, "num"],
        [/[A-Za-z_$][\w$]*(?=\s*\()/y, "fn"],
    ],
    go: [
        [/\/\/[^\n]*/y, "com"], [/\/\*[\s\S]*?(?:\*\/|$)/y, "com"],
        [STR_DQ, "str"], [STR_BT, "str"], [STR_SQ, "str"],
        [words("true false nil iota"), "lit"],
        [words("break case chan const continue default defer else fallthrough for func go goto if import interface map package range return select struct switch type var"), "kw"],
        [words("string int int32 int64 uint uint8 uint32 uint64 bool byte rune error float32 float64 any"), "typ"],
        [NUM, "num"],
        [/[A-Za-z_][\w]*(?=\()/y, "fn"],
    ],
    python: [
        [/#[^\n]*/y, "com"],
        [/[rbfu]{0,2}("""|''')[\s\S]*?(?:\1|$)/iy, "str"],
        [/[rbfu]{0,2}"(?:[^"\\\n]|\\.)*"?/iy, "str"], [/[rbfu]{0,2}'(?:[^'\\\n]|\\.)*'?/iy, "str"],
        [words("True False None self"), "lit"],
        [words("def class return if elif else for while in not and or is import from as with try except finally raise lambda yield pass break continue global nonlocal async await assert del"), "kw"],
        [NUM, "num"],
        [/@[\w.]+/y, "fn"],
        [/[A-Za-z_][\w]*(?=\()/y, "fn"],
    ],
    sh: [
        [/(?:^|(?<=\s))#[^\n]*/my, "com"],
        [STR_DQ, "str"], [STR_SQ, "str"],
        [/\$\{[^}\n]*\}?|\$\(|\$[A-Za-z_]\w*|\$[#?@*0-9!$-]/y, "var"],
        [/(?<=^[ \t]*|[;&|][ \t]*|\$\([ \t]*)(?:sudo[ \t]+)?[\w./~@+-]+/my, "cmd"],
        [/(?<=\s)--?[\w][\w-]*(?:=)?/y, "flag"],
        [/&&|\|\||[|;]|>>?|<</y, "op"],
        [NUM, "num"],
    ],
    yaml: [
        [/#[^\n]*/y, "com"],
        [/(?<=^[ \t]*(?:-[ \t]+)?)[\w./-]+(?=[ \t]*:(?:\s|$))/my, "key"],
        [STR_DQ, "str"], [STR_SQ, "str"],
        [words("true false null yes no on off"), "lit"],
        [/[&*][\w-]+/y, "var"],
        [NUM, "num"],
    ],
    json: [
        [/"(?:[^"\\\n]|\\.)*"(?=\s*:)/y, "key"],
        [STR_DQ, "str"],
        [words("true false null"), "lit"],
        [/-?\b\d+(?:\.\d+)?(?:e[+-]?\d+)?\b/y, "num"],
    ],
};

const ALIAS = { javascript: "js", mjs: "js", ts: "js", typescript: "js", jsx: "js", tsx: "js",
    golang: "go", py: "python", python3: "python", bash: "sh", shell: "sh", zsh: "sh", console: "sh",
    yml: "yaml", jsonc: "json", patch: "diff" };

// A block with no language says what it is by its first line often enough
// to guess three cases; anything else stays plain rather than coloured wrong.
const SH_FIRST = /^\s*(?:\$\s+)?(?:sudo\s+)?(?:cd|git|go|make|docker|systemctl|journalctl|curl|ssh|scp|ls|cat|grep|sed|awk|export|mv|cp|rm|mkdir|tar|python3?|node|npm|pnpm|bash|sh|chmod|chown|kill|pkill|tail|head|find|echo|source|\.\/|~\/|\/usr\/|\/opt\/)\b/;

export function langOf(lang, text) {
    const l = String(lang || "").trim().toLowerCase();
    if (l) {
        const name = ALIAS[l] || l;
        return RULES[name] || name === "diff" ? name : "";
    }
    const t = String(text || "");
    if (/^(?:@@|diff --git|--- |\+\+\+ )/m.test(t) && /^[+-]/m.test(t)) return "diff";
    if (/^\s*[[{]/.test(t)) {
        try { JSON.parse(t); return "json"; } catch { /* not json */ }
    }
    if (SH_FIRST.test(t.split("\n", 1)[0])) return "sh";
    return "";
}

// scan walks the text once: at every place the first rule that matches wins,
// a word no rule wants is taken whole, so a keyword is never found inside a
// longer name, and runs of plain text stay one piece.
function scan(text, rules) {
    const out = [];
    let plain = "";
    let i = 0;
    const flush = () => {
        if (plain) out.push(plain);
        plain = "";
    };
    while (i < text.length) {
        SPACE.lastIndex = i;
        const sp = SPACE.exec(text);
        if (sp) {
            plain += sp[0];
            i += sp[0].length;
            continue;
        }
        let hit = null;
        for (const [re, cls] of rules) {
            re.lastIndex = i;
            const m = re.exec(text);
            if (m && m[0].length) {
                hit = [cls, m[0]];
                break;
            }
        }
        if (!hit) {
            WORD.lastIndex = i;
            const w = WORD.exec(text);
            const step = w ? w[0].length : 1;
            plain += text.slice(i, i + step);
            i += step;
            continue;
        }
        flush();
        out.push(hit);
        i += hit[1].length;
    }
    flush();
    return out;
}

// diff colours a line by its first character: what came, what went, where.
function diff(text) {
    const out = [];
    text.split("\n").forEach((line, n) => {
        if (n) out.push("\n");
        if (!line) return;
        if (/^(?:\+\+\+|---|diff |index )/.test(line)) out.push(["meta", line]);
        else if (line.startsWith("@@")) out.push(["hunk", line]);
        else if (line.startsWith("+")) out.push(["add", line]);
        else if (line.startsWith("-")) out.push(["del", line]);
        else out.push(line);
    });
    return out;
}

const CACHE = new Map();
const CAP = 400;

// stats is what the highlighter has done since the page opened: the blocks
// it coloured, the ones it served from what it kept, and the time it took.
export const stats = { runs: 0, hits: 0, ms: 0, chars: 0 };

const keyOf = (name, text) => `${name}\u0000${text}`;

// highlight returns the pieces of the block, or null when the language is not
// one it knows. The result is kept by language and text.
export function highlight(lang, text) {
    const name = langOf(lang, text);
    if (!name) return null;
    const key = keyOf(name, text);
    const was = CACHE.get(key);
    if (was !== undefined) {
        stats.hits += 1;
        return was;
    }
    const t0 = performance.now();
    const out = name === "diff" ? diff(text) : scan(text, RULES[name]);
    stats.ms += performance.now() - t0;
    stats.runs += 1;
    stats.chars += text.length;
    if (CACHE.size >= CAP) CACHE.delete(CACHE.keys().next().value);
    CACHE.set(key, out);
    return out;
}

// kept returns what was made for the block before, without making anything.
export function kept(lang, text) {
    const name = langOf(lang, text);
    return (name && CACHE.get(keyOf(name, text))) || null;
}

// forget drops what was kept and the counts: a fixture starts from nothing.
export function forget() {
    CACHE.clear();
    Object.assign(stats, { runs: 0, hits: 0, ms: 0, chars: 0 });
}
