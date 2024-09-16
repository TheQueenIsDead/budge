package application

import "github.com/labstack/echo/v4"

func LoggingMiddleware(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		if err := next(c); err != nil {
			c.Logger().Error(err)
			return err
		}
		return nil
	}
}
