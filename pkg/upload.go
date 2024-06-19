package pkg

import (
	"fmt"
	"github.com/labstack/echo/v4"
	"io"
	"os"
)

const (
	FileDir = "./uploads"
)

func saveFile(c echo.Context) (string, error) {

	// Source
	file, err := c.FormFile("file")
	if err != nil {
		return "", err
	}
	src, err := file.Open()
	if err != nil {
		return "", err
	}
	defer src.Close()

	// Destination
	filepath := fmt.Sprintf("./uploads/%s", file.Filename)
	dst, err := os.Create(filepath)
	if err != nil {
		return "", err
	}
	defer dst.Close()

	// Copy
	if _, err = io.Copy(dst, src); err != nil {
		return "", err
	}

	return filepath, nil
}

func parseFile(c echo.Context, filepath string) ([]KiwibankExportRow, error) {
	return KiwibankParser{}.ParseCSV(filepath)
}
