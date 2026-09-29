#!/usr/bin/env bash
# make release VERSION=P.M.m [PUSH=1] — a release of the panel. There are no
# artifacts: a release is the annotated tag vP.M.m, which install.sh taken
# with curl clones and ./install.sh update moves to. P.M.m is Paradigm.Major.
# Minor: a paradigm is a change of approach, which an update crosses only
# when asked; a major brings features and migrations; a minor brings fixes.
#
# The tree has to be clean and make check green, with the tests that need a
# database among them. The release is then written into install.sh in a
# commit of its own, so the script taken from the tag clones that very tag,
# and the tag goes on that commit. It stays here: a pushed tag is public and
# out for good, so it leaves only with PUSH=1, together with the branch.

set -euo pipefail

stop() {
	printf 'stop: %s\n' "$*" >&2
	exit 1
}

main() {
	local version=${1:-} push=${2:-}
	[[ $version =~ ^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]] ||
		stop "VERSION=$version is not P.M.m: plain numbers, no v, like make release VERSION=1.4.0"
	local tag=v$version
	cd "$(git rev-parse --show-toplevel)"

	if [ -n "$(git status --porcelain)" ]; then
		git status --short >&2
		stop "the tree has changes: a release is made of commits only. Commit them or put them aside, then run again."
	fi
	if git rev-parse -q --verify "refs/tags/$tag" >/dev/null; then
		stop "$tag is there already: a release is never made twice. Take the next number."
	fi
	[ -n "${AACP_TEST_DSN:-}" ] ||
		stop "AACP_TEST_DSN is not set, and make check would skip the tests with a database. Point it at the test database, then run again."

	"${MAKE:-make}" check || stop "make check is red: a release is made of a tree that passes it."
	if [ -n "$(git status --porcelain)" ]; then
		git status --short >&2
		stop "make check changed the tree: what it made is not in the commits. Commit it, then run again."
	fi

	sed -i "s/^RELEASE=.*/RELEASE=$tag/" install.sh
	grep -qx "RELEASE=$tag" install.sh ||
		stop "install.sh has no RELEASE= line to write the release into: the download from the tag would not know it."
	# A tag dropped before it was pushed leaves its release written in, and
	# the same release made again goes on the commit that has it.
	if ! git diff --quiet -- install.sh; then
		git commit --quiet -m "Release $tag" -- install.sh
	fi
	git tag -a "$tag" -m "aacpanel $tag"

	if [ "$push" = 1 ]; then
		git push --atomic origin HEAD "refs/tags/$tag"
		printf '%s is released and pushed.\n' "$tag"
		return
	fi
	printf '%s is tagged here and not pushed. Publish it with: git push --atomic origin HEAD refs/tags/%s\n' "$tag" "$tag"
}

main "$@"
