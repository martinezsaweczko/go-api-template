#!/usr/bin/env bash
# Bootstrap a project created from go-api-template: rename the Go module, the
# application name and (optionally) the GitHub owner, then verify the result.
#
# Usage: scripts/init.sh [--dry-run] [--force] [--copyright "Name"] <module-path> <app-name> [github-owner]
# Example: scripts/init.sh github.com/acme/orders-api orders-api acme
#
# Run it once, right after creating the repository, then delete it.
set -euo pipefail

readonly OLD_MODULE="github.com/martinezsaweczko/go-api-template"
readonly OLD_OWNER="martinezsaweczko"
readonly OLD_APP="go-api-template"
readonly OLD_TITLE="Go API Template"

# Files that keep their original text: the license and this script.
readonly EXCLUDES=(':(exclude)LICENSE' ':(exclude)CHANGELOG.md' ':(exclude)scripts/init.sh')

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
[[ "$new_app" =~ ^[a-z][a-z0-9-]*$ ]] ||
	die "invalid app name '$new_app' (lowercase letters, digits and dashes, starting with a letter)"
if [[ -n "$new_owner" ]]; then
	[[ "$new_owner" =~ ^[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?$ ]] ||
		die "invalid GitHub owner '$new_owner'"
fi
[[ "$copyright" != *$'\n'* ]] || die "--copyright must be a single line"

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

if [[ $dry_run -eq 0 && $force -eq 0 && -n "$(git status --porcelain)" ]]; then
	die "the working tree has uncommitted changes; commit or stash them, or pass --force"
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

# files_with lists tracked text files that contain the literal string $1.
files_with() {
	git grep -lIF -e "$1" -- . "${EXCLUDES[@]}" || true
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

for i in "${!olds[@]}"; do
	old="${olds[$i]}"
	new="${news[$i]}"
	mapfile -t files < <(files_with "$old")
	[[ ${#files[@]} -gt 0 ]] || continue
	if [[ $dry_run -eq 1 ]]; then
		info "would replace '$old' -> '$new' in:"
		printf '      %s\n' "${files[@]}"
	else
		replace_in_files "$old" "$new" "${files[@]}"
		info "replaced '$old' -> '$new' in ${#files[@]} file(s)"
	fi
done

if [[ -n "$copyright" ]]; then
	if [[ $dry_run -eq 1 ]]; then
		info "would set the LICENSE copyright holder to '$copyright'"
	else
		HOLDER="$copyright" YEAR="$(date +%Y)" perl -pi -e \
			's/^Copyright \(c\) \d{4}.*$/Copyright (c) $ENV{YEAR} $ENV{HOLDER}/' LICENSE
		info "set the LICENSE copyright holder to '$copyright'"
	fi
fi

if [[ $dry_run -eq 1 ]]; then
	info "dry run: nothing was changed"
	exit 0
fi

# The blanket replacement already rewrote go.mod; go mod edit validates it.
go mod edit -module "$new_module"

info "go mod tidy"
go mod tidy

if command -v swag >/dev/null 2>&1; then
	info "make swagger"
	make swagger
else
	warn "swag not found: docs/ was updated textually; install swag and run 'make swagger' to regenerate it"
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

leftovers="$(git grep -nIF -e "$OLD_MODULE" -e "$OLD_APP" -e "$OLD_OWNER" -- . "${EXCLUDES[@]}" || true)"
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
  6. Delete this script:        git rm scripts/init.sh
EOF
