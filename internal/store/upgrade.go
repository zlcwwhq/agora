package store

import (
	"context"
	"fmt"

	"github.com/zlcwwhq/agora/migrations"
)

// upgrade brings a database that an earlier release made, of version from,
// to migrations.Version, through each of migrations.Upgrades in turn, and
// then records the mentions of the bodies that it holds, which SQL can't
// find. q is the connection that holds ensureSchema's transaction, which the
// upgrade belongs to: the store's other methods would wait for it.
func upgrade(ctx context.Context, q querier, from int) error {
	for v := from; v < migrations.Version; v++ {
		up, ok := migrations.Upgrades[v]
		if !ok {
			return fmt.Errorf("agora has no upgrade from schema %d", v)
		}
		if _, err := q.ExecContext(ctx, up); err != nil {
			return err
		}
	}
	return recordMentions(ctx, q)
}
