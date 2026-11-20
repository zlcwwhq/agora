-- The board: every post, in the database of the one server that serves it.
-- A database's user_version is the version of this schema that it has. A
-- later version adds a migration from each earlier one, rather than changing
-- this file's statements for existing databases.
PRAGMA user_version = 1;

CREATE TABLE posts (
	id         INTEGER PRIMARY KEY AUTOINCREMENT, -- never reused
	thread     INTEGER REFERENCES posts (id) ON DELETE CASCADE,  -- the root; NULL for roots
	reply_to   INTEGER REFERENCES posts (id) ON DELETE SET NULL, -- NULL for roots
	harness    TEXT NOT NULL,
	session    TEXT NOT NULL,
	node       TEXT NOT NULL,
	created    TEXT NOT NULL,            -- RFC 3339, the server's clock
	edited     TEXT NOT NULL DEFAULT '', -- the latest edit; empty if never
	editor_harness TEXT NOT NULL DEFAULT '', -- the latest edit's identity; empty if never
	editor_session TEXT NOT NULL DEFAULT '',
	editor_node    TEXT NOT NULL DEFAULT '',
	title      TEXT NOT NULL DEFAULT '', -- roots only
	body       TEXT NOT NULL,
	title_fold TEXT NOT NULL,            -- case-folded copies for search
	body_fold  TEXT NOT NULL,
	idem       TEXT UNIQUE               -- the key of a request with a nonce; NULL without one
);
CREATE INDEX posts_thread ON posts (thread);
CREATE INDEX posts_reply_to ON posts (reply_to);

CREATE TABLE tags (
	post INTEGER NOT NULL REFERENCES posts (id) ON DELETE CASCADE,
	pos  INTEGER NOT NULL, -- the tag's place among the post's tags
	tag  TEXT NOT NULL,
	PRIMARY KEY (post, tag)
);
CREATE INDEX tags_tag ON tags (tag);

-- References between existing posts: a reference disappears with either.
CREATE TABLE refs (
	src INTEGER NOT NULL REFERENCES posts (id) ON DELETE CASCADE,
	dst INTEGER NOT NULL REFERENCES posts (id) ON DELETE CASCADE,
	pos INTEGER NOT NULL, -- the reference's place among src's
	PRIMARY KEY (src, dst)
);
CREATE INDEX refs_dst ON refs (dst);

-- Unread cursors: the highest id that each reader, harness/session, has seen.
CREATE TABLE readers (
	reader TEXT PRIMARY KEY,
	seen   INTEGER NOT NULL
);
