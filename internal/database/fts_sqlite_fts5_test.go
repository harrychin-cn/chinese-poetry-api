//go:build sqlite_fts5

package database

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"
)

func TestSearchPoemsFTSWithCompoundFilters(t *testing.T) {
	db := setupTestDB(t)
	require.NoError(t, db.migrateTagTables())
	require.NoError(t, db.migrateFTSTableForLang(LangHans))
	repo := NewRepository(db)

	tangID, err := repo.GetOrCreateDynasty("唐")
	require.NoError(t, err)
	liBaiID, err := repo.GetOrCreateAuthor("李白", tangID)
	require.NoError(t, err)
	duFuID, err := repo.GetOrCreateAuthor("杜甫", tangID)
	require.NoError(t, err)

	require.NoError(t, repo.InsertPoem(&Poem{
		ID:        201,
		Title:     "静夜思",
		Content:   datatypes.JSON([]byte(`["床前明月光","低头思故乡"]`)),
		AuthorID:  &liBaiID,
		DynastyID: &tangID,
	}))
	require.NoError(t, repo.InsertPoem(&Poem{
		ID:        202,
		Title:     "春望",
		Content:   datatypes.JSON([]byte(`["国破山河在","城春草木深"]`)),
		AuthorID:  &duFuID,
		DynastyID: &tangID,
	}))

	_, err = repo.AssignTagsToPoem(201, []TagInput{{Name: "思乡", Category: "theme"}})
	require.NoError(t, err)

	require.NoError(t, repo.RebuildPoemFTSIndex())

	tagIDs, err := repo.ResolveTagIDsForQuery([]TagInput{{Name: "思乡", Category: "theme"}})
	require.NoError(t, err)
	results, total, err := repo.SearchPoemsFTS(PoemSearchFilter{
		Keyword:   "明月",
		SearchIn:  "content",
		DynastyID: &tangID,
		TagIDs:    tagIDs,
		Page:      1,
		PageSize:  10,
	})

	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	require.Len(t, results, 1)
	assert.Equal(t, "静夜思", results[0].Poem.Title)
	assert.Contains(t, results[0].HitFields, "content")
	assert.Contains(t, results[0].Snippets["content"], "明月")

	results, total, err = repo.SearchPoemsFTS(PoemSearchFilter{
		Keyword:  "李白",
		SearchIn: "author",
		Page:     1,
		PageSize: 10,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	require.Len(t, results, 1)
	assert.Equal(t, "静夜思", results[0].Poem.Title)
}
