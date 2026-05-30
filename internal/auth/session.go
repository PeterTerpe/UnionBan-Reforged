package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	SessionCookieName = "meshban_web_session"
	SessionTTL        = 12 * time.Hour
)

type LoginLimiter struct {
	mu          sync.Mutex
	failures    map[string]loginFailure
	maxFailures int
	window      time.Duration
	lockout     time.Duration
}

type loginFailure struct {
	Count       int
	FirstFailed time.Time
	LockedUntil time.Time
}

func NewLoginLimiter(maxFailures int, window time.Duration, lockout time.Duration) *LoginLimiter {
	return &LoginLimiter{
		failures:    make(map[string]loginFailure),
		maxFailures: maxFailures,
		window:      window,
		lockout:     lockout,
	}
}

func (l *LoginLimiter) IsLocked(r *http.Request) (time.Duration, bool) {
	key := clientKey(r)

	l.mu.Lock()
	defer l.mu.Unlock()

	record, ok := l.failures[key]
	if !ok {
		return 0, false
	}

	now := time.Now()

	if !record.LockedUntil.IsZero() && now.Before(record.LockedUntil) {
		return time.Until(record.LockedUntil), true
	}

	if !record.LockedUntil.IsZero() && now.After(record.LockedUntil) {
		delete(l.failures, key)
		return 0, false
	}

	return 0, false
}

func (l *LoginLimiter) RecordFailure(r *http.Request) (time.Duration, bool) {
	key := clientKey(r)

	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	record := l.failures[key]

	if record.FirstFailed.IsZero() || now.Sub(record.FirstFailed) > l.window {
		record = loginFailure{
			Count:       1,
			FirstFailed: now,
		}
	} else {
		record.Count++
	}

	if record.Count >= l.maxFailures {
		record.LockedUntil = now.Add(l.lockout)
	}

	l.failures[key] = record

	if !record.LockedUntil.IsZero() && now.Before(record.LockedUntil) {
		return time.Until(record.LockedUntil), true
	}

	return 0, false
}

func (l *LoginLimiter) Reset(r *http.Request) {
	key := clientKey(r)

	l.mu.Lock()
	defer l.mu.Unlock()

	delete(l.failures, key)
}

func TokenMatches(provided string, expected string) bool {
	provided = strings.TrimSpace(provided)
	expected = strings.TrimSpace(expected)

	if provided == "" || expected == "" {
		return false
	}

	return subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) == 1
}

func HasValidSession(r *http.Request, token string) bool {
	token = strings.TrimSpace(token)
	if token == "" {
		return false
	}

	cookie, err := r.Cookie(SessionCookieName)
	if err != nil {
		return false
	}

	parts := strings.Split(cookie.Value, ".")
	if len(parts) != 2 {
		return false
	}

	expiresAtText := parts[0]
	signatureText := parts[1]

	expiresAt, err := strconv.ParseInt(expiresAtText, 10, 64)
	if err != nil {
		return false
	}

	if time.Now().Unix() > expiresAt {
		return false
	}

	expectedSignature := signSessionValue(expiresAtText, token)

	return subtle.ConstantTimeCompare([]byte(signatureText), []byte(expectedSignature)) == 1
}

func SetSessionCookie(w http.ResponseWriter, r *http.Request, token string) {
	expiresAt := time.Now().Add(SessionTTL).Unix()
	expiresAtText := fmt.Sprintf("%d", expiresAt)
	signature := signSessionValue(expiresAtText, token)

	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    expiresAtText + "." + signature,
		Path:     "/ui",
		MaxAge:   int(SessionTTL.Seconds()),
		Expires:  time.Unix(expiresAt, 0),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   r.TLS != nil,
	})
}

func ClearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/ui",
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

func signSessionValue(expiresAtText string, token string) string {
	mac := hmac.New(sha256.New, []byte(token))
	mac.Write([]byte("MeshBan WebUI session v1\n"))
	mac.Write([]byte(expiresAtText))

	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func clientKey(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return strings.TrimSpace(r.RemoteAddr)
	}

	return strings.TrimSpace(host)
}
