package main

import (
	"net/http"
	"os"
	"time"

	// Pay cycles and spending days are read in local time, set by TZ. The
	// image may have no zone database of its own, so carry one.
	_ "time/tzdata"

	"github.com/TheQueenIsDead/budge/pkg"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(healthcheck())
	}

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

// healthcheck asks the running server's /healthz, for the image's HEALTHCHECK:
// distroless has no shell or curl to do it with. 0 is healthy.
func healthcheck() int {
	c := &http.Client{Timeout: 3 * time.Second}
	resp, err := c.Get("http://127.0.0.1:1337/healthz")
	if err != nil {
		return 1
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}
