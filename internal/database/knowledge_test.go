package database

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"
)

func TestResolveExistingTagIDsAndListTagsByPoemIDs(t *testing.T) {
	db := setupTestDB(t)
	require.NoError(t, db.migrateTagTables())
	repo := NewRepository(db)

	tangID, err := repo.GetOrCreateDynasty("唐")
	require.NoError(t, err)
	liBaiID, err := repo.GetOrCreateAuthor("李白", tangID)
	require.NoError(t, err)

	require.NoError(t, repo.InsertPoem(&Poem{
		ID:        201,
		Title:     "静夜思",
		Content:   datatypes.JSON([]byte(`["床前明月光","低头思故乡"]`)),
		AuthorID:  &liBaiID,
		DynastyID: &tangID,
	}))

	_, err = repo.AssignTagsToPoem(201, []TagInput{
		{Name: "思乡", Category: "theme", Source: "manual"},
		{Name: "月亮", Category: "theme", Source: "manual"},
	})
	require.NoError(t, err)

	ids, resolved, err := repo.ResolveExistingTagIDsForQuery([]TagInput{
		{Name: "思乡", Category: "theme"},
		{Name: "不存在", Category: "theme"},
	})
	require.NoError(t, err)
	assert.Len(t, ids, 1)
	require.Len(t, resolved, 1)
	assert.Equal(t, "思乡", resolved[0].Name)

	tagsByPoemID, err := repo.ListTagsByPoemIDs([]int64{201})
	require.NoError(t, err)
	require.Len(t, tagsByPoemID[201], 2)
	assert.ElementsMatch(t, []string{"月亮", "思乡"}, []string{
		tagsByPoemID[201][0].Name,
		tagsByPoemID[201][1].Name,
	})
}
