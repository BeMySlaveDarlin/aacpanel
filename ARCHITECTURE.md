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

**A switch moves a live session between the two** (`session.switch`): the
executor closes the process on one side and resumes the same conversation on
the other, under the same name. The id and the history stay; the process does
not, and what lives only inside it goes with it — a turn in progress, an open
question or permission, messages in the queue, background tasks, Monitor,
wakeups, agents. So a switch happens only between turns and never under an
open request: the executor refuses rather than cut anything off. Background
work is the one loss a person may accept: the conversation header names it
before the press, and on the stream, where the holder knows the tasks exactly,
the executor refuses a switch nobody agreed to.

The new process is started with the project's launch parameters and what the
session changed since its start. On the stream that is the permission mode:
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

Twenty-three actions, and the list is closed.

| Family | Actions |
|---|---|
| containers | `container.start`, `container.stop`, `container.restart` |
| stacks | `stack.up`, `stack.down` |
| sessions | `session.open`, `session.resume`, `session.close`, `session.restart`, `session.kill`, `session.send`, `session.answer`, `session.dismiss`, `session.stop`, `session.escape`, `session.file`, `session.command`, `session.permit`, `session.switch` |
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
the end. What typing would change goes in as a paste instead: a line beginning
with a bang, a hash or a slash is a mode or a command, and the palette of a
command swallows whatever follows it. Such a line arrives wearing the mark, and
that is the price of it arriving at all. A message ending in an unfinished
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
