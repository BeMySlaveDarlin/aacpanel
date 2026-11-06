#!/usr/bin/env bash
# test-summary.sh LOG [--database|--chrome] — what a verbose go test run ran
# and skipped, read off its log by a CI job.
#
# A green run says nothing until it is known what it skipped: the tests that
# need a live host skip themselves, the ones that need the database or Chrome
# must not. --database fails the job when the tests with a database did not
# run, --chrome when the frontend fixtures found no Chrome to run in.
set -uo pipefail

log=${1:?usage: test-summary.sh LOG [--database|--chrome]}
need=${2:-}

test -s "$log" || { echo "the tests left no log"; exit 1; }
echo "passed:  $(grep -c '^--- PASS' "$log" || true)"
echo "skipped: $(grep -c '^--- SKIP' "$log" || true)"
echo
# What the run costs is read off the run: a package that grew shows here
# before the whole job is felt to be slow.
echo "the slowest packages:"
grep -E '^(ok|FAIL)[[:space:]]' "$log" | sort -t$'\t' -k3 -rn | head -n 5 || true
echo
echo "why the skipped ones were skipped:"
grep -B1 '^--- SKIP' "$log" \
	| grep -oE '[a-z_]+\.go:[0-9]+: .*' \
	| sed -E 's/^[a-z_]+\.go:[0-9]+: //' \
	| sort | uniq -c | sort -rn
echo

case $need in
--database)
	if grep -q 'AACP_TEST_DSN is not set' "$log"; then
		echo "!! the tests with a database did not run: the service was not reached"
		exit 1
	fi
	echo "the tests with a database ran: the database was reachable"
	;;
--chrome)
	if grep -q 'no Chrome on this machine' "$log"; then
		echo "!! the frontend fixtures did not run: the runner has no Chrome"
		exit 1
	fi
	echo "the frontend fixtures ran in Chrome"
	;;
esac
