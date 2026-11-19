#!/bin/sh
# Package dependency allowlist. Dependencies point inward only:
# cmd -> adapters (internal/board for files, internal/index for SQLite,
# internal/web for HTTP) -> logic (internal/post, internal/cursor).
# The web interface gets the board's posts from the index, and its forms post
# only through the function that the command line passes it: it doesn't import
# internal/board, which writes the board, nor touch files itself.
set -eu

module=$(go list -m)

violations=$(go list -f '{{.ImportPath}} {{join .Imports " "}}' ./... | while read -r pkg imports; do
	rel=${pkg#"$module"/}
	for imp in $imports; do
		case $imp in
		net/http | net/http/*)
			case $rel in
			internal/web) ;;
			*) echo "$rel imports $imp: only internal/web serves HTTP" ;;
			esac ;;
		database/sql | database/sql/* | modernc.org/*)
			case $rel in
			internal/index) ;;
			*) echo "$rel imports $imp: only internal/index talks to the database" ;;
			esac ;;
		"$module"/cmd/*)
			echo "$rel imports $imp: nothing imports commands" ;;
		esac
		case $rel in
		internal/web)
			case $imp in
			"$module"/internal/board | "$module"/internal/cli)
				echo "$rel imports $imp: the web interface reads from the index that the command line brings up to date, and posts only through the function that the command line passes it" ;;
			os | os/* | path/filepath)
				echo "$rel imports $imp: the web interface doesn't touch files; it posts through the function that the command line passes it" ;;
			esac ;;
		internal/post | internal/cursor)
			case $imp in
			os | os/* | io/fs | path/filepath | "$module"/*)
				echo "$rel imports $imp: logic packages are pure, without filesystem access or internal dependencies" ;;
			esac ;;
		esac
	done
done)

if [ -n "$violations" ]; then
	echo "$violations" | sed 's/^/depcheck: /' >&2
	echo "depcheck: see scripts/depcheck.sh for the allowed directions" >&2
	exit 1
fi
