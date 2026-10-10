GOVULNCHECK_VERSION ?= v1.7.0
GOVULNCHECK ?= go run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION)

.PHONY: check fmt vet test vuln build image front agent-import agent-test agent-confined usage-replay stream-contract codex-contract delivery-test shellcheck release

# check — everything that runs before a commit. front is here as a check, not for
# the bundle: an error in the screen markup is caught by no test. It goes first,
# since the frontend tests read the bundle; the rest do not wait on one another
# and run side by side, so the run lasts as long as its longest part, and a red
# one does not keep the others from saying what they found.
check: front
	@$(MAKE) --no-print-directory -j --output-sync=target \
		fmt vet shellcheck agent-import test agent-test agent-confined delivery-test vuln

# gofmt -l does not fix anything and says nothing through its exit code, so the
# output is what gets checked.
fmt:
	@out=$$(git ls-files -co --exclude-standard -z '*.go' | xargs -0 -r gofmt -l); test -z "$$out" || { echo "!! not gofmt-clean:"; echo "$$out"; exit 1; }

vet:
	go vet ./...

# Tests run with a substituted home directory, so they cannot reach the real one.
# The directory is made outside /tmp, which is RAM here, and outside the project,
# where walking tests would find it. Without AACP_TEST_DSN the tests with a
# database are skipped silently, hence the reminder.
#
# GOTEST_FLAGS is for a run that wants the outcome of every test by name — a CI
# that counts what was skipped, say. The substitution of the home directory is
# worth more than the convenience of calling go test by hand, so the flags come
# through here rather than around.
#
# GOTEST_PARALLEL is how many frontend fixtures run at once. A fixture spends
# its time in the pauses of its page, not on the CPU, so it is more than the
# cores: sixteen keep a machine of eight cores under six. A small CI runner
# asks for fewer.
GOTEST_FLAGS ?=
GOTEST_PARALLEL ?= 16
# The installer writes the test database it makes into .env; a DSN in the
# environment wins.
AACP_TEST_DSN ?= $(shell sed -n 's/^AACP_TEST_DSN=//p' .env 2>/dev/null | tail -n 1)
export AACP_TEST_DSN
test:
	@test -n "$$AACP_TEST_DSN" || echo "!! AACP_TEST_DSN is not set: the tests with a database will be skipped"
	@home=$$(mktemp -d "$${TMPDIR:-/var/tmp}/aacpanel-testhome.XXXXXX"); \
	trap 'chmod -R u+w "$$home" 2>/dev/null; rm -rf "$$home"' EXIT; \
	HOME="$$home" \
	XDG_CONFIG_HOME="$$home/.config" \
	XDG_DATA_HOME="$$home/.local/share" \
	XDG_STATE_HOME="$$home/.local/state" \
	XDG_CACHE_HOME="$$home/.cache" \
	GOCACHE="$$(go env GOCACHE)" \
	GOMODCACHE="$$(go env GOMODCACHE)" \
	go test -parallel $(GOTEST_PARALLEL) $(GOTEST_FLAGS) ./...

# The agent is deployed from the working tree, so a restart is a release: a name
# undefined at module level keeps the service from starting at all, and tests do
# not catch that. The installer runs the same check before it restarts the
# collector.
agent-import:
	@cd agent && python3 importcheck.py

# Agent tests run with a substituted state directory — the same barrier as the
# home directory above, since the question book lives by absolute path.
# AACP_ASK_STORE and the brief store are unset: inherited, they would lead the
# run into the real book and the real shelf.
agent-test:
	@state=$$(mktemp -d "$${TMPDIR:-/var/tmp}/aacpanel-teststate.XXXXXX"); \
	tmp=$$(mktemp -d "$${TMPDIR:-/var/tmp}/aacpanel-testtmp.XXXXXX"); \
	trap 'rm -rf "$$state" "$$tmp"' EXIT; \
	env -u AACP_ASK_STORE -u AACP_BRIEF_STORE -u AACP_BRIEF_DIR AACP_STATE_DIR="$$state" AACP_TEST_TMPDIR="$$tmp" \
	python3 -m unittest discover -s agent -t agent -p 'test_*.py'

# The same agent tests under the restrictions of the production unit: a read-only
# filesystem except the state directory. Next to agent-test rather than instead of
# it, because the user systemd manager is not on every machine.
agent-confined:
	@agent/confined.sh

# Checks that two parsing paths agree on live transcripts: a file parsed whole and
# the same file parsed halfway and finished must give the same thing. Not in
# check: transcripts are not on every machine.
usage-replay:
	@cd agent && python3 -m usage.replay $(REPLAY_ARGS)

# The stream protocol of the installed claude against every request the panel
# leans on. Not in check: it runs a live session, spends tokens and goes to the
# network. Run it before sessions move to a new version of claude.
stream-contract:
	python3 deploy/claude/stream-contract.py $(CONTRACT_ARGS)

# The app-server protocol of the installed codex against what the panel reads
# and sends of it. Not in check: codex is not on every machine. It spends
# nothing — codex only writes the schema of its protocol — so it is run every
# time the daemon has updated itself; CONTRACT_ARGS=-write takes the release as
# the reference once the report is read.
codex-contract:
	go run ./cmd/codex-contract $(CONTRACT_ARGS)

# Python scripts of the delivery (deploy/claude). Its own target rather than part
# of agent-test, which has a different import root. In check because what breaks
# here breaks silently.
delivery-test:
	python3 -m unittest discover -s deploy/claude -t deploy/claude -p 'test_*.py'

# Shell scripts have no tests, and static analysis is all that checks them before
# the stand. A missing shellcheck is reported aloud rather than skipped quietly.
SHELL_SCRIPTS = install.sh $(shell find deploy agent -name '*.sh' | sort)
shellcheck:
	@if command -v shellcheck >/dev/null 2>&1; then shellcheck $(SHELL_SCRIPTS); \
	else echo "!! shellcheck not found: the scripts are unchecked ($(SHELL_SCRIPTS))"; fi

# Fails only on vulnerabilities the code actually calls: a module-level finding
# with no call must not hold up a hotfix.
vuln:
	$(GOVULNCHECK) ./...

build:
	go build ./...

# Builds the bundle with the same toolchain as the service: there is no npm here.
front:
	go run ./cmd/webbuild

image:
	docker compose build aacpanel

# A release is the tag vP.M.m of a clean tree that passed check, with the
# release written into install.sh. The tag stays here without PUSH=1: once
# pushed it is public, so the push is asked for and never implied.
release:
	@deploy/release.sh '$(VERSION)' '$(PUSH)'
