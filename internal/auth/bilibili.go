package auth

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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
	return err
}

// SaveBiliAccount inserts or updates a Bilibili account.
func (s *Store) SaveBiliAccount(name, sessdata, biliJCT string, uid int64, danmakuMax int, expiresAt string) (*BiliAccount, error) {
	// Update if same name exists
	res, err := s.db.Exec(
		`UPDATE bili_accounts SET sessdata=?, bili_jct=?, uid=?, danmaku_max=?, expires_at=?, valid=1 WHERE name=?`,
		sessdata, biliJCT, uid, danmakuMax, expiresAt, name,
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
		`INSERT INTO bili_accounts (name, sessdata, bili_jct, uid, danmaku_max, expires_at) VALUES (?, ?, ?, ?, ?, ?)`,
		name, sessdata, biliJCT, uid, danmakuMax, expiresAt,
	)
	if err != nil {
		return nil, err
	}
	id, _ := r.LastInsertId()
	return &BiliAccount{ID: id, Name: name, SESSDATA: sessdata, BiliJCT: biliJCT, UID: uid, DanmakuMax: danmakuMax, Valid: true}, nil
}

func (s *Store) getBiliAccountByName(name string) (*BiliAccount, error) {
	var a BiliAccount
	var expiresAt sql.NullString
	err := s.db.QueryRow(
		`SELECT id, name, sessdata, bili_jct, uid, danmaku_max, created_at, expires_at, valid FROM bili_accounts WHERE name=?`, name,
	).Scan(&a.ID, &a.Name, &a.SESSDATA, &a.BiliJCT, &a.UID, &a.DanmakuMax, &a.CreatedAt, &expiresAt, &a.Valid)
	if err != nil {
		return nil, err
	}
	if expiresAt.Valid {
		a.ExpiresAt = expiresAt.String
	}
	return &a, nil
}

// ListBiliAccounts returns all accounts (with credentials).
func (s *Store) ListBiliAccounts() ([]BiliAccount, error) {
	rows, err := s.db.Query(`SELECT id, name, sessdata, bili_jct, uid, danmaku_max, created_at, COALESCE(expires_at,''), valid FROM bili_accounts ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var accounts []BiliAccount
	for rows.Next() {
		var a BiliAccount
		if err := rows.Scan(&a.ID, &a.Name, &a.SESSDATA, &a.BiliJCT, &a.UID, &a.DanmakuMax, &a.CreatedAt, &a.ExpiresAt, &a.Valid); err != nil {
			return nil, err
		}
		accounts = append(accounts, a)
	}
	return accounts, nil
}

// ListBiliAccountSummaries returns accounts without credentials.
func (s *Store) ListBiliAccountSummaries() ([]BiliAccountSummary, error) {
	rows, err := s.db.Query(`SELECT id, name, uid, danmaku_max, created_at, COALESCE(expires_at,''), valid FROM bili_accounts ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var accounts []BiliAccountSummary
	for rows.Next() {
		var a BiliAccountSummary
		if err := rows.Scan(&a.ID, &a.Name, &a.UID, &a.DanmakuMax, &a.CreatedAt, &a.ExpiresAt, &a.Valid); err != nil {
			return nil, err
		}
		accounts = append(accounts, a)
	}
	return accounts, nil
}

// DeleteBiliAccount removes an account.
func (s *Store) DeleteBiliAccount(id int64) error {
	_, err := s.db.Exec(`DELETE FROM bili_accounts WHERE id=?`, id)
	return err
}

// UpdateBiliAccountValid marks an account as valid/invalid.
func (s *Store) UpdateBiliAccountValid(id int64, valid bool) error {
	_, err := s.db.Exec(`UPDATE bili_accounts SET valid=? WHERE id=?`, valid, id)
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
	resp, err := http.Get("https://passport.bilibili.com/x/passport-login/web/qrcode/generate")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
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
	Status   string `json:"status"`   // "waiting", "scanned", "confirmed", "expired"
	SESSDATA string `json:"sessdata,omitempty"`
	BiliJCT  string `json:"bili_jct,omitempty"`
	UID      int64  `json:"uid,omitempty"`
}

// PollQRCode checks login status and extracts cookies on success.
func PollQRCode(qrcodeKey string) (*QRPollResult, error) {
	url := fmt.Sprintf("https://passport.bilibili.com/x/passport-login/web/qrcode/poll?qrcode_key=%s", qrcodeKey)

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

	body, _ := io.ReadAll(resp.Body)
	var result struct {
		Code int `json:"code"`
		Data struct {
			Code      int    `json:"code"`
			Message   string `json:"message"`
			URL       string `json:"url"`
			Timestamp int64  `json:"timestamp"`
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
			Status:   "confirmed",
			SESSDATA: sessdata,
			BiliJCT:  biliJCT,
			UID:      uid,
		}, nil
	default:
		return nil, fmt.Errorf("unknown status code %d: %s", result.Data.Code, result.Data.Message)
	}
}

// GetBiliUserInfo fetches username from Bilibili API using SESSDATA.
func GetBiliUserInfo(sessdata string) (string, error) {
	req, _ := http.NewRequest("GET", "https://api.bilibili.com/x/web-interface/nav", nil)
	req.Header.Set("Cookie", "SESSDATA="+sessdata)
	req.Header.Set("User-Agent", "Mozilla/5.0 livesub/1.0")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result struct {
		Code int `json:"code"`
		Data struct {
			UName string `json:"uname"`
		} `json:"data"`
	}
	json.Unmarshal(body, &result)
	if result.Code != 0 || result.Data.UName == "" {
		return "", fmt.Errorf("failed to get user info (code=%d)", result.Code)
	}
	return result.Data.UName, nil
}
