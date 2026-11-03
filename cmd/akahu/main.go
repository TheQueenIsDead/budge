package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/TheQueenIsDead/budge/pkg/integrations/akahu"
)

func main() {
	userToken := os.Getenv("AKAHU_USER_TOKEN")
	appToken := os.Getenv("AKAHU_APP_TOKEN")

	if userToken == "" || appToken == "" {
		slog.Error("AKAHU_USER_TOKEN and AKAHU_APP_TOKEN must both be set")
		return
	}

	client := akahu.NewClient(
		akahu.WithApptoken(appToken),
		akahu.WithUserToken(userToken),
	)

	me, err := client.Me(context.Background())
	if err != nil {
		panic(err)
	}
	fmt.Println(me)

	accounts, err := client.GetAccounts(context.Background())
	if err != nil {
		panic(err)
	}
	for _, a := range accounts {
		fmt.Println(a)
	}

}
