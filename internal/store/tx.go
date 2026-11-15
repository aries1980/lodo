package store

import (
	"database/sql"
	"fmt"
)

func (s *Store) execTx(operation string, write func(*sql.Tx) error) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("store: %s: %w", operation, err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := write(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: %s: %w", operation, err)
	}
	return nil
}

func clearPendingDelete(tx *sql.Tx, key BindingKey) error {
	if _, err := tx.Exec(`DELETE FROM pending_deletes WHERE task_id=? AND block_index=?`, key.TaskID, key.BlockIndex); err != nil {
		return fmt.Errorf("store: clear pending delete %s/%d: %w", key.TaskID, key.BlockIndex, err)
	}
	return nil
}
