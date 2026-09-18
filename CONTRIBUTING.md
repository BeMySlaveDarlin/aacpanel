# How to change the panel

## What the machine needs

Go, python3, docker with compose, `shellcheck`. Everything else arrives on its
own: the front end is built with the same toolchain as the service, there is no
npm in the project and there will not be.

A database for the tests is not required — but without it dozens of tests are
skipped **silently**: rollups, retention, the action journal, the profile map. A
green run without it means only that the code compiled.
It can be brought up as a container:

```bash
docker run --rm -d --name aacpanel-test-db -p 55432:5432 \
  -e POSTGRES_USER=aacpanel -e POSTGRES_PASSWORD=test -e POSTGRES_DB=aacpanel \
  postgres:18-alpine
export AACP_TEST_DSN='postgres://aacpanel:test@127.0.0.1:55432/aacpanel?sslmode=disable'
```

The DSN is an entry point, not a workplace: the harness creates a database of
its own for every package and takes it away with the run.

---

## Build

```bash
make front      # the front-end bundle into web/dist
make build      # go build ./...
make image      # the service image
```

The executor is built separately and installed by hand:

```bash
systemctl --user stop aacpanel-exec      # otherwise "text file busy"
go build -o ~/bin/aacpanel-exec ./cmd/aacpanel-exec
systemctl --user start aacpanel-exec
```

**Changing the list of actions requires this rebuild.** The service travels as
an image, the executor as a binary: while it is the old one, the button is there
on the phone and the journal says "unknown action". The same the other way
round — when an action has been removed.

The collector travels from the working tree, and `systemctl restart
aacpanel-agent@<user>` is its rollout.

---

## Before a commit

```bash
make check
```

That is everything at once:

| Target | What it checks |
|---|---|
| `fmt` | `gofmt -l` — the format is not fixed but shown: one unnoticed file in the output drowns the next finding |
| `vet` | `go vet ./...` |
| `front` | the bundle build: an error in a screen's markup is caught by no test — they work with ready structures |
| `shellcheck` | the deployment and stand scripts; with no linter on the machine it is a skip said out loud, not silence |
| `agent-import` | that every module of the collector imports: an undefined name at module level does not break one screen — it stops the service from coming up at all |
| `test` | the Go tests with a substituted home directory |
| `agent-test` | the collector's tests with a substituted state directory |
| `agent-confined` | the same tests inside the restrictions of the collector's production unit |
| `delivery-test` | the python deployment scripts |
| `vuln` | `govulncheck ./...` |

A red `make check` is a reason not to send the change, not a "we will fix it
later".

**The same checks run on a push and on a pull request**, minus the ones that need
a live host: the tests of `internal/executor` and `internal/launcher` skip
themselves without tmux, konsole and a graphical session, and `agent-confined`
has no user systemd manager to run inside. The workflow counts what it skipped
and prints the reasons, so a green tick is not read as "everything was checked".

The database is a service container there, and a run where the tests with a
database quietly skipped is failed on purpose: without `AACP_TEST_DSN` forty of
them pass by doing nothing, and the run stays green.

Separately, outside `check`, because they need data that not every machine has:

```bash
make usage-replay    # incremental usage collection against a straight pass over live transcripts
make skills-diff     # whether the shipped skills have parted from the installed ones
```

**A skill a machine keeps in a version of its own** is marked rather than
argued with every time: `make skills-local SKILL=<name> WHY='why'` writes a
`.local` beside the installed skill, and `skills-diff` then names it instead of
printing a diff nobody is going to act on. A machine may well want its own: the
shipped skill has to work anywhere and asks the panel where things are, while a
local one can go straight to what that machine has.

The mark records which delivery it was taken against. When the shipped skill
moves on, the check says the mark is older than the delivery and asks for it to
be made again — an ignore that never expires is an ignore that hides the next
change too.

---

## The boundaries of tests

**A test touches nothing outside the project directory.** No reads for
convenience, no writes, no deletions: the home directory, `/var/lib`, `/run`,
other people's directories — all of that is out of reach. If a home directory is
needed, substitute a temporary one.

This is not an agreement but a barrier: the Go tests run with a substituted
`HOME` and `XDG_*`, the collector's tests with their own state directory, and a
run whose directory was not substituted stops at the import without having run a
single test. The price of the rule has already been paid: a test pretending to
be a fresh machine wiped a real claude configuration together with the mark that
onboarding had been done.

**The harness of the collector's tests names the place for temporary files
explicitly.** `tempfile` without `dir=` asks the system for a place, and inside
the unit's restrictions the system temporary directory is read-only — so the
whole run falls over, with four hundred identical errors among which the real
finding is invisible.

**A fresh machine is simulated on the stand** (`deploy/stand/`), not on the
working one.

---

## What is expected of a change

**A rule and its reason live next to the code.** A comment explains not "what
the line does" but why it was done this way and what breaks otherwise. A rule
that costs more than a line goes into [`ARCHITECTURE.md`](ARCHITECTURE.md) — in
the same commit as the code.

**Texts are written in the present tense.** No task numbers, no dates of
decisions, no "it used to be different", no names of who decided what: only the
current rule and its price. A measurement is worth something for its numbers and
conditions, not for the day it was taken.

**Everything in this repository is written in English** — the texts the panel
shows, the errors that travel back in the executor's answer, the logs, the
refusals at startup, the comments in the code, the names of tests and what they
report. The reader of a public repository is anyone, and so is the owner of the
machine it runs on.

The exceptions are three, and every one of them is named by file in
`TestRepositorySpeaksOneLanguage` (`internal/hostcfg`), which is what holds this
rule. A handful of string constants are keys arriving from data written earlier
rather than text meant for a human, and they keep their old spelling. The phrases
by which a shipped skill recognises a request said in Russian are not text for a
reader either. And an applied migration is untouchable down to a single
character, so whatever it says stays as it was written.

**This check walks the list git keeps, not the working tree.** A new file
enters it only once it is added, so a skill whose description carries the
Russian phrases it is recognised by passes the whole of `make check` while it
is untracked and fails on the next run. Adding an exception is a line beside
the other skills, and the run after `git add` is the one that counts.

**The text of a refusal does not promise an environment the reader may not
have.** A refusal names tmux, and a specific terminal comes second and as a
particular case: otherwise an install without one sends the human off to install
something that will never be there. Machine names and home paths are not in such
texts at all.

**Fixtures are neutral.** `/srv/proj`, `/home/u`, `wg-lab` are not the paths of
your machine. `TestFixturesCarryNoHostPaths` watches over this, and getting
around it by editing the list of forbidden strings is not a solution. Next to it
stands `TestHostIdentityLivesInDescriptionOnly`: the owner's name and home
directory are not baked into working code, their place is in the machine
description.

**A fixture refuses the way the panel refuses.** The action gate reads the
status of the answer, so a body saying `{"ok": false}` under a 200 is a success
to it, and the screen goes down the branch that follows a keypress that worked.
A fixture that means "the executor said no" answers with the status, as the
service does.

**The verdict of a run is its exit code.** `go test ./...` ends with a line per
package, so a failure scrolls past and an `ok` from a quiet neighbour stands
last. A change that does not compile at all prints no failure of its own — the
build error sits above, and the run reads green to a glance. This matters most
while checking that a test catches what it was written for: a mutation that
breaks the build proves nothing and looks like proof.

**A notion is called by one word on both sides of a boundary.** A second word
for the same notion is not free: a repository is read as a whole, and a person
told one thing by a screen and another by a protocol field will rightly ask
whether these are two things or one.

---

## Invariants the tests hold

These rules are broken silently, so checks watch over them. A red test from here
does not mean "adjust the test", it means "you broke the thing it was written
for".

| Check | What it holds |
|---|---|
| `TestEveryExecActionReachableFromUI` | every executor action is reachable from a screen: an action with no button is an unfinished job |
| `TestEveryActionButtonChecksHostKnowsIt` | an action's button asks the host whether it can do it |
| `TestSessionActionsDeclareWhatToWaitFor` | a new action over a session says what to wait for afterwards; "nothing" is an answer too, but it has to be given |
| `TestCommandListMatchesRegistry` | the list of slash commands in Go and on the panel is one list |
| `TestAPIPathsAvoidTrackerWords` | an endpoint's name takes no words from web analytics: a blocker cuts such a path before the network, and the failure is indistinguishable from a dead address |
| `TestEverySnapshotBlockIsVisibleOnScreen` | the failure of a snapshot block is named to the human on at least one screen |
| `TestDesktopRulesStayInsideMediaQuery` | a desktop rule does not touch the phone |
| `TestMigrationsHaveUniqueNumbers` | no two migrations take the same number: a duplicate stops the service from coming up |
| `TestActionTextsPromiseNoSpecificEnvironment` | action texts do not promise somebody else's environment |
| `TestAnEmptyListTravelsAsAnEmptyList` | a list that is empty is still sent: dropped by `omitempty` it reaches the screen as `undefined`, and a reader counting its length takes the panel down with it |

### A reply keeps its shape

An empty list is an answer — "nothing changed", "the directory is empty" — and
it travels as an empty list. `omitempty` on a slice drops it from the json
altogether, and the screen that counts its length finds nothing to count: it
dies in the middle of a draw, leaves what it had drawn standing, and every
redraw after it lays another screen on top. This has taken the panel down once,
with a tab that stopped answering and three copies of one screen on it.

The rule: `omitempty` is for a value that is absent, not for one that is empty.
A list, a map and a count the screen reads keep their place in the reply.

---

## Migrations

A number is taken by the creation of a file, not by an intention: two identical
numbers bring down the loading of migrations entirely.

**An applied file is untouchable.** Its checksum is recorded in the database,
and editing any character — a comment included — parts the file from the
database; after that the service does not come up, and repeating does not cure
it. That is why there are no comments in migration files at all, and the
explanation lives in the code next to whatever reads that table.

**Grants are not written in a migration.** The rights of the application role
are issued by the service at every startup: a migration is applied once in the
life of a database, and a role created later would never have got anything. A
table with a trimmed set of rights belongs in the exception list in the code, not
in a new migration.

The production database is changed only by a migration and only after a backup.

**A rule or a probe is not a migration.** What the panel watches is described in
`internal/watchcfg/config.yaml`, and the service brings the database to that
file at every startup. Changing a wording, a threshold, a hold or an interval is
a change of that file and a rebuild. A migration is for the shape of a table, not
for what stands in it.

Removing an entry from the file switches the record off; it does not delete it.
To add one, give it a key that is not taken — the key is the identity and is
never reused for something else, since a rule's alerts hang off it.

---

## Dependencies

A dependency is allowed only if `govulncheck` is clean on it. The check stands
in two places — in `make check` and inside the image build — so that the
condition does not go stale on the very first updated version.

Only a vulnerability the code actually calls brings it down: a module-level
finding with no call does not stop the build, otherwise somebody else's news
about an uninvited package would stop a hotfix rollout while protecting nothing.

For the front end a dependency is a file in `web/vendor`, not a line in
`package.json`: the head of the file records the version, the source, the licence
and the checksum of the original, and it is updated by hand and with a diff.

---

## Commits and pull requests

The message explains what changed and why, it does not retell the diff. The
first line is the gist without a full stop, then a paragraph of the reason. A
commit holds one thought: a change to the code and the rewriting of its
documentation are better sent together, and unrelated things separately.

A branch off `main`, a green `make check`, and in the description what and what
for. If the change alters behaviour somebody could have seen on a screen, say so
outright: the panel is opened from a phone on an alert, and a surprise there
costs more than one in the code.
