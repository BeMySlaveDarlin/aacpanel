---
name: notify
description: "Calls the person to this session by pushing a line to their phone through the panel. Use when the work is genuinely blocked on them and they are not at the screen: \"call me\", \"ping me\", \"tell me when you need me\", \"notify me\", \"push me\" — or, in Russian, \"позови меня\", \"пингани\", \"напиши мне\", \"скинь уведомление\", \"дай знать на телефон\". Also on your own judgement when a session stalls on something only they can decide and the answer is worth their attention now. NOT a question and NOT a permission request: nothing appears in the console, nothing waits for an answer, the line just arrives on the phone."
user-invocable: true
argument-hint: "<what to tell them, in one line>"
---

Call the person. One command, nothing to wait for.

```sh
<repo>/deploy/claude/notify.py "stuck on the migration, need you"
```

The line arrives on their phone as a push. A tap on it opens this session in the
panel. Nothing appears in this console, nothing asks for permission, and nothing
blocks: the command returns, and the turn goes on or ends as it would have.

## This is not a question

A question is `AskUserQuestion`: it stops the turn and waits for an answer. A
permission request stops the turn and waits for a yes. A call does neither. It
says one thing to a person who is not looking at the screen, and that is all it
can do.

So the line has to stand on its own. The person reads it on a phone, in a queue,
between two other things, with no conversation around it. `"need you"` tells them
nothing; `"stuck on the migration: the applied file's checksum does not match and
I will not edit it without you"` tells them whether to get up.

## When it is worth a call

- The work is **blocked on a decision only they can make**, and waiting silently
  wastes the rest of the day.
- Something **broke in a way they would want to know about now** — a deploy that
  went wrong, a check that went red on the main branch.
- They **asked to be called** when a long job finished.

## When it is not

- **Instead of finishing.** A call is not a way to hand back work that is merely
  hard. Get as far as honest work goes first, then call with what is left.
- **For progress.** "Done with the first half" is for the conversation, not for
  a phone.
- **Because a question would be blocked.** If the thing you need is an answer,
  ask it properly and let the turn wait. A call is for when nobody is there.

## The rules the panel keeps

- **One call a minute from a session.** A second one inside that minute is
  refused and says how long is left. Collect what you have to say into one line
  rather than calling twice.
- **Three hundred characters**, the rest is cut. It is a push, not a letter.
- **The phone that already has this session open gets nothing.** The panel knows
  which device is looking and stays quiet there, so a call costs nothing when
  they are already with you.
- **A call nobody came for goes stale in a few minutes.** It is not a queue and
  it is not a log: say it again later if it still matters.

## What comes back

`OK` means the panel took it. `STOP` with a reason means nobody was called — the
collector is not listening, the minute has not passed, or the line was empty.
Read the reason and say it in the conversation instead; a refused call that goes
unmentioned leaves them waiting for a push that never comes.

Everything written in the line goes to a device outside this machine. No keys,
no tokens, no contents of a private file.
