package database

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUserAccountAutoCreatedForAPIKeyAndPublicWorks(t *testing.T) {
	repo, key := setupWorksTestRepo(t)
	require.NotNil(t, key.AccountID)

	account, err := repo.GetOrCreateUserAccountForAPIKey(key.ID)
	require.NoError(t, err)
	assert.Equal(t, *key.AccountID, account.ID)
	assert.NotEmpty(t, account.Handle)

	display := "\u5c71\u7a97\u4f5c\u8005"
	bio := "\u5199\u5c71\u6c34\u4e0e\u706f\u5f71\u3002"
	handle := "mountain-window"
	updated, err := repo.UpdateUserAccountForAPIKey(key.ID, UpdateUserAccountParams{
		Handle:      &handle,
		DisplayName: &display,
		Bio:         &bio,
	})
	require.NoError(t, err)
	assert.Equal(t, handle, updated.Handle)
	assert.Equal(t, display, updated.DisplayName)

	work, err := repo.CreateOriginalWork(CreateOriginalWorkParams{
		APIKeyID:           key.ID,
		Title:              "\u5c71\u7a97\u591c\u5750",
		Content:            "\u5c71\u7a97\u706f\u5f71\u7ec6\n\u4e00\u76cf\u7167\u6e05\u98ce",
		OriginalCommitment: true,
		LicenseAccepted:    true,
		Publish:            true,
	})
	require.NoError(t, err)
	assert.Equal(t, WorkStatusPublished, work.Status)

	publicAccount, err := repo.GetPublicUserAccountByHandle("MOUNTAIN-WINDOW")
	require.NoError(t, err)
	assert.Equal(t, updated.ID, publicAccount.ID)

	count, err := repo.CountPublicOriginalWorksByAccount(updated.ID)
	require.NoError(t, err)
	assert.EqualValues(t, 1, count)

	works, err := repo.ListPublicOriginalWorksByAccount(updated.ID, 20)
	require.NoError(t, err)
	require.Len(t, works, 1)
	assert.Equal(t, work.ID, works[0].ID)
}

func TestUserAccountRejectsDuplicateHandle(t *testing.T) {
	repo, key1 := setupWorksTestRepo(t)
	key2, _, err := repo.CreateAPIKey(CreateAPIKeyParams{Name: "another customer", DailyLimit: 10})
	require.NoError(t, err)

	handle := "same-handle"
	_, err = repo.UpdateUserAccountForAPIKey(key1.ID, UpdateUserAccountParams{Handle: &handle})
	require.NoError(t, err)

	_, err = repo.UpdateUserAccountForAPIKey(key2.ID, UpdateUserAccountParams{Handle: &handle})
	assert.ErrorIs(t, err, ErrUserHandleTaken)
}
