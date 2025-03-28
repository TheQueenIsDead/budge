package main

import (
	"fmt"
	"github.com/TheQueenIsDead/budge/pkg/integrations/akahu"
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

	me, err := client.Me()
	if err != nil {
		panic(err)
	}
	fmt.Println(me)

	accounts, err := client.GetAccounts()
	if err != nil {
		panic(err)
	}
	for _, a := range accounts {
		fmt.Println(a)
	}

}
