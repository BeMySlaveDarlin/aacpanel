GOVULNCHECK_VERSION ?= v1.7.0
GOVULNCHECK ?= go run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION)

.PHONY: check fmt vet test test-go test-web vuln build image front agent-import agent-test agent-confined usage-replay stream-contract codex-contract shellcheck release

# check — everything that runs before a commit. front is here as a check, not for
# the bundle: an error in the screen markup is caught by no test. It goes first,
# since the frontend tests read the bundle; the rest do not wait on one another
# and run side by side, so the run lasts as long as its longest part, and a red
# one does not keep the others from saying what they found.
#
# The run ends with the wall time of every part, the longest first: what a
# change adds to the cost of a check is read off the check itself, and the
# longest part is the length of the whole run.
#
# CHECK_JOBS caps how many parts run at once; empty runs them all together. A
# shared machine sets it, with GOMAXPROCS for the test binaries: each of them
# otherwise takes every core, whatever -p says about how many run. The parts
# are listed longest first, since make starts them in that order: the
# frontend checks and the Go tests are the length of the run, and what is
# short fits in beside them.
CHECKS = test-web test-go agent-test agent-confined vet vuln fmt shellcheck agent-import
CHECK_JOBS ?=
check:
	@times=$$(mktemp -d "$${TMPDIR:-/var/tmp}/aacpanel-checktimes.XXXXXX"); \
	trap 'rm -rf "$$times"' EXIT; \
	export CHECK_TIMES="$$times"; \
	$(MAKE) --no-print-directory timed-front && \
	$(MAKE) --no-print-directory -j$(CHECK_JOBS) --output-sync=target $(CHECKS:%=timed-%); \
	rc=$$?; \
	echo; echo "check: wall time of each part"; \
	cat "$$times"/* | LC_ALL=C sort -k2,2nr | \
		LC_ALL=C awk '{ printf "  %-15s %6.1f s%s\n", $$1, $$2 / 1000, ($$3 == 0 ? "" : "  FAILED") }'; \
	exit $$rc

# timed-<part> runs a part of check and writes down how long it took.
timed-%:
	@start=$$(date +%s%N); \
	$(MAKE) --no-print-directory $*; rc=$$?; \
	echo "$* $$(( ($$(date +%s%N) - start) / 1000000 )) $$rc" > "$$CHECK_TIMES/$*"; \
	exit $$rc

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
# The build and module caches are the machine's, read before the home is
# substituted: asked under the new home, go names caches inside it, and every
# run builds the standard library and the modules from nothing and downloads
# the modules again. A cached test result is not taken (-count=1): every test
# runs on every check, as it would on a tree nobody built before.
#
# GOTEST_FLAGS is for a run that wants the outcome of every test by name — a CI
# that counts what was skipped, say. The substitution of the home directory is
# worth more than the convenience of calling go test by hand, so the flags come
# through here rather than around.
#
# The frontend checks (test-web) and the other packages (test-go) are two
# parts of check that run side by side: the frontend checks are one package
# that is the length of the run on its own, and the rest is many packages that
# wait more than they work.
#
# GOTEST_PARALLEL is how many frontend fixtures run at once. A fixture keeps a
# renderer of the shared Chrome busy while its page runs, so it is about a
# core: a machine sets it to the cores it gives the check, less what the other
# parts take.
#
# GOTEST_PACKAGES is how many packages of test-go build and run at once. Their
# tests mostly wait on fakes, a database and timers, so a few at once cost
# little more than one. The packages that take longest are named first: go
# test starts them in the order it is given them, and the longest one started
# last would be the tail of the run on its own.
SLOW_PACKAGES = ./internal/executor/ ./internal/store/ ./internal/install/ ./cmd/aacpanel/
GOTEST_FLAGS ?=
GOTEST_PARALLEL ?= 16
GOTEST_PACKAGES ?= 4
# The installer writes the test database it makes into .env; a DSN in the
# environment wins.
AACP_TEST_DSN ?= $(shell sed -n 's/^AACP_TEST_DSN=//p' .env 2>/dev/null | tail -n 1)
export AACP_TEST_DSN
define testhome
gocache=$$(go env GOCACHE) gomodcache=$$(go env GOMODCACHE); \
home=$$(mktemp -d "$${TMPDIR:-/var/tmp}/aacpanel-testhome.XXXXXX"); \
trap 'chmod -R u+w "$$home" 2>/dev/null; rm -rf "$$home"' EXIT; \
HOME="$$home" \
XDG_CONFIG_HOME="$$home/.config" \
XDG_DATA_HOME="$$home/.local/share" \
XDG_STATE_HOME="$$home/.local/state" \
XDG_CACHE_HOME="$$home/.cache" \
GOCACHE="$$gocache" \
GOMODCACHE="$$gomodcache"
endef
test: test-go test-web

test-go:
	@test -n "$$AACP_TEST_DSN" || echo "!! AACP_TEST_DSN is not set: the tests with a database will be skipped"
	@$(testhome) \
	go test -count=1 -p $(GOTEST_PACKAGES) $(GOTEST_FLAGS) $(SLOW_PACKAGES) $$(go list ./... | grep -v '/web/check$$')

# GOTEST_RUN, from the environment, narrows test-web to the tests it matches:
# a CI that splits the fixtures between runners names each runner's share.
test-web:
	@$(testhome) \
	go test -count=1 -parallel $(GOTEST_PARALLEL) $(GOTEST_FLAGS) $${GOTEST_RUN:+-run "$$GOTEST_RUN"} ./web/check/

# The agent is deployed from the working tree, so a restart is a release: a name
# undefined at module level keeps the service from starting at all, and tests do
# not catch that. The installer runs the same check before it restarts the
# collector.
agent-import:
	@cd agent && python3 importcheck.py

# Agent tests run with a substituted state directory — the same barrier as the
# home directory above, since the question book lives by absolute path.
# AACP_ASK_STORE and the brief store are unset: inherited, they would lead the
# run into the real book and the real shelf. The contour variables are unset for
# the same reason: a session the panel started carries the machine's contours.
agent-test:
	@state=$$(mktemp -d "$${TMPDIR:-/var/tmp}/aacpanel-teststate.XXXXXX"); \
	tmp=$$(mktemp -d "$${TMPDIR:-/var/tmp}/aacpanel-testtmp.XXXXXX"); \
	trap 'rm -rf "$$state" "$$tmp"' EXIT; \
	env -u AACP_ASK_STORE -u AACP_BRIEF_STORE -u AACP_BRIEF_DIR -u AACP_CLAUDE_HOME -u AACP_CLAUDE_REGISTRY \
	AACP_STATE_DIR="$$state" AACP_TEST_TMPDIR="$$tmp" \
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
	go run ./cmd/stream-contract $(CONTRACT_ARGS)

# The app-server protocol of the installed codex against what the panel reads
# and sends of it. Not in check: codex is not on every machine. It spends
# nothing — codex writes its schema and lists its hooks without a thread,
# through the daemon or a private stdio app-server if its socket is absent;
# CODEX_HOME selects the home to check. CONTRACT_ARGS=-write takes the release as
# the reference once the report is read.
codex-contract:
	go run ./cmd/codex-contract $(CONTRACT_ARGS)

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
