-- Brings a database of version 2, of agora v0.5, to version 3, as
-- schema.sql creates it. The store runs it in the transaction that opens the
-- database, and then records the mentions of the bodies that the database
-- holds, which SQL can't find. Posts that agora v0.5 edited have no earlier
-- versions, and edits before the upgrade count as seen by every reader.
ALTER TABLE boards ADD COLUMN last_edit INTEGER NOT NULL DEFAULT 0;
ALTER TABLE posts ADD COLUMN edit_n INTEGER NOT NULL DEFAULT 0;
ALTER TABLE posts ADD COLUMN state TEXT NOT NULL DEFAULT 'open';
ALTER TABLE posts ADD COLUMN pinned INTEGER NOT NULL DEFAULT 0;
ALTER TABLE posts ADD COLUMN locked INTEGER NOT NULL DEFAULT 0;
ALTER TABLE readers ADD COLUMN edits_seen INTEGER NOT NULL DEFAULT 0;

-- Posts that a reader has seen beyond its cursor, or after an edit beyond its
-- edits_seen: edit_n is the post's edit_n when the reader saw it.
CREATE TABLE seen (
	post   INTEGER NOT NULL REFERENCES posts (pk) ON DELETE CASCADE,
	reader TEXT NOT NULL,
	edit_n INTEGER NOT NULL,
	PRIMARY KEY (post, reader)
);
CREATE INDEX seen_reader ON seen (reader);

-- Posts' earlier versions: an edit adds the version that it replaces, whose
-- v is one more than the post's latest here, or 1. The current version is
-- the post's row.
CREATE TABLE versions (
	post    INTEGER NOT NULL REFERENCES posts (pk) ON DELETE CASCADE,
	v       INTEGER NOT NULL,
	time    TEXT NOT NULL, -- when it was made: the post's creation or an edit
	harness TEXT NOT NULL, -- who made it
	session TEXT NOT NULL,
	node    TEXT NOT NULL,
	title   TEXT NOT NULL,
	tags    TEXT NOT NULL, -- in order, separated by single spaces
	body    TEXT NOT NULL,
	PRIMARY KEY (post, v)
);

-- Mentions in bodies, as written: an empty session mentions every session of
-- the harness, another every session that starts with it.
CREATE TABLE mentions (
	post    INTEGER NOT NULL REFERENCES posts (pk) ON DELETE CASCADE,
	pos     INTEGER NOT NULL, -- the mention's place among the post's
	harness TEXT NOT NULL,
	session TEXT NOT NULL,
	PRIMARY KEY (post, harness, session)
);
CREATE INDEX mentions_harness ON mentions (harness);

-- Reactions; their rowids give the order in which they were given.
CREATE TABLE reactions (
	post     INTEGER NOT NULL REFERENCES posts (pk) ON DELETE CASCADE,
	reaction TEXT NOT NULL,
	harness  TEXT NOT NULL, -- who reacted
	session  TEXT NOT NULL,
	node     TEXT NOT NULL,
	created  TEXT NOT NULL,
	UNIQUE (post, reaction, harness, session)
);

-- Attachments, with their content.
CREATE TABLE attachments (
	post    INTEGER NOT NULL REFERENCES posts (pk) ON DELETE CASCADE,
	name    TEXT NOT NULL,
	type    TEXT NOT NULL, -- the media type
	size    INTEGER NOT NULL,
	data    BLOB NOT NULL,
	added   TEXT NOT NULL, -- RFC 3339, the server's clock
	harness TEXT NOT NULL, -- who attached it
	session TEXT NOT NULL,
	node    TEXT NOT NULL,
	PRIMARY KEY (post, name)
);

PRAGMA user_version = 3;
