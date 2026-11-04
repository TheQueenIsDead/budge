package application

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"

	"github.com/labstack/echo/v5"
)

// StaticCaching tells caches how long to keep a static file. A stamped URL
// names one version of a file forever, so it can be kept for a year without
// checking back. An unstamped one, from a bookmark or an old page, must be
// checked every time: embedded files carry no modification time, so there is
// nothing to say how stale a kept copy is.
//
// Only a file actually served is marked. A missing file falls through to the
// not-found redirect, and a year-long header on that would have Cloudflare
// keep the redirect long after the file appeared.
func StaticCaching() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			res, err := echo.UnwrapResponse(c.Response())
			if err != nil {
				return next(c)
			}
			res.Before(func() {
				if res.Status != http.StatusOK {
					return
				}
				if c.QueryParam("v") != "" {
					res.Header().Set(echo.HeaderCacheControl, "public, max-age=31536000, immutable")
				} else {
					res.Header().Set(echo.HeaderCacheControl, "no-cache")
				}
			})
			return next(c)
		}
	}
}

// cspNonceKey is where a request's nonce is kept, for the templates to mark
// the page's own scripts with.
const cspNonceKey = "cspNonce"

// SecurityHeaders sets a Content Security Policy on every response, with a
// nonce made for the request. Only scripts carrying the nonce, or loaded from
// budge or the CDNs its libraries are pinned on, run, and nothing the page
// does can send data anywhere but budge.
//
// Alpine and htmx's hx-on run expressions written in the markup, which needs
// 'unsafe-eval'. Styles may be inline: markup sets widths and colours as it is
// drawn, and a style cannot run code. Images may come from Akahu, which hosts
// the logos of the banks it connects to.
func SecurityHeaders() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			nonce := make([]byte, 16)
			if _, err := rand.Read(nonce); err != nil {
				return err
			}
			encoded := base64.StdEncoding.EncodeToString(nonce)
			c.Set(cspNonceKey, encoded)

			header := c.Response().Header()
			header.Set("Content-Security-Policy", contentSecurityPolicy(encoded))
			header.Set("X-Content-Type-Options", "nosniff")
			header.Set("Referrer-Policy", "same-origin")
			return next(c)
		}
	}
}

// contentSecurityPolicy is the policy for a page carrying the given nonce.
func contentSecurityPolicy(nonce string) string {
	const cdnjs = "https://cdnjs.cloudflare.com"
	return "default-src 'self'; " +
		"script-src 'self' 'nonce-" + nonce + "' 'unsafe-eval' " + cdnjs + " https://cdn.jsdelivr.net; " +
		"style-src 'self' 'unsafe-inline' " + cdnjs + " https://fonts.googleapis.com; " +
		"font-src " + cdnjs + " https://fonts.gstatic.com; " +
		"img-src 'self' data: https://cdn.akahu.nz; " +
		"connect-src 'self'; " +
		"object-src 'none'; " +
		"base-uri 'none'; " +
		"form-action 'self'; " +
		"frame-ancestors 'none'"
}
