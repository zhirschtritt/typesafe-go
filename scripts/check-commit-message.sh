#!/bin/sh

set -eu

message_file=${1:?commit message file is required}
subject=$(sed -n '1p' "$message_file")

case "$subject" in
  "Merge "*|"Revert "*|"fixup! "*|"squash! "*)
    exit 0
    ;;
esac

if printf '%s\n' "$subject" | grep -Eq '^(build|chore|ci|docs|feat|fix|perf|refactor|revert|style|test)(\([a-z0-9][a-z0-9._/-]*\))?!?: .+'; then
  exit 0
fi

cat >&2 <<'EOF'
Invalid commit message. Use Conventional Commits:

  <type>[optional scope][!]: <description>

Release types:
  fix:   patch release
  feat:  minor release
  !:     major release, for example feat!: remove legacy API

Other accepted types:
  build, chore, ci, docs, perf, refactor, revert, style, test
EOF
exit 1
