# How to change the panel

## What the machine needs

Go, python3, docker with compose, `shellcheck`. Everything else arrives on its
own: the front end is built with the same toolchain as the service, there is no
npm in the project and there will not be.

A database for the tests is not required — but without it every test that needs
one is skipped **silently**: rollups, retention, the action journal, the profile
map, the choice of pushes. A green run without it means only that the code
compiled.
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

The executor is built separately and installed by hand, beside the running
file and renamed over it:

```bash
go build -o ~/bin/aacpanel-exec.new ./cmd/aacpanel-exec
mv ~/bin/aacpanel-exec.new ~/bin/aacpanel-exec
systemctl --user restart aacpanel-exec
```

The holders of stream sessions run the same file (`aacpanel-exec -hold`), so
stopping the unit does not free it, and a build over it answers "text file
busy". The rename leaves a live holder on the copy it started with until its
session ends.

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

That is everything at once — the bundle first, the rest side by side, so the
run lasts as long as its longest part and a red target does not hide what the
others found:

| Target | What it checks |
|---|---|
| `fmt` | `gofmt -l` over the files the repository tracks or would add — the format is not fixed but shown: one unnoticed file in the output drowns the next finding |
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
database quietly skipped is failed on purpose: without `AACP_TEST_DSN` every
test with a database passes by doing nothing, and the run stays green.

Separately, outside `check`, because they need what not every machine has — live transcripts, a signed-in claude and the tokens it spends:

```bash
make usage-replay    # incremental usage collection against a straight pass over live transcripts
make stream-contract # the stream protocol of the installed claude, before sessions move to a new version
```

---

## The boundaries of tests

**A test touches nothing outside the project directory.** No reads for
convenience, no writes, no deletions: the home directory, `/var/lib`, `/run`,
other people's directories — all of that is out of reach. If a home directory is
needed, substitute a temporary one.

This is not an agreement but a barrier: the Go tests run with a substituted
`HOME` and `XDG_*`, the collector's tests with their own state directory, and a
run whose directory was not substituted stops at the import without having run a
single test. The price of going without it is real: a test pretending to be a
fresh machine against the real home directory wipes the claude configuration
together with the mark that onboarding was done.

**The harness of the collector's tests names the place for temporary files
explicitly.** `tempfile` without `dir=` asks the system for a place, and inside
the unit's restrictions the system temporary directory is read-only — so the
whole run falls over, with four hundred identical errors among which the real
finding is invisible.

**A fresh machine is simulated on the stand** (`deploy/stand/`), not on the
working one.

---

## The installer

`./install.sh` builds `cmd/aacpanel-install` with a Go of its own and runs it.
While working on it, the same installer runs from the clone with the Go on your
`PATH`:

```bash
go run ./cmd/aacpanel-install plan    # the check, the questions and the plan; changes nothing
go run ./cmd/aacpanel-install demo    # the whole install on a made-up machine; changes nothing
```

`install`, `update` and `uninstall` change the machine they run on: they run on
the stand.

**A change that leaves a trace on a host is a step with its undo.** Every step
is written to the contract in `internal/install/step.go`: `Done` looks at the
machine itself, `Apply` records every piece in the manifest before it makes it,
`Verify` checks the machine after, and the undo takes a line of the manifest
back and is idempotent. A new kind of line gets its undo in
`internal/install/undo.go`; `Record` refuses a kind without one, and
`TestEveryKindHasAnUndo` holds it. A thing put on the machine with no line in
the manifest is a thing uninstall leaves behind.

**A new question is data.** It goes into `internal/install/questions.go` — its
options from what the machine gives, the source of each, where the answer is
written — with its flag in `internal/install/flags.go`. `TestEveryQuestionHasAFlag`
and `TestYesAsksNothing` hold that a run with nobody at a terminal answers it
too. A question changes a screen, and the screen has a golden file.

**The screens are compared with golden files**, `testdata/*.golden` of
`internal/install/ui` and `internal/install/view`, at 60 and 80 columns. A change
of a screen that is meant rewrites them, and their diff is read before the
commit:

```bash
go test ./internal/install/ui ./internal/install/view -update
go test ./cmd/aacpanel-install -run Bare -update    # the refusals of a bare machine, in a container
```

`-update` is a flag of those packages' tests alone: given to
`./internal/install/...` it stops every other package with "flag provided but
not defined".

**The stand and the CI job run it for real.** The stand's virtual machine
(`deploy/stand/vm.sh`, its variants in `deploy/stand/README.md`) is where the
installer runs whole: Ubuntu or Debian 13, a bare machine, a user of uid 1001,
sudo with a password. `deploy/stand/scenario.sh` answers its questions through
tmux, a key only after the screen it answers is seen — a digit picks at once, so
a key sent blind answers a question nobody saw. `deploy/stand/traces.sh` lists
what the machine holds before the install and after the uninstall, and compares
the two. The same run goes in `.github/workflows/install.yml` on a GitHub
runner — install, check, a second run that must restart nothing, uninstall with
the data, the traces compared — on a pull request that touches the installer or
what it puts on the machine, and once a week. `act` does not run that job: its
containers have no systemd.

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
by which a tool of the panel, in its line to a session, recognises a request
said in Russian are not text for a reader either. And the range of letters a
check walks over and a file name as the desktop of a machine in another locale
writes it are data the code has to accept.

**This check walks the list git keeps, not the working tree.** A new file
enters it only once it is added, so a file carrying the Russian phrases a tool
is recognised by passes the whole of `make check` while it is untracked and
fails on the next run. Adding an exception is a line beside the other tools,
and the run after `git add` is the one that counts.

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
| `TestRetiredMigrationNumbersStayRetired` | the number of a removed migration never gets a file again: a database that logged it would skip the new file without a word |
| `TestActionTextsPromiseNoSpecificEnvironment` | action texts do not promise somebody else's environment |
| `TestTheLauncherTakesEveryKeyOfTheSchema` | the launcher reads exactly the launch parameters the schema lists: a key one of them knows and the other does not is saved and then never launched |
| `TestPreviewIsTheCommandTheLaunchRuns` | the command a settings page shows is the one the launch runs, built by the same code |
| `TestProfilesSchemaNeedsNoDatabase` | the schema is answered with the database down: the screens are drawn from it |
| `TestSchemaIsWellFormed` | every parameter of the schema names its levels, what an absent value leaves to and when a live session takes a change |
| `TestTheServersWordFitsWhatClaudeKeeps` | the word the panel's MCP server gives a session fits the 4096 UTF-16 units claude keeps of it under the ceiling the launcher lifts, even with the longest checklist: past them claude cuts it, and the lines of the last tools never reach the model |
| `TestAnEmptyListTravelsAsAnEmptyList` | a list that is empty is still sent: dropped by `omitempty` it reaches the screen as `undefined`, and a reader counting its length takes the panel down with it |

### A reply keeps its shape

An empty list is an answer — "nothing changed", "the directory is empty" — and
it travels as an empty list. `omitempty` on a slice drops it from the json
altogether, and the screen that counts its length finds nothing to count: it
dies in the middle of a draw, leaves what it had drawn standing, and every
redraw after it lays another screen on top: a tab that stops answering, with
three copies of one screen on it.

The rule: `omitempty` is for a value that is absent, not for one that is empty.
A list, a map and a count the screen reads keep their place in the reply.

---

## Migrations

A number is taken by the creation of a file, not by an intention: two identical
numbers bring down the loading of migrations entirely.

**An applied migration is never run again.** The database logs the number and
the name of every file it has run in `schema_version`, and at startup the
service applies the files whose number is not there — in order, each in a
transaction of its own. Nothing compares an applied file with anything, so a
change of schema ships as a new file under a new number. The cost, plainly:
editing an applied file changes nothing on a database that has already run it.
Only a database set up afterwards gets the new text, and the two part without a
word. A number works the same way: a database that has logged it never runs a
new file under it, so a number is not reused. A start that finds a number
logged under another name than its file's says in the service log that the
number was reused and the file will not run.

A number in the log with no file beside it — a file removed from the tree, or a
database newer than the binary — is named in the service log at startup and
stops nothing. Removing a file retires its number: it goes into
`retiredMigrations` in `internal/store/store_test.go`, and
`TestRetiredMigrationNumbersStayRetired` refuses a file that takes it again.

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
