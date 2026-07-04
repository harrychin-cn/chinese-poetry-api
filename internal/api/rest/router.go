package rest

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/palemoky/chinese-poetry-api/internal/api/middleware"
	"github.com/palemoky/chinese-poetry-api/internal/api/rest/handler"
	"github.com/palemoky/chinese-poetry-api/internal/config"
	"github.com/palemoky/chinese-poetry-api/internal/database"
)

// SetupRouter sets up the Gin router with all routes
func SetupRouter(cfg *config.Config, db *database.DB, repo *database.Repository) *gin.Engine {
	// Set Gin mode
	gin.SetMode(cfg.Server.Mode)

	router := gin.New()
	router.Use(gin.Logger())
	router.Use(gin.Recovery())

	// CORS middleware
	router.Use(middleware.CORS())
	router.Use(middleware.RequestAudit(repo))

	router.GET("/console", handler.ConsolePage)
	router.GET("/console-placeholder-bg.png", handler.ConsolePlaceholderImage)
	router.StaticFS(handler.MediaPublicBasePath(cfg.Image), http.Dir(handler.MediaStorageDir(cfg.Image)))
	router.GET("/docs", handler.DocsPage)
	router.GET("/pricing", handler.PricingPage)
	router.GET("/u/:handle", handler.UserPage)
	router.GET("/users/:handle", handler.UserPage)
	router.GET("/openapi.yaml", handler.OpenAPIYAML)

	if cfg.Abuse.Enabled {
		router.Use(middleware.AbuseBlocklist(repo))
		if cfg.Abuse.AutoBlockEnabled {
			detector := middleware.NewAbuseDetector(
				repo,
				cfg.Abuse.FailureThreshold,
				time.Duration(cfg.Abuse.WindowSeconds)*time.Second,
				time.Duration(cfg.Abuse.BlockMinutes)*time.Minute,
			)
			router.Use(detector.Middleware())
		}
	}

	// Rate limiting middleware
	var apiKeyLimiters []gin.HandlerFunc
	if cfg.RateLimit.Enabled {
		rateLimiter := middleware.NewRateLimiter(cfg.RateLimit.RequestsPerSecond, cfg.RateLimit.Burst)
		router.Use(rateLimiter.Middleware())

		apiKeyRateLimiter := middleware.NewRateLimiter(cfg.RateLimit.APIKeyRequestsPerSecond, cfg.RateLimit.APIKeyBurst)
		apiKeyLimiters = append(apiKeyLimiters, apiKeyRateLimiter.APIKeyTokenMiddleware())
	}

	// API v1 routes
	v1 := router.Group("/api/v1")
	{
		withAPIKey := func(auth gin.HandlerFunc, h gin.HandlerFunc) []gin.HandlerFunc {
			handlers := make([]gin.HandlerFunc, 0, len(apiKeyLimiters)+2)
			handlers = append(handlers, apiKeyLimiters...)
			handlers = append(handlers, auth, h)
			return handlers
		}

		// Health check
		v1.GET("/health", handler.HealthHandler(db))

		// Statistics
		v1.GET("/stats", handler.StatsHandler(repo))

		// Poem routes
		poemHandler := handler.NewPoemHandler(repo)
		v1.GET("/poems", poemHandler.ListPoems)
		if cfg.APIAuth.Enabled {
			v1.GET("/poems/query", withAPIKey(middleware.APIKeyAuthWithRecharge(repo, cfg.Qanlo.RechargeURL), poemHandler.QueryPoems)...)
			v1.GET("/poems/search/fulltext", withAPIKey(middleware.APIKeyAuthWithRecharge(repo, cfg.Qanlo.RechargeURL), poemHandler.SearchPoemsFTS)...)
		} else {
			v1.GET("/poems/query", poemHandler.QueryPoems)
			v1.GET("/poems/search/fulltext", poemHandler.SearchPoemsFTS)
		}
		v1.GET("/poems/random", poemHandler.RandomPoem)
		v1.GET("/poems/search", poemHandler.SearchPoems)

		tagHandler := handler.NewTagHandler(repo)
		v1.GET("/tags", tagHandler.ListTags)

		knowledgeHandler := handler.NewKnowledgeHandler(repo)
		v1.GET("/knowledge/scenarios", knowledgeHandler.ListScenarios)
		if cfg.APIAuth.Enabled {
			v1.GET("/knowledge/recall", withAPIKey(middleware.APIKeyAuthWithRecharge(repo, cfg.Qanlo.RechargeURL), knowledgeHandler.Recall)...)
			v1.POST("/knowledge/batch", withAPIKey(middleware.APIKeyAuthWithRecharge(repo, cfg.Qanlo.RechargeURL), knowledgeHandler.BatchRecall)...)
		} else {
			v1.GET("/knowledge/recall", knowledgeHandler.Recall)
			v1.POST("/knowledge/batch", knowledgeHandler.BatchRecall)
		}

		imageHandler := handler.NewImageHandler(repo, cfg.Image)
		if cfg.APIAuth.Enabled {
			v1.POST("/images/generate", withAPIKey(middleware.APIKeyAuthNoUsage(repo), imageHandler.Generate)...)
		} else {
			v1.POST("/images/generate", imageHandler.Generate)
		}

		// Author routes
		authorHandler := handler.NewAuthorHandler(repo)
		v1.GET("/authors", authorHandler.ListAuthors)
		v1.GET("/authors/:id", authorHandler.GetAuthor)

		// Dynasty routes
		dynastyHandler := handler.NewDynastyHandler(repo)
		v1.GET("/dynasties", dynastyHandler.ListDynasties)
		v1.GET("/dynasties/:id", dynastyHandler.GetDynasty)

		// Poetry type routes
		poetryTypeHandler := handler.NewPoetryTypeHandler(repo)
		v1.GET("/types", poetryTypeHandler.ListPoetryTypes)
		v1.GET("/types/:id", poetryTypeHandler.GetPoetryType)

		// Client commercial entrypoint:
		// create local API key -> bind/recharge via Qanlo -> use enhanced API.
		apiKeyHandler := handler.NewAPIKeyHandler(repo, cfg.APIAuth)
		v1.POST("/keys", apiKeyHandler.CreateClientAPIKey)
		v1.GET("/keys/current", withAPIKey(middleware.APIKeyAuthNoUsage(repo), apiKeyHandler.GetCurrentAPIKey)...)

		accountHandler := handler.NewAccountHandler(repo)
		v1.GET("/account", withAPIKey(middleware.APIKeyAuthNoUsage(repo), accountHandler.Current)...)
		v1.PATCH("/account", withAPIKey(middleware.APIKeyAuthNoUsage(repo), accountHandler.Update)...)

		billingHandler := handler.NewBillingHandler(repo, cfg.Qanlo)
		v1.POST("/billing/qanlo/provision", withAPIKey(middleware.APIKeyAuthNoUsage(repo), billingHandler.ProvisionQanlo)...)
		v1.POST("/billing/qanlo/recharge-session", withAPIKey(middleware.APIKeyAuthNoUsage(repo), billingHandler.CreateQanloRechargeSession)...)
		v1.GET("/billing/qanlo/callback", billingHandler.QanloCallback)
		v1.GET("/billing/status", withAPIKey(middleware.APIKeyAuthNoUsage(repo), billingHandler.BillingStatus)...)

		usageHandler := handler.NewUsageHandler(repo)
		v1.GET("/usage/daily", withAPIKey(middleware.APIKeyAuthNoUsage(repo), usageHandler.ClientDaily)...)
		v1.GET("/usage/endpoints", withAPIKey(middleware.APIKeyAuthNoUsage(repo), usageHandler.ClientEndpoints)...)
		v1.GET("/usage/queries", withAPIKey(middleware.APIKeyAuthNoUsage(repo), usageHandler.ClientQueries)...)

		feedbackHandler := handler.NewFeedbackHandler(repo)
		v1.POST("/feedback", withAPIKey(middleware.APIKeyAuthNoUsage(repo), feedbackHandler.Create)...)

		workHandler := handler.NewWorkHandler(repo)
		v1.GET("/public/works/:code", workHandler.PublicGet)
		v1.GET("/public/users/:handle", accountHandler.PublicProfile)
		v1.GET("/public/users/:handle/works", accountHandler.PublicWorks)
		v1.POST("/works", withAPIKey(middleware.APIKeyAuthNoUsage(repo), workHandler.Create)...)
		v1.GET("/works", withAPIKey(middleware.APIKeyAuthNoUsage(repo), workHandler.List)...)
		reverseCreationHandler := handler.NewReverseCreationHandler(repo)
		v1.POST("/works/reverse-create", withAPIKey(middleware.APIKeyAuthNoUsage(repo), reverseCreationHandler.Create)...)
		v1.GET("/works/reverse-jobs", withAPIKey(middleware.APIKeyAuthNoUsage(repo), reverseCreationHandler.ListJobs)...)
		workImageHandler := handler.NewWorkImageHandler(repo, cfg.Image)
		workAudioHandler := handler.NewWorkAudioHandler(repo, cfg.Audio, cfg.Image)
		v1.GET("/works/:id/media-assets", withAPIKey(middleware.APIKeyAuthNoUsage(repo), workImageHandler.ListMediaAssets)...)
		v1.GET("/works/:id/image-jobs", withAPIKey(middleware.APIKeyAuthNoUsage(repo), workImageHandler.ListImageJobs)...)
		v1.GET("/works/:id/audio-jobs", withAPIKey(middleware.APIKeyAuthNoUsage(repo), workAudioHandler.ListAudioJobs)...)
		v1.GET("/works/:id/music-jobs", withAPIKey(middleware.APIKeyAuthNoUsage(repo), workAudioHandler.ListMusicJobs)...)
		v1.POST("/works/:id/images/generate", withAPIKey(middleware.APIKeyAuthNoUsage(repo), workImageHandler.Generate)...)
		v1.POST("/works/:id/audio/generate", withAPIKey(middleware.APIKeyAuthNoUsage(repo), workAudioHandler.GenerateAudio)...)
		v1.POST("/works/:id/music/generate", withAPIKey(middleware.APIKeyAuthNoUsage(repo), workAudioHandler.GenerateMusic)...)
		v1.GET("/works/:id/versions", withAPIKey(middleware.APIKeyAuthNoUsage(repo), workHandler.Versions)...)
		v1.GET("/works/:id/license-acceptances", withAPIKey(middleware.APIKeyAuthNoUsage(repo), workHandler.LicenseAcceptances)...)
		v1.GET("/works/:id/plagiarism-report", withAPIKey(middleware.APIKeyAuthNoUsage(repo), workHandler.PlagiarismReport)...)
		v1.POST("/works/:id/publish", withAPIKey(middleware.APIKeyAuthNoUsage(repo), workHandler.Publish)...)
		v1.GET("/works/:id", withAPIKey(middleware.APIKeyAuthNoUsage(repo), workHandler.Get)...)
		v1.PATCH("/works/:id", withAPIKey(middleware.APIKeyAuthNoUsage(repo), workHandler.Update)...)

		// Admin routes for commercial API key management
		admin := v1.Group("/admin", middleware.AdminAuth(cfg.APIAuth.AdminToken))
		abuseHandler := handler.NewAbuseHandler(repo)
		admin.POST("/api-keys", apiKeyHandler.CreateAPIKey)
		admin.GET("/api-keys", apiKeyHandler.ListAPIKeys)
		admin.PATCH("/api-keys/:id", apiKeyHandler.UpdateAPIKey)
		admin.DELETE("/api-keys/:id", apiKeyHandler.RevokeAPIKey)
		admin.GET("/abuse/blocks", abuseHandler.ListBlocks)
		admin.POST("/abuse/blocks", abuseHandler.CreateBlock)
		admin.PATCH("/abuse/blocks/:id", abuseHandler.UpdateBlock)
		admin.POST("/search/rebuild", poemHandler.RebuildSearchIndex)
		admin.POST("/tags", tagHandler.UpsertTag)
		admin.POST("/poems/:id/tags", tagHandler.AssignPoemTags)
		admin.GET("/usage/daily", usageHandler.AdminDaily)
		admin.GET("/usage/endpoints", usageHandler.AdminEndpoints)
		admin.GET("/usage/queries", usageHandler.AdminQueries)
		admin.GET("/feedback", feedbackHandler.List)
		admin.PATCH("/feedback/:id", feedbackHandler.Update)

		plagiarismAdminHandler := handler.NewPlagiarismAdminHandler(repo)
		admin.GET("/plagiarism/review-queue", plagiarismAdminHandler.ListReviewQueue)
		admin.POST("/plagiarism/review-queue/:id/approve", plagiarismAdminHandler.ApproveReviewQueueItem)
		admin.POST("/plagiarism/review-queue/:id/reject", plagiarismAdminHandler.RejectReviewQueueItem)
		admin.GET("/plagiarism/corpus-sources", plagiarismAdminHandler.ListCorpusSources)
		admin.POST("/plagiarism/corpus-sources", plagiarismAdminHandler.CreateCorpusSource)

		enrichmentHandler := handler.NewEnrichmentHandler(repo)
		admin.POST("/enrichment/jobs", enrichmentHandler.CreateJob)
		admin.GET("/enrichment/jobs", enrichmentHandler.ListJobs)
		admin.GET("/enrichment/runs/:run_id/summary", enrichmentHandler.RunSummary)
		admin.POST("/enrichment/review-items", enrichmentHandler.CreateReviewItem)
		admin.GET("/enrichment/review-items", enrichmentHandler.ListReviewItems)
		admin.POST("/enrichment/review-items/:id/accept", enrichmentHandler.AcceptReviewItem)
		admin.PATCH("/enrichment/review-items/:id", enrichmentHandler.CorrectReviewItem)
		admin.POST("/enrichment/review-items/:id/reject", enrichmentHandler.RejectReviewItem)
	}

	return router
}
