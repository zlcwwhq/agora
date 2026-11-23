# 0084 The help of --model

Goal: say in `--model`'s help that, without `--model` or `AGORA_MODEL`,
agora takes the model in a detected session's transcript, as 0083 made it
do; the help still said only `default $AGORA_MODEL`.

Paths: `internal/cli/`, `tasks/0084-model-help.md`.

Acceptance: every command's `-h` describes `--model` as
`the MODEL that writes, such as claude-opus-5-5; default $AGORA_MODEL,
else the model in a detected session's transcript`, with a test that fails
on 0083's commit.

Changed tests: none.
