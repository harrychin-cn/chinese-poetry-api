package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// OpenAPIYAML returns the built-in OpenAPI 3.0 specification for API clients and docs tools.
func OpenAPIYAML(c *gin.Context) {
	c.Data(http.StatusOK, "application/yaml; charset=utf-8", []byte(openAPIYAML))
}

const openAPIYAML = `openapi: 3.0.3
info:
  title: AI Chinese Poetry Knowledge API
  version: 1.1.0
  description: |
    Commercial API for Chinese poetry query, full-text search, tag retrieval,
    AI knowledge recall, QanloAPI billing, usage analytics, feedback, abuse control,
    and AI data-enrichment review operations.
servers:
  - url: http://localhost:1279
    description: Local development server
  - url: https://your-domain.com
    description: Production server placeholder
tags:
  - name: Documentation
  - name: Health
  - name: Poetry
  - name: Knowledge
  - name: Images
  - name: Client Keys
  - name: Billing
  - name: Usage
  - name: Feedback
  - name: Admin
  - name: Enrichment
security: []
components:
  securitySchemes:
    ApiKeyAuth:
      type: apiKey
      in: header
      name: X-API-Key
      description: Client API key. Use it for protected query, knowledge, billing status, usage, and feedback endpoints.
    AdminToken:
      type: apiKey
      in: header
      name: X-Admin-Token
      description: Admin token configured by API_ADMIN_TOKEN.
  parameters:
    IDParam:
      name: id
      in: path
      required: true
      schema:
        type: string
    RunIDParam:
      name: run_id
      in: path
      required: true
      schema:
        type: string
    DaysParam:
      name: days
      in: query
      required: false
      schema:
        type: integer
        default: 30
    LimitParam:
      name: limit
      in: query
      required: false
      schema:
        type: integer
        default: 20
  requestBodies:
    JSONBody:
      required: false
      content:
        application/json:
          schema:
            type: object
            additionalProperties: true
  schemas:
    GenericResponse:
      type: object
      additionalProperties: true
    ErrorResponse:
      type: object
      properties:
        error:
          type: string
        message:
          type: string
        recharge_url:
          type: string
      additionalProperties: true
  responses:
    OK:
      description: OK
      content:
        application/json:
          schema:
            $ref: "#/components/schemas/GenericResponse"
    Accepted:
      description: Accepted
      content:
        application/json:
          schema:
            $ref: "#/components/schemas/GenericResponse"
    BadRequest:
      description: Bad request
      content:
        application/json:
          schema:
            $ref: "#/components/schemas/ErrorResponse"
    Unauthorized:
      description: Missing or invalid API key/admin token
      content:
        application/json:
          schema:
            $ref: "#/components/schemas/ErrorResponse"
    Forbidden:
      description: Blocked, disabled, over quota, or not allowed
      content:
        application/json:
          schema:
            $ref: "#/components/schemas/ErrorResponse"
paths:
  /openapi.yaml:
    get:
      tags:
        - Documentation
      summary: Download this OpenAPI specification
      operationId: getOpenAPIYAML
      security: []
      responses:
        "200":
          description: OpenAPI YAML document
          content:
            application/yaml:
              schema:
                type: string
  /api/v1/health:
    get:
      tags:
        - Health
      summary: Health check
      operationId: getHealth
      security: []
      responses:
        "200":
          $ref: "#/components/responses/OK"
  /api/v1/stats:
    get:
      tags:
        - Health
      summary: Basic dataset statistics
      operationId: getStats
      security: []
      responses:
        "200":
          $ref: "#/components/responses/OK"
  /api/v1/poems:
    get:
      tags:
        - Poetry
      summary: List poems
      operationId: listPoems
      security: []
      responses:
        "200":
          $ref: "#/components/responses/OK"
  /api/v1/poems/query:
    get:
      tags:
        - Poetry
      summary: Composite query by author, dynasty, type, tags, length, and keyword
      operationId: queryPoems
      security:
        - ApiKeyAuth: []
      parameters:
        - name: author
          in: query
          schema:
            type: string
        - name: dynasty
          in: query
          schema:
            type: string
        - name: tag
          in: query
          schema:
            type: string
        - name: q
          in: query
          schema:
            type: string
        - $ref: "#/components/parameters/LimitParam"
      responses:
        "200":
          $ref: "#/components/responses/OK"
        "401":
          $ref: "#/components/responses/Unauthorized"
        "403":
          $ref: "#/components/responses/Forbidden"
  /api/v1/poems/search/fulltext:
    get:
      tags:
        - Poetry
      summary: Full-text search poems by title, author, and content
      operationId: searchPoemsFullText
      security:
        - ApiKeyAuth: []
      parameters:
        - name: q
          in: query
          required: true
          schema:
            type: string
        - $ref: "#/components/parameters/LimitParam"
      responses:
        "200":
          $ref: "#/components/responses/OK"
        "401":
          $ref: "#/components/responses/Unauthorized"
  /api/v1/poems/random:
    get:
      tags:
        - Poetry
      summary: Get a random poem
      operationId: randomPoem
      security: []
      responses:
        "200":
          $ref: "#/components/responses/OK"
  /api/v1/poems/search:
    get:
      tags:
        - Poetry
      summary: Legacy keyword search
      operationId: searchPoemsLegacy
      security: []
      parameters:
        - name: q
          in: query
          schema:
            type: string
      responses:
        "200":
          $ref: "#/components/responses/OK"
  /api/v1/tags:
    get:
      tags:
        - Poetry
      summary: List tags
      operationId: listTags
      security: []
      responses:
        "200":
          $ref: "#/components/responses/OK"
  /api/v1/knowledge/scenarios:
    get:
      tags:
        - Knowledge
      summary: List built-in knowledge recall scenarios
      operationId: listKnowledgeScenarios
      security: []
      responses:
        "200":
          $ref: "#/components/responses/OK"
  /api/v1/knowledge/recall:
    get:
      tags:
        - Knowledge
      summary: Recall poem knowledge by natural-language intent
      operationId: recallKnowledge
      security:
        - ApiKeyAuth: []
      parameters:
        - name: q
          in: query
          required: true
          schema:
            type: string
        - name: page_size
          in: query
          schema:
            type: integer
            default: 5
      responses:
        "200":
          $ref: "#/components/responses/OK"
        "401":
          $ref: "#/components/responses/Unauthorized"
  /api/v1/knowledge/batch:
    post:
      tags:
        - Knowledge
      summary: Batch recall poem knowledge
      operationId: batchRecallKnowledge
      security:
        - ApiKeyAuth: []
      requestBody:
        $ref: "#/components/requestBodies/JSONBody"
      responses:
        "200":
          $ref: "#/components/responses/OK"
        "401":
          $ref: "#/components/responses/Unauthorized"
  /api/v1/images/generate:
    post:
      tags:
        - Images
      summary: Generate a poetry mood image in the console
      operationId: generatePoetryImage
      description: Requires X-API-Key. Public clients provide their own Qanlo image key as image_api_key in the request body; the server only proxies that key for this request and does not store it.
      security:
        - ApiKeyAuth: []
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              required:
                - prompt
                - image_api_key
              properties:
                prompt:
                  type: string
                size:
                  type: string
                image_api_key:
                  type: string
                  format: password
                  enum: ["1024x1024", "1024x1536", "1536x1024", "2048x1152"]
      responses:
        "200":
          $ref: "#/components/responses/OK"
        "401":
          $ref: "#/components/responses/Unauthorized"
        "429":
          $ref: "#/components/responses/Forbidden"
        "503":
          $ref: "#/components/responses/Forbidden"
  /api/v1/authors:
    get:
      tags:
        - Poetry
      summary: List authors
      operationId: listAuthors
      security: []
      responses:
        "200":
          $ref: "#/components/responses/OK"
  /api/v1/authors/{id}:
    get:
      tags:
        - Poetry
      summary: Get author detail
      operationId: getAuthor
      security: []
      parameters:
        - $ref: "#/components/parameters/IDParam"
      responses:
        "200":
          $ref: "#/components/responses/OK"
  /api/v1/dynasties:
    get:
      tags:
        - Poetry
      summary: List dynasties
      operationId: listDynasties
      security: []
      responses:
        "200":
          $ref: "#/components/responses/OK"
  /api/v1/dynasties/{id}:
    get:
      tags:
        - Poetry
      summary: Get dynasty detail
      operationId: getDynasty
      security: []
      parameters:
        - $ref: "#/components/parameters/IDParam"
      responses:
        "200":
          $ref: "#/components/responses/OK"
  /api/v1/types:
    get:
      tags:
        - Poetry
      summary: List poetry types
      operationId: listPoetryTypes
      security: []
      responses:
        "200":
          $ref: "#/components/responses/OK"
  /api/v1/types/{id}:
    get:
      tags:
        - Poetry
      summary: Get poetry type detail
      operationId: getPoetryType
      security: []
      parameters:
        - $ref: "#/components/parameters/IDParam"
      responses:
        "200":
          $ref: "#/components/responses/OK"
  /api/v1/keys:
    post:
      tags:
        - Client Keys
      summary: Public client API key creation is disabled
      operationId: createClientAPIKey
      security: []
      requestBody:
        $ref: "#/components/requestBodies/JSONBody"
      responses:
        "403":
          $ref: "#/components/responses/Forbidden"
  /api/v1/keys/current:
    get:
      tags:
        - Client Keys
      summary: Get current client API key status and usage
      operationId: getCurrentAPIKey
      security:
        - ApiKeyAuth: []
      responses:
        "200":
          $ref: "#/components/responses/OK"
        "401":
          $ref: "#/components/responses/Unauthorized"
  /api/v1/billing/qanlo/provision:
    post:
      tags:
        - Billing
      summary: Create or refresh Qanlo Agent Key binding URL
      operationId: provisionQanlo
      security:
        - ApiKeyAuth: []
      responses:
        "200":
          $ref: "#/components/responses/OK"
        "401":
          $ref: "#/components/responses/Unauthorized"
  /api/v1/billing/qanlo/recharge-session:
    post:
      tags:
        - Billing
      summary: Create Qanlo compact recharge session URL
      operationId: createQanloRechargeSession
      security:
        - ApiKeyAuth: []
      responses:
        "200":
          $ref: "#/components/responses/OK"
        "401":
          $ref: "#/components/responses/Unauthorized"
  /api/v1/billing/qanlo/callback:
    get:
      tags:
        - Billing
      summary: Qanlo redirect callback
      operationId: qanloCallback
      security: []
      responses:
        "200":
          $ref: "#/components/responses/OK"
        "400":
          $ref: "#/components/responses/BadRequest"
  /api/v1/billing/status:
    get:
      tags:
        - Billing
      summary: Get billing, recharge, quota, and Qanlo binding status
      operationId: getBillingStatus
      security:
        - ApiKeyAuth: []
      responses:
        "200":
          $ref: "#/components/responses/OK"
        "401":
          $ref: "#/components/responses/Unauthorized"
  /api/v1/usage/daily:
    get:
      tags:
        - Usage
      summary: Get current API key daily usage trend
      operationId: getClientDailyUsage
      security:
        - ApiKeyAuth: []
      parameters:
        - $ref: "#/components/parameters/DaysParam"
      responses:
        "200":
          $ref: "#/components/responses/OK"
  /api/v1/usage/endpoints:
    get:
      tags:
        - Usage
      summary: Get current API key endpoint usage, errors, and latency
      operationId: getClientEndpointUsage
      security:
        - ApiKeyAuth: []
      parameters:
        - $ref: "#/components/parameters/DaysParam"
        - $ref: "#/components/parameters/LimitParam"
      responses:
        "200":
          $ref: "#/components/responses/OK"
  /api/v1/usage/queries:
    get:
      tags:
        - Usage
      summary: Get current API key popular query summaries
      operationId: getClientQueryUsage
      security:
        - ApiKeyAuth: []
      parameters:
        - $ref: "#/components/parameters/DaysParam"
        - $ref: "#/components/parameters/LimitParam"
      responses:
        "200":
          $ref: "#/components/responses/OK"
  /api/v1/feedback:
    post:
      tags:
        - Feedback
      summary: Submit customer feedback
      operationId: createFeedback
      security:
        - ApiKeyAuth: []
      requestBody:
        $ref: "#/components/requestBodies/JSONBody"
      responses:
        "200":
          $ref: "#/components/responses/OK"
  /api/v1/admin/api-keys:
    post:
      tags:
        - Admin
      summary: Admin create API key
      operationId: adminCreateAPIKey
      security:
        - AdminToken: []
      requestBody:
        $ref: "#/components/requestBodies/JSONBody"
      responses:
        "200":
          $ref: "#/components/responses/OK"
        "401":
          $ref: "#/components/responses/Unauthorized"
    get:
      tags:
        - Admin
      summary: Admin list API keys
      operationId: adminListAPIKeys
      security:
        - AdminToken: []
      responses:
        "200":
          $ref: "#/components/responses/OK"
        "401":
          $ref: "#/components/responses/Unauthorized"
  /api/v1/admin/api-keys/{id}:
    patch:
      tags:
        - Admin
      summary: Admin update API key status, tier, quota, and notes
      operationId: adminUpdateAPIKey
      security:
        - AdminToken: []
      parameters:
        - $ref: "#/components/parameters/IDParam"
      requestBody:
        $ref: "#/components/requestBodies/JSONBody"
      responses:
        "200":
          $ref: "#/components/responses/OK"
    delete:
      tags:
        - Admin
      summary: Admin revoke API key
      operationId: adminRevokeAPIKey
      security:
        - AdminToken: []
      parameters:
        - $ref: "#/components/parameters/IDParam"
      responses:
        "200":
          $ref: "#/components/responses/OK"
  /api/v1/admin/abuse/blocks:
    get:
      tags:
        - Admin
      summary: Admin list abuse block rules
      operationId: adminListAbuseBlocks
      security:
        - AdminToken: []
      responses:
        "200":
          $ref: "#/components/responses/OK"
    post:
      tags:
        - Admin
      summary: Admin create abuse block rule
      operationId: adminCreateAbuseBlock
      security:
        - AdminToken: []
      requestBody:
        $ref: "#/components/requestBodies/JSONBody"
      responses:
        "200":
          $ref: "#/components/responses/OK"
  /api/v1/admin/abuse/blocks/{id}:
    patch:
      tags:
        - Admin
      summary: Admin update abuse block rule
      operationId: adminUpdateAbuseBlock
      security:
        - AdminToken: []
      parameters:
        - $ref: "#/components/parameters/IDParam"
      requestBody:
        $ref: "#/components/requestBodies/JSONBody"
      responses:
        "200":
          $ref: "#/components/responses/OK"
  /api/v1/admin/search/rebuild:
    post:
      tags:
        - Admin
      summary: Admin rebuild full-text search index
      operationId: adminRebuildSearchIndex
      security:
        - AdminToken: []
      responses:
        "200":
          $ref: "#/components/responses/OK"
  /api/v1/admin/tags:
    post:
      tags:
        - Admin
      summary: Admin upsert tag
      operationId: adminUpsertTag
      security:
        - AdminToken: []
      requestBody:
        $ref: "#/components/requestBodies/JSONBody"
      responses:
        "200":
          $ref: "#/components/responses/OK"
  /api/v1/admin/poems/{id}/tags:
    post:
      tags:
        - Admin
      summary: Admin assign tags to poem
      operationId: adminAssignPoemTags
      security:
        - AdminToken: []
      parameters:
        - $ref: "#/components/parameters/IDParam"
      requestBody:
        $ref: "#/components/requestBodies/JSONBody"
      responses:
        "200":
          $ref: "#/components/responses/OK"
  /api/v1/admin/usage/daily:
    get:
      tags:
        - Admin
      summary: Admin get all-site or key-specific daily usage
      operationId: adminGetDailyUsage
      security:
        - AdminToken: []
      parameters:
        - $ref: "#/components/parameters/DaysParam"
      responses:
        "200":
          $ref: "#/components/responses/OK"
  /api/v1/admin/usage/endpoints:
    get:
      tags:
        - Admin
      summary: Admin get endpoint usage, error rate, and latency
      operationId: adminGetEndpointUsage
      security:
        - AdminToken: []
      parameters:
        - $ref: "#/components/parameters/DaysParam"
        - $ref: "#/components/parameters/LimitParam"
      responses:
        "200":
          $ref: "#/components/responses/OK"
  /api/v1/admin/usage/queries:
    get:
      tags:
        - Admin
      summary: Admin get popular query summaries
      operationId: adminGetQueryUsage
      security:
        - AdminToken: []
      parameters:
        - $ref: "#/components/parameters/DaysParam"
        - $ref: "#/components/parameters/LimitParam"
      responses:
        "200":
          $ref: "#/components/responses/OK"
  /api/v1/admin/feedback:
    get:
      tags:
        - Admin
      summary: Admin list customer feedback
      operationId: adminListFeedback
      security:
        - AdminToken: []
      responses:
        "200":
          $ref: "#/components/responses/OK"
  /api/v1/admin/feedback/{id}:
    patch:
      tags:
        - Admin
      summary: Admin update customer feedback status and notes
      operationId: adminUpdateFeedback
      security:
        - AdminToken: []
      parameters:
        - $ref: "#/components/parameters/IDParam"
      requestBody:
        $ref: "#/components/requestBodies/JSONBody"
      responses:
        "200":
          $ref: "#/components/responses/OK"
  /api/v1/admin/enrichment/jobs:
    post:
      tags:
        - Enrichment
      summary: Admin create enrichment job
      operationId: adminCreateEnrichmentJob
      security:
        - AdminToken: []
      requestBody:
        $ref: "#/components/requestBodies/JSONBody"
      responses:
        "200":
          $ref: "#/components/responses/OK"
    get:
      tags:
        - Enrichment
      summary: Admin list enrichment jobs
      operationId: adminListEnrichmentJobs
      security:
        - AdminToken: []
      responses:
        "200":
          $ref: "#/components/responses/OK"
  /api/v1/admin/enrichment/runs/{run_id}/summary:
    get:
      tags:
        - Enrichment
      summary: Admin get enrichment run summary, pass rate, and reject reasons
      operationId: adminGetEnrichmentRunSummary
      security:
        - AdminToken: []
      parameters:
        - $ref: "#/components/parameters/RunIDParam"
      responses:
        "200":
          $ref: "#/components/responses/OK"
  /api/v1/admin/enrichment/review-items:
    post:
      tags:
        - Enrichment
      summary: Admin create enrichment review item
      operationId: adminCreateEnrichmentReviewItem
      security:
        - AdminToken: []
      requestBody:
        $ref: "#/components/requestBodies/JSONBody"
      responses:
        "200":
          $ref: "#/components/responses/OK"
    get:
      tags:
        - Enrichment
      summary: Admin list enrichment review items
      operationId: adminListEnrichmentReviewItems
      security:
        - AdminToken: []
      parameters:
        - name: status
          in: query
          schema:
            type: string
            enum:
              - pending
              - accepted
              - rejected
        - name: run_id
          in: query
          schema:
            type: string
        - $ref: "#/components/parameters/LimitParam"
      responses:
        "200":
          $ref: "#/components/responses/OK"
  /api/v1/admin/enrichment/review-items/{id}:
    patch:
      tags:
        - Enrichment
      summary: Admin correct enrichment review item candidate JSON
      operationId: adminCorrectEnrichmentReviewItem
      security:
        - AdminToken: []
      parameters:
        - $ref: "#/components/parameters/IDParam"
      requestBody:
        $ref: "#/components/requestBodies/JSONBody"
      responses:
        "200":
          $ref: "#/components/responses/OK"
  /api/v1/admin/enrichment/review-items/{id}/accept:
    post:
      tags:
        - Enrichment
      summary: Admin accept and publish enrichment review item
      operationId: adminAcceptEnrichmentReviewItem
      security:
        - AdminToken: []
      parameters:
        - $ref: "#/components/parameters/IDParam"
      requestBody:
        $ref: "#/components/requestBodies/JSONBody"
      responses:
        "200":
          $ref: "#/components/responses/OK"
  /api/v1/admin/enrichment/review-items/{id}/reject:
    post:
      tags:
        - Enrichment
      summary: Admin reject enrichment review item without publishing
      operationId: adminRejectEnrichmentReviewItem
      security:
        - AdminToken: []
      parameters:
        - $ref: "#/components/parameters/IDParam"
      requestBody:
        $ref: "#/components/requestBodies/JSONBody"
      responses:
        "200":
          $ref: "#/components/responses/OK"
`
