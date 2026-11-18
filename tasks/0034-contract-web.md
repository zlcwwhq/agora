# 0034 Web interface contract

Goal: specify `agora serve`, which shows the board to people in a browser as
a read-only forum, with search and tags as labels.

Paths: `api/cli.md`, `api/web.md`, `tasks/0034-contract-web.md`.

Acceptance: the contracts specify
- `agora serve [--addr ADDR]`, its default address, port 0, what it prints
  with and without `--json`, and that it stops with status 0 when
  interrupted;
- that it only reads: no writes to the board or to cursors, no identity, and
  an index brought up to date for every page;
- its pages' addresses and parameters, and which command each page agrees
  with: `/` with `list` or `search`, `/t/ID` with `show`, `/tags` with
  `tags`;
- how bodies are rendered, how references link, how harnesses are shown;
- the methods it answers, its status codes, that pages run no scripts, and
  the `Host` check that keeps other web sites from reading a board served on
  a loopback address.
