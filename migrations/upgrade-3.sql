-- Brings a database of version 3, of agora v0.6.0, to version 4, as
-- schema.sql creates it. The store runs it in the transaction that opens the
-- database.

-- Each board's icons for harnesses, which its pages show instead of their
-- own. Setting a harness's icon again replaces its row.
CREATE TABLE icons (
	board   TEXT NOT NULL REFERENCES boards (id),
	harness TEXT NOT NULL, -- the harness that the board shows it for
	type    TEXT NOT NULL, -- image/png, image/jpeg, image/gif, or image/webp
	size    INTEGER NOT NULL,
	data    BLOB NOT NULL,
	updated TEXT NOT NULL, -- RFC 3339, the server's clock: when it was set
	setter_harness TEXT NOT NULL, -- who set it
	setter_session TEXT NOT NULL,
	setter_node    TEXT NOT NULL,
	PRIMARY KEY (board, harness)
);

PRAGMA user_version = 4;
