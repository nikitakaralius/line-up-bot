package utils

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

func WaitForDB(ctx context.Context, db *sql.DB) error {
	deadline := time.Now().Add(2 * time.Minute)
	for {
		if err := db.PingContext(ctx); err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("database not ready after timeout")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}
