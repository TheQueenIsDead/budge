package database

import (
	"testing"
	"time"

	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	bolt "go.etcd.io/bbolt"
)

func TestAkahuSettingsNeverSavedAreEmpty(t *testing.T) {
	store := tempStore(t)
	settings, err := store.GetAkahuSettings(t.Context())
	require.NoError(t, err, "a fresh instance has no settings, which is not an error")
	assert.Empty(t, settings.UserToken)

	through := time.Now().Add(-time.Hour)
	require.NoError(t, store.RecordAkahuSync(t.Context(), through), "a first sync records itself")
	settings, err = store.GetAkahuSettings(t.Context())
	require.NoError(t, err)
	assert.True(t, settings.DataThrough.Equal(through))
}

func TestAkahuSettingsThatCannotBeReadAreAnError(t *testing.T) {
	store := tempStore(t)
	key := models.IntegrationAkahuSettings{}.Key()
	require.NoError(t, store.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(models.IntegrationAkahuSettings{}.Bucket()).Put(key, []byte("{not json"))
	}))
	_, err := store.GetAkahuSettings(t.Context())
	assert.Error(t, err)
}
