#!/bin/sh
# SPDX-License-Identifier: AGPL-3.0-or-later
# check-actions-pinned.sh — every third-party action must be pinned to a
# commit SHA.
#
# Why: a tag like `actions/checkout@v7` is a mutable pointer. Whoever owns
# the upstream repository can move that tag at any moment, and every workflow
# in this repo would then execute the new commit with the repository's token
# and write permissions — without a pull request, without a diff, without a
# review. Pinning to the SHA turns "trust the tag" into "trust this exact
# tree", and the human-readable version comment next to it keeps the pin
# reviewable ("which release is this?".
#
# A local action ("./some/path") is already part of this tree and needs no
# pin. Everything else must be `owner/repo@<40 hex chars>`.
#
# POSIX sh, no dependencies beyond grep/sed. Exits non-zero on drift.
set -u

fail=0
repo_root=$(cd "$(dirname "$0")/.." && pwd) || exit 1
cd "$repo_root" || exit 1

workflow_dir=".github/workflows"
if [ ! -d "$workflow_dir" ]; then
	echo "FAIL: $workflow_dir not found"
	exit 1
fi

for wf in "$workflow_dir"/*.yml "$workflow_dir"/*.yaml; do
	[ -f "$wf" ] || continue
	# Every `uses:` line, minus any trailing comment.
	grep -nE '^[[:space:]]*(-[[:space:]]*)?uses:[[:space:]]*[^[:space:]]+' "$wf" |
	while IFS= read -r line; do
		uses=$(printf '%s' "$line" | sed -E 's/.*uses:[[:space:]]*//' | sed -E 's/[[:space:]]+#.*$//' | tr -d '[:space:]')
		case "$uses" in
		./*)
			# Local action — part of this tree.
			;;
		*@*)
			ref=${uses##*@}
			# 40 hex characters is a commit SHA; anything else (v7, v2.6.1,
			# main, a branch) is a mutable pointer.
			if ! printf '%s' "$ref" | grep -Eq '^[0-9a-f]{40}$'; then
				echo "FAIL: $wf: action not pinned to a commit SHA: $uses"
				fail=1
			fi
			;;
		*)
			echo "FAIL: $wf: malformed uses: reference: $uses"
			fail=1
			;;
		esac
	done
	# The pipeline above runs the while-loop in a subshell, so a `fail=1` set
	# inside it does not survive into this shell. Detect the same condition
	# again with a direct grep and record it here, where it counts.
	if grep -E '^[[:space:]]*(-[[:space:]]*)?uses:[[:space:]]*' "$wf" |
		sed -E 's/[[:space:]]+#.*$//' |
		grep -vE '^[[:space:]]*(-[[:space:]]*)?uses:[[:space:]]*\./' |
		grep -vE '@[0-9a-f]{40}[[:space:]]*$' >/dev/null 2>&1; then
		fail=1
	fi
done

if [ "$fail" -ne 0 ]; then
	echo "check-actions-pinned: FAIL"
	exit 1
fi
echo "check-actions-pinned: OK"
exit 0
