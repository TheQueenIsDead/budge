package application

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMemoryPage(t *testing.T) {
	app := newTestApp(t)
	for _, tx := range append(ordinaryYear(),
		purchase("flights", "Air NZ", 1400, time.March, 1),
		purchase("hotel", "Hotel", 900, time.March, 10),
		purchase("taxi", "Taxi", 60, time.March, 11),
		purchase("souvenir", "Souvenirs", 40, time.March, 10),
	) {
		require.NoError(t, app.store.CreateTransaction(t.Context(), tx))
	}
	require.NoError(t, app.store.SaveMemory(t.Context(), models.Memory{Id: "gifts", Title: "Gifts", TransactionIDs: []string{"souvenir"}}))

	get := func(path string, id string, handler func(*echo.Context) error) string {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("HX-Request", "true")
		rec := httptest.NewRecorder()
		c := app.http.NewContext(req, rec)
		if id != "" {
			c.SetPathValues(echo.PathValues{{Name: "id", Value: id}})
		}
		require.NoError(t, handler(c))
		return rec.Body.String()
	}
	send := func(values url.Values, id string, handler func(*echo.Context) error) *httptest.ResponseRecorder {
		t.Helper()
		c, rec := postForm(t, app, values)
		c.Request().Header.Set("HX-Request", "true")
		if id != "" {
			c.SetPathValues(echo.PathValues{{Name: "id", Value: id}})
		}
		require.NoError(t, handler(c))
		return rec
	}

	t.Run("a new memory opens with what was ticked, and what was bought around it", func(t *testing.T) {
		page := get("/memories/new?tx=hotel&return=%2Ftransactions%3Fsearch%3Dhotel", "", app.MemoryNew)
		assert.Contains(t, page, `name="tx" value="hotel" checked`)
		assert.Contains(t, page, `name="tx" value="taxi"`, "the day after is offered")
		assert.NotContains(t, page, `name="tx" value="taxi" checked`, "but not ticked")
		assert.NotContains(t, page, `value="souvenir"`, "one in another memory is marked, not offered")
		assert.Contains(t, page, `name="return" value="/transactions?search=hotel"`)
		assert.Contains(t, page, `hx-post="/memories"`)
	})

	var id string
	t.Run("making it saves what is ticked, and goes back", func(t *testing.T) {
		rec := send(url.Values{"title": {"Japan"}, "kind": {"trip"}, "tx": {"hotel", "taxi"}, "return": {"/transactions?search=hotel"}}, "", app.MemoryCreate)
		assert.JSONEq(t, `{"path": "/transactions?search=hotel", "target": "#main"}`, rec.Header().Get("HX-Location"))
		assert.Contains(t, toastMessage(t, rec.Header().Get("Hx-Trigger")), "Japan")

		memories, err := app.store.ReadMemories(t.Context())
		require.NoError(t, err)
		for _, memory := range memories {
			if memory.Title == "Japan" {
				id = memory.Id
				assert.Equal(t, []string{"hotel", "taxi"}, memory.TransactionIDs)
			}
		}
		require.NotEmpty(t, id)
	})

	t.Run("editing holds exactly what is ticked", func(t *testing.T) {
		page := get("/memories/"+id+"/edit", id, app.MemoryEdit)
		assert.Contains(t, page, `hx-post="/memories/`+id+`"`)
		assert.Contains(t, page, `name="tx" value="taxi" checked`)
		assert.Contains(t, page, `name="return" value="`+memoriesHome+`"`, "opened from nowhere, it goes back to the memories")

		send(url.Values{"title": {"Japan 2026"}, "kind": {"experience"}, "note": {"Cherry blossoms"}, "tx": {"hotel", "flights"}}, id, app.MemoryUpdate)
		memory, err := app.store.GetMemory(t.Context(), id)
		require.NoError(t, err)
		assert.Equal(t, "Japan 2026", memory.Title)
		assert.Equal(t, "experience", memory.Kind)
		assert.Equal(t, "Cherry blossoms", memory.Note)
		assert.ElementsMatch(t, []string{"hotel", "flights"}, memory.TransactionIDs, "the taxi unticked, the flights ticked")
	})

	t.Run("untick everything and it's refused, rather than emptied", func(t *testing.T) {
		c, _ := postForm(t, app, url.Values{"title": {"Japan"}})
		c.SetPathValues(echo.PathValues{{Name: "id", Value: id}})
		var refused *echo.HTTPError
		require.True(t, errors.As(app.MemoryUpdate(c), &refused))
		assert.Equal(t, http.StatusBadRequest, refused.Code)
	})

	t.Run("deleting goes back too", func(t *testing.T) {
		rec := send(url.Values{"return": {"/timeline?view=memories"}}, id, app.MemoryDelete)
		assert.JSONEq(t, `{"path": "/timeline?view=memories", "target": "#main"}`, rec.Header().Get("HX-Location"))
		_, err := app.store.GetMemory(t.Context(), id)
		assert.Error(t, err)
	})
}

func TestReturnPathStaysInBudge(t *testing.T) {
	for _, ok := range []string{"/transactions?search=hotel&account=a", "/timeline?view=memories", "/"} {
		assert.Equal(t, ok, returnPath(ok))
	}
	for _, away := range []string{"", "https://evil.example/", "//evil.example/", "/\\evil.example", "javascript:alert(1)", "transactions"} {
		assert.Equal(t, memoriesHome, returnPath(away), away)
	}
}
