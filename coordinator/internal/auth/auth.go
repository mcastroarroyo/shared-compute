// Package auth adds OAuth sign-in (GitHub, Google), cookie sessions, and the
// per-account consumer key / provider token that the signed-in app (app.ayni-ai
// .com) uses. Postgres-only: without SC_DATABASE_URL and at least one OAuth
// client configured, New returns nil and every /auth route 404s.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/store"
)

const (
	sessionCookie = "ayni_session"
	stateCookie   = "ayni_oauth_state"
	sessionTTL    = 30 * 24 * time.Hour
)

// Config is the OAuth + cookie configuration (all from env).
type Config struct {
	GitHubClientID, GitHubSecret string
	GoogleClientID, GoogleSecret string
	CallbackBase                 string // e.g. https://api.ayni-ai.com
	AppURL                       string // e.g. https://app.ayni-ai.com (post-login redirect)
	CookieDomain                 string // e.g. .ayni-ai.com  ("" -> host-only)
	Secure                       bool   // Secure + SameSite=None cookies (cross-subdomain)
}

// User is an account.
type User struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatar_url"`
}

// Auth wires OAuth handlers and session lookups.
type Auth struct {
	pool *pgxpool.Pool
	main store.Store
	cfg  Config
	log  *slog.Logger
	http *http.Client
	prov map[string]bool // which providers are configured
}

// New connects a dedicated pool and returns an Auth, or nil if disabled.
func New(ctx context.Context, dbURL string, cfg Config, main store.Store, log *slog.Logger) (*Auth, error) {
	if dbURL == "" {
		return nil, nil
	}
	prov := map[string]bool{}
	if cfg.GitHubClientID != "" && cfg.GitHubSecret != "" {
		prov["github"] = true
	}
	if cfg.GoogleClientID != "" && cfg.GoogleSecret != "" {
		prov["google"] = true
	}
	if len(prov) == 0 {
		return nil, nil
	}
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		return nil, fmt.Errorf("auth pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("auth ping: %w", err)
	}
	if cfg.CallbackBase == "" {
		cfg.CallbackBase = "https://api.ayni-ai.com"
	}
	if cfg.AppURL == "" {
		cfg.AppURL = "https://app.ayni-ai.com"
	}
	return &Auth{
		pool: pool, main: main, cfg: cfg, log: log,
		http: &http.Client{Timeout: 15 * time.Second},
		prov: prov,
	}, nil
}

func (a *Auth) Close() {
	if a != nil && a.pool != nil {
		a.pool.Close()
	}
}

// Providers lists the configured sign-in methods (for the app's login screen).
func (a *Auth) Providers() []string {
	out := make([]string, 0, len(a.prov))
	for p := range a.prov {
		out = append(out, p)
	}
	return out
}

// Mount registers the /auth/* and /v1/me routes on mux.
func (a *Auth) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /auth/providers", a.handleProviders)
	mux.HandleFunc("GET /auth/{provider}", a.handleStart)
	mux.HandleFunc("GET /auth/{provider}/callback", a.handleCallback)
	mux.HandleFunc("POST /auth/logout", a.handleLogout)
	mux.HandleFunc("GET /v1/me", a.handleMe)
	mux.HandleFunc("GET /v1/me/earnings", a.handleMyEarnings)
	mux.HandleFunc("POST /v1/me/pair", a.handlePairCreate)
}

func (a *Auth) handleMyEarnings(w http.ResponseWriter, r *http.Request) {
	u, ok := a.SessionUser(r)
	if !ok {
		writeJSON(w, 401, map[string]any{"error": "not signed in"})
		return
	}
	owed, jobs, devices, err := a.EarningsForUser(r.Context(), u.ID)
	if err != nil {
		a.log.Warn("earnings for user", "err", err)
		writeJSON(w, 200, map[string]any{"owed_usd": 0, "jobs": 0, "devices": 0})
		return
	}
	writeJSON(w, 200, map[string]any{
		"owed_usd": float64(owed) / 1e6, "jobs": jobs, "devices": devices,
	})
}

func (a *Auth) handleProviders(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, 200, map[string]any{"providers": a.Providers()})
}

// --- OAuth start ---

func (a *Auth) handleStart(w http.ResponseWriter, r *http.Request) {
	p := r.PathValue("provider")
	if !a.prov[p] {
		http.Error(w, "unknown provider", http.StatusNotFound)
		return
	}
	state := randToken()
	http.SetCookie(w, a.cookie(stateCookie, state, 10*time.Minute))
	var auURL string
	switch p {
	case "github":
		auURL = "https://github.com/login/oauth/authorize?" + url.Values{
			"client_id":    {a.cfg.GitHubClientID},
			"redirect_uri": {a.cfg.CallbackBase + "/auth/github/callback"},
			"scope":        {"read:user user:email"},
			"state":        {state},
		}.Encode()
	case "google":
		auURL = "https://accounts.google.com/o/oauth2/v2/auth?" + url.Values{
			"client_id":     {a.cfg.GoogleClientID},
			"redirect_uri":  {a.cfg.CallbackBase + "/auth/google/callback"},
			"response_type": {"code"},
			"scope":         {"openid email profile"},
			"state":         {state},
		}.Encode()
	}
	http.Redirect(w, r, auURL, http.StatusFound)
}

// --- OAuth callback ---

func (a *Auth) handleCallback(w http.ResponseWriter, r *http.Request) {
	p := r.PathValue("provider")
	if !a.prov[p] {
		http.Error(w, "unknown provider", http.StatusNotFound)
		return
	}
	if c, err := r.Cookie(stateCookie); err != nil || c.Value == "" || c.Value != r.URL.Query().Get("state") {
		http.Error(w, "bad oauth state", http.StatusBadRequest)
		return
	}
	http.SetCookie(w, a.cookie(stateCookie, "", -time.Hour))
	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "missing code", http.StatusBadRequest)
		return
	}

	var (
		providerUserID, email, name, avatar string
		err                                 error
	)
	switch p {
	case "github":
		providerUserID, email, name, avatar, err = a.githubIdentity(r.Context(), code)
	case "google":
		providerUserID, email, name, avatar, err = a.googleIdentity(r.Context(), code)
	}
	if err != nil {
		a.log.Warn("oauth callback failed", "provider", p, "err", err)
		http.Error(w, "sign-in failed", http.StatusBadGateway)
		return
	}
	if email == "" {
		http.Error(w, "the provider did not return a verified email", http.StatusBadRequest)
		return
	}

	u, err := a.upsertUser(r.Context(), p, providerUserID, email, name, avatar)
	if err != nil {
		a.log.Error("upsert user", "err", err)
		http.Error(w, "account error", http.StatusInternalServerError)
		return
	}
	if err := a.ensureUserCredentials(r.Context(), u.ID, u.Email); err != nil {
		a.log.Error("mint user credentials", "err", err)
	}

	raw := randToken()
	if err := a.createSession(r.Context(), raw, u.ID); err != nil {
		http.Error(w, "session error", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, a.cookie(sessionCookie, raw, sessionTTL))
	http.Redirect(w, r, a.cfg.AppURL+"/dashboard/", http.StatusFound)
}

func (a *Auth) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil && c.Value != "" {
		_, _ = a.pool.Exec(r.Context(), `DELETE FROM sessions WHERE token_hash=$1`, hashToken(c.Value))
	}
	http.SetCookie(w, a.cookie(sessionCookie, "", -time.Hour))
	writeJSON(w, 200, map[string]any{"ok": true})
}

// --- /v1/me ---

func (a *Auth) handleMe(w http.ResponseWriter, r *http.Request) {
	u, ok := a.SessionUser(r)
	if !ok {
		writeJSON(w, 401, map[string]any{"error": "not signed in"})
		return
	}
	keyID, _ := a.keyIDForUser(r.Context(), u.ID)
	provTok, _ := a.providerTokenForUser(r.Context(), u.ID)
	var balance int64
	if keyID != "" {
		balance, _ = a.main.CreditBalance(r.Context(), keyID)
	}
	writeJSON(w, 200, map[string]any{
		"user":           u,
		"api_key_id":     keyID,
		"provider_token": provTok, // the app shows this in the "share your computer" command
		"credit_usd":     float64(balance) / 1e6,
	})
}

// --- session helpers (used by api.withAuth too) ---

// SessionUser resolves the session cookie to a user.
func (a *Auth) SessionUser(r *http.Request) (User, bool) {
	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" {
		return User{}, false
	}
	var u User
	row := a.pool.QueryRow(r.Context(), `
		SELECT u.id, u.email, u.name, u.avatar_url
		FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token_hash=$1 AND s.expires_at > now()`, hashToken(c.Value))
	if err := row.Scan(&u.ID, &u.Email, &u.Name, &u.AvatarURL); err != nil {
		return User{}, false
	}
	return u, true
}

// KeyIDForRequest maps a signed-in request to its consumer key id (for withAuth).
func (a *Auth) KeyIDForRequest(r *http.Request) (string, bool) {
	u, ok := a.SessionUser(r)
	if !ok {
		return "", false
	}
	return a.keyIDForUser(r.Context(), u.ID)
}

// --- provider-token linkage (used by wshub) ---

// ValidProviderToken reports whether a raw token is a live per-account token.
func (a *Auth) ValidProviderToken(ctx context.Context, raw string) bool {
	_, ok := a.userForProviderToken(ctx, raw)
	return ok
}

// LinkProviderDevice records which account owns a connected device, given the
// registration token it presented.
func (a *Auth) LinkProviderDevice(ctx context.Context, raw, staticPKb64 string) {
	uid, ok := a.userForProviderToken(ctx, raw)
	if !ok {
		return
	}
	_, err := a.pool.Exec(ctx, `
		INSERT INTO provider_owners (static_pk, user_id) VALUES ($1,$2)
		ON CONFLICT (static_pk) DO UPDATE SET user_id=EXCLUDED.user_id, last_seen=now()`,
		staticPKb64, uid)
	if err != nil {
		a.log.Warn("link provider device", "err", err)
	}
}

// EarningsForUser sums this account's connected devices' accrued, unpaid micros.
func (a *Auth) EarningsForUser(ctx context.Context, userID string) (owedMicros int64, jobs int, devices int, err error) {
	err = a.pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(e.provider_micros),0), COALESCE(SUM(e.jobs),0), COUNT(DISTINCT o.static_pk)
		FROM provider_owners o
		LEFT JOIN (
		  SELECT static_pk, SUM(provider_micros) provider_micros, COUNT(*) jobs
		  FROM provider_earnings WHERE state='accrued' GROUP BY static_pk
		) e ON e.static_pk = o.static_pk
		WHERE o.user_id=$1`, userID).Scan(&owedMicros, &jobs, &devices)
	return
}

// AdminUser is an account as the operator console sees it: the public profile
// plus join facts. No credentials are included.
type AdminUser struct {
	User
	CreatedAt time.Time `json:"created_at"`
	APIKeyID  string    `json:"api_key_id"`
	Devices   int       `json:"devices"` // provider identities ever linked to the account
}

// ListUsers returns every account, newest first.
func (a *Auth) ListUsers(ctx context.Context) ([]AdminUser, error) {
	rows, err := a.pool.Query(ctx, `
		SELECT u.id, u.email, u.name, u.avatar_url, u.created_at, COALESCE(k.key_id,''),
		       (SELECT count(*) FROM provider_owners o WHERE o.user_id = u.id)
		FROM users u
		LEFT JOIN user_api_keys k ON k.user_id = u.id
		ORDER BY u.created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AdminUser
	for rows.Next() {
		var u AdminUser
		if err := rows.Scan(&u.ID, &u.Email, &u.Name, &u.AvatarURL, &u.CreatedAt, &u.APIKeyID, &u.Devices); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// OwnersByStaticPK maps every linked provider identity to the account that
// registered it, so the console can show who a device belongs to.
func (a *Auth) OwnersByStaticPK(ctx context.Context) (map[string]User, error) {
	rows, err := a.pool.Query(ctx, `
		SELECT o.static_pk, u.id, u.email, u.name
		FROM provider_owners o JOIN users u ON u.id = o.user_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]User{}
	for rows.Next() {
		var pk string
		var u User
		if err := rows.Scan(&pk, &u.ID, &u.Email, &u.Name); err != nil {
			return nil, err
		}
		out[pk] = u
	}
	return out, rows.Err()
}

// --- internals ---

func (a *Auth) upsertUser(ctx context.Context, provider, puid, email, name, avatar string) (User, error) {
	tx, err := a.pool.Begin(ctx)
	if err != nil {
		return User{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var uid string
	err = tx.QueryRow(ctx, `SELECT user_id FROM oauth_identities WHERE provider=$1 AND provider_user_id=$2`,
		provider, puid).Scan(&uid)
	switch {
	case err == nil:
		// known identity — refresh profile
		_, _ = tx.Exec(ctx, `UPDATE users SET name=$2, avatar_url=$3 WHERE id=$1`, uid, name, avatar)
	case errors.Is(err, pgx.ErrNoRows):
		// match on email, else create
		if e := tx.QueryRow(ctx, `SELECT id FROM users WHERE lower(email)=lower($1)`, email).Scan(&uid); e != nil {
			uid = "usr_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:20]
			if _, e2 := tx.Exec(ctx, `INSERT INTO users (id,email,name,avatar_url) VALUES ($1,$2,$3,$4)`,
				uid, email, name, avatar); e2 != nil {
				return User{}, e2
			}
		}
		if _, e := tx.Exec(ctx, `INSERT INTO oauth_identities (provider,provider_user_id,user_id) VALUES ($1,$2,$3)`,
			provider, puid, uid); e != nil {
			return User{}, e
		}
	default:
		return User{}, err
	}
	var u User
	if e := tx.QueryRow(ctx, `SELECT id,email,name,avatar_url FROM users WHERE id=$1`, uid).
		Scan(&u.ID, &u.Email, &u.Name, &u.AvatarURL); e != nil {
		return User{}, e
	}
	return u, tx.Commit(ctx)
}

// ensureUserCredentials mints (once) the consumer key + provider token for a user.
func (a *Auth) ensureUserCredentials(ctx context.Context, userID, email string) error {
	var keyID string
	err := a.pool.QueryRow(ctx, `SELECT key_id FROM user_api_keys WHERE user_id=$1`, userID).Scan(&keyID)
	if errors.Is(err, pgx.ErrNoRows) {
		id, _, e := a.main.CreateConsumerKey(ctx, "app:"+email)
		if e != nil {
			return e
		}
		if _, e := a.pool.Exec(ctx, `INSERT INTO user_api_keys (user_id,key_id) VALUES ($1,$2)
			ON CONFLICT (user_id) DO NOTHING`, userID, id); e != nil {
			return e
		}
	} else if err != nil {
		return err
	}
	if _, e := a.pool.Exec(ctx, `INSERT INTO user_provider_tokens (user_id, token) VALUES ($1,$2)
		ON CONFLICT (user_id) DO NOTHING`, userID, "sc_prov_"+randToken()); e != nil {
		return e
	}
	return nil
}

func (a *Auth) keyIDForUser(ctx context.Context, userID string) (string, bool) {
	var k string
	if err := a.pool.QueryRow(ctx, `SELECT key_id FROM user_api_keys WHERE user_id=$1`, userID).Scan(&k); err != nil {
		return "", false
	}
	return k, true
}

func (a *Auth) providerTokenForUser(ctx context.Context, userID string) (string, bool) {
	var raw string
	if err := a.pool.QueryRow(ctx, `SELECT token FROM user_provider_tokens WHERE user_id=$1`, userID).Scan(&raw); err != nil {
		return "", false
	}
	return raw, true
}

func (a *Auth) userForProviderToken(ctx context.Context, raw string) (string, bool) {
	var uid string
	if err := a.pool.QueryRow(ctx, `SELECT user_id FROM user_provider_tokens WHERE token=$1`,
		strings.TrimSpace(raw)).Scan(&uid); err != nil {
		return "", false
	}
	return uid, true
}

func (a *Auth) createSession(ctx context.Context, raw, userID string) error {
	_, err := a.pool.Exec(ctx, `INSERT INTO sessions (token_hash,user_id,expires_at) VALUES ($1,$2,$3)`,
		hashToken(raw), userID, time.Now().Add(sessionTTL))
	return err
}

// --- OAuth identity fetch (hand-rolled; no extra deps) ---

func (a *Auth) githubIdentity(ctx context.Context, code string) (id, email, name, avatar string, err error) {
	form := url.Values{
		"client_id":     {a.cfg.GitHubClientID},
		"client_secret": {a.cfg.GitHubSecret},
		"code":          {code},
		"redirect_uri":  {a.cfg.CallbackBase + "/auth/github/callback"},
	}
	var tok struct {
		AccessToken string `json:"access_token"`
	}
	if err = a.postForm(ctx, "https://github.com/login/oauth/access_token", form, &tok); err != nil {
		return
	}
	if tok.AccessToken == "" {
		err = errors.New("github: empty access token")
		return
	}
	var user struct {
		ID     json.Number `json:"id"`
		Login  string      `json:"login"`
		Name   string      `json:"name"`
		Email  string      `json:"email"`
		Avatar string      `json:"avatar_url"`
	}
	if err = a.getJSON(ctx, "https://api.github.com/user", tok.AccessToken, &user); err != nil {
		return
	}
	email = user.Email
	if email == "" {
		var emails []struct {
			Email    string `json:"email"`
			Primary  bool   `json:"primary"`
			Verified bool   `json:"verified"`
		}
		if e := a.getJSON(ctx, "https://api.github.com/user/emails", tok.AccessToken, &emails); e == nil {
			for _, em := range emails {
				if em.Primary && em.Verified {
					email = em.Email
					break
				}
			}
		}
	}
	name = user.Name
	if name == "" {
		name = user.Login
	}
	return user.ID.String(), email, name, user.Avatar, nil
}

func (a *Auth) googleIdentity(ctx context.Context, code string) (id, email, name, avatar string, err error) {
	form := url.Values{
		"client_id":     {a.cfg.GoogleClientID},
		"client_secret": {a.cfg.GoogleSecret},
		"code":          {code},
		"grant_type":    {"authorization_code"},
		"redirect_uri":  {a.cfg.CallbackBase + "/auth/google/callback"},
	}
	var tok struct {
		AccessToken string `json:"access_token"`
	}
	if err = a.postForm(ctx, "https://oauth2.googleapis.com/token", form, &tok); err != nil {
		return
	}
	if tok.AccessToken == "" {
		err = errors.New("google: empty access token")
		return
	}
	var user struct {
		Sub           string `json:"sub"`
		ID            string `json:"id"`
		Email         string `json:"email"`
		VerifiedEmail bool   `json:"verified_email"`
		Name          string `json:"name"`
		Picture       string `json:"picture"`
	}
	if err = a.getJSON(ctx, "https://www.googleapis.com/oauth2/v2/userinfo", tok.AccessToken, &user); err != nil {
		return
	}
	uid := user.Sub
	if uid == "" {
		uid = user.ID
	}
	return uid, user.Email, user.Name, user.Picture, nil
}

func (a *Auth) postForm(ctx context.Context, endpoint string, form url.Values, out any) error {
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	res, err := a.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode/100 != 2 {
		return fmt.Errorf("%s: %d %s", endpoint, res.StatusCode, string(body))
	}
	return json.Unmarshal(body, out)
}

func (a *Auth) getJSON(ctx context.Context, endpoint, bearer string, out any) error {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	req.Header.Set("Authorization", "Bearer "+bearer)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "ayni-coordinator")
	res, err := a.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode/100 != 2 {
		return fmt.Errorf("%s: %d %s", endpoint, res.StatusCode, string(body))
	}
	return json.Unmarshal(body, out)
}

// --- misc ---

func (a *Auth) cookie(name, value string, ttl time.Duration) *http.Cookie {
	c := &http.Cookie{
		Name: name, Value: value, Path: "/",
		HttpOnly: true, Secure: a.cfg.Secure,
		Expires: time.Now().Add(ttl),
	}
	if a.cfg.CookieDomain != "" {
		c.Domain = a.cfg.CookieDomain
	}
	if a.cfg.Secure {
		c.SameSite = http.SameSiteNoneMode // cross-subdomain app -> api
	} else {
		c.SameSite = http.SameSiteLaxMode
	}
	if ttl < 0 {
		c.MaxAge = -1
	}
	return c
}

func randToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func hashToken(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
