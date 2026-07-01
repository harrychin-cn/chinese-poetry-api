package database

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"
)

func TestQueryPoemsCompoundFilters(t *testing.T) {
	db := setupTestDB(t)
	repo := NewRepository(db)

	tangID, err := repo.GetOrCreateDynasty("唐")
	require.NoError(t, err)
	songID, err := repo.GetOrCreateDynasty("宋")
	require.NoError(t, err)

	liBaiID, err := repo.GetOrCreateAuthor("李白", tangID)
	require.NoError(t, err)
	mengHaoRanID, err := repo.GetOrCreateAuthor("孟浩然", tangID)
	require.NoError(t, err)
	suShiID, err := repo.GetOrCreateAuthor("苏轼", songID)
	require.NoError(t, err)

	wujue := &PoetryType{
		ID:           1001,
		Name:         "五言绝句",
		Category:     "唐诗",
		Lines:        intPtr(4),
		CharsPerLine: intPtr(5),
	}
	require.NoError(t, createTestPoetryType(repo, wujue))

	songCi := &PoetryType{
		ID:       1002,
		Name:     "宋词",
		Category: "宋词",
	}
	require.NoError(t, createTestPoetryType(repo, songCi))

	poems := []*Poem{
		{
			ID:        1,
			Title:     "静夜思",
			Content:   datatypes.JSON([]byte(`["床前明月光","疑是地上霜","举头望明月","低头思故乡"]`)),
			AuthorID:  &liBaiID,
			DynastyID: &tangID,
			TypeID:    &wujue.ID,
		},
		{
			ID:        2,
			Title:     "春晓",
			Content:   datatypes.JSON([]byte(`["春眠不觉晓","处处闻啼鸟","夜来风雨声","花落知多少"]`)),
			AuthorID:  &mengHaoRanID,
			DynastyID: &tangID,
			TypeID:    &wujue.ID,
		},
		{
			ID:        3,
			Title:     "水调歌头",
			Content:   datatypes.JSON([]byte(`["明月几时有","把酒问青天"]`)),
			AuthorID:  &suShiID,
			DynastyID: &songID,
			TypeID:    &songCi.ID,
		},
	}

	for _, poem := range poems {
		require.NoError(t, repo.InsertPoem(poem))
	}

	lines := 4
	charsPerLine := 5
	result, total, err := repo.QueryPoems(PoemQueryFilter{
		Keyword:      "明月",
		SearchIn:     "content",
		DynastyID:    &tangID,
		AuthorID:     &liBaiID,
		TypeIDs:      []int64{wujue.ID},
		Lines:        &lines,
		CharsPerLine: &charsPerLine,
		Page:         1,
		PageSize:     10,
		Sort:         "id_asc",
	})

	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	require.Len(t, result, 1)
	assert.Equal(t, "静夜思", result[0].Title)
	assert.NotNil(t, result[0].Author)
	assert.Equal(t, "李白", result[0].Author.Name)
	assert.NotNil(t, result[0].Dynasty)
	assert.Equal(t, "唐", result[0].Dynasty.Name)
	assert.NotNil(t, result[0].Type)
	assert.Equal(t, "五言绝句", result[0].Type.Name)
}

func TestQueryPoemsAuthorKeywordAndInvalidParams(t *testing.T) {
	db := setupTestDB(t)
	repo := NewRepository(db)

	tangID, err := repo.GetOrCreateDynasty("唐")
	require.NoError(t, err)
	liBaiID, err := repo.GetOrCreateAuthor("李白", tangID)
	require.NoError(t, err)
	duFuID, err := repo.GetOrCreateAuthor("杜甫", tangID)
	require.NoError(t, err)

	require.NoError(t, repo.InsertPoem(&Poem{
		ID:        11,
		Title:     "望庐山瀑布",
		Content:   datatypes.JSON([]byte(`["日照香炉生紫烟","遥看瀑布挂前川"]`)),
		AuthorID:  &liBaiID,
		DynastyID: &tangID,
	}))
	require.NoError(t, repo.InsertPoem(&Poem{
		ID:        12,
		Title:     "春望",
		Content:   datatypes.JSON([]byte(`["国破山河在","城春草木深"]`)),
		AuthorID:  &duFuID,
		DynastyID: &tangID,
	}))

	result, total, err := repo.QueryPoems(PoemQueryFilter{
		Keyword:  "李白",
		SearchIn: "author",
		Page:     1,
		PageSize: 20,
	})

	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	require.Len(t, result, 1)
	assert.Equal(t, "望庐山瀑布", result[0].Title)

	_, _, err = repo.QueryPoems(PoemQueryFilter{
		SearchIn: "raw_sql",
		Page:     1,
		PageSize: 20,
	})
	assert.True(t, errors.Is(err, ErrInvalidQueryParam))

	_, _, err = repo.QueryPoems(PoemQueryFilter{
		Sort: "drop_table",
		Page: 1,
	})
	assert.True(t, errors.Is(err, ErrInvalidQueryParam))
}

func intPtr(value int) *int {
	return &value
}
