package database

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupFeedbackTestRepo(t *testing.T) *Repository {
	t.Helper()
	gormDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	db := NewDBFromGorm(gormDB)
	require.NoError(t, db.Migrate())
	return NewRepository(db)
}

func TestCreateListUpdateFeedback(t *testing.T) {
	repo := setupFeedbackTestRepo(t)
	key, _, err := repo.CreateAPIKey(CreateAPIKeyParams{Name: "feedback customer", DailyLimit: 10})
	require.NoError(t, err)

	item, err := repo.CreateFeedback(CreateFeedbackParams{
		APIKeyID: key.ID,
		Type:     "data",
		Subject:  "missing poem",
		Message:  "希望补更多中秋诗句",
		Contact:  "wechat",
	})
	require.NoError(t, err)
	assert.Equal(t, "data", item.Type)
	assert.Equal(t, "open", item.Status)

	items, err := repo.ListFeedback("open", &key.ID, 10)
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, item.ID, items[0].ID)

	status := "resolved"
	notes := "已加入增强队列"
	updated, err := repo.UpdateFeedback(item.ID, UpdateFeedbackParams{
		Status:     &status,
		AdminNotes: &notes,
	})
	require.NoError(t, err)
	assert.Equal(t, "resolved", updated.Status)
	assert.Equal(t, notes, updated.AdminNotes)
}

func TestCreateFeedbackRequiresMessage(t *testing.T) {
	repo := setupFeedbackTestRepo(t)
	_, err := repo.CreateFeedback(CreateFeedbackParams{
		APIKeyID: 1,
		Message:  " ",
	})
	assert.ErrorIs(t, err, ErrInvalidQueryParam)
}
