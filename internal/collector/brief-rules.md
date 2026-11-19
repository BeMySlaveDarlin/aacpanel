# A brief is a task and its decisions

Use `brief_publish` when the person wants a brief, a questionnaire, or choices
for later. A brief always asks for decisions about one task. It can wait while
independent work continues and outlives the conversation that wrote it.

Use `ask` for a question while work continues. When work stops until the person
answers, use AskUserQuestion. Long reading without decisions goes through
`file_send`; credentials go through `secret_ask`. `notify` calls the person and
carries no answer.

Call `brief_publish` without `doc` before the first publication and read these
rules and the machine's guide. Then pass `doc` as an object. `check: true` beside
`doc` counts decisions and options without publishing; the collector validates
the full document when it is published. A refusal means nothing was published:
say why in the conversation.

## The document

This YAML shows the shape; the tool accepts the corresponding JSON object:

```yaml
id: rollout-order
title: Prepare the rollout
task: Choose how to deploy the release without losing incoming work.
why: The release is ready and the service must stay available during deployment.
context:
  - title: Dependency
    text: The new code needs the database change before it starts.
decisions:
  - id: order
    title: What goes first
    ask: Which deployment order should we use?
    multi: false
    options:
      - key: A
        label: Apply the database change first
        reason: Both versions can use the changed database.
        cost: The old service remains running for one rebuild.
        hours: 0.5
        recommended: true
      - key: B
        label: Stop the service and deploy both together
        reason: There is only one version to check.
        cost: Incoming work waits until the rebuild finishes.
        hours: 0.3
        recommended: false
```

`id` names the brief: lowercase Latin letters, digits and dashes, at most 64
characters. `title` is required and at most 120 characters. `task` is required,
at most 400 characters: name what needs doing and how the person can tell it is
done. `why` is required, at most 800 characters: say why it matters now and what
happens if it waits. Do not repeat the title in longer words.

`context` is optional supporting reading, folded on the screen: up to six blocks
with `title` (80 characters) and markdown `text` (2000 characters). Keep the
material needed to choose in the decision itself.

Each decision has a `title` (120 characters) and an optional markdown `ask`
(600 characters) saying what is being decided and what follows from it. Its `id`
is optional, at most 40 characters, unique in the brief; an absent id becomes
its number starting at 1. `multi` defaults to false. Set it to true only when
several options may be chosen together. Each decision offers two to eight
options with unique keys. A missing key becomes A, B, and so on; a key is at
most four characters.

Every option requires:

- `label` (120 characters): the concrete choice;
- `reason` (400 characters): why someone would choose it;
- `cost` (300 characters): what it gives up, leaves waiting, or makes harder;
- `hours`: a number from 0.1 to 1000, the estimated agent hours to do the work.
  Agent hours measure work by an agent, not a promise about the person's clock.

Mark `recommended: true` on exactly one option for a single choice, and at least
one for multiple choice. The mark lets the person see your judgement before
reading every alternative and accept the recommendation in one press. Give its
reason in the option; the mark alone is not an argument.

Write plain sentences about consequences for people, time and money. Explain
unavoidable jargon where it first appears. A title names the problem, and an
option names an action. Avoid filler, chains of nouns and code identifiers that
the person cannot act on. Before publishing, read each decision with all its
options and check that every option has a reason, cost and agent hours.

The whole publication is at most 4 MB. Text is cut at its field's ceiling;
broken structure is refused. The document carries 1–100 decisions. HTML in a
field is text; the panel renders markdown itself.

## Answers and a closed brief

The person may decide some choices and leave the rest unanswered. At least one
choice is needed to send. Accept recommended fills only undecided choices and
keeps choices already made. Each decision has an optional note; the task has
one shared note. The server sends all decisions as one message, marking the
brief sent with the same operation. Unanswered decisions say `no answer`.

The same id in the same project updates the document and keeps its answers.
A brief whose answers were sent is closed: a reissue does not open it again.
New decisions after sending need a new brief under a new id. Do not overwrite
a closed document to ask again. Answers arrive as a message when the person
sends them: continue independent work or end the turn, never poll for them.

A brief belongs to the directory its session works in. Its answers reach that
session or a live session carrying on work in the same directory. The document
stays on the host for a month even if its author ends.

## Taking a brief off the shelf

Use `brief_delete` with the id when the person asks to remove it. A session can
remove only briefs of the directory it works in. The answers go with the
document; publishing a removed id starts a new publication with no answers.
An unanswered brief is swept after a month.

The response starts with `Published as <id>` when the panel has the document.
If the collector refuses it or is unavailable, report the reason: the person
must not wait for a brief that was never published.
