package database

import (
	"encoding/json"
	"errors"
	"github.com/TheQueenIsDead/budge/pkg/database/buckets"
	"github.com/TheQueenIsDead/budge/pkg/database/models"
	bolt "go.etcd.io/bbolt"
)

type SettingsStore interface {
	GetAkahuSettings() (models.IntegrationAkahuSettings, error)
	PutAkahuSettings(models.IntegrationAkahuSettings) error
}

type settingsKeys struct {
	akahu []byte
}

type SettingsStorer struct {
	db     *bolt.DB
	bucket []byte

	keys settingsKeys
}

var _ SettingsStore = (*SettingsStorer)(nil)

func NewSettingsStorer(db *bolt.DB) *SettingsStorer {
	return &SettingsStorer{
		db:     db,
		bucket: buckets.SettingsBucket,
		keys: settingsKeys{
			akahu: []byte("akahu"),
		},
	}
}

func (s *SettingsStorer) GetAkahuSettings() (models.IntegrationAkahuSettings, error) {
	var akahuSettings models.IntegrationAkahuSettings

	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(s.bucket)
		buf := b.Get(s.keys.akahu)
		if buf == nil {
			return errors.New("not found")
		}
		err := json.Unmarshal(buf, &akahuSettings)
		if err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return akahuSettings, err
	}

	return akahuSettings, nil
}

func (s *SettingsStorer) PutAkahuSettings(settings models.IntegrationAkahuSettings) error {
	err := s.db.Update(func(tx *bolt.Tx) (txErr error) {
		b := tx.Bucket(s.bucket)
		buf, err := json.Marshal(&settings)
		if err != nil {
			return err
		}
		return b.Put(s.keys.akahu, buf)
	})
	return err
}
