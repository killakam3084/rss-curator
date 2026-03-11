package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const sessionCookieName = "curator_session"

// newSessionToken creates a signed session token.
// Format: base64url("username:expiry_unix") + ":" + hmac_hex
func newSessionToken(username string, secret []byte, ttl time.Duration) string {
	expiry := time.Now().Add(ttl).Unix()
	payload := fmt.Sprintf("%s:%d", username, expiry)
	encoded := base64.RawURLEncoding.EncodeToString([]byte(payload))
	return encoded + ":" + hmacSign(encoded, secret)
}

// validateSessionToken returns the username if the token is valid and unexpired.
func validateSessionToken(token string, secret []byte) (string, bool) {
	parts := strings.SplitN(token, ":", 2)
	if len(parts) != 2 {
		return "", false
	}
	encoded, sig := parts[0], parts[1]

	// Constant-time HMAC verification
	if !hmac.Equal([]byte(sig), []byte(hmacSign(encoded, secret))) {
		return "", false
	}

	payload, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return "", false
	}

	fields := strings.SplitN(string(payload), ":", 2)
	if len(fields) != 2 {
		return "", false
	}

	expiry, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil {
		return "", false
	}
	if time.Now().Unix() > expiry {
		return "", false
	}
	return fields[0], true
}

func hmacSign(data string, secret []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(data))
	return hex.EncodeToString(mac.Sum(nil))
}

// authMiddleware enforces session cookie auth over the wrapped handler.
// Exempt paths: /login, /logout, /api/health.
// Unauthenticated /api/* → 401 JSON. Everything else → 302 /login.
func authMiddleware(next http.Handler, secret []byte) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		// Exempt paths — no auth required
		if path == "/login" || path == "/logout" || path == "/api/health" {
			next.ServeHTTP(w, r)
			return
		}

		// Validate session cookie
		if cookie, err := r.Cookie(sessionCookieName); err == nil {
			if _, ok := validateSessionToken(cookie.Value, secret); ok {
				next.ServeHTTP(w, r)
				return
			}
		}

		// Unauthenticated: API gets 401 JSON; UI paths get redirect
		if strings.HasPrefix(path, "/api/") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error":"unauthorized"}`))
			return
		}

		nextURL := r.URL.RequestURI()
		if nextURL == "/" || nextURL == "" {
			http.Redirect(w, r, "/login", http.StatusFound)
		} else {
			http.Redirect(w, r, "/login?next="+nextURL, http.StatusFound)
		}
	})
}

// handleLogin serves GET /login and processes POST /login credential checks.
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		http.ServeFile(w, r, "./web/login.html")
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	inputUser := r.FormValue("username")
	inputPass := r.FormValue("password")
	nextURL := r.FormValue("next")

	// Constant-time comparison to prevent timing-based username enumeration
	userMatch := hmac.Equal([]byte(inputUser), []byte(s.authUsername))
	passMatch := hmac.Equal([]byte(inputPass), []byte(s.authPassword))

	if !userMatch || !passMatch {
		redirect := "/login?error=1"
		if nextURL != "" {
			redirect += "&next=" + nextURL
		}
		http.Redirect(w, r, redirect, http.StatusFound)
		return
	}

	token := newSessionToken(s.authUsername, s.sessionSecret, s.sessionTTL)
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		// Secure is intentionally false: TLS is terminated by the nginx reverse proxy.
		// The Go app receives plain HTTP on loopback; the cookie is protected by
		// the proxy's HTTPS layer end-to-end with the browser.
		Secure: false,
		MaxAge: int(s.sessionTTL.Seconds()),
	})

	if nextURL == "" || nextURL == "/login" {
		nextURL = "/"
	}
	http.Redirect(w, r, nextURL, http.StatusFound)
}

// handleLogout clears the session cookie and redirects to /login.
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	})
	http.Redirect(w, r, "/login", http.StatusFound)
}
