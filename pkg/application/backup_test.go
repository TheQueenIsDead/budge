package application

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func uploadBackup(t *testing.T, app *Application, contents []byte) (echo.Context, *httptest.ResponseRecorder) {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, err := form.CreateFormFile("backup", "budge-settings.json")
	require.NoError(t, err)
	_, _ = part.Write(contents)
	require.NoError(t, form.Close())

	req := httptest.NewRequest(http.MethodPost, "/settings/backup", &body)
	req.Header.Set(echo.HeaderContentType, form.FormDataContentType())
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()
	return app.http.NewContext(req, rec), rec
}

func TestSettingsExportAndImport(t *testing.T) {
	source := newTestApp(t)
	require.NoError(t, source.store.SaveMemory(models.Memory{Id: "japan", Title: "Japan", TransactionIDs: []string{"trans_1"}}))
	require.NoError(t, source.store.UpdateAkahuSettings(models.IntegrationAkahuSettings{AppToken: "app_x", UserToken: "user_y"}))

	req := httptest.NewRequest(http.MethodGet, "/settings/backup", nil)
	rec := httptest.NewRecorder()
	require.NoError(t, source.SettingsExport(source.http.NewContext(req, rec)))
	assert.Contains(t, rec.Header().Get(echo.HeaderContentDisposition), `attachment; filename="budge-settings-`)
	exported := rec.Body.Bytes()

	var backup models.Backup
	require.NoError(t, json.Unmarshal(exported, &backup))
	assert.NotContains(t, string(exported), "user_y", "tokens never leave the instance")

	// A real instance saves its Akahu settings at startup, and the settings
	// page this answers with expects them.
	target := newTestApp(t)
	require.NoError(t, target.store.UpdateAkahuSettings(models.IntegrationAkahuSettings{}))
	c, rec := uploadBackup(t, target, exported)
	require.NoError(t, target.SettingsImport(c))

	assert.Contains(t, rec.Header().Get("Hx-Trigger"), "Imported 1 memories")
	assert.Contains(t, rec.Body.String(), `id="backup"`, "answers with the settings page as it now stands")
	memory, err := target.store.GetMemory("japan")
	require.NoError(t, err)
	assert.Equal(t, "Japan", memory.Title)
}

func TestSettingsImportIgnoresTokensInOlderBackups(t *testing.T) {
	app := newTestApp(t)
	require.NoError(t, app.store.UpdateAkahuSettings(models.IntegrationAkahuSettings{AppToken: "app_mine", UserToken: "user_mine"}))

	c, _ := uploadBackup(t, app, []byte(`{"version": 1, "akahu": {"app_token": "app_x", "user_token": "user_y"}}`))
	require.NoError(t, app.SettingsImport(c))

	akahu, err := app.store.GetAkahuSettings()
	require.NoError(t, err)
	assert.Equal(t, "user_mine", akahu.UserToken)
}

func TestSettingsImportRefusesTheWrongFile(t *testing.T) {
	app := newTestApp(t)

	c, _ := uploadBackup(t, app, []byte(`{"name": "something else"}`))
	err := app.SettingsImport(c)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a Budge backup")

	c, _ = uploadBackup(t, app, []byte(`{"version": 99}`))
	err = app.SettingsImport(c)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "newer version")

	c, _ = uploadBackup(t, app, []byte(strings.Repeat("x", 20)))
	assert.Error(t, app.SettingsImport(c))
}
