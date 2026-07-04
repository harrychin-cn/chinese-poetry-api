package database

import (
	"fmt"
	"strings"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/palemoky/chinese-poetry-api/internal/classifier"
)

// DB wraps the gorm.DB connection
type DB struct {
	*gorm.DB
}

// Open opens a connection to the SQLite database using GORM
// maxOpenConns: maximum number of open connections (0 = use default of 1 for safety)
// maxIdleConns: maximum number of idle connections (0 = use default of 1)
func Open(path string, maxOpenConns, maxIdleConns int) (*DB, error) {
	// Configure GORM
	config := &gorm.Config{
		Logger:  logger.Default.LogMode(logger.Silent), // Change to logger.Info for debugging
		NowFunc: time.Now,
		// Prepare statements for better performance
		PrepareStmt: true,
	}

	// SQLite connection string with optimizations for concurrent writes
	// _busy_timeout: wait up to 5 seconds if database is locked
	// _journal_mode=WAL: Write-Ahead Logging for better concurrency
	// _synchronous=NORMAL: balance between safety and performance
	// cache=shared: allow multiple connections to share cache
	// _cache_size=-64000: 64MB page cache (negative = KB, positive = pages)
	// _temp_store=MEMORY: use memory for temporary tables and indices
	dsn := path + "?_foreign_keys=on&_journal_mode=WAL&_busy_timeout=5000&_synchronous=NORMAL&cache=shared&_cache_size=-64000&_temp_store=MEMORY"

	// Open database with GORM SQLite driver
	db, err := gorm.Open(sqlite.Open(dsn), config)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Get underlying sql.DB for connection pool settings
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get underlying sql.DB: %w", err)
	}

	// Set connection pool settings
	// Default to 1 connection for safety (data processing)
	// Can be increased for read-heavy API serving
	if maxOpenConns <= 0 {
		maxOpenConns = 1 // Safe default for write-heavy workloads
	}
	if maxIdleConns <= 0 {
		maxIdleConns = 1
	}

	sqlDB.SetMaxOpenConns(maxOpenConns)
	sqlDB.SetMaxIdleConns(maxIdleConns)
	sqlDB.SetConnMaxLifetime(5 * time.Minute)

	// Test connection
	if err := sqlDB.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return &DB{db}, nil
}

// NewDBFromGorm wraps an existing gorm.DB connection.
// This is useful for testing with custom database configurations.
func NewDBFromGorm(db *gorm.DB) *DB {
	return &DB{db}
}

// Migrate creates all tables, indexes, and initial data for both language variants
func (db *DB) Migrate() error {
	// Create metadata table first
	if err := db.Exec(`CREATE TABLE IF NOT EXISTS metadata (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`).Error; err != nil {
		return fmt.Errorf("failed to create metadata table: %w", err)
	}

	// Create tables for both language variants
	for _, lang := range []Lang{LangHans, LangHant} {
		if err := db.migrateTablesForLang(lang); err != nil {
			return fmt.Errorf("failed to migrate tables for %s: %w", lang, err)
		}
		if sqliteFTS5Enabled {
			if err := db.migrateFTSTableForLang(lang); err != nil {
				return fmt.Errorf("failed to migrate fts table for %s: %w", lang, err)
			}
		}

		// Insert initial data for this language variant
		if err := db.insertInitialDataForLang(lang); err != nil {
			return fmt.Errorf("failed to insert initial data for %s: %w", lang, err)
		}
	}

	if err := db.migrateAPIKeyTables(); err != nil {
		return fmt.Errorf("failed to migrate api key tables: %w", err)
	}
	if err := db.migrateBillingTables(); err != nil {
		return fmt.Errorf("failed to migrate billing tables: %w", err)
	}
	if err := db.migrateTagTables(); err != nil {
		return fmt.Errorf("failed to migrate tag tables: %w", err)
	}
	if err := db.migrateKnowledgeTables(); err != nil {
		return fmt.Errorf("failed to migrate knowledge tables: %w", err)
	}
	if err := db.migrateRequestLogTables(); err != nil {
		return fmt.Errorf("failed to migrate request log tables: %w", err)
	}
	if err := db.migrateFeedbackTables(); err != nil {
		return fmt.Errorf("failed to migrate feedback tables: %w", err)
	}
	if err := db.migrateAbuseTables(); err != nil {
		return fmt.Errorf("failed to migrate abuse protection tables: %w", err)
	}
	if err := db.migrateOriginalWorkTables(); err != nil {
		return fmt.Errorf("failed to migrate original work tables: %w", err)
	}

	// Update schema version
	if err := db.Exec(
		`INSERT OR REPLACE INTO metadata (key, value, updated_at) VALUES (?, ?, ?)`,
		"schema_version",
		fmt.Sprintf("%d", SchemaVersion),
		time.Now(),
	).Error; err != nil {
		return fmt.Errorf("failed to update schema version: %w", err)
	}

	return nil
}

// migrateFTSTableForLang creates an FTS5 index table for Chinese full-text search.
// The table is optional at runtime: if the binary is not built with -tags sqlite_fts5,
// this returns a clear error so operators know how to build the commercial search edition.
func (db *DB) migrateFTSTableForLang(lang Lang) error {
	ftsTable := poemFTSTable(lang)
	sql := fmt.Sprintf(`CREATE VIRTUAL TABLE IF NOT EXISTS %s USING fts5(
		title,
		content,
		author
	)`, ftsTable)
	if err := db.Exec(sql).Error; err != nil {
		return fmt.Errorf("failed to create FTS5 table %s; rebuild with -tags sqlite_fts5: %w", ftsTable, err)
	}

	return nil
}

func (db *DB) migrateTagTables() error {
	if err := db.Exec(`CREATE TABLE IF NOT EXISTS tags (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		category TEXT NOT NULL,
		description TEXT,
		source TEXT NOT NULL DEFAULT 'manual',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(name, category)
	)`).Error; err != nil {
		return err
	}

	if err := db.Exec(`CREATE TABLE IF NOT EXISTS poem_tags (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		poem_id INTEGER NOT NULL,
		tag_id INTEGER NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (tag_id) REFERENCES tags(id),
		UNIQUE(poem_id, tag_id)
	)`).Error; err != nil {
		return err
	}

	db.Exec(`CREATE INDEX IF NOT EXISTS idx_tags_category_name ON tags(category, name)`)
	db.Exec(`CREATE INDEX IF NOT EXISTS idx_poem_tags_poem ON poem_tags(poem_id)`)
	db.Exec(`CREATE INDEX IF NOT EXISTS idx_poem_tags_tag ON poem_tags(tag_id)`)

	return nil
}

func (db *DB) migrateAPIKeyTables() error {
	if err := db.Exec(`CREATE TABLE IF NOT EXISTS user_accounts (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		handle TEXT NOT NULL UNIQUE,
		display_name TEXT NOT NULL,
		email TEXT,
		bio TEXT,
		avatar_url TEXT,
		website_url TEXT,
		status TEXT NOT NULL DEFAULT 'active',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`).Error; err != nil {
		return err
	}

	if err := db.Exec(`CREATE TABLE IF NOT EXISTS api_keys (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		account_id INTEGER,
		key_hash TEXT NOT NULL UNIQUE,
		key_prefix TEXT NOT NULL,
		name TEXT NOT NULL,
		tier TEXT NOT NULL DEFAULT 'free',
		daily_limit INTEGER NOT NULL DEFAULT 1000,
		enabled BOOLEAN NOT NULL DEFAULT TRUE,
		notes TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		revoked_at DATETIME,
		FOREIGN KEY (account_id) REFERENCES user_accounts(id)
	)`).Error; err != nil {
		return err
	}
	if err := db.ensureColumn("api_keys", "account_id", "account_id INTEGER"); err != nil {
		return err
	}
	if err := db.ensureColumn("api_keys", "notes", "notes TEXT"); err != nil {
		return err
	}
	if err := db.ensureColumn("api_keys", "updated_at", "updated_at DATETIME"); err != nil {
		return err
	}

	if err := db.Exec(`CREATE TABLE IF NOT EXISTS api_key_usage (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		api_key_id INTEGER NOT NULL,
		usage_date TEXT NOT NULL,
		request_count INTEGER NOT NULL DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (api_key_id) REFERENCES api_keys(id),
		UNIQUE(api_key_id, usage_date)
	)`).Error; err != nil {
		return err
	}

	db.Exec(`CREATE INDEX IF NOT EXISTS idx_user_accounts_handle ON user_accounts(handle)`)
	db.Exec(`CREATE INDEX IF NOT EXISTS idx_api_keys_account ON api_keys(account_id)`)
	db.Exec(`CREATE INDEX IF NOT EXISTS idx_api_keys_hash ON api_keys(key_hash)`)
	db.Exec(`CREATE INDEX IF NOT EXISTS idx_api_key_usage_key_date ON api_key_usage(api_key_id, usage_date)`)

	return nil
}

func (db *DB) migrateBillingTables() error {
	if err := db.Exec(`CREATE TABLE IF NOT EXISTS api_key_qanlo_bindings (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		api_key_id INTEGER NOT NULL UNIQUE,
		status TEXT NOT NULL DEFAULT 'pending',
		external_user_id TEXT NOT NULL,
		external_device_id TEXT NOT NULL,
		qanlo_key_hash TEXT,
		qanlo_key_prefix TEXT,
		qanlo_base_url TEXT,
		callback_state TEXT UNIQUE,
		callback_expires_at DATETIME,
		last_synced_at DATETIME,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (api_key_id) REFERENCES api_keys(id)
	)`).Error; err != nil {
		return err
	}

	if err := db.Exec(`CREATE TABLE IF NOT EXISTS qanlo_callback_events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		api_key_id INTEGER NOT NULL,
		callback_state TEXT NOT NULL UNIQUE,
		event_type TEXT NOT NULL DEFAULT 'callback',
		key_prefix TEXT,
		qanlo_base_url TEXT,
		raw_query TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (api_key_id) REFERENCES api_keys(id)
	)`).Error; err != nil {
		return err
	}

	db.Exec(`CREATE INDEX IF NOT EXISTS idx_qanlo_bindings_api_key ON api_key_qanlo_bindings(api_key_id)`)
	db.Exec(`CREATE INDEX IF NOT EXISTS idx_qanlo_bindings_state ON api_key_qanlo_bindings(callback_state)`)
	db.Exec(`CREATE INDEX IF NOT EXISTS idx_qanlo_events_api_key ON qanlo_callback_events(api_key_id)`)

	return nil
}

func (db *DB) migrateKnowledgeTables() error {
	if err := db.Exec(`CREATE TABLE IF NOT EXISTS poem_knowledge (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		poem_id INTEGER NOT NULL UNIQUE,
		summary TEXT,
		translation TEXT,
		annotation TEXT,
		recommendation TEXT,
		quality_status TEXT NOT NULL DEFAULT 'draft',
		source TEXT NOT NULL DEFAULT 'ai',
		reviewer TEXT,
		review_notes TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`).Error; err != nil {
		return err
	}

	if err := db.Exec(`CREATE TABLE IF NOT EXISTS poem_embeddings (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		poem_id INTEGER NOT NULL,
		provider TEXT NOT NULL,
		model TEXT NOT NULL,
		dimension INTEGER NOT NULL,
		vector_json TEXT NOT NULL,
		content_hash TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(poem_id, provider, model)
	)`).Error; err != nil {
		return err
	}

	if err := db.Exec(`CREATE TABLE IF NOT EXISTS enrichment_jobs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		status TEXT NOT NULL DEFAULT 'pending',
		scope TEXT NOT NULL DEFAULT 'sample',
		total_count INTEGER NOT NULL DEFAULT 0,
		processed_count INTEGER NOT NULL DEFAULT 0,
		accepted_count INTEGER NOT NULL DEFAULT 0,
		rejected_count INTEGER NOT NULL DEFAULT 0,
		error_count INTEGER NOT NULL DEFAULT 0,
		config_json TEXT,
		started_at DATETIME,
		finished_at DATETIME,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`).Error; err != nil {
		return err
	}

	if err := db.Exec(`CREATE TABLE IF NOT EXISTS enrichment_review_items (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		job_id INTEGER,
		poem_id INTEGER NOT NULL,
		status TEXT NOT NULL DEFAULT 'pending',
		proposed_tags_json TEXT,
		proposed_knowledge_json TEXT,
		applied_tag_ids_json TEXT,
		previous_knowledge_json TEXT,
		reviewer TEXT,
		review_notes TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (job_id) REFERENCES enrichment_jobs(id)
	)`).Error; err != nil {
		return err
	}
	if err := db.ensureColumn("enrichment_review_items", "applied_tag_ids_json", "applied_tag_ids_json TEXT"); err != nil {
		return err
	}
	if err := db.ensureColumn("enrichment_review_items", "previous_knowledge_json", "previous_knowledge_json TEXT"); err != nil {
		return err
	}

	db.Exec(`CREATE INDEX IF NOT EXISTS idx_poem_knowledge_status ON poem_knowledge(quality_status)`)
	db.Exec(`CREATE INDEX IF NOT EXISTS idx_poem_embeddings_poem_model ON poem_embeddings(poem_id, provider, model)`)
	db.Exec(`CREATE INDEX IF NOT EXISTS idx_enrichment_jobs_status ON enrichment_jobs(status)`)
	db.Exec(`CREATE INDEX IF NOT EXISTS idx_enrichment_review_status ON enrichment_review_items(status)`)
	db.Exec(`CREATE INDEX IF NOT EXISTS idx_enrichment_review_poem ON enrichment_review_items(poem_id)`)

	return nil
}

func (db *DB) migrateRequestLogTables() error {
	if err := db.Exec(`CREATE TABLE IF NOT EXISTS api_request_logs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		api_key_id INTEGER,
		usage_date TEXT NOT NULL,
		method TEXT NOT NULL,
		path TEXT NOT NULL,
		endpoint TEXT NOT NULL,
		status_code INTEGER NOT NULL,
		latency_ms INTEGER NOT NULL DEFAULT 0,
		billable BOOLEAN NOT NULL DEFAULT FALSE,
		error_class TEXT,
		query_text TEXT,
		query_signature TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (api_key_id) REFERENCES api_keys(id)
	)`).Error; err != nil {
		return err
	}

	db.Exec(`CREATE INDEX IF NOT EXISTS idx_api_request_logs_key_date ON api_request_logs(api_key_id, usage_date)`)
	db.Exec(`CREATE INDEX IF NOT EXISTS idx_api_request_logs_date_endpoint ON api_request_logs(usage_date, endpoint)`)
	db.Exec(`CREATE INDEX IF NOT EXISTS idx_api_request_logs_query ON api_request_logs(usage_date, query_signature)`)

	return nil
}

func (db *DB) migrateFeedbackTables() error {
	if err := db.Exec(`CREATE TABLE IF NOT EXISTS feedback_items (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		api_key_id INTEGER NOT NULL,
		type TEXT NOT NULL DEFAULT 'other',
		subject TEXT,
		message TEXT NOT NULL,
		contact TEXT,
		status TEXT NOT NULL DEFAULT 'open',
		admin_notes TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (api_key_id) REFERENCES api_keys(id)
	)`).Error; err != nil {
		return err
	}

	db.Exec(`CREATE INDEX IF NOT EXISTS idx_feedback_status_created ON feedback_items(status, created_at)`)
	db.Exec(`CREATE INDEX IF NOT EXISTS idx_feedback_api_key_created ON feedback_items(api_key_id, created_at)`)

	return nil
}

func (db *DB) migrateAbuseTables() error {
	if err := db.Exec(`CREATE TABLE IF NOT EXISTS abuse_blocks (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		target_type TEXT NOT NULL,
		target_value TEXT NOT NULL,
		reason TEXT NOT NULL DEFAULT 'manual',
		enabled BOOLEAN NOT NULL DEFAULT TRUE,
		expires_at DATETIME,
		created_by TEXT NOT NULL DEFAULT 'operator',
		notes TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(target_type, target_value)
	)`).Error; err != nil {
		return err
	}

	db.Exec(`CREATE INDEX IF NOT EXISTS idx_abuse_blocks_target ON abuse_blocks(target_type, target_value)`)
	db.Exec(`CREATE INDEX IF NOT EXISTS idx_abuse_blocks_active ON abuse_blocks(enabled, expires_at)`)

	return nil
}

func (db *DB) migrateOriginalWorkTables() error {
	if err := db.Exec(`CREATE TABLE IF NOT EXISTS original_works (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		work_code TEXT UNIQUE,
		api_key_id INTEGER NOT NULL,
		title TEXT NOT NULL,
		work_type TEXT NOT NULL DEFAULT 'poem',
		content TEXT NOT NULL,
		content_hash TEXT NOT NULL,
		description TEXT,
		visibility TEXT NOT NULL DEFAULT 'private',
		status TEXT NOT NULL DEFAULT 'draft',
		license_type TEXT NOT NULL DEFAULT 'cc0-like',
		license_version TEXT NOT NULL DEFAULT 'v0.1',
		original_commitment BOOLEAN NOT NULL DEFAULT FALSE,
		license_accepted BOOLEAN NOT NULL DEFAULT FALSE,
		plagiarism_status TEXT NOT NULL DEFAULT 'pending',
		image_prompt TEXT,
		version INTEGER NOT NULL DEFAULT 1,
		published_at DATETIME,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (api_key_id) REFERENCES api_keys(id)
	)`).Error; err != nil {
		return err
	}

	if err := db.Exec(`CREATE TABLE IF NOT EXISTS original_work_versions (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		work_id INTEGER NOT NULL,
		version INTEGER NOT NULL,
		title TEXT NOT NULL,
		content TEXT NOT NULL,
		content_hash TEXT NOT NULL,
		change_note TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (work_id) REFERENCES original_works(id),
		UNIQUE(work_id, version)
	)`).Error; err != nil {
		return err
	}

	if err := db.Exec(`CREATE TABLE IF NOT EXISTS work_license_acceptances (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		work_id INTEGER NOT NULL,
		api_key_id INTEGER NOT NULL,
		license_type TEXT NOT NULL,
		license_version TEXT NOT NULL,
		original_commitment BOOLEAN NOT NULL DEFAULT FALSE,
		license_accepted BOOLEAN NOT NULL DEFAULT FALSE,
		acceptance_text TEXT,
		accepted_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (work_id) REFERENCES original_works(id),
		FOREIGN KEY (api_key_id) REFERENCES api_keys(id)
	)`).Error; err != nil {
		return err
	}

	if err := db.Exec(`CREATE TABLE IF NOT EXISTS work_publication_events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		work_id INTEGER NOT NULL,
		api_key_id INTEGER NOT NULL,
		from_status TEXT,
		to_status TEXT NOT NULL,
		visibility TEXT NOT NULL,
		event_type TEXT NOT NULL DEFAULT 'publish',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (work_id) REFERENCES original_works(id),
		FOREIGN KEY (api_key_id) REFERENCES api_keys(id)
	)`).Error; err != nil {
		return err
	}

	if err := db.Exec(`CREATE TABLE IF NOT EXISTS image_prompts (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		work_id INTEGER NOT NULL,
		api_key_id INTEGER NOT NULL,
		prompt TEXT NOT NULL,
		source TEXT NOT NULL DEFAULT 'work',
		style TEXT,
		size TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (work_id) REFERENCES original_works(id),
		FOREIGN KEY (api_key_id) REFERENCES api_keys(id)
	)`).Error; err != nil {
		return err
	}

	if err := db.Exec(`CREATE TABLE IF NOT EXISTS media_assets (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		work_id INTEGER NOT NULL,
		api_key_id INTEGER NOT NULL,
		asset_type TEXT NOT NULL DEFAULT 'image',
		source TEXT NOT NULL DEFAULT 'generated',
		url TEXT,
		b64_json TEXT,
		mime_type TEXT,
		model TEXT,
		size TEXT,
		quality TEXT,
		output_format TEXT,
		prompt TEXT,
		revised_prompt TEXT,
		storage_provider TEXT,
		storage_key TEXT,
		file_path TEXT,
		byte_size INTEGER NOT NULL DEFAULT 0,
		checksum_sha256 TEXT,
		credit_cost INTEGER NOT NULL DEFAULT 0,
		visibility TEXT NOT NULL DEFAULT 'private',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (work_id) REFERENCES original_works(id),
		FOREIGN KEY (api_key_id) REFERENCES api_keys(id)
	)`).Error; err != nil {
		return err
	}
	if err := db.ensureColumn("media_assets", "storage_provider", "storage_provider TEXT"); err != nil {
		return err
	}
	if err := db.ensureColumn("media_assets", "storage_key", "storage_key TEXT"); err != nil {
		return err
	}
	if err := db.ensureColumn("media_assets", "file_path", "file_path TEXT"); err != nil {
		return err
	}
	if err := db.ensureColumn("media_assets", "byte_size", "byte_size INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := db.ensureColumn("media_assets", "checksum_sha256", "checksum_sha256 TEXT"); err != nil {
		return err
	}
	if err := db.ensureColumn("media_assets", "credit_cost", "credit_cost INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}

	if err := db.Exec(`CREATE TABLE IF NOT EXISTS image_generation_jobs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		work_id INTEGER NOT NULL,
		api_key_id INTEGER NOT NULL,
		status TEXT NOT NULL DEFAULT 'pending',
		prompt TEXT NOT NULL,
		style TEXT,
		size TEXT,
		model TEXT,
		quality TEXT,
		output_format TEXT,
		error_message TEXT,
		media_asset_id INTEGER,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (work_id) REFERENCES original_works(id),
		FOREIGN KEY (api_key_id) REFERENCES api_keys(id),
		FOREIGN KEY (media_asset_id) REFERENCES media_assets(id)
	)`).Error; err != nil {
		return err
	}

	if err := db.Exec(`CREATE TABLE IF NOT EXISTS audio_generation_jobs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		work_id INTEGER NOT NULL,
		api_key_id INTEGER NOT NULL,
		status TEXT NOT NULL DEFAULT 'pending',
		prompt TEXT NOT NULL,
		voice TEXT,
		style TEXT,
		background_style TEXT,
		model TEXT,
		output_format TEXT,
		error_message TEXT,
		media_asset_id INTEGER,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (work_id) REFERENCES original_works(id),
		FOREIGN KEY (api_key_id) REFERENCES api_keys(id),
		FOREIGN KEY (media_asset_id) REFERENCES media_assets(id)
	)`).Error; err != nil {
		return err
	}

	if err := db.Exec(`CREATE TABLE IF NOT EXISTS music_generation_jobs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		work_id INTEGER NOT NULL,
		api_key_id INTEGER NOT NULL,
		status TEXT NOT NULL DEFAULT 'pending',
		prompt TEXT NOT NULL,
		music_style TEXT,
		mode TEXT,
		model TEXT,
		output_format TEXT,
		error_message TEXT,
		media_asset_id INTEGER,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (work_id) REFERENCES original_works(id),
		FOREIGN KEY (api_key_id) REFERENCES api_keys(id),
		FOREIGN KEY (media_asset_id) REFERENCES media_assets(id)
	)`).Error; err != nil {
		return err
	}

	if err := db.Exec(`CREATE TABLE IF NOT EXISTS reverse_creation_jobs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		api_key_id INTEGER NOT NULL,
		work_id INTEGER,
		status TEXT NOT NULL DEFAULT 'pending',
		source_type TEXT NOT NULL DEFAULT 'story',
		source_text TEXT,
		image_url TEXT,
		work_type TEXT NOT NULL DEFAULT 'poem',
		style TEXT,
		prompt TEXT NOT NULL,
		generated_title TEXT,
		generated_content TEXT,
		error_message TEXT,
		model TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (api_key_id) REFERENCES api_keys(id),
		FOREIGN KEY (work_id) REFERENCES original_works(id)
	)`).Error; err != nil {
		return err
	}

	if err := db.Exec(`CREATE TABLE IF NOT EXISTS credit_wallets (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		api_key_id INTEGER NOT NULL UNIQUE,
		balance INTEGER NOT NULL DEFAULT 0,
		total_granted INTEGER NOT NULL DEFAULT 0,
		total_spent INTEGER NOT NULL DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (api_key_id) REFERENCES api_keys(id)
	)`).Error; err != nil {
		return err
	}

	if err := db.Exec(`CREATE TABLE IF NOT EXISTS credit_transactions (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		wallet_id INTEGER NOT NULL,
		api_key_id INTEGER NOT NULL,
		work_id INTEGER,
		media_asset_id INTEGER,
		job_id INTEGER,
		transaction_type TEXT NOT NULL,
		amount INTEGER NOT NULL,
		balance_after INTEGER NOT NULL,
		reason TEXT,
		idempotency_key TEXT UNIQUE,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (wallet_id) REFERENCES credit_wallets(id),
		FOREIGN KEY (api_key_id) REFERENCES api_keys(id),
		FOREIGN KEY (work_id) REFERENCES original_works(id),
		FOREIGN KEY (media_asset_id) REFERENCES media_assets(id),
		FOREIGN KEY (job_id) REFERENCES image_generation_jobs(id)
	)`).Error; err != nil {
		return err
	}

	if err := db.Exec(`CREATE TABLE IF NOT EXISTS work_tips (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		work_id INTEGER NOT NULL,
		from_api_key_id INTEGER NOT NULL,
		to_api_key_id INTEGER NOT NULL,
		amount INTEGER NOT NULL,
		message TEXT,
		idempotency_key TEXT NOT NULL,
		status TEXT NOT NULL DEFAULT 'succeeded',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (work_id) REFERENCES original_works(id),
		FOREIGN KEY (from_api_key_id) REFERENCES api_keys(id),
		FOREIGN KEY (to_api_key_id) REFERENCES api_keys(id),
		UNIQUE(from_api_key_id, idempotency_key)
	)`).Error; err != nil {
		return err
	}

	if err := db.Exec(`CREATE TABLE IF NOT EXISTS work_fingerprints (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		work_id INTEGER NOT NULL,
		normalized_text TEXT NOT NULL,
		normalized_hash TEXT NOT NULL,
		simhash TEXT NOT NULL,
		ngram_json TEXT,
		embedding_json TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (work_id) REFERENCES original_works(id)
	)`).Error; err != nil {
		return err
	}
	if err := db.ensureColumn("work_fingerprints", "embedding_json", "embedding_json TEXT"); err != nil {
		return err
	}

	if err := db.Exec(`CREATE TABLE IF NOT EXISTS plagiarism_reports (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		work_id INTEGER NOT NULL,
		normalized_hash TEXT NOT NULL,
		simhash TEXT NOT NULL,
		risk_level TEXT NOT NULL,
		risk_reason TEXT,
		exact_match_count INTEGER NOT NULL DEFAULT 0,
		similar_match_count INTEGER NOT NULL DEFAULT 0,
		top_matches_json TEXT,
		review_status TEXT NOT NULL DEFAULT 'auto_checked',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (work_id) REFERENCES original_works(id)
	)`).Error; err != nil {
		return err
	}

	if err := db.Exec(`CREATE TABLE IF NOT EXISTS similarity_matches (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		report_id INTEGER NOT NULL,
		work_id INTEGER NOT NULL,
		source_type TEXT NOT NULL,
		source_id TEXT NOT NULL,
		source_title TEXT,
		source_author TEXT,
		similarity REAL NOT NULL DEFAULT 0,
		match_type TEXT NOT NULL,
		excerpt TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (report_id) REFERENCES plagiarism_reports(id),
		FOREIGN KEY (work_id) REFERENCES original_works(id)
	)`).Error; err != nil {
		return err
	}

	if err := db.Exec(`CREATE TABLE IF NOT EXISTS manual_review_queue (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		work_id INTEGER NOT NULL,
		report_id INTEGER NOT NULL,
		risk_level TEXT NOT NULL,
		reason TEXT,
		status TEXT NOT NULL DEFAULT 'pending',
		reviewer TEXT,
		review_notes TEXT,
		decided_at DATETIME,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (work_id) REFERENCES original_works(id),
		FOREIGN KEY (report_id) REFERENCES plagiarism_reports(id)
	)`).Error; err != nil {
		return err
	}
	if err := db.ensureColumn("manual_review_queue", "reviewer", "reviewer TEXT"); err != nil {
		return err
	}
	if err := db.ensureColumn("manual_review_queue", "review_notes", "review_notes TEXT"); err != nil {
		return err
	}
	if err := db.ensureColumn("manual_review_queue", "decided_at", "decided_at DATETIME"); err != nil {
		return err
	}

	if err := db.Exec(`CREATE TABLE IF NOT EXISTS plagiarism_corpus_sources (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		source_type TEXT NOT NULL DEFAULT 'network_corpus',
		title TEXT NOT NULL,
		author TEXT,
		source_url TEXT,
		content TEXT NOT NULL,
		normalized_hash TEXT NOT NULL,
		embedding_json TEXT,
		status TEXT NOT NULL DEFAULT 'enabled',
		notes TEXT,
		created_by TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`).Error; err != nil {
		return err
	}

	db.Exec(`CREATE INDEX IF NOT EXISTS idx_original_works_api_key_created ON original_works(api_key_id, created_at)`)
	db.Exec(`CREATE INDEX IF NOT EXISTS idx_original_works_public ON original_works(status, visibility, published_at)`)
	db.Exec(`CREATE INDEX IF NOT EXISTS idx_original_works_hash ON original_works(content_hash)`)
	db.Exec(`CREATE INDEX IF NOT EXISTS idx_original_work_versions_work ON original_work_versions(work_id, version)`)
	db.Exec(`CREATE INDEX IF NOT EXISTS idx_work_license_acceptances_work ON work_license_acceptances(work_id, accepted_at)`)
	db.Exec(`CREATE INDEX IF NOT EXISTS idx_work_publication_events_work ON work_publication_events(work_id, created_at)`)
	db.Exec(`CREATE INDEX IF NOT EXISTS idx_image_prompts_work ON image_prompts(work_id, created_at)`)
	db.Exec(`CREATE INDEX IF NOT EXISTS idx_media_assets_work ON media_assets(work_id, asset_type, created_at)`)
	db.Exec(`CREATE INDEX IF NOT EXISTS idx_media_assets_api_key ON media_assets(api_key_id, created_at)`)
	db.Exec(`CREATE INDEX IF NOT EXISTS idx_media_assets_storage ON media_assets(storage_provider, storage_key)`)
	db.Exec(`CREATE INDEX IF NOT EXISTS idx_credit_wallets_api_key ON credit_wallets(api_key_id)`)
	db.Exec(`CREATE INDEX IF NOT EXISTS idx_credit_transactions_api_key ON credit_transactions(api_key_id, created_at)`)
	db.Exec(`CREATE INDEX IF NOT EXISTS idx_credit_transactions_work ON credit_transactions(work_id, created_at)`)
	db.Exec(`CREATE INDEX IF NOT EXISTS idx_work_tips_work ON work_tips(work_id, created_at)`)
	db.Exec(`CREATE INDEX IF NOT EXISTS idx_work_tips_from_key ON work_tips(from_api_key_id, created_at)`)
	db.Exec(`CREATE INDEX IF NOT EXISTS idx_work_tips_to_key ON work_tips(to_api_key_id, created_at)`)
	db.Exec(`CREATE INDEX IF NOT EXISTS idx_image_generation_jobs_work ON image_generation_jobs(work_id, status, created_at)`)
	db.Exec(`CREATE INDEX IF NOT EXISTS idx_image_generation_jobs_api_key ON image_generation_jobs(api_key_id, created_at)`)
	db.Exec(`CREATE INDEX IF NOT EXISTS idx_audio_generation_jobs_work ON audio_generation_jobs(work_id, status, created_at)`)
	db.Exec(`CREATE INDEX IF NOT EXISTS idx_audio_generation_jobs_api_key ON audio_generation_jobs(api_key_id, created_at)`)
	db.Exec(`CREATE INDEX IF NOT EXISTS idx_music_generation_jobs_work ON music_generation_jobs(work_id, status, created_at)`)
	db.Exec(`CREATE INDEX IF NOT EXISTS idx_music_generation_jobs_api_key ON music_generation_jobs(api_key_id, created_at)`)
	db.Exec(`CREATE INDEX IF NOT EXISTS idx_reverse_creation_jobs_api_key ON reverse_creation_jobs(api_key_id, created_at)`)
	db.Exec(`CREATE INDEX IF NOT EXISTS idx_reverse_creation_jobs_work ON reverse_creation_jobs(work_id, created_at)`)
	db.Exec(`CREATE INDEX IF NOT EXISTS idx_work_fingerprints_work ON work_fingerprints(work_id, created_at)`)
	db.Exec(`CREATE INDEX IF NOT EXISTS idx_work_fingerprints_hash ON work_fingerprints(normalized_hash)`)
	db.Exec(`CREATE INDEX IF NOT EXISTS idx_plagiarism_reports_work ON plagiarism_reports(work_id, created_at)`)
	db.Exec(`CREATE INDEX IF NOT EXISTS idx_plagiarism_reports_risk ON plagiarism_reports(risk_level, created_at)`)
	db.Exec(`CREATE INDEX IF NOT EXISTS idx_similarity_matches_report ON similarity_matches(report_id, similarity)`)
	db.Exec(`CREATE INDEX IF NOT EXISTS idx_similarity_matches_work ON similarity_matches(work_id, created_at)`)
	db.Exec(`CREATE INDEX IF NOT EXISTS idx_manual_review_queue_status ON manual_review_queue(status, risk_level, created_at)`)
	db.Exec(`CREATE INDEX IF NOT EXISTS idx_plagiarism_corpus_status ON plagiarism_corpus_sources(status, source_type, updated_at)`)
	db.Exec(`CREATE INDEX IF NOT EXISTS idx_plagiarism_corpus_hash ON plagiarism_corpus_sources(normalized_hash)`)

	return nil
}

func (db *DB) ensureColumn(table, name, definition string) error {
	var columns []struct {
		Name string
	}
	if err := db.Raw(fmt.Sprintf("PRAGMA table_info(%s)", table)).Scan(&columns).Error; err != nil {
		return err
	}
	for _, column := range columns {
		if column.Name == name {
			return nil
		}
	}
	return db.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s", table, definition)).Error
}

// migrateTablesForLang creates all tables for a specific language variant
func (db *DB) migrateTablesForLang(lang Lang) error {
	dynastyTable := dynastiesTable(lang)
	authorTable := authorsTable(lang)
	poetryTypeTable := poetryTypesTable(lang)
	poemTable := poemsTable(lang)

	// Create dynasties table
	dynastySQL := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL UNIQUE,
		name_en TEXT,
		start_year INTEGER,
		end_year INTEGER,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`, dynastyTable)
	if err := db.Exec(dynastySQL).Error; err != nil {
		return fmt.Errorf("failed to create %s: %w", dynastyTable, err)
	}

	// Create authors table
	authorSQL := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		dynasty_id INTEGER,
		description TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (dynasty_id) REFERENCES %s(id),
		UNIQUE(name, dynasty_id)
	)`, authorTable, dynastyTable)
	if err := db.Exec(authorSQL).Error; err != nil {
		return fmt.Errorf("failed to create %s: %w", authorTable, err)
	}
	// Create index on dynasty_id
	db.Exec(fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%s_dynasty ON %s(dynasty_id)", authorTable, authorTable))

	// Create poetry_types table
	poetryTypeSQL := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
		id INTEGER PRIMARY KEY,
		name TEXT NOT NULL UNIQUE,
		category TEXT NOT NULL,
		lines INTEGER,
		chars_per_line INTEGER,
		description TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`, poetryTypeTable)
	if err := db.Exec(poetryTypeSQL).Error; err != nil {
		return fmt.Errorf("failed to create %s: %w", poetryTypeTable, err)
	}

	// Create poems table
	poemSQL := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
		id INTEGER PRIMARY KEY,
		type_id INTEGER,
		title TEXT NOT NULL,
		content TEXT NOT NULL,
		content_hash TEXT,
		author_id INTEGER,
		dynasty_id INTEGER,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (type_id) REFERENCES %s(id),
		FOREIGN KEY (author_id) REFERENCES %s(id),
		FOREIGN KEY (dynasty_id) REFERENCES %s(id)
	)`, poemTable, poetryTypeTable, authorTable, dynastyTable)
	if err := db.Exec(poemSQL).Error; err != nil {
		return fmt.Errorf("failed to create %s: %w", poemTable, err)
	}

	// Create indexes for poems
	db.Exec(fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%s_type ON %s(type_id)", poemTable, poemTable))
	db.Exec(fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%s_title ON %s(title)", poemTable, poemTable))
	db.Exec(fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%s_author ON %s(author_id)", poemTable, poemTable))
	db.Exec(fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%s_dynasty ON %s(dynasty_id)", poemTable, poemTable))
	db.Exec(fmt.Sprintf("CREATE UNIQUE INDEX IF NOT EXISTS idx_%s_unique ON %s(title, content_hash)", poemTable, poemTable))
	// Composite index for efficient multi-type random selection (type_id IN ... with id range lookups)
	db.Exec(fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%s_type_id ON %s(type_id, id)", poemTable, poemTable))

	return nil
}

// insertInitialDataForLang inserts initial data for a specific language variant
func (db *DB) insertInitialDataForLang(lang Lang) error {
	dynastyTable := dynastiesTable(lang)
	poetryTypeTable := poetryTypesTable(lang)

	// Prepare SQL - convert to traditional if needed
	dynastiesSQL := strings.ReplaceAll(InitialDynastiesSQL, "dynasties", dynastyTable)
	poetryTypesSQL := strings.ReplaceAll(InitialPoetryTypesSQL, "poetry_types", poetryTypeTable)

	if lang == LangHant {
		var err error
		dynastiesSQL, err = convertSQLToTraditional(dynastiesSQL)
		if err != nil {
			return fmt.Errorf("failed to convert dynasties SQL: %w", err)
		}
		poetryTypesSQL, err = convertSQLToTraditional(poetryTypesSQL)
		if err != nil {
			return fmt.Errorf("failed to convert poetry types SQL: %w", err)
		}
	}

	// Insert dynasties
	if err := db.Exec(dynastiesSQL).Error; err != nil {
		return fmt.Errorf("failed to insert dynasties: %w", err)
	}

	// Insert poetry types
	if err := db.Exec(poetryTypesSQL).Error; err != nil {
		return fmt.Errorf("failed to insert poetry types: %w", err)
	}

	return nil
}

// convertSQLToTraditional converts Chinese characters in SQL string to traditional
// Preserves SQL syntax and only converts Chinese text within quotes
func convertSQLToTraditional(sql string) (string, error) {
	// Split by single quotes to find string literals
	parts := strings.Split(sql, "'")

	for i := range parts {
		// Only convert odd-indexed parts (inside quotes)
		if i%2 == 1 {
			converted, err := classifier.ToTraditional(parts[i])
			if err != nil {
				return "", err
			}
			parts[i] = converted
		}
	}

	return strings.Join(parts, "'"), nil
}

// GetSchemaVersion returns the current schema version
func (db *DB) GetSchemaVersion() (int, error) {
	var version int
	err := db.Raw(`SELECT value FROM metadata WHERE key = ?`, "schema_version").Scan(&version).Error
	if err == gorm.ErrRecordNotFound {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return version, nil
}

// Close closes the database connection
func (db *DB) Close() error {
	sqlDB, err := db.DB.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}
