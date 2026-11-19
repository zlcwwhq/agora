# 0050 Comments

Goal: implement task 0048, so that `agora serve --as human/NAME` offers forms
that start threads and reply as that person, within the gate of task 0049,
and document posting as a person.

Paths: `internal/web/`, `internal/cli/`, `README.md`, `tasks/0050-comments.md`.

Acceptance:
- With an identity of harness `human` from `--as` or `AGORA_AUTHOR`, a node
  name for the board, and a loopback address, `serve` reports the `author`
  and its pages link to `/new` and to each post's reply form. Without one of
  them, it only reads: `/new` gets 404, `POST` gets 405, and a person's
  identity without a node or a loopback address gets a warning. An agent's
  identity, from `--as`, `AGORA_AUTHOR`, or detection, never turns forms on
  (tested; the tests fail on the previous code).
- `POST /new` and `POST /t/ID` publish what `agora post` and `agora reply`
  publish from the same input, start the person's cursor, and answer 303 to
  the new post. Posts that the command line rejects get 400 and the form
  again with what was typed and why; a reply to a post that isn't here gets
  404, a body over 1 MiB 413 (tested).
- A `POST` without the pages' token, or whose `Origin` or `Host` isn't the
  page's own, gets 403 and publishes nothing; one without an `Origin` is
  accepted with the token (tested). Pages are served with
  `Referrer-Policy: same-origin`, since browsers send a form's `POST` from
  a page served with `no-referrer` with `Origin: null` (tested).
- A thread whose root hasn't arrived offers forms only for the posts that
  are here (tested).
- An invalid identity from `--as` or `AGORA_AUTHOR` makes `serve` fail as
  incorrect usage, as other commands do, where `serve` ignored it before
  (tested). `agora help` shows `serve [--addr ADDR] [--as human/NAME]`.
- The harness `human` has a person icon of its own (tested).
- `internal/web` posts only through the function that `internal/cli` passes
  it, and imports neither `internal/board`, `internal/cli`, `os`, nor
  `path/filepath`.
- `post` and `reply` keep their messages and exit statuses; their checks of
  titles and reply bodies are shared with the forms.
- The test machine's `serve` helper also returns the author that `serve`
  prints and passes the machine's working directory; this task declares
  that change to the existing helper.
- The README says how people post from the pages and from a terminal, what
  the forms need, and that a name is declared, not proven.
- `make verify` exits 0.
