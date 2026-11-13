package application

import (
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/labstack/echo/v5"
)

// MemoryPage is a memory being made or changed, on a page of its own: its
// name, kind and note, the purchases in it, and what else was bought around
// them, in case any were forgotten. Every purchase is a tick box, and the
// memory holds what is ticked when it is saved.
type MemoryPage struct {
	Memory models.Memory
	New    bool
	Kinds  []models.MemoryKind

	// Rows are the purchases in the memory, or ticked to start one.
	Rows []TransactionRow
	// Nearby is what else was bought on and around their days.
	Nearby []TimelineContextRow

	// Return is where the page was opened from, to go back to once done.
	Return string
}

// Total is what the purchases in the memory come to, as on the timeline.
func (p MemoryPage) Total() float64 {
	total := 0.0
	for _, row := range p.Rows {
		total -= row.Amount
	}
	return total
}

// Kind is what the memory is, which its header wears.
func (p MemoryPage) Kind() models.MemoryKind {
	return models.MemoryKindByKey(p.Memory.Kind)
}

// Looks is each kind's icon and colour, for the header to follow the kind
// picked before it is saved.
func (p MemoryPage) Looks() map[string]map[string]string {
	looks := make(map[string]map[string]string, len(p.Kinds))
	for _, kind := range p.Kinds {
		looks[kind.Key] = map[string]string{"icon": kind.Icon, "accent": kind.Accent}
	}
	return looks
}

// Span is the days the memory's purchases fall on, as the timeline says it.
func (p MemoryPage) Span() string {
	if len(p.Rows) == 0 {
		return ""
	}
	return TimelineEntry{Start: p.Rows[0].Date, End: p.Rows[len(p.Rows)-1].Date}.Span()
}

// memoriesHome is where a memory's page goes back to when it was opened from
// nowhere it can return to.
const memoriesHome = "/timeline?view=memories"

// returnPath is the page to go back to: the one named, so long as it is one
// of budge's own, so a link can't send the owner off to another site.
func returnPath(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || raw == "" || u.Scheme != "" || u.Host != "" || u.User != nil ||
		!strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "//") || strings.ContainsRune(raw, '\\') {
		return memoriesHome
	}
	return raw
}

// goBack answers a save or a delete by going back to the page the memory's
// page was opened from. htmx is told where, to load it as any page is loaded.
func goBack(c *echo.Context, to string) error {
	if c.Request().Header.Get("HX-Request") == "" {
		return c.Redirect(http.StatusSeeOther, to)
	}
	location, err := json.Marshal(map[string]string{"path": to, "target": "#main"})
	if err != nil {
		return err
	}
	c.Response().Header().Set("HX-Location", string(location))
	return c.NoContent(http.StatusOK)
}

// memoryPage gathers the page for a memory holding ids. self is the memory's
// own id, empty for a new one, so its purchases aren't marked as another's.
func (app *Application) memoryPage(c *echo.Context, memory models.Memory, ids []string) (MemoryPage, error) {
	ctx := c.Request().Context()
	transactions, err := app.store.ReadTransactions(ctx)
	if err != nil {
		return MemoryPage{}, err
	}
	memories, err := app.store.ReadMemories(ctx)
	if err != nil {
		return MemoryPage{}, err
	}
	rows, nearby := memoryContext(transactions, memories, ids, memory.Id, time.Now().Location())
	return MemoryPage{
		Memory: memory,
		New:    memory.Id == "",
		Kinds:  models.MemoryKinds,
		Rows:   rows,
		Nearby: nearby,
		Return: returnPath(c.QueryParam("return")),
	}, nil
}

// memoryContext finds the purchases a memory holds, by date, and what else
// was bought around them that it doesn't. A purchase in another memory is
// marked, as it would be taken from it.
func memoryContext(transactions []models.Transaction, memories []models.Memory, ids []string, self string, location *time.Location) ([]TransactionRow, []TimelineContextRow) {
	held := make(map[string]bool, len(ids))
	for _, id := range ids {
		held[id] = true
	}
	claimed := map[string]bool{}
	for _, memory := range memories {
		if memory.Id == self {
			continue
		}
		for _, id := range memory.TransactionIDs {
			claimed[id] = true
		}
	}

	var entry TimelineEntry
	byDay := map[string][]models.Transaction{}
	for _, tx := range transactions {
		if held[tx.Id] {
			entry.Rows = append(entry.Rows, BuildTransactionRow(tx))
		}
		if timelineSpend(tx) {
			day := tx.Date.In(location).Format(time.DateOnly)
			byDay[day] = append(byDay[day], tx)
		}
	}
	slices.SortFunc(entry.Rows, func(a, b TransactionRow) int {
		if c := a.Date.Compare(b.Date); c != 0 {
			return c
		}
		return strings.Compare(a.Id, b.Id)
	})
	for i := range entry.Rows {
		entry.Rows[i].Title = timelineTitle(entry.Rows[i])
	}
	return entry.Rows, nearbyPurchases(entry, byDay, claimed, location)
}

// MemoryNew opens the page to start a memory from the purchases ticked.
func (app *Application) MemoryNew(c *echo.Context) error {
	form, err := c.FormValues()
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	ids := uniqueValues(form["tx"])
	if len(ids) == 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "Tick at least one purchase for the memory.")
	}
	page, err := app.memoryPage(c, models.Memory{Kind: "trip"}, ids)
	if err != nil {
		return err
	}
	return c.Render(http.StatusOK, "memory", page)
}

// MemoryEdit opens the page to change a memory.
func (app *Application) MemoryEdit(c *echo.Context) error {
	memory, err := app.store.GetMemory(c.Request().Context(), c.Param("id"))
	if err != nil {
		return lookupError(err, "That memory no longer exists.")
	}
	page, err := app.memoryPage(c, memory, memory.TransactionIDs)
	if err != nil {
		return err
	}
	return c.Render(http.StatusOK, "memory", page)
}

// MemoryCreate saves a new memory of the purchases ticked, and goes back.
func (app *Application) MemoryCreate(c *echo.Context) error {
	form, err := c.FormValues()
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	ids := uniqueValues(form["tx"])
	if len(ids) == 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "Tick at least one purchase for the memory.")
	}
	if c.FormValue("memory") != "" {
		return echo.NewHTTPError(http.StatusBadRequest, "A new memory can't name an existing one.")
	}
	memory, err := app.addToMemory(c, ids)
	if err != nil {
		return err
	}
	app.Toast(c, "Saved", "Made “"+memory.Title+"”.")
	return goBack(c, returnPath(c.FormValue("return")))
}

// MemoryUpdate saves a memory's name, kind and note, and makes it hold
// exactly the purchases ticked, then goes back.
func (app *Application) MemoryUpdate(c *echo.Context) error {
	ctx := c.Request().Context()
	memory, err := app.store.GetMemory(ctx, c.Param("id"))
	if err != nil {
		return lookupError(err, "That memory no longer exists.")
	}
	form, err := c.FormValues()
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	ids := uniqueValues(form["tx"])
	if len(ids) == 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "A memory needs at least one purchase. Delete it instead, if it's no longer wanted.")
	}

	memory.Kind = models.MemoryKindByKey(c.FormValue("kind")).Key
	memory.Note = strings.TrimSpace(c.FormValue("note"))
	if title := strings.TrimSpace(c.FormValue("title")); title != "" {
		memory.Title = title
	}
	added := slices.DeleteFunc(slices.Clone(ids), func(id string) bool { return slices.Contains(memory.TransactionIDs, id) })
	memory.TransactionIDs = ids
	if err := app.store.SaveMemory(ctx, memory); err != nil {
		return err
	}
	if err := app.unhide(c, added); err != nil {
		return err
	}
	app.Toast(c, "Saved", "Saved “"+memory.Title+"”.")
	return goBack(c, returnPath(c.FormValue("return")))
}

// MemoryDelete deletes a memory from its page, and goes back. Its purchases
// go back on the timeline rather than being lost with it.
func (app *Application) MemoryDelete(c *echo.Context) error {
	ctx := c.Request().Context()
	memory, err := app.store.GetMemory(ctx, c.Param("id"))
	if err != nil {
		return lookupError(err, "That memory no longer exists.")
	}
	if err := app.store.DeleteMemory(ctx, memory.Id); err != nil {
		return err
	}
	app.Toast(c, "Deleted", "Deleted “"+memory.Title+"”.")
	return goBack(c, returnPath(c.FormValue("return")))
}
