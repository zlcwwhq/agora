# 0076 Images in bodies

Goal: implement the part of 0075 that concerns bodies: a thread's page
shows an image, `![TEXT](ADDRESS)`, as the attachment that `ADDRESS` names,
the post's own by its name or any post's by `/b/BOARD/a/ID/NAME`, when the
pages show that attachment inline as an image. Every other image stays a
link, or text, as before, so pages still load nothing from other origins.
0077 implements the rest of 0075, icons, at the same time.

Paths: `internal/server/`, `tasks/0076-body-images.md`.

Acceptance:
- An image whose address is the name of one of the post's attachments, as
  written, in angle brackets, percent-escaped, or with characters outside
  ASCII, is rendered as `<img>` from `/b/BOARD/a/ID/NAME`, with its text as
  `alt`, inside a link to that address.
- An image whose address is exactly `/b/BOARD/a/ID/NAME`, without scheme,
  host, query, or fragment, and with percent-escapes only in `NAME`, is
  rendered the same way when that attachment exists, whether its post is
  in the thread shown, in another thread, or on another board; the server
  finds the attachments of posts outside the thread through `Store.Get`,
  which reads no attachment's content.
- An image that names an attachment that the pages don't show as an image
  is a link to the attachment's address; one that names no existing
  attachment, `//host/x.png`, or an address on another origin is a link to
  its address, as before, with the text `image` when it has none; one
  inside a link is an `<img>` inside that link, with no link of its own,
  when it shows an attachment, and its text otherwise.
- Whether an image is shown follows the post's attachments each time the
  page is shown: attaching the file shows it, detaching it turns it into a
  link.
- Images in bodies are no wider than the post and no taller than a
  screenful, by `style.css`, since pages allow no style attributes.
- Every new test fails on 0075's commit; tests are self-contained, as
  before.

Changed tests: none.
