package main

import (
	"encoding/csv"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"

	"github.com/palemoky/chinese-poetry-api/internal/database"
)

func TestApplyReviewCommandDryRunAndApply(t *testing.T) {
	tempDir := t.TempDir()
	testDBPath := filepath.Join(tempDir, "poetry.db")
	db, err := database.Open(testDBPath, 1, 1)
	require.NoError(t, err)
	require.NoError(t, db.Migrate())

	repo := database.NewRepository(db)
	tangID, err := repo.GetOrCreateDynasty("唐")
	require.NoError(t, err)
	authorID, err := repo.GetOrCreateAuthor("李白", tangID)
	require.NoError(t, err)
	require.NoError(t, repo.InsertPoem(&database.Poem{
		ID:        101,
		Title:     "静夜思",
		Content:   datatypes.JSON([]byte(`["床前明月光","疑是地上霜","举头望明月","低头思故乡"]`)),
		AuthorID:  &authorID,
		DynastyID: &tangID,
	}))
	job, err := repo.CreateEnrichmentJob(database.CreateEnrichmentJobParams{
		Scope:      "enrich-cli-test",
		TotalCount: 3,
		Config:     map[string]any{"run_id": "enrich-cli-test"},
	})
	require.NoError(t, err)
	acceptedCandidate := database.ProposedKnowledgeInput{
		Summary:        "这首诗借明月写思乡之情，适合中秋与乡愁场景引用。",
		Translation:    "床前月光明亮，诗人抬头望月，低头思念故乡。",
		Annotation:     "明月是触发乡思的核心意象。",
		Recommendation: "适合思乡、月亮、中秋主题引用。",
		Source:         "manual",
	}
	acceptItem, err := repo.CreateEnrichmentReviewItem(database.CreateReviewItemParams{
		JobID:  &job.ID,
		PoemID: 101,
		ProposedTags: []database.TagInput{
			{Name: "思乡", Category: "theme", Source: "manual"},
			{Name: "月亮", Category: "image", Source: "manual"},
		},
		ProposedKnowledge: acceptedCandidate,
	})
	require.NoError(t, err)
	rejectItem, err := repo.CreateEnrichmentReviewItem(database.CreateReviewItemParams{
		JobID:  &job.ID,
		PoemID: 101,
		ProposedTags: []database.TagInput{
			{Name: "错误标签", Category: "theme", Source: "rules"},
		},
		ProposedKnowledge: database.ProposedKnowledgeInput{
			Summary: "这是一条用于测试退回流程的候选摘要，人工判断后不发布。",
			Source:  "rules",
		},
	})
	require.NoError(t, err)
	correctItem, err := repo.CreateEnrichmentReviewItem(database.CreateReviewItemParams{
		JobID:  &job.ID,
		PoemID: 101,
		ProposedTags: []database.TagInput{
			{Name: "待修正标签", Category: "theme", Source: "rules"},
		},
		ProposedKnowledge: database.ProposedKnowledgeInput{
			Summary: "这是一条用于测试人工修正流程的候选摘要，人工修正后应直接发布为通过状态。",
			Source:  "rules",
		},
	})
	require.NoError(t, err)
	require.NoError(t, db.Close())

	reviewFile := filepath.Join(tempDir, "manual-review.jsonl")
	correctedCandidate := database.ProposedKnowledgeInput{
		Summary:        "这首诗经人工修正后只保留月亮主题，适合月亮意象检索。",
		Translation:    "诗中有明月意象，适合用于月亮主题召回。",
		Annotation:     "人工修正后删除无关标签，只保留月亮。",
		Recommendation: "适合月亮主题引用。",
		Source:         "manual",
	}
	writeReviewJSONL(t, reviewFile, []manualReviewInputRecord{
		{
			ReviewItemID:      acceptItem.ID,
			RunID:             "enrich-cli-test",
			ProposedTags:      []database.TagInput{{Name: "思乡", Category: "theme", Source: "manual"}, {Name: "月亮", Category: "image", Source: "manual"}},
			ProposedKnowledge: acceptedCandidate,
			ReviewDecision:    manualReviewDecision{Action: "accept", Notes: "抽检通过"},
		},
		{
			ReviewItemID:   rejectItem.ID,
			RunID:          "enrich-cli-test",
			ReviewDecision: manualReviewDecision{Action: "reject", Notes: "标签与原文不符"},
		},
		{
			ReviewItemID:      correctItem.ID,
			RunID:             "enrich-cli-test",
			ProposedTags:      []database.TagInput{{Name: "月亮", Category: "image", Source: "manual"}},
			ProposedKnowledge: correctedCandidate,
			ReviewDecision:    manualReviewDecision{Action: "correct", Notes: "人工修正后通过"},
		},
	})

	oldDBPath := dbPath
	dbPath = testDBPath
	defer func() { dbPath = oldDBPath }()

	dryRun := applyReviewCmd()
	dryRun.SetArgs([]string{"--input", reviewFile, "--reviewer", "tester"})
	require.NoError(t, dryRun.Execute())

	db, err = database.Open(testDBPath, 1, 1)
	require.NoError(t, err)
	repo = database.NewRepository(db)
	summary, err := repo.GetEnrichmentRunSummary("enrich-cli-test")
	require.NoError(t, err)
	assert.Equal(t, 3, summary.PendingCount)
	assert.Equal(t, 0, summary.ReviewedCount)
	require.NoError(t, db.Close())

	apply := applyReviewCmd()
	apply.SetArgs([]string{"--input", reviewFile, "--reviewer", "tester", "--apply"})
	require.NoError(t, apply.Execute())

	db, err = database.Open(testDBPath, 1, 1)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	repo = database.NewRepository(db)
	summary, err = repo.GetEnrichmentRunSummary("enrich-cli-test")
	require.NoError(t, err)
	assert.Equal(t, 0, summary.PendingCount)
	assert.Equal(t, 2, summary.AcceptedCount)
	assert.Equal(t, 1, summary.RejectedCount)
	assert.InDelta(t, 2.0/3.0, summary.PassRate, 0.0001)

	knowledge, err := repo.GetPoemKnowledge(101)
	require.NoError(t, err)
	assert.Equal(t, database.EnrichmentStatusAccepted, knowledge.QualityStatus)
	assert.Equal(t, "tester", knowledge.Reviewer)
}

func TestBuildManualReviewAuditSummarizesOffsetBatch(t *testing.T) {
	records := []manualReviewInputRecord{
		{
			ReviewItemID:   1,
			RunID:          "run-a",
			ReviewDecision: manualReviewDecision{Action: "accept", Notes: "ok"},
		},
		{
			ReviewItemID:   2,
			RunID:          "run-a",
			ReviewDecision: manualReviewDecision{Action: "correct", Notes: "fixed"},
		},
		{
			ReviewItemID:   3,
			RunID:          "run-a",
			ReviewDecision: manualReviewDecision{Action: "reject", Notes: "tag mismatch"},
		},
		{
			ReviewItemID:   4,
			RunID:          "run-a",
			ReviewDecision: manualReviewDecision{Action: "pending", Notes: ""},
		},
	}

	report := buildManualReviewAudit("manual.jsonl", "run-a", records)

	assert.Equal(t, 4, report.Total)
	assert.Equal(t, 3, report.ReviewedCount)
	assert.Equal(t, 1, report.PendingCount)
	assert.Equal(t, 1, report.AcceptCount)
	assert.Equal(t, 1, report.CorrectCount)
	assert.Equal(t, 1, report.RejectCount)
	assert.Equal(t, 2, report.PublishableCount)
	assert.InDelta(t, 2.0/3.0, report.PassRate, 0.0001)
	assert.Equal(t, "66.67%", report.PassRatePercent)
	assert.Contains(t, report.RecommendedNextStep, "停止规则扩批")
	require.Len(t, report.RejectedNoteTop10, 1)
	assert.Equal(t, "tag mismatch", report.RejectedNoteTop10[0].Note)
}

func TestBuildQualityGateReportFlagsWeakCandidates(t *testing.T) {
	candidates := []candidateRecord{
		{
			PoemID: 1,
			ProposedTags: []database.TagInput{
				{Name: "思乡", Category: "theme", Source: "ai"},
				{Name: "思乡", Category: "theme", Source: "ai"},
				{Name: "长句标签", Category: "bad_category", Source: "ai"},
			},
			ProposedKnowledge: database.ProposedKnowledgeInput{
				Summary: "这首诗写政治失意和思乡情绪，适合知识库召回使用。",
				Source:  "ai",
			},
			Meta: map[string]any{"confidence": 0.4},
		},
	}
	samples := map[int64]sampleRecord{
		1: {
			PoemID:  1,
			Title:   "静夜思",
			Content: []string{"床前明月光", "低头思故乡"},
		},
	}

	report := buildQualityGateReport("candidates.jsonl", "sample.jsonl", candidates, samples, 0.7)

	assert.False(t, report.ValidFormat)
	assert.Equal(t, 1, report.Total)
	assert.GreaterOrEqual(t, report.ErrorCount, 2)
	assert.GreaterOrEqual(t, report.WarningCount, 2)
	assert.Equal(t, 1, report.LowConfidenceCount)
	assert.Equal(t, 1, report.NeedsReviewCount)
	assert.Equal(t, 0, report.PassedCount)
	assert.NotEmpty(t, report.IssueTop10)
}

func TestValidateCommandWritesReportFile(t *testing.T) {
	tempDir := t.TempDir()
	input := filepath.Join(tempDir, "candidates.jsonl")
	output := filepath.Join(tempDir, "validate.json")
	writeCandidateJSONL(t, input, []candidateRecord{
		{
			PoemID: 1,
			ProposedTags: []database.TagInput{
				{Name: "思乡", Category: "theme", Source: "ai"},
			},
			ProposedKnowledge: database.ProposedKnowledgeInput{
				Summary: "这首诗借原文意象表现情绪与场景，适合用于知识库召回和内容引用。",
				Source:  "ai",
			},
		},
	})

	cmd := validateCmd()
	cmd.SetArgs([]string{"--input", input, "--out", output, "--skip-db-check"})
	require.NoError(t, cmd.Execute())

	raw, err := os.ReadFile(output)
	require.NoError(t, err)
	var report map[string]any
	require.NoError(t, json.Unmarshal(raw, &report))
	assert.Equal(t, float64(1), report["total"])
	assert.Equal(t, float64(0), report["error_count"])
	assert.Equal(t, true, report["valid"])
}

func TestBuildGoldenAuditReportDetectsAnnotationGaps(t *testing.T) {
	records := []goldenSampleRecord{
		{
			PoemID:  1,
			Title:   "todo sample",
			Content: []string{"moon over river"},
			GoldenMeta: map[string]any{
				"stratum":           "Tang / poem",
				"expected_tags":     []database.TagInput{},
				"evidence_lines":    []string{},
				"annotation_status": "todo",
			},
		},
		{
			PoemID:  2,
			Title:   "complete sample",
			Content: []string{"white sun over mountain"},
			GoldenMeta: map[string]any{
				"stratum":           "Tang / poem",
				"expected_tags":     []database.TagInput{{Name: "mountain", Category: "image"}},
				"evidence_lines":    []string{"white sun over mountain"},
				"annotation_status": "done",
			},
		},
		{
			PoemID:  2,
			Title:   "bad evidence sample",
			Content: []string{"river flows east"},
			GoldenMeta: map[string]any{
				"stratum":           "Song / ci",
				"expected_tags":     []database.TagInput{{Name: "river", Category: "image"}},
				"evidence_lines":    []string{"not in source"},
				"annotation_status": "reviewed",
			},
		},
	}

	report := buildGoldenAuditReport("golden.jsonl", records, 1.0)

	assert.False(t, report.ReadyForEvaluation)
	assert.Equal(t, 3, report.Total)
	assert.Equal(t, 2, report.UniquePoemIDs)
	assert.Equal(t, []int64{2}, report.DuplicatePoemIDs)
	assert.Equal(t, 2, report.ExpectedTagsFilledCount)
	assert.Equal(t, 2, report.EvidenceLinesFilledCount)
	assert.Equal(t, 2, report.ReviewedStatusCount)
	assert.Equal(t, 1, report.CompleteCount)
	assert.Equal(t, 1, report.InvalidEvidenceCount)
	assert.Equal(t, 1, report.StatusCounts["todo"])
	assert.Contains(t, report.RequiredAction, "fill expected_tags")
	assert.NotEmpty(t, report.IssueTop10)
}

func TestGoldenAuditCommandWritesReadyReport(t *testing.T) {
	tempDir := t.TempDir()
	input := filepath.Join(tempDir, "golden.jsonl")
	output := filepath.Join(tempDir, "golden.audit.json")
	writeGoldenJSONL(t, input, []goldenSampleRecord{
		{
			PoemID:  9,
			Title:   "complete sample",
			Content: []string{"bright moon in source"},
			GoldenMeta: map[string]any{
				"stratum":           "Tang / poem",
				"expected_tags":     []database.TagInput{{Name: "moon", Category: "image"}},
				"evidence_lines":    []string{"bright moon in source"},
				"annotation_status": "done",
			},
		},
	})

	cmd := goldenAuditCmd()
	cmd.SetArgs([]string{"--input", input, "--out", output, "--require-complete"})
	require.NoError(t, cmd.Execute())

	raw, err := os.ReadFile(output)
	require.NoError(t, err)
	var report goldenAuditReport
	require.NoError(t, json.Unmarshal(raw, &report))
	assert.True(t, report.ReadyForEvaluation)
	assert.Equal(t, 1, report.Total)
	assert.Equal(t, 1, report.CompleteCount)
	assert.Equal(t, "100.00%", report.CompleteRatePercent)
}

func TestBuildGoldenPrefillUsesAcceptedManualEvidence(t *testing.T) {
	tempDir := t.TempDir()
	testDBPath := filepath.Join(tempDir, "poetry.db")
	db, err := database.Open(testDBPath, 1, 1)
	require.NoError(t, err)
	require.NoError(t, db.Migrate())
	repo := database.NewRepository(db)
	require.NoError(t, repo.InsertPoem(&database.Poem{
		ID:      501,
		Title:   "golden source",
		Content: datatypes.JSON([]byte(`["春花秋月何时了，往事知多少？","小楼昨夜又东风，故国不堪回首月明中。"]`)),
	}))
	_, err = repo.AssignTagsToPoem(501, []database.TagInput{
		{Name: "月亮", Category: "theme", Source: "manual_review"},
		{Name: "春天", Category: "season", Source: "manual_review"},
	})
	require.NoError(t, err)
	require.NoError(t, repo.UpsertPoemKnowledge(501, database.ProposedKnowledgeInput{
		Summary: "accepted reviewed knowledge",
		Source:  "manual_review",
	}, database.EnrichmentStatusAccepted, "tester", "ok"))

	result, err := buildGoldenPrefill(repo, []goldenSampleRecord{
		{
			PoemID:  501,
			Title:   "golden source",
			Content: []string{"春花秋月何时了，往事知多少？", "小楼昨夜又东风，故国不堪回首月明中。"},
			GoldenMeta: map[string]any{
				"stratum":           "Song / ci",
				"expected_tags":     []database.TagInput{},
				"evidence_lines":    []string{},
				"annotation_status": "todo",
			},
		},
	}, "accepted-reviewed", 0)
	require.NoError(t, err)
	require.Len(t, result.Records, 1)
	assert.Equal(t, 1, result.Report.Updated)
	meta := result.Records[0].GoldenMeta
	assert.Equal(t, "prefilled_review_required", meta["annotation_status"])
	assert.Len(t, meta["expected_tags"], 2)
	assert.NotEmpty(t, meta["evidence_lines"])
	assert.Equal(t, "manual_review", meta["prefill_source"])
	require.NoError(t, db.Close())
}

func TestGoldenReviewQueueAndApplyReview(t *testing.T) {
	base := []goldenSampleRecord{
		{
			PoemID:  1,
			Title:   "needs review",
			Content: []string{"明月照高楼"},
			GoldenMeta: map[string]any{
				"stratum":           "Tang / poem",
				"expected_tags":     []database.TagInput{{Name: "月亮", Category: "theme"}},
				"evidence_lines":    []string{"明月照高楼"},
				"annotation_status": "prefilled_review_required",
			},
		},
		{
			PoemID:  2,
			Title:   "todo",
			Content: []string{"春风又绿江南岸"},
			GoldenMeta: map[string]any{
				"stratum":           "Song / poem",
				"expected_tags":     []database.TagInput{},
				"evidence_lines":    []string{},
				"annotation_status": "todo",
			},
		},
	}

	queue := goldenReviewQueue(base, "prefilled_review_required", 0)
	require.Len(t, queue, 1)
	queue[0].GoldenMeta["annotation_status"] = "done"
	queue[0].GoldenMeta["review_notes"] = "checked"

	result, err := applyGoldenReview(base, queue, "golden-reviewer", true)
	require.NoError(t, err)
	assert.Equal(t, 1, result.Report["applied"])
	assert.Equal(t, "done", result.Records[0].GoldenMeta["annotation_status"])
	assert.Equal(t, "golden-reviewer", result.Records[0].GoldenMeta["reviewed_by"])
	assert.Equal(t, "todo", result.Records[1].GoldenMeta["annotation_status"])
}

func TestGoldenReviewSheetRoundTripAndApply(t *testing.T) {
	tempDir := t.TempDir()
	basePath := filepath.Join(tempDir, "golden.prefilled.jsonl")
	sheetPath := filepath.Join(tempDir, "golden.review.csv")
	outputPath := filepath.Join(tempDir, "golden.reviewed.jsonl")
	writeGoldenJSONL(t, basePath, []goldenSampleRecord{
		{
			PoemID:  1,
			Title:   "needs review",
			Content: []string{"明月照高楼"},
			GoldenMeta: map[string]any{
				"stratum":           "Tang / poem",
				"expected_tags":     []database.TagInput{{Name: "月亮", Category: "theme"}},
				"evidence_lines":    []string{"明月照高楼"},
				"annotation_status": "prefilled_review_required",
			},
		},
		{
			PoemID:  2,
			Title:   "todo",
			Content: []string{"春风又绿江南岸"},
			GoldenMeta: map[string]any{
				"stratum":           "Song / poem",
				"expected_tags":     []database.TagInput{},
				"evidence_lines":    []string{},
				"annotation_status": "todo",
			},
		},
	})

	exportCmd := goldenReviewSheetCmd()
	exportCmd.SetArgs([]string{"--input", basePath, "--output", sheetPath})
	require.NoError(t, exportCmd.Execute())

	file, err := os.Open(sheetPath)
	require.NoError(t, err)
	rows, err := csv.NewReader(file).ReadAll()
	require.NoError(t, err)
	require.NoError(t, file.Close())
	require.Len(t, rows, 2)
	statusIndex := -1
	notesIndex := -1
	for i, name := range rows[0] {
		if name == "annotation_status" {
			statusIndex = i
		}
		if name == "review_notes" {
			notesIndex = i
		}
	}
	require.NotEqual(t, -1, statusIndex)
	require.NotEqual(t, -1, notesIndex)
	rows[1][statusIndex] = "done"
	rows[1][notesIndex] = "checked"

	out, err := os.Create(sheetPath)
	require.NoError(t, err)
	writer := csv.NewWriter(out)
	require.NoError(t, writer.WriteAll(rows))
	writer.Flush()
	require.NoError(t, writer.Error())
	require.NoError(t, out.Close())

	auditCmd := goldenReviewSheetAuditCmd()
	auditCmd.SetArgs([]string{"--sheet", sheetPath, "--require-done"})
	require.NoError(t, auditCmd.Execute())

	applyCmd := goldenApplyReviewSheetCmd()
	applyCmd.SetArgs([]string{"--base", basePath, "--sheet", sheetPath, "--output", outputPath, "--reviewer", "sheet-reviewer"})
	require.NoError(t, applyCmd.Execute())

	reviewed, err := readGoldenSamples(outputPath)
	require.NoError(t, err)
	require.Len(t, reviewed, 2)
	assert.Equal(t, "done", reviewed[0].GoldenMeta["annotation_status"])
	assert.Equal(t, "checked", reviewed[0].GoldenMeta["review_notes"])
	assert.Equal(t, "sheet-reviewer", reviewed[0].GoldenMeta["reviewed_by"])
	assert.Equal(t, "todo", reviewed[1].GoldenMeta["annotation_status"])
}

func TestGoldenToSampleCommandWritesRegularSamples(t *testing.T) {
	tempDir := t.TempDir()
	input := filepath.Join(tempDir, "golden.jsonl")
	output := filepath.Join(tempDir, "sample.jsonl")
	writeGoldenJSONL(t, input, []goldenSampleRecord{
		{
			PoemID:  11,
			Title:   "golden one",
			Content: []string{"line one"},
			Author:  "author",
			Dynasty: "dynasty",
			Type:    "type",
			GoldenMeta: map[string]any{
				"annotation_status": "todo",
			},
		},
	})

	cmd := goldenToSampleCmd()
	cmd.SetArgs([]string{"--input", input, "--output", output})
	require.NoError(t, cmd.Execute())
	samples, err := readSamples(output)
	require.NoError(t, err)
	require.Len(t, samples, 1)
	assert.Equal(t, int64(11), samples[0].PoemID)
	assert.Equal(t, "golden one", samples[0].Title)
}

func TestGenerateRulesCandidateAvoidsOverBroadTags(t *testing.T) {
	tests := []struct {
		name       string
		sample     sampleRecord
		wantTags   []string
		rejectTags []string
	}{
		{
			name: "鸡塞远不是边塞主旨时不误打边塞",
			sample: sampleRecord{
				PoemID: 6,
				Title:  "摊破浣溪沙·菡萏香销翠叶残",
				Content: []string{
					"菡萏香销翠叶残，西风愁起绿波间。",
					"细雨梦回鸡塞远，小楼吹彻玉笙寒。",
					"多少泪珠无限恨，倚栏杆。",
				},
			},
			wantTags:   []string{"愁绪"},
			rejectTags: []string{"边塞"},
		},
		{
			name: "亡国愁词不误打山水文旅",
			sample: sampleRecord{
				PoemID: 1,
				Title:  "虞美人·春花秋月何时了",
				Content: []string{
					"春花秋月何时了，往事知多少？",
					"小楼昨夜又东风，故国不堪回首月明中。",
					"问君能有几多愁？恰似一江春水向东流。",
				},
			},
			wantTags:   []string{"月亮", "春天", "家国", "愁绪"},
			rejectTags: []string{"山水", "文旅"},
		},
		{
			name: "别巷不是送别且暮烟不是文旅山水",
			sample: sampleRecord{
				PoemID: 7,
				Title:  "临江仙·樱桃落尽春归去",
				Content: []string{
					"樱桃落尽春归去，蝶翻轻粉双飞。",
					"子规啼月小楼西。",
					"别巷寂寥人散后，望残烟草低迷。",
					"空持罗带，回首恨依依。",
				},
			},
			wantTags:   []string{"月亮", "春天", "愁绪"},
			rejectTags: []string{"山水", "文旅", "送别"},
		},
		{
			name: "短篇愁词不因单字误打边塞山水文旅",
			sample: sampleRecord{
				PoemID: 2,
				Title:  "望江南·多少泪",
				Content: []string{
					"多少泪，断脸复横颐。",
					"心事莫将和泪说，凤笙休向泪时吹。",
					"肠断更无疑。",
				},
			},
			wantTags:   []string{"愁绪"},
			rejectTags: []string{"边塞", "山水", "文旅"},
		},
		{
			name: "旧游上苑不是山水文旅",
			sample: sampleRecord{
				PoemID: 8,
				Title:  "望江南·多少恨",
				Content: []string{
					"多少恨，昨夜梦魂中。",
					"还似旧时游上苑，车如流水马如龙。",
					"花月正春风。",
				},
			},
			wantTags:   []string{"月亮", "春天", "愁绪"},
			rejectTags: []string{"山水", "文旅"},
		},
		{
			name: "晓月庭院词不误打思乡",
			sample: sampleRecord{
				PoemID: 9,
				Title:  "喜迁莺·晓月坠",
				Content: []string{
					"晓月坠，宿烟微，无语枕频倾攲。",
					"梦回芳草思依依，天远雁声稀。",
					"啼莺散，馀花乱，寂寞画堂深院。",
				},
			},
			wantTags:   []string{"月亮", "春天", "相思", "愁绪"},
			rejectTags: []string{"思乡", "宴乐歌舞"},
		},
		{
			name: "别殿不是送别",
			sample: sampleRecord{
				PoemID: 3,
				Title:  "浣溪沙·红日已高三丈透",
				Content: []string{
					"红日已高三丈透，金炉次第添香兽。",
					"佳人舞点金钗溜，酒恶时拈花蕊嗅。",
					"别殿遥闻箫鼓奏。",
				},
			},
			wantTags:   []string{"宴乐歌舞"},
			rejectTags: []string{"送别"},
		},
		{
			name: "离愁不是送别场景",
			sample: sampleRecord{
				PoemID: 10,
				Title:  "相见欢·无言独上西楼",
				Content: []string{
					"无言独上西楼，月如钩。",
					"寂寞梧桐深院，锁清秋。",
					"剪不断，理还乱，是离愁。",
					"别是一般滋味，在心头。",
				},
			},
			wantTags:   []string{"月亮", "愁绪"},
			rejectTags: []string{"送别"},
		},
		{
			name: "春梦不是春天季节场景",
			sample: sampleRecord{
				PoemID: 11,
				Title:  "菩萨蛮·铜簧韵脆锵寒竹",
				Content: []string{
					"铜簧韵脆锵寒竹，新声慢奏移纤玉。",
					"眼色暗相钩，秋波横欲流。",
					"雨云深绣户，未便谐衷素。",
					"宴罢又成空，魂迷春梦中。",
				},
			},
			wantTags:   []string{"相思"},
			rejectTags: []string{"春天"},
		},
		{
			name: "临春宫殿名不等于春天季节",
			sample: sampleRecord{
				PoemID: 12,
				Title:  "玉楼春·晚妆初了明肌雪",
				Content: []string{
					"晚妆初了明肌雪，春殿嫔娥鱼贯列。",
					"笙箫吹断水云间，重按霓裳歌遍彻。",
					"临春谁更飘香屑？",
					"醉拍栏杆情味切。",
					"归时休放烛光红，待踏马啼清夜月。",
				},
			},
			wantTags:   []string{"月亮", "宴乐歌舞"},
			rejectTags: []string{"春天"},
		},
		{
			name: "明确离恨归梦保留送别思乡",
			sample: sampleRecord{
				PoemID: 4,
				Title:  "清平乐·别来春半",
				Content: []string{
					"别来春半，触目柔肠断。",
					"雁来音信无凭，路遥归梦难成。",
					"离恨恰如春草，更行更远还生。",
				},
			},
			wantTags: []string{"送别", "思乡", "春天", "愁绪"},
		},
		{
			name: "真实登临山水保留文旅",
			sample: sampleRecord{
				PoemID: 5,
				Title:  "登鹳雀楼",
				Content: []string{
					"白日依山尽，黄河入海流。",
					"欲穷千里目，更上一层楼。",
				},
			},
			wantTags: []string{"山水", "文旅"},
		},
		{
			name: "折柳攀花不是送别",
			sample: sampleRecord{
				PoemID: 117,
				Title:  "南吕・一枝花不伏老",
				Content: []string{
					"攀出墙朵朵花，折临路枝枝柳。",
					"凭着我折柳攀花手，直煞得花残柳败休。",
				},
			},
			rejectTags: []string{"送别"},
		},
		{
			name: "送香茶不是送别",
			sample: sampleRecord{
				PoemID: 147,
				Title:  "收江南",
				Content: []string{
					"冷丁丁舌尖上送香茶，都不到半霎。",
				},
			},
			rejectTags: []string{"送别"},
		},
		{
			name: "风月花月约不是月亮",
			sample: sampleRecord{
				PoemID: 157,
				Title:  "双调・新水令",
				Content: []string{
					"寨儿中风月煞经谙，收心也合搠淹。",
					"花月约，凤鸾交，半世疏狂。",
				},
			},
			rejectTags: []string{"月亮"},
		},
		{
			name: "春衫柳絮心性比喻不是春天",
			sample: sampleRecord{
				PoemID: 157,
				Title:  "双调・新水令",
				Content: []string{
					"寨儿中风月煞经谙，收心也合搠淹。",
					"再不缠头戴蜀锦，沽酒典春衫。",
					"心如柳絮粘泥，狂风过怎摇撼。",
				},
			},
			rejectTags: []string{"春天", "月亮"},
		},
		{
			name: "嫦娥美貌比喻不是月亮",
			sample: sampleRecord{
				PoemID: 145,
				Title:  "豆叶黄",
				Content: []string{
					"比月里嫦娥，媚媚孜孜，那更撑达。",
				},
			},
			rejectTags: []string{"月亮"},
		},
		{
			name: "青春身体比喻不是春天",
			sample: sampleRecord{
				PoemID: 155,
				Title:  "天仙子",
				Content: []string{
					"这一扇儿比他每情更深，是君瑞莺莺。",
					"画的来厮顾盼厮温存，比各青春。",
				},
			},
			rejectTags: []string{"春天"},
		},
		{
			name: "粉蝶舞步比喻不是春天",
			sample: sampleRecord{
				PoemID: 140,
				Title:  "圣药王",
				Content: []string{
					"甚整齐，省气力，旁行侧脚步频移，来往似粉蝶儿飞。",
				},
			},
			rejectTags: []string{"春天"},
		},
		{
			name: "醉春风曲牌名不是春天",
			sample: sampleRecord{
				PoemID: 226,
				Title:  "钱大尹智宠谢天香・醉春风",
				Content: []string{
					"那里敢深蘸着指头搽，我则索轻将绵絮纽。",
				},
			},
			rejectTags: []string{"春天"},
		},
		{
			name: "飞絮孤舟比喻不是春天",
			sample: sampleRecord{
				PoemID: 230,
				Title:  "钱大尹智宠谢天香・耍孩儿",
				Content: []string{
					"我本是沾泥飞絮，倒做了不缆孤舟！",
				},
			},
			rejectTags: []string{"春天"},
		},
		{
			name: "和气春风满画堂不是春天",
			sample: sampleRecord{
				PoemID: 207,
				Title:  "钱大尹智宠谢天香・隔尾",
				Content: []string{
					"我见他严容端坐挨着罗幌，可甚么和气春风满画堂！",
					"我最愁是劈先里递一声唱。",
				},
			},
			wantTags:   []string{"愁绪"},
			rejectTags: []string{"春天"},
		},
		{
			name: "杨柳樱桃身体比喻不是春天",
			sample: sampleRecord{
				PoemID: 192,
				Title:  "木丫叉",
				Content: []string{
					"小蛮腰瘦如杨柳，浅淡樱桃樊素口。",
				},
			},
			rejectTags: []string{"春天"},
		},
		{
			name: "柳絮桃花作情性比喻不是春天",
			sample: sampleRecord{
				PoemID: 191,
				Title:  "不是路・万种风流，今日番成一段愁。泪盈眸，云山满目恨悠悠。谩追求，",
				Content: []string{
					"情如柳絮风前斗，性似桃花逐水流。",
					"沉吟久，因他数尽残更漏，恁般亻孱亻愁！",
				},
			},
			wantTags:   []string{"愁绪"},
			rejectTags: []string{"春天"},
		},
		{
			name: "落花流水人何处不是春天",
			sample: sampleRecord{
				PoemID: 126,
				Title:  "幺・天付两风流，翻成南北悠悠，落花流水人何处？相思一点，离愁几许，",
				Content: []string{
					"撮上心头。",
				},
			},
			wantTags:   []string{"相思"},
			rejectTags: []string{"春天"},
		},
		{
			name: "星前月下誓约不是月亮主题",
			sample: sampleRecord{
				PoemID: 128,
				Title:  "幺・坐想行思，伤怀感旧，各辜负了星前月下深深咒。愿不损，愁不煞，",
				Content: []string{
					"神天还佑。",
					"他有日不测相逢，话别离情取一场消瘦。",
				},
			},
			wantTags:   []string{"送别"},
			rejectTags: []string{"月亮"},
		},
		{
			name: "月下情星前约保留相思不标春天",
			sample: sampleRecord{
				PoemID: 165,
				Title:  "甜水令・佳人有意郎君俏，郎君没钞莺花恼。如今等惜花人弄巧，指不过",
				Content: []string{
					"想着月下情，星前约，是则是花木瓜儿看好。",
				},
			},
			wantTags:   []string{"月亮", "相思"},
			rejectTags: []string{"春天"},
		},
		{
			name: "月下砧声只取愁绪不作月亮主题",
			sample: sampleRecord{
				PoemID: 193,
				Title:  "幺篇・月下砧声幽，月下砧声幽，风前笛秦。断肠声无了无休，捣碎我心",
				Content: []string{
					"又加上一场症候，顿使我愁不寐，襄王梦雨散云收。",
				},
			},
			wantTags:   []string{"愁绪"},
			rejectTags: []string{"月亮"},
		},
		{
			name: "惜花愁伤春怨不额外标春天",
			sample: sampleRecord{
				PoemID: 195,
				Title:  "煞尾・惜花愁，伤春怨，萦系杀多情少年。何处狂游袅玉鞭，谩教人暗卜",
				Content: []string{
					"空写遍翠涛笺，鱼雁难传。",
					"紫箫声转，画楼中闲杀月明天。",
				},
			},
			wantTags:   []string{"月亮", "相思"},
			rejectTags: []string{"春天"},
		},
		{
			name: "莺花笑人保留春天",
			sample: sampleRecord{
				PoemID: 125,
				Title:  "卖花声煞・愁山闷海不许当敌，好教我无一个百刂划，耐心儿多陪下些凄",
				Content: []string{
					"惶泪。",
					"呼侍婢将绣帘低放，把重门深闭，怕莺花笑人憔悴。",
				},
			},
			wantTags: []string{"春天", "愁绪"},
		},
		{
			name: "若道伤春保留春天",
			sample: sampleRecord{
				PoemID: 153,
				Title:  "驻马听・多绪多情，病身躯憔悴损；闲愁闲闷，将柳带结同心。瘦岩岩宽",
				Content: []string{
					"褪了绛绡裙，羞答答恐怕他邻姬问。",
					"若道伤春，今年更比年时甚。",
				},
			},
			wantTags: []string{"春天", "相思", "愁绪"},
		},
		{
			name: "正文无月但题目月宵保留月亮",
			sample: sampleRecord{
				PoemID: 132,
				Title:  "催拍子・爱共寝花间锦鸠，恨孤眠水上白鸥。月宵花昼，大筵排回雪韦娘，",
				Content: []string{
					"小酌会窃香韩寿。",
					"水仙山鬼，月妹花妖，如还得遇，不许干休，会埋伏未尝泄漏。",
				},
			},
			wantTags: []string{"月亮"},
		},
		{
			name: "题目群芳绿肥红瘦保留春天",
			sample: sampleRecord{
				PoemID: 133,
				Title:  "幺・群芳会首，繁英故友，梦回时绿肥红瘦。荣华过可见蔬薄，财物广始",
				Content: []string{
					"知亲厚。",
					"慕新思旧，簪遗佩解，镜破钗分，蜂妒蝶羞。",
				},
			},
			wantTags: []string{"春天"},
		},
		{
			name: "瘦岩岩和清江村不是山水",
			sample: sampleRecord{
				PoemID: 101,
				Title:  "双调・大德歌（四首）春",
				Content: []string{
					"子规啼，不如归，道是春归人未归。",
					"瘦岩岩羞带石榴花。",
					"那里是清江江上村？",
				},
			},
			wantTags:   []string{"春天"},
			rejectTags: []string{"山水", "文旅"},
		},
		{
			name: "浪子宴乐闲愁郎君不标相思愁绪",
			sample: sampleRecord{
				PoemID: 118,
				Title:  "梁州・我是个普天下郎君领袖，盖世界浪子班头。愿朱颜不改常依旧，花",
				Content: []string{
					"中消遣，酒内忘忧。",
					"通五音六律滑熟，甚闲愁到我心头！",
					"伴的是银筝女银台前理银筝笑倚银屏，伴的是金钗客歌《金缕》捧金樽满泛金瓯。",
				},
			},
			wantTags:   []string{"宴乐歌舞"},
			rejectTags: []string{"相思", "愁绪"},
		},
		{
			name: "春山秋波不是山水",
			sample: sampleRecord{
				PoemID: 175,
				Title:  "金盏子",
				Content: []string{
					"眼去眉来相思恋，春山摇，秋波转。",
				},
			},
			wantTags:   []string{"相思"},
			rejectTags: []string{"山水"},
		},
		{
			name: "春山秋水眉眼不是山水",
			sample: sampleRecord{
				PoemID: 249,
				Title:  "草桥店梦莺莺(第四本)・紫花儿序",
				Content: []string{
					"俺小姐这些时春山低翠，秋水凝眸。",
				},
			},
			rejectTags: []string{"山水"},
		},
		{
			name: "山长水远离别套语不是山水",
			sample: sampleRecord{
				PoemID: 196,
				Title:  "赏花时",
				Content: []string{
					"双泪落尊前。山长水远，愁见理行轩。",
				},
			},
			wantTags:   []string{"愁绪"},
			rejectTags: []string{"山水"},
		},
		{
			name: "泪添黄河恨压华岳并有古道长堤保留山水",
			sample: sampleRecord{
				PoemID: 273,
				Title:  "草桥店梦莺莺(第四本)・四煞",
				Content: []string{
					"这忧愁诉与谁？",
					"相思只自知，老天不管人憔悴。",
					"泪添九曲黄河溢，恨压三峰华岳低。",
					"到晚来闷把西楼倚，见了些夕阳古道，衰柳长堤。",
				},
			},
			wantTags: []string{"山水", "相思", "愁绪"},
		},
		{
			name: "千山万水阻隔套语不是山水",
			sample: sampleRecord{
				PoemID: 291,
				Title:  "草桥店梦莺莺(第四本)・络丝娘煞尾",
				Content: []string{
					"都则为一官半职，阻隔得千山万水。",
				},
			},
			rejectTags: []string{"山水"},
		},
		{
			name: "白马将军飞虎叛贼不是边塞",
			sample: sampleRecord{
				PoemID: 256,
				Title:  "草桥店梦莺莺(第四本)・幺篇",
				Content: []string{
					"起白马将军故友，斩飞虎叛贼草寇。",
				},
			},
			rejectTags: []string{"边塞"},
		},
		{
			name: "才郎别后是背景不是送别场景",
			sample: sampleRecord{
				PoemID: 229,
				Title:  "钱大尹智宠谢天香・哨遍",
				Content: []string{
					"一自才郎别后，相公那帘幕里香风透。",
					"不问我舞旋，只着我歌讴。",
					"唱到惨绿愁红。",
				},
			},
			wantTags:   []string{"愁绪", "宴乐歌舞"},
			rejectTags: []string{"送别"},
		},
		{
			name: "有名无实宅中对白不是相思",
			sample: sampleRecord{
				PoemID: 212,
				Title:  "钱大尹智宠谢天香・滚绣球",
				Content: []string{
					"整三年有名无实。",
					"本是个见交风月耆卿伴，教我做遥受恩情大尹妻，端的谁知？",
				},
			},
			rejectTags: []string{"相思"},
		},
		{
			name: "牡丹东君章台柳命运比喻不是相思",
			sample: sampleRecord{
				PoemID: 232,
				Title:  "钱大尹智宠谢天香・煞尾",
				Content: []string{
					"这天香不想艳阳天气开，我则道无情干罢休！",
					"谁想这牡丹花折入东君手，今日个分与章台路傍柳。",
				},
			},
			rejectTags: []string{"相思"},
		},
		{
			name: "不识忧不识愁是否定不标愁绪",
			sample: sampleRecord{
				PoemID: 254,
				Title:  "草桥店梦莺莺(第四本)・圣药王",
				Content: []string{
					"他每不识忧，不识愁，一双心意两相投。",
					"夫人得好休，便好休，这其间何必苦追求？",
				},
			},
			rejectTags: []string{"愁绪"},
		},
		{
			name: "月圆云遮套语不是月亮主题",
			sample: sampleRecord{
				PoemID: 286,
				Title:  "草桥店梦莺莺(第四本)・甜水令",
				Content: []string{
					"便枕冷衾寒，凤只鸾孤，月圆云遮，寻思来有甚伤嗟。",
				},
			},
			rejectTags: []string{"月亮"},
		},
		{
			name: "身体春意春色不是春天",
			sample: sampleRecord{
				PoemID: 247,
				Title:  "草桥店梦莺莺(第四本)・煞尾",
				Content: []string{
					"春意透酥胸，春色横眉黛，贱却人间玉帛。",
					"杏脸桃腮，乘着月色，娇滴滴越显得红白。",
				},
			},
			wantTags:   []string{"月亮"},
			rejectTags: []string{"春天"},
		},
		{
			name: "生则同衾死则同穴保留相思",
			sample: sampleRecord{
				PoemID: 287,
				Title:  "草桥店梦莺莺(第四本)・折桂令",
				Content: []string{
					"想人生最苦离别，可怜见千里关山，独自跋涉。",
					"自愿的生则同衾，死则同穴。",
				},
			},
			wantTags: []string{"送别", "相思"},
		},
		{
			name: "从别后是别后愁绪不是送别现场",
			sample: sampleRecord{
				PoemID: 302,
				Title:  "梅花酒・雁儿呀呀的叫几声，惊起那人听，说着咱名姓，他自有人相迎。",
				Content: []string{
					"从别后不见影，闪得人亡了魂灵。",
					"罗帷中愁怎禁，则为他挂心情。",
					"朝忘餐泪如倾，曲慵唱酒慵斟。",
				},
			},
			wantTags:   []string{"愁绪"},
			rejectTags: []string{"送别"},
		},
		{
			name: "题目里的不思量芳心不额外标相思",
			sample: sampleRecord{
				PoemID: 307,
				Title:  "出队子・粉香一捻，不思量难弃舍。语怜檀口口咨嗟，情怨芳心心哽噎，",
				Content: []string{
					"愁压蛾眉眉暗结。",
				},
			},
			wantTags:   []string{"愁绪"},
			rejectTags: []string{"相思"},
		},
		{
			name: "萧郎久去保留相思",
			sample: sampleRecord{
				PoemID: 110,
				Title:  "寄生草・为甚忧，为甚愁？为萧郎一去经今久。玉台宝鉴生尘垢，绿窗冷",
				Content: []string{
					"落闲针锈。",
					"岂知人玉腕钏儿松，岂知人两叶眉儿皱！",
				},
			},
			wantTags: []string{"相思"},
		},
		{
			name: "团圆相敬爱保留相思愁绪",
			sample: sampleRecord{
				PoemID: 149,
				Title:  "双调・新水令",
				Content: []string{
					"闲争夺鼎沸了丽春园，欠排场不堪久恋。",
					"时间相敬爱，端的怎团圆？",
					"白没事教人笑，惹人怨。",
				},
			},
			wantTags: []string{"相思", "愁绪"},
		},
		{
			name: "好事天悭保留相思",
			sample: sampleRecord{
				PoemID: 151,
				Title:  "天仙子・从今后，识破野狐涎。红粉无情，灾星不现。村酒酽野花浓，再",
				Content: []string{
					"不粘拈。",
					"当时话儿无应显，好事天悭。",
				},
			},
			wantTags: []string{"相思"},
		},
		{
			name: "密爱幽欢不能恋保留相思",
			sample: sampleRecord{
				PoemID: 169,
				Title:  "石竹子・夜夜嬉游赛上元，朝朝宴乐赏禁烟。密爱幽欢不能恋，无奈被名",
				Content: []string{
					"缰利锁牵。",
				},
			},
			wantTags: []string{"相思"},
		},
		{
			name: "别后相思保留相思愁绪但不是春天",
			sample: sampleRecord{
				PoemID: 190,
				Title:  "仙吕・桂枝香",
				Content: []string{
					"因他别后，恹恹消瘦。",
					"粉褪了雨后桃花，带宽了风前杨柳。",
					"这相思怎休？",
					"害得我天长地久，难禁难受！",
					"泪痕流，滴破芙蓉面，却似珍珠断线头。",
				},
			},
			wantTags:   []string{"相思", "愁绪"},
			rejectTags: []string{"春天"},
		},
		{
			name: "尊前离恨理行轩保留送别愁绪",
			sample: sampleRecord{
				PoemID: 196,
				Title:  "钱大尹智宠谢天香・仙吕/赏花时",
				Content: []string{
					"则这一曲翻成和泪篇，最苦偏高离恨天，双泪落尊前。",
					"山长水远，愁见理行轩。",
				},
			},
			wantTags:   []string{"送别", "愁绪"},
			rejectTags: []string{"山水"},
		},
		{
			name: "并头莲顾恋保留相思",
			sample: sampleRecord{
				PoemID: 197,
				Title:  "钱大尹智宠谢天香・玄篇",
				Content: []string{
					"待得鸾胶续断弦，欲盼雕鞍难顾恋。",
					"谢他新理任这官员，常好是与民方便，咱又得个一夜并头莲。",
				},
			},
			wantTags: []string{"相思"},
		},
		{
			name: "暮云遮离情第一夜保留送别愁绪",
			sample: sampleRecord{
				PoemID: 278,
				Title:  "草桥店梦莺莺(第四本)・双调/新水令",
				Content: []string{
					"望薄东萧寺暮云遮，惨离情半林黄叶。",
					"马迟人意懒，风急雁行斜。",
					"离恨重叠，破题儿第一夜。",
				},
			},
			wantTags: []string{"送别", "愁绪"},
		},
		{
			name: "愁山闷海是情绪比喻不是山水",
			sample: sampleRecord{
				PoemID: 310,
				Title:  "仙吕・祆神急",
				Content: []string{
					"绿阴笼小院，红雨点苍苔。",
					"谁想东君也是人间客，纵分连理枝，谩解合欢带，伤春早是心地窄。",
					"愁山和闷海，畅会栽排。",
				},
			},
			wantTags:   []string{"愁绪"},
			rejectTags: []string{"山水"},
		},
		{
			name: "金陵故址故国怀古不是个人思乡",
			sample: sampleRecord{
				PoemID: 321,
				Title:  "越调・柳营曲金陵故址",
				Content: []string{
					"临故国，认残碑，伤心六朝如逝水。",
					"四围山护绕，几处树高低。",
					"谁，曾赋黍离离。",
				},
			},
			wantTags:   []string{"家国", "山水"},
			rejectTags: []string{"思乡"},
		},
		{
			name: "万山烟水旅途概括不是山水主题",
			sample: sampleRecord{
				PoemID: 370,
				Title:  "同前",
				Content: []string{
					"那日孩儿，私奔故里，历尽万山烟水。",
					"途中寂寞痛伤悲，到了东平得见伊。",
				},
			},
			wantTags:   []string{"愁绪"},
			rejectTags: []string{"山水"},
		},
		{
			name: "武陵溪畔典故不是山水主题",
			sample: sampleRecord{
				PoemID: 378,
				Title:  "李云英风送梧桐叶・寄生草",
				Content: []string{
					"是何处风流客，谁家年少人？",
					"莫不是游仙梦里乍相逢，多管是武陵溪畔曾相近。",
				},
			},
			rejectTags: []string{"山水"},
		},
		{
			name: "春风翡翠巢姻缘比喻不是春天",
			sample: sampleRecord{
				PoemID: 403,
				Title:  "李云英风送梧桐叶・斗鹌鹑",
				Content: []string{
					"再寻个凤友鸾交，分甚么文强武弱。",
					"有福分先夺春风翡翠巢，美姻缘天凑巧。",
				},
			},
			rejectTags: []string{"春天"},
		},
		{
			name: "沙场剧情不是边塞主题",
			sample: sampleRecord{
				PoemID: 418,
				Title:  "庞涓夜走马陵道・油葫芦",
				Content: []string{
					"我这里布网张罗打大虫，谁着你将军校冲，早沙场上杀的血染马蹄红。",
					"我将你捉在马前，你今日落在彀中。",
				},
			},
			rejectTags: []string{"边塞"},
		},
		{
			name: "哀告求情问答不是愁绪主导",
			sample: sampleRecord{
				PoemID: 420,
				Title:  "庞涓夜走马陵道・后庭花",
				Content: []string{
					"我喜的是弟兄每两意同，你则待执轮竿作钓翁。",
					"哀告这掌军权的燕孙膑。",
					"若到那殿庭中，怎忘了弟兄的情重。",
				},
			},
			rejectTags: []string{"愁绪"},
		},
		{
			name: "弓弯秋月海沸山裂战争夸喻不是月亮山水",
			sample: sampleRecord{
				PoemID: 438,
				Title:  "庞涓夜走马陵道・中吕/粉蝶儿",
				Content: []string{
					"打一轮皂盖轻车，按天书把三军摆设，谁识俺这阵似长蛇。",
					"端的个角生风，旗掣电，弓弯秋月。",
					"喊一声海沸山裂，管杀的他众儿郎不能相借。",
				},
			},
			rejectTags: []string{"月亮", "山水"},
		},
		{
			name: "水底捞明月比喻不是月亮主题",
			sample: sampleRecord{
				PoemID: 444,
				Title:  "庞涓夜走马陵道・朝天子",
				Content: []string{
					"你如今死也，再休想放舍，恰便似水底捞明月。",
				},
			},
			rejectTags: []string{"月亮"},
		},
		{
			name: "标题征夫但正文离乡背井不足以标边塞",
			sample: sampleRecord{
				PoemID: 455,
				Title:  "幺・征夫梦寐清，深夜疆场静，四面悲歌忍泪听。",
				Content: []string{
					"想离乡背井。",
				},
			},
			rejectTags: []string{"边塞"},
		},
		{
			name: "芳草封高冢墓地伤情不是春天",
			sample: sampleRecord{
				PoemID: 458,
				Title:  "幺・绝疑的宝剑挥圆颈",
				Content: []string{
					"望碧云芳草封高冢，对黄土寒沙赴浅坑。",
					"伤情兴，须臾天晓，仿佛平明。",
				},
			},
			rejectTags: []string{"春天"},
		},
		{
			name: "玉兔东生保留月亮但愁甚么前程不标愁绪",
			sample: sampleRecord{
				PoemID: 484,
				Title:  "汉钟离度脱蓝采和・梁州",
				Content: []string{
					"直吃的簌簌的红轮西坠，焱焱的玉兔东生。",
					"若逢，对棚，怎生来妆点的排场盛。",
					"那的愁甚么前程。",
				},
			},
			wantTags:   []string{"月亮"},
			rejectTags: []string{"愁绪"},
		},
		{
			name: "洞房花烛团圆宴乐不额外标相思",
			sample: sampleRecord{
				PoemID: 414,
				Title:  "李云英风送梧桐叶・鸳鸯煞",
				Content: []string{
					"我则道凉宵衾枕无人共，谁承望洞房花烛笙歌送。",
					"乐事重重，喜气融融。",
					"畅道人月团圆，鱼水和同，依旧的举案齐眉，到老相陪奉。",
					"金榜挂名双及第，洞房花烛两团圆。",
				},
			},
			wantTags:   []string{"宴乐歌舞"},
			rejectTags: []string{"相思"},
		},
		{
			name: "山寿人名花木堂不是山水",
			sample: sampleRecord{
				PoemID: 504,
				Title:  "董秀英花月东墙记・幺篇",
				Content: []string{
					"老夫有一小顽，名曰山寿，就托足下教训攻书。",
					"老夫东墙下有一花木堂，先生就在其中设馆。",
				},
			},
			rejectTags: []string{"山水"},
		},
		{
			name: "黄河连城婚姻阻隔比喻不是山水",
			sample: sampleRecord{
				PoemID: 532,
				Title:  "董秀英花月东墙记・二煞",
				Content: []string{
					"婚姻配偶迟，难挨更漏永，画蛾眉懒去临妆镜。",
					"老天不管人憔翠，一派黄河九遍清。",
					"贞烈性，也只是粉墙一堵，似隔着百座连城。",
				},
			},
			rejectTags: []string{"山水"},
		},
		{
			name: "羞态整理金钗不是愁绪",
			sample: sampleRecord{
				PoemID: 546,
				Title:  "董秀英花月东墙记・二煞",
				Content: []string{
					"灯前试把香罗看，点点猩红映莹白。",
					"则见他羞无奈，困腾腾倚墙靠壁，急忙忙重整金钗。",
				},
			},
			rejectTags: []string{"愁绪"},
		},
		{
			name: "成姻眷登科得志不是相思",
			sample: sampleRecord{
				PoemID: 574,
				Title:  "董秀英花月东墙记・鸳鸯煞",
				Content: []string{
					"佳人才子心留恋，东墙花下成姻眷，标写青编，唱道一举登科将名姓显。",
					"男儿得志共赏在琼林宴，玉堂中千古名贤。",
				},
			},
			rejectTags: []string{"相思"},
		},
		{
			name: "寻梅老迈倦客不是相思",
			sample: sampleRecord{
				PoemID: 575,
				Title:  "仙吕・忆王孙寻梅",
				Content: []string{
					"寻香曾到葛仙台，踏雪今临和靖宅，横斜数枝僧寺侧。",
					"老迈情怀悲倦客，吟笔未成贾谊策。",
				},
			},
			rejectTags: []string{"相思"},
		},
		{
			name: "歌楼酒力雪景不是宴乐歌舞",
			sample: sampleRecord{
				PoemID: 598,
				Title:  "山神庙裴度还带・隔尾",
				Content: []string{
					"这其间正乱飘僧舍茶烟湿，密洒歌楼酒力微，青山也白头老了尘世。",
					"都不到一时半刻，可又早周围四壁，添我在冰壶画图里。",
				},
			},
			rejectTags: []string{"宴乐歌舞"},
		},
		{
			name: "患难哀怜报恩不是愁绪主旨",
			sample: sampleRecord{
				PoemID: 599,
				Title:  "山神庙裴度还带・牧羊关",
				Content: []string{
					"念小生居在白屋，处于布衣，多感谢长老慈悲！",
					"你今日患难哀怜我，久以后得峥嵘答报你。",
				},
			},
			rejectTags: []string{"愁绪"},
		},
		{
			name: "有家难奔山河容易改不是家国山水",
			sample: sampleRecord{
				PoemID: 592,
				Title:  "山神庙裴度还带・收江南",
				Content: []string{
					"呀，常言道有家难奔，有国难投。",
					"山河容易改，贫穷最难受。",
				},
			},
			wantTags:   []string{"愁绪"},
			rejectTags: []string{"家国", "山水"},
		},
		{
			name: "山神庙标题和山根面相不是山水",
			sample: sampleRecord{
				PoemID: 603,
				Title:  "山神庙裴度还带・贺新郎",
				Content: []string{
					"通神的许负细详推，地阁天仓，兰台廷尉测他那山根印堂人中贵。",
					"断祸福、观气色、占凶吉。",
				},
			},
			rejectTags: []string{"山水"},
		},
		{
			name: "破庙水浸不是山水",
			sample: sampleRecord{
				PoemID: 608,
				Title:  "山神庙裴度还带・醉太平",
				Content: []string{
					"更和这水浸过这笆箔。",
					"这一座十疏九漏山神庙，如十花九裂寒冰窖。",
				},
			},
			rejectTags: []string{"山水"},
		},
		{
			name: "还带报恩且贫不忧愁不是愁绪",
			sample: sampleRecord{
				PoemID: 616,
				Title:  "山神庙裴度还带・倘秀才",
				Content: []string{
					"这的是贫不忧愁富不骄。",
					"投之以木桃，报之以琼瑶。",
				},
			},
			rejectTags: []string{"愁绪"},
		},
		{
			name: "上青山变化身美貌比喻不是山水愁绪",
			sample: sampleRecord{
				PoemID: 625,
				Title:  "山神庙裴度还带・水仙子",
				Content: []string{
					"想起他那芙蓉娇貌蕙兰魂，杨柳纤腰红杏春，海棠颜色江梅韵。",
					"他恨不的上青山变化身，这其间卖登科寻觅回文。",
				},
			},
			rejectTags: []string{"山水", "愁绪"},
		},
		{
			name: "不同桃李芳不是春天",
			sample: sampleRecord{
				PoemID: 632,
				Title:  "余音・溪南剩把茅堂构",
				Content: []string{
					"赠妓桂香秀马氏不同桃李芳，自历风霜久。",
					"幽姿超万卉，素质压凡流。",
				},
			},
			rejectTags: []string{"春天"},
		},
		{
			name: "莫愁人名不作愁绪",
			sample: sampleRecord{
				PoemID: 633,
				Title:  "梁州・银蟾影里孤根瘦",
				Content: []string{
					"银蟾影里孤根瘦，玉兔光中万粟稠。",
					"舞霓裳步撒香钩，莫愁，见羞。",
				},
			},
			wantTags:   []string{"月亮", "宴乐歌舞"},
			rejectTags: []string{"愁绪"},
		},
		{
			name: "镜水山海盟誓不是山水",
			sample: sampleRecord{
				PoemID: 634,
				Title:  "梁州・秦台夜月乘鸾凤",
				Content: []string{
					"秦台夜月乘鸾凤，谢馆春风醉管弦。",
					"临鸾镜水映红莲，按银筝珠玑错落，歌白雪金玉相宣。",
					"山海深盟胶漆坚。",
				},
			},
			wantTags:   []string{"月亮", "宴乐歌舞"},
			rejectTags: []string{"山水"},
		},
		{
			name: "寿颂南山北海桑榆不是山水文旅",
			sample: sampleRecord{
				PoemID: 637,
				Title:  "余音・香缣貌得三冬景",
				Content: []string{
					"寿人八十南山颂载歌，北海樽频敬。",
					"享期颐松柏遐龄，宜受用桑榆晚景。",
				},
			},
			rejectTags: []string{"山水", "文旅"},
		},
		{
			name: "元宵寿宴黄河祝寿不是山水文旅",
			sample: sampleRecord{
				PoemID: 638,
				Title:  "梁州・碧天边一点孤星现",
				Content: []string{
					"正生甲却值元宵景。欢声涌沸，弦管铿锵。",
					"高开绮筵，胜会宾朋。歌白雪金钗列整。",
					"见尽黄河几浅清，则愿寿等岗陵。",
				},
			},
			wantTags:   []string{"宴乐歌舞"},
			rejectTags: []string{"山水", "文旅"},
		},
		{
			name: "桃李开青春不再来不是春天",
			sample: sampleRecord{
				PoemID: 643,
				Title:  "江州司马青衫泪・天下乐",
				Content: []string{
					"他管甚桃李开，风雨筛，更问甚青春不再来。",
				},
			},
			rejectTags: []string{"春天"},
		},
		{
			name: "山河易改不是家国山水",
			sample: sampleRecord{
				PoemID: 648,
				Title:  "江州司马青衫泪・赚煞",
				Content: []string{
					"俺娘山河易改，解元每少怪。",
				},
			},
			rejectTags: []string{"家国", "山水"},
		},
		{
			name: "昭君出塞典故不是边塞",
			sample: sampleRecord{
				PoemID: 664,
				Title:  "江州司马青衫泪・搅筝琵",
				Content: []string{
					"都是你个琵琶罪，少欢乐足别离。",
					"为你引商妇到江南，送昭君出塞北。",
					"紫檀面拂金猊，越引的我伤悲。",
					"想故人何日回归，生被这四条弦拨俺在两下里。",
				},
			},
			wantTags:   []string{"送别"},
			rejectTags: []string{"边塞"},
		},
		{
			name: "秋月春花套语不是月亮春天",
			sample: sampleRecord{
				PoemID: 675,
				Title:  "江州司马青衫泪・粉蝶儿",
				Content: []string{
					"秋月春花，都出在侍郎门下。",
				},
			},
			rejectTags: []string{"月亮", "春天"},
		},
		{
			name: "山呼朝拜不是山水",
			sample: sampleRecord{
				PoemID: 677,
				Title:  "江州司马青衫泪・迎仙客",
				Content: []string{
					"无礼法，妇人家，山呼委实不会他。",
				},
			},
			rejectTags: []string{"山水"},
		},
		{
			name: "寄哀书假书骗局不是愁绪",
			sample: sampleRecord{
				PoemID: 680,
				Title:  "江州司马青衫泪・红绣鞋",
				Content: []string{
					"可不这寄哀书的该万剐！",
					"老虔婆与茶客设计，寄假书一封，说侍郎死了。",
				},
			},
			rejectTags: []string{"愁绪"},
		},
		{
			name: "思往事不是相思",
			sample: sampleRecord{
				PoemID: 682,
				Title:  "江州司马青衫泪・普天乐",
				Content: []string{
					"思往事，空嗟呀。",
					"泪和愁付与琵琶。明月芦花。",
				},
			},
			wantTags:   []string{"月亮", "愁绪"},
			rejectTags: []string{"相思"},
		},
		{
			name: "寻春色风月不是春天",
			sample: sampleRecord{
				PoemID: 687,
				Title:  "江州司马青衫泪・蔓菁菜",
				Content: []string{
					"他当日为寻春色到儿家，便待强风情下榻。",
				},
			},
			rejectTags: []string{"春天"},
		},
		{
			name: "春风攀折凤城花不是春天",
			sample: sampleRecord{
				PoemID: 688,
				Title:  "江州司马青衫泪・随煞",
				Content: []string{
					"恰才来万里天涯，早愁鬓萧萧生白发。",
					"再不去趁春风攀折凤城花！",
				},
			},
			wantTags:   []string{"愁绪"},
			rejectTags: []string{"春天"},
		},
		{
			name: "西出阳关典故不是送别",
			sample: sampleRecord{
				PoemID: 689,
				Title:  "诸宫调风月紫云庭・幺",
				Content: []string{
					"西出阳关无故人，则见俺在这南国梁园依旧亲。",
				},
			},
			rejectTags: []string{"送别"},
		},
		{
			name: "不是相思病不标相思",
			sample: sampleRecord{
				PoemID: 691,
				Title:  "诸宫调风月紫云庭・油葫芦",
				Content: []string{
					"惭愧呵谢天地不是相思病。",
				},
			},
			rejectTags: []string{"相思"},
		},
		{
			name: "春风和气生不是春天",
			sample: sampleRecord{
				PoemID: 696,
				Title:  "诸宫调风月紫云庭・后庭花",
				Content: []string{
					"未见钱罗，呀，冬雪严霜降；得了钞罗，春风和气生。",
				},
			},
			rejectTags: []string{"春天"},
		},
		{
			name: "那咤短命贪财讽刺不是愁绪",
			sample: sampleRecord{
				PoemID: 697,
				Title:  "诸宫调风月紫云庭・幺",
				Content: []string{
					"也难奈何俺那六臂那咤般狠柳青。",
					"比的十恶罪尚尤轻。",
				},
			},
			rejectTags: []string{"愁绪"},
		},
		{
			name: "风月交易难成不是愁绪",
			sample: sampleRecord{
				PoemID: 698,
				Title:  "诸宫调风月紫云庭・赚尾",
				Content: []string{
					"把俺这等嘿交易难成？",
					"一分银买一分情！",
				},
			},
			rejectTags: []string{"愁绪"},
		},
		{
			name: "押送囚系别离不是送别但有愁绪",
			sample: sampleRecord{
				PoemID: 704,
				Title:  "诸宫调风月紫云庭・二煞",
				Content: []string{
					"我也觑不得这光景掩不迭这泪。",
					"我这壁道防送早催逼，他那壁带铁锁囚人监系，俺两处各心碎！",
					"是有遭间阻的也不似俺不吉利，兀的是甚末娘别离！",
				},
			},
			wantTags:   []string{"愁绪"},
			rejectTags: []string{"送别"},
		},
		{
			name: "艳歌银锣青楼语境不是宴乐歌舞",
			sample: sampleRecord{
				PoemID: 710,
				Title:  "诸宫调风月紫云庭・石榴花",
				Content: []string{
					"听一曲艳歌，细卷红罗。",
					"却则是央及杀那象板银锣。",
					"坐着俺那爱钞的的劣虔婆。",
				},
			},
			rejectTags: []string{"宴乐歌舞"},
		},
		{
			name: "披枷带锁对白不是愁绪",
			sample: sampleRecord{
				PoemID: 711,
				Title:  "诸宫调风月紫云庭・斗鹌鹑",
				Content: []string{
					"若是共别人并枕同床，他便不送得我披枷带锁！",
				},
			},
			rejectTags: []string{"愁绪"},
		},
		{
			name: "白云隔黄河是情感阻隔不是山水",
			sample: sampleRecord{
				PoemID: 714,
				Title:  "诸宫调风月紫云庭・哨遍",
				Content: []string{
					"越精细的越着他，怎出俺这打多情地网天罗。",
					"几时得两扶红日上青天，空望着一片白云隔黄河。",
				},
			},
			wantTags:   []string{"相思"},
			rejectTags: []string{"山水"},
		},
		{
			name: "楼心舞扇底歌风月厌倦不是宴乐歌舞",
			sample: sampleRecord{
				PoemID: 717,
				Title:  "诸宫调风月紫云庭・二煞",
				Content: []string{
					"委实倦那月斜杨柳楼心舞，风软桃花扇底歌。",
					"欲将这把戏都参破。",
				},
			},
			rejectTags: []string{"宴乐歌舞"},
		},
		{
			name: "云雨楚山高唐风月不是山水相思",
			sample: sampleRecord{
				PoemID: 719,
				Title:  "诸宫调风月紫云庭・双调/新水令",
				Content: []string{
					"何况这莺花燕市客，更逢着云雨楚山娘。",
					"我凭那想像高唐，怎强如俺满意宿鸳鸯。",
				},
			},
			rejectTags: []string{"山水", "相思"},
		},
		{
			name: "江岸渔船风雪只是剧情场景不是山水",
			sample: sampleRecord{
				PoemID: 731,
				Title:  "朱太守风雪渔樵记・天下乐",
				Content: []string{
					"那江岸边不是哥哥的渔船？",
					"雪下的紧，着哥哥久等也。",
					"哥哥，便好道风雪酒家天。",
				},
			},
			rejectTags: []string{"山水", "文旅"},
		},
		{
			name: "历史人物秦将军卒不是边塞",
			sample: sampleRecord{
				PoemID: 737,
				Title:  "朱太守风雪渔樵记・后庭花",
				Content: []string{
					"有一个秦白起是军卒。",
					"有一个灌将军曾贩屦。",
				},
			},
			rejectTags: []string{"边塞"},
		},
		{
			name: "蟠溪水上为渔是姜子牙典故不是山水",
			sample: sampleRecord{
				PoemID: 738,
				Title:  "朱太守风雪渔樵记・青哥儿",
				Content: []string{
					"则说那姜子牙，正与区区可比如。",
					"他也曾朝歌市里为屠，蟠溪水上为渔。",
				},
			},
			rejectTags: []string{"山水"},
		},
		{
			name: "月中丹桂科举典故不是月亮",
			sample: sampleRecord{
				PoemID: 739,
				Title:  "朱太守风雪渔樵记・赚煞",
				Content: []string{
					"别的书生说道月中丹桂，若到的那里，折得一枝回来。",
					"落可便我把那月中仙桂剖根除。",
				},
			},
			rejectTags: []string{"月亮"},
		},
		{
			name: "哭倒长城典故套话不是愁绪",
			sample: sampleRecord{
				PoemID: 741,
				Title:  "朱太守风雪渔樵记・快活三",
				Content: []string{
					"你怎不学孟姜女，把长城哭倒也则一声哀？",
					"将休书来，休书来！",
				},
			},
			rejectTags: []string{"愁绪"},
		},
		{
			name: "梁山伯人名不是山水",
			sample: sampleRecord{
				PoemID: 742,
				Title:  "朱太守风雪渔樵记・醉太平",
				Content: []string{
					"这梁山伯也不恋你祝英台，任从改嫁，并不争论。",
				},
			},
			rejectTags: []string{"山水"},
		},
		{
			name: "转述小恨不是愁绪相思",
			sample: sampleRecord{
				PoemID: 748,
				Title:  "朱太守风雪渔樵记・上小楼",
				Content: []string{
					"你道他忘人大恩，又道他记人小恨。",
					"谁着你生勒开他，生则同衾，死则同坟。",
				},
			},
			rejectTags: []string{"相思", "愁绪"},
		},
		{
			name: "风雪渔樵剧情收束不是山水愁绪",
			sample: sampleRecord{
				PoemID: 762,
				Title:  "朱太守风雪渔樵记・鸳鸯煞尾",
				Content: []string{
					"便是妻子何缘，早遂了团圆愿。",
					"倒与他后世流传，道这风雪渔樵也只落的做一场故事儿演。",
					"怀旧恨夫妇两参商，覆盆水险做傍州例。",
				},
			},
			rejectTags: []string{"山水", "愁绪"},
		},
		{
			name: "一江春水自尽套语不是春天但有愁绪",
			sample: sampleRecord{
				PoemID: 779,
				Title:  "临江驿潇湘秋夜雨・昆江龙",
				Content: []string{
					"险些儿趁一江春水向东流。",
					"则我这一寸心怀千古恨，两条眉锁十分忧。",
				},
			},
			wantTags:   []string{"愁绪"},
			rejectTags: []string{"春天"},
		},
		{
			name: "淮河口楚峰头是地点推进不是山水",
			sample: sampleRecord{
				PoemID: 782,
				Title:  "临江驿潇湘秋夜雨・醉中天",
				Content: []string{
					"才救出淮河口，又送上楚峰头。",
					"俺那父亲呵，生死茫茫未可水。",
					"有多少雨泣云愁。",
				},
			},
			wantTags:   []string{"愁绪"},
			rejectTags: []string{"山水"},
		},
		{
			name: "江淮关山不归舟是行路思念不是山水",
			sample: sampleRecord{
				PoemID: 784,
				Title:  "临江驿潇湘秋夜雨・赚煞",
				Content: []string{
					"则他这胸臆卷江淮，宝剑辉星斗。",
					"想着你千里关山独自个走。",
					"休着我倚柴门，凝望断不归舟。",
				},
			},
			rejectTags: []string{"山水", "文旅"},
		},
		{
			name: "离别三年负心争执不是送别",
			sample: sampleRecord{
				PoemID: 786,
				Title:  "临江驿潇湘秋夜雨・牧羊关",
				Content: []string{
					"我和他离别了三年，我怎肯半星儿失志？",
					"他原来别寻了个女娇姿。",
				},
			},
			rejectTags: []string{"送别"},
		},
		{
			name: "淮河渡翻船剧情不是山水",
			sample: sampleRecord{
				PoemID: 805,
				Title:  "临江驿潇湘秋夜雨・货郎儿",
				Content: []string{
					"想着淮河渡翻船的这灾变，也是俺那时乖运蹇。",
					"排岸司救了咱性命，崔老的与我配了姻缘。",
				},
			},
			rejectTags: []string{"山水"},
		},
		{
			name: "薄命婵娟乐昌镜不是月亮",
			sample: sampleRecord{
				PoemID: 806,
				Title:  "临江驿潇湘秋夜雨・醉太平",
				Content: []string{
					"又打着我薄命的婵娟。",
					"险些儿做乐昌镜破不重圆。",
				},
			},
			rejectTags: []string{"月亮"},
		},
		{
			name: "射柳军马家国不误打边塞山水宴乐",
			sample: sampleRecord{
				PoemID: 810,
				Title:  "四丞相高会丽春堂・天下乐",
				Content: []string{
					"可正是气压山河百二雄，元也波戎，将军校统。",
					"宰臣每为头儿又尽忠，文官每守正直，武将每建大功。",
					"圣人的命着俺大小官员赴射柳会。",
					"我这官不为那武艺上得的，为我唱得好，弹得好，舞的好。",
				},
			},
			wantTags:   []string{"家国"},
			rejectTags: []string{"边塞", "山水", "宴乐歌舞"},
		},
		{
			name: "弓开秋月形容弓形不是月亮",
			sample: sampleRecord{
				PoemID: 814,
				Title:  "四丞相高会丽春堂・胜葫芦",
				Content: []string{
					"忽的呵弓开秋月，扑的呵箭明飞金电。",
				},
			},
			rejectTags: []string{"月亮"},
		},
		{
			name: "射柳赏赐饮酒不是宴乐歌舞",
			sample: sampleRecord{
				PoemID: 815,
				Title:  "四丞相高会丽春堂・幺篇",
				Content: []string{
					"老丞相射中三箭也。",
					"令人将酒来，老水相满饮一杯。",
					"翠袖殷勤捧玉钟。",
				},
			},
			rejectTags: []string{"宴乐歌舞"},
		},
		{
			name: "官场酬酢饮酒不是宴乐歌舞",
			sample: sampleRecord{
				PoemID: 818,
				Title:  "四丞相高会丽春堂・迎仙客",
				Content: []string{
					"怎当他酬酢处两三巡，揭席时五六杯。",
					"醉的我将宫锦淋漓。",
				},
			},
			rejectTags: []string{"宴乐歌舞"},
		},
		{
			name: "春风一枝花出塞美人图不是春天边塞",
			sample: sampleRecord{
				PoemID: 832,
				Title:  "四丞相高会丽春堂・秃厮儿",
				Content: []string{
					"可人意清歌妙舞，酬吾志美酒鲜鱼。",
					"则这春风一枝花解语，似出塞美人图，可便妆梳。",
				},
			},
			wantTags:   []string{"宴乐歌舞"},
			rejectTags: []string{"春天", "边塞"},
		},
		{
			name: "春风一度际遇套语不是春天",
			sample: sampleRecord{
				PoemID: 838,
				Title:  "四丞相高会丽春堂・络丝娘",
				Content: []string{
					"到今日身无所如，想天公也有安排我处。",
					"可不道吕望、严陵自千古，这便算的我春风一度。",
				},
			},
			rejectTags: []string{"春天"},
		},
		{
			name: "长亭送行有山水送别但非宴乐歌舞",
			sample: sampleRecord{
				PoemID: 841,
				Title:  "四丞相高会丽春堂・收尾",
				Content: []string{
					"则我这好山好水难将去，待写入丹青画图。",
					"再到十里长亭，与丞相送行，走一遭去。",
					"香山设宴逞粗豪，久矣闲居更入朝。",
				},
			},
			wantTags:   []string{"山水", "送别"},
			rejectTags: []string{"宴乐歌舞"},
		},
		{
			name: "袖得春风马上归不是春天",
			sample: sampleRecord{
				PoemID: 863,
				Title:  "尾",
				Content: []string{
					"若是携得歌妓家中去，便是袖得春风马上归。",
				},
			},
			rejectTags: []string{"春天"},
		},
		{
			name: "尚留恋宴饮夜景不是相思",
			sample: sampleRecord{
				PoemID: 874,
				Title:  "耍鲍老南",
				Content: []string{
					"欢坐间，夜凉人静已，笑声接青霄内。",
					"纱笼罩仕女随，灯影下人扶起，尚留恋懒心回。",
				},
			},
			rejectTags: []string{"相思"},
		},
		{
			name: "岳阳楼下柳树神不是山水",
			sample: sampleRecord{
				PoemID: 881,
				Title:  "瘸李岳诗酒玩江亭・尾声",
				Content: []string{
					"俺这梅他粉包了心，檀黄嫩，插在那银瓶里宜得水温。",
					"比不的岳阳楼下枯干了的柳树神。",
				},
			},
			rejectTags: []string{"山水"},
		},
		{
			name: "展放开愁眉劝解不是愁绪",
			sample: sampleRecord{
				PoemID: 885,
				Title:  "瘸李岳诗酒玩江亭・锦上花",
				Content: []string{
					"则不如我展放开愁眉，休争闲气。",
					"则不如快活了一日，一日便宜。",
				},
			},
			rejectTags: []string{"愁绪"},
		},
		{
			name: "笙歌讥讽对白不是宴乐歌舞",
			sample: sampleRecord{
				PoemID: 886,
				Title:  "瘸李岳诗酒玩江亭・隔尾",
				Content: []string{
					"你挟着这半截家竹桶闲行立，你可甚么一部笙歌出入随。",
					"几曾见子弟舍里新添了个八仙队。",
				},
			},
			rejectTags: []string{"宴乐歌舞"},
		},
		{
			name: "清风明月为知友隐逸套语不是月亮",
			sample: sampleRecord{
				PoemID: 891,
				Title:  "瘸李岳诗酒玩江亭・石榴花",
				Content: []string{
					"我清风明月为知友。",
					"倚仗着你清风明月为知友，那的是阆苑神洲。",
				},
			},
			rejectTags: []string{"月亮"},
		},
		{
			name: "春山春水戏谑不是春天山水",
			sample: sampleRecord{
				PoemID: 895,
				Title:  "瘸李岳诗酒玩江亭・二煞",
				Content: []string{
					"你行处春日风动，你过处春山春水流，你模佯忒出丑。",
					"则恐怕误了你那春种秋收。",
				},
			},
			rejectTags: []string{"春天", "山水"},
		},
		{
			name: "蓬莱吹箫仙境保留山水不误判宴乐",
			sample: sampleRecord{
				PoemID: 904,
				Title:  "沙门岛张生煮海・油葫芦",
				Content: []string{
					"海上神仙年寿永，这蓬莱在眼界中。",
					"只待学吹箫同跨丹山凤，那其间，登碧落，趁天风。",
				},
			},
			wantTags:   []string{"山水"},
			rejectTags: []string{"宴乐歌舞"},
		},
		{
			name: "清风明月琴三弄是音乐不是月亮",
			sample: sampleRecord{
				PoemID: 908,
				Title:  "沙门岛张生煮海・六幺序",
				Content: []string{
					"表诉那弦中语，出落着指下功，胜檀槽慢掇轻拢。",
					"你听这清风明月琴三弄，端的个金徽汹涌，玉轸玲珑。",
				},
			},
			wantTags:   []string{"宴乐歌舞"},
			rejectTags: []string{"月亮"},
		},
		{
			name: "水族将军龙宫不是边塞",
			sample: sampleRecord{
				PoemID: 911,
				Title:  "沙门岛张生煮海・后庭花",
				Content: []string{
					"只在这沧海三千丈，险似那巫山十二峰。",
					"无非足蛟虬参从，还有那鼋将军、鳖相公、鱼大人、虾爱宠、鼍先锋、龟老翁。",
				},
			},
			wantTags:   []string{"山水"},
			rejectTags: []string{"边塞"},
		},
		{
			name: "煮海失约是相思不是山水",
			sample: sampleRecord{
				PoemID: 919,
				Title:  "沙门岛张生煮海・滚绣球",
				Content: []string{
					"小生前夜在于寺中操琴，有一女子前来窃听。",
					"他说是龙氏三娘，小字琼莲，亲许我中秋会约。不见他来，因此在这里煮海。",
				},
			},
			wantTags:   []string{"相思"},
			rejectTags: []string{"山水"},
		},
		{
			name: "相思海上方不是山水",
			sample: sampleRecord{
				PoemID: 920,
				Title:  "沙门岛张生煮海・滚绣球",
				Content: []string{
					"你那里得熬煎铅汞山头火？你那里觅医治相思海上方？",
					"东海龙神着老僧来做媒，招你为东床娇客。",
				},
			},
			wantTags:   []string{"相思"},
			rejectTags: []string{"山水"},
		},
		{
			name: "撮合山媒人不是山水但有相思",
			sample: sampleRecord{
				PoemID: 925,
				Title:  "沙门岛张生煮海・尾声",
				Content: []string{
					"意相投，姻缘可配当；心厮爱，夫妻谁比方。",
					"须将俺撮合山的媒人重重赏。",
				},
			},
			wantTags:   []string{"相思"},
			rejectTags: []string{"山水"},
		},
		{
			name: "水晶宫兵卒不是边塞",
			sample: sampleRecord{
				PoemID: 927,
				Title:  "沙门岛张生煮海・驻马听",
				Content: []string{
					"摆列着水里兵卒，都是些鼋将军、鼍先锋、鳖大夫。",
					"则俺这水晶宫是一搭儿奢华处。",
				},
			},
			rejectTags: []string{"边塞"},
		},
		{
			name: "山阴没缆舟不是山水",
			sample: sampleRecord{
				PoemID: 946,
				Title:  "半夜雷轰荐福碑・仙吕/赏花时",
				Content: []string{
					"我恰做访戴山阴王子猷，身似飘飘没缆舟。",
					"则这客僧投寺宿，措大谒儒流。",
				},
			},
			rejectTags: []string{"山水"},
		},
		{
			name: "山海似恩不是山水",
			sample: sampleRecord{
				PoemID: 955,
				Title:  "半夜雷轰荐福碑・煞尾",
				Content: []string{
					"若不是吾兄义气高，若不是哥哥怎生了？",
					"山海也似恩临决然报！",
				},
			},
			rejectTags: []string{"山水"},
		},
		{
			name: "月明故人来不是月亮但有愁绪",
			sample: sampleRecord{
				PoemID: 958,
				Title:  "半夜雷轰荐福碑・石榴花",
				Content: []string{
					"不想俺那月明千里故人来，他见我便困在、万丈尘埃。",
					"倚仗着他三封书，还了我这饥寒债。",
				},
			},
			wantTags:   []string{"愁绪"},
			rejectTags: []string{"月亮"},
		},
		{
			name: "江淮海狱山崖夸饰不是山水",
			sample: sampleRecord{
				PoemID: 966,
				Title:  "半夜雷轰荐福碑・鲍老儿",
				Content: []string{
					"我腹怀锦绣，剑挥星斗，胸卷江淮。",
					"饶你冲开海狱，磨昏日月，崩塌山崖。",
				},
			},
			rejectTags: []string{"山水"},
		},
		{
			name: "庐山东海龙王佛会不是山水",
			sample: sampleRecord{
				PoemID: 968,
				Title:  "半夜雷轰荐福碑・耍孩儿",
				Content: []string{
					"未曾结庐山长老白莲社，正遇着东海龙王大会垓。",
					"将这座药师佛海会，都变作赵太祖凶宅。",
				},
			},
			rejectTags: []string{"山水"},
		},
		{
			name: "未遂边塞统军不是边塞",
			sample: sampleRecord{
				PoemID: 983,
				Title:  "摩利支飞刀对箭・混江龙",
				Content: []string{
					"我不能够边塞上统军居帅府，丹墀内束带立于朝。",
					"空着我便眼巴巴盼不到长安道。",
				},
			},
			rejectTags: []string{"边塞"},
		},
		{
			name: "父母挽留是送别不是相思",
			sample: sampleRecord{
				PoemID: 988,
				Title:  "摩利支飞刀对箭・后庭花",
				Content: []string{
					"休将你这歹孩儿留恋着，枉把我这功名来耽误了。",
					"孩儿也，便好道父母在堂，不可远游也。",
				},
			},
			wantTags:   []string{"送别"},
			rejectTags: []string{"相思"},
		},
		{
			name: "我愁甚么是否定愁绪",
			sample: sampleRecord{
				PoemID: 992,
				Title:  "摩利支飞刀对箭・滚绣球",
				Content: []string{
					"存的我这胸中三卷黄公略，我愁甚么架上三封天子书。",
					"恰便似饿虎当途。",
				},
			},
			rejectTags: []string{"愁绪"},
		},
		{
			name: "逢山开路遇水叠桥不是山水",
			sample: sampleRecord{
				PoemID: 994,
				Title:  "摩利支飞刀对箭・朝天子",
				Content: []string{
					"兀那厮，你是军健汉，逢山开路，遇水叠桥，你敢去么？",
					"我马到处写满了您那功劳簿。",
				},
			},
			rejectTags: []string{"山水"},
		},
		{
			name: "百二山河壮帝居是家国不是山水",
			sample: sampleRecord{
				PoemID: 997,
				Title:  "摩利支飞刀对箭・尾声",
				Content: []string{
					"愿吾皇永坐着宗庙旧，家邦老，万万载百二山河壮帝居。",
					"到来日看排兵，列士卒；荡征尘，腾土雨。",
				},
			},
			wantTags:   []string{"家国"},
			rejectTags: []string{"山水"},
		},
		{
			name: "青山天涯海边追击不是山水",
			sample: sampleRecord{
				PoemID: 999,
				Title:  "摩利支飞刀对箭・幺篇",
				Content: []string{
					"你可甚为看青山懒赠鞭，看的俺唐十宰公卿如芥藓。",
					"离不了天涯和那海边！我与你直赶到他这个焰魔天！",
				},
			},
			rejectTags: []string{"山水"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			candidate := generateRulesCandidate(tt.sample)
			tagSet := map[string]bool{}
			for _, tag := range candidate.ProposedTags {
				tagSet[tag.Name] = true
			}
			for _, want := range tt.wantTags {
				assert.Truef(t, tagSet[want], "expected tag %q in %#v", want, candidate.ProposedTags)
			}
			for _, rejected := range tt.rejectTags {
				assert.Falsef(t, tagSet[rejected], "unexpected tag %q in %#v", rejected, candidate.ProposedTags)
			}
			assert.NotContains(t, candidate.ProposedKnowledge.Summary, "规则生成草稿")
			assert.Equal(t, rulesProviderModel, candidate.Meta["model"])
		})
	}
}

func TestGenerateRulesCandidateSkipsLowSignalGenericQuote(t *testing.T) {
	tests := []struct {
		name   string
		sample sampleRecord
	}{
		{
			name: "service dialogue should not fall back to generic quote",
			sample: sampleRecord{
				PoemID: 48,
				Title:  "\u8bc8\u59ae\u5b50\u8c03\u98ce\u6708\u30fb\u90a3\u54a4\u4ee4",
				Content: []string{
					"\u7b49\u4e0d\u5f97\u6c34\u6e29\uff0c\u4e00\u58f0\u8981\u9762\u76c6\uff1b\u6070\u9012\u4e0e\u9762\u76c6\uff0c\u4e00\u58f0\u8981\u624b\u5dfe\uff1b\u5374\u6267\u4e0e\u624b\u5dfe\uff0c\u4e00\u58f0\u89e3\u7ebd\u95e8\u3002",
					"\u4f7f\u7684\u4eba\u65e0\u6df9\u6da6\u3001\u767e\u822c\u652f\u5206\uff01",
				},
			},
		},
		{
			name: "undressing action should not fall back to generic quote",
			sample: sampleRecord{
				PoemID: 63,
				Title:  "\u8bc8\u59ae\u5b50\u8c03\u98ce\u6708\u30fb\u5341\u4e8c\u6708",
				Content: []string{
					"\u76f4\u5230\u4e2a\u5929\u660f\u5730\u9ed1\uff0c\u4e0d\u80af\u66f4\u6362\u8863\u8882\uff1b\u628a\u5154\u9e58\u89e3\u5f00\uff0c\u7ebd\u6263\u76f8\u79bb\uff0c\u628a\u8884\u5b50\u758f\u524c\u524c\u677e\u5f00\u4e0a\u62c6\uff0c\u5c06\u624b\u5e15\u6487\u6f3e\u5728\u7530\u5730\u3002",
				},
			},
		},
		{
			name: "marriage dialogue should be skipped when no safe tag exists",
			sample: sampleRecord{
				PoemID: 90,
				Title:  "\u8bc8\u59ae\u5b50\u8c03\u98ce\u6708\u30fb\u4e54\u724c\u513f",
				Content: []string{
					"\u52d8\u5a5a\u5904\u6070\u5c81\u6570\uff0c\u51fa\u5ac1\u540e\u6709\u8863\u7984\u3002",
					"\u82e5\u8a00\u62db\u5973\u5a7f\uff0c\u4e0b\u8d22\u94b1\u5c06\u5a36\u8fc7\u53bb\u3002",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			candidate := generateRulesCandidate(tt.sample)
			assert.Empty(t, candidate.ProposedTags)
			assert.Empty(t, candidate.ProposedKnowledge.Summary)
			assert.Equal(t, rulesProviderModel, candidate.Meta["model"])
			assert.Equal(t, "low_signal_no_specific_tag", candidate.Meta["skipped_reason"])
		})
	}
}

func TestGenerateRulesCommandSkipsLowSignalRulesCandidate(t *testing.T) {
	tempDir := t.TempDir()
	input := filepath.Join(tempDir, "sample.jsonl")
	output := filepath.Join(tempDir, "candidates.jsonl")

	writeSampleJSONL(t, input, []sampleRecord{
		{
			PoemID: 48,
			Title:  "\u8bc8\u59ae\u5b50\u8c03\u98ce\u6708\u30fb\u90a3\u54a4\u4ee4",
			Content: []string{
				"\u7b49\u4e0d\u5f97\u6c34\u6e29\uff0c\u4e00\u58f0\u8981\u9762\u76c6\uff1b\u6070\u9012\u4e0e\u9762\u76c6\uff0c\u4e00\u58f0\u8981\u624b\u5dfe\uff1b\u5374\u6267\u4e0e\u624b\u5dfe\uff0c\u4e00\u58f0\u89e3\u7ebd\u95e8\u3002",
				"\u4f7f\u7684\u4eba\u65e0\u6df9\u6da6\u3001\u767e\u822c\u652f\u5206\uff01",
			},
		},
		{
			PoemID: 5,
			Title:  "\u767b\u9e73\u96c0\u697c",
			Content: []string{
				"\u767d\u65e5\u4f9d\u5c71\u5c3d\uff0c\u9ec4\u6cb3\u5165\u6d77\u6d41\u3002",
				"\u6b32\u7a77\u5343\u91cc\u76ee\uff0c\u66f4\u4e0a\u4e00\u5c42\u697c\u3002",
			},
		},
	})

	cmd := generateCmd()
	cmd.SetArgs([]string{"--provider", "rules", "--input", input, "--output", output})
	require.NoError(t, cmd.Execute())

	candidates, err := readCandidates(output)
	require.NoError(t, err)
	require.Len(t, candidates, 1)
	assert.Equal(t, int64(5), candidates[0].PoemID)
	assert.Equal(t, rulesProviderModel, candidates[0].Meta["model"])
}

func TestGenerateRulesCommandAllowsAllLowSignalBatchButValidateRejectsEmptyCandidates(t *testing.T) {
	tempDir := t.TempDir()
	input := filepath.Join(tempDir, "sample.jsonl")
	output := filepath.Join(tempDir, "candidates.jsonl")

	writeSampleJSONL(t, input, []sampleRecord{
		{
			PoemID: 48,
			Title:  "\u8bc8\u59ae\u5b50\u8c03\u98ce\u6708\u30fb\u90a3\u54a4\u4ee4",
			Content: []string{
				"\u7b49\u4e0d\u5f97\u6c34\u6e29\uff0c\u4e00\u58f0\u8981\u9762\u76c6\uff1b\u6070\u9012\u4e0e\u9762\u76c6\uff0c\u4e00\u58f0\u8981\u624b\u5dfe\uff1b\u5374\u6267\u4e0e\u624b\u5dfe\uff0c\u4e00\u58f0\u89e3\u7ebd\u95e8\u3002",
				"\u4f7f\u7684\u4eba\u65e0\u6df9\u6da6\u3001\u767e\u822c\u652f\u5206\uff01",
			},
		},
	})

	generate := generateCmd()
	generate.SetArgs([]string{"--provider", "rules", "--input", input, "--output", output})
	require.NoError(t, generate.Execute())

	candidates, err := readCandidates(output)
	require.NoError(t, err)
	require.Empty(t, candidates)

	validate := validateCmd()
	validate.SetArgs([]string{"--input", output, "--skip-db-check"})
	require.ErrorContains(t, validate.Execute(), "no candidates")
}

func writeSampleJSONL(t *testing.T, path string, records []sampleRecord) {
	t.Helper()
	file, err := os.Create(path)
	require.NoError(t, err)
	defer func() { _ = file.Close() }()
	encoder := json.NewEncoder(file)
	for _, record := range records {
		require.NoError(t, encoder.Encode(record))
	}
}

func writeGoldenJSONL(t *testing.T, path string, records []goldenSampleRecord) {
	t.Helper()
	file, err := os.Create(path)
	require.NoError(t, err)
	defer func() { _ = file.Close() }()
	encoder := json.NewEncoder(file)
	for _, record := range records {
		require.NoError(t, encoder.Encode(record))
	}
}

func writeCandidateJSONL(t *testing.T, path string, records []candidateRecord) {
	t.Helper()
	file, err := os.Create(path)
	require.NoError(t, err)
	defer func() { _ = file.Close() }()
	encoder := json.NewEncoder(file)
	for _, record := range records {
		require.NoError(t, encoder.Encode(record))
	}
}

func writeReviewJSONL(t *testing.T, path string, records []manualReviewInputRecord) {
	t.Helper()
	file, err := os.Create(path)
	require.NoError(t, err)
	defer func() { _ = file.Close() }()
	encoder := json.NewEncoder(file)
	for _, record := range records {
		require.NoError(t, encoder.Encode(record))
	}
}
