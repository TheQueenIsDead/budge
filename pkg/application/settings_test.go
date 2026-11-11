package application

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/TheQueenIsDead/budge/pkg/integrations"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSettingsNeverShowsTheSavedTokens(t *testing.T) {
	app := newTestApp(t)
	require.NoError(t, app.store.UpdateAkahuSettings(t.Context(), models.IntegrationAkahuSettings{AppToken: "app_secret", UserToken: "user_secret"}))

	req := httptest.NewRequest(http.MethodGet, "/settings", nil)
	rec := httptest.NewRecorder()
	require.NoError(t, app.Settings(app.http.NewContext(req, rec)))

	page := rec.Body.String()
	assert.NotContains(t, page, "app_secret")
	assert.NotContains(t, page, "user_secret")
	assert.Contains(t, page, "Saved — leave blank to keep", "says a token is saved without showing it")
}

func TestPutAkahuSettingsKeepsATokenLeftBlank(t *testing.T) {
	app := newTestApp(t)
	require.NoError(t, app.store.UpdateAkahuSettings(t.Context(), models.IntegrationAkahuSettings{AppToken: "app_old", UserToken: "user_old"}))
	app.integrations = integrations.NewIntegrations(app.store)

	c, _ := postForm(t, app, url.Values{"akahuAppToken": {""}, "akahuUserToken": {"user_new"}})
	require.NoError(t, app.PutAkahuSettings(c))

	settings, err := app.store.GetAkahuSettings(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "app_old", settings.AppToken, "a blank field keeps what is saved")
	assert.Equal(t, "user_new", settings.UserToken, "a typed token replaces it")
}
