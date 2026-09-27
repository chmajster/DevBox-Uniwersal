package secrets

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type SQLiteStore struct {
	db     *sql.DB
	cipher Cipher
}

func NewSQLiteStore(db *sql.DB, cipher Cipher) *SQLiteStore {
	return &SQLiteStore{db: db, cipher: cipher}
}

func (s *SQLiteStore) Put(ctx context.Context, scope, name string, plaintext []byte) error {
	aad := []byte(scope + "\x00" + name)
	nonce, ciphertext, err := s.cipher.Encrypt(plaintext, aad)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO secrets(id,scope,name,nonce,ciphertext,created_at,updated_at) VALUES(lower(hex(randomblob(16))),?,?,?,?,?,?) ON CONFLICT(scope,name) DO UPDATE SET nonce=excluded.nonce,ciphertext=excluded.ciphertext,updated_at=excluded.updated_at`, scope, name, nonce, ciphertext, time.Now().UTC().Format(time.RFC3339Nano), time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("store secret: %w", err)
	}
	return nil
}
func (s *SQLiteStore) Get(ctx context.Context, scope, name string) ([]byte, error) {
	var nonce, ciphertext []byte
	err := s.db.QueryRowContext(ctx, `SELECT nonce,ciphertext FROM secrets WHERE scope=? AND name=?`, scope, name).Scan(&nonce, &ciphertext)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errors.New("secret not found")
	}
	if err != nil {
		return nil, fmt.Errorf("load secret: %w", err)
	}
	return s.cipher.Decrypt(nonce, ciphertext, []byte(scope+"\x00"+name))
}
func (s *SQLiteStore) Delete(ctx context.Context, scope, name string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM secrets WHERE scope=? AND name=?`, scope, name)
	if err != nil {
		return fmt.Errorf("delete secret: %w", err)
	}
	return nil
}
