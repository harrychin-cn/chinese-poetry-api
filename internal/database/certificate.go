package database

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

const (
	WorkCertificateStatusIssued = "issued"
	WorkAnchorStatusAnchored    = "local_anchored"
	WorkAnchorNetworkLocal      = "local-ledger"
	WorkSignatureAlgorithm      = "sha256-platform-v1"
)

// WorkCertificate stores the stage-7 public certificate, platform signature,
// and a local blockchain-style anchor summary for a published original work.
type WorkCertificate struct {
	ID                 int64      `gorm:"primaryKey;autoIncrement" json:"id"`
	WorkID             int64      `gorm:"column:work_id;not null" json:"work_id"`
	APIKeyID           int64      `gorm:"column:api_key_id;not null" json:"api_key_id"`
	CertificateCode    string     `gorm:"column:certificate_code;not null" json:"certificate_code"`
	WorkCode           string     `gorm:"column:work_code;not null" json:"work_code"`
	WorkVersion        int        `gorm:"column:work_version;not null" json:"work_version"`
	Title              string     `gorm:"column:title;not null" json:"title"`
	WorkType           string     `gorm:"column:work_type;not null" json:"work_type"`
	ContentHash        string     `gorm:"column:content_hash;not null" json:"content_hash"`
	LicenseType        string     `gorm:"column:license_type;not null" json:"license_type"`
	LicenseVersion     string     `gorm:"column:license_version;not null" json:"license_version"`
	CertificateHash    string     `gorm:"column:certificate_hash;not null" json:"certificate_hash"`
	SignatureAlgorithm string     `gorm:"column:signature_algorithm;not null" json:"signature_algorithm"`
	Signature          string     `gorm:"column:signature;not null" json:"signature"`
	CertificatePayload string     `gorm:"column:certificate_payload;not null" json:"certificate_payload"`
	AnchorNetwork      string     `gorm:"column:anchor_network;not null" json:"anchor_network"`
	AnchorStatus       string     `gorm:"column:anchor_status;not null" json:"anchor_status"`
	AnchorHash         string     `gorm:"column:anchor_hash;not null" json:"anchor_hash"`
	AnchorTxID         string     `gorm:"column:anchor_tx_id;not null" json:"anchor_tx_id"`
	AnchorPayload      string     `gorm:"column:anchor_payload;not null" json:"anchor_payload"`
	Status             string     `gorm:"column:status;not null" json:"status"`
	IssuedAt           time.Time  `gorm:"column:issued_at;not null" json:"issued_at"`
	AnchoredAt         *time.Time `gorm:"column:anchored_at" json:"anchored_at,omitempty"`
	CreatedAt          time.Time  `gorm:"column:created_at" json:"created_at"`
	UpdatedAt          time.Time  `gorm:"column:updated_at" json:"updated_at"`
}

func (WorkCertificate) TableName() string { return "work_certificates" }

type certificatePayload struct {
	SchemaVersion  string `json:"schema_version"`
	Certificate    string `json:"certificate_code"`
	WorkCode       string `json:"work_code"`
	WorkID         int64  `json:"work_id"`
	Title          string `json:"title"`
	WorkType       string `json:"work_type"`
	WorkVersion    int    `json:"work_version"`
	ContentHash    string `json:"content_hash"`
	LicenseType    string `json:"license_type"`
	LicenseVersion string `json:"license_version"`
	PublishedAt    string `json:"published_at"`
	IssuedAt       string `json:"issued_at"`
}

type anchorPayload struct {
	SchemaVersion   string `json:"schema_version"`
	Network         string `json:"network"`
	CertificateCode string `json:"certificate_code"`
	CertificateHash string `json:"certificate_hash"`
	ContentHash     string `json:"content_hash"`
	WorkCode        string `json:"work_code"`
	AnchorHash      string `json:"anchor_hash"`
	AnchoredAt      string `json:"anchored_at"`
}

// IssueWorkCertificate creates or refreshes a certificate for one owned public work.
func (r *Repository) IssueWorkCertificate(apiKeyID, workID int64) (*WorkCertificate, error) {
	if apiKeyID < 1 || workID < 1 {
		return nil, ErrInvalidQueryParam
	}
	var cert WorkCertificate
	err := r.db.Transaction(func(tx *gorm.DB) error {
		var work OriginalWork
		if err := tx.Where("id = ? AND api_key_id = ?", workID, apiKeyID).First(&work).Error; err != nil {
			return err
		}

		var existing WorkCertificate
		if err := tx.Where("work_id = ?", work.ID).First(&existing).Error; err == nil {
			if existing.ContentHash == work.ContentHash && existing.WorkVersion == work.Version {
				cert = existing
				return nil
			}
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		built, err := buildCertificateForWork(work, time.Now().UTC())
		if err != nil {
			return err
		}
		return upsertWorkCertificateTx(tx, built, &cert)
	})
	if err != nil {
		return nil, err
	}
	return &cert, nil
}

// AnchorWorkCertificate ensures the local blockchain-style anchor summary exists.
func (r *Repository) AnchorWorkCertificate(apiKeyID, workID int64) (*WorkCertificate, error) {
	cert, err := r.IssueWorkCertificate(apiKeyID, workID)
	if err != nil {
		return nil, err
	}
	if cert.AnchorStatus == WorkAnchorStatusAnchored && cert.AnchorHash != "" {
		return cert, nil
	}
	return r.IssueWorkCertificate(apiKeyID, workID)
}

// GetWorkCertificate returns an owned work certificate if it has been issued.
func (r *Repository) GetWorkCertificate(apiKeyID, workID int64) (*WorkCertificate, error) {
	if apiKeyID < 1 || workID < 1 {
		return nil, ErrInvalidQueryParam
	}
	var cert WorkCertificate
	if err := r.db.Where("api_key_id = ? AND work_id = ?", apiKeyID, workID).First(&cert).Error; err != nil {
		return nil, err
	}
	return &cert, nil
}

// GetOrIssuePublicWorkCertificate returns a certificate for a public work code,
// creating it lazily when the public work is eligible but not yet certified.
func (r *Repository) GetOrIssuePublicWorkCertificate(code string) (*WorkCertificate, error) {
	code = strings.ToUpper(strings.TrimSpace(code))
	if code == "" {
		return nil, ErrInvalidQueryParam
	}
	var cert WorkCertificate
	err := r.db.Where("work_code = ?", code).First(&cert).Error
	if err == nil {
		return &cert, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	var issued WorkCertificate
	err = r.db.Transaction(func(tx *gorm.DB) error {
		var work OriginalWork
		if err := tx.Where("work_code = ? AND status = ? AND visibility = ?", code, WorkStatusPublished, WorkVisibilityPublic).First(&work).Error; err != nil {
			return err
		}
		built, err := buildCertificateForWork(work, time.Now().UTC())
		if err != nil {
			return err
		}
		return upsertWorkCertificateTx(tx, built, &issued)
	})
	if err != nil {
		return nil, err
	}
	return &issued, nil
}

func upsertWorkCertificateTx(tx *gorm.DB, built WorkCertificate, out *WorkCertificate) error {
	var existing WorkCertificate
	err := tx.Where("work_id = ?", built.WorkID).First(&existing).Error
	if err == nil {
		if existing.ContentHash == built.ContentHash && existing.WorkVersion == built.WorkVersion && existing.CertificateHash == built.CertificateHash {
			*out = existing
			return nil
		}
		built.ID = existing.ID
		built.CreatedAt = existing.CreatedAt
		if err := tx.Save(&built).Error; err != nil {
			return err
		}
		*out = built
		return nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	if err := tx.Create(&built).Error; err != nil {
		return err
	}
	*out = built
	return nil
}

func buildCertificateForWork(work OriginalWork, issuedAt time.Time) (WorkCertificate, error) {
	if work.ID < 1 || work.APIKeyID < 1 || strings.TrimSpace(work.WorkCode) == "" {
		return WorkCertificate{}, ErrInvalidQueryParam
	}
	if work.Status != WorkStatusPublished || work.Visibility != WorkVisibilityPublic {
		return WorkCertificate{}, fmt.Errorf("%w: certificate requires a public published work", ErrInvalidQueryParam)
	}
	if !work.OriginalCommitment || !work.LicenseAccepted {
		return WorkCertificate{}, fmt.Errorf("%w: certificate requires originality and license acceptance", ErrInvalidQueryParam)
	}
	if issuedAt.IsZero() {
		issuedAt = time.Now().UTC()
	}
	issuedAt = issuedAt.UTC().Truncate(time.Second)
	publishedAt := ""
	if work.PublishedAt != nil {
		publishedAt = work.PublishedAt.UTC().Truncate(time.Second).Format(time.RFC3339)
	}
	certificateCode := fmt.Sprintf("CERT-%s-V%d", strings.ToUpper(work.WorkCode), work.Version)
	payload := certificatePayload{
		SchemaVersion:  "work-certificate-v1",
		Certificate:    certificateCode,
		WorkCode:       strings.ToUpper(work.WorkCode),
		WorkID:         work.ID,
		Title:          work.Title,
		WorkType:       work.WorkType,
		WorkVersion:    work.Version,
		ContentHash:    work.ContentHash,
		LicenseType:    work.LicenseType,
		LicenseVersion: work.LicenseVersion,
		PublishedAt:    publishedAt,
		IssuedAt:       issuedAt.Format(time.RFC3339),
	}
	payloadJSON, err := stableJSON(payload)
	if err != nil {
		return WorkCertificate{}, err
	}
	certHash := sha256Hex(payloadJSON)
	signature := sha256Hex([]byte("qanlo-platform-signature-v1\n" + certHash))

	anchorAt := issuedAt
	anchorPreimage := []byte("qanlo-local-anchor-v1\n" + certHash + "\n" + work.ContentHash)
	anchorHash := sha256Hex(anchorPreimage)
	anchor := anchorPayload{
		SchemaVersion:   "work-anchor-v1",
		Network:         WorkAnchorNetworkLocal,
		CertificateCode: certificateCode,
		CertificateHash: certHash,
		ContentHash:     work.ContentHash,
		WorkCode:        strings.ToUpper(work.WorkCode),
		AnchorHash:      anchorHash,
		AnchoredAt:      anchorAt.Format(time.RFC3339),
	}
	anchorJSON, err := stableJSON(anchor)
	if err != nil {
		return WorkCertificate{}, err
	}
	now := issuedAt
	return WorkCertificate{
		WorkID:             work.ID,
		APIKeyID:           work.APIKeyID,
		CertificateCode:    certificateCode,
		WorkCode:           strings.ToUpper(work.WorkCode),
		WorkVersion:        work.Version,
		Title:              work.Title,
		WorkType:           work.WorkType,
		ContentHash:        work.ContentHash,
		LicenseType:        work.LicenseType,
		LicenseVersion:     work.LicenseVersion,
		CertificateHash:    certHash,
		SignatureAlgorithm: WorkSignatureAlgorithm,
		Signature:          signature,
		CertificatePayload: string(payloadJSON),
		AnchorNetwork:      WorkAnchorNetworkLocal,
		AnchorStatus:       WorkAnchorStatusAnchored,
		AnchorHash:         anchorHash,
		AnchorTxID:         "local:" + anchorHash[:24],
		AnchorPayload:      string(anchorJSON),
		Status:             WorkCertificateStatusIssued,
		IssuedAt:           issuedAt,
		AnchoredAt:         &anchorAt,
		CreatedAt:          now,
		UpdatedAt:          now,
	}, nil
}

func stableJSON(value any) ([]byte, error) {
	return json.Marshal(value)
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
