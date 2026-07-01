package main

import (
	"bufio"
	"bytes"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/palemoky/chinese-poetry-api/internal/database"
)

var (
	dbPath string
	lang   string
)

const rulesProviderModel = "rules-v13"

type sampleRecord struct {
	PoemID  int64    `json:"poem_id"`
	Title   string   `json:"title"`
	Content []string `json:"content"`
	Author  string   `json:"author,omitempty"`
	Dynasty string   `json:"dynasty,omitempty"`
	Type    string   `json:"type,omitempty"`
}

type goldenSampleRecord struct {
	PoemID     int64          `json:"poem_id"`
	Title      string         `json:"title"`
	Content    []string       `json:"content"`
	Author     string         `json:"author,omitempty"`
	Dynasty    string         `json:"dynasty,omitempty"`
	Type       string         `json:"type,omitempty"`
	GoldenMeta map[string]any `json:"golden_meta"`
}

type candidateRecord struct {
	PoemID            int64                           `json:"poem_id"`
	ProposedTags      []database.TagInput             `json:"proposed_tags"`
	ProposedKnowledge database.ProposedKnowledgeInput `json:"proposed_knowledge"`
	Meta              map[string]any                  `json:"meta,omitempty"`
}

func main() {
	rootCmd := &cobra.Command{
		Use:   "enrichment",
		Short: "AI data-enrichment sample, validation, import and rollback tools",
	}
	rootCmd.PersistentFlags().StringVar(&dbPath, "db", "data/poetry.db", "Path to poetry SQLite database")
	rootCmd.PersistentFlags().StringVar(&lang, "lang", "zh-Hans", "Language variant: zh-Hans or zh-Hant")
	rootCmd.AddCommand(
		exportSampleCmd(),
		exportGoldenSampleCmd(),
		goldenAuditCmd(),
		goldenPrefillCmd(),
		goldenReviewQueueCmd(),
		goldenReviewSheetCmd(),
		goldenReviewSheetAuditCmd(),
		goldenApplyReviewCmd(),
		goldenApplyReviewSheetCmd(),
		goldenToSampleCmd(),
		generateCmd(),
		validateCmd(),
		qualityGateCmd(),
		importCandidatesCmd(),
		sampleReviewCmd(),
		applyReviewCmd(),
		reviewAuditCmd(),
		reviewReportCmd(),
		rollbackCmd(),
	)

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func exportSampleCmd() *cobra.Command {
	var limit int
	var offset int
	var out string

	cmd := &cobra.Command{
		Use:   "export-sample",
		Short: "Export poems to JSONL for enrichment generation",
		RunE: func(cmd *cobra.Command, args []string) error {
			if limit < 1 {
				return fmt.Errorf("--limit must be positive")
			}
			if offset < 0 {
				return fmt.Errorf("--offset cannot be negative")
			}
			if strings.TrimSpace(out) == "" {
				return fmt.Errorf("--out is required")
			}

			repo, closeFn, err := openRepo()
			if err != nil {
				return err
			}
			defer closeFn()

			writer, closeOut, err := createWriter(out)
			if err != nil {
				return err
			}
			defer closeOut()

			encoder := json.NewEncoder(writer)
			exported := 0
			pageSize := 100
			page := offset/pageSize + 1
			skipInFirstPage := offset % pageSize
			firstPage := page
			for exported < limit {
				poems, total, err := repo.QueryPoems(database.PoemQueryFilter{
					Page:     page,
					PageSize: pageSize,
					Sort:     "id_asc",
					SearchIn: "all",
				})
				if err != nil {
					return err
				}
				if len(poems) == 0 {
					break
				}
				for i, poem := range poems {
					if page == firstPage && i < skipInFirstPage {
						continue
					}
					if exported >= limit {
						break
					}
					if err := encoder.Encode(sampleFromPoem(poem)); err != nil {
						return err
					}
					exported++
				}
				if int64(page*pageSize) >= total {
					break
				}
				page++
			}

			return printJSON(map[string]any{
				"exported": exported,
				"offset":   offset,
				"out":      out,
				"lang":     lang,
			})
		},
	}

	cmd.Flags().IntVar(&limit, "limit", 100, "Number of poems to export")
	cmd.Flags().IntVar(&offset, "offset", 0, "Number of sorted poems to skip before exporting")
	cmd.Flags().StringVar(&out, "out", "", "Output JSONL path")
	return cmd
}

func exportGoldenSampleCmd() *cobra.Command {
	var total int
	var perStratum int
	var out string
	var includeAll bool

	cmd := &cobra.Command{
		Use:   "export-golden-sample",
		Short: "Export a stratified JSONL sample for a stable golden evaluation set",
		RunE: func(cmd *cobra.Command, args []string) error {
			if total < 1 {
				return fmt.Errorf("--total must be positive")
			}
			if perStratum < 1 {
				return fmt.Errorf("--per-stratum must be positive")
			}
			if strings.TrimSpace(out) == "" {
				return fmt.Errorf("--out is required")
			}

			repo, closeFn, err := openRepo()
			if err != nil {
				return err
			}
			defer closeFn()

			writer, closeOut, err := createWriter(out)
			if err != nil {
				return err
			}
			defer closeOut()

			encoder := json.NewEncoder(writer)
			exported := 0
			counts, err := buildGoldenSamplesStream(repo, total, perStratum, includeAll, func(sample goldenSampleRecord) error {
				if err := encoder.Encode(sample); err != nil {
					return err
				}
				exported++
				return nil
			})
			if err != nil {
				return err
			}

			return printJSON(map[string]any{
				"exported":       exported,
				"target_total":   total,
				"per_stratum":    perStratum,
				"include_all":    includeAll,
				"out":            out,
				"stratum_counts": counts,
				"next_step":      "人工给这份 golden sample 标注期望标签/证据；后续所有规则和 AI 生成都先跑这份评测集，不再随机碰运气。",
			})
		},
	}

	cmd.Flags().IntVar(&total, "total", 1000, "Target number of poems to export")
	cmd.Flags().IntVar(&perStratum, "per-stratum", 80, "Max poems picked from each dynasty/type stratum per pass")
	cmd.Flags().StringVar(&out, "out", "", "Output JSONL path")
	cmd.Flags().BoolVar(&includeAll, "include-all", true, "Include low-priority unknown strata when filling the sample")
	return cmd
}

func goldenAuditCmd() *cobra.Command {
	var input string
	var out string
	var minCompleteRate float64
	var requireComplete bool

	cmd := &cobra.Command{
		Use:   "golden-audit",
		Short: "Audit golden evaluation JSONL annotation completeness",
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(input) == "" {
				return fmt.Errorf("--input is required")
			}
			if minCompleteRate < 0 || minCompleteRate > 1 {
				return fmt.Errorf("--min-complete-rate must be between 0 and 1")
			}
			records, err := readGoldenSamples(input)
			if err != nil {
				return err
			}
			report := buildGoldenAuditReport(input, records, minCompleteRate)
			if strings.TrimSpace(out) != "" && strings.TrimSpace(out) != "-" {
				writer, closeOut, err := createWriter(out)
				if err != nil {
					return err
				}
				defer closeOut()
				encoder := json.NewEncoder(writer)
				encoder.SetIndent("", "  ")
				if err := encoder.Encode(report); err != nil {
					return err
				}
			}
			if err := printJSON(report); err != nil {
				return err
			}
			if requireComplete && !report.ReadyForEvaluation {
				return fmt.Errorf("golden annotations incomplete: %s", report.RequiredAction)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&input, "input", "", "Golden sample JSONL path")
	cmd.Flags().StringVar(&out, "out", "", "Optional JSON report output path")
	cmd.Flags().Float64Var(&minCompleteRate, "min-complete-rate", 1.0, "Minimum complete annotation rate required for readiness")
	cmd.Flags().BoolVar(&requireComplete, "require-complete", false, "Return non-zero when the golden set is not ready for evaluation")
	return cmd
}

func goldenPrefillCmd() *cobra.Command {
	var input string
	var output string
	var limit int
	var mode string

	cmd := &cobra.Command{
		Use:   "golden-prefill",
		Short: "Prefill golden evaluation annotations from accepted reviewed enrichment evidence",
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(input) == "" {
				return fmt.Errorf("--input is required")
			}
			if strings.TrimSpace(output) == "" {
				return fmt.Errorf("--output is required")
			}
			if limit < 0 {
				return fmt.Errorf("--limit cannot be negative")
			}
			records, err := readGoldenSamples(input)
			if err != nil {
				return err
			}

			repo, closeFn, err := openRepo()
			if err != nil {
				return err
			}
			defer closeFn()

			result, err := buildGoldenPrefill(repo, records, mode, limit)
			if err != nil {
				return err
			}

			writer, closeOut, err := createWriter(output)
			if err != nil {
				return err
			}
			defer closeOut()
			encoder := json.NewEncoder(writer)
			for _, record := range result.Records {
				if err := encoder.Encode(record); err != nil {
					return err
				}
			}
			report := result.Report
			report.Input = input
			report.Output = output
			return printJSON(report)
		},
	}

	cmd.Flags().StringVar(&input, "input", "", "Golden sample JSONL path")
	cmd.Flags().StringVar(&output, "output", "", "Output prefilled golden sample JSONL path")
	cmd.Flags().IntVar(&limit, "limit", 0, "Maximum records to prefill; 0 means no limit")
	cmd.Flags().StringVar(&mode, "mode", "accepted-reviewed", "Prefill source mode: accepted-reviewed or accepted-any")
	return cmd
}

func goldenReviewQueueCmd() *cobra.Command {
	var input string
	var output string
	var status string
	var limit int

	cmd := &cobra.Command{
		Use:   "golden-review-queue",
		Short: "Export golden records that need human review",
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(input) == "" {
				return fmt.Errorf("--input is required")
			}
			if strings.TrimSpace(output) == "" {
				return fmt.Errorf("--output is required")
			}
			if limit < 0 {
				return fmt.Errorf("--limit cannot be negative")
			}
			records, err := readGoldenSamples(input)
			if err != nil {
				return err
			}
			selected := goldenReviewQueue(records, status, limit)
			writer, closeOut, err := createWriter(output)
			if err != nil {
				return err
			}
			defer closeOut()
			encoder := json.NewEncoder(writer)
			for _, record := range selected {
				if err := encoder.Encode(record); err != nil {
					return err
				}
			}
			return printJSON(map[string]any{
				"input":     input,
				"output":    output,
				"status":    strings.TrimSpace(status),
				"limit":     limit,
				"exported":  len(selected),
				"next_step": "edit expected_tags/evidence_lines, set annotation_status=done, then run golden-apply-review",
			})
		},
	}

	cmd.Flags().StringVar(&input, "input", "", "Golden sample JSONL path")
	cmd.Flags().StringVar(&output, "output", "", "Output review queue JSONL path")
	cmd.Flags().StringVar(&status, "status", "prefilled_review_required", "annotation_status to export; use all to export every incomplete record")
	cmd.Flags().IntVar(&limit, "limit", 0, "Maximum records to export; 0 means no limit")
	return cmd
}

func goldenReviewSheetCmd() *cobra.Command {
	var input string
	var output string
	var status string
	var limit int

	cmd := &cobra.Command{
		Use:   "golden-review-sheet",
		Short: "Export a human-editable CSV sheet for golden annotation review",
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(input) == "" {
				return fmt.Errorf("--input is required")
			}
			if strings.TrimSpace(output) == "" {
				return fmt.Errorf("--output is required")
			}
			if limit < 0 {
				return fmt.Errorf("--limit cannot be negative")
			}
			records, err := readGoldenSamples(input)
			if err != nil {
				return err
			}
			selected := goldenReviewQueue(records, status, limit)
			if err := writeGoldenReviewSheet(output, selected); err != nil {
				return err
			}
			return printJSON(map[string]any{
				"input":     input,
				"output":    output,
				"status":    strings.TrimSpace(status),
				"limit":     limit,
				"exported":  len(selected),
				"next_step": "open CSV, edit expected_tags_json/evidence_lines_json, set annotation_status=done, then run golden-apply-review-sheet",
			})
		},
	}

	cmd.Flags().StringVar(&input, "input", "", "Golden sample or review queue JSONL path")
	cmd.Flags().StringVar(&output, "output", "", "Output human-editable CSV path")
	cmd.Flags().StringVar(&status, "status", "prefilled_review_required", "annotation_status to export; use all to export every incomplete record")
	cmd.Flags().IntVar(&limit, "limit", 0, "Maximum records to export; 0 means no limit")
	return cmd
}

func goldenReviewSheetAuditCmd() *cobra.Command {
	var sheet string
	var output string
	var requireDone bool

	cmd := &cobra.Command{
		Use:   "golden-review-sheet-audit",
		Short: "Audit a reviewed golden CSV sheet before merging it",
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(sheet) == "" {
				return fmt.Errorf("--sheet is required")
			}
			records, err := readGoldenReviewSheet(sheet)
			if err != nil {
				return err
			}
			audit := buildGoldenAuditReport(sheet, records, 1.0)
			report := map[string]any{
				"sheet":           sheet,
				"total":           audit.Total,
				"complete_count":  audit.CompleteCount,
				"ready_for_merge": audit.ReadyForEvaluation,
				"issue_count":     len(audit.IssueExamples),
				"audit":           audit,
				"next_step":       "when ready_for_merge is true, run golden-apply-review-sheet and then golden-audit on the merged golden JSONL",
			}
			if strings.TrimSpace(output) != "" && strings.TrimSpace(output) != "-" {
				writer, closeOut, err := createWriter(output)
				if err != nil {
					return err
				}
				defer closeOut()
				encoder := json.NewEncoder(writer)
				encoder.SetIndent("", "  ")
				if err := encoder.Encode(report); err != nil {
					return err
				}
			}
			if err := printJSON(report); err != nil {
				return err
			}
			if requireDone && !audit.ReadyForEvaluation {
				return fmt.Errorf("golden review sheet is not ready: %s", audit.RequiredAction)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&sheet, "sheet", "", "Reviewed CSV sheet exported by golden-review-sheet")
	cmd.Flags().StringVar(&output, "out", "", "Optional JSON audit output path")
	cmd.Flags().BoolVar(&requireDone, "require-done", false, "Return non-zero unless all sheet rows are complete and evidence is valid")
	return cmd
}

func goldenApplyReviewCmd() *cobra.Command {
	var base string
	var review string
	var output string
	var reviewer string
	var requireDone bool

	cmd := &cobra.Command{
		Use:   "golden-apply-review",
		Short: "Merge reviewed golden annotations back into the golden JSONL file",
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(base) == "" {
				return fmt.Errorf("--base is required")
			}
			if strings.TrimSpace(review) == "" {
				return fmt.Errorf("--review is required")
			}
			if strings.TrimSpace(output) == "" {
				return fmt.Errorf("--output is required")
			}
			baseRecords, err := readGoldenSamples(base)
			if err != nil {
				return err
			}
			reviewRecords, err := readGoldenSamples(review)
			if err != nil {
				return err
			}
			result, err := applyGoldenReview(baseRecords, reviewRecords, reviewer, requireDone)
			if err != nil {
				return err
			}
			writer, closeOut, err := createWriter(output)
			if err != nil {
				return err
			}
			defer closeOut()
			encoder := json.NewEncoder(writer)
			for _, record := range result.Records {
				if err := encoder.Encode(record); err != nil {
					return err
				}
			}
			result.Report["base"] = base
			result.Report["review"] = review
			result.Report["output"] = output
			return printJSON(result.Report)
		},
	}

	cmd.Flags().StringVar(&base, "base", "", "Base golden JSONL path")
	cmd.Flags().StringVar(&review, "review", "", "Reviewed golden queue JSONL path")
	cmd.Flags().StringVar(&output, "output", "", "Output merged golden JSONL path")
	cmd.Flags().StringVar(&reviewer, "reviewer", "operator", "Reviewer name written to golden_meta.reviewed_by")
	cmd.Flags().BoolVar(&requireDone, "require-done", true, "Only merge records whose annotation_status is done/reviewed/accepted/complete")
	return cmd
}

func goldenApplyReviewSheetCmd() *cobra.Command {
	var base string
	var sheet string
	var output string
	var reviewer string
	var requireDone bool

	cmd := &cobra.Command{
		Use:   "golden-apply-review-sheet",
		Short: "Merge a reviewed golden CSV sheet back into the golden JSONL file",
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(base) == "" {
				return fmt.Errorf("--base is required")
			}
			if strings.TrimSpace(sheet) == "" {
				return fmt.Errorf("--sheet is required")
			}
			if strings.TrimSpace(output) == "" {
				return fmt.Errorf("--output is required")
			}
			baseRecords, err := readGoldenSamples(base)
			if err != nil {
				return err
			}
			reviewRecords, err := readGoldenReviewSheet(sheet)
			if err != nil {
				return err
			}
			result, err := applyGoldenReview(baseRecords, reviewRecords, reviewer, requireDone)
			if err != nil {
				return err
			}
			writer, closeOut, err := createWriter(output)
			if err != nil {
				return err
			}
			defer closeOut()
			encoder := json.NewEncoder(writer)
			for _, record := range result.Records {
				if err := encoder.Encode(record); err != nil {
					return err
				}
			}
			result.Report["base"] = base
			result.Report["sheet"] = sheet
			result.Report["output"] = output
			return printJSON(result.Report)
		},
	}

	cmd.Flags().StringVar(&base, "base", "", "Base golden JSONL path")
	cmd.Flags().StringVar(&sheet, "sheet", "", "Reviewed CSV sheet exported by golden-review-sheet")
	cmd.Flags().StringVar(&output, "output", "", "Output merged golden JSONL path")
	cmd.Flags().StringVar(&reviewer, "reviewer", "operator", "Reviewer name written to golden_meta.reviewed_by")
	cmd.Flags().BoolVar(&requireDone, "require-done", true, "Only merge records whose annotation_status is done/reviewed/accepted/complete")
	return cmd
}

func goldenToSampleCmd() *cobra.Command {
	var input string
	var output string
	var limit int
	var requireDone bool

	cmd := &cobra.Command{
		Use:   "golden-to-sample",
		Short: "Convert golden evaluation JSONL to regular sample JSONL for generation and quality-gate checks",
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(input) == "" {
				return fmt.Errorf("--input is required")
			}
			if strings.TrimSpace(output) == "" {
				return fmt.Errorf("--output is required")
			}
			if limit < 0 {
				return fmt.Errorf("--limit cannot be negative")
			}
			records, err := readGoldenSamples(input)
			if err != nil {
				return err
			}
			writer, closeOut, err := createWriter(output)
			if err != nil {
				return err
			}
			defer closeOut()
			encoder := json.NewEncoder(writer)
			exported := 0
			skippedNotDone := 0
			for _, record := range records {
				if limit > 0 && exported >= limit {
					break
				}
				if requireDone && !goldenRecordComplete(record) {
					skippedNotDone++
					continue
				}
				if err := encoder.Encode(sampleRecord{
					PoemID:  record.PoemID,
					Title:   record.Title,
					Content: record.Content,
					Author:  record.Author,
					Dynasty: record.Dynasty,
					Type:    record.Type,
				}); err != nil {
					return err
				}
				exported++
			}
			return printJSON(map[string]any{
				"input":            input,
				"output":           output,
				"limit":            limit,
				"exported":         exported,
				"require_done":     requireDone,
				"skipped_not_done": skippedNotDone,
			})
		},
	}

	cmd.Flags().StringVar(&input, "input", "", "Golden sample JSONL path")
	cmd.Flags().StringVar(&output, "output", "", "Output regular sample JSONL path")
	cmd.Flags().IntVar(&limit, "limit", 0, "Maximum records to export; 0 means no limit")
	cmd.Flags().BoolVar(&requireDone, "require-done", false, "Only export records with reviewed annotation status, expected tags, and evidence lines")
	return cmd
}

func generateCmd() *cobra.Command {
	var input string
	var output string
	var provider string
	var model string
	var baseURL string
	var apiKeyEnv string
	var batchSize int

	cmd := &cobra.Command{
		Use:   "generate",
		Short: "Generate enrichment candidates with rules or Qanlo OpenAI-compatible API",
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(input) == "" || strings.TrimSpace(output) == "" {
				return fmt.Errorf("--input and --output are required")
			}
			if batchSize < 1 {
				return fmt.Errorf("--batch-size must be positive")
			}

			samples, err := readSamples(input)
			if err != nil {
				return err
			}
			writer, closeOut, err := createWriter(output)
			if err != nil {
				return err
			}
			defer closeOut()

			provider = strings.ToLower(strings.TrimSpace(provider))
			if provider == "" {
				provider = "rules"
			}
			encoder := json.NewEncoder(writer)
			generated := 0
			skippedLowSignal := 0
			for _, sample := range samples {
				var candidate candidateRecord
				switch provider {
				case "rules":
					candidate = generateRulesCandidate(sample)
					if len(candidate.ProposedTags) == 0 {
						skippedLowSignal++
						continue
					}
				case "qanlo":
					candidate, err = generateQanloCandidate(sample, qanloGenerateOptions{
						Model:     model,
						BaseURL:   baseURL,
						APIKeyEnv: apiKeyEnv,
					})
					if err != nil {
						return err
					}
				default:
					return fmt.Errorf("unsupported provider %q; use rules or qanlo", provider)
				}
				if err := encoder.Encode(candidate); err != nil {
					return err
				}
				generated++
			}

			reportedModel := model
			if provider == "rules" {
				reportedModel = rulesProviderModel
			}
			return printJSON(map[string]any{
				"generated":          generated,
				"input":              input,
				"output":             output,
				"provider":           provider,
				"model":              reportedModel,
				"batch_size":         batchSize,
				"skipped_low_signal": skippedLowSignal,
			})
		},
	}

	cmd.Flags().StringVar(&input, "input", "", "Input sample JSONL path")
	cmd.Flags().StringVar(&output, "output", "", "Output candidate JSONL path")
	cmd.Flags().StringVar(&provider, "provider", "rules", "Generation provider: rules or qanlo")
	cmd.Flags().StringVar(&model, "model", "deepseek-v4-flash", "Qanlo model name")
	cmd.Flags().StringVar(&baseURL, "base-url", getenvDefault("QANLO_OPENAI_BASE_URL", "https://qanlo.com/v1"), "Qanlo OpenAI-compatible base URL")
	cmd.Flags().StringVar(&apiKeyEnv, "api-key-env", "QANLO_AGENT_KEY", "Environment variable containing Qanlo Agent Key")
	cmd.Flags().IntVar(&batchSize, "batch-size", 20, "Logical batch size for operator logs; Qanlo calls are sent one poem at a time")
	return cmd
}

func validateCmd() *cobra.Command {
	var input string
	var out string
	var skipDBCheck bool
	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate enrichment candidate JSONL before import",
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(input) == "" {
				return fmt.Errorf("--input is required")
			}

			candidates, err := readCandidates(input)
			if err != nil {
				return err
			}
			errs := validateCandidates(candidates)
			if !skipDBCheck {
				repo, closeFn, err := openRepo()
				if err != nil {
					return err
				}
				defer closeFn()
				errs = append(errs, validateCandidatePoemIDs(repo, candidates)...)
			}
			report := map[string]any{
				"input":          input,
				"total":          len(candidates),
				"error_count":    len(errs),
				"valid":          len(candidates) > 0 && len(errs) == 0,
				"max_examples":   20,
				"error_examples": firstStrings(errs, 20),
				"db_check":       !skipDBCheck,
			}
			if strings.TrimSpace(out) != "" && strings.TrimSpace(out) != "-" {
				writer, closeOut, err := createWriter(out)
				if err != nil {
					return err
				}
				defer closeOut()
				encoder := json.NewEncoder(writer)
				encoder.SetIndent("", "  ")
				if err := encoder.Encode(report); err != nil {
					return err
				}
			}
			if err := printJSON(report); err != nil {
				return err
			}
			if len(candidates) == 0 {
				return fmt.Errorf("candidate validation failed: no candidates")
			}
			if len(errs) > 0 {
				return fmt.Errorf("candidate validation failed")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&input, "input", "", "Input candidate JSONL path")
	cmd.Flags().StringVar(&out, "out", "", "Optional JSON report output path")
	cmd.Flags().BoolVar(&skipDBCheck, "skip-db-check", false, "Skip checking that poem_id exists in the configured database")
	return cmd
}

func qualityGateCmd() *cobra.Command {
	var input string
	var samplePath string
	var output string
	var minConfidence float64
	var maxErrors int

	cmd := &cobra.Command{
		Use:   "quality-gate",
		Short: "Run conservative automatic checks before importing AI/rules candidates",
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(input) == "" {
				return fmt.Errorf("--input is required")
			}
			if minConfidence < 0 || minConfidence > 1 {
				return fmt.Errorf("--min-confidence must be between 0 and 1")
			}
			if maxErrors < 0 {
				return fmt.Errorf("--max-errors cannot be negative")
			}

			candidates, err := readCandidates(input)
			if err != nil {
				return err
			}
			samplesByID := map[int64]sampleRecord{}
			if strings.TrimSpace(samplePath) != "" {
				samples, err := readSamples(samplePath)
				if err != nil {
					return err
				}
				for _, sample := range samples {
					samplesByID[sample.PoemID] = sample
				}
			}

			report := buildQualityGateReport(input, samplePath, candidates, samplesByID, minConfidence)
			if strings.TrimSpace(output) != "" && strings.TrimSpace(output) != "-" {
				writer, closeOut, err := createWriter(output)
				if err != nil {
					return err
				}
				defer closeOut()
				encoder := json.NewEncoder(writer)
				encoder.SetIndent("", "  ")
				if err := encoder.Encode(report); err != nil {
					return err
				}
			}
			if err := printJSON(report); err != nil {
				return err
			}
			if report.ErrorCount > maxErrors {
				return fmt.Errorf("quality gate failed: error_count=%d > max_errors=%d", report.ErrorCount, maxErrors)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&input, "input", "", "Input candidate JSONL path")
	cmd.Flags().StringVar(&samplePath, "sample", "", "Optional source sample JSONL path for evidence checks")
	cmd.Flags().StringVar(&output, "out", "", "Optional JSON report output path")
	cmd.Flags().Float64Var(&minConfidence, "min-confidence", 0.7, "Minimum confidence required when candidate meta.confidence is present")
	cmd.Flags().IntVar(&maxErrors, "max-errors", 0, "Maximum allowed errors before failing")
	return cmd
}

func importCandidatesCmd() *cobra.Command {
	var input string
	var runID string
	var scope string

	cmd := &cobra.Command{
		Use:   "import-candidates",
		Short: "Import validated candidates into pending manual-review queue",
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(input) == "" {
				return fmt.Errorf("--input is required")
			}
			if strings.TrimSpace(runID) == "" {
				runID = "enrich-" + time.Now().UTC().Format("20060102-150405")
			}
			if strings.TrimSpace(scope) == "" {
				scope = runID
			}

			candidates, err := readCandidates(input)
			if err != nil {
				return err
			}
			if errs := validateCandidates(candidates); len(errs) > 0 {
				return fmt.Errorf("candidate validation failed: %s", strings.Join(firstStrings(errs, 5), "; "))
			}
			if len(candidates) == 0 {
				return fmt.Errorf("candidate validation failed: no candidates")
			}

			repo, closeFn, err := openRepo()
			if err != nil {
				return err
			}
			defer closeFn()
			if errs := validateCandidatePoemIDs(repo, candidates); len(errs) > 0 {
				return fmt.Errorf("candidate validation failed: %s", strings.Join(firstStrings(errs, 5), "; "))
			}

			job, err := repo.CreateEnrichmentJob(database.CreateEnrichmentJobParams{
				Scope:      scope,
				TotalCount: len(candidates),
				Config: map[string]any{
					"run_id": runID,
					"input":  input,
				},
			})
			if err != nil {
				return err
			}

			imported := 0
			for _, candidate := range candidates {
				if _, err := repo.CreateEnrichmentReviewItem(database.CreateReviewItemParams{
					JobID:             &job.ID,
					PoemID:            candidate.PoemID,
					ProposedTags:      candidate.ProposedTags,
					ProposedKnowledge: candidate.ProposedKnowledge,
				}); err != nil {
					return fmt.Errorf("failed to import poem_id=%d: %w", candidate.PoemID, err)
				}
				imported++
			}

			return printJSON(map[string]any{
				"run_id":       runID,
				"job_id":       job.ID,
				"imported":     imported,
				"status":       database.EnrichmentStatusPending,
				"review_queue": "/api/v1/admin/enrichment/review-items?status=pending",
			})
		},
	}

	cmd.Flags().StringVar(&input, "input", "", "Input candidate JSONL path")
	cmd.Flags().StringVar(&runID, "run-id", "", "Batch run id, e.g. enrich-20260629-sample100")
	cmd.Flags().StringVar(&scope, "scope", "", "Job scope; defaults to run-id")
	return cmd
}

func sampleReviewCmd() *cobra.Command {
	var runID string
	var status string
	var limit int
	var out string

	cmd := &cobra.Command{
		Use:   "sample-review",
		Short: "Export pending review items as JSONL for manual sampling",
		RunE: func(cmd *cobra.Command, args []string) error {
			runID = strings.TrimSpace(runID)
			if runID == "" {
				return fmt.Errorf("--run-id is required")
			}
			if limit < 1 {
				return fmt.Errorf("--limit must be positive")
			}
			if strings.TrimSpace(out) == "" {
				out = filepath.Join("data", "enrichment", "manual-sample-"+safeFileName(runID)+".jsonl")
			}

			repo, closeFn, err := openRepo()
			if err != nil {
				return err
			}
			defer closeFn()

			items, err := repo.ListEnrichmentReviewItemsForRun(runID, status, limit)
			if err != nil {
				return err
			}

			writer, closeOut, err := createWriter(out)
			if err != nil {
				return err
			}
			defer closeOut()

			encoder := json.NewEncoder(writer)
			exported := 0
			for _, item := range items {
				record, err := manualReviewRecordFromItem(repo, runID, item)
				if err != nil {
					return err
				}
				if err := encoder.Encode(record); err != nil {
					return err
				}
				exported++
			}

			return printJSON(map[string]any{
				"run_id":           runID,
				"status":           defaultStatus(status),
				"requested_limit":  limit,
				"exported":         exported,
				"out":              out,
				"next_step":        "人工抽检 JSONL；确认后用后台 accept/reject/correct 处理，并重新执行 review-report",
				"review_endpoint":  "/api/v1/admin/enrichment/review-items",
				"summary_endpoint": "/api/v1/admin/enrichment/runs/" + runID + "/summary",
			})
		},
	}

	cmd.Flags().StringVar(&runID, "run-id", "", "Batch run id, e.g. enrich-20260630-rules100")
	cmd.Flags().StringVar(&status, "status", database.EnrichmentStatusPending, "Review item status to export")
	cmd.Flags().IntVar(&limit, "limit", 30, "Number of review items to export; max 1000")
	cmd.Flags().StringVar(&out, "out", "", "Output JSONL path; defaults to data/enrichment/manual-sample-<run-id>.jsonl")
	return cmd
}

func applyReviewCmd() *cobra.Command {
	var input string
	var reviewer string
	var apply bool

	cmd := &cobra.Command{
		Use:   "apply-review",
		Short: "Validate or apply edited manual-review JSONL decisions",
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(input) == "" {
				return fmt.Errorf("--input is required")
			}
			reviewer = strings.TrimSpace(reviewer)
			if reviewer == "" {
				reviewer = "operator"
			}

			inputs, err := readManualReviewInputs(input)
			if err != nil {
				return err
			}

			repo, closeFn, err := openRepo()
			if err != nil {
				return err
			}
			defer closeFn()

			result := reviewApplyResult{
				Input:    input,
				Mode:     "dry_run",
				Reviewer: reviewer,
				Total:    len(inputs),
			}
			if apply {
				result.Mode = "apply"
			}

			plans := make([]reviewApplyPlan, 0, len(inputs))
			for _, inputRecord := range inputs {
				plan, skipped, err := buildReviewApplyPlan(repo, inputRecord, reviewer)
				if err != nil {
					result.Errors = append(result.Errors, err.Error())
					continue
				}
				if skipped {
					result.Skipped++
					continue
				}
				plans = append(plans, plan)
				result.addPlannedAction(plan.Action)
			}

			if len(result.Errors) > 0 {
				_ = printJSON(result)
				return fmt.Errorf("review input validation failed")
			}
			if !apply {
				result.NextStep = "确认无误后追加 --apply 写回数据库；写回后执行 review-report"
				return printJSON(result)
			}

			for _, plan := range plans {
				if err := executeReviewApplyPlan(repo, plan); err != nil {
					return err
				}
			}
			result.NextStep = "执行 review-report 查看通过率和退回原因；必要时用 rollback 回滚"
			return printJSON(result)
		},
	}

	cmd.Flags().StringVar(&input, "input", "", "Edited manual-review JSONL exported by sample-review")
	cmd.Flags().StringVar(&reviewer, "reviewer", "operator", "Reviewer/operator name written to review records")
	cmd.Flags().BoolVar(&apply, "apply", false, "Actually write decisions to database; default is dry-run validation")
	return cmd
}

func rollbackCmd() *cobra.Command {
	var runID string
	var poemID int64
	var reviewer string
	var notes string

	cmd := &cobra.Command{
		Use:   "rollback",
		Short: "Rollback accepted enrichment data by run id or poem id",
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(runID) == "" && poemID < 1 {
				return fmt.Errorf("--run-id or --poem-id is required")
			}
			repo, closeFn, err := openRepo()
			if err != nil {
				return err
			}
			defer closeFn()

			params := database.ReviewDecisionParams{Reviewer: reviewer, Notes: notes}
			var result *database.EnrichmentRollbackResult
			if strings.TrimSpace(runID) != "" {
				result, err = repo.RollbackEnrichmentJob(runID, params)
			} else {
				result, err = repo.RollbackPoemEnrichment(poemID, params)
			}
			if err != nil {
				return err
			}
			return printJSON(result)
		},
	}

	cmd.Flags().StringVar(&runID, "run-id", "", "Rollback by enrichment run id")
	cmd.Flags().Int64Var(&poemID, "poem-id", 0, "Rollback one poem's accepted enrichment")
	cmd.Flags().StringVar(&reviewer, "reviewer", "operator", "Reviewer/operator name")
	cmd.Flags().StringVar(&notes, "notes", "rollback", "Rollback reason")
	return cmd
}

func reviewAuditCmd() *cobra.Command {
	var input string
	var runID string
	var out string

	cmd := &cobra.Command{
		Use:   "review-audit",
		Short: "Summarize edited manual-review JSONL without writing to database",
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(input) == "" {
				return fmt.Errorf("--input is required")
			}
			records, err := readManualReviewInputs(input)
			if err != nil {
				return err
			}
			report := buildManualReviewAudit(input, runID, records)
			if strings.TrimSpace(out) != "" && strings.TrimSpace(out) != "-" {
				writer, closeOut, err := createWriter(out)
				if err != nil {
					return err
				}
				defer closeOut()
				encoder := json.NewEncoder(writer)
				encoder.SetIndent("", "  ")
				if err := encoder.Encode(report); err != nil {
					return err
				}
			}
			return printJSON(report)
		},
	}

	cmd.Flags().StringVar(&input, "input", "", "Edited manual-review JSONL path")
	cmd.Flags().StringVar(&runID, "run-id", "", "Optional run id filter")
	cmd.Flags().StringVar(&out, "out", "", "Optional JSON report output path")
	return cmd
}

func reviewReportCmd() *cobra.Command {
	var runID string
	var out string

	cmd := &cobra.Command{
		Use:   "review-report",
		Short: "Export review progress, pass rate, and rejection reasons for one enrichment run",
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(runID) == "" {
				return fmt.Errorf("--run-id is required")
			}
			repo, closeFn, err := openRepo()
			if err != nil {
				return err
			}
			defer closeFn()

			summary, err := repo.GetEnrichmentRunSummary(runID)
			if err != nil {
				return err
			}

			if strings.TrimSpace(out) == "" || strings.TrimSpace(out) == "-" {
				return printJSON(summary)
			}
			writer, closeOut, err := createWriter(out)
			if err != nil {
				return err
			}
			defer closeOut()
			encoder := json.NewEncoder(writer)
			encoder.SetIndent("", "  ")
			if err := encoder.Encode(summary); err != nil {
				return err
			}
			return printJSON(map[string]any{
				"run_id": runID,
				"out":    out,
			})
		},
	}

	cmd.Flags().StringVar(&runID, "run-id", "", "Batch run id, e.g. enrich-20260629-sample100")
	cmd.Flags().StringVar(&out, "out", "", "Optional JSON output path; defaults to stdout")
	return cmd
}

func openRepo() (*database.Repository, func(), error) {
	db, err := database.Open(dbPath, 1, 1)
	if err != nil {
		return nil, nil, err
	}
	if err := db.Migrate(); err != nil {
		_ = db.Close()
		return nil, nil, err
	}
	return database.NewRepositoryWithLang(db, database.ParseLang(lang)), func() { _ = db.Close() }, nil
}

func createWriter(path string) (io.Writer, func(), error) {
	if strings.TrimSpace(path) == "-" {
		return os.Stdout, func() {}, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, nil, err
	}
	file, err := os.Create(path)
	if err != nil {
		return nil, nil, err
	}
	return file, func() { _ = file.Close() }, nil
}

func sampleFromPoem(poem database.Poem) sampleRecord {
	record := sampleRecord{
		PoemID:  poem.ID,
		Title:   poem.Title,
		Content: contentLines(poem.Content),
	}
	if poem.Author != nil {
		record.Author = poem.Author.Name
	}
	if poem.Dynasty != nil {
		record.Dynasty = poem.Dynasty.Name
	}
	if poem.Type != nil {
		record.Type = poem.Type.Name
	}
	return record
}

func goldenSampleFromPoem(poem database.Poem, stratum string) goldenSampleRecord {
	sample := sampleFromPoem(poem)
	return goldenSampleRecord{
		PoemID:  sample.PoemID,
		Title:   sample.Title,
		Content: sample.Content,
		Author:  sample.Author,
		Dynasty: sample.Dynasty,
		Type:    sample.Type,
		GoldenMeta: map[string]any{
			"stratum":              stratum,
			"expected_tags":        []database.TagInput{},
			"evidence_lines":       []string{},
			"review_notes":         "",
			"annotation_status":    "todo",
			"annotation_checklist": []string{"标签必须有原文证据", "不能靠标题或典故过度脑补", "元曲对白/戏文先按上下文判断", "不确定就标 low_confidence"},
		},
	}
}

type manualReviewRecord struct {
	ReviewItemID      int64                           `json:"review_item_id"`
	JobID             *int64                          `json:"job_id,omitempty"`
	RunID             string                          `json:"run_id"`
	PoemID            int64                           `json:"poem_id"`
	Title             string                          `json:"title"`
	Author            string                          `json:"author,omitempty"`
	Dynasty           string                          `json:"dynasty,omitempty"`
	Type              string                          `json:"type,omitempty"`
	Content           []string                        `json:"content"`
	ProposedTags      []database.TagInput             `json:"proposed_tags"`
	ProposedKnowledge database.ProposedKnowledgeInput `json:"proposed_knowledge"`
	Checklist         []string                        `json:"checklist"`
	SuggestedAction   string                          `json:"suggested_action"`
	ReviewDecision    manualReviewDecision            `json:"review_decision"`
}

type manualReviewDecision struct {
	Action string `json:"action"`
	Notes  string `json:"notes"`
}

type manualReviewInputRecord struct {
	ReviewItemID      int64                           `json:"review_item_id"`
	RunID             string                          `json:"run_id"`
	ProposedTags      []database.TagInput             `json:"proposed_tags"`
	ProposedKnowledge database.ProposedKnowledgeInput `json:"proposed_knowledge"`
	ReviewDecision    manualReviewDecision            `json:"review_decision"`
}

type reviewApplyPlan struct {
	ReviewItemID      int64
	RunID             string
	Action            string
	Reviewer          string
	Notes             string
	ProposedTags      []database.TagInput
	ProposedKnowledge database.ProposedKnowledgeInput
}

type reviewApplyResult struct {
	Input          string   `json:"input"`
	Mode           string   `json:"mode"`
	Reviewer       string   `json:"reviewer"`
	Total          int      `json:"total"`
	PlannedAccept  int      `json:"planned_accept"`
	PlannedReject  int      `json:"planned_reject"`
	PlannedCorrect int      `json:"planned_correct"`
	Skipped        int      `json:"skipped"`
	Errors         []string `json:"errors,omitempty"`
	NextStep       string   `json:"next_step"`
}

type manualReviewAuditReport struct {
	Input               string                               `json:"input"`
	RunID               string                               `json:"run_id,omitempty"`
	Total               int                                  `json:"total"`
	ReviewedCount       int                                  `json:"reviewed_count"`
	PendingCount        int                                  `json:"pending_count"`
	AcceptCount         int                                  `json:"accept_count"`
	CorrectCount        int                                  `json:"correct_count"`
	RejectCount         int                                  `json:"reject_count"`
	PublishableCount    int                                  `json:"publishable_count"`
	PassRate            float64                              `json:"pass_rate"`
	PassRatePercent     string                               `json:"pass_rate_percent"`
	RejectedNoteTop10   []database.EnrichmentReviewNoteCount `json:"rejected_note_top10"`
	UnsupportedActions  []string                             `json:"unsupported_actions,omitempty"`
	RecommendedNextStep string                               `json:"recommended_next_step"`
}

type qualityGateReport struct {
	Input              string              `json:"input"`
	Sample             string              `json:"sample,omitempty"`
	Total              int                 `json:"total"`
	ValidFormat        bool                `json:"valid_format"`
	ErrorCount         int                 `json:"error_count"`
	WarningCount       int                 `json:"warning_count"`
	PassedCount        int                 `json:"passed_count"`
	NeedsReviewCount   int                 `json:"needs_review_count"`
	LowConfidenceCount int                 `json:"low_confidence_count"`
	MissingSampleCount int                 `json:"missing_sample_count"`
	IssueTop10         []qualityIssueCount `json:"issue_top10"`
	IssueExamples      []qualityIssue      `json:"issue_examples,omitempty"`
	NextStep           string              `json:"next_step"`
}

type qualityIssue struct {
	PoemID   int64  `json:"poem_id,omitempty"`
	Severity string `json:"severity"`
	Reason   string `json:"reason"`
	Evidence string `json:"evidence,omitempty"`
}

type qualityIssueCount struct {
	Reason string `json:"reason"`
	Count  int    `json:"count"`
}

type goldenStratumCount struct {
	Stratum string `json:"stratum"`
	Count   int    `json:"count"`
}

type goldenAuditReport struct {
	Input                    string               `json:"input"`
	Total                    int                  `json:"total"`
	UniquePoemIDs            int                  `json:"unique_poem_ids"`
	DuplicatePoemIDs         []int64              `json:"duplicate_poem_ids,omitempty"`
	MissingContentCount      int                  `json:"missing_content_count"`
	MissingMetaCount         int                  `json:"missing_meta_count"`
	ExpectedTagsFilledCount  int                  `json:"expected_tags_filled_count"`
	EvidenceLinesFilledCount int                  `json:"evidence_lines_filled_count"`
	ReviewedStatusCount      int                  `json:"reviewed_status_count"`
	CompleteCount            int                  `json:"complete_count"`
	CompleteRate             float64              `json:"complete_rate"`
	CompleteRatePercent      string               `json:"complete_rate_percent"`
	InvalidEvidenceCount     int                  `json:"invalid_evidence_count"`
	MinCompleteRate          float64              `json:"min_complete_rate"`
	ReadyForEvaluation       bool                 `json:"ready_for_evaluation"`
	StatusCounts             map[string]int       `json:"status_counts"`
	StratumCounts            []goldenStratumCount `json:"stratum_counts"`
	IssueTop10               []qualityIssueCount  `json:"issue_top10"`
	IssueExamples            []goldenAuditIssue   `json:"issue_examples,omitempty"`
	RequiredAction           string               `json:"required_action"`
}

type goldenAuditIssue struct {
	PoemID   int64  `json:"poem_id,omitempty"`
	Reason   string `json:"reason"`
	Evidence string `json:"evidence,omitempty"`
}

type goldenPrefillResult struct {
	Records []goldenSampleRecord
	Report  goldenPrefillReport
}

type goldenPrefillReport struct {
	Input                  string `json:"input,omitempty"`
	Output                 string `json:"output,omitempty"`
	Mode                   string `json:"mode"`
	Total                  int    `json:"total"`
	Updated                int    `json:"updated"`
	SkippedNoAcceptedData  int    `json:"skipped_no_accepted_data"`
	SkippedNoTags          int    `json:"skipped_no_tags"`
	SkippedNoEvidence      int    `json:"skipped_no_evidence"`
	SkippedAlreadyComplete int    `json:"skipped_already_complete"`
	Limit                  int    `json:"limit"`
	ReviewRequired         bool   `json:"review_required"`
	NextStep               string `json:"next_step"`
}

type goldenApplyReviewResult struct {
	Records []goldenSampleRecord
	Report  map[string]any
}

func (r *reviewApplyResult) addPlannedAction(action string) {
	switch action {
	case "accept":
		r.PlannedAccept++
	case "reject":
		r.PlannedReject++
	case "correct":
		r.PlannedCorrect++
	}
}

func manualReviewRecordFromItem(repo *database.Repository, runID string, item database.EnrichmentReviewItem) (manualReviewRecord, error) {
	poem, err := repo.GetPoemByID(strconv.FormatInt(item.PoemID, 10))
	if err != nil {
		return manualReviewRecord{}, fmt.Errorf("failed to load poem_id=%d: %w", item.PoemID, err)
	}

	var tags []database.TagInput
	if strings.TrimSpace(item.ProposedTagsJSON) != "" {
		if err := json.Unmarshal([]byte(item.ProposedTagsJSON), &tags); err != nil {
			return manualReviewRecord{}, fmt.Errorf("review_item_id=%d proposed_tags_json: %w", item.ID, err)
		}
	}
	var knowledge database.ProposedKnowledgeInput
	if strings.TrimSpace(item.ProposedKnowledgeJSON) != "" {
		if err := json.Unmarshal([]byte(item.ProposedKnowledgeJSON), &knowledge); err != nil {
			return manualReviewRecord{}, fmt.Errorf("review_item_id=%d proposed_knowledge_json: %w", item.ID, err)
		}
	}

	record := manualReviewRecord{
		ReviewItemID:      item.ID,
		JobID:             item.JobID,
		RunID:             runID,
		PoemID:            item.PoemID,
		Title:             poem.Title,
		Content:           contentLines(poem.Content),
		ProposedTags:      tags,
		ProposedKnowledge: knowledge,
		Checklist: []string{
			"标签是否贴合原文，不是泛词或错词",
			"summary 是否 20-220 字，且没有编造作者经历、历史背景或人物关系",
			"translation、annotation、recommendation 是否可直接用于知识库召回",
			"不确定时先 reject 或人工 correct 后再 accept",
		},
		SuggestedAction: "pending_manual_review",
		ReviewDecision: manualReviewDecision{
			Action: "pending",
			Notes:  "",
		},
	}
	if poem.Author != nil {
		record.Author = poem.Author.Name
	}
	if poem.Dynasty != nil {
		record.Dynasty = poem.Dynasty.Name
	}
	if poem.Type != nil {
		record.Type = poem.Type.Name
	}
	return record, nil
}

func readManualReviewInputs(path string) ([]manualReviewInputRecord, error) {
	var records []manualReviewInputRecord
	if err := readJSONL(path, func(line []byte, lineNo int) error {
		var record manualReviewInputRecord
		if err := json.Unmarshal(line, &record); err != nil {
			return fmt.Errorf("line %d: %w", lineNo, err)
		}
		records = append(records, record)
		return nil
	}); err != nil {
		return nil, err
	}
	return records, nil
}

func buildReviewApplyPlan(repo *database.Repository, record manualReviewInputRecord, reviewer string) (reviewApplyPlan, bool, error) {
	if record.ReviewItemID < 1 {
		return reviewApplyPlan{}, false, fmt.Errorf("review_item_id is required")
	}
	action := normalizeReviewAction(record.ReviewDecision.Action)
	if action == "" || action == "pending" {
		return reviewApplyPlan{}, true, nil
	}
	if action != "accept" && action != "reject" && action != "correct" {
		return reviewApplyPlan{}, false, fmt.Errorf("review_item_id=%d has unsupported action %q", record.ReviewItemID, record.ReviewDecision.Action)
	}

	item, err := repo.GetEnrichmentReviewItem(record.ReviewItemID)
	if err != nil {
		return reviewApplyPlan{}, false, fmt.Errorf("review_item_id=%d not found: %w", record.ReviewItemID, err)
	}
	if item.Status != database.EnrichmentStatusPending {
		return reviewApplyPlan{}, false, fmt.Errorf("review_item_id=%d is %q, only pending items can be reviewed", record.ReviewItemID, item.Status)
	}

	notes := strings.TrimSpace(record.ReviewDecision.Notes)
	if notes == "" {
		return reviewApplyPlan{}, false, fmt.Errorf("review_item_id=%d review_decision.notes is required", record.ReviewItemID)
	}
	tags := record.ProposedTags
	knowledge := record.ProposedKnowledge
	if action == "accept" || action == "correct" {
		if len(tags) == 0 && isEmptyKnowledge(knowledge) {
			tags, knowledge, err = candidateFromReviewItem(item)
			if err != nil {
				return reviewApplyPlan{}, false, fmt.Errorf("review_item_id=%d current candidate invalid: %w", record.ReviewItemID, err)
			}
		}
		candidate := candidateRecord{
			PoemID:            item.PoemID,
			ProposedTags:      tags,
			ProposedKnowledge: knowledge,
		}
		if errs := validateCandidates([]candidateRecord{candidate}); len(errs) > 0 {
			return reviewApplyPlan{}, false, fmt.Errorf("review_item_id=%d candidate invalid: %s", record.ReviewItemID, strings.Join(firstStrings(errs, 5), "; "))
		}
	}

	plan := reviewApplyPlan{
		ReviewItemID:      record.ReviewItemID,
		RunID:             strings.TrimSpace(record.RunID),
		Action:            action,
		Reviewer:          reviewer,
		Notes:             notes,
		ProposedTags:      tags,
		ProposedKnowledge: knowledge,
	}
	return plan, false, nil
}

func executeReviewApplyPlan(repo *database.Repository, plan reviewApplyPlan) error {
	params := database.ReviewDecisionParams{Reviewer: plan.Reviewer, Notes: plan.Notes}
	switch plan.Action {
	case "accept":
		if _, err := repo.CorrectEnrichmentReviewItem(plan.ReviewItemID, plan.ProposedTags, plan.ProposedKnowledge, params); err != nil {
			return err
		}
		_, err := repo.AcceptEnrichmentReviewItem(plan.ReviewItemID, params)
		return err
	case "reject":
		_, err := repo.RejectEnrichmentReviewItem(plan.ReviewItemID, params)
		return err
	case "correct":
		if _, err := repo.CorrectEnrichmentReviewItem(plan.ReviewItemID, plan.ProposedTags, plan.ProposedKnowledge, params); err != nil {
			return err
		}
		_, err := repo.AcceptEnrichmentReviewItem(plan.ReviewItemID, params)
		return err
	default:
		return fmt.Errorf("unsupported action %q", plan.Action)
	}
}

func normalizeReviewAction(action string) string {
	action = strings.ToLower(strings.TrimSpace(action))
	switch action {
	case "", "pending_manual_review", "manual_review", "todo":
		return "pending"
	case "accept|reject|correct":
		return "pending"
	case "pass", "approve", "approved":
		return "accept"
	case "fail", "decline", "denied":
		return "reject"
	default:
		return action
	}
}

func candidateFromReviewItem(item *database.EnrichmentReviewItem) ([]database.TagInput, database.ProposedKnowledgeInput, error) {
	var tags []database.TagInput
	if strings.TrimSpace(item.ProposedTagsJSON) != "" {
		if err := json.Unmarshal([]byte(item.ProposedTagsJSON), &tags); err != nil {
			return nil, database.ProposedKnowledgeInput{}, err
		}
	}
	var knowledge database.ProposedKnowledgeInput
	if strings.TrimSpace(item.ProposedKnowledgeJSON) != "" {
		if err := json.Unmarshal([]byte(item.ProposedKnowledgeJSON), &knowledge); err != nil {
			return nil, database.ProposedKnowledgeInput{}, err
		}
	}
	return tags, knowledge, nil
}

func isEmptyKnowledge(knowledge database.ProposedKnowledgeInput) bool {
	return strings.TrimSpace(knowledge.Summary) == "" &&
		strings.TrimSpace(knowledge.Translation) == "" &&
		strings.TrimSpace(knowledge.Annotation) == "" &&
		strings.TrimSpace(knowledge.Recommendation) == "" &&
		strings.TrimSpace(knowledge.Source) == ""
}

func contentLines(raw []byte) []string {
	var lines []string
	if err := json.Unmarshal(raw, &lines); err == nil && len(lines) > 0 {
		return lines
	}
	text := strings.TrimSpace(string(raw))
	if text == "" {
		return []string{}
	}
	return []string{text}
}

func readSamples(path string) ([]sampleRecord, error) {
	var samples []sampleRecord
	if err := readJSONL(path, func(line []byte, lineNo int) error {
		var sample sampleRecord
		if err := json.Unmarshal(line, &sample); err != nil {
			return fmt.Errorf("line %d: %w", lineNo, err)
		}
		samples = append(samples, sample)
		return nil
	}); err != nil {
		return nil, err
	}
	return samples, nil
}

func readGoldenSamples(path string) ([]goldenSampleRecord, error) {
	var samples []goldenSampleRecord
	if err := readJSONL(path, func(line []byte, lineNo int) error {
		var sample goldenSampleRecord
		if err := json.Unmarshal(line, &sample); err != nil {
			return fmt.Errorf("line %d: %w", lineNo, err)
		}
		samples = append(samples, sample)
		return nil
	}); err != nil {
		return nil, err
	}
	return samples, nil
}

func readCandidates(path string) ([]candidateRecord, error) {
	var candidates []candidateRecord
	if err := readJSONL(path, func(line []byte, lineNo int) error {
		var candidate candidateRecord
		if err := json.Unmarshal(line, &candidate); err != nil {
			return fmt.Errorf("line %d: %w", lineNo, err)
		}
		candidates = append(candidates, candidate)
		return nil
	}); err != nil {
		return nil, err
	}
	return candidates, nil
}

func readJSONL(path string, handle func([]byte, int) error) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		if err := handle(line, lineNo); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func buildGoldenSamples(repo *database.Repository, total, perStratum int, includeAll bool) ([]goldenSampleRecord, []goldenStratumCount, error) {
	samples := make([]goldenSampleRecord, 0, total)
	counts, err := buildGoldenSamplesStream(repo, total, perStratum, includeAll, func(sample goldenSampleRecord) error {
		samples = append(samples, sample)
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return samples, counts, nil
}

func buildGoldenSamplesStream(repo *database.Repository, total, perStratum int, includeAll bool, emit func(goldenSampleRecord) error) ([]goldenStratumCount, error) {
	counts := map[string]int{}
	exported := 0

	query := fmt.Sprintf(`
SELECT
	p.id,
	p.title,
	p.content,
	COALESCE(a.name, '') AS author,
	COALESCE(d.name, '') AS dynasty,
	COALESCE(t.name, '') AS type_name,
	COALESCE(t.category, '') AS type_category
FROM %s p
LEFT JOIN %s a ON a.id = p.author_id
LEFT JOIN %s d ON d.id = p.dynasty_id
LEFT JOIN %s t ON t.id = p.type_id
ORDER BY p.id ASC`, repo.PoemsTable(), repo.AuthorsTable(), repo.DynastiesTable(), repo.PoetryTypesTable())

	rows, err := repo.DB().Raw(query).Rows()
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() && exported < total {
		var poemID int64
		var title string
		var rawContent string
		var author sql.NullString
		var dynasty sql.NullString
		var typeName sql.NullString
		var typeCategory sql.NullString
		if err := rows.Scan(&poemID, &title, &rawContent, &author, &dynasty, &typeName, &typeCategory); err != nil {
			return nil, err
		}
		stratum := goldenStratumFromFields(dynasty.String, typeCategory.String, typeName.String)
		if !includeAll && lowPriorityGoldenStratum(stratum) {
			continue
		}
		if counts[stratum] >= perStratum {
			continue
		}
		sample := goldenSampleRecord{
			PoemID:  poemID,
			Title:   title,
			Content: contentLines([]byte(rawContent)),
			Author:  author.String,
			Dynasty: dynasty.String,
			Type:    typeName.String,
			GoldenMeta: map[string]any{
				"stratum":              stratum,
				"expected_tags":        []database.TagInput{},
				"evidence_lines":       []string{},
				"review_notes":         "",
				"annotation_status":    "todo",
				"annotation_checklist": []string{"标签必须有原文证据", "不能靠标题或典故过度脑补", "元曲对白/戏文先按上下文判断", "不确定就标 low_confidence"},
			},
		}
		if err := emit(sample); err != nil {
			return nil, err
		}
		counts[stratum]++
		exported++
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if exported < total {
		return nil, fmt.Errorf("only exported %d golden samples; increase --per-stratum or allow more strata", exported)
	}

	stratumCounts := make([]goldenStratumCount, 0, len(counts))
	for stratum, count := range counts {
		stratumCounts = append(stratumCounts, goldenStratumCount{Stratum: stratum, Count: count})
	}
	sort.Slice(stratumCounts, func(i, j int) bool {
		return stratumCounts[i].Stratum < stratumCounts[j].Stratum
	})
	return stratumCounts, nil
}

func goldenStratumFromFields(dynasty, category, typeName string) string {
	dynasty = strings.TrimSpace(dynasty)
	if dynasty == "" {
		dynasty = "unknown_dynasty"
	}
	poemType := strings.TrimSpace(typeName)
	if poemType == "" {
		poemType = "unknown_type"
	}
	if category = strings.TrimSpace(category); category != "" {
		poemType = category + "/" + poemType
	}
	return dynasty + " / " + poemType
}

func goldenStratum(poem database.Poem) string {
	dynasty := "unknown_dynasty"
	if poem.Dynasty != nil && strings.TrimSpace(poem.Dynasty.Name) != "" {
		dynasty = strings.TrimSpace(poem.Dynasty.Name)
	}
	poemType := "unknown_type"
	if poem.Type != nil && strings.TrimSpace(poem.Type.Name) != "" {
		poemType = strings.TrimSpace(poem.Type.Name)
		if strings.TrimSpace(poem.Type.Category) != "" {
			poemType = strings.TrimSpace(poem.Type.Category) + "/" + poemType
		}
	}
	return dynasty + " / " + poemType
}

func lowPriorityGoldenStratum(stratum string) bool {
	lowered := strings.ToLower(stratum)
	return strings.Contains(lowered, "unknown") || strings.Contains(stratum, "未知")
}

func buildManualReviewAudit(input, runID string, records []manualReviewInputRecord) manualReviewAuditReport {
	report := manualReviewAuditReport{
		Input:             input,
		RunID:             strings.TrimSpace(runID),
		RejectedNoteTop10: []database.EnrichmentReviewNoteCount{},
	}
	noteCounts := map[string]int{}
	for _, record := range records {
		if report.RunID != "" && strings.TrimSpace(record.RunID) != report.RunID {
			continue
		}
		report.Total++
		action := normalizeReviewAction(record.ReviewDecision.Action)
		switch action {
		case "", "pending":
			report.PendingCount++
		case "accept":
			report.AcceptCount++
			report.ReviewedCount++
			report.PublishableCount++
		case "correct":
			report.CorrectCount++
			report.ReviewedCount++
			report.PublishableCount++
		case "reject":
			report.RejectCount++
			report.ReviewedCount++
			notes := strings.TrimSpace(record.ReviewDecision.Notes)
			if notes == "" {
				notes = "(empty)"
			}
			noteCounts[notes]++
		default:
			report.UnsupportedActions = append(report.UnsupportedActions, fmt.Sprintf("review_item_id=%d action=%q", record.ReviewItemID, record.ReviewDecision.Action))
		}
	}
	if report.ReviewedCount > 0 {
		report.PassRate = float64(report.PublishableCount) / float64(report.ReviewedCount)
	}
	report.PassRatePercent = fmt.Sprintf("%.2f%%", roundPercent(report.PassRate))

	notes := make([]database.EnrichmentReviewNoteCount, 0, len(noteCounts))
	for note, count := range noteCounts {
		notes = append(notes, database.EnrichmentReviewNoteCount{Note: note, Count: count})
	}
	sort.Slice(notes, func(i, j int) bool {
		if notes[i].Count == notes[j].Count {
			return notes[i].Note < notes[j].Note
		}
		return notes[i].Count > notes[j].Count
	})
	report.RejectedNoteTop10 = firstReviewNoteCounts(notes, 10)

	switch {
	case len(report.UnsupportedActions) > 0:
		report.RecommendedNextStep = "先修正 unsupported_actions，再 dry-run；不要写回数据库。"
	case report.ReviewedCount == 0:
		report.RecommendedNextStep = "还没有有效人工决策；先完成人工审查。"
	case report.PassRate < 0.9:
		report.RecommendedNextStep = "通过率低于 90%，停止规则扩批；只保留人工 accept/correct 证据，主生产改走 AI 候选 + 自动校验 + 抽样质检。"
	default:
		report.RecommendedNextStep = "通过率达到 90%，仍建议先跑 golden eval 和自动校验，再考虑扩大。"
	}
	return report
}

func firstReviewNoteCounts(values []database.EnrichmentReviewNoteCount, limit int) []database.EnrichmentReviewNoteCount {
	if len(values) <= limit {
		return values
	}
	return values[:limit]
}

func buildGoldenPrefill(repo *database.Repository, records []goldenSampleRecord, mode string, limit int) (goldenPrefillResult, error) {
	mode = strings.TrimSpace(mode)
	if mode == "" {
		mode = "accepted-reviewed"
	}
	if mode != "accepted-reviewed" && mode != "accepted-any" {
		return goldenPrefillResult{}, fmt.Errorf("--mode must be accepted-reviewed or accepted-any")
	}
	poemIDs := make([]int64, 0, len(records))
	for _, record := range records {
		if record.PoemID > 0 {
			poemIDs = append(poemIDs, record.PoemID)
		}
	}
	knowledgeByID, err := repo.ListPoemKnowledgeByPoemIDs(poemIDs)
	if err != nil {
		return goldenPrefillResult{}, err
	}
	tagsByID, err := repo.ListTagsByPoemIDs(poemIDs)
	if err != nil {
		return goldenPrefillResult{}, err
	}

	out := make([]goldenSampleRecord, len(records))
	copy(out, records)
	report := goldenPrefillReport{
		Mode:           mode,
		Total:          len(records),
		Limit:          limit,
		ReviewRequired: true,
		NextStep:       "manual review these prefilled expected_tags and evidence_lines, then set annotation_status to done before using as golden gate",
	}
	for i := range out {
		if limit > 0 && report.Updated >= limit {
			break
		}
		if goldenRecordComplete(out[i]) {
			report.SkippedAlreadyComplete++
			continue
		}
		knowledge, hasKnowledge := knowledgeByID[out[i].PoemID]
		if !hasKnowledge || knowledge.QualityStatus != database.EnrichmentStatusAccepted || !goldenPrefillSourceAllowed(knowledge.Source, mode) {
			report.SkippedNoAcceptedData++
			continue
		}
		tags := tagsByID[out[i].PoemID]
		tagInputs := goldenTagInputsFromTags(tags)
		if len(tagInputs) == 0 {
			report.SkippedNoTags++
			continue
		}
		evidenceLines := goldenEvidenceLinesForTags(out[i].Content, tagInputs)
		if len(evidenceLines) == 0 {
			report.SkippedNoEvidence++
			continue
		}
		out[i].GoldenMeta = cloneGoldenMeta(out[i].GoldenMeta)
		out[i].GoldenMeta["expected_tags"] = tagInputs
		out[i].GoldenMeta["evidence_lines"] = evidenceLines
		out[i].GoldenMeta["annotation_status"] = "prefilled_review_required"
		out[i].GoldenMeta["review_notes"] = strings.TrimSpace(fmt.Sprintf("prefilled from accepted enrichment source=%s; manual review required", knowledge.Source))
		out[i].GoldenMeta["prefill_source"] = knowledge.Source
		out[i].GoldenMeta["prefill_mode"] = mode
		report.Updated++
	}
	return goldenPrefillResult{Records: out, Report: report}, nil
}

func goldenPrefillSourceAllowed(source, mode string) bool {
	source = strings.TrimSpace(source)
	switch mode {
	case "accepted-any":
		return source != ""
	default:
		return strings.Contains(source, "manual_review")
	}
}

func goldenRecordComplete(record goldenSampleRecord) bool {
	if record.GoldenMeta == nil {
		return false
	}
	return goldenMetaSliceLength(record.GoldenMeta["expected_tags"]) > 0 &&
		len(goldenMetaStringSlice(record.GoldenMeta["evidence_lines"])) > 0 &&
		reviewedGoldenStatus(goldenMetaString(record.GoldenMeta, "annotation_status"))
}

func cloneGoldenMeta(meta map[string]any) map[string]any {
	cloned := map[string]any{}
	for key, value := range meta {
		cloned[key] = value
	}
	return cloned
}

func goldenTagInputsFromTags(tags []database.Tag) []database.TagInput {
	inputs := make([]database.TagInput, 0, len(tags))
	seen := map[string]bool{}
	for _, tag := range tags {
		name := strings.TrimSpace(tag.Name)
		category := strings.TrimSpace(tag.Category)
		if name == "" {
			continue
		}
		key := category + "/" + name
		if seen[key] {
			continue
		}
		seen[key] = true
		inputs = append(inputs, database.TagInput{
			Name:        name,
			Category:    category,
			Description: strings.TrimSpace(tag.Description),
			Source:      "accepted_enrichment_prefill",
		})
	}
	return inputs
}

func goldenEvidenceLinesForTags(content []string, tags []database.TagInput) []string {
	evidence := make([]string, 0, len(tags))
	seen := map[string]bool{}
	for _, tag := range tags {
		line := goldenEvidenceLineForTag(content, tag.Name)
		if strings.TrimSpace(line) == "" {
			continue
		}
		if !seen[line] {
			seen[line] = true
			evidence = append(evidence, line)
		}
	}
	return evidence
}

func goldenEvidenceLineForTag(content []string, tagName string) string {
	tagName = strings.TrimSpace(tagName)
	if tagName == "" {
		return ""
	}
	for _, keyword := range goldenEvidenceKeywords(tagName) {
		for _, line := range content {
			if strings.Contains(line, keyword) {
				return strings.TrimSpace(line)
			}
		}
	}
	return ""
}

func goldenEvidenceKeywords(tagName string) []string {
	switch strings.TrimSpace(tagName) {
	case "月亮":
		return []string{"明月", "月明", "月光", "月色", "新月", "初月", "夜月", "月下", "月如钩", "晓月", "秋月", "月华", "月影", "月", "蟾", "婵娟", "玉兔"}
	case "思乡":
		return []string{"故乡", "思乡", "怀乡", "乡关", "故园", "归梦", "归心", "归日", "家书", "客中", "客舍", "旅夜", "雁来", "路遥归梦"}
	case "送别":
		return []string{"送别", "赠别", "留别", "别", "送", "离", "饯"}
	case "春天":
		return []string{"春花", "春风", "春水", "春草", "春色", "春", "花", "柳", "莺", "燕", "芳草", "东风", "桃", "杏"}
	case "边塞":
		return []string{"边塞", "边", "塞", "胡", "羌", "戍", "关", "沙场", "楼兰", "鸡塞", "雁门"}
	case "家国":
		return []string{"家国", "故国", "旧国", "国破", "社稷", "山河", "宫", "朝"}
	case "山水":
		return []string{"山水", "江山", "山河", "山", "水", "江", "河", "湖", "溪", "峰", "岳", "海", "波", "舟", "瀑", "潭"}
	case "文旅":
		return []string{"登", "游", "行", "客", "舟", "路", "旅", "驿", "道"}
	case "相思":
		return []string{"相思", "思", "情", "恋", "梦", "眉", "郎", "佳人", "离恨", "衷素"}
	case "愁绪":
		return []string{"春愁", "闲愁", "愁", "恨", "泪", "怅", "惆怅", "寂寞", "寂寥", "肠断", "断肠", "不寐", "无奈", "销魂", "伤春", "不堪", "哀", "悲", "苦", "痛"}
	case "宴乐歌舞":
		return []string{"歌舞", "歌", "舞", "乐", "酒", "宴", "筵", "管弦", "笙", "鼓", "钟", "琵琶"}
	case "经典引用":
		return []string{}
	default:
		return []string{tagName}
	}
}

func goldenReviewQueue(records []goldenSampleRecord, status string, limit int) []goldenSampleRecord {
	status = strings.TrimSpace(status)
	selected := make([]goldenSampleRecord, 0)
	for _, record := range records {
		if limit > 0 && len(selected) >= limit {
			break
		}
		recordStatus := goldenMetaString(record.GoldenMeta, "annotation_status")
		if status == "" || strings.EqualFold(status, "all") {
			if goldenRecordComplete(record) {
				continue
			}
			selected = append(selected, record)
			continue
		}
		if recordStatus == status {
			selected = append(selected, record)
		}
	}
	return selected
}

func applyGoldenReview(baseRecords, reviewRecords []goldenSampleRecord, reviewer string, requireDone bool) (goldenApplyReviewResult, error) {
	reviewer = strings.TrimSpace(reviewer)
	if reviewer == "" {
		reviewer = "operator"
	}
	reviewByPoem := map[int64]goldenSampleRecord{}
	for _, record := range reviewRecords {
		if record.PoemID < 1 {
			return goldenApplyReviewResult{}, fmt.Errorf("review record has invalid poem_id")
		}
		if _, exists := reviewByPoem[record.PoemID]; exists {
			return goldenApplyReviewResult{}, fmt.Errorf("duplicate review poem_id=%d", record.PoemID)
		}
		if requireDone && !reviewedGoldenStatus(goldenMetaString(record.GoldenMeta, "annotation_status")) {
			return goldenApplyReviewResult{}, fmt.Errorf("poem_id=%d annotation_status must be done/reviewed/accepted/complete", record.PoemID)
		}
		if goldenMetaSliceLength(record.GoldenMeta["expected_tags"]) == 0 {
			return goldenApplyReviewResult{}, fmt.Errorf("poem_id=%d expected_tags is required", record.PoemID)
		}
		evidenceLines := goldenMetaStringSlice(record.GoldenMeta["evidence_lines"])
		if len(evidenceLines) == 0 {
			return goldenApplyReviewResult{}, fmt.Errorf("poem_id=%d evidence_lines is required", record.PoemID)
		}
		for _, evidence := range evidenceLines {
			if !goldenEvidenceInContent(evidence, record.Content) {
				return goldenApplyReviewResult{}, fmt.Errorf("poem_id=%d evidence line is not in content: %s", record.PoemID, evidence)
			}
		}
		reviewByPoem[record.PoemID] = record
	}

	out := make([]goldenSampleRecord, len(baseRecords))
	copy(out, baseRecords)
	applied := 0
	for i, record := range out {
		reviewed, ok := reviewByPoem[record.PoemID]
		if !ok {
			continue
		}
		reviewed.GoldenMeta = cloneGoldenMeta(reviewed.GoldenMeta)
		reviewed.GoldenMeta["reviewed_by"] = reviewer
		reviewed.GoldenMeta["reviewed_at"] = time.Now().UTC().Format(time.RFC3339)
		out[i] = reviewed
		applied++
		delete(reviewByPoem, record.PoemID)
	}
	if len(reviewByPoem) > 0 {
		missing := make([]int64, 0, len(reviewByPoem))
		for poemID := range reviewByPoem {
			missing = append(missing, poemID)
		}
		sort.Slice(missing, func(i, j int) bool { return missing[i] < missing[j] })
		return goldenApplyReviewResult{}, fmt.Errorf("review poem_id not found in base: %v", missing)
	}
	return goldenApplyReviewResult{
		Records: out,
		Report: map[string]any{
			"base_total":       len(baseRecords),
			"review_total":     len(reviewRecords),
			"applied":          applied,
			"reviewer":         reviewer,
			"require_done":     requireDone,
			"next_step":        "run golden-audit on output and use it as the next golden sample if ready",
			"remaining_review": len(baseRecords) - applied,
		},
	}, nil
}

func writeGoldenReviewSheet(path string, records []goldenSampleRecord) error {
	writer, closeOut, err := createWriter(path)
	if err != nil {
		return err
	}
	defer closeOut()

	csvWriter := csv.NewWriter(writer)
	header := []string{
		"poem_id",
		"title",
		"author",
		"dynasty",
		"type",
		"content",
		"expected_tags_json",
		"evidence_lines_json",
		"annotation_status",
		"review_notes",
		"prefill_source",
		"stratum",
	}
	if err := csvWriter.Write(header); err != nil {
		return err
	}
	for _, record := range records {
		expectedTagsJSON, err := goldenMetaJSON(record.GoldenMeta["expected_tags"])
		if err != nil {
			return fmt.Errorf("poem_id=%d expected_tags: %w", record.PoemID, err)
		}
		evidenceLinesJSON, err := goldenMetaJSON(goldenMetaStringSlice(record.GoldenMeta["evidence_lines"]))
		if err != nil {
			return fmt.Errorf("poem_id=%d evidence_lines: %w", record.PoemID, err)
		}
		row := []string{
			strconv.FormatInt(record.PoemID, 10),
			record.Title,
			record.Author,
			record.Dynasty,
			record.Type,
			strings.Join(nonEmptyLines(record.Content), "\n"),
			expectedTagsJSON,
			evidenceLinesJSON,
			goldenMetaString(record.GoldenMeta, "annotation_status"),
			goldenMetaString(record.GoldenMeta, "review_notes"),
			goldenMetaString(record.GoldenMeta, "prefill_source"),
			goldenMetaString(record.GoldenMeta, "stratum"),
		}
		if err := csvWriter.Write(row); err != nil {
			return err
		}
	}
	csvWriter.Flush()
	if err := csvWriter.Error(); err != nil {
		return err
	}
	return nil
}

func readGoldenReviewSheet(path string) ([]goldenSampleRecord, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()

	reader := csv.NewReader(file)
	reader.FieldsPerRecord = -1
	reader.TrimLeadingSpace = true
	rows, err := reader.ReadAll()
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("review sheet is empty")
	}
	header := map[string]int{}
	for i, name := range rows[0] {
		name = strings.TrimPrefix(strings.TrimSpace(name), "\ufeff")
		header[name] = i
	}
	required := []string{"poem_id", "content", "expected_tags_json", "evidence_lines_json", "annotation_status"}
	for _, name := range required {
		if _, ok := header[name]; !ok {
			return nil, fmt.Errorf("review sheet missing required column %q", name)
		}
	}

	records := make([]goldenSampleRecord, 0, len(rows)-1)
	for rowIndex, row := range rows[1:] {
		if csvRowBlank(row) {
			continue
		}
		line := rowIndex + 2
		poemID, err := strconv.ParseInt(strings.TrimSpace(csvValue(row, header, "poem_id")), 10, 64)
		if err != nil || poemID < 1 {
			return nil, fmt.Errorf("line %d: poem_id must be positive", line)
		}
		tags, err := parseGoldenTags(csvValue(row, header, "expected_tags_json"))
		if err != nil {
			return nil, fmt.Errorf("line %d poem_id=%d expected_tags_json: %w", line, poemID, err)
		}
		evidenceLines, err := parseGoldenStringArray(csvValue(row, header, "evidence_lines_json"))
		if err != nil {
			return nil, fmt.Errorf("line %d poem_id=%d evidence_lines_json: %w", line, poemID, err)
		}
		meta := map[string]any{
			"expected_tags":     tags,
			"evidence_lines":    evidenceLines,
			"annotation_status": strings.TrimSpace(csvValue(row, header, "annotation_status")),
		}
		for _, key := range []string{"review_notes", "prefill_source", "stratum"} {
			if value := strings.TrimSpace(csvValue(row, header, key)); value != "" {
				meta[key] = value
			}
		}
		records = append(records, goldenSampleRecord{
			PoemID:     poemID,
			Title:      strings.TrimSpace(csvValue(row, header, "title")),
			Author:     strings.TrimSpace(csvValue(row, header, "author")),
			Dynasty:    strings.TrimSpace(csvValue(row, header, "dynasty")),
			Type:       strings.TrimSpace(csvValue(row, header, "type")),
			Content:    nonEmptyLines(strings.Split(csvValue(row, header, "content"), "\n")),
			GoldenMeta: meta,
		})
	}
	return records, nil
}

func goldenMetaJSON(value any) (string, error) {
	if value == nil {
		return "[]", nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func csvValue(row []string, header map[string]int, name string) string {
	index, ok := header[name]
	if !ok || index < 0 || index >= len(row) {
		return ""
	}
	return row[index]
}

func csvRowBlank(row []string) bool {
	for _, value := range row {
		if strings.TrimSpace(value) != "" {
			return false
		}
	}
	return true
}

func parseGoldenTags(value string) ([]database.TagInput, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return []database.TagInput{}, nil
	}
	var tags []database.TagInput
	if err := json.Unmarshal([]byte(value), &tags); err == nil {
		return tags, nil
	}
	var names []string
	if err := json.Unmarshal([]byte(value), &names); err != nil {
		return nil, err
	}
	tags = make([]database.TagInput, 0, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		tags = append(tags, database.TagInput{Name: name, Category: "theme", Source: "human_review"})
	}
	return tags, nil
}

func parseGoldenStringArray(value string) ([]string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return []string{}, nil
	}
	var lines []string
	if err := json.Unmarshal([]byte(value), &lines); err != nil {
		return nil, err
	}
	return nonEmptyLines(lines), nil
}

func buildGoldenAuditReport(input string, records []goldenSampleRecord, minCompleteRate float64) goldenAuditReport {
	report := goldenAuditReport{
		Input:              strings.TrimSpace(input),
		Total:              len(records),
		MinCompleteRate:    minCompleteRate,
		StatusCounts:       map[string]int{},
		RequiredAction:     "golden set is ready for AI evaluation",
		ReadyForEvaluation: true,
		DuplicatePoemIDs:   []int64{},
		IssueExamples:      []goldenAuditIssue{},
		StratumCounts:      []goldenStratumCount{},
		IssueTop10:         []qualityIssueCount{},
	}
	seenPoems := map[int64]int{}
	stratumCounts := map[string]int{}
	issueCounts := map[string]int{}
	issues := make([]goldenAuditIssue, 0)

	addIssue := func(poemID int64, reason, evidence string) {
		issueCounts[reason]++
		if len(issues) < 20 {
			issues = append(issues, goldenAuditIssue{PoemID: poemID, Reason: reason, Evidence: evidence})
		}
	}

	for _, record := range records {
		seenPoems[record.PoemID]++
		if seenPoems[record.PoemID] == 2 {
			report.DuplicatePoemIDs = append(report.DuplicatePoemIDs, record.PoemID)
			addIssue(record.PoemID, "duplicate_poem_id", strconv.FormatInt(record.PoemID, 10))
		}
		if len(nonEmptyLines(record.Content)) == 0 {
			report.MissingContentCount++
			addIssue(record.PoemID, "missing_content", record.Title)
		}
		if record.GoldenMeta == nil {
			report.MissingMetaCount++
			addIssue(record.PoemID, "missing_golden_meta", record.Title)
			continue
		}

		status := strings.TrimSpace(goldenMetaString(record.GoldenMeta, "annotation_status"))
		if status == "" {
			status = "(empty)"
			addIssue(record.PoemID, "missing_annotation_status", record.Title)
		}
		report.StatusCounts[status]++
		if reviewedGoldenStatus(status) {
			report.ReviewedStatusCount++
		}

		stratum := strings.TrimSpace(goldenMetaString(record.GoldenMeta, "stratum"))
		if stratum == "" {
			stratum = "(empty)"
			addIssue(record.PoemID, "missing_stratum", record.Title)
		}
		stratumCounts[stratum]++

		expectedTagsCount := goldenMetaSliceLength(record.GoldenMeta["expected_tags"])
		evidenceLines := goldenMetaStringSlice(record.GoldenMeta["evidence_lines"])
		if expectedTagsCount > 0 {
			report.ExpectedTagsFilledCount++
		} else {
			addIssue(record.PoemID, "missing_expected_tags", record.Title)
		}
		if len(evidenceLines) > 0 {
			report.EvidenceLinesFilledCount++
		} else {
			addIssue(record.PoemID, "missing_evidence_lines", record.Title)
		}
		invalidEvidence := false
		for _, evidence := range evidenceLines {
			if !goldenEvidenceInContent(evidence, record.Content) {
				report.InvalidEvidenceCount++
				invalidEvidence = true
				addIssue(record.PoemID, "invalid_evidence_line", evidence)
			}
		}
		if expectedTagsCount > 0 && len(evidenceLines) > 0 && !invalidEvidence && reviewedGoldenStatus(status) {
			report.CompleteCount++
		}
	}

	report.UniquePoemIDs = len(seenPoems)
	if report.Total > 0 {
		report.CompleteRate = float64(report.CompleteCount) / float64(report.Total)
	}
	report.CompleteRatePercent = fmt.Sprintf("%.2f%%", roundPercent(report.CompleteRate))
	report.StratumCounts = goldenCountMapToRows(stratumCounts)
	report.IssueTop10 = goldenIssueTop(issueCounts, 10)
	report.IssueExamples = issues

	report.ReadyForEvaluation = report.Total > 0 &&
		report.CompleteRate >= minCompleteRate &&
		report.MissingContentCount == 0 &&
		report.MissingMetaCount == 0 &&
		report.InvalidEvidenceCount == 0 &&
		len(report.DuplicatePoemIDs) == 0
	if !report.ReadyForEvaluation {
		report.RequiredAction = "fill expected_tags, evidence_lines, and reviewed annotation_status before using this golden set as a gate"
	}
	return report
}

func reviewedGoldenStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "done", "reviewed", "accepted", "complete", "completed":
		return true
	default:
		return false
	}
}

func goldenMetaString(meta map[string]any, key string) string {
	if meta == nil {
		return ""
	}
	value, ok := meta[key]
	if !ok || value == nil {
		return ""
	}
	switch v := value.(type) {
	case string:
		return v
	case fmt.Stringer:
		return v.String()
	default:
		return fmt.Sprint(v)
	}
}

func goldenMetaSliceLength(value any) int {
	switch v := value.(type) {
	case nil:
		return 0
	case []any:
		return len(v)
	case []string:
		return len(nonEmptyLines(v))
	case []database.TagInput:
		count := 0
		for _, tag := range v {
			if strings.TrimSpace(tag.Name) != "" || strings.TrimSpace(tag.Category) != "" {
				count++
			}
		}
		return count
	default:
		raw, err := json.Marshal(v)
		if err != nil {
			return 0
		}
		var values []any
		if err := json.Unmarshal(raw, &values); err == nil {
			return len(values)
		}
		return 0
	}
}

func goldenMetaStringSlice(value any) []string {
	switch v := value.(type) {
	case nil:
		return []string{}
	case []string:
		return nonEmptyLines(v)
	case []any:
		lines := make([]string, 0, len(v))
		for _, item := range v {
			if line := strings.TrimSpace(fmt.Sprint(item)); line != "" {
				lines = append(lines, line)
			}
		}
		return lines
	default:
		raw, err := json.Marshal(v)
		if err != nil {
			return []string{}
		}
		var lines []string
		if err := json.Unmarshal(raw, &lines); err == nil {
			return nonEmptyLines(lines)
		}
		return []string{}
	}
}

func nonEmptyLines(lines []string) []string {
	values := make([]string, 0, len(lines))
	for _, line := range lines {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			values = append(values, trimmed)
		}
	}
	return values
}

func goldenEvidenceInContent(evidence string, content []string) bool {
	evidence = strings.TrimSpace(evidence)
	if evidence == "" {
		return false
	}
	for _, line := range content {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if line == evidence || strings.Contains(line, evidence) || strings.Contains(evidence, line) {
			return true
		}
	}
	return strings.Contains(strings.Join(content, "\n"), evidence)
}

func goldenCountMapToRows(counts map[string]int) []goldenStratumCount {
	rows := make([]goldenStratumCount, 0, len(counts))
	for key, count := range counts {
		rows = append(rows, goldenStratumCount{Stratum: key, Count: count})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Count == rows[j].Count {
			return rows[i].Stratum < rows[j].Stratum
		}
		return rows[i].Count > rows[j].Count
	})
	return rows
}

func goldenIssueTop(counts map[string]int, limit int) []qualityIssueCount {
	rows := make([]qualityIssueCount, 0, len(counts))
	for reason, count := range counts {
		rows = append(rows, qualityIssueCount{Reason: reason, Count: count})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Count == rows[j].Count {
			return rows[i].Reason < rows[j].Reason
		}
		return rows[i].Count > rows[j].Count
	})
	if len(rows) <= limit {
		return rows
	}
	return rows[:limit]
}

func buildQualityGateReport(input, samplePath string, candidates []candidateRecord, samplesByID map[int64]sampleRecord, minConfidence float64) qualityGateReport {
	report := qualityGateReport{
		Input:    input,
		Sample:   strings.TrimSpace(samplePath),
		Total:    len(candidates),
		NextStep: "通过的候选可以进入待审队列；warning 样本进低置信人工队列；error 样本不导入。",
	}
	issues := make([]qualityIssue, 0)
	issueByPoem := map[int64]bool{}
	errorByPoem := map[int64]bool{}

	addIssue := func(poemID int64, severity, reason, evidence string) {
		issue := qualityIssue{PoemID: poemID, Severity: severity, Reason: reason, Evidence: evidence}
		issues = append(issues, issue)
		if severity == "error" {
			report.ErrorCount++
			if poemID > 0 {
				errorByPoem[poemID] = true
			}
		} else {
			report.WarningCount++
		}
		if poemID > 0 {
			issueByPoem[poemID] = true
		}
	}

	if len(candidates) == 0 {
		addIssue(0, "error", "no_candidates", "candidate JSONL is empty")
	}
	for _, errMsg := range validateCandidates(candidates) {
		addIssue(0, "error", "format_validation", errMsg)
	}

	for _, candidate := range candidates {
		if confidence, ok := metaFloat(candidate.Meta, "confidence"); ok && confidence < minConfidence {
			report.LowConfidenceCount++
			addIssue(candidate.PoemID, "warning", "low_confidence", fmt.Sprintf("confidence %.2f < %.2f", confidence, minConfidence))
		}

		var sample sampleRecord
		hasSample := false
		if strings.TrimSpace(samplePath) != "" {
			sample, hasSample = samplesByID[candidate.PoemID]
			if !hasSample {
				report.MissingSampleCount++
				addIssue(candidate.PoemID, "warning", "missing_source_sample", "cannot check textual evidence without source sample")
			}
		}

		seenTags := map[string]bool{}
		for _, tag := range candidate.ProposedTags {
			key := strings.TrimSpace(tag.Category) + "/" + strings.TrimSpace(tag.Name)
			if key == "/" {
				continue
			}
			if seenTags[key] {
				addIssue(candidate.PoemID, "error", "duplicate_tag", key)
			}
			seenTags[key] = true

			if strings.TrimSpace(tag.Category) != "" && !allowedTagCategory(tag.Category) {
				addIssue(candidate.PoemID, "error", "unsupported_tag_category", tag.Category)
			}
			if hasSample && strings.TrimSpace(tag.Name) != "" && !tagHasDirectEvidence(tag.Name, sample) {
				addIssue(candidate.PoemID, "warning", "tag_without_direct_evidence", strings.TrimSpace(tag.Name))
			}
		}

		if evidence := possibleOverInterpretation(candidate.ProposedKnowledge); evidence != "" {
			addIssue(candidate.PoemID, "warning", "possible_overinterpretation", evidence)
		}
	}

	report.ValidFormat = report.ErrorCount == 0
	report.NeedsReviewCount = len(issueByPoem)
	report.PassedCount = 0
	for _, candidate := range candidates {
		if !issueByPoem[candidate.PoemID] && !errorByPoem[candidate.PoemID] {
			report.PassedCount++
		}
	}
	report.IssueTop10 = buildQualityIssueTop(issues, 10)
	report.IssueExamples = firstQualityIssues(issues, 20)
	return report
}

func allowedTagCategory(category string) bool {
	switch strings.TrimSpace(category) {
	case "theme", "mood", "scenario", "festival", "season", "image", "grade", "keyword":
		return true
	default:
		return false
	}
}

func metaFloat(meta map[string]any, key string) (float64, bool) {
	if meta == nil {
		return 0, false
	}
	value, ok := meta[key]
	if !ok {
		return 0, false
	}
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case json.Number:
		parsed, err := v.Float64()
		return parsed, err == nil
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}

func tagHasDirectEvidence(tagName string, sample sampleRecord) bool {
	tagName = strings.TrimSpace(tagName)
	if tagName == "" {
		return false
	}
	text := sample.Title + "\n" + strings.Join(sample.Content, "\n")
	if strings.Contains(text, tagName) {
		return true
	}
	runes := []rune(tagName)
	if len(runes) == 2 {
		return strings.ContainsRune(text, runes[0]) && strings.ContainsRune(text, runes[1])
	}
	return false
}

func possibleOverInterpretation(knowledge database.ProposedKnowledgeInput) string {
	joined := knowledge.Summary + knowledge.Translation + knowledge.Annotation + knowledge.Recommendation
	for _, phrase := range []string{"仕途", "被贬", "贬谪", "政治失意", "家族", "出生", "早年经历", "历史事件"} {
		if strings.Contains(joined, phrase) {
			return phrase
		}
	}
	return ""
}

func buildQualityIssueTop(issues []qualityIssue, limit int) []qualityIssueCount {
	counts := map[string]int{}
	for _, issue := range issues {
		counts[issue.Severity+":"+issue.Reason]++
	}
	rows := make([]qualityIssueCount, 0, len(counts))
	for reason, count := range counts {
		rows = append(rows, qualityIssueCount{Reason: reason, Count: count})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Count == rows[j].Count {
			return rows[i].Reason < rows[j].Reason
		}
		return rows[i].Count > rows[j].Count
	})
	if len(rows) <= limit {
		return rows
	}
	return rows[:limit]
}

func firstQualityIssues(values []qualityIssue, limit int) []qualityIssue {
	if len(values) <= limit {
		return values
	}
	return values[:limit]
}

func validateCandidates(candidates []candidateRecord) []string {
	var errs []string
	seen := map[int64]bool{}
	for i, candidate := range candidates {
		line := i + 1
		if candidate.PoemID < 1 {
			errs = append(errs, fmt.Sprintf("line %d: poem_id must be positive", line))
		}
		if seen[candidate.PoemID] {
			errs = append(errs, fmt.Sprintf("line %d: duplicate poem_id %d", line, candidate.PoemID))
		}
		seen[candidate.PoemID] = true

		if len(candidate.ProposedTags) == 0 {
			errs = append(errs, fmt.Sprintf("line %d: proposed_tags is required", line))
		}
		if len(candidate.ProposedTags) > 30 {
			errs = append(errs, fmt.Sprintf("line %d: too many tags", line))
		}
		for _, tag := range candidate.ProposedTags {
			name := strings.TrimSpace(tag.Name)
			if name == "" {
				errs = append(errs, fmt.Sprintf("line %d: tag name is required", line))
			}
			if runeLen(name) > 12 {
				errs = append(errs, fmt.Sprintf("line %d: tag %q is too long", line, name))
			}
			if strings.ContainsAny(name, "，。；,.!?！？\n\r\t") {
				errs = append(errs, fmt.Sprintf("line %d: tag %q should be a short word", line, name))
			}
		}

		knowledge := candidate.ProposedKnowledge
		if strings.TrimSpace(knowledge.Summary) == "" {
			errs = append(errs, fmt.Sprintf("line %d: proposed_knowledge.summary is required", line))
		}
		if length := runeLen(knowledge.Summary); length > 0 && (length < 20 || length > 220) {
			errs = append(errs, fmt.Sprintf("line %d: summary length should be 20-220 chars", line))
		}
		joined := knowledge.Summary + knowledge.Translation + knowledge.Annotation + knowledge.Recommendation
		for _, banned := range []string{"作为AI", "作为 AI", "根据提供的信息", "无法确定"} {
			if strings.Contains(joined, banned) {
				errs = append(errs, fmt.Sprintf("line %d: contains prompt residue %q", line, banned))
			}
		}
	}
	return errs
}

func validateCandidatePoemIDs(repo *database.Repository, candidates []candidateRecord) []string {
	var errs []string
	seenMissing := map[int64]bool{}
	for i, candidate := range candidates {
		if candidate.PoemID < 1 || seenMissing[candidate.PoemID] {
			continue
		}
		if _, err := repo.GetPoemByID(strconv.FormatInt(candidate.PoemID, 10)); err != nil {
			errs = append(errs, fmt.Sprintf("line %d: poem_id %d does not exist", i+1, candidate.PoemID))
			seenMissing[candidate.PoemID] = true
		}
	}
	return errs
}

func generateRulesCandidate(sample sampleRecord) candidateRecord {
	text := strings.Join(sample.Content, " ")
	titleAndText := sample.Title + " " + text
	tags := make([]database.TagInput, 0, 8)
	add := func(name, category string) {
		name = strings.TrimSpace(name)
		category = strings.TrimSpace(category)
		for _, existing := range tags {
			if existing.Name == name && existing.Category == category {
				return
			}
		}
		tags = append(tags, database.TagInput{Name: name, Category: category, Source: "rules"})
	}
	if isMoonText(titleAndText) {
		add("月亮", "theme")
	}
	if isHomesickText(text) {
		add("思乡", "theme")
	}
	if isFarewellText(sample.Title, text) {
		add("送别", "scenario")
	}
	if isSpringText(sample.Title, text) {
		add("春天", "season")
	}
	if isBorderlandText(text) {
		add("边塞", "theme")
	}
	if hasAnyPhrase(text, "家国", "故国", "旧国", "国破", "社稷", "山河") &&
		!hasAnyPhrase(text, "有家难奔", "山河容易改", "山河易改") {
		add("家国", "theme")
	}
	if isLandscapeText(text) {
		add("山水", "theme")
	}
	if isTravelText(sample.Title, text) {
		add("文旅", "scenario")
	}
	if isLoveLongingText(text) || isLoveLongingTitleText(sample.Title) {
		add("相思", "mood")
	}
	if isSorrowText(text) {
		add("愁绪", "mood")
	}
	if isCourtBanquetText(text) {
		add("宴乐歌舞", "scenario")
	}
	translation := strings.Join(sample.Content, " / ")
	if len(tags) == 0 {
		return candidateRecord{
			PoemID: sample.PoemID,
			ProposedKnowledge: database.ProposedKnowledgeInput{
				Translation: translation,
				Source:      "rules",
			},
			Meta: map[string]any{
				"provider":       "rules",
				"model":          rulesProviderModel,
				"skipped_reason": "low_signal_no_specific_tag",
			},
		}
	}

	summaryTags := make([]string, len(tags))
	for i, tag := range tags {
		summaryTags[i] = tag.Name
	}
	sort.Strings(summaryTags)
	summary := fmt.Sprintf("《%s》可归入%s等方向，适合作为诗词知识库的候选引用。", sample.Title, strings.Join(summaryTags, "、"))

	return candidateRecord{
		PoemID:       sample.PoemID,
		ProposedTags: tags,
		ProposedKnowledge: database.ProposedKnowledgeInput{
			Summary:        summary,
			Translation:    translation,
			Annotation:     "标签依据标题和正文意象生成，适合做主题、情绪和场景召回的辅助字段。",
			Recommendation: "适合用于知识库召回、教育内容标注和内容创作选句。",
			Source:         "rules",
		},
		Meta: map[string]any{
			"provider": "rules",
			"model":    rulesProviderModel,
		},
	}
}

func hasAnyPhrase(text string, phrases ...string) bool {
	for _, phrase := range phrases {
		if phrase != "" && strings.Contains(text, phrase) {
			return true
		}
	}
	return false
}

func countAnyPhrase(text string, phrases ...string) int {
	count := 0
	for _, phrase := range phrases {
		if phrase != "" && strings.Contains(text, phrase) {
			count++
		}
	}
	return count
}

func isHomesickText(text string) bool {
	if hasAnyPhrase(text, "归心更比江流急") {
		return false
	}
	return hasAnyPhrase(text,
		"故乡", "思乡", "怀乡", "乡关", "故园", "归梦", "归心", "归日", "家书",
		"客中", "客舍", "旅夜", "雁来音信", "路遥归梦",
	)
}

func isFarewellText(title, text string) bool {
	if hasAnyPhrase(title, "送别", "赠别", "留别", "别董大", "别友") {
		return true
	}
	if hasAnyPhrase(text, "折柳攀花", "攀花折柳", "送香茶", "才郎别后", "从别后") {
		return false
	}
	if hasAnyPhrase(text, "西出阳关无故人，则见俺在这南国梁园依旧亲") {
		return false
	}
	if hasAnyPhrase(text, "甚末娘别离", "带铁锁囚人监系", "离别了三年") {
		return false
	}
	if hasAnyPhrase(text, "离恨") && hasAnyPhrase(text, "归梦", "更行更远", "千里", "尊前", "理行轩", "第一夜") {
		return true
	}
	if hasAnyPhrase(text, "离情") && hasAnyPhrase(text, "暮云遮", "马迟人意懒", "雁行斜") {
		return true
	}
	if hasAnyPhrase(text, "父母在堂，不可远游") {
		return true
	}
	return hasAnyPhrase(text,
		"送别", "别离", "离别", "赠别", "饯别", "留别", "别君", "别友",
		"长亭", "阳关", "故人西辞",
	)
}

func isMoonText(text string) bool {
	if hasAnyPhrase(text,
		"风月", "花月约", "花月酒", "批风切月", "月里嫦娥", "月圆云遮",
		"星前月下", "月下砧声", "水底捞明月", "弓弯秋月",
		"秋月春花",
		"月中丹桂", "月中仙桂",
		"薄命的婵娟", "弓开秋月", "清风明月为知友",
		"清风明月琴三弄", "月明千里故人来",
	) {
		return false
	}
	strongMoon := hasAnyPhrase(text,
		"明月", "月明", "月色", "新月", "夜月", "月下", "月圆", "月当", "月上", "月照",
		"月落", "月出", "月夜", "月华", "月影", "婵娟", "蟾光", "玉兔", "马头明月", "梁园月",
	)
	if strongMoon {
		return true
	}
	if hasAnyPhrase(text, "月如钩", "月小楼西", "月宵", "晓月", "秋月", "月中", "花月正春风") {
		return true
	}
	return false
}

func isSpringText(title, text string) bool {
	if hasAnyPhrase(text,
		"落花流水人何处",
		"和气春风满画堂",
		"春意透酥胸",
		"春色横眉黛",
		"春风翡翠巢",
		"芳草封高冢",
		"不同桃李芳",
		"他管甚桃李开",
		"青春不再来",
		"秋月春花",
		"为寻春色到儿家",
		"春风和气生",
		"趁春风攀折凤城花",
		"趁一江春水向东流",
		"春风一枝花解语", "春风一度", "袖得春风马上归",
		"春日风动", "春山春水流", "春种秋收",
	) {
		return false
	}
	if hasAnyPhrase(text,
		"春风", "春花", "春水", "春草", "春色", "春红", "春半", "芳春", "寻春",
		"春归", "春来", "暮春", "早春", "新春", "清明", "芳草",
		"落花", "红英", "桃李", "柳堤", "柳眼", "啼莺", "黄莺", "娇莺", "紫燕", "燕语",
		"蝶影", "绿肥红瘦", "群芳", "繁英", "落梅", "莺花笑人", "若道伤春",
	) {
		return true
	}
	return hasAnyPhrase(title, "绿肥红瘦", "群芳", "繁英")
}

func isBorderlandText(text string) bool {
	if hasAnyPhrase(text,
		"白马将军", "飞虎叛贼",
		"沙场上杀的血染马蹄红",
		"送昭君出塞北",
		"秦白起是军卒", "灌将军曾贩屦",
		"元也波戎", "将军校统", "出塞美人图",
		"鼋将军", "鼍先锋", "水里兵卒", "不能够边塞上统军居帅府",
	) {
		return false
	}
	return hasAnyPhrase(text,
		"边塞", "塞上", "塞下", "塞外", "出塞", "入塞", "从军", "征人", "征夫", "征戍",
		"将军", "军中", "沙场", "烽火", "戍楼", "边关", "边城", "单于", "楼兰",
		"玉门关", "阴山", "天山", "大漠", "胡马", "羌笛",
	) || (hasAnyPhrase(text, "辽阳", "鸡塞") && hasAnyPhrase(text, "征", "戍", "军", "将", "胡", "烽"))
}

func isLandscapeText(text string) bool {
	if hasAnyPhrase(text,
		"春山摇", "秋波转", "春山低翠", "秋水凝眸",
		"水仙山鬼", "山长水远", "千山万水", "万山烟水",
		"水远山重", "江山和宇宙", "山寿", "百座连城", "山河容易改",
		"愁山", "闷海", "愁山和闷海", "愁山闷海",
		"渡水登山", "武陵溪畔", "海沸山裂",
		"睢河岸外", "广武山前",
		"山根印堂", "山神庙", "山河易改",
		"恨不的上青山变化身", "海棠颜色江梅韵",
		"南山颂载歌", "北海樽频敬", "桑榆晚景", "松柏遐龄",
		"镜水映红莲", "山海深盟", "黄河几浅清", "蓬岛仙乡", "寿等岗陵",
		"海变桑田", "红蓼岸绿杨川", "山呼",
		"一片白云隔黄河", "云雨楚山娘", "江岸边不是哥哥的渔船",
		"蟠溪水上为渔", "梁山伯", "风雪渔樵也只落的做一场故事儿演",
		"淮河口，又送上楚峰头", "胸臆卷江淮", "千里关山独自个走",
		"凝望断不归舟",
		"淮河渡翻船", "气压山河百二雄", "岳阳楼下枯干了的柳树神",
		"春山春水流",
		"煮海", "熬煎铅汞山头火", "医治相思海上方", "撮合山",
		"山阴王子猷", "没缆舟", "山海也似恩", "胸卷江淮", "冲开海狱",
		"崩塌山崖", "未曾结庐山长老白莲社", "东海龙王大会垓",
		"药师佛海会", "逢山开路，遇水叠桥", "百二山河壮帝居",
		"为看青山懒赠鞭", "离不了天涯和那海边",
	) {
		return false
	}
	if hasAnyPhrase(text,
		"山水", "江山", "山河", "洞庭",
		"庐山", "黄河", "长江", "瀑布", "望岳", "登高", "登山", "溪水",
	) {
		return true
	}
	landCount := countAnyPhrase(text, "山", "峰", "岭", "岳", "峡", "谷", "岸")
	waterCount := countAnyPhrase(text, "水", "江", "河", "湖", "溪", "泉", "潭", "瀑", "潮", "海", "波", "浪", "舟")
	return landCount > 0 && waterCount > 0
}

func isTravelText(title, text string) bool {
	if !isLandscapeText(text) {
		return false
	}
	if hasAnyPhrase(title, "景", "登", "游", "过", "宿", "泊", "泛舟") {
		return true
	}
	return hasAnyPhrase(text,
		"一到处堪游戏", "闲游戏", "曾玩府游州", "画船儿来往", "泛舟", "行舟",
	)
}

func isLoveLongingText(text string) bool {
	if hasAnyPhrase(text,
		"成姻眷",
		"老迈情怀悲倦客",
		"不是相思病",
		"思往事",
		"云雨楚山娘",
		"生则同衾，死则同坟",
		"尚留恋懒心回",
		"休将你这歹孩儿留恋着",
	) {
		return false
	}
	if hasAnyPhrase(text, "亲许我中秋会约", "意相投", "姻缘可配当", "心厮爱", "夫妻谁比方") {
		return true
	}
	return hasAnyPhrase(text,
		"相思", "长相思", "思君", "思妇", "思依依", "芳心", "情怀", "传情", "多情", "红粉无情",
		"无限情", "偎人", "萧郎", "檀郎", "月下情", "星前约", "秋波", "衷素", "春梦",
		"留恋", "相留恋", "盼顾恋", "恋多娇", "生则同衾", "死则同穴",
		"好事天悭", "密爱幽欢", "不能恋", "并头莲", "相敬爱",
	)
}

func isLoveLongingTitleText(title string) bool {
	if hasAnyPhrase(title, "不思量", "芳心心哽噎", "成姻眷") {
		return false
	}
	return hasAnyPhrase(title,
		"相思", "长相思", "思君", "思妇", "思依依", "留恋", "相留恋", "盼顾恋", "恋多娇",
		"多情", "萧郎一去", "密爱幽欢不能恋",
	)
}

func isSorrowText(text string) bool {
	if hasAnyPhrase(text,
		"甚闲愁到我心头",
		"不识忧，不识愁",
		"愁甚么前程",
		"羞无奈",
		"患难哀怜我",
		"老迈情怀悲倦客",
		"贫不忧愁富不骄",
		"恨不的上青山变化身",
		"莫愁",
		"寄哀书",
		"难奈何俺那六臂",
		"交易难成",
		"披枷带锁",
		"哭倒也则一声哀",
		"他把你十分恨",
		"记人小恨",
		"怀旧恨夫妇两参商",
		"展放开愁眉",
		"我愁甚么架上三封天子书",
	) {
		return false
	}
	if hasAnyPhrase(text, "哀告") && !hasAnyPhrase(text, "愁", "忧", "恨", "泪", "怅", "悲", "苦", "痛") {
		return false
	}
	return hasAnyPhrase(text,
		"愁", "恨", "泪", "怅", "寂寞", "寂寥", "肠断", "断肠", "不寐", "无奈", "销魂", "伤春",
		"难成", "梦断", "不堪", "不能平", "浮生", "奈何", "哀", "难排", "难禁", "难受", "独无言", "伤悲",
		"饥寒债",
	)
}

func isCourtBanquetText(text string) bool {
	if hasAnyPhrase(text,
		"歌楼酒力", "一曲艳歌", "象板银锣", "楼心舞", "扇底歌",
		"唱得好，弹得好，舞的好", "翠袖殷勤捧玉钟", "酬酢处两三巡",
		"酒肴整备，再到十里长亭", "一部笙歌出入随",
		"只待学吹箫同跨丹山凤",
	) {
		return false
	}
	if hasAnyPhrase(text, "琴三弄", "曲奏求凰", "弦中语") {
		return true
	}
	return countAnyPhrase(text,
		"清歌", "歌", "舞", "笙", "箫", "箫鼓", "管弦", "霓裳", "罗袖", "金钗", "香醪", "宴", "酒",
	) >= 2
}

type qanloGenerateOptions struct {
	Model     string
	BaseURL   string
	APIKeyEnv string
}

func generateQanloCandidate(sample sampleRecord, opts qanloGenerateOptions) (candidateRecord, error) {
	apiKey := strings.TrimSpace(os.Getenv(opts.APIKeyEnv))
	if apiKey == "" && opts.APIKeyEnv == "QANLO_AGENT_KEY" {
		apiKey = strings.TrimSpace(os.Getenv("QANLO_API_KEY"))
	}
	if apiKey == "" {
		return candidateRecord{}, fmt.Errorf("%s is not set", opts.APIKeyEnv)
	}
	baseURL := strings.TrimRight(strings.TrimSpace(opts.BaseURL), "/")
	if baseURL == "" {
		baseURL = "https://qanlo.com/v1"
	}
	model := strings.TrimSpace(opts.Model)
	if model == "" {
		model = "deepseek-v4-flash"
	}

	payload := map[string]any{
		"model":       model,
		"temperature": 0.2,
		"messages": []map[string]string{
			{
				"role":    "system",
				"content": "你是诗词知识库数据标注员。只基于给定标题、作者、朝代、正文生成结构化 JSON，不编造历史背景。输出必须是 JSON，不要 Markdown。",
			},
			{
				"role":    "user",
				"content": buildQanloPrompt(sample),
			},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return candidateRecord{}, err
	}

	req, err := http.NewRequest(http.MethodPost, baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return candidateRecord{}, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return candidateRecord{}, err
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return candidateRecord{}, err
	}
	if resp.StatusCode >= 300 {
		return candidateRecord{}, fmt.Errorf("qanlo request failed: status=%d body=%s", resp.StatusCode, truncate(string(respBody), 800))
	}

	var completion struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(respBody, &completion); err != nil {
		return candidateRecord{}, err
	}
	if len(completion.Choices) == 0 {
		return candidateRecord{}, fmt.Errorf("qanlo response has no choices")
	}

	var candidate candidateRecord
	if err := json.Unmarshal([]byte(cleanJSONContent(completion.Choices[0].Message.Content)), &candidate); err != nil {
		return candidateRecord{}, err
	}
	candidate.PoemID = sample.PoemID
	if candidate.Meta == nil {
		candidate.Meta = map[string]any{}
	}
	candidate.Meta["provider"] = "qanlo"
	candidate.Meta["model"] = model
	return candidate, nil
}

func buildQanloPrompt(sample sampleRecord) string {
	payload, _ := json.Marshal(sample)
	return `请为这首诗生成知识库候选数据，格式必须严格为：
{
  "poem_id": 123,
  "proposed_tags": [
    {"name":"思乡","category":"theme","description":"短说明","source":"ai"}
  ],
  "proposed_knowledge": {
    "summary":"80-150字，说明诗意、情绪和适用场景",
    "translation":"简短白话说明，可为空但尽量给出",
    "annotation":"关键意象或关键词说明",
    "recommendation":"20-80字推荐理由",
    "source":"ai"
  }
}
标签类别只能从 theme、mood、scenario、festival、season、image、grade、keyword 中选；标签用短词，不要句子。
诗词数据：` + string(payload)
}

func cleanJSONContent(content string) string {
	content = strings.TrimSpace(content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	return strings.TrimSpace(content)
}

func printJSON(value any) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func getenvDefault(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func firstStrings(values []string, limit int) []string {
	if len(values) <= limit {
		return values
	}
	return values[:limit]
}

func roundPercent(rate float64) float64 {
	return math.Round(rate*10000) / 100
}

func runeLen(value string) int {
	return len([]rune(strings.TrimSpace(value)))
}

func truncate(value string, maxLen int) string {
	runes := []rune(value)
	if len(runes) <= maxLen {
		return value
	}
	return string(runes[:maxLen]) + "..."
}

func defaultStatus(status string) string {
	status = strings.TrimSpace(status)
	if status == "" {
		return database.EnrichmentStatusPending
	}
	return status
}

func safeFileName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "run"
	}
	var builder strings.Builder
	lastDash := false
	for _, r := range value {
		ok := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-'
		if ok {
			builder.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			builder.WriteRune('-')
			lastDash = true
		}
	}
	safe := strings.Trim(builder.String(), "-")
	if safe == "" {
		return "run"
	}
	return safe
}
