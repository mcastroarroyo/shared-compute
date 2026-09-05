package api

import "net/http"

// secureHeaders sets baseline browser-facing security headers on every response.
// The API serves JSON (and one shell script), never HTML, so the CSP is the
// strictest possible: nothing may load. HSTS is 1 year + preload-eligible.
func secureHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains; preload")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=()")
		h.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'")
		h.Set("Cross-Origin-Resource-Policy", "cross-origin") // the site/app/console are other origins
		h.Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}
