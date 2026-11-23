package store

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"strings"

	"github.com/zlcwwhq/agora/internal/post"
)

// Names, the sessions that they adopt, and claims, as api/cli.md's Names
// says. A request's identity resolves, on a board, to the actor that it acts
// as: the name that its session carries, a name that it names, or itself.

// claim is a row of claims.
type claim struct {
	rowid                      int64
	name, session, node, model string
	claimed, lastActive        string
}

// currentClaim returns the claim that a session holds on a board, if any.
func currentClaim(ctx context.Context, q querier, board, harness, session string) (claim, bool, error) {
	c := claim{session: session}
	err := q.QueryRowContext(ctx, `SELECT rowid, name, node, model, claimed, last_active FROM claims
		WHERE board = ? AND harness = ? AND session = ? AND ended = ''`, board, harness, session).
		Scan(&c.rowid, &c.name, &c.node, &c.model, &c.claimed, &c.lastActive)
	if errors.Is(err, sql.ErrNoRows) {
		return c, false, nil
	}
	return c, err == nil, err
}

// holder returns the claim that holds a name, if a session carries it.
func holder(ctx context.Context, q querier, board, harness, name string) (claim, bool, error) {
	c := claim{name: name}
	err := q.QueryRowContext(ctx, `SELECT rowid, session, node, model, claimed, last_active FROM claims
		WHERE board = ? AND harness = ? AND name = ? AND ended = ''`, board, harness, name).
		Scan(&c.rowid, &c.session, &c.node, &c.model, &c.claimed, &c.lastActive)
	if errors.Is(err, sql.ErrNoRows) {
		return c, false, nil
	}
	return c, err == nil, err
}

// isName reports whether the board declared harness/name.
func isName(ctx context.Context, q querier, board, harness, name string) (bool, error) {
	var one int
	err := q.QueryRowContext(ctx, "SELECT 1 FROM names WHERE board = ? AND harness = ? AND name = ?", board, harness, name).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// adoptedBy returns the sessions that a name adopted, in order.
func adoptedBy(ctx context.Context, q querier, board, harness, name string) ([]string, error) {
	return column[string](ctx, q, "SELECT session FROM adoptions WHERE board = ? AND harness = ? AND name = ? ORDER BY rowid", board, harness, name)
}

// adopter returns the name that adopted a session, or empty.
func adopter(ctx context.Context, q querier, board, harness, session string) (string, error) {
	var name string
	err := q.QueryRowContext(ctx, "SELECT name FROM adoptions WHERE board = ? AND harness = ? AND session = ?", board, harness, session).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return name, err
}

// actor resolves who, the identity of a request on a board, as api/cli.md's
// Names says, with what post.ViaOf(ctx) adds.
func actor(ctx context.Context, q querier, board string, who post.Identity) (post.Actor, error) {
	via := post.ViaOf(ctx)
	a := post.Actor{Identity: who, Model: via.Model}
	if who.IsZero() || who.IsPerson() {
		return a, nil
	}
	c, carries, err := currentClaim(ctx, q, board, who.Harness, who.Session)
	if err != nil {
		return a, err
	}
	if carries {
		a.Identity = post.Identity{Harness: who.Harness, Session: c.name, Node: who.Node}
		a.Name, a.Carrier = true, who.Session
		if a.Model == "" {
			a.Model = c.model
		}
		a.Adopted, err = adoptedBy(ctx, q, board, who.Harness, c.name)
		return a, err
	}
	named, err := isName(ctx, q, board, who.Harness, who.Session)
	if err != nil {
		return a, err
	}
	if named {
		a.Name = true
		if a.Adopted, err = adoptedBy(ctx, q, board, who.Harness, who.Session); err != nil {
			return a, err
		}
		h, held, err := holder(ctx, q, board, who.Harness, who.Session)
		switch {
		case err != nil:
			return a, err
		case held && h.session == via.Carrier:
			a.Carrier = h.session
			if a.Model == "" {
				a.Model = h.model
			}
		case held:
			a.Holder = post.Author(who.Harness, h.session, h.node)
		default:
			// No session carries the name: the one that writes as it is
			// still the post's carrier.
			a.Carrier = via.Carrier
		}
		return a, nil
	}
	if a.AdoptedBy, err = adopter(ctx, q, board, who.Harness, who.Session); err != nil {
		return a, err
	}
	a.Displaced, err = displaced(ctx, q, board, who.Harness, who.Session)
	return a, err
}

// displaced returns the name that another session took from a session that
// carries none, if it hasn't claimed one since, as harness/name.
func displaced(ctx context.Context, q querier, board, harness, session string) (string, error) {
	var name, takenBy string
	err := q.QueryRowContext(ctx, `SELECT name, taken_by FROM claims WHERE board = ? AND harness = ? AND session = ?
		ORDER BY rowid DESC LIMIT 1`, board, harness, session).Scan(&name, &takenBy)
	if errors.Is(err, sql.ErrNoRows) || takenBy == "" {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return harness + "/" + name, nil
}

// Resolve returns the actor that who acts as on board, renewing the last
// activity of the name that its session carries. A board that doesn't exist
// resolves who as itself.
func (s *Store) Resolve(ctx context.Context, board string, who post.Identity) (post.Actor, error) {
	var a post.Actor
	err := s.tx(ctx, func(tx *sql.Tx) error {
		if err := checkBoard(ctx, tx, board); err != nil {
			a = post.Actor{Identity: who, Model: post.ViaOf(ctx).Model}
			return nil
		}
		var err error
		if a, err = actor(ctx, tx, board, who); err != nil || a.Carrier == "" {
			return err
		}
		_, err = tx.ExecContext(ctx, "UPDATE claims SET last_active = ? WHERE board = ? AND harness = ? AND name = ? AND ended = ''",
			s.stamp(), board, a.Harness, a.Session)
		return err
	})
	return a, err
}

// sessionsOn returns the sessions of a harness that wrote or read on a board,
// as themselves or as the names that they carried, but names, sorted.
func sessionsOn(ctx context.Context, q querier, board, harness string) ([]string, error) {
	out, err := column[string](ctx, q, `SELECT session FROM posts WHERE board = ? AND harness = ?
		UNION SELECT substr(reader, length(?) + 2) FROM readers WHERE board = ? AND substr(reader, 1, length(?) + 1) = ? || '/'
		UNION SELECT session FROM claims WHERE board = ? AND harness = ?
		UNION SELECT session FROM adoptions WHERE board = ? AND harness = ?
		EXCEPT SELECT name FROM names WHERE board = ? AND harness = ?
		ORDER BY 1`, board, harness, harness, board, harness, harness, board, harness, board, harness, board, harness)
	return out, err
}

// declare declares harness/name on a board unless it has it, refusing a name
// that a session of the harness starts with. Its cursor starts at the board's
// newest post, having seen every edit until then. It returns whether it
// declared the name.
func (s *Store) declare(ctx context.Context, q querier, board, harness, name string, who post.Identity) (bool, error) {
	if err := post.CheckName(harness, name); err != nil {
		return false, err
	}
	named, err := isName(ctx, q, board, harness, name)
	if err != nil || named {
		return false, err
	}
	sessions, err := sessionsOn(ctx, q, board, harness)
	if err != nil {
		return false, err
	}
	for _, session := range sessions {
		if strings.HasPrefix(session, name) {
			return false, post.Errorf(post.Invalid, "%s/%s is the start of %s, a session that wrote or read on board %s: mentions of that session would reach the name instead; pick another name",
				harness, name, post.Short(harness, session), board)
		}
	}
	if _, err := q.ExecContext(ctx, `INSERT INTO names (board, harness, name, created, creator_harness, creator_session, creator_node)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, board, harness, name, s.stamp(), who.Harness, who.Session, who.Node); err != nil {
		return false, err
	}
	_, err = q.ExecContext(ctx, `INSERT INTO readers (board, reader, seen, edits_seen)
		SELECT ?, ?, last_n, last_edit FROM boards WHERE id = ? ON CONFLICT DO NOTHING`, board, harness+"/"+name, board)
	return true, err
}

// adoptable returns the one session of a harness that wrote or read on a
// board and starts with prefix, given as SESSION or HARNESS/SESSION.
func adoptable(ctx context.Context, q querier, board, harness, prefix string) (string, error) {
	if h, rest, ok := strings.Cut(prefix, "/"); ok {
		if h != harness {
			return "", post.Errorf(post.Invalid, "can't adopt %s: a name adopts only sessions of its harness, %s", prefix, harness)
		}
		prefix = rest
	}
	if !post.ValidSession(prefix) {
		return "", post.Errorf(post.Invalid, "invalid session %q to adopt", prefix)
	}
	sessions, err := sessionsOn(ctx, q, board, harness)
	if err != nil {
		return "", err
	}
	var found []string
	for _, session := range sessions {
		if strings.HasPrefix(session, prefix) {
			found = append(found, session)
		}
	}
	switch len(found) {
	case 0:
		return "", post.Errorf(post.Invalid, "no session of %s that wrote or read on board %s starts with %q", harness, board, prefix)
	case 1:
		return found[0], nil
	}
	return "", post.Errorf(post.Invalid, "%d sessions of %s on board %s start with %q: give more of the one to adopt", len(found), harness, board, prefix)
}

// adopt makes harness/name adopt session, and merges what the session had
// seen into what the name has, as api/cli.md's Names says. It fails if
// another name adopted the session, and changes nothing if this one did.
func adopt(ctx context.Context, q querier, board, harness, name, session, stamp string) error {
	other, err := adopter(ctx, q, board, harness, session)
	switch {
	case err != nil:
		return err
	case other == name:
		return nil
	case other != "":
		return post.Errorf(post.Invalid, "%s is adopted by %s/%s: at most one name of a board adopts a session",
			post.Short(harness, session), harness, other)
	}
	before, err := adoptedBy(ctx, q, board, harness, name)
	if err != nil {
		return err
	}
	if _, err := q.ExecContext(ctx, "INSERT INTO adoptions (board, harness, session, name, adopted) SELECT ?, ?, ?, ?, ?",
		board, harness, session, name, stamp); err != nil {
		return err
	}
	return mergeSeen(ctx, q, board, harness, name, before, session)
}

// view is what a reader has seen of a board: its cursor, the edits that it
// has seen, and its seen marks, by post key.
type view struct {
	seen, edits int64
	marks       map[int64]int64
}

func viewOf(ctx context.Context, q querier, board, reader string) (view, bool, error) {
	v := view{marks: map[int64]int64{}}
	err := q.QueryRowContext(ctx, "SELECT seen, edits_seen FROM readers WHERE board = ? AND reader = ?", board, reader).Scan(&v.seen, &v.edits)
	if errors.Is(err, sql.ErrNoRows) {
		return v, false, nil
	}
	if err != nil {
		return v, false, err
	}
	rs, err := q.QueryContext(ctx, "SELECT s.post, s.edit_n FROM seen s JOIN posts p ON p.pk = s.post WHERE p.board = ? AND s.reader = ?", board, reader)
	if err != nil {
		return v, false, err
	}
	for rs.Next() {
		var pk, n int64
		if err := rs.Scan(&pk, &n); err != nil {
			_ = rs.Close()
			return v, false, err
		}
		v.marks[pk] = n
	}
	return v, true, rs.Close()
}

// mergeSeen makes the name harness/name, which had adopted the sessions
// before, see what it or session had seen, but the posts for session that
// session hadn't seen.
func mergeSeen(ctx context.Context, q querier, board, harness, name string, before []string, session string) error {
	nameReader, sessionReader := harness+"/"+name, harness+"/"+session
	sv, ok, err := viewOf(ctx, q, board, sessionReader)
	if err != nil || !ok {
		// A session that never read has seen only its own posts, which are
		// the name's now.
		return err
	}
	nv, _, err := viewOf(ctx, q, board, nameReader)
	if err != nil {
		return err
	}
	type p struct {
		pk, n            int64
		harness, session string
	}
	rs, err := q.QueryContext(ctx, "SELECT pk, n, harness, session FROM posts WHERE board = ? ORDER BY n", board)
	if err != nil {
		return err
	}
	var all []p
	for rs.Next() {
		var x p
		if err := rs.Scan(&x.pk, &x.n, &x.harness, &x.session); err != nil {
			_ = rs.Close()
			return err
		}
		all = append(all, x)
	}
	if err := rs.Close(); err != nil {
		return err
	}
	// The posts for the session: those of threads that hold a post of its,
	// and those that mention it by its id or a prefix of it, but not a name.
	forSession := map[int64]bool{}
	for _, query := range []string{
		`SELECT p.pk FROM posts p WHERE p.board = ? AND EXISTS (SELECT 1 FROM posts o
			WHERE (o.pk = coalesce(p.thread, p.pk) OR o.thread = coalesce(p.thread, p.pk)) AND o.harness = ? AND o.session = ?)`,
		`SELECT p.pk FROM posts p WHERE p.board = ? AND EXISTS (SELECT 1 FROM mentions m WHERE m.post = p.pk
			AND m.harness = ? AND m.session <> '' AND substr(?, 1, length(m.session)) = m.session
			AND NOT EXISTS (SELECT 1 FROM names n WHERE n.board = p.board AND n.harness = m.harness AND n.name = m.session))`,
	} {
		pks, err := column[int64](ctx, q, query, board, harness, session)
		if err != nil {
			return err
		}
		for _, pk := range pks {
			forSession[pk] = true
		}
	}
	nameOwn := append([]string{name}, before...)
	seen, edits := min(nv.seen, sv.seen), min(nv.edits, sv.edits)
	if _, err := q.ExecContext(ctx, "UPDATE readers SET seen = ?, edits_seen = ? WHERE board = ? AND reader = ?", seen, edits, board, nameReader); err != nil {
		return err
	}
	if _, err := q.ExecContext(ctx, "DELETE FROM seen WHERE reader = ? AND post IN (SELECT pk FROM posts WHERE board = ?)", nameReader, board); err != nil {
		return err
	}
	for _, x := range all {
		markN, hasN := nv.marks[x.pk]
		markS, hasS := sv.marks[x.pk]
		seenN := x.n <= nv.seen || hasN || (x.harness == harness && slices.Contains(nameOwn, x.session))
		seenS := x.n <= sv.seen || hasS || (x.harness == harness && x.session == session)
		var e int64
		switch {
		case forSession[x.pk] && !seenS, !seenN && !seenS:
			continue
		case seenN && seenS:
			e = max(nv.edits, markN, sv.edits, markS)
		case seenN:
			e = max(nv.edits, markN)
		default:
			e = max(sv.edits, markS)
		}
		if x.n > seen || e > edits {
			if _, err := q.ExecContext(ctx, "INSERT INTO seen (post, reader, edit_n) VALUES (?, ?, ?)", x.pk, nameReader, e); err != nil {
				return err
			}
		}
	}
	return nil
}

// AddName declares a name on a board, unless it has it, and makes it adopt
// the sessions of req.Adopt, which only people may. created says whether it
// declared the name.
func (s *Store) AddName(ctx context.Context, who post.Identity, board string, req post.NewName) (n post.Name, created bool, err error) {
	harness, name, err := post.ParseName(req.Name)
	if err != nil {
		return n, false, err
	}
	if len(req.Adopt) > 0 && !who.IsPerson() {
		return n, false, post.Errorf(post.Forbidden, "only a person (%s/...) can make a name adopt sessions", post.Person)
	}
	err = s.tx(ctx, func(tx *sql.Tx) error {
		if err := checkBoard(ctx, tx, board); err != nil {
			return err
		}
		if created, err = s.declare(ctx, tx, board, harness, name, who); err != nil {
			return err
		}
		for _, prefix := range req.Adopt {
			session, err := adoptable(ctx, tx, board, harness, prefix)
			if err != nil {
				return err
			}
			if err := adopt(ctx, tx, board, harness, name, session, s.stamp()); err != nil {
				return err
			}
		}
		n, err = s.nameInfo(ctx, tx, board, harness, name)
		return err
	})
	return n, created, err
}

// Claim makes who's session carry the name req.Name of its harness on a
// board, as api/cli.md's agora claim says.
func (s *Store) Claim(ctx context.Context, who post.Identity, board string, req post.ClaimRequest) (post.Name, error) {
	var n post.Name
	if who.IsPerson() {
		return n, post.Errorf(post.Invalid, "people don't claim names: %s/... already outlives sessions", post.Person)
	}
	if err := post.CheckName(who.Harness, req.Name); err != nil {
		return n, err
	}
	model := post.ViaOf(ctx).Model
	err := s.tx(ctx, func(tx *sql.Tx) error {
		if err := checkBoard(ctx, tx, board); err != nil {
			return err
		}
		if named, err := isName(ctx, tx, board, who.Harness, who.Session); err != nil || named {
			if err == nil {
				err = post.Errorf(post.Invalid, "%s is a name, not a session: claim it from the session that carries it, without --as or AGORA_AUTHOR", who.Harness+"/"+who.Session)
			}
			return err
		}
		cur, carries, err := currentClaim(ctx, tx, board, who.Harness, who.Session)
		if err != nil {
			return err
		}
		if carries {
			if cur.name != req.Name {
				return post.Errorf(post.Invalid, "this session carries %s/%s on board %s: agora claim --release lets it go first", who.Harness, cur.name, board)
			}
			if model != "" {
				if _, err := tx.ExecContext(ctx, "UPDATE claims SET model = ? WHERE rowid = ?", model, cur.rowid); err != nil {
					return err
				}
			}
			n, err = s.nameInfo(ctx, tx, board, who.Harness, req.Name)
			return err
		}
		if _, err := s.declare(ctx, tx, board, who.Harness, req.Name, who); err != nil {
			return err
		}
		h, held, err := holder(ctx, tx, board, who.Harness, req.Name)
		if err != nil {
			return err
		}
		if held {
			if !req.Take {
				return post.Errorf(post.Conflict, "%s/%s is carried by %s, last active %s: agora claim %s --take takes it from that session",
					who.Harness, req.Name, post.Author(who.Harness, h.session, h.node), h.lastActive, req.Name)
			}
			if _, err := tx.ExecContext(ctx, "UPDATE claims SET ended = ?, taken_by = ? WHERE rowid = ?", s.stamp(), who.Session, h.rowid); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO claims (board, harness, name, session, node, model, claimed, last_active)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, board, who.Harness, req.Name, who.Session, who.Node, model, s.stamp(), s.stamp()); err != nil {
			return err
		}
		if other, err := adopter(ctx, tx, board, who.Harness, who.Session); err != nil {
			return err
		} else if other == "" {
			if err := adopt(ctx, tx, board, who.Harness, req.Name, who.Session, s.stamp()); err != nil {
				return err
			}
		}
		n, err = s.nameInfo(ctx, tx, board, who.Harness, req.Name)
		return err
	})
	return n, err
}

// Release lets the name that who's session carries on a board go.
func (s *Store) Release(ctx context.Context, who post.Identity, board string) (post.Name, error) {
	var n post.Name
	err := s.tx(ctx, func(tx *sql.Tx) error {
		c, err := claimOf(ctx, tx, board, who)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE claims SET ended = ? WHERE rowid = ?", s.stamp(), c.rowid); err != nil {
			return err
		}
		n, err = s.nameInfo(ctx, tx, board, who.Harness, c.name)
		return err
	})
	return n, err
}

// ClaimOf returns the name that who's session carries on a board.
func (s *Store) ClaimOf(ctx context.Context, who post.Identity, board string) (post.Name, error) {
	var n post.Name
	err := s.tx(ctx, func(tx *sql.Tx) error {
		c, err := claimOf(ctx, tx, board, who)
		if err != nil {
			return err
		}
		n, err = s.nameInfo(ctx, tx, board, who.Harness, c.name)
		return err
	})
	return n, err
}

// claimOf returns the claim of who's session on a board, failing with an
// error of kind NotFound that says whether another session took its name.
func claimOf(ctx context.Context, q querier, board string, who post.Identity) (claim, error) {
	if err := checkBoard(ctx, q, board); err != nil {
		return claim{}, err
	}
	c, carries, err := currentClaim(ctx, q, board, who.Harness, who.Session)
	if err != nil || carries {
		return c, err
	}
	lost, err := displaced(ctx, q, board, who.Harness, who.Session)
	if err != nil {
		return c, err
	}
	if lost != "" {
		var by, node string
		_ = q.QueryRowContext(ctx, `SELECT c.taken_by, coalesce(t.node, '') FROM claims c
			LEFT JOIN claims t ON t.board = c.board AND t.harness = c.harness AND t.name = c.name AND t.session = c.taken_by
			WHERE c.board = ? AND c.harness = ? AND c.session = ? ORDER BY c.rowid DESC, t.rowid DESC LIMIT 1`,
			board, who.Harness, who.Session).Scan(&by, &node)
		return c, post.Errorf(post.NotFound, "this session carries no name on board %s: %s took %s from it; agora claim %s --take takes it back",
			board, post.Author(who.Harness, by, node), lost, strings.TrimPrefix(lost, who.Harness+"/"))
	}
	return c, post.Errorf(post.NotFound, "this session carries no name on board %s: agora claim NAME claims one", board)
}

// Names returns a board's names, by harness, then by name.
func (s *Store) Names(ctx context.Context, board string) ([]post.Name, error) {
	out := []post.Name{}
	err := s.tx(ctx, func(tx *sql.Tx) error {
		if err := checkBoard(ctx, tx, board); err != nil {
			return err
		}
		rs, err := tx.QueryContext(ctx, "SELECT harness, name FROM names WHERE board = ? ORDER BY harness, name", board)
		if err != nil {
			return err
		}
		var keys [][2]string
		for rs.Next() {
			var k [2]string
			if err := rs.Scan(&k[0], &k[1]); err != nil {
				_ = rs.Close()
				return err
			}
			keys = append(keys, k)
		}
		if err := rs.Close(); err != nil {
			return err
		}
		for _, k := range keys {
			n, err := s.nameInfo(ctx, tx, board, k[0], k[1])
			if err != nil {
				return err
			}
			out = append(out, n)
		}
		return nil
	})
	return out, err
}

// nameInfo returns a name's <name>.
func (s *Store) nameInfo(ctx context.Context, q querier, board, harness, name string) (post.Name, error) {
	n := post.Name{Board: board, Harness: harness, Name: harness + "/" + name, Adopted: []string{}}
	if err := q.QueryRowContext(ctx, "SELECT created FROM names WHERE board = ? AND harness = ? AND name = ?", board, harness, name).Scan(&n.Created); err != nil {
		return n, err
	}
	var ended string
	err := q.QueryRowContext(ctx, `SELECT session, node, model, claimed, last_active, ended FROM claims
		WHERE board = ? AND harness = ? AND name = ? ORDER BY ended = '' DESC, rowid DESC LIMIT 1`, board, harness, name).
		Scan(&n.Session, &n.Node, &n.Model, &n.Claimed, &n.LastActive, &ended)
	switch {
	case errors.Is(err, sql.ErrNoRows):
	case err != nil:
		return n, err
	default:
		n.Carried = ended == ""
		n.Carrier = post.Author(harness, n.Session, n.Node)
	}
	adopted, err := adoptedBy(ctx, q, board, harness, name)
	if err != nil {
		return n, err
	}
	for _, session := range adopted {
		n.Adopted = append(n.Adopted, harness+"/"+session)
	}
	a := post.Actor{Identity: post.Identity{Harness: harness, Session: name}, Name: true, Adopted: adopted}
	n.Pending, err = pending(ctx, q, board, a)
	return n, err
}
