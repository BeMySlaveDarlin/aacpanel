GOVULNCHECK_VERSION ?= v1.7.0
GOVULNCHECK ?= go run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION)

.PHONY: check fmt vet test vuln build image front agent-import agent-test agent-confined usage-replay delivery-test shellcheck skills-diff

# check — everything that runs before a commit. front is here as a check, not for
# the bundle: an error in the screen markup is caught by no test.
check: fmt vet front shellcheck agent-import test agent-test agent-confined delivery-test vuln

# gofmt -l does not fix anything and says nothing through its exit code, so the
# output is what gets checked.
fmt:
	@out=$$(gofmt -l .); test -z "$$out" || { echo "!! not gofmt-clean:"; echo "$$out"; exit 1; }

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
GOTEST_FLAGS ?=
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
	go test $(GOTEST_FLAGS) ./...

# The agent is deployed from the working tree, so a restart is a release: a name
# undefined at module level does not break one screen, it keeps the service from
# starting at all. Tests do not catch that — they import what they need.
define AGENT_IMPORT_PY
import importlib, importlib.util, pathlib, sys, traceback
bad = []
for path in sorted(pathlib.Path(".").glob("*.py")):
    if path.name.startswith("test_"):
        continue
    try:
        if "-" in path.stem:
            spec = importlib.util.spec_from_file_location(path.stem.replace("-", "_"), path)
            spec.loader.exec_module(importlib.util.module_from_spec(spec))
        else:
            importlib.import_module(path.stem)
    except BaseException as err:
        where = ""
        if not getattr(err, "filename", None):
            here = pathlib.Path(".").resolve()
            frames = [f for f in traceback.extract_tb(err.__traceback__)
                      if here in pathlib.Path(f.filename).parents]
            if frames:
                where = " (%s:%s)" % (pathlib.Path(frames[-1].filename).relative_to(here),
                                      frames[-1].lineno)
        bad.append("%s: %s: %s%s" % (path.name, type(err).__name__, err, where))
if bad:
    print("!! the collector will not come up - these modules do not import:")
    for line in bad:
        print("   " + line)
    sys.exit(1)
endef
export AGENT_IMPORT_PY

agent-import:
	@cd agent && python3 -c "$$AGENT_IMPORT_PY"

# Agent tests run with a substituted state directory — the same barrier as the
# home directory above, since the question book lives by absolute path.
# AACP_ASK_STORE is unset: inherited, it would lead the run into the real book.
agent-test:
	@state=$$(mktemp -d "$${TMPDIR:-/var/tmp}/aacpanel-teststate.XXXXXX"); \
	tmp=$$(mktemp -d "$${TMPDIR:-/var/tmp}/aacpanel-testtmp.XXXXXX"); \
	trap 'rm -rf "$$state" "$$tmp"' EXIT; \
	env -u AACP_ASK_STORE AACP_STATE_DIR="$$state" AACP_TEST_TMPDIR="$$tmp" \
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

# Python scripts of the delivery (deploy/claude). Its own target rather than part
# of agent-test, which has a different import root. In check because what breaks
# here breaks silently.
delivery-test:
	python3 -m unittest discover -s deploy/claude -t deploy/claude -p 'test_*.py'

# Shell scripts have no tests, and static analysis is all that checks them before
# the stand. A missing shellcheck is reported aloud rather than skipped quietly.
SHELL_SCRIPTS = $(shell find deploy agent -name '*.sh' | sort)
shellcheck:
	@if command -v shellcheck >/dev/null 2>&1; then shellcheck $(SHELL_SCRIPTS); \
	else echo "!! shellcheck not found: the scripts are unchecked ($(SHELL_SCRIPTS))"; fi

# Fails only on vulnerabilities the code actually calls: a module-level finding
# with no call must not hold up a hotfix.
vuln:
	$(GOVULNCHECK) ./...

build:
	go build ./...

# Whether the delivered skills differ from the ones installed in the claude
# account. Breaks nothing: a difference is a reason to look at the diff.
skills-diff:
	@dir="$${CLAUDE_CONFIG_DIR:-$$HOME/.claude}/skills"; \
	for s in deploy/claude/skills/*/; do n=$$(basename "$$s"); \
	  if [ ! -f "$$dir/$$n/SKILL.md" ]; then echo "-- $$n: not installed in $$dir"; \
	  elif cmp -s "$$s/SKILL.md" "$$dir/$$n/SKILL.md"; then echo "ok $$n"; \
	  else echo "!! $$n: differs from deploy/claude/skills/$$n/SKILL.md"; diff -u "$$s/SKILL.md" "$$dir/$$n/SKILL.md" | head -20; fi; \
	done

# Builds the bundle with the same toolchain as the service: there is no npm here.
front:
	go run ./cmd/webbuild

image:
	docker compose build aacpanel
