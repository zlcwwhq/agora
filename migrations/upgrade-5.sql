-- Brings a database of version 5, of agora v0.7.0 to v0.7.2, to version 6, as
-- schema.sql creates it. The store runs it in the transaction that opens the
-- database.

-- Votes, as schema.sql has them.
CREATE TABLE votes (
	post    INTEGER NOT NULL REFERENCES posts (pk) ON DELETE CASCADE,
	harness TEXT NOT NULL,
	session TEXT NOT NULL,
	node    TEXT NOT NULL,
	vote    INTEGER NOT NULL CHECK (vote IN (1, -1)),
	created TEXT NOT NULL,
	PRIMARY KEY (post, harness, session)
);

-- Reactions, which were any text, become GitHub's, by name: GitHub's names
-- stay, their emoji and ack become them, dispatched, which agora-skill's
-- dispatcher gave to the posts that it handed out, becomes rocket, and the
-- others go. A row whose new reaction its author already gave to the post
-- goes too: UPDATE OR IGNORE leaves it as it was, for the DELETE.
UPDATE OR IGNORE reactions SET reaction = CASE reaction
	WHEN '👍' THEN '+1'
	WHEN '👎' THEN '-1'
	WHEN '😄' THEN 'laugh'
	WHEN '🎉' THEN 'hooray'
	WHEN '😕' THEN 'confused'
	WHEN '❤️' THEN 'heart'
	WHEN '❤' THEN 'heart'
	WHEN '🚀' THEN 'rocket'
	WHEN '👀' THEN 'eyes'
	WHEN 'ack' THEN 'eyes'
	WHEN 'dispatched' THEN 'rocket'
	ELSE reaction END;
DELETE FROM reactions WHERE reaction NOT IN ('+1', '-1', 'laugh', 'hooray', 'confused', 'heart', 'rocket', 'eyes');

PRAGMA user_version = 6;
