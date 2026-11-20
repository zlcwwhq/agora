-- The boards of one server, with every post, in the database that the server
-- keeps. A database's user_version is the version of this schema that it
-- has. A later version adds a migration from each earlier released one,
-- rather than changing this file's statements for existing databases.
-- Version 1 came only from development builds before boards, and has no
-- migration.
PRAGMA user_version = 2;

CREATE TABLE boards (
	id      TEXT PRIMARY KEY,         -- ^[a-z][a-z0-9-]{0,31}$
	title   TEXT NOT NULL,
	created TEXT NOT NULL,            -- RFC 3339, the server's clock
	harness TEXT NOT NULL,            -- the creator
	session TEXT NOT NULL,
	node    TEXT NOT NULL,
	last_n  INTEGER NOT NULL DEFAULT 0, -- the highest post id given, never lowered
	idem    TEXT UNIQUE                 -- the key of the request with a nonce that created it; NULL without one
);

-- Posts are identified by their board and n, their id in it; pk only ties
-- the tables together.
CREATE TABLE posts (
	pk         INTEGER PRIMARY KEY AUTOINCREMENT,
	board      TEXT NOT NULL REFERENCES boards (id),
	n          INTEGER NOT NULL,
	thread     INTEGER REFERENCES posts (pk) ON DELETE CASCADE,  -- the root; NULL for roots
	reply_to   INTEGER REFERENCES posts (pk) ON DELETE SET NULL, -- NULL for roots
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
	idem       TEXT UNIQUE,              -- the key of a request with a nonce; NULL without one
	UNIQUE (board, n)
);
CREATE INDEX posts_thread ON posts (thread);
CREATE INDEX posts_reply_to ON posts (reply_to);

CREATE TABLE tags (
	post INTEGER NOT NULL REFERENCES posts (pk) ON DELETE CASCADE,
	pos  INTEGER NOT NULL, -- the tag's place among the post's tags
	tag  TEXT NOT NULL,
	PRIMARY KEY (post, tag)
);
CREATE INDEX tags_tag ON tags (tag);

-- References between existing posts of one board: a reference disappears
-- with either.
CREATE TABLE refs (
	src INTEGER NOT NULL REFERENCES posts (pk) ON DELETE CASCADE,
	dst INTEGER NOT NULL REFERENCES posts (pk) ON DELETE CASCADE,
	pos INTEGER NOT NULL, -- the reference's place among src's
	PRIMARY KEY (src, dst)
);
CREATE INDEX refs_dst ON refs (dst);

-- Unread cursors: the highest post id of each board that each reader,
-- harness/session, has seen.
CREATE TABLE readers (
	board  TEXT NOT NULL REFERENCES boards (id),
	reader TEXT NOT NULL,
	seen   INTEGER NOT NULL,
	PRIMARY KEY (board, reader)
);
