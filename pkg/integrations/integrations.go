package integrations

import (
	"github.com/TheQueenIsDead/budge/pkg/integrations/akahu"
	"os"
)

type Integrations struct {
	akahu *akahu.AkahuClient
}

type Integration interface {
	Config() map[string]string
}

func NewIntegrations() *Integrations {

	i := &Integrations{}
	i.RegisterAkahu()

	return i
}

func (i *Integrations) RegisterAkahu() {
	i.akahu = akahu.NewClient(
		akahu.WithUserToken(os.Getenv("AKAHU_USER_TOKEN")),
		akahu.WithApptoken(os.Getenv("AKAHU_APP_TOKEN")),
	)
}

func (i *Integrations) Config() map[string]interface{} {

	return map[string]interface{}{
		"akahu": i.akahu.Config(),
	}
}
