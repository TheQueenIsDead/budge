package akahu

import (
	"encoding/json"
	"fmt"
	"io"
	"time"
)

type Me struct {
	Success bool `json:"success"`
	Item    struct {
		Id              string    `json:"_id"`
		AccessGrantedAt time.Time `json:"access_granted_at"`
		Email           string    `json:"email"`
	} `json:"item"`
}

func (a *AkahuClient) Me() {
	res, err := a.Get("/me")
	if err != nil {
		return
	}

	body, _ := io.ReadAll(res.Body)
	var me *Me
	json.Unmarshal(body, &me)
	fmt.Println(me)

}
