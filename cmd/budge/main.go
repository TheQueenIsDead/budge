package main

import (
	"github.com/TheQueenIsDead/budge/pkg"
)

func main() {
	// Setup application container
	budge, err := pkg.NewBudge()
	if err != nil {
		panic(err)
	}
	err = budge.Start()
	if err != nil {
		budge.Logger.Error(err.Error())
		budge.Teardown()
	}
}
