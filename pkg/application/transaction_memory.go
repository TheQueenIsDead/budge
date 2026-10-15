package application

import (
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

// transactionMemoryLayouts are the two places a transaction is drawn: the
// phone cards and the desktop table. Both copies are refreshed together so a
// resize never shows the old state.
var transactionMemoryLayouts = []string{"m", "d"}

// TransactionAddToMemory adds one transaction to a memory and answers with its
// memory control alone, so a long list neither reflows nor loses its place.
func (app *Application) TransactionAddToMemory(c echo.Context) error {
	id := c.Param("id")
	if id == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Pick a transaction to add.")
	}

	memory, err := app.addToMemory(c, []string{id})
	if err != nil {
		return err
	}

	_, options, err := app.memoryIndex()
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	layout := c.FormValue("layout")
	if !slices.Contains(transactionMemoryLayouts, layout) {
		layout = transactionMemoryLayouts[0]
	}

	// The control that was used is swapped in place; its twin in the other
	// layout rides along out of band.
	controls := make([]map[string]interface{}, 0, len(transactionMemoryLayouts))
	for _, each := range transactionMemoryLayouts {
		controls = append(controls, map[string]interface{}{
			"ID":          id,
			"MemoryID":    memory.Id,
			"MemoryTitle": memory.Title,
			"Layout":      each,
			"Options":     options,
			"Kinds":       models.MemoryKinds,
			"OOB":         each != layout,
		})
	}

	app.Toast(c, "Saved", "Added to “"+memory.Title+"”.")
	return c.Render(http.StatusOK, "transactions.memory.swap", controls)
}
