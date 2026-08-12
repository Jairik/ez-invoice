package web

import (
	"encoding/base64"
	"net/http"
	"strings"
)

const flashCookie = "ez_invoice_flash"

// setFlash stores a one-shot message for the next page render.
func setFlash(w http.ResponseWriter, ok bool, message string) {
	prefix := "ok:"
	if !ok {
		prefix = "err:"
	}
	http.SetCookie(w, &http.Cookie{
		Name:     flashCookie,
		Value:    prefix + base64.RawURLEncoding.EncodeToString([]byte(message)),
		Path:     "/",
		SameSite: http.SameSiteLaxMode,
	})
}

// takeFlash reads and clears the one-shot flash message.
func takeFlash(w http.ResponseWriter, r *http.Request) (message string, ok bool) {
	cookie, err := r.Cookie(flashCookie)
	if err != nil {
		return "", true
	}
	http.SetCookie(w, &http.Cookie{Name: flashCookie, Value: "", Path: "/", MaxAge: -1})
	value, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(cookie.Value, "ok:"))
	if err != nil {
		return "", true
	}
	if strings.HasPrefix(cookie.Value, "err:") {
		return string(value), false
	}
	return string(value), true
}
