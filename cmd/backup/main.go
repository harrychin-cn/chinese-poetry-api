package main

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

const (
	toolName        = "chinese-poetry-api/cmd/backup"
	manifestVersion = "1"
	backupMethod    = "sqlite-vacuum-into"
)

type options struct {
	DBPath string
	OutDir string
	Keep   int
	Now    time.Time
}

type backupManifest struct {
	Tool            string    `json:"tool"`
	Version         string    `json:"version"`
	Method          string    `json:"method"`
	CreatedAt       time.Time `json:"created_at"`
	SourcePath      string    `json:"source_path"`
	SourceAbs       string    `json:"source_abs"`
	SourceSizeBytes int64     `json:"source_size_bytes"`
	BackupFile      string    `json:"backup_file"`
	BackupPath      string    `json:"backup_path"`
	BackupSizeBytes int64     `json:"backup_size_bytes"`
	ManifestFile    string    `json:"manifest_file"`
	SHA256          string    `json:"sha256"`
	SQLiteVersion   string    `json:"sqlite_version,omitempty"`
	PageCount       int64     `json:"page_count,omitempty"`
	FreelistCount   int64     `json:"freelist_count,omitempty"`
	QuickCheck      string    `json:"quick_check"`
	Keep            int       `json:"keep"`
}

type backupSummary struct {
	CreatedAt       time.Time `json:"created_at"`
	BackupFile      string    `json:"backup_file"`
	ManifestFile    string    `json:"manifest_file"`
	BackupSizeBytes int64     `json:"backup_size_bytes"`
	SHA256          string    `json:"sha256"`
	QuickCheck      string    `json:"quick_check"`
}

type manifestIndex struct {
	Tool        string          `json:"tool"`
	Version     string          `json:"version"`
	GeneratedAt time.Time       `json:"generated_at"`
	SourceAbs   string          `json:"source_abs"`
	Keep        int             `json:"keep"`
	Latest      *backupSummary  `json:"latest,omitempty"`
	Backups     []backupSummary `json:"backups"`
}

type discoveredManifest struct {
	manifest backupManifest
	path     string
}

func main() {
	if err := run(os.Args[1:], time.Now().UTC()); err != nil {
		fmt.Fprintf(os.Stderr, "backup failed: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string, now time.Time) error {
	opts := options{Now: now.UTC()}

	fs := flag.NewFlagSet("backup", flag.ContinueOnError)
	fs.StringVar(&opts.DBPath, "db", "data/poetry.db", "path to source SQLite database")
	fs.StringVar(&opts.OutDir, "out", "backups", "directory to write backup files and manifests")
	fs.IntVar(&opts.Keep, "keep", 7, "number of latest backups to keep; 0 disables cleanup")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage:\n  go run ./cmd/backup --db data/poetry.db --out backups --keep 7\n\nOptions:\n")
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if len(fs.Args()) > 0 {
		return fmt.Errorf("unexpected positional arguments: %s", strings.Join(fs.Args(), " "))
	}

	return runBackup(opts)
}

func runBackup(opts options) error {
	if strings.TrimSpace(opts.DBPath) == "" {
		return errors.New("--db is required")
	}
	if strings.TrimSpace(opts.OutDir) == "" {
		return errors.New("--out is required")
	}
	if opts.Keep < 0 {
		return errors.New("--keep must be 0 or greater")
	}
	if opts.Now.IsZero() {
		opts.Now = time.Now().UTC()
	}
	opts.Now = opts.Now.UTC()

	sourceAbs, err := filepath.Abs(opts.DBPath)
	if err != nil {
		return fmt.Errorf("resolve source database path: %w", err)
	}
	sourceInfo, err := os.Stat(sourceAbs)
	if err != nil {
		return fmt.Errorf("stat source database %q: %w", sourceAbs, err)
	}
	if sourceInfo.IsDir() {
		return fmt.Errorf("source database %q is a directory", sourceAbs)
	}

	outAbs, err := filepath.Abs(opts.OutDir)
	if err != nil {
		return fmt.Errorf("resolve output directory: %w", err)
	}
	if err := os.MkdirAll(outAbs, 0o755); err != nil {
		return fmt.Errorf("create output directory %q: %w", outAbs, err)
	}

	base := backupBaseName(sourceAbs)
	backupPath, manifestPath, err := nextBackupPaths(outAbs, base, filepath.Ext(sourceAbs), opts.Now)
	if err != nil {
		return err
	}

	sqliteVersion, pageCount, freelistCount, err := vacuumInto(sourceAbs, backupPath)
	if err != nil {
		_ = os.Remove(backupPath)
		return err
	}

	quickCheck, err := sqliteQuickCheck(backupPath)
	if err != nil {
		return fmt.Errorf("quick_check backup %q: %w", backupPath, err)
	}
	if strings.ToLower(strings.TrimSpace(quickCheck)) != "ok" {
		return fmt.Errorf("quick_check backup %q returned %q", backupPath, quickCheck)
	}

	backupHash, err := fileSHA256(backupPath)
	if err != nil {
		return fmt.Errorf("hash backup %q: %w", backupPath, err)
	}
	backupInfo, err := os.Stat(backupPath)
	if err != nil {
		return fmt.Errorf("stat backup %q: %w", backupPath, err)
	}

	manifest := backupManifest{
		Tool:            toolName,
		Version:         manifestVersion,
		Method:          backupMethod,
		CreatedAt:       opts.Now,
		SourcePath:      opts.DBPath,
		SourceAbs:       sourceAbs,
		SourceSizeBytes: sourceInfo.Size(),
		BackupFile:      filepath.Base(backupPath),
		BackupPath:      backupPath,
		BackupSizeBytes: backupInfo.Size(),
		ManifestFile:    filepath.Base(manifestPath),
		SHA256:          backupHash,
		SQLiteVersion:   sqliteVersion,
		PageCount:       pageCount,
		FreelistCount:   freelistCount,
		QuickCheck:      quickCheck,
		Keep:            opts.Keep,
	}
	if err := writeJSONExclusive(manifestPath, manifest); err != nil {
		_ = os.Remove(backupPath)
		return fmt.Errorf("write backup manifest %q: %w", manifestPath, err)
	}

	deleted, err := applyRetention(outAbs, sourceAbs, opts.Keep)
	if err != nil {
		return err
	}
	if err := writeManifestIndex(outAbs, sourceAbs, opts.Keep, time.Now().UTC()); err != nil {
		return err
	}

	fmt.Fprintf(os.Stdout, "Backup created: %s\n", backupPath)
	fmt.Fprintf(os.Stdout, "Manifest: %s\n", manifestPath)
	fmt.Fprintf(os.Stdout, "SHA256: %s\n", backupHash)
	fmt.Fprintf(os.Stdout, "Quick check: %s\n", quickCheck)
	if opts.Keep > 0 {
		fmt.Fprintf(os.Stdout, "Retention: kept latest %d, deleted %d old backup(s)\n", opts.Keep, deleted)
	} else {
		fmt.Fprintln(os.Stdout, "Retention: disabled")
	}

	return nil
}

func vacuumInto(sourceAbs, backupPath string) (sqliteVersion string, pageCount, freelistCount int64, err error) {
	sqliteVersion, pageCount, freelistCount, goErr := vacuumIntoGo(sourceAbs, backupPath)
	if goErr == nil {
		return sqliteVersion, pageCount, freelistCount, nil
	}

	_ = os.Remove(backupPath)
	sqliteVersion, pageCount, freelistCount, cliErr := vacuumIntoCLI(sourceAbs, backupPath)
	if cliErr == nil {
		return sqliteVersion, pageCount, freelistCount, nil
	}

	return "", 0, 0, fmt.Errorf("run VACUUM INTO with Go sqlite driver: %w; sqlite3 CLI fallback failed: %v", goErr, cliErr)
}

func vacuumIntoGo(sourceAbs, backupPath string) (sqliteVersion string, pageCount, freelistCount int64, err error) {
	db, err := sql.Open("sqlite3", sqliteReadOnlyDSN(sourceAbs))
	if err != nil {
		return "", 0, 0, fmt.Errorf("open source database read-only: %w", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		return "", 0, 0, fmt.Errorf("ping source database: %w", err)
	}
	if err := db.QueryRow("SELECT sqlite_version()").Scan(&sqliteVersion); err != nil {
		return "", 0, 0, fmt.Errorf("read SQLite version: %w", err)
	}
	if err := db.QueryRow("PRAGMA page_count").Scan(&pageCount); err != nil {
		return "", 0, 0, fmt.Errorf("read page_count: %w", err)
	}
	if err := db.QueryRow("PRAGMA freelist_count").Scan(&freelistCount); err != nil {
		return "", 0, 0, fmt.Errorf("read freelist_count: %w", err)
	}

	if _, err := db.Exec("VACUUM INTO ?", backupPath); err != nil {
		_ = os.Remove(backupPath)
		if _, fallbackErr := db.Exec("VACUUM INTO " + sqliteString(backupPath)); fallbackErr != nil {
			return "", 0, 0, fmt.Errorf("run VACUUM INTO: %w; fallback failed: %v", err, fallbackErr)
		}
	}

	return sqliteVersion, pageCount, freelistCount, nil
}

func vacuumIntoCLI(sourceAbs, backupPath string) (string, int64, int64, error) {
	sql := strings.Join([]string{
		"SELECT sqlite_version();",
		"PRAGMA page_count;",
		"PRAGMA freelist_count;",
		"VACUUM INTO " + sqliteString(backupPath) + ";",
	}, " ")
	output, err := runSQLiteCLI(sourceAbs, sql)
	if err != nil {
		_ = os.Remove(backupPath)
		return "", 0, 0, err
	}

	lines := nonEmptyLines(output)
	if len(lines) < 3 {
		_ = os.Remove(backupPath)
		return "", 0, 0, fmt.Errorf("unexpected sqlite3 output: %q", output)
	}
	pageCount, err := strconv.ParseInt(lines[1], 10, 64)
	if err != nil {
		_ = os.Remove(backupPath)
		return "", 0, 0, fmt.Errorf("parse page_count %q: %w", lines[1], err)
	}
	freelistCount, err := strconv.ParseInt(lines[2], 10, 64)
	if err != nil {
		_ = os.Remove(backupPath)
		return "", 0, 0, fmt.Errorf("parse freelist_count %q: %w", lines[2], err)
	}

	return lines[0], pageCount, freelistCount, nil
}

func sqliteQuickCheck(dbPath string) (string, error) {
	result, goErr := sqliteQuickCheckGo(dbPath)
	if goErr == nil {
		return result, nil
	}

	result, cliErr := sqliteQuickCheckCLI(dbPath)
	if cliErr == nil {
		return result, nil
	}

	return "", fmt.Errorf("quick_check with Go sqlite driver: %w; sqlite3 CLI fallback failed: %v", goErr, cliErr)
}

func sqliteQuickCheckGo(dbPath string) (string, error) {
	db, err := sql.Open("sqlite3", sqliteReadOnlyDSN(dbPath))
	if err != nil {
		return "", err
	}
	defer db.Close()

	var result string
	if err := db.QueryRow("PRAGMA quick_check").Scan(&result); err != nil {
		return "", err
	}
	return result, nil
}

func sqliteQuickCheckCLI(dbPath string) (string, error) {
	output, err := runSQLiteCLI(dbPath, "PRAGMA quick_check;")
	if err != nil {
		return "", err
	}
	lines := nonEmptyLines(output)
	if len(lines) == 0 {
		return "", fmt.Errorf("empty sqlite3 quick_check output")
	}
	return lines[0], nil
}

func runSQLiteCLI(dbPath, sql string) (string, error) {
	cmd := exec.Command("sqlite3", "-batch", "-readonly", "-cmd", ".timeout 30000", dbPath, sql)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("sqlite3 CLI command failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return string(output), nil
}

func nonEmptyLines(output string) []string {
	raw := strings.Split(output, "\n")
	lines := make([]string, 0, len(raw))
	for _, line := range raw {
		line = strings.TrimSpace(line)
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func applyRetention(outAbs, sourceAbs string, keep int) (int, error) {
	if keep == 0 {
		return 0, nil
	}

	items, err := loadManifests(outAbs, sourceAbs)
	if err != nil {
		return 0, err
	}
	if len(items) <= keep {
		return 0, nil
	}

	deleted := 0
	for _, item := range items[keep:] {
		if item.manifest.BackupPath != "" {
			if err := os.Remove(item.manifest.BackupPath); err != nil && !os.IsNotExist(err) {
				return deleted, fmt.Errorf("delete old backup %q: %w", item.manifest.BackupPath, err)
			}
		}
		if err := os.Remove(item.path); err != nil && !os.IsNotExist(err) {
			return deleted, fmt.Errorf("delete old manifest %q: %w", item.path, err)
		}
		deleted++
	}

	return deleted, nil
}

func writeManifestIndex(outAbs, sourceAbs string, keep int, generatedAt time.Time) error {
	items, err := loadManifests(outAbs, sourceAbs)
	if err != nil {
		return err
	}

	index := manifestIndex{
		Tool:        toolName,
		Version:     manifestVersion,
		GeneratedAt: generatedAt.UTC(),
		SourceAbs:   sourceAbs,
		Keep:        keep,
		Backups:     make([]backupSummary, 0, len(items)),
	}
	for _, item := range items {
		summary := backupSummary{
			CreatedAt:       item.manifest.CreatedAt,
			BackupFile:      item.manifest.BackupFile,
			ManifestFile:    filepath.Base(item.path),
			BackupSizeBytes: item.manifest.BackupSizeBytes,
			SHA256:          item.manifest.SHA256,
			QuickCheck:      item.manifest.QuickCheck,
		}
		if index.Latest == nil {
			latest := summary
			index.Latest = &latest
		}
		index.Backups = append(index.Backups, summary)
	}

	return writeJSONFile(filepath.Join(outAbs, "manifest.json"), index)
}

func loadManifests(outAbs, sourceAbs string) ([]discoveredManifest, error) {
	entries, err := os.ReadDir(outAbs)
	if err != nil {
		return nil, fmt.Errorf("read backup directory %q: %w", outAbs, err)
	}

	items := make([]discoveredManifest, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".manifest.json") {
			continue
		}

		path := filepath.Join(outAbs, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read manifest %q: %w", path, err)
		}

		var manifest backupManifest
		if err := json.Unmarshal(data, &manifest); err != nil {
			continue
		}
		if manifest.Tool != toolName || manifest.SourceAbs != sourceAbs {
			continue
		}
		items = append(items, discoveredManifest{manifest: manifest, path: path})
	}

	sort.SliceStable(items, func(i, j int) bool {
		return items[i].manifest.CreatedAt.After(items[j].manifest.CreatedAt)
	})
	return items, nil
}

func nextBackupPaths(outAbs, base, ext string, now time.Time) (string, string, error) {
	if ext == "" {
		ext = ".db"
	}
	stamp := now.UTC().Format("20060102T150405.000000000Z")
	for i := 0; i < 100; i++ {
		suffix := stamp
		if i > 0 {
			suffix = fmt.Sprintf("%s-%02d", stamp, i)
		}
		stem := fmt.Sprintf("%s-%s", base, suffix)
		backupPath := filepath.Join(outAbs, stem+ext)
		manifestPath := filepath.Join(outAbs, stem+".manifest.json")
		if !fileExists(backupPath) && !fileExists(manifestPath) {
			return backupPath, manifestPath, nil
		}
	}
	return "", "", fmt.Errorf("could not find an unused backup filename in %q", outAbs)
}

func backupBaseName(sourceAbs string) string {
	name := strings.TrimSuffix(filepath.Base(sourceAbs), filepath.Ext(sourceAbs))
	name = regexp.MustCompile(`[^A-Za-z0-9._-]+`).ReplaceAllString(name, "-")
	name = strings.Trim(name, "-_.")
	if name == "" {
		return "sqlite-backup"
	}
	return name
}

func sqliteReadOnlyDSN(path string) string {
	return "file:" + filepath.ToSlash(path) + "?mode=ro&_busy_timeout=30000"
}

func sqliteString(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func writeJSONExclusive(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(data)
	return err
}

func writeJSONFile(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal %q: %w", path, err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write %q: %w", path, err)
	}
	return nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
