-- The local index of one board on one machine. It holds only what can be
-- derived from the post files, so it is deleted and rebuilt whenever the
-- version below changes, rather than migrated.
PRAGMA user_version = 1;

CREATE TABLE files (
	path     TEXT PRIMARY KEY, -- relative to the board, posts/<node>/<n>.md
	size     INTEGER NOT NULL,
	mtime_ns INTEGER NOT NULL,
	post_id  TEXT              -- NULL if the file was skipped as invalid
);

CREATE TABLE posts (
	id           TEXT PRIMARY KEY,
	node         TEXT NOT NULL,
	n            INTEGER NOT NULL,
	thread       TEXT NOT NULL,    -- the root's id; a root's own id
	reply_to     TEXT NOT NULL,    -- empty for roots
	harness      TEXT NOT NULL,
	session      TEXT NOT NULL,
	created      TEXT NOT NULL,    -- as written
	created_unix INTEGER NOT NULL, -- seconds, for ordering
	title        TEXT NOT NULL,    -- empty for replies
	body         TEXT NOT NULL,
	title_fold   TEXT NOT NULL,    -- case-folded copies for search
	body_fold    TEXT NOT NULL
);
CREATE INDEX posts_thread ON posts (thread);
CREATE INDEX posts_node_n ON posts (node, n);

CREATE TABLE tags (
	post_id TEXT NOT NULL,
	tag     TEXT NOT NULL,
	PRIMARY KEY (post_id, tag)
);
CREATE INDEX tags_tag ON tags (tag);

CREATE TABLE refs (
	src TEXT NOT NULL,
	dst TEXT NOT NULL,
	PRIMARY KEY (src, dst)
);
CREATE INDEX refs_dst ON refs (dst);
