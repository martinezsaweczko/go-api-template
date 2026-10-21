#!/usr/bin/env bash
# Bootstrap a project created from go-api-template: rename the Go module, the
# application name and (optionally) the GitHub owner, then verify the result.
#
# Usage: scripts/init.sh [--dry-run] [--force] [--copyright "Name"] <module-path> <app-name> [github-owner]
# Example: scripts/init.sh github.com/acme/orders-api orders-api acme
#
# The script checks everything it can before writing. If a later step fails
# (go mod tidy, make swagger, go build, go test...), it restores the working
# tree to HEAD, so it is safe to fix the cause and run it again. With --force
# (uncommitted changes allowed) it never rolls back; it prints what changed.
#
# Run it once, right after creating the repository, then delete it together
# with scripts/init_test.sh (its test, run by `make test-init`).
set -euo pipefail

readonly OLD_MODULE="github.com/martinezsaweczko/go-api-template"
readonly OLD_OWNER="martinezsaweczko"
readonly OLD_APP="go-api-template"
readonly OLD_TITLE="Go API Template"

# Files the blanket text replacement never touches:
#   LICENSE                the holder changes only through --copyright
#   CHANGELOG.md           release history keeps the old names (if present)
#   go.mod                 the module is renamed by `go mod edit -module`
#   scripts/init.sh        this script
#   scripts/init_test.sh   its test, which must keep the template names
readonly EXCLUDES=(
	':(exclude)LICENSE'
	':(exclude)CHANGELOG.md'
	':(exclude)go.mod'
	':(exclude)scripts/init.sh'
	':(exclude)scripts/init_test.sh'
)

# Matches the copyright line rewritten by --copyright.
readonly COPYRIGHT_RE='^Copyright \(c\) \d{4}'

usage() {
	cat <<'EOF'
Usage: scripts/init.sh [options] <module-path> <app-name> [github-owner]

Arguments:
  module-path    New Go module path, e.g. github.com/acme/orders-api
  app-name       Lowercase name for binaries, images and defaults, e.g. orders-api
  github-owner   Optional GitHub user or organization for CODEOWNERS and badges

Options:
  --dry-run            Show what would change and exit without modifying anything
  --force              Run even if the git working tree has uncommitted changes
                       (no automatic rollback on failure)
  --copyright NAME     Replace the copyright holder in LICENSE
  -h, --help           Show this help
EOF
}

die() {
	printf 'error: %s\n' "$*" >&2
	exit 1
}

info() {
	printf '==> %s\n' "$*"
}

warn() {
	printf 'warning: %s\n' "$*" >&2
}

# ---------------------------------------------------------------------------
# Preflight: nothing below this section writes to the working tree until
# "writes_started=1".
# ---------------------------------------------------------------------------

dry_run=0
force=0
copyright=""
positional=()

while [[ $# -gt 0 ]]; do
	case "$1" in
	--dry-run) dry_run=1 ;;
	--force) force=1 ;;
	--copyright)
		[[ $# -ge 2 ]] || die "--copyright needs a value"
		copyright="$2"
		shift
		;;
	-h | --help)
		usage
		exit 0
		;;
	-*) die "unknown option: $1 (see --help)" ;;
	*) positional+=("$1") ;;
	esac
	shift
done

if [[ ${#positional[@]} -lt 2 || ${#positional[@]} -gt 3 ]]; then
	usage >&2
	exit 2
fi

new_module="${positional[0]}"
new_app="${positional[1]}"
new_owner="${positional[2]:-}"

[[ "$new_module" =~ ^[a-z0-9]+([.-][a-z0-9]+)*\.[a-z]{2,}(/[A-Za-z0-9._~-]+)+$ ]] ||
	die "invalid module path '$new_module' (expected something like github.com/acme/orders-api)"
[[ "$new_module" != "$OLD_MODULE" ]] || die "the new module path is the template's own path"
[[ "$new_app" =~ ^[a-z][a-z0-9-]*$ ]] ||
	die "invalid app name '$new_app' (lowercase letters, digits and dashes, starting with a letter)"
if [[ -n "$new_owner" ]]; then
	[[ "$new_owner" =~ ^[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?$ ]] ||
		die "invalid GitHub owner '$new_owner'"
fi
[[ "$copyright" != *$'\n'* ]] || die "--copyright must be a single line"

missing=()
for tool in git go gofmt perl make; do
	command -v "$tool" >/dev/null 2>&1 || missing+=("$tool")
done
[[ ${#missing[@]} -eq 0 ]] || die "required tools not found: ${missing[*]}"
have_swag=0
if command -v swag >/dev/null 2>&1; then
	have_swag=1
fi

# Title Case name for the OpenAPI title: orders-api -> Orders API.
title_case() {
	local word out=""
	local IFS='-'
	for word in $1; do
		case "$word" in
		api | http | id | jwt | url | ui) word="${word^^}" ;;
		*) word="${word^}" ;;
		esac
		out+="${out:+ }$word"
	done
	printf '%s' "$out"
}
new_title="$(title_case "$new_app")"

repo_root="$(git rev-parse --show-toplevel 2>/dev/null)" || die "run this script inside the git repository"
cd "$repo_root"

[[ -f go.mod ]] || die "go.mod not found in $repo_root"
current_module="$(awk '$1 == "module" { print $2; exit }' go.mod)"
[[ "$current_module" == "$OLD_MODULE" ]] ||
	die "module is '$current_module', expected '$OLD_MODULE' (already initialized?)"

status="$(git status --porcelain)" || die "git status failed"
if [[ $dry_run -eq 0 && $force -eq 0 && -n "$status" ]]; then
	die "the working tree has uncommitted changes; commit or stash them, or pass --force"
fi

if [[ -n "$copyright" ]]; then
	[[ -f LICENSE ]] || die "--copyright given but LICENSE does not exist"
	RE="$COPYRIGHT_RE" perl -ne 'if (/$ENV{RE}/) { $found = 1 } END { exit($found ? 0 : 1) }' LICENSE ||
		die "--copyright given but LICENSE has no 'Copyright (c) YEAR ...' line to replace"
fi

# Literal replacements, applied in order (olds[i] -> news[i]). The URL rule
# runs before the module rule so links point at the GitHub repository even
# when the module path is hosted elsewhere.
olds=()
news=()
add_rule() {
	olds+=("$1")
	news+=("$2")
}
if [[ -n "$new_owner" ]]; then
	add_rule "https://github.com/$OLD_OWNER/$OLD_APP" "https://github.com/$new_owner/$new_app"
fi
add_rule "$OLD_MODULE" "$new_module"
if [[ -n "$new_owner" ]]; then
	add_rule "$OLD_OWNER/$OLD_APP" "$new_owner/$new_app"
	add_rule "$OLD_OWNER" "$new_owner"
fi
add_rule "$OLD_APP" "$new_app"
add_rule "$OLD_TITLE" "$new_title"

# files_with prints the tracked text files that contain the literal string $1,
# one per line. git grep exits 1 for "no match", which is fine; any other
# failure aborts the script (and rolls back if writes already started).
files_with() {
	local rc=0
	git grep -lIF -e "$1" -- . "${EXCLUDES[@]}" || rc=$?
	[[ $rc -le 1 ]] || die "git grep failed (exit $rc) while searching for '$1'"
}

# replace_in_files replaces the literal $1 with $2 in every given file.
replace_in_files() {
	local old="$1" new="$2"
	shift 2
	OLD="$old" NEW="$new" perl -pi -e 's/\Q$ENV{OLD}\E/$ENV{NEW}/g' -- "$@"
}

info "Module : $OLD_MODULE -> $new_module"
info "App    : $OLD_APP -> $new_app (title: $new_title)"
if [[ -n "$new_owner" ]]; then
	info "Owner  : $OLD_OWNER -> $new_owner"
else
	warn "no github-owner given: CODEOWNERS and owner-specific links keep '$OLD_OWNER'"
fi
if [[ $have_swag -eq 0 ]]; then
	warn "swag not found: docs/ will be updated textually only; install swag and run 'make swagger' afterwards"
fi

if [[ $dry_run -eq 1 ]]; then
	info "would set the module in go.mod to '$new_module' (go mod edit -module)"
	for i in "${!olds[@]}"; do
		out="$(files_with "${olds[$i]}")"
		[[ -n "$out" ]] || continue
		info "would replace '${olds[$i]}' -> '${news[$i]}' in:"
		while IFS= read -r file; do
			printf '      %s\n' "$file"
		done <<<"$out"
	done
	if [[ -n "$copyright" ]]; then
		info "would set the LICENSE copyright holder to '$copyright'"
	fi
	info "dry run: nothing was changed"
	exit 0
fi

# ---------------------------------------------------------------------------
# Writes. From here on, a failure triggers on_exit.
# ---------------------------------------------------------------------------

writes_started=0
finished=0

rollback() {
	git restore --source=HEAD --staged --worktree -- . &&
		git clean -fdq -- .
}

on_exit() {
	local rc=$?
	[[ $writes_started -eq 1 && $finished -eq 0 ]] || return 0
	[[ $rc -ne 0 ]] || rc=1
	printf '\n' >&2
	if [[ $force -eq 1 ]]; then
		warn "init failed after modifying files. Because of --force (the tree had uncommitted changes), nothing was rolled back."
		warn "Files changed now (yours and the script's):"
		git status --short >&2 || true
		warn "Inspect them with 'git diff'. 'git restore <file>' undoes the script's edits to a file but also discards your own uncommitted edits in it."
	elif rollback; then
		warn "init failed; rolled back: the working tree is back at HEAD, nothing changed. Fix the cause above and run the script again."
	else
		warn "init failed and the rollback also failed. Restore the tree manually:"
		warn "  git restore --source=HEAD --staged --worktree -- . && git clean -fd"
	fi
	exit "$rc"
}
trap on_exit EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

writes_started=1

# go.mod is excluded from the text replacement: go mod edit is the authoritative
# module rename and validates the new path.
info "go mod edit -module $new_module"
go mod edit -module "$new_module"

for i in "${!olds[@]}"; do
	old="${olds[$i]}"
	new="${news[$i]}"
	out="$(files_with "$old")"
	[[ -n "$out" ]] || continue
	mapfile -t files <<<"$out"
	replace_in_files "$old" "$new" "${files[@]}"
	info "replaced '$old' -> '$new' in ${#files[@]} file(s)"
done

if [[ -n "$copyright" ]]; then
	year="$(date +%Y)"
	RE="$COPYRIGHT_RE" HOLDER="$copyright" YEAR="$year" perl -pi -e \
		's/$ENV{RE}.*$/Copyright (c) $ENV{YEAR} $ENV{HOLDER}/' LICENSE
	# Fail (and roll back) unless the line now names the new holder.
	HOLDER="$copyright" YEAR="$year" perl -ne \
		'if ($_ eq "Copyright (c) $ENV{YEAR} $ENV{HOLDER}\n") { $found = 1 } END { exit($found ? 0 : 1) }' LICENSE ||
		die "the LICENSE copyright line was not updated to '$copyright'"
	info "set the LICENSE copyright holder to '$copyright'"
fi

info "go mod tidy"
go mod tidy

if [[ $have_swag -eq 1 ]]; then
	info "make swagger"
	make swagger
fi

info "gofmt"
mapfile -t go_files < <(git ls-files '*.go')
gofmt -w "${go_files[@]}"
unformatted="$(gofmt -l .)"
[[ -z "$unformatted" ]] || die "files are not gofmt-formatted: $unformatted"

info "go build ./..."
go build ./...

info "go test ./..."
go test ./...

rc=0
leftovers="$(git grep -nIF -e "$OLD_MODULE" -e "$OLD_APP" -e "$OLD_OWNER" -- . "${EXCLUDES[@]}")" || rc=$?
[[ $rc -le 1 ]] || die "git grep failed (exit $rc) while checking for leftovers"

finished=1
trap - EXIT INT TERM

if [[ -n "$leftovers" ]]; then
	warn "occurrences of the old names remain; review them:"
	printf '%s\n' "$leftovers" >&2
fi

cat <<EOF

Done. Next steps:
  1. Review the changes:        git diff
  2. Generate the signing keys: make keys
  3. Run the API:               make run
  4. Commit:                    git commit -am "chore: initialize $new_app from template"
  5. Enable the repository settings listed in README.md.
  6. Delete the bootstrap:      git rm scripts/init.sh scripts/init_test.sh
     (and, optionally, the test-init Make target and the init-script CI job)
EOF
