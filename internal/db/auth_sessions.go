package db

import (
	"database/sql"
	"fmt"
	"time"
)

// AuthSession contains server-side identity data. None of its fields are public JSON.
type AuthSession struct {
	TokenHash string    `json:"-"`
	Issuer    string    `json:"-"`
	ClientID  string    `json:"-"`
	Subject   string    `json:"-"`
	Phone     string    `json:"-"`
	CreatedAt time.Time `json:"-"`
	ExpiresAt time.Time `json:"-"`
}

func initAccessSchema(database *sql.DB) error {
	rows, err := database.Query("PRAGMA table_info(deploy_tasks)")
	if err != nil {
		return err
	}
	columns := map[string]bool{}
	for rows.Next() {
		var id, notNull, pk int
		var name, kind string
		var defaultValue interface{}
		if err := rows.Scan(&id, &name, &kind, &notNull, &defaultValue, &pk); err != nil {
			rows.Close()
			return err
		}
		columns[name] = true
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	tx, err := database.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, name := range []string{"operator_type", "operator_value", "operator_ip"} {
		if !columns[name] {
			if _, err := tx.Exec("ALTER TABLE deploy_tasks ADD COLUMN " + name + " TEXT NOT NULL DEFAULT ''"); err != nil {
				return err
			}
		}
	}
	if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS auth_sessions (
		token_hash TEXT PRIMARY KEY, issuer TEXT NOT NULL, client_id TEXT NOT NULL,
		subject TEXT NOT NULL, phone TEXT NOT NULL, created_at INTEGER NOT NULL, expires_at INTEGER NOT NULL
	); CREATE INDEX IF NOT EXISTS idx_auth_sessions_expiry ON auth_sessions(expires_at);`); err != nil {
		return err
	}
	return tx.Commit()
}

func SaveAuthSession(database *sql.DB, session AuthSession) error {
	_, err := database.Exec(`INSERT INTO auth_sessions (token_hash,issuer,client_id,subject,phone,created_at,expires_at) VALUES (?,?,?,?,?,?,?)`,
		session.TokenHash, session.Issuer, session.ClientID, session.Subject, session.Phone, session.CreatedAt.Unix(), session.ExpiresAt.Unix())
	if err != nil {
		return fmt.Errorf("save authentication session: %w", err)
	}
	return nil
}

func FindAuthSession(database *sql.DB, hash string) (AuthSession, error) {
	var session AuthSession
	var created, expires int64
	err := database.QueryRow(`SELECT token_hash,issuer,client_id,subject,phone,created_at,expires_at FROM auth_sessions WHERE token_hash=?`, hash).
		Scan(&session.TokenHash, &session.Issuer, &session.ClientID, &session.Subject, &session.Phone, &created, &expires)
	session.CreatedAt = time.Unix(created, 0)
	session.ExpiresAt = time.Unix(expires, 0)
	return session, err
}

func DeleteExpiredAuthSessions(database *sql.DB, now time.Time) error {
	_, err := database.Exec("DELETE FROM auth_sessions WHERE expires_at<=?", now.Unix())
	return err
}
