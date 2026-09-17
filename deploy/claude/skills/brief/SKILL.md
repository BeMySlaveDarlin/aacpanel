---
name: brief
description: "Publishes a brief to the panel: a long piece the person reads and walks through — several questions with what each rests on, the options and their cost, and a place to answer. Use when what you have to ask does not fit a question in the console: more than four questions, more than four options, an option that needs a paragraph to explain, or facts the choice depends on. Also for a finished piece of reading with no question in it at all — an analysis, a report, a set of findings — instead of publishing an artifact the person would have to open in another account. Triggers: \"send me a brief\", \"put the questions in the panel\", \"I will answer later\", \"write it up for me to read\" — or, in Russian, \"опросник\", \"бриф\", \"разбор с вопросами\", \"скинь в панель\", \"отвечу потом\"."
user-invocable: true
argument-hint: "<path to the document, .yaml or .json>"
---

A brief is a document the person walks through in the panel. Write it to a file,
publish it, and let the turn end.

```sh
<repo>/deploy/claude/brief.py briefs/seven-questions.yaml
```

The answers come back **later, as a message in this session** — minutes or hours
from now, whenever they finish. Nothing waits for them here.

## When it is a brief and when it is a question

`AskUserQuestion` stops the turn and is answered in seconds. It holds at most
four questions, two to four options each, and a twelve-character header. Answers
picked from the panel are pressed into the console dialog as keystrokes, and
that dialog fits nine items.

Take a brief when any of that is in the way:

- **more than four questions**, or more than four options in one;
- an option that **needs a paragraph** to say what it costs;
- the choice **rests on facts** the person has to see first, with their sources;
- the answer is **not due now**: they will read it on a phone, in the evening,
  in several sittings;
- there is **nothing to ask at all** and the piece is simply worth reading.

Take a question, not a brief, when the work stops until they answer. A brief
that blocks the work is a brief nobody asked for.

## What goes in the document

```yaml
id: seven-questions           # lowercase letters, digits, dashes; a reissue under
                              # the same id updates the document in place
title: Seven questions after twelve
eyebrow: after twelve answers # a line of context above the title
lede: |
  What this is and why it exists. Inline **bold**, `code` and *italics* work.
summary:                      # up to four counters across the top
  - {n: "3", label: still open}
lineage:                      # where each question came from
  - {from: did not close, to: "**07** the configuration directory"}
sections:                     # blocks that ask nothing
  - {title: Where this came from, body: The tail of yesterday.}
questions:
  - id: r1
    chips: [{tone: stuck, text: "did not close · decision 07"}]
    title: The configuration directory
    ask: You picked one directory. **The CLI may not offer that.**
    facts:                    # what the choice rests on, with sources
      - {text: "`--setting-sources user` reads the directory whole", src: stack 5}
      - {text: "`--restricted` ignores user settings", src: help 2.1.270, flag: true}
    kind: pick                # pick · multi · text · none
    options:
      - {key: A, label: Curate through `--settings`, note: argv carries the role, not the directory}
      - {key: B, label: A directory per role, note: a copy of the token in each}
    read:                     # your reading, marked on screen as a proposal
      - Option A is what you described, with argv as the carrier.
    capture: {note: {placeholder: The fallback}}
    answered: {pick: C}       # what you already know was decided
closing:
  - What closed for good, and where the analysis ends and a guess begins.
```

`kind` decides what the question takes: `pick` one option, `multi` several,
`text` only words, `none` nothing at all. A `pick` or `multi` question without
options is refused, and so is a `text` or `none` one carrying them.

Every question also takes a free note, and the person can skip any of them.

## What makes a brief worth reading

The half that is not questions is the half that earns it:

- **`facts` carry their source.** `stack 5`, `admin-role.md:61`, `checked`,
  `follows from`. A claim with nowhere to check it is a claim the person has to
  take on trust, and they will not.
- **`read` is marked as yours.** The screen prints it under "my reading — a
  proposal, not a fact". Keep it that way: say what you would pick and why, and
  do not dress a guess as a finding.
- **`options` say what each one costs**, not what it is called. "A directory per
  role" is a name; "a copy of the subscription token in each" is a reason to
  choose or refuse.
- **`flag: true`** marks the fact that changes the answer. Two or three in a
  document, not half of them.

## The rules the panel keeps

- **The id names the document, not the conversation.** Publishing the same id
  from the same project updates it and keeps the answers already given.
  From another project it is refused rather than overwritten.
- **Ceilings**: 100 questions, 8 options and 16 facts each, 4 MB for the whole
  document. Long text is cut rather than refused; a broken structure is refused
  with the reason.
- **A brief belongs to the directory of the session, not of the shell.** The
  shelf asks where the session itself works, so a `cd` before the script — into
  this repository, say — changes nothing: the document still shows up in the
  conversation of the project the session is working on, and only that session
  may take it off the shelf.
- **A brief outlives its session.** It is kept for a month, and the answers
  reach the session even after it has been restarted.
- **Nothing you write is markup.** Inline markdown is turned into nodes by the
  panel; html in a field arrives as the characters you typed.

## Taking one off the shelf

```sh
<repo>/deploy/claude/brief.py --delete seven-questions
```

A session removes only the briefs of the directory it works in: the document
belongs to the work it was written for, and the conversation carrying that work
on is the one that may put it down. A brief from elsewhere is refused with the
directory it belongs to. The answers to the document go with it.

Remove one when the person asks. A brief nobody answered is swept on its own
after a month.

## What comes back

`OK` with the id means the panel has it. `STOP` with a reason means nothing was
published — the collector is not listening, the id belongs to another project,
or the document is malformed. Say the reason in the conversation; a refused
brief that goes unmentioned leaves them waiting for a document that is not there.

Use `--check` to read the document and count what is in it without publishing.
