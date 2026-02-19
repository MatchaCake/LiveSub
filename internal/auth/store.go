package auth

import (
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"golang.org/x/crypto/bcrypt"
)

type User struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	IsAdmin  bool   `json:"is_admin"`
}

type Store struct {
	db *sql.DB
}

func NewStore(dbPath string) (*Store, error) {
	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_synchronous=NORMAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	// SQLite only supports one writer at a time; limit pool to 1 connection
	// to avoid SQLITE_BUSY under concurrent web handler access.
	db.SetMaxOpenConns(1)

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	if err := s.migrateBili(); err != nil {
		return nil, fmt.Errorf("migrate bili: %w", err)
	}
	if err := s.migrateStreams(); err != nil {
		return nil, fmt.Errorf("migrate streams: %w", err)
	}
	return s, nil
}

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			username TEXT UNIQUE NOT NULL,
			password_hash TEXT NOT NULL,
			is_admin INTEGER NOT NULL DEFAULT 0
		);
		CREATE TABLE IF NOT EXISTS user_rooms (
			user_id INTEGER NOT NULL,
			room_id INTEGER NOT NULL,
			PRIMARY KEY (user_id, room_id),
			FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
		);
		CREATE TABLE IF NOT EXISTS user_accounts (
			user_id INTEGER NOT NULL,
			account_name TEXT NOT NULL,
			PRIMARY KEY (user_id, account_name),
			FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
		);
		CREATE TABLE IF NOT EXISTS audit_log (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			ts DATETIME NOT NULL DEFAULT (datetime('now', 'localtime')),
			user_id INTEGER NOT NULL,
			username TEXT NOT NULL,
			action TEXT NOT NULL,
			detail TEXT,
			ip TEXT
		);
		CREATE INDEX IF NOT EXISTS idx_audit_ts ON audit_log(ts DESC);
		CREATE TABLE IF NOT EXISTS sessions (
			token TEXT PRIMARY KEY,
			user_id INTEGER NOT NULL,
			expiry DATETIME NOT NULL,
			FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
		);
	`)
	return err
}

// SaveSession persists a session token.
func (s *Store) SaveSession(token string, userID int64, expiry time.Time) error {
	_, err := s.db.Exec("INSERT OR REPLACE INTO sessions (token, user_id, expiry) VALUES (?, ?, ?)",
		token, userID, expiry.Format(time.RFC3339))
	return err
}

// LoadSessions returns all non-expired sessions.
func (s *Store) LoadSessions() (map[string]*Session, error) {
	rows, err := s.db.Query("SELECT token, user_id, expiry FROM sessions WHERE expiry > datetime('now', 'localtime')")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string]*Session)
	for rows.Next() {
		var token string
		var userID int64
		var expiryStr string
		if err := rows.Scan(&token, &userID, &expiryStr); err != nil {
			continue
		}
		t, _ := time.Parse(time.RFC3339, expiryStr)
		result[token] = &Session{UserID: userID, Expiry: t}
	}
	return result, nil
}

// DeleteSession removes a session.
func (s *Store) DeleteSession(token string) error {
	_, err := s.db.Exec("DELETE FROM sessions WHERE token = ?", token)
	return err
}

// CleanExpiredSessions removes expired sessions.
func (s *Store) CleanExpiredSessions() {
	s.db.Exec("DELETE FROM sessions WHERE expiry <= datetime('now', 'localtime')")
}

// Session represents a stored session.
type Session struct {
	UserID int64
	Expiry time.Time
}

// EnsureAdmin creates the admin user if no users exist, or updates password if admin exists.
func (s *Store) EnsureAdmin(username, password string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	// Try update first
	res, err := s.db.Exec(
		`UPDATE users SET password_hash = ?, is_admin = 1 WHERE username = ?`,
		string(hash), username,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n > 0 {
		return nil
	}

	// Insert new
	_, err = s.db.Exec(
		`INSERT INTO users (username, password_hash, is_admin) VALUES (?, ?, 1)`,
		username, string(hash),
	)
	return err
}

// Authenticate checks credentials and returns the user.
func (s *Store) Authenticate(username, password string) (*User, error) {
	var u User
	var hash string
	err := s.db.QueryRow(
		`SELECT id, username, is_admin, password_hash FROM users WHERE username = ?`,
		username,
	).Scan(&u.ID, &u.Username, &u.IsAdmin, &hash)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)); err != nil {
		return nil, nil
	}
	return &u, nil
}

// GetUser returns a user by ID.
func (s *Store) GetUser(id int64) (*User, error) {
	var u User
	err := s.db.QueryRow(
		`SELECT id, username, is_admin FROM users WHERE id = ?`, id,
	).Scan(&u.ID, &u.Username, &u.IsAdmin)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &u, err
}

// ListUsers returns all users.
func (s *Store) ListUsers() ([]User, error) {
	rows, err := s.db.Query(`SELECT id, username, is_admin FROM users ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Username, &u.IsAdmin); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, nil
}

// CreateUser creates a new user.
func (s *Store) CreateUser(username, password string, isAdmin bool) (*User, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	res, err := s.db.Exec(
		`INSERT INTO users (username, password_hash, is_admin) VALUES (?, ?, ?)`,
		username, string(hash), isAdmin,
	)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &User{ID: id, Username: username, IsAdmin: isAdmin}, nil
}

// DeleteUser removes a user.
func (s *Store) DeleteUser(id int64) error {
	_, err := s.db.Exec(`DELETE FROM users WHERE id = ? AND is_admin = 0`, id)
	return err
}

// UpdatePassword changes a user's password.
func (s *Store) UpdatePassword(id int64, password string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`UPDATE users SET password_hash = ? WHERE id = ?`, string(hash), id)
	return err
}

// --- Room assignments ---

// GetUserRooms returns room IDs assigned to a user.
func (s *Store) GetUserRooms(userID int64) ([]int64, error) {
	rows, err := s.db.Query(`SELECT room_id FROM user_rooms WHERE user_id = ?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rooms []int64
	for rows.Next() {
		var r int64
		if err := rows.Scan(&r); err != nil {
			return nil, err
		}
		rooms = append(rooms, r)
	}
	return rooms, nil
}

// SetUserRooms replaces all room assignments for a user.
func (s *Store) SetUserRooms(userID int64, roomIDs []int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM user_rooms WHERE user_id = ?`, userID); err != nil {
		return err
	}
	for _, rid := range roomIDs {
		if _, err := tx.Exec(`INSERT INTO user_rooms (user_id, room_id) VALUES (?, ?)`, userID, rid); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// --- Account assignments ---

// GetUserAccounts returns account names assigned to a user.
func (s *Store) GetUserAccounts(userID int64) ([]string, error) {
	rows, err := s.db.Query(`SELECT account_name FROM user_accounts WHERE user_id = ?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var accounts []string
	for rows.Next() {
		var a string
		if err := rows.Scan(&a); err != nil {
			return nil, err
		}
		accounts = append(accounts, a)
	}
	return accounts, nil
}

// SetUserAccounts replaces all account assignments for a user.
func (s *Store) SetUserAccounts(userID int64, accounts []string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM user_accounts WHERE user_id = ?`, userID); err != nil {
		return err
	}
	for _, a := range accounts {
		if _, err := tx.Exec(`INSERT INTO user_accounts (user_id, account_name) VALUES (?, ?)`, userID, a); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// UserDetail includes assignments.
type UserDetail struct {
	User
	Rooms    []int64  `json:"rooms"`
	Accounts []string `json:"accounts"`
}

// GetUserDetail returns user with their assignments.
func (s *Store) GetUserDetail(id int64) (*UserDetail, error) {
	u, err := s.GetUser(id)
	if err != nil || u == nil {
		return nil, err
	}
	rooms, _ := s.GetUserRooms(id)
	accounts, _ := s.GetUserAccounts(id)
	if rooms == nil {
		rooms = []int64{}
	}
	if accounts == nil {
		accounts = []string{}
	}
	return &UserDetail{User: *u, Rooms: rooms, Accounts: accounts}, nil
}

// ListUserDetails returns all users with assignments.
func (s *Store) ListUserDetails() ([]UserDetail, error) {
	users, err := s.ListUsers()
	if err != nil {
		return nil, err
	}
	var details []UserDetail
	for _, u := range users {
		rooms, _ := s.GetUserRooms(u.ID)
		accounts, _ := s.GetUserAccounts(u.ID)
		if rooms == nil {
			rooms = []int64{}
		}
		if accounts == nil {
			accounts = []string{}
		}
		details = append(details, UserDetail{User: u, Rooms: rooms, Accounts: accounts})
	}
	return details, nil
}

// --- Audit log ---

type AuditEntry struct {
	ID       int64  `json:"id"`
	Time     string `json:"time"`
	UserID   int64  `json:"user_id"`
	Username string `json:"username"`
	Action   string `json:"action"`
	Detail   string `json:"detail"`
	IP       string `json:"ip"`
}

// Log records a user action.
func (s *Store) Log(userID int64, username, action, detail, ip string) {
	if _, err := s.db.Exec(
		`INSERT INTO audit_log (user_id, username, action, detail, ip) VALUES (?, ?, ?, ?, ?)`,
		userID, username, action, detail, ip,
	); err != nil {
		slog.Error("audit log write failed", "err", err)
	}
}

// GetAuditLog returns recent audit entries (newest first).
func (s *Store) GetAuditLog(limit int) ([]AuditEntry, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.Query(
		`SELECT id, ts, user_id, username, action, COALESCE(detail,''), COALESCE(ip,'') FROM audit_log ORDER BY id DESC LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []AuditEntry
	for rows.Next() {
		var e AuditEntry
		if err := rows.Scan(&e.ID, &e.Time, &e.UserID, &e.Username, &e.Action, &e.Detail, &e.IP); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}
