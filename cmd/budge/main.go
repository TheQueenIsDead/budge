package main

import (
	// Pay cycles and spending days are read in local time, set by TZ. The
	// image may have no zone database of its own, so carry one.
	_ "time/tzdata"

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
		err := budge.Teardown()
		if err != nil {
			panic(err)
		}
	}
}
