#!/usr/bin/env bash
# Tests for scripts/init.sh. Run with `make test-init`.
#
# Each case runs init.sh in a throwaway git repository built from a snapshot of
# the current working tree (tracked and untracked, non-ignored files), so
# uncommitted changes to init.sh are tested too. Requires git, go, gofmt, perl
# and make; swag is optional (init.sh warns and skips `make swagger`).
#
# Once a project has been initialized, the module is no longer the template's
# and the tests skip themselves.
set -euo pipefail

readonly TEMPLATE_MODULE="github.com/martinezsaweczko/go-api-template"
readonly LEFTOVER_RE='martinezsaweczko|go-api-template'

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
module="$(awk '$1 == "module" { print $2; exit }' "$repo_root/go.mod")"
if [[ "$module" != "$TEMPLATE_MODULE" ]]; then
	printf 'skip: module is %s, not the template; nothing to test\n' "$module"
	exit 0
fi

work="$(mktemp -d "${TMPDIR:-/tmp}/init-test.XXXXXX")"
trap 'rm -rf "$work"' EXIT

failures=0
passes=0

pass() {
	passes=$((passes + 1))
	printf 'ok   %s\n' "$*"
}

fail() {
	failures=$((failures + 1))
	printf 'FAIL %s\n' "$*" >&2
}

git_test() {
	git -c user.name=init-test -c user.email=init-test@example.invalid -c commit.gpgsign=false "$@"
}

# Snapshot the working tree into a base repository with a single commit.
base="$work/base"
mkdir -p "$base"
files=()
while IFS= read -r -d '' file; do
	[[ -e "$repo_root/$file" ]] && files+=("$file")
done < <(git -C "$repo_root" ls-files -z --cached --others --exclude-standard)
tar -C "$repo_root" -cf - -- "${files[@]}" | tar -C "$base" -xf -
git -C "$base" init -q
git -C "$base" add -A
git_test -C "$base" commit -qm "snapshot"

# new_clone prints the path of a fresh, clean clone of the snapshot.
new_clone() {
	local dir
	dir="$(mktemp -d "$work/clone.XXXXXX")"
	git clone -q "$base" "$dir"
	printf '%s' "$dir"
}

# run_init DIR LOG ARGS... runs init.sh inside DIR, writes its output to LOG and
# returns its exit status.
run_init() {
	local dir="$1" log="$2"
	shift 2
	(cd "$dir" && scripts/init.sh "$@") >"$log" 2>&1
}

# expect_failure NAME DIR MESSAGE ARGS...: init.sh must fail, print MESSAGE and
# leave the tree clean.
expect_failure() {
	local name="$1" dir="$2" message="$3"
	shift 3
	local log="$work/$name.log"
	if run_init "$dir" "$log" "$@"; then
		fail "$name: init.sh succeeded, expected failure"
		return
	fi
	if ! contains "$message" "$log"; then
		fail "$name: output lacks '$message'"
		sed 's/^/     | /' "$log" >&2
		return
	fi
	if [[ -n "$(git -C "$dir" status --porcelain)" ]]; then
		fail "$name: working tree changed"
		git -C "$dir" status --short >&2
		return
	fi
	pass "$name"
}

# contains PATTERN FILE: fixed-string search that does not depend on grep/rg.
contains() {
	PATTERN="$1" perl -ne 'if (index($_, $ENV{PATTERN}) >= 0) { $found = 1 } END { exit($found ? 0 : 1) }' "$2"
}

readonly ARGS=(github.com/acme/orders-api orders-api acme)

# --- argument and precondition guards -----------------------------------------

dir="$(new_clone)"
expect_failure "invalid module path" "$dir" "invalid module path" not-a-module orders-api
expect_failure "invalid app name" "$dir" "invalid app name" github.com/acme/orders-api Orders_API
expect_failure "template module as target" "$dir" "template's own path" "$TEMPLATE_MODULE" orders-api

dir="$(new_clone)"
printf '\nlocal edit\n' >>"$dir/README.md"
log="$work/dirty.log"
if run_init "$dir" "$log" "${ARGS[@]}"; then
	fail "dirty tree: init.sh succeeded, expected failure"
elif ! contains "uncommitted changes" "$log"; then
	fail "dirty tree: output lacks 'uncommitted changes'"
elif [[ "$(git -C "$dir" status --porcelain)" != " M README.md" ]]; then
	fail "dirty tree: tree changed beyond the local edit"
else
	pass "dirty tree without --force"
fi

dir="$(new_clone)"
perl -pi -e 's/^Copyright \(c\).*$/All rights reserved./' "$dir/LICENSE"
git_test -C "$dir" commit -qam "license without copyright line"
expect_failure "copyright line missing" "$dir" "no 'Copyright (c) YEAR ...' line" --copyright "Acme Inc." "${ARGS[@]}"

# --- dry run ------------------------------------------------------------------

dir="$(new_clone)"
log="$work/dry-run.log"
if ! run_init "$dir" "$log" --dry-run --copyright "Acme Inc." "${ARGS[@]}"; then
	fail "dry run: init.sh failed"
	sed 's/^/     | /' "$log" >&2
elif [[ -n "$(git -C "$dir" status --porcelain)" ]]; then
	fail "dry run: working tree changed"
elif ! contains "would replace" "$log"; then
	fail "dry run: no planned replacements listed"
else
	pass "dry run leaves the tree unchanged"
fi

# --- rollback after a failure mid-run -----------------------------------------

# A go shim that fails `go mod tidy` and delegates everything else.
shim="$work/shim"
mkdir -p "$shim"
real_go="$(command -v go)"
cat >"$shim/go" <<EOF
#!/usr/bin/env bash
if [[ "\${1:-} \${2:-}" == "mod tidy" ]]; then
	echo "simulated go mod tidy failure" >&2
	exit 1
fi
exec "$real_go" "\$@"
EOF
chmod +x "$shim/go"

dir="$(new_clone)"
log="$work/rollback.log"
if PATH="$shim:$PATH" run_init "$dir" "$log" --copyright "Acme Inc." "${ARGS[@]}"; then
	fail "rollback: init.sh succeeded despite the failing go mod tidy"
elif ! contains "simulated go mod tidy failure" "$log" || ! contains "rolled back" "$log"; then
	fail "rollback: output lacks the failure or the rollback message"
	sed 's/^/     | /' "$log" >&2
elif [[ -n "$(git -C "$dir" status --porcelain)" ]]; then
	fail "rollback: working tree not restored"
	git -C "$dir" status --short >&2
else
	pass "rollback restores the tree after a failure"
fi

# With --force the tree may hold user work, so a failure must not roll back.
dir="$(new_clone)"
printf '\nlocal edit\n' >>"$dir/README.md"
log="$work/force.log"
if PATH="$shim:$PATH" run_init "$dir" "$log" --force "${ARGS[@]}"; then
	fail "force: init.sh succeeded despite the failing go mod tidy"
elif ! contains "nothing was rolled back" "$log"; then
	fail "force: output lacks the no-rollback message"
	sed 's/^/     | /' "$log" >&2
elif ! contains "local edit" "$dir/README.md"; then
	fail "force: the local edit was lost"
else
	pass "--force failure keeps user changes and does not roll back"
fi

# --- successful run -----------------------------------------------------------

dir="$(new_clone)"
log="$work/success.log"
if ! run_init "$dir" "$log" --copyright "Acme Inc." "${ARGS[@]}"; then
	fail "init: init.sh failed"
	sed 's/^/     | /' "$log" >&2
else
	pass "init succeeds"

	got="$(awk '$1 == "module" { print $2; exit }' "$dir/go.mod")"
	if [[ "$got" == "github.com/acme/orders-api" ]]; then
		pass "go.mod module renamed"
	else
		fail "go.mod module is '$got'"
	fi

	if contains "Copyright (c) $(date +%Y) Acme Inc." "$dir/LICENSE"; then
		pass "LICENSE holder updated"
	else
		fail "LICENSE holder not updated"
	fi

	rc=0
	leftovers="$(git -C "$dir" grep -nIE -e "$LEFTOVER_RE" -- . \
		':(exclude)LICENSE' ':(exclude)CHANGELOG.md' \
		':(exclude)scripts/init.sh' ':(exclude)scripts/init_test.sh')" || rc=$?
	if [[ $rc -gt 1 ]]; then
		fail "leftover check: git grep failed (exit $rc)"
	elif [[ -n "$leftovers" ]]; then
		fail "old names remain:"
		printf '     | %s\n' "$leftovers" >&2
	else
		pass "no old names left outside the exclusions"
	fi

	if (cd "$dir" && go build ./...) >"$work/build.log" 2>&1; then
		pass "go build ./... in the initialized project"
	else
		fail "go build ./... failed"
		sed 's/^/     | /' "$work/build.log" >&2
	fi

	git_test -C "$dir" add -A
	git_test -C "$dir" commit -qm "initialized"
	expect_failure "rerun after init" "$dir" "already initialized" "${ARGS[@]}"
fi

printf '\n%d passed, %d failed\n' "$passes" "$failures"
[[ $failures -eq 0 ]]
