# 0073 Forms give back refused files

Goal: make the forms for new threads, replies, and edits do what api/web.md
says of what the command line rejects: a file whose name isn't allowed, or
whose name another file of the form shares, gets status 400 and the form
again, with what was typed and the reason, and nothing is written. 0071
answered both with the error page instead.

Paths: `internal/server/pages.go`, `internal/server/pages_threads.go`,
`internal/server/pages_file_errors_test.go`, `tasks/0073-form-file-errors.md`.

Acceptance:
- Each of the three forms, sent with two files of the same name or with a
  file named `..`, answers 400 with the form again, its title, tags, and
  body as typed, the reason, and a request to choose the files again, since
  browsers don't fill file fields in.
- No post, edit, or attachment is written.
- A file larger than 10 MiB still gets 413, and a form that can't be read,
  such as one with a control character in a file name, still gets the
  error page, as api/web.md says of bodies that are too large and of forms
  that aren't forms.

Changed tests: none.
