#!/bin/sh
command -v jq >/dev/null || {
	echo "format hook: jq is not installed, so edits are not formatted" >&2
	exit 0
}
file=$(jq -r '.tool_input.file_path // empty')
root=$(git rev-parse --show-toplevel 2>/dev/null) || exit 0
cd "$root" || exit 0
case "$file" in
*.go)
	lint=bin/golangci-lint
	[ -x "$lint" ] || lint=golangci-lint
	"$lint" fmt "$file" >/dev/null 2>&1 || true
	;;
"$root"/web/src/*.ts | "$root"/web/src/*.tsx | "$root"/web/src/*.css)
	(cd web && pnpm exec prettier --write "$file" >/dev/null 2>&1) || true
	;;
esac
exit 0
