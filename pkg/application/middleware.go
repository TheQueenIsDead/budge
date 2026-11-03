package application

import (
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
