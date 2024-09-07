package application

import (
	"github.com/labstack/echo/v4"
	"net/http"
)

func (app *Application) GetMerchant(c echo.Context) error {

	id := c.Param("id")
	if id != "" {
		return c.HTML(http.StatusBadRequest, "id is required")
	}
	merchants, err := app.store.Merchants.Get(id)
	if err != nil {
		// TODO: Nice error handling
		return c.HTML(500, err.Error())
	}

	return c.Render(http.StatusOK, "merchants", merchants)
}

func (app *Application) ListMerchants(c echo.Context) error {
	merchants, _ := app.store.Merchants.List()
	return c.Render(http.StatusOK, "merchants", merchants)
}

//func (app *Application) EditMerchant(c echo.Context) error {
//
//	id, err := strconv.Atoi(c.Param("id"))
//	if err != nil {
//		return c.HTML(http.StatusBadRequest, "id is required")
//	}
//
//	var merchant Merchant
//	if err = app.DB.First(&merchant, id).Error; err != nil {
//		return c.HTML(http.StatusNotFound, "error finding merchant")
//	}
//
//	return c.Render(http.StatusOK, "merchant_edit", merchant)
//}
//
//func (app *Application) PutMerchant(c echo.Context) error {
//
//	id, err := strconv.Atoi(c.Param("id"))
//	if err != nil {
//		return c.HTML(http.StatusBadRequest, "id is required")
//	}
//
//	name := c.FormValue("name")
//	description := c.FormValue("description")
//
//	var merchant Merchant
//	if err := app.DB.First(&merchant, id).Error; err != nil {
//		return c.HTML(http.StatusNotFound, "error finding merchant")
//	}
//
//	if name != "" {
//		merchant.Name = name
//	}
//	if description != "" {
//		merchant.Description = description
//	}
//
//	if err = app.DB.Save(merchant).Error; err != nil {
//		return c.HTML(http.StatusNotFound, "error saving merchant")
//	}
//
//	return c.Render(http.StatusOK, "merchant_row", merchant)
//}
//
//func (app *Application) GetMerchant(c echo.Context) error {
//
//	id, err := strconv.Atoi(c.Param("id"))
//	if err != nil {
//		return c.HTML(http.StatusBadRequest, "id is required")
//	}
//
//	var merchant Merchant
//	if err := app.DB.First(&merchant, id).Error; err != nil {
//		return c.HTML(http.StatusNotFound, "error finding merchant")
//	}
//
//	return c.Render(http.StatusOK, "merchant_row", merchant)
//}
//
