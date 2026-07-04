package database

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPlagiarismSemanticCorpusAndManualReview(t *testing.T) {
	repo, key := setupWorksTestRepo(t)

	source, err := repo.CreatePlagiarismCorpusSource(CreatePlagiarismCorpusSourceParams{
		SourceType: PlagiarismCorpusTypeDispute,
		Title:      "Disputed source",
		SourceURL:  "https://example.test/dispute/1",
		Content:    "alpha beta gamma delta epsilon zeta eta theta",
		CreatedBy:  "tester",
	})
	require.NoError(t, err)
	assert.NotZero(t, source.ID)
	assert.Equal(t, PlagiarismCorpusTypeDispute, source.SourceType)
	assert.NotEmpty(t, source.EmbeddingJSON)

	work, err := repo.CreateOriginalWork(CreateOriginalWorkParams{
		APIKeyID:           key.ID,
		Title:              "Reworked source",
		WorkType:           "poem",
		Content:            "theta eta zeta epsilon delta gamma beta alpha",
		OriginalCommitment: true,
		LicenseAccepted:    true,
		Publish:            true,
	})
	require.NoError(t, err)
	assert.Equal(t, WorkStatusReviewRequired, work.Status)
	assert.Equal(t, WorkVisibilityPrivate, work.Visibility)
	assert.Equal(t, PlagiarismStatusHighRisk, work.PlagiarismStatus)

	report, err := repo.LatestPlagiarismReport(key.ID, work.ID)
	require.NoError(t, err)
	assert.Equal(t, PlagiarismRiskHigh, report.Report.RiskLevel)
	require.NotEmpty(t, report.Matches)
	assert.Equal(t, "embedding_semantic", report.Matches[0].MatchType)
	assert.Equal(t, PlagiarismCorpusTypeDispute, report.Matches[0].SourceType)

	queue, err := repo.ListManualReviewQueue(ManualReviewStatusPending, 10)
	require.NoError(t, err)
	require.Len(t, queue, 1)
	assert.Equal(t, work.ID, queue[0].Work.ID)

	approved, err := repo.DecideManualReviewQueue(queue[0].Queue.ID, true, ManualReviewDecisionParams{
		Reviewer: "operator",
		Notes:    "authorized quotation",
	})
	require.NoError(t, err)
	assert.Equal(t, ManualReviewStatusApproved, approved.Queue.Status)
	assert.Equal(t, "manual_approved", approved.Report.ReviewStatus)
	assert.Equal(t, WorkStatusPublished, approved.Work.Status)
	assert.Equal(t, WorkVisibilityPublic, approved.Work.Visibility)

	publicWork, err := repo.GetPublicOriginalWorkByCode(work.WorkCode)
	require.NoError(t, err)
	assert.Equal(t, work.ID, publicWork.ID)
}
