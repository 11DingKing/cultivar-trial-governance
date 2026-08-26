package store

import (
 "context"
 "database/sql"
 "errors"
 "testing"
)

func TestTransactionPreservesCancellationCause(t *testing.T) {
 db := openTestDB(t); ctx := context.Background(); err := db.WithTx(ctx, func(*sql.Tx) error { return context.Canceled }); if !errors.Is(err, context.Canceled) { t.Fatalf("cancellation cause lost: %v", err) }
}
