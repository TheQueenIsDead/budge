package application

import (
	"fmt"
	"net/http"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/labstack/echo/v4"
)

// A transaction can be added to a memory straight from the transactions list,
// not only from the timeline. The timeline only offers what stood out, and the
// thing worth remembering is sometimes a $12 museum ticket found by searching.

// TransactionMemoryOption is a memory a transaction can be added to.
type TransactionMemoryOption struct {
	Id    string
	Label string
}

// memoryIndex reads every memory, returning which memory holds each
// transaction and the memories to offer, newest first.
func (app *Application) memoryIndex() (map[string]models.Memory, []TransactionMemoryOption, error) {
	memories, err := app.store.ReadMemories()
	if err != nil {
		return nil, nil, err
	}

	sort.SliceStable(memories, func(i, j int) bool {
		return memories[i].CreatedAt.After(memories[j].CreatedAt)
	})

	held := map[string]models.Memory{}
	options := make([]TransactionMemoryOption, 0, len(memories))
	for _, memory := range memories {
		for _, id := range memory.TransactionIDs {
			held[id] = memory
		}
		options = append(options, TransactionMemoryOption{
			Id:    memory.Id,
			Label: memory.Title + " · " + models.MemoryKindByKey(memory.Kind).Label,
		})
	}
	return held, options, nil
}

// markMemories notes on each row the memory that holds it, if any.
func markMemories(days []TransactionDay, held map[string]models.Memory) {
	for d := range days {
		for r := range days[d].Rows {
			if memory, ok := held[days[d].Rows[r].Id]; ok {
				days[d].Rows[r].MemoryID = memory.Id
				days[d].Rows[r].MemoryTitle = memory.Title
			}
		}
	}
}

// addToMemory puts transactions into the memory named by the form, or into a
// new one built from its title, kind and note when none is named. Anything put
// into a memory was worth keeping, so it is unhidden from the timeline too.
func (app *Application) addToMemory(c echo.Context, ids []string) (models.Memory, error) {
	var memory models.Memory
	if id := c.FormValue("memory"); id != "" {
		existing, err := app.store.GetMemory(id)
		if err != nil {
			return memory, echo.NewHTTPError(http.StatusNotFound, "That memory no longer exists.")
		}
		memory = existing
		memory.TransactionIDs = uniqueValues(append(memory.TransactionIDs, ids...))
	} else {
		kind := models.MemoryKindByKey(c.FormValue("kind"))
		memory = models.Memory{
			Id:             newID(),
			Title:          strings.TrimSpace(c.FormValue("title")),
			Kind:           kind.Key,
			Note:           strings.TrimSpace(c.FormValue("note")),
			TransactionIDs: ids,
			CreatedAt:      time.Now(),
		}
		if memory.Title == "" {
			memory.Title = kind.Label
		}
	}

	if err := app.store.SaveMemory(memory); err != nil {
		return memory, echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	prefs, err := app.store.GetTimelinePreferences()
	if err != nil {
		return memory, echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	prefs.HiddenTransactions = slices.DeleteFunc(prefs.HiddenTransactions, func(id string) bool {
		return slices.Contains(ids, id)
	})
	if err := app.store.SaveTimelinePreferences(prefs); err != nil {
		return memory, echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return memory, nil
}

// removeFromMemories takes transactions back out of whichever memories hold
// them, returning the titles of the memories touched. A memory left empty is
// removed, as on the timeline, since it has no date to sit at.
func (app *Application) removeFromMemories(ids []string) ([]string, error) {
	memories, err := app.store.ReadMemories()
	if err != nil {
		return nil, echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	var touched []string
	for _, memory := range memories {
		before := len(memory.TransactionIDs)
		memory.TransactionIDs = slices.DeleteFunc(memory.TransactionIDs, func(id string) bool {
			return slices.Contains(ids, id)
		})
		if len(memory.TransactionIDs) == before {
			continue
		}
		touched = append(touched, memory.Title)

		if len(memory.TransactionIDs) == 0 {
			err = app.store.DeleteMemory(memory.Id)
		} else {
			err = app.store.SaveMemory(memory)
		}
		if err != nil {
			return touched, echo.NewHTTPError(http.StatusInternalServerError, err.Error())
		}
	}
	return touched, nil
}

// transactionMemoryLayouts are the two places a transaction is drawn: the
// phone cards and the desktop table. Both copies are refreshed together so a
// resize never shows the old state.
var transactionMemoryLayouts = []string{"m", "d"}

// renderMemoryControls answers with the memory control of each transaction
// alone, so a long list neither reflows nor loses its place. The control that
// was used, named by its layout, is swapped in place; everything else rides
// along out of band. With no layout named, as from the selection bar, all of
// it is out of band, and the bar is redrawn so it offers any memory just made.
func (app *Application) renderMemoryControls(c echo.Context, ids []string, used string) error {
	held, options, err := app.memoryIndex()
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	controls := make([]map[string]interface{}, 0, len(ids)*len(transactionMemoryLayouts))
	for _, id := range ids {
		memory := held[id]
		for _, layout := range transactionMemoryLayouts {
			controls = append(controls, map[string]interface{}{
				"ID":          id,
				"MemoryID":    memory.Id,
				"MemoryTitle": memory.Title,
				"Layout":      layout,
				"Options":     options,
				"OOB":         layout != used,
			})
		}
	}

	return c.Render(http.StatusOK, "transactions.memory.swap", map[string]interface{}{
		"Controls": controls,
		"Bar":      used == "",
		"Options":  options,
	})
}

// usedLayout reads which copy of a control a request came from.
func usedLayout(c echo.Context) string {
	layout := c.FormValue("layout")
	if !slices.Contains(transactionMemoryLayouts, layout) {
		layout = transactionMemoryLayouts[0]
	}
	return layout
}

// pickedMemory insists on an existing memory. The transactions list only
// adds to memories already made; new ones start on the timeline.
func pickedMemory(c echo.Context) error {
	if c.FormValue("memory") == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Pick a memory to add to.")
	}
	return nil
}

// TransactionAddToMemory adds one transaction to a memory.
func (app *Application) TransactionAddToMemory(c echo.Context) error {
	id := c.Param("id")
	if id == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Pick a transaction to add.")
	}
	if err := pickedMemory(c); err != nil {
		return err
	}

	memory, err := app.addToMemory(c, []string{id})
	if err != nil {
		return err
	}

	app.Toast(c, "Saved", "Added to “"+memory.Title+"”.")
	return app.renderMemoryControls(c, []string{id}, usedLayout(c))
}

// TransactionRemoveFromMemory takes one transaction back out of its memory.
func (app *Application) TransactionRemoveFromMemory(c echo.Context) error {
	id := c.Param("id")
	if id == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Pick a transaction to remove.")
	}

	touched, err := app.removeFromMemories([]string{id})
	if err != nil {
		return err
	}

	if len(touched) > 0 {
		app.Toast(c, "Removed", "Taken out of “"+touched[0]+"”.")
	}
	return app.renderMemoryControls(c, []string{id}, usedLayout(c))
}

// selectedTransactions reads the transactions ticked in the list.
func selectedTransactions(c echo.Context) ([]string, error) {
	form, err := c.FormParams()
	if err != nil {
		return nil, echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	ids := uniqueValues(form["tx"])
	if len(ids) == 0 {
		return nil, echo.NewHTTPError(http.StatusBadRequest, "Tick at least one transaction.")
	}
	return ids, nil
}

// TransactionsAddToMemory adds every ticked transaction to one memory.
func (app *Application) TransactionsAddToMemory(c echo.Context) error {
	ids, err := selectedTransactions(c)
	if err != nil {
		return err
	}
	if err := pickedMemory(c); err != nil {
		return err
	}

	memory, err := app.addToMemory(c, ids)
	if err != nil {
		return err
	}

	app.Toast(c, "Saved", fmt.Sprintf("Added %d to “%s”.", len(ids), memory.Title))
	return app.renderMemoryControls(c, ids, "")
}

// TransactionsRemoveFromMemory takes every ticked transaction out of whichever
// memory holds it.
func (app *Application) TransactionsRemoveFromMemory(c echo.Context) error {
	ids, err := selectedTransactions(c)
	if err != nil {
		return err
	}

	touched, err := app.removeFromMemories(ids)
	if err != nil {
		return err
	}

	switch len(touched) {
	case 0:
		app.Toast(c, "Nothing to remove", "None of those are in a memory.")
	case 1:
		app.Toast(c, "Removed", "Taken out of “"+touched[0]+"”.")
	default:
		app.Toast(c, "Removed", fmt.Sprintf("Taken out of %d memories.", len(touched)))
	}
	return app.renderMemoryControls(c, ids, "")
}
