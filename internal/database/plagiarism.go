package database

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"math"
	"sort"
	"strings"
	"time"
	"unicode"

	"gorm.io/gorm"
)

const (
	PlagiarismRiskLow    = "low"
	PlagiarismRiskMedium = "medium"
	PlagiarismRiskHigh   = "high"
	PlagiarismRiskExact  = "exact_duplicate"

	PlagiarismStatusPending        = "pending"
	PlagiarismStatusPassed         = "passed"
	PlagiarismStatusMediumRisk     = "medium_risk"
	PlagiarismStatusHighRisk       = "high_risk"
	PlagiarismStatusExactDuplicate = "exact_duplicate"
)

// WorkFingerprint stores a normalized text fingerprint for one plagiarism check.
type WorkFingerprint struct {
	ID             int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	WorkID         int64     `gorm:"column:work_id;not null" json:"work_id"`
	NormalizedText string    `gorm:"column:normalized_text;not null" json:"normalized_text"`
	NormalizedHash string    `gorm:"column:normalized_hash;not null" json:"normalized_hash"`
	SimHash        string    `gorm:"column:simhash;not null" json:"simhash"`
	NGramJSON      string    `gorm:"column:ngram_json" json:"ngram_json,omitempty"`
	EmbeddingJSON  string    `gorm:"column:embedding_json" json:"embedding_json,omitempty"`
	CreatedAt      time.Time `gorm:"column:created_at" json:"created_at"`
}

func (WorkFingerprint) TableName() string { return "work_fingerprints" }

// PlagiarismReport records one automatic originality check.
type PlagiarismReport struct {
	ID                int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	WorkID            int64     `gorm:"column:work_id;not null" json:"work_id"`
	NormalizedHash    string    `gorm:"column:normalized_hash;not null" json:"normalized_hash"`
	SimHash           string    `gorm:"column:simhash;not null" json:"simhash"`
	RiskLevel         string    `gorm:"column:risk_level;not null" json:"risk_level"`
	RiskReason        string    `gorm:"column:risk_reason" json:"risk_reason,omitempty"`
	ExactMatchCount   int       `gorm:"column:exact_match_count;not null" json:"exact_match_count"`
	SimilarMatchCount int       `gorm:"column:similar_match_count;not null" json:"similar_match_count"`
	TopMatchesJSON    string    `gorm:"column:top_matches_json" json:"top_matches_json,omitempty"`
	ReviewStatus      string    `gorm:"column:review_status;not null" json:"review_status"`
	CreatedAt         time.Time `gorm:"column:created_at" json:"created_at"`
}

func (PlagiarismReport) TableName() string { return "plagiarism_reports" }

// SimilarityMatch records one matched source from ancient poems or original works.
type SimilarityMatch struct {
	ID           int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	ReportID     int64     `gorm:"column:report_id;not null" json:"report_id"`
	WorkID       int64     `gorm:"column:work_id;not null" json:"work_id"`
	SourceType   string    `gorm:"column:source_type;not null" json:"source_type"`
	SourceID     string    `gorm:"column:source_id;not null" json:"source_id"`
	SourceTitle  string    `gorm:"column:source_title" json:"source_title,omitempty"`
	SourceAuthor string    `gorm:"column:source_author" json:"source_author,omitempty"`
	Similarity   float64   `gorm:"column:similarity;not null" json:"similarity"`
	MatchType    string    `gorm:"column:match_type;not null" json:"match_type"`
	Excerpt      string    `gorm:"column:excerpt" json:"excerpt,omitempty"`
	CreatedAt    time.Time `gorm:"column:created_at" json:"created_at"`
}

func (SimilarityMatch) TableName() string { return "similarity_matches" }

// ManualReviewQueueItem marks high-risk works for operator review.
type ManualReviewQueueItem struct {
	ID          int64      `gorm:"primaryKey;autoIncrement" json:"id"`
	WorkID      int64      `gorm:"column:work_id;not null" json:"work_id"`
	ReportID    int64      `gorm:"column:report_id;not null" json:"report_id"`
	RiskLevel   string     `gorm:"column:risk_level;not null" json:"risk_level"`
	Reason      string     `gorm:"column:reason" json:"reason,omitempty"`
	Status      string     `gorm:"column:status;not null" json:"status"`
	Reviewer    string     `gorm:"column:reviewer" json:"reviewer,omitempty"`
	ReviewNotes string     `gorm:"column:review_notes" json:"review_notes,omitempty"`
	DecidedAt   *time.Time `gorm:"column:decided_at" json:"decided_at,omitempty"`
	CreatedAt   time.Time  `gorm:"column:created_at" json:"created_at"`
	UpdatedAt   time.Time  `gorm:"column:updated_at" json:"updated_at"`
}

func (ManualReviewQueueItem) TableName() string { return "manual_review_queue" }

// PlagiarismReportWithMatches is returned to API callers.
type PlagiarismReportWithMatches struct {
	Report  PlagiarismReport  `json:"report"`
	Matches []SimilarityMatch `json:"matches"`
}

type plagiarismCheckResult struct {
	report  PlagiarismReport
	matches []SimilarityMatch
}

type plagiarismSource struct {
	sourceType string
	sourceID   string
	title      string
	author     string
	content    string
	embedding  []float64
}

// LatestPlagiarismReport returns the newest report for an owned work.
func (r *Repository) LatestPlagiarismReport(apiKeyID, workID int64) (*PlagiarismReportWithMatches, error) {
	if _, err := r.GetOriginalWork(apiKeyID, workID); err != nil {
		return nil, err
	}

	var report PlagiarismReport
	if err := r.db.Where("work_id = ?", workID).Order("created_at DESC, id DESC").First(&report).Error; err != nil {
		return nil, err
	}

	var matches []SimilarityMatch
	if err := r.db.Where("report_id = ?", report.ID).Order("similarity DESC, id ASC").Find(&matches).Error; err != nil {
		return nil, err
	}

	return &PlagiarismReportWithMatches{Report: report, Matches: matches}, nil
}

func (r *Repository) runPlagiarismCheckTx(tx *gorm.DB, work *OriginalWork) (*plagiarismCheckResult, error) {
	normalized := normalizePlagiarismText(work.Content)
	if normalized == "" {
		return nil, fmt.Errorf("%w: content is required", ErrInvalidQueryParam)
	}

	ngrams := plagiarismNGrams(normalized, 4)
	normalizedHash := hashNormalizedText(normalized)
	simHash := plagiarismSimHash(normalized)
	ngramJSON := marshalStringSlice(ngrams)
	embedding := plagiarismEmbedding(normalized)
	embeddingJSON := marshalFloatSlice(embedding)

	fp := WorkFingerprint{
		WorkID:         work.ID,
		NormalizedText: normalized,
		NormalizedHash: normalizedHash,
		SimHash:        simHash,
		NGramJSON:      ngramJSON,
		EmbeddingJSON:  embeddingJSON,
	}
	if err := tx.Create(&fp).Error; err != nil {
		return nil, err
	}

	sources, err := r.findPlagiarismSourcesTx(tx, work, normalized)
	if err != nil {
		return nil, err
	}

	matches := scorePlagiarismSources(ngrams, normalizedHash, embedding, sources)
	riskLevel, reason, exactCount, similarCount := classifyPlagiarism(matches)
	reviewStatus := "auto_checked"
	if riskLevel == PlagiarismRiskHigh || riskLevel == PlagiarismRiskExact {
		reviewStatus = "manual_review_required"
	}

	topJSON := marshalSimilarityMatches(matches)
	report := PlagiarismReport{
		WorkID:            work.ID,
		NormalizedHash:    normalizedHash,
		SimHash:           simHash,
		RiskLevel:         riskLevel,
		RiskReason:        reason,
		ExactMatchCount:   exactCount,
		SimilarMatchCount: similarCount,
		TopMatchesJSON:    topJSON,
		ReviewStatus:      reviewStatus,
	}
	if err := tx.Create(&report).Error; err != nil {
		return nil, err
	}

	for i := range matches {
		matches[i].ReportID = report.ID
		matches[i].WorkID = work.ID
		if err := tx.Create(&matches[i]).Error; err != nil {
			return nil, err
		}
	}

	if reviewStatus == "manual_review_required" {
		item := ManualReviewQueueItem{
			WorkID:    work.ID,
			ReportID:  report.ID,
			RiskLevel: riskLevel,
			Reason:    reason,
			Status:    "pending",
		}
		if err := tx.Create(&item).Error; err != nil {
			return nil, err
		}
	}

	work.PlagiarismStatus = plagiarismStatusFromRisk(riskLevel)
	return &plagiarismCheckResult{report: report, matches: matches}, nil
}

func plagiarismBlocksPublish(risk string) bool {
	return risk == PlagiarismRiskExact || risk == PlagiarismRiskHigh
}

func plagiarismStatusFromRisk(risk string) string {
	switch risk {
	case PlagiarismRiskExact:
		return PlagiarismStatusExactDuplicate
	case PlagiarismRiskHigh:
		return PlagiarismStatusHighRisk
	case PlagiarismRiskMedium:
		return PlagiarismStatusMediumRisk
	default:
		return PlagiarismStatusPassed
	}
}

func (r *Repository) findPlagiarismSourcesTx(tx *gorm.DB, work *OriginalWork, normalized string) ([]plagiarismSource, error) {
	sources := make([]plagiarismSource, 0, 32)
	seeds := plagiarismCandidateSeeds(work.Content, normalized)
	seen := map[string]bool{}

	ancient, err := r.findAncientPoemSourcesTx(tx, seeds)
	if err != nil {
		return nil, err
	}
	for _, src := range ancient {
		key := src.sourceType + ":" + src.sourceID
		if !seen[key] {
			seen[key] = true
			sources = append(sources, src)
		}
	}

	originals, err := r.findOriginalWorkSourcesTx(tx, work, seeds)
	if err != nil {
		return nil, err
	}
	for _, src := range originals {
		key := src.sourceType + ":" + src.sourceID
		if !seen[key] {
			seen[key] = true
			sources = append(sources, src)
		}
	}

	corpus, err := r.findCorpusSourcesTx(tx)
	if err != nil {
		return nil, err
	}
	for _, src := range corpus {
		key := src.sourceType + ":" + src.sourceID
		if !seen[key] {
			seen[key] = true
			sources = append(sources, src)
		}
	}

	return sources, nil
}

func (r *Repository) findAncientPoemSourcesTx(tx *gorm.DB, seeds []string) ([]plagiarismSource, error) {
	if len(seeds) == 0 {
		return nil, nil
	}
	conditions := make([]string, 0, len(seeds)*2)
	args := make([]any, 0, len(seeds)*2)
	for _, seed := range seeds {
		like := "%" + seed + "%"
		conditions = append(conditions, "CAST(p.content AS TEXT) LIKE ?", "p.title LIKE ?")
		args = append(args, like, like)
	}

	var rows []struct {
		ID         int64  `gorm:"column:id"`
		Title      string `gorm:"column:title"`
		Content    string `gorm:"column:content"`
		AuthorName string `gorm:"column:author_name"`
	}
	err := tx.Table(r.poemsTable()+" AS p").
		Select("p.id, p.title, CAST(p.content AS TEXT) AS content, COALESCE(a.name, '') AS author_name").
		Joins("LEFT JOIN "+r.authorsTable()+" AS a ON a.id = p.author_id").
		Where(strings.Join(conditions, " OR "), args...).
		Limit(120).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	sources := make([]plagiarismSource, 0, len(rows))
	for _, row := range rows {
		sources = append(sources, plagiarismSource{
			sourceType: "ancient_poem",
			sourceID:   fmt.Sprintf("%d", row.ID),
			title:      row.Title,
			author:     row.AuthorName,
			content:    poemJSONText(row.Content),
		})
	}
	return sources, nil
}

func (r *Repository) findOriginalWorkSourcesTx(tx *gorm.DB, work *OriginalWork, seeds []string) ([]plagiarismSource, error) {
	query := tx.Model(&OriginalWork{}).Where("id <> ?", work.ID)
	if len(seeds) > 0 {
		conditions := []string{"content_hash = ?"}
		args := []any{work.ContentHash}
		for _, seed := range seeds {
			conditions = append(conditions, "content LIKE ?", "title LIKE ?")
			args = append(args, "%"+seed+"%", "%"+seed+"%")
		}
		query = query.Where(strings.Join(conditions, " OR "), args...)
	} else {
		query = query.Where("content_hash = ?", work.ContentHash)
	}

	var works []OriginalWork
	if err := query.Order("updated_at DESC, id DESC").Limit(120).Find(&works).Error; err != nil {
		return nil, err
	}
	sources := make([]plagiarismSource, 0, len(works))
	for _, other := range works {
		sources = append(sources, plagiarismSource{
			sourceType: "original_work",
			sourceID:   fmt.Sprintf("%d", other.ID),
			title:      other.Title,
			author:     fmt.Sprintf("api_key:%d", other.APIKeyID),
			content:    other.Content,
		})
	}
	return sources, nil
}

func (r *Repository) findCorpusSourcesTx(tx *gorm.DB) ([]plagiarismSource, error) {
	var rows []PlagiarismCorpusSource
	if err := tx.Model(&PlagiarismCorpusSource{}).
		Where("status = ?", PlagiarismCorpusStatusEnabled).
		Order("updated_at DESC, id DESC").
		Limit(300).
		Find(&rows).Error; err != nil {
		return nil, err
	}

	sources := make([]plagiarismSource, 0, len(rows))
	for _, row := range rows {
		sources = append(sources, plagiarismSource{
			sourceType: row.SourceType,
			sourceID:   fmt.Sprintf("%d", row.ID),
			title:      row.Title,
			author:     firstNonEmptyString(row.Author, row.SourceURL),
			content:    row.Content,
			embedding:  parseEmbeddingJSON(row.EmbeddingJSON),
		})
	}
	return sources, nil
}

func scorePlagiarismSources(ngrams []string, normalizedHash string, embedding []float64, sources []plagiarismSource) []SimilarityMatch {
	matches := make([]SimilarityMatch, 0, len(sources))
	for _, src := range sources {
		sourceNormalized := normalizePlagiarismText(src.content)
		if sourceNormalized == "" {
			continue
		}
		sourceHash := hashNormalizedText(sourceNormalized)
		sourceNGrams := plagiarismNGrams(sourceNormalized, 4)
		score := ngramSimilarity(ngrams, sourceNGrams)
		matchType := "ngram_overlap"
		if normalizedHash == sourceHash {
			score = 1
			matchType = "exact"
		} else {
			sourceEmbedding := src.embedding
			if len(sourceEmbedding) == 0 {
				sourceEmbedding = plagiarismEmbedding(sourceNormalized)
			}
			semanticScore := cosineSimilarity(embedding, sourceEmbedding)
			if semanticScore >= 0.72 && semanticScore > score {
				score = semanticScore
				matchType = "embedding_semantic"
			}
		}
		if score < 0.35 && matchType != "exact" {
			continue
		}
		matches = append(matches, SimilarityMatch{
			SourceType:   src.sourceType,
			SourceID:     src.sourceID,
			SourceTitle:  limitString(src.title, 160),
			SourceAuthor: limitString(src.author, 120),
			Similarity:   score,
			MatchType:    matchType,
			Excerpt:      plagiarismExcerpt(src.content),
		})
	}
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].Similarity == matches[j].Similarity {
			return matches[i].SourceID < matches[j].SourceID
		}
		return matches[i].Similarity > matches[j].Similarity
	})
	if len(matches) > 10 {
		matches = matches[:10]
	}
	return matches
}

func classifyPlagiarism(matches []SimilarityMatch) (risk, reason string, exactCount, similarCount int) {
	risk = PlagiarismRiskLow
	reason = "no high-similarity source was found in the ancient-poem, platform-original, semantic, or dispute-corpus indexes"
	for _, match := range matches {
		if match.MatchType == "exact" {
			exactCount++
		} else {
			similarCount++
		}
	}
	if exactCount > 0 {
		top := matches[0]
		return PlagiarismRiskExact, fmt.Sprintf("exact duplicate of %s %s", top.SourceType, top.SourceTitle), exactCount, similarCount
	}
	if len(matches) == 0 {
		return risk, reason, exactCount, similarCount
	}
	top := matches[0]
	if top.Similarity >= 0.82 {
		return PlagiarismRiskHigh, fmt.Sprintf("high similarity %.2f with %s %s", top.Similarity, top.SourceType, top.SourceTitle), exactCount, similarCount
	}
	if top.Similarity >= 0.55 {
		return PlagiarismRiskMedium, fmt.Sprintf("medium similarity %.2f with %s %s", top.Similarity, top.SourceType, top.SourceTitle), exactCount, similarCount
	}
	return risk, reason, exactCount, similarCount
}

func normalizePlagiarismText(value string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(value)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func hashNormalizedText(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func plagiarismNGrams(value string, size int) []string {
	runes := []rune(value)
	if len(runes) == 0 {
		return nil
	}
	if size < 1 {
		size = 4
	}
	if len(runes) < size {
		return []string{string(runes)}
	}
	set := make(map[string]struct{}, len(runes))
	for i := 0; i <= len(runes)-size; i++ {
		set[string(runes[i:i+size])] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for gram := range set {
		out = append(out, gram)
	}
	sort.Strings(out)
	return out
}

func ngramSimilarity(a, b []string) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	i, j, intersection := 0, 0, 0
	for i < len(a) && j < len(b) {
		switch {
		case a[i] == b[j]:
			intersection++
			i++
			j++
		case a[i] < b[j]:
			i++
		default:
			j++
		}
	}
	union := len(a) + len(b) - intersection
	if union <= 0 {
		return 0
	}
	jaccard := float64(intersection) / float64(union)
	containment := float64(intersection) / float64(minInt(len(a), len(b)))
	if containment > jaccard {
		return containment
	}
	return jaccard
}

func plagiarismEmbedding(value string) []float64 {
	const dims = 64
	vector := make([]float64, dims)
	runes := []rune(value)
	if len(runes) == 0 {
		return vector
	}
	for i, r := range runes {
		addEmbeddingFeature(vector, "c:"+string(r), 1.0)
		if i+1 < len(runes) {
			addEmbeddingFeature(vector, "b:"+string(runes[i:i+2]), 0.35)
		}
		if i+2 < len(runes) {
			addEmbeddingFeature(vector, "s:"+string([]rune{r, runes[i+2]}), 0.2)
		}
	}
	normalizeVector(vector)
	return vector
}

func addEmbeddingFeature(vector []float64, feature string, weight float64) {
	h := fnv.New64a()
	_, _ = h.Write([]byte(feature))
	sum := h.Sum64()
	idx := int(sum % uint64(len(vector)))
	sign := 1.0
	if (sum>>63)&1 == 1 {
		sign = -1.0
	}
	vector[idx] += sign * weight
}

func normalizeVector(vector []float64) {
	var norm float64
	for _, value := range vector {
		norm += value * value
	}
	if norm == 0 {
		return
	}
	norm = math.Sqrt(norm)
	for i := range vector {
		vector[i] = vector[i] / norm
	}
}

func cosineSimilarity(a, b []float64) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	n := minInt(len(a), len(b))
	var dot, normA, normB float64
	for i := 0; i < n; i++ {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	score := dot / (math.Sqrt(normA) * math.Sqrt(normB))
	if score < 0 {
		return 0
	}
	return score
}

func plagiarismCandidateSeeds(content string, normalized string) []string {
	seen := map[string]bool{}
	var seeds []string
	add := func(seed string) {
		seed = strings.TrimSpace(seed)
		if len([]rune(seed)) < 4 || seen[seed] {
			return
		}
		seen[seed] = true
		seeds = append(seeds, seed)
	}

	for _, line := range strings.FieldsFunc(content, func(r rune) bool {
		return r == '\n' || r == '\r' || r == '，' || r == '。' || r == ',' || r == '.' || r == ';' || r == '；'
	}) {
		n := normalizePlagiarismText(line)
		runes := []rune(n)
		if len(runes) >= 4 {
			if len(runes) > 8 {
				runes = runes[:8]
			}
			add(string(runes))
		}
	}

	runes := []rune(normalized)
	if len(runes) >= 4 {
		for _, start := range []int{0, len(runes) / 3, len(runes) / 2, maxInt(0, len(runes)-8)} {
			end := minInt(len(runes), start+8)
			if end-start >= 4 {
				add(string(runes[start:end]))
			}
		}
	}
	if len(seeds) > 8 {
		seeds = seeds[:8]
	}
	return seeds
}

func poemJSONText(value string) string {
	var parts []string
	if err := json.Unmarshal([]byte(value), &parts); err == nil && len(parts) > 0 {
		return strings.Join(parts, "\n")
	}
	return value
}

func plagiarismExcerpt(value string) string {
	value = strings.TrimSpace(poemJSONText(value))
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	return limitString(value, 240)
}

func plagiarismSimHash(value string) string {
	grams := plagiarismNGrams(value, 2)
	if len(grams) == 0 {
		grams = []string{value}
	}
	var weights [64]int
	for _, gram := range grams {
		h := fnv.New64a()
		_, _ = h.Write([]byte(gram))
		sum := h.Sum64()
		for i := 0; i < 64; i++ {
			if (sum>>i)&1 == 1 {
				weights[i]++
			} else {
				weights[i]--
			}
		}
	}
	var out uint64
	for i := 0; i < 64; i++ {
		if weights[i] > 0 {
			out |= 1 << i
		}
	}
	return fmt.Sprintf("%016x", out)
}

func marshalStringSlice(values []string) string {
	data, err := json.Marshal(values)
	if err != nil {
		return "[]"
	}
	return string(data)
}

func marshalFloatSlice(values []float64) string {
	data, err := json.Marshal(values)
	if err != nil {
		return "[]"
	}
	return string(data)
}

func parseEmbeddingJSON(value string) []float64 {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	var vector []float64
	if err := json.Unmarshal([]byte(value), &vector); err != nil {
		return nil
	}
	return vector
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func marshalSimilarityMatches(matches []SimilarityMatch) string {
	if len(matches) == 0 {
		return "[]"
	}
	type matchSummary struct {
		SourceType   string  `json:"source_type"`
		SourceID     string  `json:"source_id"`
		SourceTitle  string  `json:"source_title,omitempty"`
		SourceAuthor string  `json:"source_author,omitempty"`
		Similarity   float64 `json:"similarity"`
		MatchType    string  `json:"match_type"`
		Excerpt      string  `json:"excerpt,omitempty"`
	}
	summaries := make([]matchSummary, len(matches))
	for i, match := range matches {
		summaries[i] = matchSummary{
			SourceType:   match.SourceType,
			SourceID:     match.SourceID,
			SourceTitle:  match.SourceTitle,
			SourceAuthor: match.SourceAuthor,
			Similarity:   match.Similarity,
			MatchType:    match.MatchType,
			Excerpt:      match.Excerpt,
		}
	}
	data, err := json.Marshal(summaries)
	if err != nil {
		return "[]"
	}
	return string(data)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
