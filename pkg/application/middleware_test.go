package application

import (
	"html/template"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/TheQueenIsDead/budge/web"
	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSecurityHeaders(t *testing.T) {
	tpl, err := template.New("").Funcs(templateFuncs()).ParseFS(web.Templates, "templates/*.gohtml")
	require.NoError(t, err)
	renderer, err := newTemplate(tpl, false)
	require.NoError(t, err)
	e := echo.New()
	e.Renderer = renderer
	e.Use(SecurityHeaders())
	e.GET("/", func(c *echo.Context) error { return c.Render(http.StatusOK, "4XX", nil) })

	get := func(htmx bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		if htmx {
			req.Header.Set("HX-Request", "true")
		}
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)
		return rec
	}
	nonceOf := regexp.MustCompile(`'nonce-([^']+)'`)

	page := get(false)
	policy := page.Header().Get("Content-Security-Policy")
	match := nonceOf.FindStringSubmatch(policy)
	require.NotNil(t, match, "the policy carries a nonce: %s", policy)
	nonce := match[1]

	t.Run("the page's own scripts carry the request's nonce", func(t *testing.T) {
		inline := regexp.MustCompile(`<script\b[^>]*>`)
		var count int
		for _, tag := range inline.FindAllString(page.Body.String(), -1) {
			if strings.Contains(tag, "src=") {
				continue
			}
			count++
			assert.Contains(t, tag, `nonce="`+nonce+`"`)
		}
		assert.Positive(t, count, "the layout has inline scripts to check")
		assert.Contains(t, page.Body.String(), `htmx.config.inlineScriptNonce = "`+nonce+`"`,
			"htmx runs the scripts it swaps in with the page's nonce")
	})

	t.Run("each request has a nonce of its own", func(t *testing.T) {
		again := nonceOf.FindStringSubmatch(get(false).Header().Get("Content-Security-Policy"))
		require.NotNil(t, again)
		assert.NotEqual(t, nonce, again[1])
	})

	t.Run("nothing can be sent anywhere but budge, or frame it", func(t *testing.T) {
		for _, directive := range []string{"default-src 'self'", "connect-src 'self'", "img-src 'self' data: https://cdn.akahu.nz", "form-action 'self'",
			"frame-ancestors 'none'", "object-src 'none'", "base-uri 'none'"} {
			assert.Contains(t, policy, directive)
		}
		assert.NotContains(t, policy, "script-src 'self' 'unsafe-inline'", "scripts need the nonce, not just to be inline")
		assert.Equal(t, "nosniff", page.Header().Get("X-Content-Type-Options"))
		assert.Equal(t, "same-origin", page.Header().Get("Referrer-Policy"))
	})

	t.Run("an htmx request is answered too, and carries the policy", func(t *testing.T) {
		fragment := get(true)
		assert.Contains(t, fragment.Header().Get("Content-Security-Policy"), "'nonce-")
		assert.NotContains(t, fragment.Body.String(), "<html", "only the fragment")
	})
}
