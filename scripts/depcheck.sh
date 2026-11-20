#!/bin/sh
# Package dependency allowlist. Dependencies point inward only:
# cmd -> internal/cli -> adapters (internal/config for the configuration
# file, internal/client for the server's HTTP API, internal/server for
# serving it, internal/store for the database) -> logic (internal/post).
# The server reaches the database only through the interface that it
# declares, which the command line fills with internal/store, so its HTTP
# code never touches SQL, and the store never touches HTTP.
set -eu

module=$(go list -m)

violations=$(go list -f '{{.ImportPath}} {{join .Imports " "}}' ./... | while read -r pkg imports; do
	rel=${pkg#"$module"/}
	for imp in $imports; do
		case $imp in
		net/http | net/http/*)
			case $rel in
			internal/server | internal/client) ;;
			*) echo "$rel imports $imp: only internal/server serves HTTP and only internal/client calls it" ;;
			esac ;;
		database/sql | database/sql/* | modernc.org/*)
			case $rel in
			internal/store) ;;
			*) echo "$rel imports $imp: only internal/store talks to the database" ;;
			esac ;;
		"$module"/cmd/*)
			echo "$rel imports $imp: nothing imports commands" ;;
		esac
		case $rel in
		internal/server)
			case $imp in
			"$module"/internal/store | "$module"/internal/cli | "$module"/internal/client | "$module"/internal/config)
				echo "$rel imports $imp: the server reaches the board only through the interface it declares, which the command line fills" ;;
			os | os/* | path/filepath)
				echo "$rel imports $imp: the server doesn't touch files; its pages and styles are embedded" ;;
			esac ;;
		internal/client)
			case $imp in
			"$module"/internal/store | "$module"/internal/server | "$module"/internal/cli | "$module"/internal/config)
				echo "$rel imports $imp: the client knows only the HTTP API and the shapes in internal/post" ;;
			esac ;;
		internal/store)
			case $imp in
			"$module"/internal/server | "$module"/internal/client | "$module"/internal/cli | "$module"/internal/config)
				echo "$rel imports $imp: the store knows only the schema and internal/post" ;;
			esac ;;
		internal/config)
			case $imp in
			"$module"/internal/server | "$module"/internal/client | "$module"/internal/cli | "$module"/internal/store)
				echo "$rel imports $imp: the configuration knows only its file and internal/post" ;;
			esac ;;
		internal/post)
			case $imp in
			os | os/* | io/fs | path/filepath | net | net/* | "$module"/*)
				echo "$rel imports $imp: logic packages are pure, without files, network, or internal dependencies" ;;
			esac ;;
		esac
	done
done)

if [ -n "$violations" ]; then
	echo "$violations" | sed 's/^/depcheck: /' >&2
	echo "depcheck: see scripts/depcheck.sh for the allowed directions" >&2
	exit 1
fi
