package application

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/TheQueenIsDead/budge/pkg/database"
	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUnknownIdsAreNotFound(t *testing.T) {
	t.Setenv("BUDGE_BOLT_PATH", t.TempDir())
	store, err := database.NewStore()
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	app, err := NewApplication(store, nil, nil, nil)
	require.NoError(t, err)

	// Asked by htmx, so the answer is the status itself rather than the
	// redirect a page load gets.
	serve := func(method, target string, form url.Values) int {
		req := httptest.NewRequest(method, target, strings.NewReader(form.Encode()))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
		req.Header.Set("HX-Request", "true")
		rec := httptest.NewRecorder()
		app.http.ServeHTTP(rec, req)
		return rec.Code
	}

	for _, target := range []string{"/portfolio/accounts/nope", "/portfolio/assets/nope", "/transactions/nope/category"} {
		assert.Equal(t, http.StatusNotFound, serve(http.MethodGet, target, nil), target)
	}
	assert.Equal(t, http.StatusNotFound, serve(http.MethodPost, "/portfolio/assets/nope/purchase", url.Values{"purchase_price": {"1"}}))

	t.Run("a token that is not one is the owner's mistake", func(t *testing.T) {
		code := serve(http.MethodPost, "/integrations/akahu/save", url.Values{"akahuAppToken": {"nonsense"}, "akahuUserToken": {"user_x"}})
		assert.Equal(t, http.StatusBadRequest, code)
	})

	t.Run("a store that fails is not mistaken for something missing", func(t *testing.T) {
		err := lookupError(errors.New("disk on fire"), "asset not found")
		var coded *echo.HTTPError
		assert.False(t, errors.As(err, &coded), "left to answer as a server error")
	})
}
