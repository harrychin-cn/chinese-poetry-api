package database

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"
)

func TestTagsAndQueryPoemsByTags(t *testing.T) {
	db := setupTestDB(t)
	require.NoError(t, db.migrateTagTables())
	repo := NewRepository(db)

	tangID, err := repo.GetOrCreateDynasty("唐")
	require.NoError(t, err)
	liBaiID, err := repo.GetOrCreateAuthor("李白", tangID)
	require.NoError(t, err)
	duFuID, err := repo.GetOrCreateAuthor("杜甫", tangID)
	require.NoError(t, err)

	require.NoError(t, repo.InsertPoem(&Poem{
		ID:        101,
		Title:     "静夜思",
		Content:   datatypes.JSON([]byte(`["床前明月光","低头思故乡"]`)),
		AuthorID:  &liBaiID,
		DynastyID: &tangID,
	}))
	require.NoError(t, repo.InsertPoem(&Poem{
		ID:        102,
		Title:     "春望",
		Content:   datatypes.JSON([]byte(`["国破山河在","城春草木深"]`)),
		AuthorID:  &duFuID,
		DynastyID: &tangID,
	}))

	tags, err := repo.AssignTagsToPoem(101, []TagInput{
		{Name: "思乡", Category: "theme", Source: "manual"},
		{Name: "月亮", Category: "theme", Source: "manual"},
	})
	require.NoError(t, err)
	assert.Len(t, tags, 2)

	_, err = repo.AssignTagsToPoem(102, []TagInput{
		{Name: "春天", Category: "theme", Source: "manual"},
	})
	require.NoError(t, err)

	allTags, err := repo.ListTags("")
	require.NoError(t, err)
	assert.Len(t, allTags, 3)

	result, total, err := repo.QueryPoemsByTags(PoemQueryFilter{
		Page:     1,
		PageSize: 10,
		Sort:     "id_asc",
	}, []TagInput{
		{Name: "思乡", Category: "theme"},
		{Name: "月亮", Category: "theme"},
	})

	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	require.Len(t, result, 1)
	assert.Equal(t, "静夜思", result[0].Title)

	result, total, err = repo.QueryPoemsByTags(PoemQueryFilter{
		Page:     1,
		PageSize: 10,
	}, []TagInput{
		{Name: "春天", Category: "theme"},
	})

	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	require.Len(t, result, 1)
	assert.Equal(t, "春望", result[0].Title)
}
