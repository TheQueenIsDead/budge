package application

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/labstack/echo/v4"
)

// backupMaxBytes bounds an uploaded backup. A real one is a few hundred
// kilobytes at most; anything far bigger is the wrong file.
const backupMaxBytes = 10 << 20

// SettingsExport downloads everything set up by hand as a JSON file, for
// importing into another instance.
func (app *Application) SettingsExport(c echo.Context) error {
	backup, err := app.store.ExportBackup()
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	name := fmt.Sprintf("budge-settings-%s.json", backup.ExportedAt.Format(time.DateOnly))
	c.Response().Header().Set(echo.HeaderContentDisposition, fmt.Sprintf("attachment; filename=%q", name))
	return c.JSONPretty(http.StatusOK, backup, "  ")
}

// SettingsImport reads a backup into this instance and shows the settings page
// as it now stands.
func (app *Application) SettingsImport(c echo.Context) error {
	file, err := c.FormFile("backup")
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Choose a backup file to import.")
	}
	src, err := file.Open()
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	defer src.Close()

	backup, err := readBackup(io.LimitReader(src, backupMaxBytes))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	if err := app.store.ImportBackup(backup); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	counts := backup.Counts()
	message := fmt.Sprintf("Imported %d memories, %d assets and %d budget items.", counts.Memories, counts.Assets, counts.BudgetItems)
	app.Toast(c, "Imported", message)
	return app.Settings(c)
}

// readBackup decodes a backup file, refusing anything that is not one this
// build can read.
func readBackup(r io.Reader) (models.Backup, error) {
	var backup models.Backup
	if err := json.NewDecoder(r).Decode(&backup); err != nil {
		return backup, fmt.Errorf("That file is not a Budge backup: %w", err)
	}
	if backup.Version == 0 {
		return backup, fmt.Errorf("That file is not a Budge backup.")
	}
	if backup.Version > models.BackupVersion {
		return backup, fmt.Errorf("That backup is from a newer version of Budge. Update this instance first.")
	}
	return backup, nil
}
