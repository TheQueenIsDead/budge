package application

import (
	"encoding/json"
	"github.com/labstack/echo/v4"
)

func LoggingMiddleware(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		if err := next(c); err != nil {
			c.Logger().Error(err)
			return err
		}
		return nil
	}
}

func ErrorToast(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		if err := next(c); err != nil {
			toast := map[string]interface{}{
				//"eventName": "error",
				"error": err.Error(),
			}
			buf, err := json.Marshal(toast)
			if err != nil {
				return err
			}
			c.Response().Header().Add("Hx-Trigger", string(buf))
			c.Response().Header().Add("Hx-Reswap", "none")
			c.Logger().Error(err)
			return err
		}
		return nil
	}
}
