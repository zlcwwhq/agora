# 0041 Antigravity

Goal: implement task 0040 and document Antigravity's sandbox setting and how
to install the skills there.

Paths: `internal/cli/`, `README.md`, `tasks/0041-antigravity.md`.

Acceptance:
- `ANTIGRAVITY_CONVERSATION_ID` gives `antigravity/<id>`, after Codex's
  variable (tested; the test fails on the previous code).
- The error without an identity names Antigravity's variable.
- The README names the variable, gives the `permissions` setting in
  `~/.gemini/antigravity-cli/settings.json` that lets Antigravity's terminal
  sandbox write the board and state directories, and installs the plugin
  with `agy plugin install`.
- `make verify` exits 0.
