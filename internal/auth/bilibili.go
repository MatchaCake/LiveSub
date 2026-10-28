package auth

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"time"
)

// BiliAccount represents a stored Bilibili account.
type BiliAccount struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	SESSDATA   string `json:"sessdata,omitempty"`
	BiliJCT    string `json:"bili_jct,omitempty"`
	UID        int64  `json:"uid"`
	DanmakuMax int    `json:"danmaku_max"`
	CreatedAt  string `json:"created_at"`
	ExpiresAt  string `json:"expires_at,omitempty"`
	Valid      bool   `json:"valid"` // whether cookies are still working
	// RefreshToken is the web login refresh_token (QR login data.refresh_token),
	// needed for the official cookie refresh flow. Same at-rest protection as
	// the cookies (users.db, 0600). Never log or return it from the API.
	RefreshToken string `json:"-"`
	BiliRefreshState
}

// BiliRefreshState is the cookie auto-refresh status shown in the admin panel.
type BiliRefreshState struct {
	HasRefreshToken  bool   `json:"has_refresh_token"`
	RefreshStatus    string `json:"refresh_status"`     // "", not-needed, refreshed, failed, no-token, login-invalid
	RefreshError     string `json:"refresh_error"`      // human-readable failed step (never contains secrets)
	RefreshCheckedAt string `json:"refresh_checked_at"` // last cookie/info check
	RefreshedAt      string `json:"refreshed_at"`       // last successful refresh
}

// BiliAccountSummary is the safe version without credentials.
type BiliAccountSummary struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	UID        int64  `json:"uid"`
	DanmakuMax int    `json:"danmaku_max"`
	CreatedAt  string `json:"created_at"`
	ExpiresAt  string `json:"expires_at,omitempty"`
	Valid      bool   `json:"valid"`
	BiliRefreshState
}

func (s *Store) migrateBili() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS bili_accounts (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			sessdata TEXT NOT NULL,
			bili_jct TEXT NOT NULL,
			uid INTEGER NOT NULL DEFAULT 0,
			danmaku_max INTEGER NOT NULL DEFAULT 20,
			created_at DATETIME NOT NULL DEFAULT (datetime('now')),
			expires_at TEXT,
			valid INTEGER NOT NULL DEFAULT 1
		);
	`)
	if err != nil {
		return err
	}
	// Cookie auto-refresh columns (2026-10-28). Added in place so existing
	// databases keep their accounts; old rows have no refresh_token until the
	// account is re-scanned once.
	cols := map[string]bool{}
	rows, err := s.db.Query(`PRAGMA table_info(bili_accounts)`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var cid, notnull, pk int
		var name, typ string
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			rows.Close()
			return err
		}
		cols[name] = true
	}
	rows.Close()
	for _, col := range []string{"refresh_token", "refresh_status", "refresh_error", "refresh_checked_at", "refreshed_at"} {
		if cols[col] {
			continue
		}
		if _, err := s.db.Exec(`ALTER TABLE bili_accounts ADD COLUMN ` + col + ` TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("add column %s: %w", col, err)
		}
	}
	return nil
}

const biliAccountCols = `id, name, sessdata, bili_jct, uid, danmaku_max, created_at, COALESCE(expires_at,''), valid,
	refresh_token, refresh_status, refresh_error, refresh_checked_at, refreshed_at`

type rowScanner interface{ Scan(dest ...any) error }

func scanBiliAccount(r rowScanner) (*BiliAccount, error) {
	var a BiliAccount
	if err := r.Scan(&a.ID, &a.Name, &a.SESSDATA, &a.BiliJCT, &a.UID, &a.DanmakuMax, &a.CreatedAt, &a.ExpiresAt, &a.Valid,
		&a.RefreshToken, &a.RefreshStatus, &a.RefreshError, &a.RefreshCheckedAt, &a.RefreshedAt); err != nil {
		return nil, err
	}
	a.HasRefreshToken = a.RefreshToken != ""
	return &a, nil
}

// GetBiliAccount returns one account (with credentials) by ID.
func (s *Store) GetBiliAccount(id int64) (*BiliAccount, error) {
	return scanBiliAccount(s.db.QueryRow(`SELECT `+biliAccountCols+` FROM bili_accounts WHERE id=?`, id))
}

// ReplaceBiliCookies swaps in refreshed credentials in a single UPDATE. It is a
// compare-and-swap on the old SESSDATA: if the row changed meanwhile (QR
// re-scan, delete) nothing is written and an error is returned, so a refresh
// can never clobber a newer login.
func (s *Store) ReplaceBiliCookies(id int64, oldSessdata, sessdata, biliJCT, refreshToken, refreshedAt string) error {
	res, err := s.db.Exec(
		`UPDATE bili_accounts SET sessdata=?, bili_jct=?, refresh_token=?, refreshed_at=?, valid=1 WHERE id=? AND sessdata=?`,
		sessdata, biliJCT, refreshToken, refreshedAt, id, oldSessdata,
	)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return fmt.Errorf("account %d changed during refresh", id)
	}
	return nil
}

// SetBiliRefreshStatus records the outcome of the last refresh check.
func (s *Store) SetBiliRefreshStatus(id int64, status, errText, checkedAt string) error {
	_, err := s.db.Exec(`UPDATE bili_accounts SET refresh_status=?, refresh_error=?, refresh_checked_at=? WHERE id=?`,
		status, errText, checkedAt, id)
	return err
}

// SaveBiliAccount inserts or updates a Bilibili account. refreshToken may be
// empty (older callers); a QR login always supplies it.
func (s *Store) SaveBiliAccount(name, sessdata, biliJCT, refreshToken string, uid int64, danmakuMax int, expiresAt string) (*BiliAccount, error) {
	// Update if same name exists
	res, err := s.db.Exec(
		`UPDATE bili_accounts SET sessdata=?, bili_jct=?, refresh_token=?, uid=?, danmaku_max=?, expires_at=?, valid=1,
			refresh_status='', refresh_error='' WHERE name=?`,
		sessdata, biliJCT, refreshToken, uid, danmakuMax, expiresAt, name,
	)
	if err != nil {
		return nil, err
	}
	n, _ := res.RowsAffected()
	if n > 0 {
		// Return updated
		return s.getBiliAccountByName(name)
	}

	// Insert new
	r, err := s.db.Exec(
		`INSERT INTO bili_accounts (name, sessdata, bili_jct, refresh_token, uid, danmaku_max, expires_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		name, sessdata, biliJCT, refreshToken, uid, danmakuMax, expiresAt,
	)
	if err != nil {
		return nil, err
	}
	id, _ := r.LastInsertId()
	return s.GetBiliAccount(id)
}

func (s *Store) getBiliAccountByName(name string) (*BiliAccount, error) {
	return scanBiliAccount(s.db.QueryRow(`SELECT `+biliAccountCols+` FROM bili_accounts WHERE name=?`, name))
}

// ListBiliAccounts returns all accounts (with credentials).
func (s *Store) ListBiliAccounts() ([]BiliAccount, error) {
	rows, err := s.db.Query(`SELECT ` + biliAccountCols + ` FROM bili_accounts ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var accounts []BiliAccount
	for rows.Next() {
		a, err := scanBiliAccount(rows)
		if err != nil {
			return nil, err
		}
		accounts = append(accounts, *a)
	}
	return accounts, rows.Err()
}

// ListBiliAccountSummaries returns accounts without credentials.
func (s *Store) ListBiliAccountSummaries() ([]BiliAccountSummary, error) {
	accounts, err := s.ListBiliAccounts()
	if err != nil {
		return nil, err
	}
	out := make([]BiliAccountSummary, 0, len(accounts))
	for _, a := range accounts {
		out = append(out, BiliAccountSummary{ID: a.ID, Name: a.Name, UID: a.UID, DanmakuMax: a.DanmakuMax,
			CreatedAt: a.CreatedAt, ExpiresAt: a.ExpiresAt, Valid: a.Valid, BiliRefreshState: a.BiliRefreshState})
	}
	return out, nil
}

// SetBiliAccountValid flips the stored validity flag. Marking an account
// invalid also makes syncDBBots disable its live bot, so a dead cookie stops
// burning sends until the operator re-scans the QR code.
func (s *Store) SetBiliAccountValid(name string, valid bool) error {
	v := 0
	if valid {
		v = 1
	}
	_, err := s.db.Exec(`UPDATE bili_accounts SET valid=? WHERE name=?`, v, name)
	return err
}

// CheckSessdataAlive asks Bilibili whether a SESSDATA cookie still carries a
// login session (nav API isLogin). A network/parse error returns err — the
// caller must NOT treat that as "expired"; only an explicit isLogin=false is.
func CheckSessdataAlive(sessdata string) (bool, error) {
	req, err := http.NewRequest("GET", "https://api.bilibili.com/x/web-interface/nav", nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.AddCookie(&http.Cookie{Name: "SESSDATA", Value: sessdata})
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return false, err
	}
	var nav struct {
		Data struct {
			IsLogin bool `json:"isLogin"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &nav); err != nil {
		return false, fmt.Errorf("nav parse: %w", err)
	}
	return nav.Data.IsLogin, nil
}

// DeleteBiliAccount removes an account.
func (s *Store) DeleteBiliAccount(id int64) error {
	_, err := s.db.Exec(`DELETE FROM bili_accounts WHERE id=?`, id)
	return err
}

// UpdateBiliAccountDanmakuMax updates the danmaku max length.
func (s *Store) UpdateBiliAccountDanmakuMax(id int64, max int) error {
	_, err := s.db.Exec(`UPDATE bili_accounts SET danmaku_max=? WHERE id=?`, max, id)
	return err
}

// --- Bilibili QR Login ---

type QRCodeResult struct {
	URL       string `json:"url"`
	QRCodeKey string `json:"qrcode_key"`
}

// GenerateQRCode calls Bilibili API to get a login QR code.
func GenerateQRCode() (*QRCodeResult, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get("https://passport.bilibili.com/x/passport-login/web/qrcode/generate")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	var result struct {
		Code int `json:"code"`
		Data struct {
			URL       string `json:"url"`
			QRCodeKey string `json:"qrcode_key"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	if result.Code != 0 {
		return nil, fmt.Errorf("bilibili API error code: %d", result.Code)
	}
	return &QRCodeResult{URL: result.Data.URL, QRCodeKey: result.Data.QRCodeKey}, nil
}

// QRPollResult represents the status of a QR login poll.
type QRPollResult struct {
	Status       string `json:"status"` // "waiting", "scanned", "confirmed", "expired"
	SESSDATA     string `json:"-"`
	BiliJCT      string `json:"-"`
	RefreshToken string `json:"-"`
	UID          int64  `json:"uid,omitempty"`
}

// qrPollURL is a var so tests can point it at a fake server.
var qrPollURL = "https://passport.bilibili.com/x/passport-login/web/qrcode/poll"

// PollQRCode checks login status and extracts cookies on success.
func PollQRCode(qrcodeKey string) (*QRPollResult, error) {
	url := qrPollURL + "?qrcode_key=" + neturl.QueryEscape(qrcodeKey)

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse // don't follow redirects, we need cookies
		},
		Timeout: 10 * time.Second,
	}

	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	var result struct {
		Code int `json:"code"`
		Data struct {
			Code         int    `json:"code"`
			Message      string `json:"message"`
			URL          string `json:"url"`
			RefreshToken string `json:"refresh_token"`
			Timestamp    int64  `json:"timestamp"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	switch result.Data.Code {
	case 86101:
		return &QRPollResult{Status: "waiting"}, nil
	case 86090:
		return &QRPollResult{Status: "scanned"}, nil
	case 86038:
		return &QRPollResult{Status: "expired"}, nil
	case 0:
		// Success! Extract cookies
		var sessdata, biliJCT string
		var uid int64
		for _, cookie := range resp.Cookies() {
			switch cookie.Name {
			case "SESSDATA":
				sessdata = cookie.Value
			case "bili_jct":
				biliJCT = cookie.Value
			case "DedeUserID":
				fmt.Sscanf(cookie.Value, "%d", &uid)
			}
		}
		if sessdata == "" || biliJCT == "" {
			return nil, fmt.Errorf("login succeeded but cookies not found in response")
		}
		return &QRPollResult{
			Status:       "confirmed",
			SESSDATA:     sessdata,
			BiliJCT:      biliJCT,
			RefreshToken: result.Data.RefreshToken,
			UID:          uid,
		}, nil
	default:
		return nil, fmt.Errorf("unknown status code %d: %s", result.Data.Code, result.Data.Message)
	}
}

// GetBiliUserInfo fetches username from Bilibili API using SESSDATA.
func GetBiliUserInfo(sessdata string) (string, error) {
	req, err := http.NewRequest("GET", "https://api.bilibili.com/x/web-interface/nav", nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Cookie", "SESSDATA="+sessdata)
	req.Header.Set("User-Agent", "Mozilla/5.0 livesub/1.0")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read body: %w", err)
	}
	var result struct {
		Code int `json:"code"`
		Data struct {
			UName string `json:"uname"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("parse json: %w", err)
	}
	if result.Code != 0 || result.Data.UName == "" {
		return "", fmt.Errorf("failed to get user info (code=%d)", result.Code)
	}
	return result.Data.UName, nil
}
