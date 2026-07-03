package database

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupWorksTestRepo(t *testing.T) (*Repository, *APIKey) {
	t.Helper()
	gormDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	db := NewDBFromGorm(gormDB)
	require.NoError(t, db.Migrate())
	repo := NewRepository(db)
	key, _, err := repo.CreateAPIKey(CreateAPIKeyParams{Name: "works customer", DailyLimit: 10})
	require.NoError(t, err)
	return repo, key
}

func TestOriginalWorkCreatePublishVersionAndLicense(t *testing.T) {
	repo, key := setupWorksTestRepo(t)

	work, err := repo.CreateOriginalWork(CreateOriginalWorkParams{
		APIKeyID:           key.ID,
		Title:              "Mountain Window",
		WorkType:           "poem",
		Content:            "line one\nline two",
		OriginalCommitment: true,
		LicenseAccepted:    true,
		Publish:            true,
	})
	require.NoError(t, err)
	require.NotZero(t, work.ID)
	assert.Contains(t, work.WorkCode, "PCQF-")
	assert.Equal(t, WorkStatusPublished, work.Status)
	assert.Equal(t, WorkVisibilityPublic, work.Visibility)

	content := "line one\nline two\nline three"
	updated, err := repo.UpdateOriginalWork(key.ID, work.ID, UpdateOriginalWorkParams{
		Content:    &content,
		ChangeNote: "add third line",
	})
	require.NoError(t, err)
	assert.Equal(t, 2, updated.Version)

	versions, err := repo.ListOriginalWorkVersions(key.ID, work.ID)
	require.NoError(t, err)
	require.Len(t, versions, 2)
	assert.Equal(t, 2, versions[0].Version)

	licenses, err := repo.ListWorkLicenseAcceptances(key.ID, work.ID)
	require.NoError(t, err)
	require.NotEmpty(t, licenses)

	publicWork, err := repo.GetPublicOriginalWorkByCode(work.WorkCode)
	require.NoError(t, err)
	assert.Equal(t, work.ID, publicWork.ID)
}

func TestOriginalWorkPublishRequiresLicense(t *testing.T) {
	repo, key := setupWorksTestRepo(t)

	_, err := repo.CreateOriginalWork(CreateOriginalWorkParams{
		APIKeyID: key.ID,
		Title:    "Draft",
		Content:  "line",
		Publish:  true,
	})
	assert.ErrorIs(t, err, ErrInvalidQueryParam)
}

func TestOriginalWorkPublishDuplicateAncientRequiresReview(t *testing.T) {
	repo, key := setupWorksTestRepo(t)

	require.NoError(t, repo.db.Table(repo.poemsTable()).Create(&Poem{
		Title:       "Jing Ye Si",
		Content:     datatypes.JSON([]byte(`["床前明月光","疑是地上霜"]`)),
		ContentHash: "classic-hash",
	}).Error)

	work, err := repo.CreateOriginalWork(CreateOriginalWorkParams{
		APIKeyID:           key.ID,
		Title:              "My Moon",
		Content:            "床前明月光\n疑是地上霜",
		OriginalCommitment: true,
		LicenseAccepted:    true,
		Publish:            true,
	})
	require.NoError(t, err)
	assert.Equal(t, WorkStatusReviewRequired, work.Status)
	assert.Equal(t, WorkVisibilityPrivate, work.Visibility)
	assert.Equal(t, PlagiarismStatusExactDuplicate, work.PlagiarismStatus)

	_, err = repo.GetPublicOriginalWorkByCode(work.WorkCode)
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound)

	report, err := repo.LatestPlagiarismReport(key.ID, work.ID)
	require.NoError(t, err)
	assert.Equal(t, PlagiarismRiskExact, report.Report.RiskLevel)
	require.NotEmpty(t, report.Matches)
	assert.Equal(t, "ancient_poem", report.Matches[0].SourceType)
	assert.Equal(t, "exact", report.Matches[0].MatchType)
}

func TestWorkMediaAssetAndImageJobLifecycle(t *testing.T) {
	repo, key := setupWorksTestRepo(t)

	work, err := repo.CreateOriginalWork(CreateOriginalWorkParams{
		APIKeyID:    key.ID,
		Title:       "山窗夜坐",
		Content:     "山窗灯影薄\n一盏照清风",
		ImagePrompt: "古风水墨，小窗夜灯",
		ChangeNote:  "create",
		Description: "test work",
	})
	require.NoError(t, err)

	prompt, err := repo.CreateImagePrompt(CreateImagePromptParams{
		WorkID:   work.ID,
		APIKeyID: key.ID,
		Prompt:   "根据《山窗夜坐》生成古风水墨题诗画。",
		Source:   "test",
		Style:    "古风水墨",
		Size:     "1024x1024",
	})
	require.NoError(t, err)
	assert.NotZero(t, prompt.ID)

	job, err := repo.CreateImageGenerationJob(CreateImageGenerationJobParams{
		WorkID:       work.ID,
		APIKeyID:     key.ID,
		Status:       ImageJobStatusPending,
		Prompt:       prompt.Prompt,
		Style:        "古风水墨",
		Size:         "1024x1024",
		Model:        "gpt-image-2",
		Quality:      "high",
		OutputFormat: "png",
	})
	require.NoError(t, err)
	assert.Equal(t, ImageJobStatusPending, job.Status)

	asset, err := repo.CreateMediaAsset(CreateMediaAssetParams{
		WorkID:       work.ID,
		APIKeyID:     key.ID,
		AssetType:    MediaAssetTypeImage,
		Source:       MediaAssetSourceGenerated,
		URL:          "data:image/png;base64,aGVsbG8=",
		B64JSON:      "aGVsbG8=",
		MimeType:     "image/png",
		Model:        "gpt-image-2",
		Size:         "1024x1024",
		Quality:      "high",
		OutputFormat: "png",
		Prompt:       prompt.Prompt,
		Visibility:   WorkVisibilityPrivate,
	})
	require.NoError(t, err)
	assert.NotZero(t, asset.ID)

	completed, err := repo.CompleteImageGenerationJob(job.ID, &asset.ID)
	require.NoError(t, err)
	assert.Equal(t, ImageJobStatusSucceeded, completed.Status)
	require.NotNil(t, completed.MediaAssetID)
	assert.Equal(t, asset.ID, *completed.MediaAssetID)

	assets, err := repo.ListWorkMediaAssets(key.ID, work.ID, MediaAssetTypeImage, 10)
	require.NoError(t, err)
	require.Len(t, assets, 1)
	assert.Equal(t, asset.ID, assets[0].ID)

	jobs, err := repo.ListWorkImageJobs(key.ID, work.ID, 10)
	require.NoError(t, err)
	require.Len(t, jobs, 1)
	assert.Equal(t, ImageJobStatusSucceeded, jobs[0].Status)
}
