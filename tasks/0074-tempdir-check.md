# 0074 Gate: no TempDir in tests

Goal: keep new tests from bringing back the Windows failures that 0072
fixed. A test that calls testing's `TempDir` fails on Windows whenever
another program still holds a file that the test deleted; `testdir.New`
waits for it. The gate says so, with the fix, instead of a reminder.

Paths: `Makefile`, `scripts/tempdircheck.sh`, `tasks/0074-tempdir-check.md`.

Acceptance:
- `make verify` runs `make tempdirs`, which fails for every `.go` file
  outside directories whose names start with `.` that calls `TempDir()` on
  anything but the package `os`, naming each file and line, and says to use
  `testdir.New(t)` from `internal/testdir`.
- `os.TempDir()`, which only names the system's directory, passes, also on
  a line that calls the other too, which fails.
- It works with gawk and with mawk, Ubuntu's `awk`, which CI runs it with.
- It passes on `main` after 0072, and fails on `1162f76`, before 0072, with
  its 21 calls.

Changed tests: none; this change holds no business code or tests, as
AGENTS.md's fifth rule asks of changes to the gate.
