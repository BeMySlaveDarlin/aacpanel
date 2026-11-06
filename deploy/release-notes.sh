#!/usr/bin/env bash
# deploy/release-notes.sh vP.M.m [vP.M.m] — the notes of a release as
# Markdown on stdout: the GitHub release make release PUSH=1 creates carries
# them, and make release-notes VERSION=P.M.m prints them to be read first.
#
# The notes say the kind of the release by the rule of release.sh, against
# the release before it: a paradigm when the first number moved, a major
# when the second did, a minor otherwise. Then the subjects of the commits
# since that release, the commits of the releases left out, and how an
# install gets there. The release before is the newest release tag below
# this one, unless the second argument names it. A tag that is not made yet
# stands for HEAD, so the notes are read before the release is made.

set -euo pipefail

stop() {
	printf 'stop: %s\n' "$*" >&2
	exit 1
}

release='^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$'

# older A B says whether release A comes out before release B.
older() {
	[ "$1" != "$2" ] && [ "$(printf '%s\n%s\n' "$1" "$2" | sort -V | head -n 1)" = "$1" ]
}

main() {
	local tag=${1:-} previous=${2:-}
	[ -n "$tag" ] || stop "name the release: deploy/release-notes.sh vP.M.m, like v1.4.0"
	[[ $tag =~ $release ]] ||
		stop "$tag is not a release: the notes are of a tag vP.M.m, like deploy/release-notes.sh v1.4.0"
	cd "$(git rev-parse --show-toplevel)"

	if [ -z "$previous" ]; then
		local t
		while read -r t; do
			if [[ $t =~ $release ]] && older "$t" "$tag" && { [ -z "$previous" ] || older "$previous" "$t"; }; then
				previous=$t
			fi
		done < <(git tag --list 'v*')
	elif ! [[ $previous =~ $release ]] || ! git rev-parse -q --verify "refs/tags/$previous" >/dev/null; then
		stop "$previous is not a release tag here: name the release before $tag, like v1.3.0, or leave it out"
	fi

	local end=HEAD
	if git rev-parse -q --verify "refs/tags/$tag" >/dev/null; then
		end=$tag
	fi

	local kind
	if [ -z "$previous" ]; then
		kind="The first release of the panel."
	elif [ "${tag%%.*}" != "${previous%%.*}" ]; then
		kind="A paradigm release, after $previous: a change of approach."
	elif [ "${tag%.*}" != "${previous%.*}" ]; then
		kind="A major release, after $previous: features and migrations."
	else
		kind="A minor release, after $previous: fixes."
	fi

	local range=$end
	if [ -n "$previous" ]; then
		range=$previous..$end
	fi
	local log changes
	log=$(git log --reverse --no-merges --format='- %s' "$range")
	changes=$(printf '%s\n' "$log" | grep -v '^- Release v' || true)

	printf '%s\n\n## Changes\n\n' "$kind"
	if [ -n "$changes" ]; then
		printf '%s\n' "$changes"
	else
		printf 'Nothing but the release itself.\n'
	fi
	printf '\n## Update\n\n'
	local paradigm=${tag%%.*}
	paradigm=${paradigm#v}
	if [ -n "$previous" ] && [ "${tag%%.*}" != "${previous%%.*}" ]; then
		printf 'A plain update stays in the paradigm a clone is on, so it does not take %s. In the clone of the panel:\n\n' "$tag"
		printf '    ./install.sh update --to %s\n' "$paradigm"
	else
		printf 'In the clone of the panel:\n\n'
		printf '    ./install.sh update\n'
	fi
	printf '\nA clone on a release moves to the newest release of its paradigm, and a clone on a branch pulls it.\n'
}

main "$@"
