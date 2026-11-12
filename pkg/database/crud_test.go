package database

import (
	"testing"

	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	bolt "go.etcd.io/bbolt"
)

func TestGetSaysWhenNothingIsThere(t *testing.T) {
	store := tempStore(t)

	_, err := store.GetAsset(t.Context(), "no-such-asset")
	assert.ErrorIs(t, err, ErrNotFound)

	// A document that is there but cannot be read is a different failure.
	require.NoError(t, store.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(models.Asset{}.Bucket()).Put([]byte("broken"), []byte("{not json"))
	}))
	_, err = store.GetAsset(t.Context(), "broken")
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrNotFound)

	require.NoError(t, store.CreateAsset(t.Context(), models.Asset{Id: "house", Name: "House"}))
	asset, err := store.GetAsset(t.Context(), "house")
	require.NoError(t, err)
	assert.Equal(t, "House", asset.Name)
}
