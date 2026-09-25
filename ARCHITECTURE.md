# Architecture

What the panel is made of, who talks to whom, why the parts are split exactly
this way and what the panel deliberately does not have.

---

## The main constraint

The panel faces the internet and runs the machine. These two things do not live
in one process:

> **The web service has no rights on the host and will not get any.** It reaches
> docker through a proxy that passes reads only, and everything
> that changes the state of the machine it asks a separate process for, from a
> closed list of actions.

Everything else follows from that. Between the service and `docker.sock` stands
a socket-proxy with `POST` switched off — starting or removing a container this
way is physically impossible. The claude transcripts do not reach the container
at all: another process reads them and hands out only numbers. The service passes
the executor **a structure, not a string for a shell**, and even full capture of
the container yields exactly the list of actions written down in
`internal/action/action.go`.

---

## Three processes

| Process | Where | Rights | Job |
|---|---|---|---|
| `aacpanel` | container, distroless, under the owner's uid | nothing on the host; docker read-only | HTTP, API, streams, rules, pushes, database |
| `aacpanel-agent` | host, system unit | reading only, no graphics and no docker | metrics and claude sessions → snapshot |
| `aacpanel-exec` | host, user unit | `docker.sock`, the graphical session; transcripts closed off | carrying out actions |

The sets of rights of the collector and the executor are **opposite**, and that
is the reason there are two of them and not one.

The collector needs claude transcripts — the whole conversation with the model
is in there, keys, code, everything at once. That is why its unit is cut down:
`ProtectSystem=strict`, `ProtectHome=read-only`, it may write only into its own
state directory, and it has no graphics and no docker at all.

The executor needs exactly the opposite: `docker.sock`, the session bus, the
display — otherwise it will not open a terminal window. Transcripts it does not
need, and they are closed to it explicitly (`InaccessiblePaths` on the claude
projects directory). Folding this into one process would mean giving the reader
of the conversation the right to run programs.

The executor's unit is a user unit and not a system one for the same reason: a
system unit gets neither the address of the session bus, nor `XDG_RUNTIME_DIR`,
nor any link to the graphical shell.

---

## Reading a repository

Git runs in the collector and nowhere else. The service has no rights on the
host, and that is not suspended for the sake of a diff: reading a repository is
reading, which is the collector's half of the split above. What comes back is
raw — the list of changed files, the listing of one directory, one blob, the
diff of one file, the trailer of one commit. Cutting a diff into hunks and
colouring it happens in the service, where a mistake costs a redraw rather than
a shell command on the host.

**One list of changes.** What the commits of a branch did, what the working tree
has done since, and what git does not know about at all arrive together. The
tree on a screen and the run of diffs under it are the same set of files:
counted twice they come apart, and the file a person taps is not the file they
were shown. A file that is both in a commit and changed again since is marked as
not yet committed — that is the half still unanswered for.

**A revision travels with every window.** It holds the head, the merge base and
the ids of the tracked files. A repository under a working session moves between
two requests, and a window answered from the new state while the list came from
the old one is a diff nobody can trust: it comes back stale, with the revision
to ask again under.

**Where a branch came from is named, not guessed.** Git keeps no record of it.
The order is the project's own setting, then the upstream of the branch, then
the reflog, then the trunk — and which of them answered is said on the screen,
because none of them is right every time.

**Colour travels as spans, not as html.** The service colours a file and sends a
run length and a class number per span: the same file as html is six to ten
times its own size on the wire. The colouring is keyed by the id of the blob it
came from — an id changes with the content and with nothing else, so an entry
never goes stale and nothing is invalidated by hand.

**Git is asked in one language.** Its output is read, not shown: under the
language of the host the messages arrive translated, and a reply parsed by its
words comes apart.

**The files do not need git.** A directory that is not a repository is a state
of the screen, not a failure — a project can be a shelf of notes. Its tree, its
files and a search by name are read off the disk: a file carries the id git
would give its content, counted by the collector, so its colouring is kept
under the same key either way. A search walks the disk breadth first under a
ceiling of names and of seconds, skips the directories tools fill, and says
when it stopped short — typing more of a name narrows what was found, not what
was walked. What belongs to a branch — the changes, a diff, the base, a commit,
the notes — answers that there is no repository, and the screen leaves it out
rather than drawing it empty.

---

## Who talks to whom

```
              ┌──────────────────────────────────────────────┐
  browser ───►│ aacpanel (container)                         │
   https      │  · pages and bundle (embed)                  │
              │  · /api/*, SSE                               │
              │  · alert rules, pushes, history              │
              └───┬───────┬──────────┬──────────┬────────────┘
                  │       │          │          │
        compose   │       │ volume   │ socket   │ socket
        network   │       │ :ro      │          │
                  ▼       ▼          ▼          ▼
          socket-proxy  snapshot  chat.sock  sock / term.sock
           (GET-only)  state.json usage.sock
                  │       ▲          ▲          ▲
                  ▼       │          │          │
            docker.sock   └── aacpanel-agent    └── aacpanel-exec
                                 (host)                (host)
                                    │                     │
                            /proc, claude           docker, tmux, claude,
                            transcripts, ask.sock   the terminal window
```

**The snapshot.** The collector writes the state of the machine as a file into
the state directory; that directory is mounted into the service read-only.
Everything else that only the host knows goes through it as well: the socket of
the conversation feed, the socket of token usage collection.

**The action socket.** `0600` owned by the session owner — behind it is the
whole list of actions on the host, and its permissions must not be loosened.
That is why the service in the container runs under the same uid (`user:` in
compose). What is mounted is the **directory**, not the socket itself: a
bind-mount of a file is tied to the inode, and a restart of the executor would
silently break the link until the container was recreated.

**The question socket.** The claude hook reports a session's question through
`ask.sock`, and that one lives in the collector's separate runtime directory,
not in the state directory: the latter is mounted into the service, and "a
session's question" would become data the panel accepts from whatever sticks out
into the internet.

**The brief socket.** A session can publish a brief: a long piece with
questions the person walks through, kept whole on the host beside the question
book. It takes the same road as a question and for the same reason — data from
a session is taken by the host, never by the service that faces the internet —
but it is a different kind of thing, and the differences are what shape it.

A question stops the turn and is answered in seconds; a brief is read for as
long as it takes and **outlives the conversation that wrote it**. So it is swept
by age and by count, never by whether its session is still alive: a sweep on a
dead session would take the document out from under the person reading it. It
is kept one file per brief, because a brief runs to tens of kilobytes and the
screen opens one at a time.

**The document never enters the database.** What the panel keeps is the part a
person typed into it — the picks, the notes, the mark that it was sent. The
brief itself carries whatever the session was talking about, and that belongs
on the host with the transcripts, not in the store of a service exposed to the
internet.

**The answers travel back as an ordinary message.** The text is built by the
service, so that what the person reads before sending and what the session
receives are one text; it goes into the session through `session.send`, the
action that already exists. No new right, no new kind of action, and a brief
answered after its session was restarted still reaches it.

**The shelf of readings.** A review of a branch travels the other way: the
person writes notes on lines of code, and the session is handed them. The notes
are kept in the database while they are being written, the way the answers of a
brief are, and go out as a file on a shelf of the host — a file can grow a field
without breaking whoever already reads it, and a session finds a path in its
composer instead of a wall of quotes.

Writing the file is the one operation of the viewer that writes at all, so the
directory is fixed in the code and the name of the file is made by the panel
from the name of the reading; a name that is not a plain one is refused rather
than repaired into something that lands elsewhere. It has a socket of its own
because a reading is up to two hundred notes with the lines they stand on, and
the socket of the conversation feed answers a request that fits in one read.

The session is told the same way a brief is answered — through `session.send`,
through the queue of a busy composer — and the reading is written down as gone
only once that has happened. Told before the file exists, a session would open a
path to nothing; settled before the signal, a reading that never left would read
as delivered.

Every note carries the text of its line. A branch moves while it is being read,
so a number alone stops meaning anything the moment somebody commits above it:
the text identifies the line, the number says where to look first, and three
lines is as far as it looks. Further than that is different code wearing the
same words, and such a note is set apart as outdated rather than dropped onto a
stranger.

**Nothing the session writes is markup.** Every field of the document is text
carrying inline markdown at most, and the panel turns that into nodes on its
own side. A brief is written by a model and read in an application that runs
the machine; html from there would be a hole with an author.

**The page socket.** A session publishes an artifact into the account it works
under. The person reading the panel is signed into one account at a time, and a
machine that serves several contours publishes under several: the address on
the card then opens for whoever happens to match and refuses everyone else. So
the page is copied as it goes out, on a socket of its own beside the brief, and
the panel shows the copy.

The copy is the page and nothing else. Its supporting files and its uploaded
assets stay where they were published: a page built from several files shows
here without them. It is swept by age, by count and by the room the copies take
together, and it never enters the database — it is somebody's document, and it
belongs on the host with the transcripts.

**A kept page is foreign code, and it runs.** A page that draws nothing is not
the page anybody published, so the frame it opens in may run scripts. What it
may not have is the origin of the panel: without that the cookie is not the
page's to read, and the route serving the copy repeats the sandbox in a header
of its own, so the rule holds even when the address is opened outside any
frame. This is the one place in the panel where foreign markup runs at all.
A file of the project, inlined into a frame with the document itself, inherits
the origin of the page it sits in and therefore stays without scripts.

**The call socket.** A session can also call the person to it: one line through
`notify.sock`, in the same runtime directory and for the same reason, which the
panel carries to their phone as a push that opens that session. It is not a
question and not a request for a permission — nothing waits for an answer, and
the turn goes on. What keeps it from becoming a bell nobody hears is the
collector, which takes one call a minute from a session and cuts the line to a
length a phone shows; a call the panel has not carried away within a few minutes
goes stale, because a call is for now and a log of them is not worth keeping.

---

## Sessions on the stream

A session is kept one of two ways, chosen by the launch parameters of its
project. In **tmux** it is a terminal: text goes in as keystrokes, and a
question or a permission is read off the screen and answered with keys. On
the **stream** it is `claude -p` with stream-json on both pipes: a message is
a line of JSON, a question and a permission are requests with an id, and an
answer is a reply to that id — nothing is guessed from what a terminal drew.

The pipes of a stream session need a process that outlives the executor and
the panel, the way the tmux server does for a terminal. That is the
**holder**: the executor's own binary in its holding mode, started by the
launcher in place of `tmux new-session`, in the same transient unit and for
the same reason — a child of the executor's unit could not write its
transcript. It is not a fourth set of rights: it runs as the owner, like the
tmux server, and like the tmux server it holds the whole conversation in
passing, because it is claude's parent.

What it lets out of that is narrow:

- **Its socket** answers the executor with the state of the session and the
  requests that wait for a person — the input of a tool asking for
  permission, the questions of a question: what the executor of a tmux session
  reads off the screen. It passes on only a closed list of control requests.
  The socket does not live beside the executor's: that directory is mounted
  into the service's container, and a holder's socket there would let the
  service talk to a session past the closed list of actions.
- **Its state file** is what the collector reads to put the session on the
  map: busy or free, the mode, the model, the names of the tools that wait,
  counts. No text of the conversation is in it.
- **The feed** is still read off the transcript by the collector, as for any
  session.

**A `claude -p` is a session of the panel only when a holder keeps it.** The
flags do not say it: an SDK reviewer or a script runs `-p` on the stream too.
A holder's state file names the conversation and the pid of its claude, and
a process that matches none is somebody else's run — seen in the archive, not
on the list of live sessions.

**A model, an effort and a permission mode are picked from a list, not
typed.** The list is the session's own where it has one — a session on the
stream gives the models its claude named at the handshake, with the efforts
each takes — and the catalogue of the account where it has not: the aliases a
terminal takes are named after it, and the older models no alias reaches are
taken by id, which the check holds to the shape of one. A row tapped in the
list is the choice, so the change goes out without a sheet asking again; the
two modes that stop a session asking at all are not in the list, because a tap
on a phone is not how that is decided. A mode on the stream is claude's own
request; a terminal has only a key that cycles the modes on its screen.

**How long a pick holds is chosen where the session can hold it either
way.** On the stream a model or an effort typed as a command holds for the
session. Saved as the default besides, an effort is written by claude itself,
for the model the session runs (max it never saves), and a model by the
executor into the settings file of the contour — rewritten key by key in
claude's own layout, so the change is one line. A terminal saves what is
typed as a command as the default, and holds a pick for the session alone
only through a dialog of its own that the panel does not drive: there the
pick is the default, and the list says so. Ultracode — xhigh with workflows
run for every task — holds for one session wherever it is set. Before it
changes the model or the effort of a conversation it holds cached, a terminal
asks whether the next answer may read the whole history again; the person
has already chosen in the list, so the panel agrees and says the price —
unless a hook is what asks, and then the question is the person's.

**The requests about settings pass only in the shape the panel sends.**
`apply_flag_settings` would merge any settings into a session, its hooks and
permissions among them, so the holder passes it with ultracode alone;
`update_settings` passes with an effort for the settings of the user alone;
and the answer to `get_settings` comes back with what the session runs with
and the merged settings bar the environment, the rules of permissions and the
hooks — whole, it carries the environment of every source, tokens and all.
The hooks listing comes back without the entries as stored, which a host would
edit by and which hold the headers of an http hook.
Claude answers an ultracode it cannot run with success and turns nothing on,
so the holder asks what the session runs with before it says ultracode is on.

**A question aside is the one piece of conversation the executor carries.**
On the stream `/btw` is a request of its own: claude answers it from what the
conversation holds and writes neither the question nor the answer into the
transcript, so the collector has nothing to read it from. It goes to the
executor as a question, not an action — the answer returns to the one who
asked and is not kept in the journal of actions, nor anywhere in the panel.
Claude keeps no side chat either: the page holds it and sends it back with
every question.

**The rules of permissions are not opened from the panel in any form.** Their
screen is driven by keys, awkward even at a terminal; `/permissions` and its
older name are refused as a command and as a message that starts with one, on
either side, and the composer says so before the press.

**A slash command is a message on the stream**, as `claude -p` takes one; the
holder remembers a model and an effort picked that way — the model as it was
picked, since the id claude resolves it to loses the context window. A clear
is the exception: it starts a conversation under a new id, the holder keeps
its session by the old one, and the session would drop off the panel. The
holder refuses it whichever way it comes, and the composer does not offer it.

**A question on the stream is answered with structure, not with keys**, so the
limits a terminal dialog puts on a layout do not hold there: a free answer and
"discuss" are open whatever the layout, a note can go beside a pick, and a
permission shows the tool's whole input rather than what fits a screen. A note
sent to a terminal session is refused with the answer: its dialog has no field
for one, and an answer that silently lost it would say less than the person did.

**A message waiting in the queue of a stream session can be taken back**
(`session.unqueue`). The panel names each message it sends there, the holder
passes the name on, and taking back is claude's own request to drop a queued
message: the holder forgets it too, or its queue would block a switch for good.
Editing is taking back into the composer. A message read before the press is
not in the queue any more, and the answer says it was delivered. The holder
learns a message is read when claude echoes it back, or — for a slash command
claude runs itself, which is never echoed — when claude reports the command
started. The
transcript writes the same record for a message taken back and one read, so
the holder keeps the fingerprints of the messages taken back — hashes, not
words — in a file that outlives it, and the feed shows those as taken back,
live and in the archive alike.

**An answered permission stays in the feed as a card**, drawn like an
answered question. On the stream a permission is a request and a reply, and
the transcript gets the call and its result with nothing of the question
between them. The holder keeps each answer — the call, the tool, allowed once,
allowed for good or refused, none of the words of the call — in a file that
outlives it, and writes it before claude has the answer: the collector parses
a record once, and a result read before its answer would stay without a card
for good. For the same reason the collector reads that file after it has
taken the length of the transcript and parses nothing past that length. The
card stands at the result of the call; what the call was about comes from the
transcript. A permission answered at a terminal leaves no card: the console
keeps nothing of it.

**A switch moves a live session between the two** (`session.switch`): the
executor closes the process on one side and resumes the same conversation on
the other, under the same name. The id and the history stay; the process does
not, and what lives only inside it goes with it — a turn in progress, an open
question or permission, messages in the queue, background tasks, Monitor,
wakeups, agents. So a switch happens only between turns and never under an
open request: the executor refuses rather than cut anything off. Background
work is the one loss a person may accept: the sheet names it before the press,
and on the stream, where the holder knows the tasks exactly, the executor
refuses a switch nobody agreed to.

The new process is started with the project's launch parameters and what the
session changed since its start. On the stream that is the model and the
effort picked in the feed, as they were picked, and the permission mode:
always when the start named one, and otherwise only when the holder saw it
change since the handshake — a mode nobody chose is claude's own name for the
default, and carrying it would override the project's choice. From a console it is the model and the effort its status
line shows, which names the model with its context window, and the mode of
the last message sent since the console started, or the mode it was started
with — a mode changed after the last message is in neither. The opening
message of the project is dropped — a resumed conversation would read it as a
new request. A stream session can always go to the console; a console goes to
the feed only when its project lives there, because which projects live in the
feed is decided in the map, not by a button in one conversation.

**The switch is the pair of views, and a window holds a console.** Where the
project lives in the feed, the terminal and the feed of the conversation
header are the two sides: the feed is the session on the stream, the terminal
is the console, and pressing the other one moves the session there. A session
on the stream has nothing a window on the host could show, so the window
button moves it to the console and opens the window in one action. While a
terminal outside the panel shows a console — a window on the host, an ssh
attached to its tmux, or a terminal of its own when it runs outside tmux — the
executor refuses to move it to the feed: the conversation would end under the
eyes of whoever reads it there. The pair then only picks what to watch the
console with. A project that lives in the console keeps the pair as a choice
of the device.

---

## The path of an action

Between a tap on the phone and a command on the host there is one road, and no
button gets around it.

1. **The registry.** The panel knows about every action what it will do, and
   tells that to the human **before** the tap: the consequence, not "are you
   sure?".
2. **The gate.** The single front-end module that can send a command. Not checks
   inside handlers — the gate precisely, so that a new button cannot be added
   around it. An action without a confirmation sheet still goes through the
   gate, and the list of such actions is closed by a test.
3. **The service.** Checks the shape of the request, fills in what the panel has
   no right to send (the identifier of a conversation to resume is taken from
   the database, not from the phone), writes a line into the journal **before**
   carrying it out, and calls the executor.
4. **The executor.** Accepts only known kinds of actions; everything else it
   rejects without trying to parse it. It finds the target itself — in the list
   docker gives — and from there works with the identifier docker handed out. It
   duplicates the audit trail into journald as two lines, before and after:
   actions happen precisely when something is wrong, and the audit trail must
   not depend on the database being alive.
5. **The answer.** The executor answers when the job is done — and the screen
   shows the consequence from that answer. Polling the snapshot stays as a
   fallback: seconds lie between the action and the next snapshot, and a screen
   that believes only the snapshot lies to the human with "nothing happened".

The target is never substituted into a command as a string: the name is checked
against the list docker gives, and from there the identifier docker handed out
goes into play. The conversation identifier is checked for the shape of a uuid,
and whether it exists is checked by the one with the transcripts directory under
its feet.

**The panel's own container is not shut down from here.** Not because of rights:
there would be nobody to report the result to — the service would die before the
answer, and the journal would lie.

---

## What the executor can do

Twenty-six actions, and the list is closed.

| Family | Actions |
|---|---|
| containers | `container.start`, `container.stop`, `container.restart` |
| stacks | `stack.up`, `stack.down` |
| sessions | `session.open`, `session.resume`, `session.close`, `session.restart`, `session.kill`, `session.send`, `session.answer`, `session.dismiss`, `session.stop`, `session.escape`, `session.file`, `session.command`, `session.set`, `session.mcp`, `session.permit`, `session.switch`, `session.unqueue` |
| windows | `window.open`, `window.close` |
| background work | `task.stop`, `agent.stop` |
| disk | `project.create` |

What is deliberately not on the list: removing containers, images and volumes,
`docker exec`, editing compose, package operations, restarting itself.

**The host's main session is not closed from the panel, but it is restarted.**
The session living in the home directory is the one the panel itself lives
next to, and it has no close button, just as the panel's own container has no
stop. `session.restart` ends it the gentle way — the same wait for the
transcript — and starts a new one in the same directory under the same name
with an empty context; the old transcript stays in the archive. Only the main
session: its launch is fixed by the home directory and its name, while a project
session carries launch parameters that live in the profile map, and a restart
without them would silently bring it up in another setup, possibly under another
account. A project session is closed here and opened again from the map.

**A conversation is resumed where it ran, with the parameters of the project
it belongs to.** Its transcript is kept by the directory it ran in, so a
session from a worktree or a subdirectory comes back there, not in the
project's own directory. The project is the nearest directory above it that is
in the map, or, for a git worktree kept beside its repository, the project of
the main checkout — the agent reads that off the worktree's `.git` file, since
the service does not see the disk. The climb stops below a project root: a
project that is a root itself, like the home directory, holds the machine's own
session, and taking it for the owner of whatever lies under it would start that
in another account.

**Kinds are split by intent, not by convenience.** Answering a question and
dismissing a question are different actions, because their consequences differ.
A file is not a field inside a message but a kind of its own: the panel asks the
executor what it can do and greys out buttons from the answer, and a field does
not change that answer — an old executor would accept the request and silently
throw the file away.

**A message is typed into the session, not handed over as a paste.** A console
folds a large write into a paste of its own making and marks it in the
transcript as pasted content: what the person at the panel wrote would then
reach the session as quoted data rather than as words addressed to it. So the
executor types — a handful of characters at a time with a pause between the
writes, line breaks as line breaks, and the key that sends the message only at
the end. A line beginning with a bang or a slash goes in as a paste instead: it
is a shell command or a slash command, it arrives the same either way, and a
paste keeps the palette of commands out of it. A line beginning with a hash is
text like any other and is typed. A message ending in an unfinished
`@name` is typed with a space after it, which closes the list of files the Enter
would otherwise pick a row from — a space rather than Esc, because Esc reaches a
session that is working as an interruption of its work.

**The list of this machine is shorter than the general one.** The executor says
what it can do here: with no tmux there are no actions over sessions, and the
window ones go away with them; with no terminal invocation template only opening
a window is gone. The button for an action the host does not know greys out and
explains why.

**A new action does not arrive on the host with a rollout of the panel.** The
service travels as an image, the executor as a separate binary that is built by
hand. Until it is rebuilt, the button is there on the phone and the journal says
"unknown action". The rule works both ways: the mismatch is the same whether an
action was added or removed.

---

## Data

Postgres in a container of its own, not lodged inside somebody else's: the panel
must not fall together with a neighbouring project's database, and the other way
round. The port is not published outwards — only the service reaches it, and
only over the compose network.

**Metrics are partitioned.** Raw measurements are written in batches, rollups
count the minute and the hour, retention detaches a whole partition and drops it
— deleting a file instead of churning through a table. Retention periods live as
a row in the database and are changed with an `UPDATE`, without rebuilding the
image.

**The history is written per container, never per process.** A process comes and
goes in seconds and its command line is a column of its own every time: kept in
the database it would grow rows nothing ever reads back. So a screen asking who
is eating the machine holds two kinds of row at once — a container measured over
the period, a process measured this second, straight from the snapshot — and it
says which is which, because one of them has no past to show.

**The schema travels with the service.** Migrations are applied when the image
starts. An applied file is untouchable: its checksum is recorded in the
database, and editing any character — a comment included — parts the file from
the database, after which the service does not come up. That is why there are no
comments in migration files at all.

**Two roles.** The service works under the application role: it has neither DDL
nor `TRUNCATE`, and on the journal table only `INSERT`, `SELECT` and `UPDATE` of
the outcome columns. The journal is kept immutable by triggers, but triggers do
not hold the table owner — it will take them off with a single command. That is
why DDL goes through a second connection under the schema owner, and user input
never gets in there.

**Grants are issued by the service at startup, not by a migration.** A migration
is applied once in the life of a database: a role created after the roll-out
would never have got anything. The grant step comes after the migrations, every
time, and brings the role's rights to the set declared in the code.

**What the panel watches is a config, not a seed.** The alert rules and the
probes the container runs live in `internal/watchcfg/config.yaml`, built into
the binary beside the migrations. At startup the service brings the database to
that file: a record is created or updated, and one that has lost its entry there
is disabled. Changing a threshold or a wording costs a line and a rebuild, where
a seed in a migration would cost a new migration every time.

Records are matched by a key, not by a name. The name is a label on the screen
and is free to change; the key is the identity and outlives every rename, which
is what keeps a rule's alerts attached to it. Nothing outside the file is
deleted, either: alerts reference their rule with `ON DELETE CASCADE`, so a rule
that leaves the config is switched off and its history stays.

Probes have a second source and the config does not reach it. The ones the
collector on the host sends arrive by name, carry no key, and are neither
updated nor disabled by the synchronisation.

Only the service writes to the database. The executor does not reach it at all.

---

## Collection

The collector reads `/proc` and claude transcripts and writes a snapshot: load,
disks, network, processes, live sessions with how full their context is, port
checks.

**A session is recognised by its process, not by the freshness of a file**:
session names are reused between runs, and yesterday's conversation under the
same name is a different conversation.

**The archive is ordered by when a conversation last spoke, not by when its
file was last written to.** Claude appends a title, a mode or a snapshot of file
history to a transcript long after the talk ended, and the file goes fresh with
nothing fresh in it: ordered by that, a conversation of the spring stands above
yesterday's while its own card says spring. The order is the stamp the card
shows, read from the tail of the transcript and remembered beside the parse; a
transcript that names no time has nothing left but its file.

**How full the context is gets counted on the spot** by the same parsing that
counts it for closed conversations: one and the same session must show one
number before and after it is closed.

**The answer of a slash command reaches the feed as numbers, not as its
text.** Claude writes what a command printed for the screen it was typed at: a
terminal gets a grid of coloured glyphs, everything else a markdown table as
long as the list of skills, and neither reads in a conversation. The collector
reads the answer of a command it knows into numbers — from the field the record
carries them in when there is one, from the markdown when there is not — and
the screen draws them as a card that opens the breakdown. What the field does
not carry — in the answer of `/usage`, which habits spent the limits — is read
off the text beside it. The terminal's grid
is left out: a markdown copy of it follows in the next record. The answer of a
command the collector does not know stays a line of text.

**`/mcp` is a screen of the panel, asked of the session.** On the stream the
panel asks claude for its MCP servers each time the screen opens, and changes
one of them — reconnect, enable, disable — with claude's own requests.
Authentication is left to the host: its browser comes back to the host, and a
phone is not that browser. Only what the screen shows leaves the host: the
headers, the arguments and the environment of a server, and the query of its
address, can carry keys. In a console `/mcp` is a screen driven by keys, and
the composer does not send it. `/status` is a screen of the panel too, in both
kinds of session: most of it the snapshot of the host already holds, the
version comes from the file of the session, and the account from claude's
answer to the handshake — which a console does not have, so its card goes
without one.

**`/hooks`, `/memory`, `/skills`, `/agents` and `/config` are read-only screens
of the panel.** On the stream claude hands out the rows of its own screens for
the first three, names the kinds of subagents at the handshake, and answers
`get_settings` with the merged settings; the panel asks each time a screen
opens and changes nothing — a hook or a skill is changed in its file or by
asking Claude. The rules of permissions are not shown in any form, and the
environment never leaves the host. In a console these are the client's own
screens, driven by keys, and the composer does not send them.

**A button under the composer lists what the panel does itself** — its
screens, the pickers of the model, the effort and the mode, a question aside,
and the commands whose answers the feed draws as cards (`/context`, `/usage`)
or that change the conversation (`/compact`). The list is read from the same
registry the composer reads, and each row does what typing it does: a command
goes through the same confirmation. A console session is offered only what
works there.

**A session on the stream is renamed from the panel** (`session.rename`, the
sheet of `/rename`). Claude takes the name with a request of its own and
writes it into the file of the session, where the panel and the host look a
session up; the holder is keyed by the conversation and does not notice. A
name keeps to what a URL and a file name take, and a name another live
session answers to is refused: two sessions would answer to one name. The
open conversation follows the session by its conversation to the new name. A
terminal is renamed on its own screen, `/rename` with keys.

**There is nowhere else to get the subscription limits from.** The 5h/7d
percentages do not lie on disk and are not handed out by any API — the only one
claude tells them to is the status line, in the payload on stdin. That is why
the snapshot is written by a script that the install puts first in the status
line chain. Hence a consequence visible in the interface: the numbers live only
while at least one claude session is running, and the header honestly shows the
age of the snapshot instead of yesterday's percentages.

**The snapshot is parsed block by block, and a failed block is visible.** One
field handed over as a fraction where an integer is expected does not bring down
the writing of all the metrics: blocks are parsed apart from each other, a
half-parsed one is zeroed out whole (half a list of disks lies silently), and
the failure goes to the panel through its own endpoint and is named to the human
on a screen.

**Token usage** is collected in a separate round on demand: the service asks the
collector what lies on disk, compares that with its own scan points and orders
the parsing of only what is new. There is only ever one round: the parsing runs
into the processor of the machine the panel is watching.

---

## The front end

**There is no npm in the project.** preact, htm, uPlot and xterm lie as files in
`web/vendor`, they are built by esbuild, which is pulled in through `go.mod` as
an ordinary Go library. A `package.json` appearing is a mistake. The built
bundle goes into the binary through `embed`.

**There are two shells, the screens are shared.** The phone has its own
navigation, the wide screen has its own; the screens are the same ones. There is
no full copy of the front end and there will not be: the screens are more than
half the code, and a second copy of the conversation would part from the first
silently. What depends on width is the layout around the screens, not the
screens themselves.

**A contour is asked for by the id of its map entry, never by the name on the
screen.** The map labels a contour however its owner likes, and the collector
knows contours by the directory they live in: a label sent as a name matches
nothing on the other side, and the answer quietly holds another contour's
conversations or none at all. The service turns an id into a directory. A
contour the map does not know keeps the name the collector gave it, which is
the only thing naming it.

**The archive is asked for one contour at a time.** A page of it comes back
sorted by the time of the last message, so a single page over every contour is
filled by whichever contour is being worked in, and the quieter ones start
several pages down — on the screen they are simply absent. The panel gives each
contour a page of its own, and a request that names no contour at all is
answered with the personal one alone.

**The size of the desktop is the person's to set.** Every size in the styles is
written in rem, so the root font size is the single number the whole interface
hangs off: the two buttons in the header move it and the columns, the rows, the
icons and the type follow at once. The scale sits on the root element while the
desktop shell is mounted and is taken off with it — left there, it would follow
the same browser to the phone, where no control can undo it. The starting point
is larger than the drawing calls for: a panel at a desk is read from further
away than one in the hand, and every row of it puts a small mark next to a
destructive one.

**A button explains itself only when it cannot work.** The tooltip of a button
that does its job repeats what the icon already says, and it covers the row the
pointer is aiming at — on a row where the neighbouring mark closes a session.
What an icon cannot say is why it does nothing: an executor that has never heard
of the action, a session already closing. That text stays; the rest is the icon
and the name a screen reader is given.

**The terminal emulator loads as a separate file**: it weighs three hundred
kilobytes and is not always needed — in the common bundle every phone would
download it, and into the service worker precache on top of that.

**The page has no external domains at all.** The fonts are vendored and lie in
the precache together with the shell: without them the panel, opened with no
network, is drawn in a system font — that is, it looks unlike itself exactly
when there is no time to study it.

**One thing is allowed out, and only by hand.** Dictation into the composer is
the browser's own recognition, and in Chrome that means the recording goes to
Google. It is off until the person turns it on for that device, the switch says
where the sound goes before they do, and nothing else in the panel crosses the
machine. The recognition is not done here because doing it here is a model on
the host, and that is a different feature with a different price.

The panel installs as a PWA. The service worker goes to the network with a cap
and falls back to the cache: an address whose packets do not get through but are
dropped would otherwise hang the app on the logo forever.

---

## Access

**There are no passwords.** A passkey (WebAuthn) is the main door; a long-lived
token is the second one, for exactly the case where a passkey cannot work at
all: a browser hands out a key only over https or on localhost.

The passkey domain is baked into the key itself, which is why the default is
`localhost` and not somebody's production address: changing the domain voids
every key already enrolled, and a value like that is not hidden in the code.

The session lies entirely in a signed cookie — there is no session database.
There are two deadlines, idle and absolute, and neither can be turned off,
including by setting it to zero: a session with no absolute deadline is an
eternal session with extra code. A token gives a session through a derived
device number, so changing or removing the token immediately closes the sessions
it issued and does not touch the passkeys of phones.

### Four listeners

| Listener | Address | Sign-in | Session terminal |
|---|---|---|---|
| main | `:8776`, published on the given host address | passkey and token | only when explicitly configured |
| local panel | `:8777`, published on the loopback and nowhere else | no sign-in — the door was checked earlier | always |
| local network | its own, TLS held by the service itself; comes up only when configured | passkey and token | by the same setting |
| tailscale | its own, only inside the compose network; TLS held by `tailscale serve` | passkey and token | by the same setting |

The listeners have **the same routes**, and there are exactly two
differences, both set by the gate: what guards an endpoint and whether the
terminal is in the set. A second set of routes would part from the first
silently.

**The live session terminal does not exist on the main listener by
default** — not "behind a password", it is not there as a route. Behind it are
the session screen and raw input into it, that is, a bypass of the confirmation
gate: somebody else's install must not get that silently along with an update.
The panel writes the fact of a connection into the journal, but not the bytes —
passwords get typed in a terminal.

**The local panel lets you in without signing in**, because the door is checked
at the entrance to the listener: origin, host and `Sec-Fetch-Site`. It is
published on the machine's loopback and nowhere else, and that address will not
become a variable: the whole point of the port is that it cannot be reached from
anywhere but the machine itself.

### The address map

After signing in, the client gets a map of addresses in order of closeness,
measures them and goes for data to the nearest one that answered, falling back
to the next one on failure. The page stays where it was opened: what moves is
not the page but the API address. For a foreign origin the request carries the
signed session in a header, and CORS is open to exactly the addresses from the
map.

A single failed request does not cross an address out: a probe is sent to it
first. If it answered with its own panel, it is alive, one request fell, and it
stays the base.

---

## What the panel does not have, and why

- **Control of other machines.** Only its own host, now and later. At best
  metrics collection travels to a remote machine.
- **`POST` on the socket-proxy.** Never: that is where the point of the split
  disappears.
- **Editing settings from the screen.** The list of settings is shown, but
  read-only: their cost of change differs — from "right away" to "recreate the
  container" — and one identical button would deceive the human.
- **Power and the graphics card.** These sources will not be there where the
  panel travels: the UPS watchdog is the machine's own system housekeeping, and
  polling the graphics card twice a minute wakes a laptop's discrete card for a
  chart nobody looks at.
- **A task registry.** It lives as a claude plugin, not everybody has it, and on
  somebody else's install the buttons would honestly answer "not found".
- **Actions of its own on a session.** The panel never types into a session by
  itself, not even to save it from a full context. What a session does about
  its own context is decided inside it: the context guard is a `Stop` hook,
  switched on by a setting of the project, and it speaks to the model at the end
  of a turn, when the session is free by construction. A watchdog outside would
  have to guess whether the session can take a keystroke right now, and a guess
  that lands in an open dialog is an answer given blind.
- **Settings for notification delivery.** No quiet hours, no importance
  threshold: the only setting is whether there is a subscription. One push when
  a reason appears and one when it is gone, with no reminders; the server
  collapses them, not the device. The only exception is that a push about a
  session does not go to the device where that session is on the screen right
  now.
- **Safari.** The panel lives in one browser. That removes a class of
  workarounds, but building into the markup what is known to be dead there is
  not worth it either.
