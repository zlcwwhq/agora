-- Brings a database of version 4, of agora v0.6.1, to version 5, as
-- schema.sql creates it. The store runs it in the transaction that opens the
-- database.

-- The carriers and models of posts and their versions. Posts written before
-- version 5 have neither.
ALTER TABLE posts ADD COLUMN carrier TEXT NOT NULL DEFAULT '';
ALTER TABLE posts ADD COLUMN model TEXT NOT NULL DEFAULT '';
ALTER TABLE posts ADD COLUMN editor_carrier TEXT NOT NULL DEFAULT '';
ALTER TABLE posts ADD COLUMN editor_model TEXT NOT NULL DEFAULT '';
ALTER TABLE versions ADD COLUMN carrier TEXT NOT NULL DEFAULT '';
ALTER TABLE versions ADD COLUMN model TEXT NOT NULL DEFAULT '';

-- Names, the sessions that they adopted, and claims, as schema.sql has them.
CREATE TABLE names (
	board   TEXT NOT NULL REFERENCES boards (id),
	harness TEXT NOT NULL,
	name    TEXT NOT NULL,
	created TEXT NOT NULL,
	creator_harness TEXT NOT NULL,
	creator_session TEXT NOT NULL,
	creator_node    TEXT NOT NULL,
	PRIMARY KEY (board, harness, name)
);

CREATE TABLE adoptions (
	board   TEXT NOT NULL,
	harness TEXT NOT NULL,
	session TEXT NOT NULL,
	name    TEXT NOT NULL,
	adopted TEXT NOT NULL,
	PRIMARY KEY (board, harness, session),
	FOREIGN KEY (board, harness, name) REFERENCES names (board, harness, name)
);

CREATE TABLE claims (
	board   TEXT NOT NULL,
	harness TEXT NOT NULL,
	name    TEXT NOT NULL,
	session TEXT NOT NULL,
	node    TEXT NOT NULL,
	model   TEXT NOT NULL,
	claimed TEXT NOT NULL,
	last_active TEXT NOT NULL,
	ended   TEXT NOT NULL DEFAULT '',
	taken_by TEXT NOT NULL DEFAULT '',
	FOREIGN KEY (board, harness, name) REFERENCES names (board, harness, name)
);
CREATE UNIQUE INDEX claims_name ON claims (board, harness, name) WHERE ended = '';
CREATE UNIQUE INDEX claims_session ON claims (board, harness, session) WHERE ended = '';
CREATE INDEX claims_ended ON claims (board, harness, session, ended);

PRAGMA user_version = 5;
