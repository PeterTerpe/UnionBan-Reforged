package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	SessionCookieName = "meshban_web_session"
	SessionTTL        = 12 * time.Hour
)

func RequestHasValidToken(r *http.Request, expectedToken string) bool {
	provided := ""

	if value := strings.TrimSpace(r.Header.Get("X-MeshBan-Token")); value != "" {
		provided = value
	}

	authHeader := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(authHeader, "Bearer ") {
		provided = strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))
	}

	if value := strings.TrimSpace(r.URL.Query().Get("token")); value != "" {
		provided = value
	}

	return TokenMatches(provided, expectedToken)
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
