package application

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"github.com/labstack/echo/v4"
	"net/http"
	"sync"
)

var (
	Cache = sync.Map{}
)

// responseWriter wraps http.ResponseWriter to capture the response body
type responseWriter struct {
	http.ResponseWriter
	buf *bytes.Buffer
}

func (w *responseWriter) Write(b []byte) (int, error) {
	w.buf.Write(b) // capture body
	return w.ResponseWriter.Write(b)
}

func Caching() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			// Set default cache hit reponse header
			c.Response().Header().Set("X-Cache-Hit", "false")

			// Attempt to find a cache key
			cookie, err := c.Request().Cookie("X-Cache-Key")

			// If we do not have a cache key, we can not read or write to the cache, continue.
			if err != nil {
				return next(c)
			}

			// Build the cache key based on the cookie (last sync time), request path, and whether this is an HTMX
			// triggered event (ie, loading a partial, not a full layout)
			md5sum := md5.Sum([]byte(cookie.Value + c.Request().URL.Path + c.Request().Header.Get("HX-Request")))
			key := hex.EncodeToString(md5sum[:])

			// Return cached HTML if we get a hit.
			if hit, ok := Cache.Load(key); ok {
				html := hit.(string)
				c.Response().Header().Set("X-Cache-Hit", "true")
				return c.HTML(200, html)
			}

			// Else, wrap the response writer to capture the response and cache on the way out
			rw := c.Response()
			buf := new(bytes.Buffer)
			mw := &responseWriter{ResponseWriter: rw.Writer, buf: buf}
			rw.Writer = mw

			// Call next
			err = next(c)
			if err != nil {
				return err
			}

			// Store the response for next time
			if err != nil {
				res := mw.buf.String()
				Cache.Store(key, res)
			}

			// Pass through any errors
			return err
		}
	}
}
