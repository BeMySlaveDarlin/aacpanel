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
the container yields exactly the lists of actions and questions written down in
`internal/action`.

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
                                  review.sock
                  │       ▲          ▲          ▲
                  ▼       │          │          │
            docker.sock   └── aacpanel-agent    └── aacpanel-exec
                                 (host)                (host)
                                    │  ◄── seen.sock ───  │
                                    │                     │
                            /proc, claude           docker, tmux, claude,
                            transcripts;            the terminal window,
                            ask, brief, page,       the holders of the
                            notify.sock for the     stream sessions
                            hooks and tools of
                            sessions
```

**The snapshot.** The collector writes the state of the machine as a file into
the state directory; that directory is mounted into the service read-only.
Everything else that only the host knows goes through it as well: the socket of
the conversation feed, the socket of token usage collection, the shelf of
readings.

**The action socket.** `0600` owned by the session owner — behind it is the
whole list of actions on the host, and its permissions must not be loosened.
That is why the service in the container runs under the same uid (`user:` in
compose). What is mounted is the **directory**, not the socket itself: a
bind-mount of a file is tied to the inode, and a restart of the executor would
silently break the link until the container was recreated.

**Questions go down the same socket.** Besides actions, the service asks the
executor what it can do here, which permission a session is standing on,
whether it has a window open, its models, its MCP servers, what it says about
itself, its commands, one of its read-only screens of settings, a question
aside, and the models codex offers — asked of the daemon of any codex home,
since a launch parameter is picked before any session runs. A question changes
nothing on the host and is written to no journal, but its list is closed the
same way (`internal/action/validate.go`): an unknown one is refused without
being parsed.

One question does write: the service hands the executor the context guard of
every place the map knows — the cap and whether a session past it restarts —
and the executor keeps them in `~/.local/state/aacpanel/guards.tsv`, where the
context guard hook and the prompt stamp read them. That file is the only thing
it touches, and it is replaced whole. The guards go out at the start of the
service, after every change of the map and every five minutes, and every
fifteen seconds while they fail: an executor that was down when the map changed
catches up on its own, and a deploy does not leave the host unguarded while the
database migrates.

A restart of a session keeps a marker in `~/.local/state/aacpanel/restarting/`,
named by the session, from its start to its end, whatever it ends with. The
context guard hook asks a session for a restart once a conversation, at the end
of a turn at its cap or within a point under it, and not while a marker of it
younger than five minutes is there: the close of a restart lets the session stop
once more, and an ask there would bring up a second session. The point under the
cap is there because a session wraps its work up for the restart it sees coming
and stops a little short, and a session stopped under the cap does not stop again
until someone writes to it; the prompt stamp names the cap alone, since a session
told of the margin would stop short of that.

**The delivery socket.** The executor has no access to transcripts, yet typing
into a session in tmux is blind without them: whether the message landed, whether the
turn is over, which permission mode the session was last in — only the
transcript says. So it asks the collector over `seen.sock`, in the collector's
runtime directory: it names the conversation and a mark cut from the message,
and gets back a fact — found, seen, queued, ended, a mode — never a line of the
conversation. The reader of the conversation answers yes or no; the one that
types does not read.

The starting position is taken before typing. If that lookup fails, only the
screen can confirm the send: taking a position after typing would skip the
reply, and reading from the start could count an older one. Once a position is
known, a silent answer keeps it for the next check; a conversation known not to
exist starts at zero so its first reply can still be seen.

**The hooks of sessions.** Every hook the kit writes runs the executor's
binary: `aacpanel-exec hook <name>` reads the event's payload on stdin and
answers on stdout, and needs nothing on the machine but the binary every
install has. Its exit is 0 for every event and every outcome, a panic
included: an exit of 2 holds a `Stop` in a loop, refuses a tool call and throws
a prompt away. A hook that holds a `Stop` says so in its answer,
`{"decision": "block"}`, which lets the turn go on for one round. The
installer recognises the python forms too, including
`test -f <script> && python3 <script>`, and rewrites them in place. The limits
and session model are kept by `aacpanel-exec statusline`, which passes the
payload to the person's line through `AACP_STATUSLINE_NEXT`. codex runs the
hooks it shares with claude from `hooks.json` in its
home and only once they are trusted in its `config.toml`; the installer writes
both (INSTALL.md, S12a). The wiring's verification and `./install.sh check`
ask each home's daemon for `hooks/list`, with no thread: the installed entries
must be present, trusted and enabled. Without its socket the check runs a
private `codex app-server --stdio` with that `CODEX_HOME`, closes stdin after
the answer and waits for its exit; the context kills it at the deadline. The
program is the installer's answer to "What starts codex?", saved as `AACP_CODEX`
in `host.env` for the executor, updates and checks. Its default keeps that
file's value, then tries the installer's `AACP_CODEX`, `PATH`, the standalone
`~/.codex/packages/standalone/current/bin/codex` and `~/.local/bin/codex`, in
that order. The question appears only with a codex home or a launcher found.
The report names the server used, and neither path starts or stops the shared
daemon. Recomputing the
installer's hash alone would miss a change to codex's trust formula.

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

**The answers belong to one publication.** A brief is named by its id and
born when it first goes on the shelf; a reissue under the same id keeps the
birth and the answers. A session removes its brief through the collector,
which does not tell the panel, so the draft of a removed brief stays in the
database — and a brief published again under the same id is born anew: the
answers typed into the first do not show in it, on its screen, on the shelf or
in what goes to the session, and the mark that the first was sent does not
lock it. The draft names the birth it was typed into, and the collector puts
the birth on every card of the shelf, so the list tells the two apart without
opening either.

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
goes stale there, because a push is for now.

A call also stays where the person looks next. The feed draws it as a card
from the call itself, the line the phone got, and a refusal of the tool marks
the card with its reason, as a letter that reached nobody is marked. The panel
keeps the last call of each conversation in `session_calls` and marks the
session in the lists until the person opens its feed on any device: the call
was for the person, not for a device, and the mark does not go stale. Opening
is the view mark a poll of the host carries, and a screen polls the moment a
feed opens, so a glance shorter than the gap between polls still counts; until
that poll answers, the device that opened the feed hides the mark itself. A
call made while some device has the session on screen sets no mark, as it
sends that device no push. The price is a row per conversation that called,
kept for a month.

A call does not travel in the snapshot. The panel keeps a request for the calls
waiting on the chat socket, and the collector answers it the moment it takes
one, so the push is in the queue within a second of the tool saying the person
was called; the snapshot is written on a tick and read on a slower one, and a
call standing in it would reach the phone up to a minute late. The price is a
thread of the collector held by the waiting request and a fresh request every
few seconds while nobody calls. Whether the phone is looking at that session is
still asked when the push is sent.

**Tasks for the person's hands.** `request` reaches the local listener named
by `AACP_PANEL_URL`, for Claude and Codex alike. Its caller supplies the
conversation through the MCP binding; the service finds its live session
name in the snapshot. The public listener cannot create or withdraw these
tasks. The title (120 characters), Markdown steps (2000 characters), blocking
flag and lifecycle live in `session_requests`. This short, explicit task is
kept by the service; the transcript remains on the host. An agent can withdraw
only its own open request. Every new request pushes with its title, regardless
of push preferences; delivery skips devices already showing that session.

The counter in both session lists opens the conversation. Its collapsible
queue over the composer holds open requests, the unseen call, briefs with
questions whose answers have not been sent, and secrets not saved after they
were asked for. Questions and permission prompts keep their own docks and do
not enter this count. Opening the session settles its call through
`session_calls`, on every device, and none of the other items. The service
adds this summary to the host snapshot: the collector supplies card anchors,
the database supplies request and brief state, and the executor supplies secret
file times. The collector indexes only these cards and reads new transcript
records after the first listing; it keeps no second copy of the whole feed.

A queue item opens the feed window around its card and collapses the queue to
leave room to read. The request card renders its Markdown with the feed's copy
buttons and sends Done or Can't with an optional note through the same action
gate as the composer. `session.send` accepts a request id and answer; the service
builds the message from the stored title and the person's answer, checks its
conversation, and locks the row while the executor accepts the message. Only
then is it marked answered. A failed send leaves it open. A retry keeps the
message id for transports whose queues know it. A database failure after delivery
is reported as delivered but not marked, rather than inviting a blind resend. There is
no new executor action or host right.

The feed recognises that reply by its whole text and draws a panel card with
the title, Done or Can't, and the note. The request id stays in the folded text
the session got. Shared samples check both `request.Reply` and the feed's
reader; a text that only resembles the reply stays the person's bubble.
The time over the composer and in the desktop graph says "last reply" so it
does not read as the time of a task for the person's hands.

**The notepad of secrets.** A session that needs a credential — a token, a
password, the env file of a database — asks the person for it with
`secret_ask`, not in the conversation, where the value would settle in the
transcript, in the feed and in whatever the phone keeps of the feed. The tool
takes a name, a title and the notepad to start from — `KEY=` lines with a
comment on where each value is found — calls the phone through `notify.sock`,
and answers with the name first. The feed draws a card from the call and that
answer, as it draws the card of a brief, and the panel keeps nothing of the
request: the transcript is the only record that it was made. A brief is no
model for it — its answers are drafts in the database, and a secret has no
place there.

The person fills the notepad in on a sheet of the panel and saves it. The text
travels in the body of `secret.put` and nowhere else: the service journals the
name and the length, the executor writes the file and answers with the name
and the size, and no journal, audit line, log, snapshot, socket answer or
refusal carries a byte of it. The sheet holds the text in memory until it
closes, never in a draft or in the storage of the browser, and the service
worker keeps only answers to GET, which never carry it. The file is
`~/.local/state/aacpanel/secrets/<name>` (`$XDG_STATE_HOME` moves it), 0600 in a
directory of 0700 — outside the state directory of the panel, which is mounted
into the container, and outside the directory of files sent from the phone,
which the feed opens. A save under a name already there replaces the file
through a temporary one beside it and a rename: that is how a token is rotated.
Then the session is told the path the way any message of the panel reaches it —
typed, sent on the stream, or queued behind a busy composer — with the names of
the keys and of the ones left empty, never a value; a session that could not be
told leaves the file in place, and the refusal names it. The list on the screen
is a question to the executor that names the files with their sizes and
times, and `secret.drop` removes one.

**What keeps a secret out of the transcript is a rule, not a wall.** The
session runs as the owner, and the owner reads the owner's files: the tool's
word tells the model to use the file by its path — `--env-file`, a redirect,
`--password-stdin` — and never to read or print it, since whatever it reads
goes into the transcript, and nothing on the host enforces that. What the panel
holds itself is its reader of files: it refuses a path whose real path lies in
the directory of secrets, even for a conversation running in the home
directory, because the message names the path, the feed makes a path a link,
and the service worker would keep what the link opened on the phone.

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

**A session on the stream gets Claude in Chrome where a terminal one would.**
claude turns Chrome on by the account's own setting only when it runs
interactively; on the stream it runs `-p`, where only the `--chrome` flag
turns it on. The launcher reads the setting from the account's `.claude.json`,
where claude itself keeps it, and passes the flag when it is on. The setting
is read at the start: turned on later, it reaches a stream session with its
next start.

**A holder runs the program it was started with until its session ends.** It is
the process claude's pipes belong to, so a new build of the executor reaches it
only through a new start. An operation or a control request the running holder
does not know is refused with what to do: the session was started before the
panel could do this, and a restart, which keeps the conversation, brings it.

**A call the turn waits on goes to the background the way Ctrl+B moves it.**
A shell command or a subagent claude runs in the foreground is a task it can
move: the call answers at once, the turn goes on, and the work runs on as a
background task, which the collector reads off that answer like any other. On
the stream one call is named by the id of its tool_use block, and the holder
follows the calls in the foreground by claude's task events and their answers.
In the console one key moves every call at once, so only all of them are
offered there, and the key is pressed only while claude shows its hint under a
running call: the hint names the key the terminal takes, and the same key into
a free composer is a keystroke into the conversation.

**The stream is claude's control protocol, and part of it is not public.** Any
release may change a request the feed leans on without a word, so
`make stream-contract` runs every such request against the installed claude
and names the one that no longer holds. It also runs after the host's Update
Claude button: the panel does not hold sessions against
the version it last passed on. The check spends tokens on haiku and goes to
the network, which is why it is not part of `make check`. The Go runner in
`cmd/stream-contract` also sends the holder's letter frame: `origin.kind=peer`
and `fromMode` matching the session's permission class. A letter must reach
the model without an error, permission request or hold, in both prompting and
bypass modes. It carries a fact, and a user turn asks for that fact after the
letter has been processed: reading a peer's information does not require
obeying its instructions. The transcript must keep its text as a user message
or a queued command attachment; claude does not persist the input origin. The framing of
a mid-turn letter is printed as information. Parsing and verdict tests use
sanitized protocol samples without starting claude.

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

**A command typed after "!" is claude's to run, not the model's**
(`session.shell`). At a terminal the composer runs it in a shell of the
session with no turn of the model, writes the command and what it printed
into the conversation, and the model answers that. On the stream claude takes
such a command as a line of its own kind and runs it the same way — in the
session's directory, with its environment, beside an answer in progress — but
keeps the output out of the conversation and hands it to its host alone. So
the holder passes the output on to claude as a message in the terminal's
shape once the command ends, cut the way a terminal cuts it for the model, and
the model reads it and answers as it does there. Claude runs the command in a
fresh shell each time: a `cd` does not outlast its line. The feed follows the
command as a card: the page draws it going out and running, and once the
output reaches the transcript the card says how the command ended, shows the
last lines of what it printed and opens the whole. A running command holds off
a switch — closing claude would stop it, and its output would never arrive. A
session in tmux gets the line as typed, since its own composer runs the command.

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

The feed's queue state travels independently of transcript rows: delivery or
withdrawal can change it without a byte being appended to the transcript.
The holder's summary names pending inputs by UUID, time and fingerprint, with
no words. A queued transcript row carries the fingerprint of its full input,
before truncation. Claude's queue records do not retain the input UUID, so the
screen joins only an exact fingerprint with a unique occurrence after the send;
ambiguous repeats offer no cancellation of a guessed message. This keeps edit
and take back on a row after its local copy gives way and after a page reload.
An input with file paths has the same identity. A truncated row can be taken
back, but cannot be edited from an incomplete copy of its words.

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
transcript. A permission answered at a terminal leaves no card: claude in tmux
keeps nothing of it.

**A switch moves a live session between tmux and the stream**
(`session.switch`): the executor closes the process in one and resumes the
same conversation in the other, under the same name. The id and the history
stay; the process does not, and what lives only inside it goes with it — a
turn in progress, an open question or permission, messages in the queue,
background tasks, Monitor, wakeups, agents. So a switch happens only between turns and never under an
open request: the executor refuses rather than cut anything off. Background
work is the one loss a person may accept: the sheet names it before the press,
and on the stream, where the holder knows the tasks exactly, the executor
refuses a switch nobody agreed to.

The new process is started with the project's launch parameters and what the
session changed since its start. On the stream that is the model and the
effort picked there, as they were picked, and the permission mode:
always when the start named one, and otherwise only when the holder saw it
change since the handshake — a mode nobody chose is claude's own name for the
default, and carrying it would override the project's choice. From tmux it
is the model and the effort its status line shows, which names the model with
its context window, and the mode claude last wrote into the transcript since
the session started, or the mode it was started with. The transcript is read
for it by the collector: the executor has no access to transcripts. The
opening message of the project is dropped — a resumed conversation would read
it as a new request. A stream session can always go to tmux; a session in tmux goes to
the stream only when its project lives there, because which projects live on
the stream is decided in the map, not by a button in one conversation.

**Where a session lives and what it is watched with are two things.** It
lives in tmux or on the stream, and it is watched with the Feed or the
Terminal. The pair of views only watches; the move is a line of its own,
named by where the session goes — Move to the stream, Move to tmux — and a
window holds a session in tmux. A session on the stream has only its feed:
the terminal of the conversation header is tmux, reached by the move, and
pressing it opens the move rather than making it. A session in tmux is
watched either way — its terminal, or its transcript read as a feed — and the
pair only picks which: the session stays in tmux, where an agent that started
it may be following its pane. The pick is the session's own, kept on the device by the session's
name; a session never picked opens in the terminal on a wide screen and in
the feed on a phone, and a renamed one opens that way again. A session in
tmux whose project lives on the stream moves there from the tools of the
session. A session on the stream has nothing a window on the host could show,
so the window button moves it to tmux and opens the window in one action.
While a terminal outside the panel shows a session in tmux — a window on the
host, an ssh attached to its tmux, or a terminal of its own when it runs
outside tmux — the executor refuses to move it to the stream: the
conversation would end under the eyes of whoever reads it there. The panel's
own view of it, the terminal of the session or the Terminals screen, does not
hold it. While the window on the host is open, the tools do not offer the move
at all.

**A claude typed into a terminal of the panel is a session in tmux.** The
executor looks for the pane of a session on the user's own tmux server and on
the server of the panel's terminals, and every call about the session — what
is typed into it, its screen, its terminal, its window, the move — goes to the
server the pane was found on. Closed or moved, it ends the way a session the
panel started does, and the shell of the terminal stays where it was: the
terminal is the person's, and it closes from the Terminals screen. A tmux
server on a socket of any other name is out of reach: a claude under it is
only read, like one typed into a terminal by hand.

**A close takes the tmux session along only when the launcher started it.**
Claude gets its signal and the transcript is waited for; then a session the
launcher started is killed by its exact name, so that a terminal with
`remain-on-exit` does not hang there empty. The launcher's session is known by
the command tmux keeps for its pane for the pane's whole life — the
environment of the launch read from its file, then claude — which nothing a
person starts by hand carries. Claude typed into a shell, in a tmux session of
the person's own or in a terminal of the panel, loses only itself, and the
shell stays. The exact name is the point: a session that already ended with its
claude, looked for by the start of its name, is a neighbour named after it.

**Remote Control is a second way in, past the panel.** It is claude's bridge
to claude.ai: while it is up, the session is reached from the Claude app and
the web under the account it runs in. Where the launch parameters ask for it,
a session in tmux gets it as a flag of the start, and a stream session from its
holder, which asks claude for it as a control request right after the handshake
and before the project's opening message. From the panel it is switched with
`session.remote`: on the stream that is the same request, and its answer is the
proof; in tmux it is `/remote-control` typed into the terminal, the dialog
that disconnects is walked by reading the screen after every key, and the
switch is known to have landed when claude writes the id of the bridge into the
file of the session or takes it out. What the bridge lets in is decided by
claude.ai, not by the panel's door: whoever holds the account reaches the
session, which is why it is off unless the map turns it on.

**A stream session is reached from the host when the panel is down.** A tmux
session is reached with `tmux attach` whatever happens to the panel; a stream
session has only its holder's pipes, and the panel is the way to them.
`aacpanel-exec -sessions` lists the live sessions with how each is kept, and
`aacpanel-exec -console <name>` asks the running executor to move one to
tmux, after which `tmux attach -t <name>` reaches it. The session is started
in tmux from what the holder keeps it was started with — the project, its
launch parameters and its contour, so it runs under the same account; a holder
that does not keep it is refused rather than guessed around, since a project
found by its directory carries no contour. The executor outlives the service's
container, and a restart of the executor leaves the holders alive. A holder
that ends ends its session: the conversation stays on the disk and resumes in
a terminal.

### Codex sessions

A codex conversation is a thread of the app-server daemon of its codex home
(`AACP_CODEX_HOMES`, `~/.codex` by default), which serves every client of that
home — the terminal client of codex, an editor, the panel. The daemon plays
the holder's part: it outlives its clients and keeps the turn and the requests
that wait for a person. The executor keeps one connection per home, WebSocket
over the daemon's control socket, and dials again ten seconds after the socket
appears or the connection drops — at once when an action waits for it.

**A session can open a Codex helper through `session_open`, send its task with
`send_to_session`, receive a report after each turn, continue by letter and
close the child by name with `session_close`.** The helper is held in the
contour's daemon and takes a letter as soon as the open returns. It starts
without the map's opening message; the task arrives in an envelope naming the
opening session as its parent. Model and effort inherit the map unless the
call overrides them. Helpers use `approvalPolicy: never`, `workspace-write`
with network on, or explicit `read-only`; extra absolute writable roots are
supplied by the caller, not inferred from the host. These choices affect one
start and leave the map intact.

The opening record is separate from the visual parent and names the parent's
conversation as well as its session. Manual grouping grants no reports or
closing rights, and another conversation under the same name inherits none.
A parent restarted anew runs a new conversation that the panel itself names
at the start, and its records move to that conversation; a restart that goes
on keeps the conversation and the records as they are. A tmux parent names no
conversation at its start, and its records stay with the previous one.
The record survives an executor restart; resuming a closed thread from the
archive clears it. `turn/completed` queues one delivery attempt per thread and
turn in the connected executor, including failure and interruption. A separate
worker reads the last agent message with `thread/items/list`: it cannot call
back into the daemon from the connection's reader. The parent receives it
through the stream holder's peer `OpLetter` or a Codex `Link.Send`. A tmux or
gone parent receives none. Delivery failures are logged; this channel has no
persistent report queue or replay of completions missed while disconnected.
Reports over 32 KiB are saved whole in an owner-readable file under the
executor's state directory and the letter names its path; files remain there
for the parent to read. Claude children have no automatic turn reports.

**A codex home pairs with one claude contour by its name.** A home is named
the way a claude config directory is: `~/.codex` is the personal one, any other
its directory's name without the leading dot. It pairs with the contour whose
account bears that name — the registry's name, or the config directory's:
`~/.claude-work` is `claude-work` — and, failing that, with the contour the
installer puts on the map under that name: `work` for the same directory.
Either way the home stands under the account's name, in the executor and the
collector alike, so New of the contour, the archive, the runs of `codex exec`
and the letters of its threads find the same home. A contour pairs with one
home: the home named after the account wins over the one named after the map,
and of two of one name the first listed in `AACP_CODEX_HOMES`; a home left over
stands as a contour of its own name, paired with no account, and so does a home
named after neither. The host does not read the map, so a contour renamed there
after the install pairs only by its account's name.

- **Thread summaries are polled.** Every two seconds the executor lists
  the loaded threads and reads each one, and writes a state file per thread in
  the holders' directory: the claude holder's summary plus `agent: "codex"`,
  the directory, the contour, the home and the rollout. A thread that is
  unloaded loses its file; a subagent's thread is part of its parent's turn and
  gets none, and so does the thread a review runs in. The panel names a thread
  by the tail of its id, `codex-<8 hex>`, and keeps that name whatever the
  thread is called: actions find the session by it. The screen reads the
  thread by the name the panel gave it — a rename, or the project's session at
  a start in tmux — for as long as codex calls the thread by it, and by the
  session of its project otherwise, as a claude session there reads; a thread
  no project holds reads by `codex-<8 hex>`. A push of the thread is titled
  by the same name, and its tap opens the session by `codex-<8 hex>`. A name codex gave the thread is
  not shown: its terminal titles a thread after the first request, and the
  protocol tells that title from a person's name in no way, so a name given in
  codex's terminal is not shown either. The name the panel gave is kept per
  thread in `~/.local/state/aacpanel-stream/named/` (`$XDG_STATE_HOME` moves
  it) for the executor started after a restart.
- **What `thread/read` does not say is read beside it**: the goal of the
  thread and how many background terminals run. Both are asked when the thread
  runs, has changed since the last read or has terminals running, and every
  thirty seconds otherwise; the daemon also tells the goal to the clients of the
  thread. The terminals are an experimental part of the protocol: a daemon that
  does not know them is not asked again on that connection, and the row says
  nothing of them; a new connection asks again, for the daemon may have updated
  itself.
- **A thread codex runs in a terminal of the panel lives in tmux.** The executor
  reads the panes of the user's tmux at every poll and finds the one the
  launcher started codex in on the thread by the command tmux keeps for it, the
  way a close finds it; the row of such a thread in the snapshot says `tmux`
  and names the tmux session, and a thread the daemon alone holds says
  `stream`, since it is reached through the panel as a session on the stream
  is. The mark the screen puts on the session goes by the daemon instead:
  `daemon` for every thread a daemon serves, in tmux or not — the panel
  reaches either only through the daemon and offers it no terminal to watch,
  no window and no move — and `outside` for a codex no daemon serves. The
  desktop's list of sessions alone reads the snapshot's word and marks a
  thread in tmux `tmux`, as it marks a claude session there.
- **A codex no daemon serves is found by its process.** The collector, which
  can read the environment of the user's processes where the executor cannot,
  looks for processes named `codex` and passes by the daemon, its clients
  started with `--remote` and every thread an executor follows. The home is the
  one `CODEX_HOME` names, `~/.codex` without it, and the contour is that
  home's, never the program's: one program serves every home. The thread is the
  one whose writer lock the process holds — the eldest, when it writes the
  threads of its subagents too — and the head of its rollout says whether it is
  a run of `codex exec`. A run is an agent of the claude session that started
  it: the first live session up the chain of parents, none when a codex is met
  first — a daemon hands the environment of the claude it was started from to
  every command of its threads — and the session in `CLAUDE_CODE_SESSION_ID`
  when the chain reaches the top with neither. It stands among the session's
  agents with `agent: "codex"` and the thread as its id, is counted at work
  while its process lives, has no stop, and its feed is the rollout of the
  thread; it is named by `CODEX_AGENT_ROLE`, never by what it was asked. Codex
  as an MCP server of a claude session (`codex mcp-server`) is a run of that
  session for every thread it writes, one per call of its tool, the threads
  of their subagents aside: the server lives as long as the session and keeps
  each thread for a call that goes on with it, so a thread is at work while a
  turn of it runs and idle otherwise. Any other such codex — a run no live
  session started, codex in a terminal with its server of threads built in —
  is a session of its own, outside and only read, as a claude typed into a
  terminal is; the service refuses an action aimed at it. It reads by the name
  the panel gave the thread for as long as the state database of its home
  says codex calls the thread by it, as a thread of a daemon does. The model
  of a run or a thread is the one its last turn named: a turn names it once,
  at its start, and a long one writes megabytes after it, so the record is
  looked for before the tail the fill is read from, reading on from where the
  last look stopped.
- **A run of `codex exec` that is over is found by the call that started it.**
  Its process is gone, and claude's transcript has no word of its thread: the
  reader of a session's state keeps every call of the shell whose command
  names codex — when it went out and came back, the directory, the description,
  whether it went to the background and the paths the command names, never the
  command — and looks for the threads of `source = 'exec'` in the state
  database of the codex home paired with the session's contour, or of
  `~/.codex`, where codex writes when no home is named, for a contour with none
  of its own. A thread is a call's when it was made in the call's directory or
  below it, or in one the command names (a task's directory under
  `.agents/tasks/` counts as the project's), within a minute after the call —
  or, failing every call of that minute, while the call was still out: a
  script that runs codex several times in turn. A call is out until its
  result comes, and one sent to the background until its task ends, not with
  the answer that it went there; one that never came back is out while the
  session lives, and no longer than its last word once it is over. Of several
  calls that may have started a thread, one still out when the thread was made
  wins over one that was back, and of those the earliest not taken yet: calls
  sent off one after another make their threads in that order. A call that
  resumes a thread makes none and takes none. The run stands among the agents
  as `done`, between the start and the last write of its thread, named by the
  call's description, else by its role, never by the first message the
  database keeps; while its process lives the live record wins. The same
  reading serves a live session and a past conversation of the archive, since
  both are read by the state reader. The price: runs started in the same
  second by calls sent off together may swap their names, and a call that
  stays out for hours — a watch on codex's log in the background — can take a
  thread no call of its minute claims.
- **A subscription lasts only while it is needed.** The daemon unloads a thread
  a while after its last client leaves, and a panel subscribed for good would
  keep every thread it ever saw loaded. So the executor subscribes while the
  thread waits on an approval or a question — the daemon sends the waiting
  request again to a new client — while a turn the panel started runs, or while messages of the
  panel's queue wait for it, and leaves once the thread is free and nothing
  waits. A thread the panel started in the daemon is
  the exception: the panel is its only client and holds it until it closes it.
  Each such thread is marked in the holders' directory, so an executor started
  again joins it before the daemon lets it go; the mark of a thread the daemon
  no longer has is dropped.
- **The executor stays blind to the conversation.** The notifications that
  carry it are turned off at the handshake, the state file holds no text of it
  — not the preview of the thread — and the only words it keeps are those a
  person decides on or gives: the command of an approval, the change to files,
  which the request names and the item of the turn holds, the question or the
  form a thread waits on, the name and the goal of a thread, and the messages
  that wait in the panel's queue. It reads the notifications that carry no
  more: the status of a thread, its settings, its name and goal, how full its
  context is and the limits of the account.
- **An approval is answered with the daemon's own decisions.** The options are
  the decisions the request offers, in its order, and the pick goes back as it
  came; the first answer of any client wins, and the others are told the
  request is gone — an answer in codex's own terminal closes the request on the
  phone.
- **Codex waits for a person on three more requests, and each is answered the
  way the screen already answers claude.** A grant of permissions the sandbox
  does not give is the permission sheet: for the turn, for the session, or
  none, the grant sending back what the request asked for. A question of plan
  mode is a question of claude's: the executor keeps it in the state file of
  the thread in the shape the collector keeps claude's, named by the item of
  the call that asks it — the id the rollout names the call by, so the card of
  the answered question closes it — and the feed offers it with the state of
  the conversation, which goes with every answer, a question or none, for a
  question answered leaves the screen only when a state without it comes.
  `session.answer` sends the labels picked,
  the person's own words, and a note beside a pick as codex's own clients mark
  one; putting it away answers every question with the note that the person
  will answer in the conversation. What an MCP server asks through codex is a
  question a field when it is a form — the answer is a value of the kind each
  field is of, a required field left empty stops it, and putting it away
  declines it — and the permission sheet otherwise: a page to open, a link
  under the request where its address is on the web's own schemes, accepted
  with no content, a yes or a no, or what the panel can only refuse — a check
  of the person only codex can make, a form of more fields or values than the
  sheet of a question shows, for one left out could be required, and a request
  in a mode the panel does not know. The question of plan mode is an experimental part of the protocol.
- **New of a project whose agent is codex, or with Codex picked on its sheet,
  starts a thread in the daemon of the project's contour**, the home paired
  with the contour; a contour without one is refused with that reason. The
  panel starts the thread with `thread/start` in the project's directory,
  with the model, the effort, the approvals and
  the sandbox of the map where it chose them, and the panel's MCP server
  beside the servers of the home — the `config.toml` of the home
  decides the rest — and New answers with the session's name, `codex-<8 hex>`,
  so the screen opens it as it opens a claude session. Where it lives is the
  map's `codexTransport`:
  - *In the daemon* (the default) the panel is the thread's client and sends
    the first message of the project as its first turn.
  - *In tmux* codex resumes the thread in a tmux session under the project's
    session name, started through the launcher as claude is: `codex resume
    <id> -C <directory>` with the home in `CODEX_HOME` and the first message as
    its prompt. The thread is named after the project's session at its start:
    the daemon writes a thread to the disk at its first turn or its naming,
    and codex resumes only a thread written there. The choices of the map are
    not passed again as flags: the thread has them from its start, the effort
    among them, and codex given a configuration override — the only way a
    terminal takes an effort — refuses a thread the daemon holds loaded. The
    id in the command tmux keeps for the pane is how a close finds the
    session.
- **The panel starts the daemon where none runs.** A home without its control
  socket gets `codex app-server daemon start` with the home in `CODEX_HOME`, in
  a transient unit of its own and in the project's directory: started from the
  executor the daemon would live in the executor's unit and its sandbox, and
  from a session in that session's unit, going down with whichever stops
  first. The executor waits for the socket in that home and for its
  connection; a daemon that puts up no socket there is named — most often a
  program that chose the home by itself. The program is `AACP_CODEX`, or
  the standalone `~/.codex/packages/standalone/current/bin/codex` when present,
  or `codex` from the executor's `PATH`. An update waits for running turns
  before restarting their daemon.
- **The panel owns updates of the standalone Codex installation.** The host's
  Machine page shows both installed agent versions and offers `codex.update`
  and `claude.update`, fixed host actions through the confirmation gate and
  executor audit. `aacpanel-codex-update.timer` asks the same executor every
  six hours; its installed file at
  `~/.config/systemd/user/aacpanel-codex-update.timer` is the ownership marker
  other host updaters check before doing anything. Uninstall removes it.
  The updater keeps the shared `~/.codex/packages/standalone/current` link:
  contour wrappers and the panel use one installation. It downloads the native
  installer and pins the release from its release channel, clears `CODEX_HOME`,
  and sends its shim to `packages/standalone/bin` with that directory in PATH,
  leaving the user's command wrapper and shell profile alone. It holds the
  lock file `.atlas-update.lock` in the package root for the handover, so it
  and another updater of the same installation that takes that lock never
  switch the link at once — the timer file alone does not stop an updater that
  started before it was installed; a lock already held refuses the panel's
  update. Each package root keeps its current
  release and the previous selection for rollback. Older releases are removed
  after a successful restart; a pending restart keeps its packages intact.
  The installed binary runs the same protocol checker as `cmd/codex-contract`.
  A failed protocol check restores the previous link and leaves daemons alone.
  A compatible release is also installed in each contour's native
  `packages/app-server-daemon` root: daemon lifecycle commands use that selection
  regardless of the CLI that invokes them. Explicit release selection pins these
  packages and disables their latest-channel updater. This costs a package per
  home and its download. The panel restarts each idle daemon in its own transient
  unit, waits for its new version at the handshake, and checks its panel hooks.
  A home without a control socket has no daemon to restart: the report names
  it as not running, its next start takes the selected package, and its hooks
  are checked with a private stdio app-server that exits after the check.
  The link's existing poll supplies the status of every loaded thread, including
  children. A running turn or unknown activity defers the restart; the host page
  names the pending contour and the next timer or button run tries again.
  Starting a panel turn is serialized with the idle check and restart command.
  The collector carries the versions and verdict to the host screen and the
  existing Codex attention mechanism. A failed restart or download remains
  separate from the protocol verdict. Downloads can take up to fifteen minutes;
  session actions keep their shorter timeout.
  Claude retains its own updater. Its button invokes `~/.local/bin/claude update`,
  never a contour wrapper found through PATH. Installed versions are sampled
  every minute so Claude's automatic updates appear too. The button runs the
  same feed runner as `make stream-contract` after updating, in a separate
  transient unit so the executor's transcript restriction stays in place.
  Its confirmation names the cost: a temporary Haiku session on the default
  account, resumed in print mode and a private console. The host page keeps
  the verdict; automatic updates outside the panel do not start this probe.
- **The protocol is held to a contract across Codex releases.** A
  release may drop a field the panel decodes, rename a status or require a new
  param without a word, and the panel would meet it as a zero or a refusal.
  `make codex-contract` has the installed codex write the schema of its
  protocol — no thread, no tokens — and walks it along
  `internal/codex/contract/uses.txt`, every method, field and value the panel
  reads or sends, against the reference of the release last passed, kept in
  the tree cut to those places. What the panel uses that is gone or changed is
  red with its name; a new kind of item, a new status, a notification nobody
  turned off is a warning. It is not in `make check`, for codex is not on every
  machine; the comparison itself is, over fixtures made of the reference. The
  rollout the feed reads, the state database of a home, the `_meta` codex adds
  to an MCP server's request and the words of a refusal are no part of the
  schema, and the check does not see them change.
  The ordinary run also asks codex in `CODEX_HOME` (`~/.codex` by default)
  for `hooks/list`, using the installer's check of the question relay and
  installed optional hooks. With no daemon socket, it starts the same program
  used for schema generation as a private stdio app-server for this call.
  Missing, untrusted or disabled hooks are red: a changed trust formula must not silently stop delivery. A saved
  `-schema` directory checks only the protocol and reports trust as unchecked.
- **A daemon that moved past the reference is said once a release.** The
  daemon names the release it runs in the user agent of its answer to the
  handshake, and a daemon that updated itself comes back on a new connection,
  so the executor learns the release without a process of its own. It writes
  it at every handshake by contour in
  `~/.local/state/aacpanel-stream/codex-release/` (`$XDG_STATE_HOME` moves
  it), beside the release of the reference it was built with; the collector
  puts both in the snapshot. An update verdict is shown once per release and
  verdict; without a verdict for that release, the service pushes when they differ: codex
  moved to one release, the protocol was checked against another, run
  `make codex-contract`. The reason is held while the daemon runs that
  release, so it is said once — not at every look, not after the service
  starts again, which keeps what it told, and not after the executor or the
  machine does, which the file outlives; a release further on is said once
  more, and a daemon brought level is forgotten without a word. The reference
  travels in the executor's binary: a reference written again counts once the
  executor is rebuilt from it.
- **A close lets the thread go.** The turn that runs is interrupted, the panel
  stops holding the thread and leaves it, and the tmux session whose pane the
  launcher started with codex resuming that thread is killed by its exact
  name. The thread is not archived in codex: the daemon unloads it once no
  client is left, its rollout stays, and it leaves the list of sessions —
  unless another client starts a turn in it — for the archive of its contour,
  where Resume brings it back. **A restart is refused**: the daemon keeps the
  thread, and no process of the panel's is there to start again.
- **A thread that is over stands in the archive, and Resume joins it again.**
  The collector reads the threads of every codex home off its state database
  and from no rollout — the rollouts of a busy home run to gigabytes, nearly
  all of them runs of `codex exec` — and puts them among the conversations of
  claude of the contour the home pairs with, by the time of their last
  change. Only the conversations of a person stand there: a thread started in
  codex's terminal or by a client of the daemon, the panel among them. A run
  of `codex exec` stands among the agents of the claude session that started
  it, a subagent's thread is part of its parent's turn, a thread a person
  archived in codex leaves the panel's archive as a deleted claude transcript
  does, and a thread on the list of sessions is left out of it. The row keeps
  no words of the conversation: it reads by the name the panel gave the thread
  while codex still calls it by it and by its directory otherwise, and says
  the model, the effort, when it ran and the tokens it spent; it opens the
  feed of the rollout. Resume (`session.resume` with `agent: codex`, the
  thread and the contour of its home) takes no project — the daemon knows
  where the thread ran — and the panel joins the thread in the daemon of that
  contour with `thread/resume` and holds it as a thread it started: it is a
  live session again, `codex-<8 hex>`, and nothing runs until it is written
  to. No daemon is started for it, since a resume brings back what a person
  had: a home whose daemon is down is refused with the way back in codex's
  own terminal, and a subagent's thread as a part of its parent's turn.
- **Only what the protocol does is done.** A message starts a turn on a free
  thread, and so does a letter of another session, in its envelope; a stop interrupts the turn — the inner turn of a review, which the
  daemon names when it refuses the root one — Esc declines what waits, a file
  goes where a claude session gets it — a picture into the turn as a file codex
  reads itself, any other file as its path — and a secret the thread asked for
  is saved where a claude session's is and its path goes as a message. The commands of codex's own
  terminal go as `session.command`, each saying what it does in a field of its
  own rather than in words to parse: `compact`, a turn that summarises the
  conversation; `review` of the uncommitted changes, of the work against a
  branch, of one commit or of what the person describes, a turn of its own and
  so refused while one runs; `goal`, set with its budget, paused, resumed or
  cleared — an active goal starts a turn at once and the next ones after it, and
  pausing does not stop the turn that runs; and `stop`, every background
  terminal of the thread. `session.rename` names the thread — any words on one
  line, since the name is a title and not the key the session is found by —
  `task.stop` stops
  one background terminal by the id codex gives it, and `agent.stop` an agent
  of the thread by the id of its own thread: the turn the agent runs there is
  interrupted, the agent waits for a task more, and the thread that started
  it is told nothing — a thread the session did not start is refused. The
  models, the MCP servers
  and the skills of a codex session are codex's own lists, answered in the
  shapes of claude's — the models of the daemon of the session's contour, which
  lists the catalogue of its own release, with no catalogue of claude's account
  beside them; `/api/codex/models` takes the contour too. The MCP servers
  are asked of the thread one by one, which starts none of them: a list of
  them all has codex start every stdio server anew to discover it — a process
  on the host, or a container, at every opening of the screen — while a list
  of one by name answers from the thread's own connection. The names come
  from what codex says without starting anything: the servers of config.toml
  as the thread's directory sees it, those of every plugin installed and
  enabled, and the panel's own server, which a thread has from its start.
  The price is that a server edited since the thread started reads as the
  thread runs it; the check against config on the screen is the discovery,
  asked for, and tells it. Every other action over a codex session is refused by name: none of the
  claude ways — keys, signals, a holder's socket — reaches it.
- **A reconnect starts every MCP server of a thread anew** (`session.mcp`
  with `reconnect`), the way an update of the panel reaches a thread that
  runs. Codex starts the servers of a thread once, when the daemon loads it; a
  reload of its configuration restarts only a server whose configuration
  changed, and an update of the executor leaves the path of its program as it
  was. So the panel takes the thread out of the daemon and back in: it
  archives the thread, which unloads it at once whoever else follows it,
  takes it out of the archive and resumes it with its own server beside the
  servers of the home, as at the start. A client that follows the thread hears
  the archive from the daemon within milliseconds; one that only lists
  threads, the picker of codex's terminal among them, sees it go and come back
  on its next refresh of the list, a moment the panel cannot shorten. Every
  server of the thread starts again, not the named one alone. The conversation, the name and the goal
  are on the disk and stay; the permissions the thread started with are not,
  and the settings a resume read just before go back with
  `thread/settings/update` — a daemon that refuses it leaves the thread on
  what config.toml says, and the answer says so. What the unload would cut
  or cross is refused rather than risked: a turn that runs, which the archive
  interrupts, background terminals, which it ends, agents at work and a goal
  codex works towards on its own, whose work goes on in the thread, and a
  thread not on the disk yet, which no resume brings back. So is a thread the
  panel does not hold: another client keeps it, codex's terminal or an app,
  and the archive would take it out from under that client. A server is
  switched on and off in codex's config.toml, and the panel refuses that. The
  price: a turn another client starts in the moment between the look and the
  archive is interrupted, and a client of the thread the panel does not know
  of — codex resumed in a terminal on a thread the panel started — sees the
  thread archived and back, and stays its client.
- **A message goes into the turn that runs**, as claude takes a message
  written during a turn at its next step: `turn/steer` names the
  turn, so the message goes into that turn or nowhere, and a free thread
  starts a turn with it. A turn that ends between the look and the steer
  leaves no turn to take it, and the message starts a turn of its own. A turn
  that takes no message — a review, a compaction — or one the daemon has not
  listed yet leaves it in the panel's queue, and a message after it goes
  behind it rather than past it. The queue gives them to the first turn that
  takes them, or starts a turn with the first once the thread is free — the
  daemon says so to its clients, and the poll finds it besides — and a turn
  that follows another with no free moment between, as a goal codex works
  towards starts the next, takes them too. A letter of another session goes
  the same way, into the turn that runs, where a busy claude reads a letter
  only once its turn is over: a codex turn lasts hours, and a letter held
  until its end comes after the work it was about. A message waiting in the
  queue is taken back (`session.unqueue`) by the name the panel
  gave it; one a turn has is beyond taking back, as one claude has read. The
  queue outlives a connection that dropped and an executor started again: it
  is kept on the disk beside the state files, the person's words in a file of
  the owner's, until a turn takes them. It goes with its thread — closed by
  the panel, or unloaded by a daemon started again — since a thread the daemon
  let go is not the panel's to bring back. A message the daemon refuses to
  start a turn with is dropped with a line in the executor's log rather than
  held in front of the rest.
- **Waiting input is read from the outbox by the collector.** Codex writes no
  rollout row until a turn takes the message. The feed carries the outbox as
  queue state, including each message's ID, so a reload or a second device
  keeps its edit and take back buttons. Removal from the outbox removes the
  waiting row. A file send carries no message ID in the action protocol, so
  codex's outbox can show it waiting but cannot offer cancellation by ID.
- **The model, the effort, the permissions and the plan of a running thread
  change for its next turns** (`session.set`), with `thread/settings/update`.
  A model and an effort are held to the catalogue of the home's daemon first:
  the daemon takes any name and fails only at the turn that runs on it. The
  permissions are three modes — read only, asking, and approval left to codex's
  own reviewer — the presets codex's terminal offers, less full access, which
  is chosen at the start or in codex itself; a thread whose settings match none
  is shown as custom. Plan mode carries a model and an effort of its own, so
  the ones the thread runs go with it. The method is experimental: a daemon
  that dropped it after updating itself still takes a model and an effort with
  the next turn the panel starts, and the answer says the pick waits for the
  next message that starts a turn; a mode and the plan are refused with the
  daemon's reason. The
  daemon tells the settings to the clients of a thread, so a thread the panel
  only reads shows its model and effort and no mode until the panel joins it.
- **The fill of the context and the limits of the account come from the
  daemon.** It tells the clients of a thread how full its context is after
  every request, and the state file carries it; the rollout says it when the
  panel heard nothing newer. The limits are read when the link connects and
  every five minutes — the daemon sends them after a turn only to the clients
  of the thread that ran it — and kept by contour beside the state files; the
  snapshot puts them on the contour's row of limits next to claude's, with the
  week codex counts, its window of 10080 minutes, as the seven days.
- **The feed is the rollout, read by the collector.** Codex writes every thread
  to a file of its own under the home's `sessions`, and the collector reads it
  as it reads a claude transcript: the messages, the summaries of reasoning, the
  commands with their output and the changes of files, from the records codex
  writes once an item is done; the plan of plan mode, the start of a review and
  its findings, every goal set, changed or cleared and the turns codex starts
  for it,
  and each question of plan mode with its answer, drawn as an answered question
  of claude's. The raw records of the model beside them say the same again and
  are not shown. An agent the thread starts is a card that opens the agent's
  own thread, and every other call to agents is a call among the calls. Codex
  has two sets of tools for agents: the first writes an item of every call; the
  second writes the call and its answer only as raw records of the model, with
  an item beside them of what the call did to the agent — started it, spoke to
  it, interrupted it — and one of its own when the agent's turn is over. A call
  of the second set is drawn from its raw record and opens with its answer;
  its items say how the agents stand, and an agent interrupted or done with its
  turn is a line of the feed besides. What the second set sends an agent
  travels encrypted and is never shown: only to whom and that it went. The
  messages between the thread and its agents are raw records of the model as
  well, encrypted all but the last answer of an agent: that one is a letter
  from the agent, and the others are calls among the calls, from whom or to
  whom. A picture codex looked at, a web search — what it looked for, opening
  with what it found — a wait codex set itself, drawn from its call while it
  waits, and what a hook put into the turn are calls among the calls too. A
  review runs in a thread of its own and codex writes its items into the
  rollout of the thread that asked for it: the feed shows its work and none of
  its words, which codex says to itself.
- **A question codex asks without waiting is a question of claude's: one
  sheet, another way for the answer.** Outside plan mode codex asks with a
  tool that does not wait: the question goes to the person as an answer
  delivered apart, its questions and their options beside the words, and the
  turn goes on. Nothing in the protocol takes the answer or clears the
  question: the next message of the person after it is its answer. While no
  such message has come, the question stands on the thread's row as a
  question of claude's does — the list says the session asks and a push goes
  out — and the row stays as busy or as free as the thread is. The collector
  reads it off the whole rollout, not the end the screen holds, and the state
  of the conversation carries it in the shape of a question of claude's —
  each question with its options, one pick or words of one's own, no header
  — named by the latest question and marked async, after the question or the
  form the thread waits on, which comes first. The screen offers it in the
  sheet a question of claude's is answered in: the pick, the words of one's
  own, the note beside a pick, Send and putting it away. A permission the
  thread waits on stands before it, and a single question does not go at
  the tap of an option, since the note is as much of the answer as the pick.
  Send writes the answer as words — a head saying it answers codex's
  question, each question a line with what it got indented under it, the
  note after `note:` — and sends them as a message naming the question
  (`session.send` with `asked`), which goes into the turn that runs
  (`turn/steer`) — codex asked so as to go on — or starts a turn on a free
  thread; putting the question away sends that the person will answer in the
  conversation. The message is the panel's, told by the
  shape of its whole text as the panel's other messages are, and the feed
  draws it as the card of the round answered, as claude's answer is drawn,
  where it was sent: on its way, until the thread reads it — when its turn
  takes it, which can be minutes later — and after. The card of the
  question itself gives way to it; a question a message answered in words of
  its own stays a card that says so, and one nobody answered says that
  where no sheet offers it.
- **The agents a thread started are its agents**, listed as a claude
  session's are and each opening its own thread. The collector reads them
  off the rollout of the thread, on from where it last stopped: an agent
  stands as the last word codex wrote of it there — at work from its start
  until its turn is over, it is interrupted or it fails, at work again with
  a task more. The thread hears nothing of a turn of the agent's stopped from
  outside — by the panel, in codex's own terminal — so an agent the thread
  holds at work is at work only while the rollout of its own thread says a
  turn runs, and waits otherwise; that rollout gives the agent its model and
  how full its context is besides. Codex lets the thread go once its own turn
  is over while its agents work on, so the row of such a thread is busy with
  its turn over, as a claude session waiting for its agents is: the screen
  says the agents work, and a message starts a turn at once. The price: an
  agent whose thread died with the daemon in the middle of a turn stands at
  work.
- **The way back is codex itself.** With the panel down, `codex resume <id>`
  in a terminal joins the same thread on the same daemon, loaded or not,
  whoever else follows it; a model, approvals or a sandbox given as flags are
  applied to the thread. Given a configuration override (`-c`, the effort
  among them) it refuses a thread the daemon holds loaded: it says the
  conversation is open in another app, and its retry succeeds only once the
  daemon has unloaded the thread, a minute after its last client left. So the
  way back is the command without `-c`.

---

## The map

The map is contours, their groups and the projects on them. What a session of a
project starts with is not a set of form fields but a schema
(`internal/schema/schema.go`): each launch parameter says where it may be kept
(a contour, a project), what its options mean, what an absent value leaves to,
how a project's value lies over the contour's, and when a running session takes
a change — now, on a move between tmux and the stream, or at the next
start. The screens are drawn from it (`GET /api/profiles/schema`, answered
without the database), the claude launcher reads exactly its common keys and
claude's, and a test holds the two lists equal. Retired keys are named with the
reason, not silently dropped.

**Every key is of an agent.** `agent` — Claude Code or Codex, Claude Code where
nothing is said — is what New and a restart of a project start; it and the first
message are common, every other key is claude's or codex's (`Param.Agent`), and
a settings page lays each agent's keys out under a tab of its own. The codex
keys — where codex lives, its model picked from what the codex daemon lists
(the question `codex.models`), the effort, the approvals and the sandbox — are
saved and checked like the rest, never asking and the sandbox off are held as
`bypassPermissions` is, and an unset one leaves it to the `config.toml` of the
codex home. New of a project whose agent is codex starts a codex session, as
"Codex sessions" says. The sheet that confirms New offers both agents, the
project's taken and marked, so Open alone starts what the map says; the other
one picked there goes as `params.agent` and is laid over the launch for that
start alone — the map stays as it was, and the next New offers the project's
agent again. Codex is offered switched off, with the reason, where the
contour has no codex home (`codexHome` of the contour in the map, from the
collector) or the project is not on the map, and the service refuses codex for
a session the map does not hold. A resume goes on with what the archive row
names rather than with what the project starts: a conversation of claude, or a
thread of codex with `agent: codex` and the contour of its home, which takes no
project, as "Codex sessions" says; any other agent named for a resume is
refused. A restart of a claude session of a project whose agent is codex is
refused: a codex thread does not take the place of a claude conversation. A
claude conversation resumed or moved between tmux and the stream stays
claude's and goes on.

**Effective values carry their layer.** A project's value comes from the
project, its contour, the account, the panel's own default or claude
(`internal/schema/effective.go`). The account layer is what the collector reads
from the account's `settings.json` — the model, the effort and the default mode,
never the environment — so a bare command is not read as "nothing set".

**A save refuses what the launcher would refuse** (`internal/schema/check.go`);
a change goes as `launchSet` and `launchUnset`, so two screens saving different
keys keep each other's work. A draft is answered without being written:
`POST /api/projects/{id}/preview` gives the command the next launch runs, built
by the launcher's own code word by word, and `POST /api/profiles/{id}/preview`
what a contour's projects would start with.

**A session name is the machine's, not the contour's.** tmux keeps one
namespace for every session, and a project with no session name of its own
answers to the name of its directory. A save that would make two projects answer
to one name — a new project, a changed name or directory, a deleted project taken
back — is refused with the project that holds it: the second of the two would
come up as `name-2` while the panel waits for it under the name, and each
project's screen would count the other's sessions as its own. A pair already in
the map stays editable until one of them is renamed.

**A group is a shelf, not a level of the launch.** What is said for all its
projects is written into each (`POST /api/groups/{id}/set`), and a full group
moves its projects onto another of the contour before it can be deleted
(`/move`). A value raised to the contour can be taken out of the projects that
store the same (`POST /api/profiles/{id}/unpin`). Each of these is one
transaction.

**The map keeps a journal of its own** (`internal/store/profiles_journal.go`):
every create, save and delete of a contour, a group or a project is written in
the same transaction, field by field, with who made it; a deleted project can be
taken back from it (`GET /api/profiles/journal`, `POST
/api/profiles/journal/{id}/undo`). A save that changed nothing writes nothing.

**A contour stands for an account the host already has.** Where a contour
router holds the accounts, the collector reads its registry and marks each
account with the directory prefix that routes a session into it; a new contour
is taken from the accounts the map has none for, and a routed contour's paths
are the host's, shown and not edited.

**A project moves to another contour with its Claude history** (`project.move`).
The service locks the project, its groups and their accounts, prepares the map
change and its journal in one transaction, then asks the executor to move the
files. The executor checks every collision first: identical files merge, a
different file or a symbolic link refuses the move. It carries the exact
`projects/<slug>` tree, including subagents and memory, and entries keyed by
those conversation IDs in `file-history`, `session-env`, `tasks`, `todos`,
`plans`, `teams` and `session-models`. Shared prompt history, paste caches and
word-named plans have no session ownership and stay; Codex threads stay with
their original home. The project's working directory does not move. Where a host router chooses
accounts by directory, its route must already point to the destination; an
inconsistent route refuses before copying because that wrapper can discard
the config directory passed by the map. The panel does not rewrite a router's
registry or its authentication rules.

A live session in any contour, including a worktree or Codex, refuses the move
by name. Panel launches and transfers serialize on the executor; the host
checks live sessions again after copying. The source trees wait in
`.aacpanel-project-moves/<id>` until the map commits. A failed map commit puts
them back and removes only files this move added to the destination. A lost
COMMIT reply is checked through another database connection before undoing.
If that outcome cannot be established, or either process stops mid-transfer,
the recovery record and backup stay on the host; another move and a panel
launch of this project refuse until the host administrator resolves the record
against the map. Cross-process commit cannot survive both sides disappearing
as an atomic filesystem transaction; retained originals are its recovery cost.

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

The list of actions is closed.

| Family | Actions |
|---|---|
| containers | `container.start`, `container.stop`, `container.restart` |
| stacks | `stack.up`, `stack.down` |
| agents of the host | `codex.update`, `claude.update` |
| sessions | `session.open`, `session.resume`, `session.close`, `session.restart`, `session.kill`, `session.send`, `session.letter`, `session.answer`, `session.dismiss`, `session.stop`, `session.background`, `session.escape`, `session.file`, `session.command`, `session.shell`, `session.set`, `session.mcp`, `session.permit`, `session.switch`, `session.unqueue`, `session.rename`, `session.role`, `session.remote`, `session.parent`, `session.detach`, `session.intent`, `session.pin` |
| windows | `window.open`, `window.close` |
| background work | `task.stop`, `agent.stop`, `observer.stop`, `alarm.cancel` |
| disk | `project.create`, `project.move` |
| terminals | `term.start`, `term.close`, `term.rename`, `term.console`, `tmux.close` |
| secrets | `secret.put`, `secret.drop` |

What is deliberately not on the list: removing containers, images and volumes,
`docker exec`, editing compose, package operations, restarting itself. The two
agent updates are the narrow exception to packages: the program and the source
of each are fixed in the executor, and the request carries neither.

**A terminal of the panel is the owner's shell on a tmux server of its own.**
It lives on the socket `tmux -L aacpanel-term`, apart from the sessions the
panel starts. Its id and place distinguish it from other sessions on that
server. It opens only in a place: the home directory or the
directory of a project of the map. The service takes the place from that list,
never from a string on the phone, and the executor checks again that it is a
directory whose real path lies inside the home directory or a project root. The
session is started with no command line — it runs the default shell and nothing
else — so nothing sent from the phone is ever run as a command: it reaches the
shell as keystrokes, over the same bridge as the session terminal, and on the
same listeners only. The first terminal starts the server from a unit of its
own, as a session does: started inside the executor, every shell would inherit
its sandbox and die with its restart.

**Terminals lists every live tmux server of the user.** The executor reads
`tmux-<uid>` under `TMUX_TMPDIR` or `/tmp`, accepts only sockets owned by the
user and never follows a socket symlink. Dead and unresponsive servers are
left out; each server gets a bounded `list-sessions` call, and only terminals
created by the panel get a captured preview. Foreign rows name their server
and offer attach and confirmed close, while the panel's terminals keep their
rename and desktop window controls.

The service passes an opaque id, never a socket path or tmux target. The id
binds the server's socket identity and process id to the tmux session id and
creation time. Attach and `tmux.close` enumerate again and refuse an id
absent from that list. This costs another listing at each operation and keeps
a stale card from addressing a different session after a server restart.
Live Claude processes are matched to the panes of every window using the
panel's session files, not names or foreground commands. Their rows open the
session screen and close through `session.close`; both terminal close actions
refuse to kill a tmux session that contains one.

**Every unit the executor starts runs on the cores of the machine
description.** A session — the launcher's unit, which keeps the tmux server or
the holder after it — its window, a terminal of the panel, a codex daemon and
an observer each get `CPUAffinity=` from `AACP_CORES`, one value for all of
them, and none at all where it is unset: then the system puts them. The value
is read once, at the executor's start, and checked there against the form
systemd reads and the cores the kernel lists. A value that would fail every
unit, or hold it to no core, is logged once with the key and the reason; the
executor starts without affinity. A description carried from another machine
must not take the panel down, at the cost of leaving core placement to the
system until the setting is corrected and the executor restarted.

**The host's main session is not closed from the panel, but it is restarted.**
The session living in the home directory is the one the panel itself lives
next to, and it has no close button, just as the panel's own container has no
stop. Where several sessions live there, the main one is the one under the
name the host gives its main session (`AACP_HOME_SESSION`), and the others
close as any session does; a lone one is the main session whatever its name. The executor tells a session's directory by the link under `/proc` and,
where that is refused, by the directory claude writes into the file of the
live session: the sandbox of the executor's unit runs in a user namespace of
its own, and the link of a process outside it does not open there. `session.restart` ends a session the gentle way — the same wait for the
transcript — and starts a new one with an empty context, or, asked to go on,
resuming the conversation it closed; the old transcript stays in the archive.
A restart asked to go on may carry the message the session wrote itself to go
on with, and that message comes first in the resumed conversation, in place of
the map's message after a restart, as "A session restarts and closes itself"
says. A session whose working directory belongs to a project
of the map comes back as that project: its launch, its contour, and the
message after a restart as its first — a restart without them would bring it up
in another setup, possibly under another account. It comes back in the
directory it ran in, not in the project's own: a session of a worktree goes on
working in the worktree. A session no project holds
is restarted only if it is the host's main one. The phone's live cards offer
Restart and Close directly, with Restart alone on the main session, Close
alone for codex and neither for a session outside the panel. A project session
can also ask for its own restart through the local listener, naming its
conversation, which is how the context guard brings it back.

**A conversation is resumed where it ran, with the parameters of the project
it belongs to.** Its transcript is kept by the directory it ran in, so a
session from a worktree or a subdirectory comes back there, not in the
project's own directory. The project is the nearest directory above it that is
in the map, or, for a git worktree kept beside its repository, the project of
the main checkout — the agent reads that off the worktree's `.git` file, since
the service does not see the disk. It reads it for the directories its walk of
the roots finds and for the directory of every live session: the walk stops at
the first directory that looks like a project, and a worktree kept under one —
a folder of worktrees beside the repositories of a contour — is never among its
rows. A directory the snapshot places nowhere is asked about when a session is
opened or resumed there: the agent lists the worktrees of its repository, main
checkout first, so a worktree made a moment ago opens as its project without a
session working in it first. The climb stops below a project root: a
project that is a root itself, like the home directory, holds the machine's own
session, and taking it for the owner of whatever lies under it would start that
in another account. A thread of codex looks for no project at all: the daemon
of its home knows where it ran, as "Codex sessions" says.

**Kinds are split by intent, not by convenience.** Answering a question and
dismissing a question are different actions, because their consequences differ.
A file is not a field inside a message but a kind of its own: the panel asks the
executor what it can do and greys out buttons from the answer, and a field does
not change that answer — an old executor would accept the request and silently
throw the file away.

**A message is typed into the session, not handed over as a paste.** Claude in
tmux folds a large write into a paste of its own making and marks it in the
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

`session.send` from the composer falls back to the session's message socket
when no terminal is reachable, and reports that the text was sent as a letter
instead of typed. Voice input requires a stream or a reachable terminal and
refuses this fallback: a letter would change the owner's words into another
session's request. The service passes the voice origin to the executor in the
same action request; the device's display name does not select this behavior.

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

## Observers and alarms

A session that waits on something outside it — a CI run, a deploy, a log, a
build through the night — has ways of its own to wait, and the process of the
session holds each of them: a background shell or a Monitor of claude has a
ceiling on how long it lives, and a restart of the session ends it. An
**observer** is held by the host. The session hands the panel a command with
`observer_start`; the executor runs it in a transient unit of the user's
manager, reads what it prints and wakes the session with a letter — on every
line, on a line a pattern finds, or only at the end — and at the end of the
command whatever was chosen, since a session waiting for a line of a command
that died would wait for good. Lines that come within the seconds the session
names of the first one go in one letter, so a burst is one wake and the session
is woken no more often than that. `observer_list`, `observer_tail` and
`observer_stop` read and end the observers of the session that calls them and
no other's. The words are the panel's own: a project may keep watchers of its
own under another word, and the two stand side by side.

**Only a session starts an observer.** The panel's server under the session
reaches the executor on a socket of its own, `aacpanel-observers/sock` in the
runtime directory, not in the directory mounted into the service's container:
an observer runs any command, and through the action socket the service would
run any command too. The server names the session it serves — the conversation
and the process of its claude, the thread of a codex — and the executor takes
that only for a live session it finds itself. The start is not allowed without
asking: it runs a command on the host, and claude asks the person first as it
asks before a command of the shell. The list, the tail and the stop touch only
what the session started and are allowed.
The action journal names the session and its observer's name, command and
directory, or its alarm's name and first ring and period. The session's MCP
server records a successful start through `/api/actions` as `observer.start`
or `alarm.set` without execution: recording travels beside the private start
so the service gains no way to run its command.

**An observer runs in a unit of its own, and outlives the executor.** The unit
runs the executor's own binary in its observing mode, which runs the command
with `sh -c` in the directory the session named, with nothing on its input,
with the session's PATH — the tools the session reaches by name — and with no
other part of the session's environment: a token the session holds in a
variable stays out of a unit and off the disk. It writes what the command
prints into the log of the observer and, when the command ends, how it ended:
the code, the signal, or the memory ceiling, which the memory controller of
the unit counts. Everything of one observer lies in a directory of its own
under `~/.local/state/aacpanel/observers/` (`$XDG_STATE_HOME` moves it): what
it was asked, whose it is, how far its log is read, the log and the end. An
executor started again takes the directories back and reads on from where the
last one stopped: a line the last one woke the session for wakes it no second
time, and a line printed while no executor ran wakes it now. What is read is
kept as read only once its wake went, so a crash between the two costs a
second letter, never a lost one. A unit no directory names is stopped.

**The ceilings are the session's own, idle.** A session on the stream that
waits holds 260 to 320 MB in its claude and 470 MB and more with its MCP
servers, and spends 0.3 to 0.7 per cent of one core, about 3 per cent at work.
An observer is a watcher and gets no more: `MemoryMax=384M` with no swap,
`CPUQuota=5%`, niceness 10, and the cores of every unit of the executor
(`AACP_CORES`), the system's choice where the description names none. A
command that polls every few seconds runs at its pace; a loop that runs away
gets a twentieth of a core. A session keeps five observers, the host twenty,
and the log of one is two files of a megabyte — the full one is moved aside
and the one before it goes — so the observers of the host never write more
than forty megabytes, and that goes with them.

**A wake is a letter, not the person's words.** On the stream it goes through
the holder, which writes it to claude as a message whose origin is another
session, with the text in the envelope claude reads a letter in, signed by the
observer and with no address to answer to; claude frames it for its model as a
letter and keeps it for the end of a turn that runs. A letter that names no
permission class of its sender is held for the person by a session that runs
without prompts, and nobody is there to let it through, so the holder names
the session's own class — bypass for one that runs tools without asking, plan
mode entered from it among them, prompting otherwise. A thread of codex gets it
through its daemon, as a letter of another session goes: into the turn that
runs, and as a turn of its own on a free thread. **In tmux the letter is typed
into the composer**, with its origin written before the envelope so the model
knows the person did not type it. Control characters in command output are
written as visible escapes so a log cannot become terminal keys. The executor
uses the composer's typing path and its decision about which screen owns the
keyboard. It reads the pane before every write, including clearing the line
and pressing Enter after the text:
an open permission, selector, question or plan approval gets no keys. An
unreadable screen is refused too. A refusal follows the keeper's existing
failed-wake path: the wake stays pending, each failure spends an attempt, and
after ten failed attempts its lines or ring are dropped with a log entry. A
session on the stream whose holder cannot take letters is refused at the
start; restarting the session brings a holder that can.

**An observer ends with the session that started it.** It belongs to the
process of a claude, by its pid and the start of it, and to the thread of a
codex; the executor looks at every observer each second, and three looks in a
row that find its session gone stop the unit and take its directory away.
That is one rule for every way a session ends: a close, a restart, the restart
past the context cap, a move between tmux and the stream, a holder that died —
each leaves no process or stream holder, or another process under the name —
and a thread the panel closed or its daemon let go. A session started again
knows nothing of what the one before it waited for, and a command nobody waits
on is a process left on the host. A thread of codex whose daemon is down cannot
be told from one that is gone, so its observers wait for at most one hour. The first look
that reaches that limit stops them and records why in the executor's journal.
The start of the absence is kept on disk: restarting the executor does not
renew the hour. A daemon that answers again clears the deadline; until it
answers, wakes wait without spending delivery attempts.

**An alarm is a moment to be woken at.** A session that has to come back to
something later — a run to look at in ten minutes, a queue to look at every
half hour — sets an **alarm** with `alarm_set`: once, at a span from now, a
time of the day on the host's clock or a moment in RFC 3339, or every so long.
An alarm runs no command, so it has no unit: the executor holds it itself and
rings it from the same look that reads the logs of the observers, once a
second, with the same letter, signed by the alarm and carrying the note the
session left itself. `alarm_list` and `alarm_cancel` read and end the alarms
of the session that calls them and no other's. The three go through the
observers' socket and the same check of the caller — a session on the stream,
one in tmux or a thread of codex, with an old stream holder refused — and are
allowed without asking: they run nothing and touch only what the session set.
The same composer check and failed-wake retries govern a ring typed into tmux.

**An alarm outlives the executor, not its session.** Each alarm is a file
under `~/.local/state/aacpanel/alarms/`, beside the directories of the
observers: whose it is, its note, its span and the moment of its next ring,
written again after every ring. An executor started again reads the files
back; a ring that came due while no executor ran comes once, at its first
look, and its letter says it is late and when it was due. A repeat skips the
rings it missed rather than making them up, and keeps its beat. A crash
between a ring and its record costs a second letter, never a lost one. The
owner is the observers' owner — the process of a claude by its pid and the
start of it, the thread of a codex — and the same three looks that find it
gone take its alarms away, so a close, a restart and the restart past the
context cap end every alarm of the session. The price: a session started
again to go on with its conversation sets its alarms anew. A thread whose
daemon is not read yet is not rung until it is, and its alarms are removed
with a line in the executor's journal after the same one-hour limit.

**An alarm is bounded so a session cannot spam itself.** A repeat is a minute
apart at least: a ring is a turn of the session's model, and a session that
has to look more often wants an observer of a command that looks for it. A
first ring is a week ahead at most, past which a moment is a slip of the
date. A session keeps ten alarms, the host a hundred.

**The person sees them where the session's background work is.** The
collector reads the directories of the observers and the files of the alarms
— the executor's own, kept by the conversation they belong to — into the state
of the conversation and the row of its session, as it reads a checklist: the
service learns of them from the collector and gets nothing that starts a
command. The list behind the clock chip holds them beside the session's own
shells, watches and alarms: an observer with its command, what of it wakes
the session, when it last did, how its command ended; an alarm with its note,
when it rings next and every how long. A tap on an observer opens the end of
its log, read by the collector for that conversation alone. The chip and the
card of the session count the observers whose command runs and the alarms
among the background tasks, under names of their own in the snapshot. A
restart and a close wait for an observer whose command runs, as for the
session's own work: the session started it to hear how that command ends, and
the restart past the context cap would end it halfway. An alarm runs nothing
and is not waited for. A press ends one through an action of the executor,
`observer.stop` or `alarm.cancel`, by the id the collector read it under and
only for the conversation of the session the action names. The session gets
one letter saying which observer was stopped or which alarm was cancelled
from the panel, through the same path as a wake. A stop by the session's own
tool sends no letter. A tmux dialog, an old holder or a failed delivery still
leaves the work ended; the action's answer says the session was not notified.
A thread of codex has the same list behind a chip of its own, shown while it
has something to list.

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
starts. The database logs the number of every file it has run, and a file whose
number is logged is never run again or compared with anything: a change of
schema is always a new file, and an edit to an applied one reaches only the
databases set up after it. A logged number with no file — removed from the tree,
or a database newer than the binary — is named in the log and stops nothing. A
number logged under another name than its file's was reused: the log says so,
and the file does not run. That is why the number of a removed file is retired
for good.

**Two roles.** The service works under the application role: it has neither DDL
nor `TRUNCATE`, and on the journal table only `INSERT`, `SELECT` and `UPDATE` of
the outcome columns. The journal is kept immutable by triggers, but triggers do
not hold the table owner — it will take them off with a single command. That is
why DDL goes through a second connection under the schema owner, and user input
never gets in there.

**Grants are issued by the service at startup, not by a migration.** A migration
is applied once in the life of a database: a role created after the roll-out
would never have got anything. The grant step comes after the migrations, every
time, and brings the role's rights to the set declared in the code. Through
the owner connection it also creates the role or updates its LOGIN/password
from the working DSN, before opening the application pool. The password is
prepared as a SCRAM verifier in Go so plaintext cannot reach SQL logs; the
verifier travels in a transaction-local setting and leaves the connection at
commit or rollback. Both steps leave an owner used as the working role alone.

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

**The install keeps its own account, apart from the panel's data.** The
installer's manifest — a line a change to the machine, written before the
change — the journals of its runs and the copies of the files it changed lie in
`${XDG_STATE_HOME:-~/.local/state}/aacpanel-install`, and uninstall takes back
what the manifest lists and nothing else. The executor's state beside it, in
`~/.local/state/aacpanel`, is the panel's and none of the installer's.

---

## Collection

The collector reads `/proc` and claude transcripts and writes a snapshot: load,
disks, network, processes, live sessions with how full their context is, port
checks.

**A session is recognised by its process, not by the freshness of a file**:
session names are reused between runs, and yesterday's conversation under the
same name is a different conversation.

**Where a session came from is read up the chain of its process's parents.**
The first one that tells decides: another live session — this one is a run
inside its work, a program of that session starting claude; the user's own
tmux server, the server of the panel's terminals or the holder of the stream —
a session of the panel; a tmux server on a socket of its own — a claude out of
the panel's reach, named with the server; the top of the chain with none of
them — a claude typed into a terminal by hand. A server of tmux is told by the
socket its start named: tmux keeps the command line of the client that started
the server, so `-L` or `-S` there is where it listens. A server whose socket
directory was moved with `TMUX_TMPDIR` passes for the socket of the same name,
and one started with `-S` is out of reach even on the usual path. The panel
only reads a session out of its reach: it has no pane to attach to, no side to
move from, nothing to write into, and the panel does not end what it did not
begin.
A run folds under the session that started it, so a suite that raises a dozen
claudes does not push the sessions of the person off the list.

**A session opened at the word of another names that one as its parent.**
`session_open` hands the panel the name of the session that calls it, and the
launcher puts the name into the environment of the new session as
`AACP_PARENT`; a codex thread has no process of its own, and the executor keeps
its parent beside the name the panel gave it. The link holds within one project
of the map — its own directory, one inside it and its worktrees: the panel
names the caller as the parent only while it lives in the project the new
session opens in, and a session opened in another project stands on its own. A
session that opens one elsewhere hands work over to that project, and the new
session is that project's, not a step of the caller's work. The rule is the
panel's, where the map is, and the open goes on either way. The collector
reads the parent as the session's `parent`, the field that names the session
a run runs inside of — without `outside`, since the panel opened this one.
The lists stand the child a
step in under its parent, in the parent's block whatever its own project, behind
a fold that is open until the person closes it: the children are the work in
view, and the block stands where the most pressing of the group puts it. The
link is by name, not by conversation: a restart starts another conversation
under the same name, and a restart or a move of the child starts its next
process with the parent again, as the snapshot holds it. The person's Resume
from the archive does not: a conversation the person brings back from there is
the person's, and a claude session and a codex thread alike come back with no
parent — the executor takes the parent of a codex thread off the disk as it
resumes it — and a claude session under the name it ran under, a free one
after it when that name is taken. A resume a session asks for through `session_open` goes by the rule
of an open: the session that asks is the parent while it lives in the project
the conversation ran in, a directory it names is only checked to be of that
project, and the conversation comes back under the name it ran under unless the
session gives another. A conversation that lives is not resumed: a second
process would write into the same transcript. A child whose parent
closed stands under a stub of the parent that says when it closed and opens its
conversation; one whose parent lives in another contour stands on its own page
and says which session opened it. Every process the launcher starts drops the
variable it inherited: a terminal or a session started from inside a child is
not a child of that parent.

**A live session is stood under another with `session.parent`**: a session
started before the panel kept a parent, or one the person names later. Both
live and work in one project of the map, or the panel refuses; a run inside the
work of a session is stood under nothing else, and two sessions are not stood
under each other. The environment of a running process does not change, so the
executor keeps the parent in a file of the session's claude process, by its pid
and with the start of the process, beside the parents of codex threads; the
collector reads it there before the environment, and the later word wins. The
file is the process's, not the name's: a session started later under the same
name is another process and no child of it, and a renamed one keeps its
parent. A restart or a move of the child starts its next process with the
parent in its environment: the one the file of the old process names, and
else the one the snapshot holds — the snapshot reads the file a moment after
the executor wrote it, and a move with the panel down knows only the parent
the session was started with. A codex thread is stood under a session where a
thread started for one keeps its parent. No screen offers it: a request on the
local listener does it, and the journal names it.

**A live session is taken from under its parent with `session.detach`**, from
its menu and its tools while it stands under one. The parent it was opened with
stays in the environment of its process, so the executor writes the same file
of the process naming none; the collector takes that over the environment, and
the session stands on its own until it is stood under another again. A restart
starts the next process with no parent at all. A run inside the work of a
session stands under that session while it runs and is refused. A codex thread
is taken from under its session by the file the executor keeps for it. The
journal names the session it stood under.

**A pinned group stands first inside its contour on every device.**
`session.pin` sets or clears the pin of the live root; a request naming a
child resolves to that root. The phone's card and the desktop's context menu
use the same action. The press moves the group and changes the pin at once;
the pin stays dimmed until a fresh snapshot confirms the requested state or
the root is gone. The same catch-up wait as a start or a close refreshes the
snapshot while it waits. A refusal restores the snapshot's order and shows
the reason; a change not seen by the wait's ceiling returns to the snapshot
too. A pin being set takes its place by the time of the press. Pins
stand in the order they were set, including when several roots work in one
project. Only a root and its children move; unrelated sessions of its project
keep their places, and contours keep the order of the map. Removing a pin
restores the ordinary order of each screen: the map on the desktop, attention
then the map on the phone.

The phone's contour reads **pinned → needs you → sessions**. Pins form one
section whatever their state. An unpinned block with a session waiting for
the person stands in needs you; the rest keep the map's order, with sessions
outside the map after its projects. Rows within a project and outside the map
stand by name, with children under their parents. Activity changes what a row
says without moving it. Projects with only a past conversation close the list
in archive order, at most `QUIET_MAX` of them.

The executor keeps each pin under `aacpanel/pins/` in the owner's state,
using the role's key: the config directory of the account, the common git
directory of the repository (the working directory outside git), and the
session's name. A codex thread uses its codex home as its account. The
collector reads the pin into the session row as `pinnedAt`. Restarting,
closing and reopening under the same name keep it, as does moving between
checkouts of the repository. Repeated pin requests keep the original stamp;
unpinning removes it, and pinning again takes a new place at the end.

**The archive is ordered by when a conversation last spoke, not by when its
file was last written to.** Claude appends a title, a mode or a snapshot of file
history to a transcript long after the talk ended, and the file goes fresh with
nothing fresh in it: ordered by that, a conversation of the spring stands above
yesterday's while its own card says spring. The order is the stamp the card
shows, read from the tail of the transcript and remembered beside the parse; a
transcript that names no time has nothing left but its file.

**The background work of a session is read from its transcript and from the
transcripts of its agents**: a shell or a watch an agent starts belongs to the
session. A call is known by its structured result, and where claude keeps none
— the transcript of an agent on the stream keeps only the words of the answer —
by the words, which name the id the way the session's screen does. An alarm the
session sets — a wake-up, or a job of its cron, once or over and over — stands
among that work until it fires, is cancelled or goes with the process. A console
names the job on the record it fires with; the stream leaves only the prompt, so
a firing is matched to its job by the prompt, word for word, and one no job owns
is the wake-up's.

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

**A picture sent from the panel is found on disk, by the collector.** The
executor saves a file it is sent in a directory of its own and types the path
into the message, a line for each file; the transcript keeps the path and
nothing else. The collector is the one process that reads both the transcript
and the owner's home — the service reads nothing of the host, the executor no
transcript — so it hangs on a message the pictures its lines name, and serves
one by its name: a file directly in that directory, still there, of a type
every browser draws, and small enough for one answer of the socket. A picture
that is neither — a camera photo over 4 MB, a HEIC — is shown by a smaller
JPEG the phone draws when it sends it, since the host has no library to
decode pictures with: the copy rides beside the file, the executor keeps it
in `previews/` of the same directory under the same sweep and never names it
in the message, and the collector serves it in place of the file. A browser
that does not decode the picture — Chrome and a HEIC — sends none, and the
feed shows the path. A path typed to a picture anywhere else stays words. The directory is found by the
executor's own variables, and the collector, a system unit, does not see what
is set for the user's manager alone: `AACP_FILES` goes into the host
description, which both units read. A picture a call returned is in the
transcript already, inside the result: the feed carries its place on the call.

**A search of a conversation runs where the transcript lies.** The screen holds
only the window of the feed it has loaded, and the service has no parser of
transcripts, so the question goes to the collector (`search` on `chat.sock`,
`GET /api/chat/search`). It reads the transcript once from its head, through
the same parser the feed is folded from, and keeps no more than its answer: the
newest three hundred matches and the count of all. What is searched is what the
feed shows as the conversation — the person's messages, the model's answers,
letters, the title of a brief, of a secret asked for and of a published page;
not a call, what it was given and what it returned, thinking, or what the
harness and the hooks say: a search that found its words in the output of
every grep would bury the one place the person remembers. Case does not count,
and neither does a line break. A match names its row as the feed does: by the
position of its record and its role, and, where a record gives several rows
of one role — the letters of several agents at once, an answer of several
blocks of text — by its number among them; a row the queue drew comes again
under the same name and takes its own place, never a neighbour's. The screen
opens the window that ends with the match by asking for the rows before the
next position. The price is a read of the whole
transcript for every question — under a second for one of fifty megabytes.

**`/mcp` is a screen of the panel, asked of the session.** On the stream the
panel asks claude for its MCP servers each time the screen opens, and changes
one of them — reconnect, enable, disable — with claude's own requests.
Authentication is left to the host: its browser comes back to the host, and a
phone is not that browser. Only what the screen shows leaves the host: the
headers, the arguments and the environment of a server, and the query of its
address, can carry keys. In tmux `/mcp` is a screen driven by keys, and
the composer does not send it. `/status` is a screen of the panel too, in both
kinds of session: most of it the snapshot of the host already holds, the
version comes from the file of the session, and the account from claude's
answer to the handshake — which a session in tmux does not have, so its card
goes without one.

**`/hooks`, `/memory`, `/skills`, `/agents` and `/config` are read-only screens
of the panel.** On the stream claude hands out the rows of its own screens for
the first three, names the kinds of subagents at the handshake, and answers
`get_settings` with the merged settings; the panel asks each time a screen
opens and changes nothing — a hook or a skill is changed in its file or by
asking Claude. The rules of permissions are not shown in any form, and the
environment never leaves the host. In tmux these are the client's own
screens, driven by keys, and the composer does not send them.

**A button under the composer lists what the panel does itself** — its
screens, the pickers of the model, the effort and the mode, a question aside,
the commands whose answers the feed draws as cards (`/context`, `/usage`)
or that change the conversation (`/compact`), and the reloads of the session's
plugins and skills (`/reload-plugins`, `/reload-skills`), which leave the
conversation alone and ask the model nothing. The list is read from the same
registry the composer reads, and each row does what typing it does: a command
goes through the same confirmation. A session in tmux is offered only what
works there.

**A session on the stream is renamed from the panel** (`session.rename`, the
sheet of `/rename`). Claude takes the name with a request of its own and
writes it into the file of the session, where the panel and the host look a
session up; the holder is keyed by the conversation and does not notice. A
name keeps to what a URL and a file name take, and a name another live
session answers to is refused: two sessions would answer to one name. The
open conversation follows the session by its conversation to the new name. A
session in tmux is renamed on its own screen, `/rename` with keys.

**A session gets the panel's tools from one MCP server.** Every session the
launcher starts, in tmux or on the stream, gets the executor as the MCP
server `aacpanel` (`aacpanel-exec -mcp`), and the tools the server marks
allowed are allowed by name, so a call of one never waits on a person; the
`panelTools` launch parameter turns them off for a contour or a project. A
session started by hand has them where its account names the same server at
the user level (`claude mcp add`, INSTALL), with nothing allowed until the
account allows it; a session the launcher starts in such an account still has
one server, since claude keeps one server of a name and the one on the command
line takes the place of the account's. The list of tools lives in one
place, `internal/toolset`: the executor serves it and the launcher takes the
allowed names from it, so a session is never allowed a tool the server does not
have. The server's word to a session — in its system prompt — is a lead and a
line from every tool, since a tool of a server is often deferred and its
description unread. The lead routes a blocking question to the client's question
tool, a phone call to `notify`, a long read to `brief_publish`, a credential to
`secret_ask`, a task for the person's hands to `request`, a letter to
`send_to_session`, a command watch to `observer_start` and a timed return to
`alarm_set`. A blocking request ends the turn until the person's answer arrives;
a nonblocking one leaves the agent free to continue independent work. Each tool's line
says when to reach for it; its description gives the arguments, limits, alternatives
and whether to continue work or end the turn. Question and permission dialogs
already call the person, so they need no extra `notify`. Claude keeps 2048 UTF-16 units of that word by default
and cuts the rest mid-sentence. The launcher lifts the ceiling to 6656 with
`CLAUDE_CODE_MAX_MCP_DESCRIPTION_LENGTH` in the environment of every session
it hands the tools; claude holds the description of every tool of every
server to the same ceiling, so those are lifted too. The lead and every line
fit the lifted ceiling even with the longest checklist standing, the
three longest brief titles and the longest role, all in letters of two units,
and a test holds that, so a new tool pays for its line out of the
same budget; that worst case is what the number is made of, and its cost is
that a tool of any other server keeps a description up to the same length. A session started by hand keeps the default unless the variable
is in its environment, and past 2048 it loses the lines of the last tools. `-plan`
starts the same server: a live session keeps the
MCP configuration it was started with, and reconnecting to the server runs the
binary on disk with the flag written there. Beside the checklist the server carries
the brief — `brief_publish`, whose call without a document returns the rules of
writing one, and `brief_delete` — the call to the person, `notify`, and the
notepad of secrets, `secret_ask`, and tasks for the person, `request`. The machine's brief guide is edited in
Settings → Briefs and kept in the database after a one-time import of
`aacpanel/brief-guide.md` from the user's configuration directory through the
collector; each call without a document reads it from the local panel within
two seconds or returns the shipped rules with a notice, without a cached copy.
At the handshake the brief tool reads the
panel's shelf and names the answered, unsent briefs of the session's directory,
newest first, up to three and a count of the rest. The answers live in the
panel's database, so reading the collector's shelf alone would not say which
are waiting. The read has two seconds; an unavailable panel adds no line.
The tools speak to the collector's sockets under the conversation and the
directory the
server finds for its claude rather than any the model names, so a brief lands
on the shelf of the session that published it and its answers go back to that
session. The feed draws the card of a published brief from the tool's answer,
which names the brief first, and never from what the call was given: a check
publishes nothing, and neither does a call the collector refused. The card of a
secret is drawn the same way, its name from the answer and its title and
notepad from the call, and a call the tool refused gives none. The card of a
call to the person is drawn from the call alone, since the line is what the
phone got; the answer only says whether anyone was called.

**The checklist of a session is the model's own, kept by a tool of the
panel.** The tool is `checklist` on the panel's server, allowed, so an update
never waits on a person. The model keeps a checklist for work with several steps,
sends the whole list before starting and as steps change, and reconciles it before
reporting; a question or a one-step task needs none. The checklist belongs to
the session, not to its conversation: the config directory of the account, the
directory claude runs in and the name the session runs under. A session
started again under its name — afresh, or going on with its conversation under
an id old or new, as the restart past the context cap does — finds the
checklist where it was left, is told at the handshake how far it got and the
step it stands at, reads it whole with a call that sends no list, and keeps it
or clears it; another session of the directory, under another name, starts
with none and keeps its own. The server finds the session through its parent:
claude keeps a file of itself under `sessions/<pid>.json` of its config
directory, which names the conversation, the directory and the session, read
on every call, since `/clear` starts another conversation in the same process;
before claude has written the file, the process's own environment, working
directory and `-n`/`--name` argument place it. The checklist is one file a
session under `aacpanel/checklists/` in the owner's state, named by a hash of
the two paths and the name and holding them, replaced whole, with the
conversation and the process that sent it last; the collector carries it in
the row of the session and in the state of its conversation — the steps with
their statuses, since when a step at work or done holds its status, and when
the list was last sent — and the feed of a conversation that is over shows the
checklist it sent last, and only while no later conversation sent it. A
session without a name is told by its place alone and keeps the checklist of
the place, named by the two paths; a server that does not tell the sessions of
a place apart writes that file too, and a named session takes it over as its
own only while its conversation is the one that sent it last — whose it is
otherwise is not to be told. The price is that the name is the key: a session
renamed starts with no checklist, and sessions without a name in one directory
under one account share one. A thread of codex keeps its checklist by its
thread alone, in `codex-<thread>.json`: codex keeps the id for the life of the
thread, resumed or joined by another client, while the place of a thread is
named by two paths that need not agree — the server finds the home by the lock
the kernel names, the collector by the home the executor was given. The row
of the thread and the state of its conversation carry it as a claude
session's do; the thread is told nothing of it at the handshake, since the
server knows no thread before a call. A checklist nobody has sent for thirty days is
swept when a server starts. Nothing makes the model keep it true: a `Stop` hook, where
the account has it, holds a turn that did work past an unfinished checklist it
did not touch, once, to ask whether it changed.

**A session can keep its own first and restart messages.** The tools of the
conversation on the phone and the desktop open the Session messages sheet,
which shows each override and the inherited text with its project or contour
source. `session.intent` sets either message independently; an empty text
removes that override. The executor keeps them under `aacpanel/intents/` in
the owner's state, with the same account, repository and name key as a role.
They survive a close, reopen and restart, and follow a rename through the panel.
The launcher reads the override after choosing the free name and account:
its own message wins over the project, then the contour; no message anywhere
means the session waits. An unreadable override leaves the inherited message
in place and adds a warning to the launch report, so it cannot block a start.
Opening or resuming uses the first message; a restart
uses the restart message. A continued conversation with words written by the
session uses those words first. Switching transport sends no opening message.
These overrides are edited only in the session sheet, not in `session_open`
or the project settings.

**The role of a session says which work of its repository is its.** Several
sessions of one project often run in one directory and on one branch, and
after a restart each would take the notes, handoffs and task picks left there
for its own. The role is a short text, up to 500 characters, set three ways:
by the session itself with the `role` tool of the panel's server, allowed; by
the session that opens it, with `role` of `session_open`; by the person, with
`session.role` from the tools of the conversation. The server tells the
session its role at every handshake, so it reaches the model before its first
word, with who set it and that the other sessions of the directory may have
roles of their own. A role is one file a session under `aacpanel/roles/` in
the owner's state, named by a hash of the config directory of the account, the
repository and the session's name. The repository is the common git directory
of the checkout the session works in, read from the `.git` the walk up from
its directory finds — a worktree's gitdir and that one's `commondir`, a
directory with a `HEAD` for a main checkout — or the directory itself where no
checkout holds it, so a session that moves into a worktree of its repository,
or comes back in another, keeps its role. The file holds what its name is made
of, the directory the role was set in, the role, who set it — the session, the
person or the opener by name — and when. A role `session_open` gives is
written by the launcher under the name the session comes up under, a free one
after a taken name, before claude starts; an open or a resume without one
leaves the role the name has, and the Resume of the screens comes back under
the name the conversation ran under, so it finds that role too. The executor
writes the person's role from the file claude keeps of the live session, on the
stream and in tmux alike, and nothing is typed into the conversation. A rename
through the panel carries the role to the new name. The collector carries it
in the row of the session, and the prompt stamp tells it from there: claude
keeps the server's word a conversation started with, and a resume or a
reconnect of the server does not read it again, so a role set or changed after
the start would never reach the model otherwise. The stamp says the role once
a conversation, on the next prompt or batch of tools — past its throttle, alone
when the throttle keeps the rest back — and again when the role changes or the
conversation is compacted, and says once that a role it told is cleared; what a
conversation was told lies in `role-<conversation>.state` beside the
throttle files and goes with their sweep. Where the stamp, a part of the kit
installed on request, is not installed, a role reaches a conversation only at
its start. The price is that the name is
the key: a session renamed outside the panel, or opened again under another
name, starts without its role. A thread of codex has no role.

**A session restarts and closes itself, writes to other sessions and opens new
ones with tools of the panel.** They go to the panel's local listener as actions
of this machine; the restart, the close and the letter name the session by the
conversation the server's parent runs, so the model cannot act as another
session.
`session_restart` asks for `session.restart`:
the panel brings the session back as its project from the map, afresh or, with
`continue`, resuming the same conversation. `continue` takes `intent`, the
message the session writes itself — what it was doing and the step it takes
next — and the tool refuses one without the other: the map's message after a
restart is one line for every restart of a project, and where the map names
none a resumed conversation comes up and waits for nobody. The request carries
the message as its text, and the executor lays it over the launch as the first
message of the resumed conversation in place of the map's, so it rides the way
any first message does — an argument of claude in tmux, a message after the
handshake on the stream. That way sets its bounds: the ceiling of the map's
first message, no leading dash, which claude in a terminal would take for an
option and leave the session closed with nothing in its place, and no leading
slash, which claude runs as a command. A restart that starts anew takes no
such message and opens with the map's, and so does a conversation on the
stream that starts anew because nobody said a word in it: the words were
written for a conversation the new one does not have. Either way the executor restarts it
only while it still runs the conversation that asked — a late repeat of the call
would otherwise close the session an earlier restart brought up under the same
name — and a conversation on the stream nobody said a word in starts anew. A restart the person asks for is a fresh one, and the tool's word keeps
`continue` for their ask to keep the conversation: left to choose, a model takes
`continue` to spare itself the notes a fresh start wants first. Nothing is asked while the collector's snapshot shows the session's
agents, workflows, background commands or observers at work, since a restart ends them;
the snapshot can be eleven seconds old, so work seen is read again in a
snapshot written after the look began, through `internal/session`, which the
context guard uses too. A session on the stream is closed only at
the end of its turn, which cannot end while the call waits, so the panel's
answer would come after the call: the tool waits three seconds and takes
silence for a restart under way. `session_close` without a name asks for
`session.close` with the calling conversation: the panel finds the live session
that runs it, and the executor closes it only while it still runs that
conversation, the way Close on its screen does — a session on the stream by its input, at
the end of the turn the call is part of, any other at once — and starts nothing
in its place. It waits for the same work as the restart, with the same `anyway`
and the same three seconds for the panel's answer, and the host's main session
is refused by the executor, as from the screen. With a name, the close carries
the caller's conversation and the host checks the separate opening record;
manual grouping is not ownership. The request also names the caller as its
conversation until the executor checks ownership and substitutes the child's:
an executor that does not know child closes refuses the mismatch. Claude
children are recorded by process and start time, Codex children by thread.
`send_to_session` lists the live sessions of
every account from the snapshot, and sends a letter as `session.letter` with
the sender's conversation: the executor finds the sender and the recipient
among the sessions of every contour and writes the letter to the recipient's
message socket in the envelope claude uses between sessions, with the sender's
name and the address of its socket, escaped as claude escapes one. The
recipient's claude frames it as a message from another session, not typed by
its person, and the recipient can answer to that address. Typed into a
terminal or sent on the stream, the same words would reach the model as its
person's, so a letter goes no other way: a recipient without a socket is
refused. That is also why a letter is a kind of its own and not a field of
`session.send`: an executor that did not know the field would type the letter
in, where one that does not know the kind refuses it. The letter does not
claim the sender's permission class, which the panel does not know for sure,
so a recipient that runs without permission prompts and has no
`crossSessionInbound` setting holds it for its person. A codex thread writes and
is written to the same way. Codex starts a server of its own for every thread
it holds — a daemon holds many from one process — in the thread's directory,
with an environment that names no thread, and names the thread in the `_meta`
of every call as `threadId`: the server under codex takes the sender from the
call, and only a thread whose lock the codex that started it holds among its
open files (`<CODEX_HOME>/thread-writer-locks/<thread>.lock`), which says the
home too. It offers the tools of a session except restart, each bound to that
thread: the checklist, the brief, the call and the notepad of secrets go under
the thread as a claude session's go under its conversation, the close closes
the thread as Close on its screen does — the turn that runs, the one making
the call, is interrupted and the panel leaves the thread — and a call that
names no thread the codex holds is refused by every one of them. The restart
is claude's — the daemon keeps the thread, and the panel refuses it. A new
session asks for confirmation as a Claude session's open does. The server's
word to codex names only what it has: a blocking choice goes through the client's
question tool when available, otherwise into the conversation with the turn ending
on it. A brief carries questions to answer later, and the letter is not offered as the way past
a `SendMessage` it does not have. Codex writes a call of an MCP tool and its
answer as one item of the rollout, and the feed draws the card of a brief or a
secret from that item; a secret saved for a thread reaches it as a message of
the panel, a turn on a free thread and into the turn that runs on a busy one,
as the person's words do, and a brief
lands under the directory the row of the thread names. A letter of a
thread goes as `session.letter` naming the thread, its home and its directory,
signed `codex-<tail> — <contour> — <directory>`: claude's envelope has no field
for where a sender works, and a codex has no socket to be answered at, so
letters go one way and an answer is a letter of its own. A letter to a thread
goes through the daemon as a message of the panel — a turn on a free thread,
into the turn that runs on a busy one — in the same envelope, with the words claude
puts around a letter for its model written out by the panel, and the feed of
the thread draws it as the card of a letter. A thread the panel starts gets
the server in its configuration at `thread/start`, with the approval mode
`approve` on every tool a claude session is allowed: codex asks the person
before any other call of a tool of a server, so a checklist would ask before
every update, and the letter asks as it does a claude session. The mode is
set a tool at a time (`mcp_servers.aacpanel.tools.<tool>.approval_mode`),
since the mode of the whole server would let the letter through too. The
threads of a terminal and the runs of `codex exec` get the server and its
modes from `config.toml` of the home.
`session_open` asks for
`session.open` with a directory, and with a name only where the model gives
one: the panel finds the project of the map the directory belongs to — the
project's own, one inside it or a worktree of it, as a restart finds it — and
opens the session there with the account and launch parameters of the map,
the map's first message for Claude and no first message for a Codex helper,
under the project's session name unless one is given; a live session of that name makes the launcher take a free one. A directory no
project holds is refused rather than looked up by the name, since the panel
would know neither the account to open it in nor its launch parameters, and
the answer names the contour, so the session can tell its person which account
the new one spends. The calling session names itself, and the new one is shown
as its child when the two are of one project. With `resume` it asks for
`session.resume` of a closed claude conversation instead: the conversation
goes on where it ran, with the launch of the map, under the name it ran under
unless one is given and under the caller by the same rule — so a session that
hands out work brings a step of it back as its own child, not around the
panel. Either may give a claude session its role, `role`, kept under the name
the session comes up under. The tool has no first message of
its own for the reason a letter is a kind of its own: words typed into a
session its model takes for its
person's, so work goes to the new session as a letter once it is up. The open
waits a minute for the panel, which answers once the new claude is up; silence
past that is an open whose outcome is not known, and the tool says not to open
again blindly. The restart and the close are allowed: the restart past the
context cap is done with nobody at the screen, and so is the close of a session
done with the one job it was opened for; the restart touches only the calling
session, and the close also takes a child that conversation opened. Both wait
out the work of the session they close. The letter is not: it puts words before
another agent,
and claude asks the person first, as it does for any tool. Nor is the open: a
new session starts work in its account and spends its limits, and the question
claude asks before the call is the person's word to open it.

**The subscription limits come from claude alone.** The 5h/7d percentages do
not lie on disk and no API hands them out: claude tells them to the status line
of a terminal, in the payload on stdin, and with every answer it gives on the
stream. So one snapshot per contour has two writers — the executor's
`statusline`, which the install puts first in the status line chain, and a
probe the executor starts for a contour whose snapshot has aged past its
interval: a minute, or longer where `AACP_LIMITS_EVERY` says so — a value under
a minute is ignored. The probe is a `claude -p` on haiku, started in the root
of the contour so the wrapper picks its account; it says one word and hands
over the windows claude tells with the answer. It keeps no session and no
transcript and loads no MCP server or tool; the sessions of the contour are not
asked anything. Its price is a claude start and a word on haiku a contour every
minute, a share of the very limit it reads — which is why the interval is the
host's to set; a terminal of the contour keeps the snapshot fresh through its
status line and spares a probe only under a longer interval. A window whose
reset time has passed is handed out by the collector as started over — nothing
used, no reset known — until the next snapshot: the share the snapshot holds is
of the window that ended. The screens show the age of the snapshot beside old
percentages.

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

## Pushes

**A push is news, said once.** The service looks at the host every twenty
seconds and compares the look with the one before it: a question, a session
waiting on a permission, a brief on the shelf, a turn of ten minutes or longer
that ended, a session gone without the panel closing it, a stack down whole, a
container down or unhealthy while the rest of its stack runs, a rule tripped, a
probe that stopped answering, a subscription limit past 80%, a codex daemon
that runs another release than the one its protocol was checked against
(Codex sessions above), and a panel gone blind — the collector silent or the
database out of reach. A reason is pushed
when it appears and, where it has one, once more when it is gone; nothing
reminds in between, and the push service collapses repeats under the reason's
own topic rather than the phone. A call of a session travels apart, the moment
the collector takes it (the call socket above). The rule that a whole stack is
down pushes nothing: the stack pushes the same fall itself, sooner.

**What is heard is the person's choice, kept once for every device**
(`push_prefs`). A kind of news can be turned off; so can the long turns and the
closings of the sessions of a contour or under a directory, the falls of a
stack, the recoveries of containers apart from their falls, a rule, and the
limits of a contour. Each list names what is off, so a kind the panel learns
later is heard. A call and a blind panel are never held back: one was asked for
by name, the other is a failure that would pass in silence. The choice is read
before every push, and a choice that cannot be read holds nothing back.

**A push that names its source carries a button that quiets it.** The
notification shade holds no page, so the press is its own confirmation: the
worker asks the panel it belongs to, with that panel's session, to turn the
source off — the one request that changes anything without passing the gate of
actions, and it changes only the choice — and says in a quiet push of its own
whether the panel took it.

**A push about a session does not go to the device where that session is on the
screen.** The open conversation marks that it is being watched, and the mark goes
stale in under a minute, so a phone put down gets the next push. A tap on a push
about a session opens that session.

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
where the sound goes before they do, and nothing else a page sends crosses the
machine. The panel's own traffic out is the push service carrying notifications,
the model catalogue of each account, read from the Anthropic API with that
account's token, and the probes of the external services it watches — liveness
checks that ask without a login (`internal/watchcfg/config.yaml`). The
recognition is not done here because doing it here is a model on the host, and
that is a different feature with a different price.

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

### Panel listeners

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

**The LAN certificate can be managed by the installer.** Choose `lego` for
the certificate, an ACME account email and a DNS-01 provider: an executable
hook of your own, or the shipped Hetzner Cloud DNS hook with a zone name and
the path of a token file owned by the user with mode `0600`. The installer
installs the `lego` package if missing and issues only a missing certificate;
adopting an existing valid pair makes no ACME request. Account files stay in
the chosen lego directory, and its `certificates` subdirectory is mounted
read-only as `/tls`. The token and ACME account never enter the container.

The installer writes `aacpanel-lego.service` and `aacpanel-lego.timer` as
user units. Renewal runs daily with `renew --days 30` and the same `--email`
as issuance: leaving out the email looks up a different account. The settings
live in `~/.config/aacpanel/lan-tls.json`, without the token itself. Uninstall
takes back the units and keeps the certificate and account data; selecting
external renewal removes the installer's renewal units. `install.sh check`
reports the certificate expiry and whether the delivered timer is ready.

The listener reloads a matching certificate and key at most once a minute,
keeping the served pair when a replacement cannot be read. Both Machine
screens and the `tls.expiring` rule read that served leaf. With fourteen days
or less left the usual alert lifecycle opens one alert per certificate,
persisted across service restarts; replacement closes it. Its push follows
the rule's existing notification choice. This deadline is later than the
renewal window, so the alert means renewal needs attention.

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

The check keeps out a page in a browser, not a program. Any process on the
machine, run by any of its users, reaches every action through this port with
no sign-in — and the tools and hooks of the sessions rely on exactly that: a
session asks for its own restart here, writes to another session here, and
learns here that a brief is waiting to be sent. A request with no device behind
it is written into the journal as made by "this machine". On a machine whose
other users must not drive its sessions, this port is the one to close —
`AACP_LOCAL_ADDR` set empty — and the price is those tools and hooks and the
monitor at the machine itself.

### Voice input

**Voice has a separate unix socket, with only `session.send`.** A host
container doing speech recognition mounts it to send the owner's words to any
live session by the same name the panel and `send_to_session` use. The service
serves it when `AACP_VOICE_DIR` is set; it listens on no TCP address, and has no
panel routes, terminal bridge, session listing or other action. The socket
keeps the entry point on the host; a bearer token grants the mounted consumer
access without a panel login.

The optional `deploy/voice.yml` compose overlay binds a dedicated host
directory, conventionally `~/.local/share/aacpanel/voice`, to
`/run/aacpanel-voice`. This overlay must be part of **every `docker compose`
call** on the host, including updates. Set
`COMPOSE_FILE=docker-compose.yml:deploy/voice.yml` in the compose `.env` to
make it persistent; if the host uses other overlays, keep them in that list
too. Explicit `-f` options override this list and must also include the voice
overlay. Otherwise the next plain `docker compose up` removes the socket
mount and disables the listener. The directory is `0700`, and `sock` and
`token` inside are `0600`, owned by the service's `AACP_UID:AACP_GID`. The consumer runs under
that same numeric uid/gid and mounts this directory read-only. It gets no
executor socket or host state. The service's cost is one writable directory
for this IPC socket, with no added host action or right to host resources.

The token is a random secret of at least 32 characters, read from `token` on
every request. Missing, unreadable, empty or non-`0600` files refuse input.
Rotation writes a new `0600` file and renames it over `token`; both processes
mount the directory so the new inode is visible, without a restart. A missing
or wrong bearer token receives HTTP 401; an unavailable token file receives
503. The token is never a URL parameter and is not written into the journal.

The request is HTTP over the socket, `POST /session.send`, with
`Authorization: Bearer <token>` and JSON `{"to":"lab","text":"Run the tests"}`.
Only those two fields are accepted. HTTP 200 with
`{"ok":true,"detail":"...","logged":true}` means the executor accepted it;
404 with `{"code":"session_not_found","error":"...","logged":true}` means
no such live session, and 409 with `code: "session_ambiguous"` means the name
has more than one match. Other failures carry an error and a non-success
status. `logged` reports the ordinary action journal's availability.

The adapter constructs `session.send` and uses the same service validation,
journal, executor name lookup and sending path as the composer. Busy stream
sessions keep the text in their input queue; terminals take the same typing
and Enter as the composer. The journal's source (`device_name`) and the
executor's audit actor say `voice`. The session receives only the text, with
no voice mark or letter envelope. Enabling this requires the matching service
and executor builds: the executor supplies the lookup error codes and refuses
to substitute a letter for voice input when typing cannot reach the session.

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

- **Control of other machines.** Only its own host. At most, metrics collection
  could travel to a remote machine; its actions never would.
- **`POST` on the socket-proxy.** Never: that is where the point of the split
  disappears.
- **Editing the panel's own settings from the screen.** The list of
  `host.env` and `.env` settings is shown, but read-only: their cost of change
  differs — from "right away" to "recreate the container" — and one identical
  button would deceive the human. The map of contours, groups and projects is
  edited from the screen; that is data, not the panel's setup.
- **Power and the graphics card.** These sources will not be there where the
  panel travels: the UPS watchdog is the machine's own system housekeeping, and
  polling the graphics card twice a minute wakes a laptop's discrete card for a
  chart nobody looks at.
- **A task registry.** It lives as a claude plugin, not everybody has it, and on
  somebody else's install the buttons would honestly answer "not found".
- **Actions of its own on a session.** The panel never types into a session by
  itself, not even to save it from a full context. What a session does about
  its own context is decided inside it: the context guard needs both its
  `Stop` hook installed in the account and `autoRestart` enabled on the
  contour or project. The service hands the map's caps and restart settings to
  the executor, which keeps them in a file on the host for the hook. At a
  stop the hook checks the session's background work and subagents, waiting
  for a fresh snapshot while they are busy, and asks the model to finalize
  and call `session_restart` only when the session is free. A watchdog
  outside would
  have to guess whether the session can take a keystroke right now, and a guess
  that lands in an open dialog is an answer given blind.
- **Quiet hours and reminders for pushes.** What is heard is chosen by kind and
  by source; when is not a setting. A reason that stands is not pushed again
  until it is gone and comes back.
- **Safari.** The panel lives in one browser. That removes a class of
  workarounds, but building into the markup what is known to be dead there is
  not worth it either.
