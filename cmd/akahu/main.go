package main

import (
	"fmt"
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

	client := akahu.NewClient(
		akahu.WithApptoken(appToken),
		akahu.WithUserToken(userToken),
	)

	client.Me()
	accounts := client.Accounts()
	for _, a := range accounts.Items {
		fmt.Println(a)
	}

}
