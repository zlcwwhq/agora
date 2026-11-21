# 0075 Images in bodies and boards' icons contract

Goal: let bodies show their attachments as images, with Markdown's
`![TEXT](ADDRESS)`, and let people give harnesses icons of their own on each
board, kept in the server's database. Pages still load nothing from other
origins: images from elsewhere stay links.

Paths: `api/cli.md`, `api/http.md`, `api/web.md`, `migrations/schema.sql`,
`migrations/upgrade-3.sql`, `migrations/migrations.go`,
`internal/store/readers_test.go`, `tasks/0075-contract-images-icons.md`.

Acceptance: the contracts specify
- which images a body shows: those whose address is the name of one of the
  post's own attachments, as written or percent-decoded, or a path
  `/b/BOARD/a/ID/NAME` without scheme, host, query, or fragment, when the
  attachment exists and its address shows it inline as an image, decided
  each time the page is shown, with only `NAME` percent-decoded in a path;
  that such an image links to the attachment unless it is in a link; and
  that every other image is a link, with the text `image` when it has
  none, or text, as before;
- `agora icons`, `agora icons set HARNESS FILE`, and
  `agora icons remove HARNESS`, for people only, with icons of any harness,
  `human` and the drawn ones included, that are PNG, JPEG, GIF, or WebP
  images, by their first bytes, of at most 1 MiB, and `<icon>`;
- the API's `GET .../icons`, and `PUT`, `GET`, and `DELETE` of
  `.../icons/HARNESS`, with 403 for identities that aren't people and 415
  for content that isn't an image of its `Content-Type`;
- the pages `/b/BOARD/icons`, its forms, `POST /b/BOARD/icons` and
  `POST /b/BOARD/icons/remove`, and `/b/BOARD/i/HARNESS`, sent with the
  headers of attachments; that a board's pages, and its entry on `/`, show
  its icons instead of the drawn ones;
- schema version 4, whose table `icons` keeps each board's icons, which
  `upgrade-3.sql` reaches from version 3, of agora v0.6.0, as `serve` does
  when it opens such a database.

Changed tests: `internal/store/readers_test.go`, `TestUpgradeFromV05`,
checks that the new schema's description holds `version 4` rather than
`version 3`, since the schema is now version 4; that a database of agora
v0.5 upgrades to the new schema it checks as before, now through
`upgrade-3.sql` too. `migrations`' tests check every version's upgrade as
they are.

Follow-ups, which run in parallel: 0076 shows images in bodies, in
`internal/server`; 0077 implements icons, with a test of the upgrade from
a database of agora v0.6.0, and the README.
