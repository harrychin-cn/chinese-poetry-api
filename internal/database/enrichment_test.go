package database

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"
)

func TestEnrichmentReviewAcceptRejectAndEmbedding(t *testing.T) {
	db := setupTestDB(t)
	require.NoError(t, db.migrateTagTables())
	require.NoError(t, db.migrateKnowledgeTables())
	repo := NewRepository(db)

	tangID, err := repo.GetOrCreateDynasty("唐")
	require.NoError(t, err)
	liBaiID, err := repo.GetOrCreateAuthor("李白", tangID)
	require.NoError(t, err)
	require.NoError(t, repo.InsertPoem(&Poem{
		ID:        501,
		Title:     "静夜思",
		Content:   datatypes.JSON([]byte(`["床前明月光","疑是地上霜","举头望明月","低头思故乡"]`)),
		AuthorID:  &liBaiID,
		DynastyID: &tangID,
	}))

	job, err := repo.CreateEnrichmentJob(CreateEnrichmentJobParams{
		Scope:      "sample-100",
		TotalCount: 2,
		Config:     map[string]any{"run_id": "enrich-test"},
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), job.ID)

	item, err := repo.CreateEnrichmentReviewItem(CreateReviewItemParams{
		JobID:  &job.ID,
		PoemID: 501,
		ProposedTags: []TagInput{
			{Name: "思乡", Category: "theme", Source: "ai"},
			{Name: "月亮", Category: "image", Source: "ai"},
		},
		ProposedKnowledge: ProposedKnowledgeInput{
			Summary:        "诗人借月色表达客居思乡之情。",
			Translation:    "床前月光皎洁，诗人抬头望月，低头思念故乡。",
			Annotation:     "明月是触发乡思的核心意象。",
			Recommendation: "适合中秋、思乡、月亮意象场景引用。",
			Source:         "ai",
		},
	})
	require.NoError(t, err)
	assert.Equal(t, EnrichmentStatusPending, item.Status)

	items, err := repo.ListEnrichmentReviewItems(EnrichmentStatusPending, 10)
	require.NoError(t, err)
	require.Len(t, items, 1)

	accepted, err := repo.AcceptEnrichmentReviewItem(item.ID, ReviewDecisionParams{
		Reviewer: "operator",
		Notes:    "抽检通过",
	})
	require.NoError(t, err)
	assert.Equal(t, EnrichmentStatusAccepted, accepted.Status)

	tagsByPoemID, err := repo.ListTagsByPoemIDs([]int64{501})
	require.NoError(t, err)
	require.Len(t, tagsByPoemID[501], 2)

	knowledge, err := repo.GetPoemKnowledge(501)
	require.NoError(t, err)
	assert.Equal(t, EnrichmentStatusAccepted, knowledge.QualityStatus)
	assert.Equal(t, "诗人借月色表达客居思乡之情。", knowledge.Summary)
	assert.Equal(t, "operator", knowledge.Reviewer)

	jobs, err := repo.ListEnrichmentJobs(10)
	require.NoError(t, err)
	require.Len(t, jobs, 1)
	assert.Equal(t, 1, jobs[0].ProcessedCount)
	assert.Equal(t, 1, jobs[0].AcceptedCount)
	assert.Equal(t, 0, jobs[0].RejectedCount)

	rejectedItem, err := repo.CreateEnrichmentReviewItem(CreateReviewItemParams{
		JobID:  &job.ID,
		PoemID: 501,
		ProposedTags: []TagInput{
			{Name: "错误标签", Category: "theme", Source: "ai"},
		},
	})
	require.NoError(t, err)
	rejected, err := repo.RejectEnrichmentReviewItem(rejectedItem.ID, ReviewDecisionParams{
		Reviewer: "operator",
		Notes:    "与原文不符",
	})
	require.NoError(t, err)
	assert.Equal(t, EnrichmentStatusRejected, rejected.Status)

	jobs, err = repo.ListEnrichmentJobs(10)
	require.NoError(t, err)
	assert.Equal(t, 2, jobs[0].ProcessedCount)
	assert.Equal(t, 1, jobs[0].AcceptedCount)
	assert.Equal(t, 1, jobs[0].RejectedCount)

	correctable, err := repo.CreateEnrichmentReviewItem(CreateReviewItemParams{
		JobID:  &job.ID,
		PoemID: 501,
		ProposedTags: []TagInput{
			{Name: "泛化标签", Category: "theme", Source: "ai"},
		},
		ProposedKnowledge: ProposedKnowledgeInput{Summary: "原始候选较泛，需要人工修正。"},
	})
	require.NoError(t, err)
	corrected, err := repo.CorrectEnrichmentReviewItem(
		correctable.ID,
		[]TagInput{{Name: "乡愁", Category: "mood", Source: "manual"}},
		ProposedKnowledgeInput{Summary: "人工修正后聚焦明月触发乡愁。", Source: "manual"},
		ReviewDecisionParams{Reviewer: "operator", Notes: "人工修正"},
	)
	require.NoError(t, err)
	assert.Equal(t, EnrichmentStatusPending, corrected.Status)
	assert.Contains(t, corrected.ProposedTagsJSON, "乡愁")

	runPendingItems, err := repo.ListEnrichmentReviewItemsForRun("enrich-test", EnrichmentStatusPending, 10)
	require.NoError(t, err)
	require.Len(t, runPendingItems, 1)
	assert.Equal(t, correctable.ID, runPendingItems[0].ID)

	summary, err := repo.GetEnrichmentRunSummary("enrich-test")
	require.NoError(t, err)
	assert.Equal(t, 3, summary.TotalItems)
	assert.Equal(t, 1, summary.PendingCount)
	assert.Equal(t, 1, summary.AcceptedCount)
	assert.Equal(t, 1, summary.RejectedCount)
	assert.Equal(t, 2, summary.ReviewedCount)
	assert.InDelta(t, 0.5, summary.PassRate, 0.0001)
	require.Len(t, summary.RejectedNoteTop10, 1)
	assert.Equal(t, "与原文不符", summary.RejectedNoteTop10[0].Note)
	assert.Equal(t, 1, summary.RejectedNoteTop10[0].Count)

	vector, err := json.Marshal([]float64{0.1, 0.2, 0.3})
	require.NoError(t, err)
	err = repo.UpsertPoemEmbedding(PoemEmbedding{
		PoemID:      501,
		Provider:    "qanlo",
		Model:       "text-embedding-test",
		Dimension:   3,
		VectorJSON:  string(vector),
		ContentHash: "hash-1",
	})
	require.NoError(t, err)

	rollback, err := repo.RollbackEnrichmentJob("enrich-test", ReviewDecisionParams{
		Reviewer: "operator",
		Notes:    "测试回滚",
	})
	require.NoError(t, err)
	assert.Equal(t, 1, rollback.ReviewItems)
	assert.Equal(t, 1, rollback.PoemsAffected)
	assert.Equal(t, int64(1), rollback.KnowledgeRows)
	assert.Equal(t, int64(2), rollback.TagsRemoved)
}
