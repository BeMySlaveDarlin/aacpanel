/*
 * Scoring ported from fzy (John Hawthorn), MIT, with Unicode word boundaries.
 * Source: https://github.com/jhawthorn/fzy/tree/34b88869d022e861da4846c4463aea3ddfb3ff30
 * Original SHA-256:
 * src/match.c: 59e2164938037fe9471235d5537a8ae2360f6ba406a3198f1e69549f3bf43e56
 * src/config.def.h: 7823ace3f3b41ab552f1ee4e752782bf9f13ce1d1b018ec56b57d8d2ca1d5385
 * src/bonus.h: e6f1e5e6c7742bd4fdd8a8cffa5647b59f3e85960d10ee01b250493aec273abc
 *
 * Copyright (c) 2014 John Hawthorn
 *
 * Permission is hereby granted, free of charge, to any person obtaining a copy
 * of this software and associated documentation files (the "Software"), to deal
 * in the Software without restriction, including without limitation the rights
 * to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
 * copies of the Software, and to permit persons to whom the Software is
 * furnished to do so, subject to the following conditions:
 *
 * The above copyright notice and this permission notice shall be included in
 * all copies or substantial portions of the Software.
 *
 * THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
 * IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
 * FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
 * AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
 * LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
 * OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
 * THE SOFTWARE.
 */

const SCORE_GAP_LEADING = -0.005;
const SCORE_GAP_TRAILING = -0.005;
const SCORE_GAP_INNER = -0.01;
const SCORE_MATCH_CONSECUTIVE = 1.0;
const SCORE_MATCH_SLASH = 0.9;
const SCORE_MATCH_WORD = 0.8;
const SCORE_MATCH_CAPITAL = 0.7;
const SCORE_MATCH_DOT = 0.6;

export function fuzzyScore(query, label) {
    if (!query) return 0;
    const needle = Array.from(query.toLowerCase());
    const haystack = Array.from(label.toLowerCase());
    if (needle.length > haystack.length) return null;
    if (needle.join("") === haystack.join("")) return Infinity;

    const bonuses = [];
    let previous = "/";
    for (const char of label) {
        let bonus = 0;
        if (/[\p{L}\p{N}]/u.test(char)) {
            if (previous === "/") bonus = SCORE_MATCH_SLASH;
            else if (previous === "-" || previous === "_" || previous === " ") bonus = SCORE_MATCH_WORD;
            else if (previous === ".") bonus = SCORE_MATCH_DOT;
            else if (/\p{Ll}/u.test(previous) && /\p{Lu}/u.test(char)) bonus = SCORE_MATCH_CAPITAL;
        }
        // Lowercasing can expand a code point; only its first letter starts a word.
        for (const [i] of Array.from(char.toLowerCase()).entries()) bonuses.push(i === 0 ? bonus : 0);
        previous = char;
    }

    // D ends in a match; M may end in a gap. Only the preceding row is needed.
    const D = new Float64Array(haystack.length).fill(-Infinity);
    const M = new Float64Array(haystack.length).fill(-Infinity);
    for (let i = 0; i < needle.length; i += 1) {
        let best = -Infinity;
        let previousD = -Infinity;
        let previousM = -Infinity;
        const gap = i === needle.length - 1 ? SCORE_GAP_TRAILING : SCORE_GAP_INNER;
        for (let j = 0; j < haystack.length; j += 1) {
            let score = -Infinity;
            if (needle[i] === haystack[j]) {
                score = i === 0 ? j * SCORE_GAP_LEADING + bonuses[j]
                    : Math.max(previousM + bonuses[j], previousD + SCORE_MATCH_CONSECUTIVE);
            }
            previousD = D[j];
            previousM = M[j];
            D[j] = score;
            M[j] = best = Math.max(score, best + gap);
        }
    }
    const score = M[haystack.length - 1];
    return score === -Infinity ? null : score;
}
