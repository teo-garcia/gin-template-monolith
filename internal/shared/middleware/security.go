package middleware

import (
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/teo-garcia/gin-template-monolith/internal/config"
)

// SecurityHeaders sets the response headers the portfolio requires wherever the
// framework can enforce them.
//
// HSTS is only emitted over TLS: sending it on plaintext localhost would pin
// browsers to https for a scheme the dev server does not serve.
func SecurityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.Writer.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Cross-Origin-Resource-Policy", "same-origin")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		h.Set("X-DNS-Prefetch-Control", "off")

		// This is an API: the only HTML it serves is the docs page, which needs
		// its own inline styles and scripts.
		if strings.HasPrefix(c.Request.URL.Path, "/docs") {
			h.Set("Content-Security-Policy",
				"default-src 'self'; script-src 'self' 'unsafe-inline'; "+
					"style-src 'self' 'unsafe-inline'; img-src 'self' data:; "+
					"connect-src 'self'; frame-ancestors 'none'")
		} else {
			h.Set("Content-Security-Policy",
				"default-src 'none'; frame-ancestors 'none'; base-uri 'none'")
		}

		if c.Request.TLS != nil || c.GetHeader("X-Forwarded-Proto") == "https" {
			h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}

		c.Next()
	}
}

// CORS applies the configured cross-origin policy.
//
// Origins are matched exactly against the allow-list and echoed back one at a
// time; the wildcard is never reflected, so credentialed requests stay safe.
func CORS(cfg config.Config) gin.HandlerFunc {
	allowed := make([]string, 0, len(cfg.CORS.Origins))
	for _, origin := range cfg.CORS.Origins {
		if trimmed := strings.TrimSpace(origin); trimmed != "" {
			allowed = append(allowed, trimmed)
		}
	}
	maxAge := strconv.Itoa(int((12 * time.Hour).Seconds()))

	return func(c *gin.Context) {
		if !cfg.CORS.Enabled {
			c.Next()
			return
		}

		origin := c.GetHeader("Origin")
		if origin == "" {
			c.Next()
			return
		}

		if !slices.Contains(allowed, origin) {
			// An unlisted origin gets no CORS headers at all. The browser
			// blocks it; a non-browser client is unaffected, which is correct.
			if c.Request.Method == http.MethodOptions {
				c.AbortWithStatus(http.StatusNoContent)
				return
			}
			c.Next()
			return
		}

		h := c.Writer.Header()
		h.Set("Access-Control-Allow-Origin", origin)
		h.Set("Access-Control-Allow-Credentials", "true")
		h.Set("Access-Control-Allow-Methods", "GET,POST,PATCH,PUT,DELETE,OPTIONS")
		h.Set("Access-Control-Allow-Headers",
			"Origin,Content-Type,Accept,Authorization,"+RequestIDHeader)
		h.Set("Access-Control-Expose-Headers",
			RequestIDHeader+",X-RateLimit-Limit,X-RateLimit-Remaining,X-RateLimit-Reset")
		h.Set("Access-Control-Max-Age", maxAge)
		// Responses vary by Origin, so shared caches must not reuse one
		// origin's response for another.
		h.Add("Vary", "Origin")

		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}
