#!/bin/sh
# Tests take temporary directories from internal/testdir, not from testing's
# TempDir: TempDir's cleanup gives up on Windows while another program still
# holds a file that the test deleted, and fails the test with "The directory
# is not empty" (see tasks/0072-windows-tempdirs.md). os.TempDir, which only
# names the system's directory, is fine.
set -eu

hits=$(find . -mindepth 1 -name '.*' -prune -o -name '*.go' -type f -exec awk '{
	line = $0
	gsub(/(^|[^A-Za-z0-9_])os\.TempDir\(\)/, "", line)
	if (line ~ /[A-Za-z0-9_)\]]\.TempDir\(\)/) print FILENAME ":" FNR ": " $0
}' {} + | sed 's|^\./||')

if [ -n "$hits" ]; then
	echo "$hits" | sed 's/^/tempdircheck: /' >&2
	echo "tempdircheck: these call TempDir, whose cleanup fails on Windows while another program holds a deleted file;" >&2
	echo "tempdircheck: use testdir.New(t) from github.com/zlcwwhq/agora/internal/testdir instead, which waits for the files to go" >&2
	exit 1
fi
