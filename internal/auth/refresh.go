package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Official Bilibili web cookie refresh (bilibili-API-collect
// docs/login/cookie_refresh.md), ported from bili-digest refresh.py:
//
//  1. GET  passport …/web/cookie/info?csrf=<bili_jct>          → data.refresh
//  2. GET  www.bilibili.com/correspond/1/<RSA-OAEP-SHA256("refresh_<ms>")>
//     → refresh_csrf from <div id="1-name">
//  3. POST passport …/web/cookie/refresh (csrf, refresh_csrf, source=main_web, refresh_token)
//     → new cookies (Set-Cookie) + new refresh_token
//  4. verify nav isLogin (and mid) with the new cookies — only then persist
//  5. hot-swap the running bots (OnPersisted hook)
//  6. POST passport …/web/confirm/refresh (csrf=<new bili_jct>, refresh_token=<old>)
//     → invalidates the old cookie / refresh_token
//
// The old record stays in the DB untouched until step 4 passed; the write is a
// single compare-and-swap UPDATE. Until step 6 the old cookie still works, so
// any failure before it leaves the account exactly as it was. Cookie and token
// values never appear in logs or error messages.

const biliRefreshPublicKey = `-----BEGIN PUBLIC KEY-----
MIGfMA0GCSqGSIb3DQEBAQUAA4GNADCBiQKBgQDLgd2OAkcGVtoE3ThUREbio0Eg
Uc/prcajMKXvkCKFCWhJYJcLkcM2DKKcSeFpD/j6Boy538YXnR6VhcuUJOhH2x71
nzPjfdTcqMz7djHum0qSZA0AyCBDABUqCrfNgCiJ00Ra7GmRj+YCK1NJEuewlb40
JNrRuoEUXpabUzGB8QIDAQAB
-----END PUBLIC KEY-----`

const refreshUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0 Safari/537.36"

var refreshCSRFRe = regexp.MustCompile(`<div id="1-name">\s*([^<\s]+)\s*</div>`)

// RefreshEndpoints are the Bilibili URLs used by the refresh flow (overridable in tests).
type RefreshEndpoints struct {
	CookieInfo string // GET ?csrf=
	Correspond string // prefix; the hex path is appended
	Refresh    string
	Confirm    string
	Nav        string
}

// DefaultRefreshEndpoints are the production Bilibili endpoints.
var DefaultRefreshEndpoints = RefreshEndpoints{
	CookieInfo: "https://passport.bilibili.com/x/passport-login/web/cookie/info",
	Correspond: "https://www.bilibili.com/correspond/1/",
	Refresh:    "https://passport.bilibili.com/x/passport-login/web/cookie/refresh",
	Confirm:    "https://passport.bilibili.com/x/passport-login/web/confirm/refresh",
	Nav:        "https://api.bilibili.com/x/web-interface/nav",
}

// Refresh outcome statuses (also stored as refresh_status).
const (
	RefreshNotNeeded    = "not-needed"
	RefreshDone         = "refreshed"
	RefreshFailed       = "failed"
	RefreshNoToken      = "no-token"
	RefreshLoginInvalid = "login-invalid"
)

var refreshStepLabels = map[string]string{
	"load":         "读取账号记录失败",
	"cookie-info":  "查询是否需要刷新失败",
	"login":        "登录已失效",
	"no-token":     "缺少 refresh_token（该账号是旧版本扫码登录的）",
	"correspond":   "获取 refresh_csrf 失败",
	"refresh":      "调用刷新接口失败",
	"verify-new":   "新 Cookie 登录校验失败（旧 Cookie 保持不变）",
	"db-write":     "新 Cookie 写入数据库失败（旧 Cookie 保持不变）",
	"confirm":      "确认刷新（作废旧 token）失败；新 Cookie 已生效",
	"verify-final": "确认后登录校验失败",
}

// RefreshError carries the failed step. Detail never contains secrets.
type RefreshError struct {
	Step   string
	Detail string
}

func (e *RefreshError) Error() string {
	if e.Detail != "" {
		return e.Step + ": " + e.Detail
	}
	return e.Step
}

// Human returns a Chinese description of the failed step plus what to do.
func (e *RefreshError) Human() string {
	label := refreshStepLabels[e.Step]
	if label == "" {
		label = e.Step
	}
	if e.Detail != "" {
		label += "（" + e.Detail + "）"
	}
	switch e.Step {
	case "confirm":
		return label
	default:
		return label + "，请在管理面板重新扫码登录该账号"
	}
}

// LoginInvalid reports whether the error means the stored cookie no longer logs in.
func (e *RefreshError) LoginInvalid() bool { return e.Step == "login" }

// RefreshResult is the outcome of one refresh check.
type RefreshResult struct {
	Status string `json:"status"` // not-needed | refreshed
	UName  string `json:"uname,omitempty"`
	MID    int64  `json:"mid,omitempty"`
	// ConfirmErr is set when the new cookie is live but confirm/refresh failed
	// (the old token simply stays valid a while longer). Not fatal.
	ConfirmErr *RefreshError `json:"-"`
}

// Refresher runs the cookie refresh flow, one at a time per account.
type Refresher struct {
	store  *Store
	client *http.Client
	ep     RefreshEndpoints
	now    func() time.Time
	pubKey *rsa.PublicKey

	// OnPersisted is called after the new cookie has been verified and written
	// and BEFORE confirm/refresh invalidates the old one, so running bots and
	// websocket clients can switch over without ever using a dead cookie.
	OnPersisted func(acc *BiliAccount)

	mu    sync.Mutex
	locks map[int64]*sync.Mutex
}

// NewRefresher creates a Refresher. client/ep may be nil/zero for defaults.
func NewRefresher(store *Store, client *http.Client, ep *RefreshEndpoints) *Refresher {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	// Never follow redirects: Set-Cookie must be read from the first response,
	// and a redirect would resend our cookies to wherever it points.
	c := *client
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	e := DefaultRefreshEndpoints
	if ep != nil {
		e = *ep
	}
	block, _ := pem.Decode([]byte(biliRefreshPublicKey))
	k, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		panic("bili refresh public key: " + err.Error())
	}
	return &Refresher{store: store, client: &c, ep: e, now: time.Now, pubKey: k.(*rsa.PublicKey), locks: map[int64]*sync.Mutex{}}
}

func (r *Refresher) lock(id int64) func() {
	r.mu.Lock()
	m := r.locks[id]
	if m == nil {
		m = &sync.Mutex{}
		r.locks[id] = m
	}
	r.mu.Unlock()
	m.Lock()
	return m.Unlock
}

// CorrespondPath returns hex(RSA-OAEP-SHA256("refresh_<tsMs>")).
func (r *Refresher) CorrespondPath(tsMs int64) (string, error) {
	ct, err := rsa.EncryptOAEP(sha256.New(), rand.Reader, r.pubKey, []byte(fmt.Sprintf("refresh_%d", tsMs)), nil)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(ct), nil
}

type refreshCreds struct {
	sessdata, biliJCT string
	uid               int64
}

func (c refreshCreds) cookieHeader() string {
	h := "SESSDATA=" + c.sessdata + "; bili_jct=" + c.biliJCT
	if c.uid != 0 {
		h += fmt.Sprintf("; DedeUserID=%d", c.uid)
	}
	return h
}

func (r *Refresher) do(ctx context.Context, method, u string, form url.Values, c refreshCreds) (*http.Response, []byte, error) {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return nil, nil, errors.New("bad request")
	}
	req.Header.Set("User-Agent", refreshUA)
	req.Header.Set("Referer", "https://www.bilibili.com/")
	req.Header.Set("Origin", "https://www.bilibili.com")
	req.Header.Set("Cookie", c.cookieHeader())
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return nil, nil, errors.New(netErrKind(err)) // never echo the URL: it can carry csrf
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, nil, errors.New("读取响应失败")
	}
	return resp, b, nil
}

func netErrKind(err error) string {
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return "网络超时"
	}
	if errors.Is(err, context.Canceled) {
		return "已取消"
	}
	return "网络错误"
}

// apiData checks HTTP 200 + code 0 and returns data.
func apiData(resp *http.Response, b []byte, step string) (json.RawMessage, *RefreshError) {
	if resp.StatusCode != 200 {
		return nil, &RefreshError{step, fmt.Sprintf("HTTP %d", resp.StatusCode)}
	}
	var d struct {
		Code int             `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(b, &d); err != nil {
		return nil, &RefreshError{step, "响应不是 JSON"}
	}
	if d.Code != 0 {
		return nil, &RefreshError{step, fmt.Sprintf("code %d", d.Code)}
	}
	return d.Data, nil
}

// nav returns (uname, mid, isLogin, err) for the given cookies.
func (r *Refresher) nav(ctx context.Context, c refreshCreds) (string, int64, bool, error) {
	resp, b, err := r.do(ctx, "GET", r.ep.Nav, nil, c)
	if err != nil {
		return "", 0, false, err
	}
	if resp.StatusCode != 200 {
		return "", 0, false, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var d struct {
		Code int `json:"code"`
		Data struct {
			IsLogin bool   `json:"isLogin"`
			UName   string `json:"uname"`
			MID     int64  `json:"mid"`
		} `json:"data"`
	}
	if err := json.Unmarshal(b, &d); err != nil {
		return "", 0, false, errors.New("响应不是 JSON")
	}
	return d.Data.UName, d.Data.MID, d.Code == 0 && d.Data.IsLogin, nil
}

// Refresh checks the account with Bilibili and runs the refresh flow when
// cookie/info says so (or force). The outcome is recorded in the account's
// refresh_* columns. Concurrent calls for the same account are serialized; the
// second one re-reads the (possibly just refreshed) record.
func (r *Refresher) Refresh(ctx context.Context, id int64, force bool) (*RefreshResult, error) {
	defer r.lock(id)()
	res, rerr := r.refresh(ctx, id, force)
	ts := r.now().Format("2006-01-02 15:04:05")
	switch {
	case rerr == nil && res.ConfirmErr != nil:
		_ = r.store.SetBiliRefreshStatus(id, RefreshDone, res.ConfirmErr.Human(), ts)
	case rerr == nil:
		_ = r.store.SetBiliRefreshStatus(id, res.Status, "", ts)
	case rerr.Step == "load":
		// account gone: nothing to record
	case rerr.Step == "no-token":
		_ = r.store.SetBiliRefreshStatus(id, RefreshNoToken, rerr.Human(), ts)
	case rerr.LoginInvalid():
		_ = r.store.SetBiliRefreshStatus(id, RefreshLoginInvalid, rerr.Human(), ts)
	default:
		_ = r.store.SetBiliRefreshStatus(id, RefreshFailed, rerr.Human(), ts)
	}
	if rerr != nil {
		return nil, rerr
	}
	return res, nil
}

func (r *Refresher) refresh(ctx context.Context, id int64, force bool) (*RefreshResult, *RefreshError) {
	acc, err := r.store.GetBiliAccount(id)
	if err != nil || acc.SESSDATA == "" || acc.BiliJCT == "" {
		return nil, &RefreshError{Step: "load"}
	}
	old := refreshCreds{acc.SESSDATA, acc.BiliJCT, acc.UID}

	// 1. cookie/info — also tells us whether the cookie is logged in at all (-101).
	resp, b, err := r.do(ctx, "GET", r.ep.CookieInfo+"?csrf="+url.QueryEscape(old.biliJCT), nil, old)
	if err != nil {
		return nil, &RefreshError{"cookie-info", err.Error()}
	}
	data, rerr := apiData(resp, b, "cookie-info")
	if rerr != nil {
		if rerr.Detail == "code -101" {
			return nil, &RefreshError{Step: "login", Detail: "B站返回 -101 账号未登录"}
		}
		return nil, rerr
	}
	var info struct {
		Refresh bool `json:"refresh"`
	}
	_ = json.Unmarshal(data, &info)
	if !info.Refresh && !force {
		return &RefreshResult{Status: RefreshNotNeeded}, nil
	}
	if acc.RefreshToken == "" {
		return nil, &RefreshError{Step: "no-token"}
	}

	// 2. refresh_csrf
	path, err := r.CorrespondPath(r.now().UnixMilli())
	if err != nil {
		return nil, &RefreshError{"correspond", "RSA 加密失败"}
	}
	resp, b, err = r.do(ctx, "GET", r.ep.Correspond+path, nil, old)
	if err != nil {
		return nil, &RefreshError{"correspond", err.Error()}
	}
	if resp.StatusCode != 200 {
		return nil, &RefreshError{"correspond", fmt.Sprintf("HTTP %d", resp.StatusCode)}
	}
	m := refreshCSRFRe.FindSubmatch(b)
	if m == nil {
		return nil, &RefreshError{"correspond", "页面里没有 refresh_csrf"}
	}

	// 3. refresh
	resp, b, err = r.do(ctx, "POST", r.ep.Refresh, url.Values{
		"csrf":          {old.biliJCT},
		"refresh_csrf":  {string(m[1])},
		"source":        {"main_web"},
		"refresh_token": {acc.RefreshToken},
	}, old)
	if err != nil {
		return nil, &RefreshError{"refresh", err.Error()}
	}
	data, rerr = apiData(resp, b, "refresh")
	if rerr != nil {
		return nil, rerr
	}
	var rd struct {
		RefreshToken string `json:"refresh_token"`
	}
	_ = json.Unmarshal(data, &rd)
	got := map[string]string{}
	for _, ck := range resp.Cookies() {
		got[ck.Name] = ck.Value
	}
	var missing []string
	for _, k := range []string{"SESSDATA", "bili_jct"} {
		if got[k] == "" {
			missing = append(missing, k)
		}
	}
	if rd.RefreshToken == "" {
		missing = append(missing, "refresh_token")
	}
	if len(missing) > 0 {
		return nil, &RefreshError{"refresh", "响应缺少 " + strings.Join(missing, ",")}
	}
	nw := refreshCreds{got["SESSDATA"], got["bili_jct"], acc.UID}

	// 4. the new cookie must log in as the same user before we persist anything.
	uname, mid, ok, err := r.nav(ctx, nw)
	if err != nil {
		return nil, &RefreshError{"verify-new", err.Error()}
	}
	if !ok {
		return nil, &RefreshError{"verify-new", "isLogin=false"}
	}
	if acc.UID != 0 && mid != acc.UID {
		return nil, &RefreshError{"verify-new", "mid 与账号不一致"}
	}
	if err := r.store.ReplaceBiliCookies(id, old.sessdata, nw.sessdata, nw.biliJCT, rd.RefreshToken,
		r.now().Format("2006-01-02 15:04:05")); err != nil {
		return nil, &RefreshError{Step: "db-write"}
	}

	// 5. hot-swap running bots before the old cookie is invalidated.
	if r.OnPersisted != nil {
		if fresh, err := r.store.GetBiliAccount(id); err == nil {
			r.OnPersisted(fresh)
		}
	}
	res := &RefreshResult{Status: RefreshDone, UName: uname, MID: mid}

	// 6. confirm: invalidate the old refresh_token / cookie.
	resp, b, err = r.do(ctx, "POST", r.ep.Confirm, url.Values{
		"csrf":          {nw.biliJCT},
		"refresh_token": {acc.RefreshToken},
	}, nw)
	if err != nil {
		res.ConfirmErr = &RefreshError{"confirm", err.Error()}
		return res, nil
	}
	if _, rerr := apiData(resp, b, "confirm"); rerr != nil {
		res.ConfirmErr = rerr
		return res, nil
	}
	if _, _, ok, err := r.nav(ctx, nw); err == nil && !ok {
		return nil, &RefreshError{Step: "verify-final", Detail: "isLogin=false"}
	}
	return res, nil
}
