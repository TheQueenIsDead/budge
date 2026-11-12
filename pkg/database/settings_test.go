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

func TestDeleteSyncedEmptiesTheBucketsAndForgetsTheSync(t *testing.T) {
	store := tempStore(t)
	ctx := t.Context()
	require.NoError(t, store.UpdateAkahuSettings(ctx, models.IntegrationAkahuSettings{UserToken: "user", AppToken: "app"}))
	require.NoError(t, store.RecordAkahuSync(ctx, time.Now()))
	require.NoError(t, store.SaveSynced(ctx, []models.Account{{Id: "acc"}}, []models.Transaction{{Id: "tx"}}, []models.Merchant{{Id: "m"}}))

	require.NoError(t, store.DeleteSynced(ctx))

	count, err := store.CountTransactions(ctx)
	require.NoError(t, err)
	assert.Zero(t, count)
	settings, err := store.GetAkahuSettings(ctx)
	require.NoError(t, err)
	assert.True(t, settings.DataThrough.IsZero(), "the next sync reads the whole history")
	assert.True(t, settings.LastSync.IsZero())
	assert.Equal(t, "user", settings.UserToken, "the tokens are kept")

	// The buckets are there to write into straight away.
	require.NoError(t, store.SaveSynced(ctx, nil, []models.Transaction{{Id: "tx"}}, nil))
}

func TestDeleteSyncedToleratesAMissingBucket(t *testing.T) {
	store := tempStore(t)
	require.NoError(t, store.db.Update(func(tx *bolt.Tx) error {
		return tx.DeleteBucket(models.Transaction{}.Bucket())
	}))
	require.NoError(t, store.DeleteSynced(t.Context()))
	_, err := store.CountTransactions(t.Context())
	assert.NoError(t, err, "made again")
}

func TestPutAllReportsAMissingBucket(t *testing.T) {
	store := tempStore(t)
	err := store.db.Update(func(tx *bolt.Tx) error {
		require.NoError(t, tx.DeleteBucket(models.Transaction{}.Bucket()))
		return putAll(tx, []models.Transaction{{Id: "tx"}})
	})
	assert.ErrorContains(t, err, "not found", "an error, not a panic")
}
