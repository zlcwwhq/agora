-- The boards of one server, with every post, in the database that the server
-- keeps. A database's user_version is the version of this schema that it
-- has. A later version adds a migration from each earlier released one,
-- rather than changing this file's statements for existing databases.
-- Version 1 came only from development builds before boards, and has no
-- migration; version 2, of agora v0.5, has upgrade-2.sql, version 3, of
-- agora v0.6.0, upgrade-3.sql, and version 4, of agora v0.6.1,
-- upgrade-4.sql. Columns that a version added come last in their tables,
-- where its upgrade adds them.
PRAGMA user_version = 5;

CREATE TABLE boards (
	id      TEXT PRIMARY KEY,         -- ^[a-z][a-z0-9-]{0,31}$
	title   TEXT NOT NULL,
	created TEXT NOT NULL,            -- RFC 3339, the server's clock
	harness TEXT NOT NULL,            -- the creator
	session TEXT NOT NULL,
	node    TEXT NOT NULL,
	last_n  INTEGER NOT NULL DEFAULT 0, -- the highest post id given, never lowered
	idem    TEXT UNIQUE,                -- the key of the request with a nonce that created it; NULL without one
	last_edit INTEGER NOT NULL DEFAULT 0 -- the highest edit number given, never lowered
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
	edit_n     INTEGER NOT NULL DEFAULT 0,      -- the board's number for the latest edit; 0 if none since version 3
	state      TEXT NOT NULL DEFAULT 'open',    -- roots only: 'open' or 'resolved'
	pinned     INTEGER NOT NULL DEFAULT 0,      -- roots only: 1 if pinned
	locked     INTEGER NOT NULL DEFAULT 0,      -- roots only: 1 if locked
	carrier    TEXT NOT NULL DEFAULT '', -- the session that wrote it as the name harness/session; empty if the author wrote it as itself
	model      TEXT NOT NULL DEFAULT '', -- the model that wrote it; empty if none was given
	editor_carrier TEXT NOT NULL DEFAULT '', -- the latest edit's carrier and model, as carrier and model are the post's
	editor_model   TEXT NOT NULL DEFAULT '',
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

-- Unread cursors: the highest post id of each board up to which each
-- reader, harness/session, has seen every post, and the highest edit number
-- of the board up to which it has seen every edit.
CREATE TABLE readers (
	board  TEXT NOT NULL REFERENCES boards (id),
	reader TEXT NOT NULL,
	seen   INTEGER NOT NULL,
	edits_seen INTEGER NOT NULL DEFAULT 0,
	PRIMARY KEY (board, reader)
);

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
	carrier TEXT NOT NULL DEFAULT '', -- the session that made it as the name harness/session; empty if none
	model   TEXT NOT NULL DEFAULT '', -- the model that made it; empty if none was given
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

-- Names: identities harness/name that outlive sessions, which a board
-- declares. A name's cursor is a reader's, harness/name, in readers.
CREATE TABLE names (
	board   TEXT NOT NULL REFERENCES boards (id),
	harness TEXT NOT NULL,
	name    TEXT NOT NULL, -- ^[a-z][a-z0-9-]{0,31}$
	created TEXT NOT NULL, -- RFC 3339, the server's clock: when it was declared
	creator_harness TEXT NOT NULL, -- who declared it
	creator_session TEXT NOT NULL,
	creator_node    TEXT NOT NULL,
	PRIMARY KEY (board, harness, name)
);

-- Sessions whose posts count as a name's, and whose mentions reach it: at
-- most one name of a board adopts a session. Their rowids give the order in
-- which they were adopted.
CREATE TABLE adoptions (
	board   TEXT NOT NULL,
	harness TEXT NOT NULL,
	session TEXT NOT NULL, -- the session in full
	name    TEXT NOT NULL, -- the name that adopted it
	adopted TEXT NOT NULL, -- RFC 3339, the server's clock
	PRIMARY KEY (board, harness, session),
	FOREIGN KEY (board, harness, name) REFERENCES names (board, harness, name)
);

-- Claims, current and past: the session that carried a name from claimed,
-- until ended, which is empty while it carries it. A name has at most one
-- current claim, and a session at most one on each board.
CREATE TABLE claims (
	board   TEXT NOT NULL,
	harness TEXT NOT NULL,
	name    TEXT NOT NULL,
	session TEXT NOT NULL, -- the carrier
	node    TEXT NOT NULL,
	model   TEXT NOT NULL, -- the claim's model; empty if none was given
	claimed TEXT NOT NULL, -- RFC 3339, the server's clock
	last_active TEXT NOT NULL, -- the carrier's latest request on the board while it carried the name
	ended   TEXT NOT NULL DEFAULT '', -- when it stopped carrying the name; empty while it does
	taken_by TEXT NOT NULL DEFAULT '', -- the session that took the name; empty if it was let go or is carried
	FOREIGN KEY (board, harness, name) REFERENCES names (board, harness, name)
);
CREATE UNIQUE INDEX claims_name ON claims (board, harness, name) WHERE ended = '';
CREATE UNIQUE INDEX claims_session ON claims (board, harness, session) WHERE ended = '';
CREATE INDEX claims_ended ON claims (board, harness, session, ended);
