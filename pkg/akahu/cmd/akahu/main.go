package main

import (
	"fmt"
	"github.com/TheQueenIsDead/akahu/pkg"
	log "github.com/sirupsen/logrus"
	"os"
)

func main() {
	userToken := os.Getenv("AKAHU_USER_TOKEN")
	appToken := os.Getenv("AKAHU_APP_TOKEN")

	if userToken == "" || appToken == "" {
		log.Error("Could not load env vars")
		return
	}

	akahu := pkg.NewClient(
		pkg.WithApptoken(appToken),
		pkg.WithUserToken(userToken),
	)

	akahu.Me()
	accounts := akahu.Accounts()
	for _, a := range accounts.Items {
		fmt.Println(a)
	}

}
