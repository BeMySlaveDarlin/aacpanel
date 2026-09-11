# Fonts

Vendored on purpose: an external domain breaks the offline mode the service
worker was written for, and adds a host to the supply chain — the same reasoning
by which there is no npm in the project.

| File | Typeface | Source | Licence |
|---|---|---|---|
| `inter-400-*.woff2`, `inter-600-*.woff2` | Inter, 400 and 600–800 | Google Fonts | SIL OFL 1.1 (`OFL-Inter.txt`) |
| `jetbrains-mono-400-*.woff2` | JetBrains Mono, 400 | Google Fonts | SIL OFL 1.1 (`OFL-JetBrainsMono.txt`) |

The typeface was chosen separately; Manrope, IBM Plex Mono and Source Serif 4
left along with the showcase. **This directory is not a dump:** `webbuild` puts
**every** `.woff2` from here into the service worker's precache, so a file no
token of `app.css` reaches is still downloaded to the phone on every install of
the application.

The files are Google Fonts subsets: `latin` and `cyrillic` separately, hooked up
with `unicode-range`. All four weigh 130 KB.

What the subsets do **not** cover: `→` (U+2192), `↔` (U+2194), `▾` (U+25BE),
`⋮` (U+22EE). Those four characters are drawn by the system font from the
fallback stack — deliberately: pulling in another subset for their sake costs
more than the difference in the shape of an arrow.

Updating: take fresh subsets with the same request to `fonts.googleapis.com/css2`
under the `User-Agent` of a current Chrome (otherwise `ttf` comes back instead of
`woff2`), replace the files and fix this table.

## JetBrains Mono — the terminal font

The system monospace cuts the eye, and `ui-monospace` is different everywhere on
top of that: one typeface on the desktop, another on the phone, which means the
terminal looked different depending on where it was seen from.

Only the 400 weight was taken, and only latin with cyrillic: the terminal does
not embolden text, and what claude writes into it is not always latin. Twenty
kilobytes for both files — a tenth of Inter, and this is the case where the
weight in the precache pays for itself: the font works on every screen with code
on it, not on one.
