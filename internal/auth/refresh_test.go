package auth

import (
	"context"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

const (
	oldSess, oldJCT, oldRT = "OLD-SESSDATA-secret", "OLD-JCT-secret", "OLD-RT-secret"
	newSess, newJCT, newRT = "NEW-SESSDATA-secret", "NEW-JCT-secret", "NEW-RT-secret"
	testUID                = int64(4242)
)

// fakeBili is an in-memory Bilibili passport: it knows which SESSDATA values
// are logged in and whether the current one needs a refresh.
type fakeBili struct {
	t  *testing.T
	mu sync.Mutex

	loggedIn      map[string]bool // SESSDATA → isLogin
	needRefresh   map[string]bool // SESSDATA → cookie/info refresh
	infoCode      int             // cookie/info code override (e.g. -101)
	correspondCSR string          // refresh_csrf served by correspond page ("" → missing)
	refreshCode   int
	navMID        int64
	newLoginOK    bool // nav isLogin for the new SESSDATA
	confirmCode   int

	refreshCalls, confirmCalls       int32
	lastRefreshForm, lastConfirmForm map[string]string
	correspondPath                   string
	persistedBeforeConfirm           bool
	persisted                        atomic.Bool
}

func newFake(t *testing.T) *fakeBili {
	return &fakeBili{
		t:             t,
		loggedIn:      map[string]bool{oldSess: true},
		needRefresh:   map[string]bool{oldSess: true},
		correspondCSR: "csrf-from-page",
		navMID:        testUID,
		newLoginOK:    true,
	}
}

func sess(r *http.Request) string {
	if c, err := r.Cookie("SESSDATA"); err == nil {
		return c.Value
	}
	return ""
}

func form(r *http.Request) map[string]string {
	_ = r.ParseForm()
	m := map[string]string{}
	for k := range r.PostForm {
		m[k] = r.PostForm.Get(k)
	}
	return m
}

func (f *fakeBili) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/info", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.infoCode != 0 {
			fmt.Fprintf(w, `{"code":%d,"message":"账号未登录"}`, f.infoCode)
			return
		}
		s := sess(r)
		if !f.loggedIn[s] {
			fmt.Fprint(w, `{"code":-101,"message":"账号未登录"}`)
			return
		}
		fmt.Fprintf(w, `{"code":0,"data":{"refresh":%v,"timestamp":1}}`, f.needRefresh[s])
	})
	mux.HandleFunc("/correspond/", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.correspondPath = strings.TrimPrefix(r.URL.Path, "/correspond/")
		csr := f.correspondCSR
		f.mu.Unlock()
		if csr == "" {
			fmt.Fprint(w, `<html><body>nothing</body></html>`)
			return
		}
		fmt.Fprintf(w, `<html><div id="1-name">%s</div></html>`, csr)
	})
	mux.HandleFunc("/refresh", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&f.refreshCalls, 1)
		f.mu.Lock()
		defer f.mu.Unlock()
		f.lastRefreshForm = form(r)
		if f.refreshCode != 0 {
			fmt.Fprintf(w, `{"code":%d}`, f.refreshCode)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "SESSDATA", Value: newSess})
		http.SetCookie(w, &http.Cookie{Name: "bili_jct", Value: newJCT})
		http.SetCookie(w, &http.Cookie{Name: "DedeUserID", Value: fmt.Sprint(testUID)})
		f.loggedIn[newSess] = f.newLoginOK
		fmt.Fprintf(w, `{"code":0,"data":{"status":0,"refresh_token":%q}}`, newRT)
	})
	mux.HandleFunc("/confirm", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&f.confirmCalls, 1)
		f.mu.Lock()
		defer f.mu.Unlock()
		f.lastConfirmForm = form(r)
		f.persistedBeforeConfirm = f.persisted.Load()
		if f.confirmCode != 0 {
			fmt.Fprintf(w, `{"code":%d}`, f.confirmCode)
			return
		}
		delete(f.loggedIn, oldSess)
		fmt.Fprint(w, `{"code":0}`)
	})
	mux.HandleFunc("/nav", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		s := sess(r)
		if !f.loggedIn[s] {
			fmt.Fprint(w, `{"code":-101,"data":{"isLogin":false}}`)
			return
		}
		fmt.Fprintf(w, `{"code":0,"data":{"isLogin":true,"uname":"tester","mid":%d}}`, f.navMID)
	})
	return mux
}

func setup(t *testing.T, refreshToken string) (*Store, *Refresher, *fakeBili, int64) {
	t.Helper()
	f := newFake(t)
	srv := httptest.NewServer(f.handler())
	t.Cleanup(srv.Close)
	st, err := NewStore(filepath.Join(t.TempDir(), "users.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	acc, err := st.SaveBiliAccount("bot1", oldSess, oldJCT, refreshToken, testUID, 20, "")
	if err != nil {
		t.Fatal(err)
	}
	r := NewRefresher(st, srv.Client(), &RefreshEndpoints{
		CookieInfo: srv.URL + "/info",
		Correspond: srv.URL + "/correspond/",
		Refresh:    srv.URL + "/refresh",
		Confirm:    srv.URL + "/confirm",
		Nav:        srv.URL + "/nav",
	})
	r.OnPersisted = func(*BiliAccount) { f.persisted.Store(true) }
	return st, r, f, acc.ID
}

func mustGet(t *testing.T, st *Store, id int64) *BiliAccount {
	t.Helper()
	a, err := st.GetBiliAccount(id)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func assertNoSecrets(t *testing.T, texts ...string) {
	t.Helper()
	for _, s := range texts {
		for _, sec := range []string{oldSess, oldJCT, oldRT, newSess, newJCT, newRT} {
			if strings.Contains(s, sec) {
				t.Fatalf("secret leaked into %q", s)
			}
		}
	}
}

func TestRefreshNotNeeded(t *testing.T) {
	st, r, f, id := setup(t, oldRT)
	f.needRefresh[oldSess] = false
	res, err := r.Refresh(context.Background(), id, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != RefreshNotNeeded || f.refreshCalls != 0 {
		t.Fatalf("status=%s refreshCalls=%d", res.Status, f.refreshCalls)
	}
	a := mustGet(t, st, id)
	if a.SESSDATA != oldSess || a.RefreshStatus != RefreshNotNeeded || a.RefreshCheckedAt == "" {
		t.Fatalf("unexpected record: status=%s checked=%q", a.RefreshStatus, a.RefreshCheckedAt)
	}
}

func TestRefreshFullFlow(t *testing.T) {
	st, r, f, id := setup(t, oldRT)
	res, err := r.Refresh(context.Background(), id, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != RefreshDone || res.MID != testUID || res.ConfirmErr != nil {
		t.Fatalf("res=%+v", res)
	}
	want := map[string]string{"csrf": oldJCT, "refresh_csrf": "csrf-from-page", "source": "main_web", "refresh_token": oldRT}
	for k, v := range want {
		if f.lastRefreshForm[k] != v {
			t.Errorf("refresh form %s mismatch", k)
		}
	}
	if f.lastConfirmForm["csrf"] != newJCT || f.lastConfirmForm["refresh_token"] != oldRT {
		t.Errorf("confirm must use new bili_jct and OLD refresh_token")
	}
	if !f.persistedBeforeConfirm {
		t.Errorf("bots must be hot-swapped before confirm invalidates the old cookie")
	}
	if b, err := hex.DecodeString(f.correspondPath); err != nil || len(b) != 128 {
		t.Errorf("correspond path is not hex RSA-1024 ciphertext (len %d)", len(f.correspondPath))
	}
	a := mustGet(t, st, id)
	if a.SESSDATA != newSess || a.BiliJCT != newJCT || a.RefreshToken != newRT || !a.Valid {
		t.Fatalf("new credentials not persisted")
	}
	if a.RefreshStatus != RefreshDone || a.RefreshedAt == "" || a.RefreshError != "" {
		t.Fatalf("status=%s refreshed_at=%q err=%q", a.RefreshStatus, a.RefreshedAt, a.RefreshError)
	}
	// Second check: new cookie does not need a refresh.
	res, err = r.Refresh(context.Background(), id, false)
	if err != nil || res.Status != RefreshNotNeeded {
		t.Fatalf("second check: %v %+v", err, res)
	}
}

func TestRefreshForceWhenNotNeeded(t *testing.T) {
	_, r, f, id := setup(t, oldRT)
	f.needRefresh[oldSess] = false
	res, err := r.Refresh(context.Background(), id, true)
	if err != nil || res.Status != RefreshDone || f.refreshCalls != 1 {
		t.Fatalf("err=%v res=%+v calls=%d", err, res, f.refreshCalls)
	}
}

func TestRefreshVerifyNewFailsKeepsOld(t *testing.T) {
	st, r, f, id := setup(t, oldRT)
	f.newLoginOK = false
	_, err := r.Refresh(context.Background(), id, false)
	rerr, ok := err.(*RefreshError)
	if !ok || rerr.Step != "verify-new" {
		t.Fatalf("want verify-new, got %v", err)
	}
	if f.confirmCalls != 0 || f.persisted.Load() {
		t.Fatalf("confirm/hot-swap must not run when the new cookie fails verification")
	}
	a := mustGet(t, st, id)
	if a.SESSDATA != oldSess || a.BiliJCT != oldJCT || a.RefreshToken != oldRT {
		t.Fatalf("old record must be kept")
	}
	if a.RefreshStatus != RefreshFailed || !strings.Contains(a.RefreshError, "重新扫码") {
		t.Fatalf("status=%s err=%q", a.RefreshStatus, a.RefreshError)
	}
	assertNoSecrets(t, err.Error(), rerr.Human(), a.RefreshError)
}

func TestRefreshWrongMIDRejected(t *testing.T) {
	st, r, f, id := setup(t, oldRT)
	f.navMID = 9999
	_, err := r.Refresh(context.Background(), id, false)
	if rerr, ok := err.(*RefreshError); !ok || rerr.Step != "verify-new" {
		t.Fatalf("want verify-new, got %v", err)
	}
	if mustGet(t, st, id).SESSDATA != oldSess {
		t.Fatal("old record must be kept")
	}
}

func TestRefreshStepFailures(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*fakeBili)
		step string
	}{
		{"correspond-missing", func(f *fakeBili) { f.correspondCSR = "" }, "correspond"},
		{"refresh-code", func(f *fakeBili) { f.refreshCode = 86095 }, "refresh"},
		{"info-error", func(f *fakeBili) { f.infoCode = -400 }, "cookie-info"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			st, r, f, id := setup(t, oldRT)
			c.mut(f)
			_, err := r.Refresh(context.Background(), id, false)
			rerr, ok := err.(*RefreshError)
			if !ok || rerr.Step != c.step {
				t.Fatalf("want %s, got %v", c.step, err)
			}
			a := mustGet(t, st, id)
			if a.SESSDATA != oldSess || f.confirmCalls != 0 {
				t.Fatal("old record must be kept, no confirm")
			}
			assertNoSecrets(t, err.Error(), rerr.Human(), a.RefreshError)
		})
	}
}

func TestRefreshLoginInvalid(t *testing.T) {
	st, r, f, id := setup(t, oldRT)
	delete(f.loggedIn, oldSess)
	_, err := r.Refresh(context.Background(), id, false)
	rerr, ok := err.(*RefreshError)
	if !ok || !rerr.LoginInvalid() {
		t.Fatalf("want login invalid, got %v", err)
	}
	if a := mustGet(t, st, id); a.RefreshStatus != RefreshLoginInvalid {
		t.Fatalf("status=%s", a.RefreshStatus)
	}
}

func TestRefreshNoToken(t *testing.T) {
	st, r, f, id := setup(t, "")
	// Not needed yet: fine even without a token.
	f.needRefresh[oldSess] = false
	if res, err := r.Refresh(context.Background(), id, false); err != nil || res.Status != RefreshNotNeeded {
		t.Fatalf("err=%v", err)
	}
	if mustGet(t, st, id).HasRefreshToken {
		t.Fatal("HasRefreshToken should be false")
	}
	f.needRefresh[oldSess] = true
	_, err := r.Refresh(context.Background(), id, false)
	if rerr, ok := err.(*RefreshError); !ok || rerr.Step != "no-token" {
		t.Fatalf("want no-token, got %v", err)
	}
	if f.refreshCalls != 0 {
		t.Fatal("must not call refresh without a token")
	}
	if a := mustGet(t, st, id); a.RefreshStatus != RefreshNoToken {
		t.Fatalf("status=%s", a.RefreshStatus)
	}
}

func TestRefreshConfirmFailureKeepsNewCookie(t *testing.T) {
	st, r, f, id := setup(t, oldRT)
	f.confirmCode = -111
	res, err := r.Refresh(context.Background(), id, false)
	if err != nil || res.Status != RefreshDone || res.ConfirmErr == nil || res.ConfirmErr.Step != "confirm" {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	a := mustGet(t, st, id)
	if a.SESSDATA != newSess || a.RefreshToken != newRT {
		t.Fatal("verified new cookie must stay persisted")
	}
	if a.RefreshStatus != RefreshDone || a.RefreshError == "" {
		t.Fatalf("status=%s err=%q", a.RefreshStatus, a.RefreshError)
	}
}

func TestRefreshConcurrentSerialized(t *testing.T) {
	st, r, f, id := setup(t, oldRT)
	var wg sync.WaitGroup
	errs := make(chan error, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := r.Refresh(context.Background(), id, false)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if f.refreshCalls != 1 || f.confirmCalls != 1 {
		t.Fatalf("refresh ran %d times, confirm %d times; want 1/1", f.refreshCalls, f.confirmCalls)
	}
	if mustGet(t, st, id).SESSDATA != newSess {
		t.Fatal("new cookie not persisted")
	}
}

func TestReplaceBiliCookiesCAS(t *testing.T) {
	st, _, _, id := setup(t, oldRT)
	// Simulate a QR re-scan between read and write.
	if _, err := st.SaveBiliAccount("bot1", "RESCAN", "J", "T", testUID, 20, ""); err != nil {
		t.Fatal(err)
	}
	if err := st.ReplaceBiliCookies(id, oldSess, newSess, newJCT, newRT, "now"); err == nil {
		t.Fatal("CAS must fail when the row changed")
	}
	if mustGet(t, st, id).SESSDATA != "RESCAN" {
		t.Fatal("re-scanned login must win")
	}
}

func TestPollQRCodeCapturesRefreshToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "SESSDATA", Value: oldSess})
		http.SetCookie(w, &http.Cookie{Name: "bili_jct", Value: oldJCT})
		http.SetCookie(w, &http.Cookie{Name: "DedeUserID", Value: "4242"})
		fmt.Fprintf(w, `{"code":0,"data":{"code":0,"message":"","url":"x","refresh_token":%q,"timestamp":1}}`, oldRT)
	}))
	defer srv.Close()
	orig := qrPollURL
	qrPollURL = srv.URL
	defer func() { qrPollURL = orig }()
	res, err := PollQRCode("k")
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "confirmed" || res.RefreshToken != oldRT || res.SESSDATA != oldSess || res.UID != 4242 {
		t.Fatalf("poll result missing fields")
	}
}

func TestMigrateOldSchemaAndPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.db")
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE bili_accounts (
		id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL, sessdata TEXT NOT NULL, bili_jct TEXT NOT NULL,
		uid INTEGER NOT NULL DEFAULT 0, danmaku_max INTEGER NOT NULL DEFAULT 20,
		created_at DATETIME NOT NULL DEFAULT (datetime('now')), expires_at TEXT, valid INTEGER NOT NULL DEFAULT 1);
		INSERT INTO bili_accounts (name, sessdata, bili_jct, uid) VALUES ('legacy', 's', 'j', 7);`)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	os.Chmod(path, 0o644)

	st, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	accs, err := st.ListBiliAccounts()
	if err != nil || len(accs) != 1 || accs[0].Name != "legacy" || accs[0].HasRefreshToken {
		t.Fatalf("legacy row not preserved: %v %+v", err, accs)
	}
	sums, err := st.ListBiliAccountSummaries()
	if err != nil || len(sums) != 1 || sums[0].HasRefreshToken {
		t.Fatalf("summaries: %v", err)
	}
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("users.db must be 0600, got %v", fi.Mode().Perm())
	}
	// Re-opening must be idempotent.
	st2, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	st2.Close()
}
