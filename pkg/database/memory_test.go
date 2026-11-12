package database

import (
	"testing"

	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	bolt "go.etcd.io/bbolt"
)

func TestSaveMemoryTakesPurchasesFromOtherMemories(t *testing.T) {
	store := tempStore(t)
	ctx := t.Context()
	require.NoError(t, store.SaveMemory(ctx, models.Memory{Id: "trip", TransactionIDs: []string{"flight", "hotel"}}))
	require.NoError(t, store.SaveMemory(ctx, models.Memory{Id: "flights", TransactionIDs: []string{"only-flight"}}))

	require.NoError(t, store.SaveMemory(ctx, models.Memory{Id: "work", TransactionIDs: []string{"flight", "only-flight"}}))

	trip, err := store.GetMemory(ctx, "trip")
	require.NoError(t, err)
	assert.Equal(t, []string{"hotel"}, trip.TransactionIDs)
	memories, err := store.ReadMemories(ctx)
	require.NoError(t, err)
	assert.Len(t, memories, 2, "the memory left empty is removed")
}

func TestSaveMemoryChangesNothingWhenItFails(t *testing.T) {
	store := tempStore(t)
	ctx := t.Context()
	require.NoError(t, store.SaveMemory(ctx, models.Memory{Id: "a-trip", TransactionIDs: []string{"flight", "hotel"}}))
	// Read after the trip, and unreadable, so the save fails part way.
	require.NoError(t, store.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(models.Memory{}.Bucket()).Put([]byte("z-broken"), []byte("{not json"))
	}))

	require.Error(t, store.SaveMemory(ctx, models.Memory{Id: "work", TransactionIDs: []string{"flight"}}))

	trip, err := store.GetMemory(ctx, "a-trip")
	require.NoError(t, err)
	assert.Equal(t, []string{"flight", "hotel"}, trip.TransactionIDs, "the trip keeps its flight")
}
