package application

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

//
//func (app *Application) Upload(c echo.Context) error {
//
//	filepath, err := saveFile(c)
//	if err != nil {
//		c.Logger().Error(err)
//		return c.HTML(http.StatusInternalServerError, err.Error())
//	}
//
//	account, merchants, transactions, err := database.ParseCSV(c, filepath)
//	if err != nil {
//		c.Logger().Error(err)
//		return c.HTML(http.StatusInternalServerError, err.Error())
//	}
//
//	err = Import(app.DB, account, merchants, transactions)
//	if err != nil {
//		c.Logger().Error(err)
//		return err
//	}
//
//	// TODO: Redirect the user to a more pertinent page.
//	return c.Render(http.StatusOK, "home", nil)
//
//	//return c.HTML(http.StatusOK, strconv.Itoa(len(transactions)))
//	//return c.HTML(http.StatusOK, fmt.Sprintf("<p>File %s uploaded successfully.</p>", file.Filename))
//}
