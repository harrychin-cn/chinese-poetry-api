package handler

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/palemoky/chinese-poetry-api/internal/config"
)

const defaultMediaPublicBasePath = "/media-assets"

type storedImageAsset struct {
	URL             string
	StorageProvider string
	StorageKey      string
	FilePath        string
	ByteSize        int64
	ChecksumSHA256  string
}

// MediaStorageDir returns the disk directory used by the built-in media-assets route.
func MediaStorageDir(cfg config.ImageConfig) string {
	dir := strings.TrimSpace(cfg.StorageDir)
	if dir == "" {
		dir = "data/media-assets"
	}
	return filepath.Clean(dir)
}

// MediaPublicBasePath returns the URL prefix exposed for generated assets.
func MediaPublicBasePath(cfg config.ImageConfig) string {
	base := strings.TrimSpace(cfg.PublicBasePath)
	if base == "" {
		base = defaultMediaPublicBasePath
	}
	if !strings.HasPrefix(base, "/") {
		base = "/" + base
	}
	return strings.TrimRight(base, "/")
}

func storeWorkImageB64Asset(cfg config.ImageConfig, apiKeyID, workID int64, format, b64 string) (*storedImageAsset, error) {
	b64 = strings.TrimSpace(b64)
	if b64 == "" {
		return nil, nil
	}
	if idx := strings.Index(b64, ","); strings.HasPrefix(strings.ToLower(b64), "data:image/") && idx >= 0 {
		b64 = strings.TrimSpace(b64[idx+1:])
	}

	payload, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		payload, err = base64.RawStdEncoding.DecodeString(b64)
	}
	if err != nil {
		return nil, fmt.Errorf("decode image b64: %w", err)
	}
	if len(payload) == 0 {
		return nil, fmt.Errorf("decode image b64: empty payload")
	}

	format = normalizeOutputFormat(format)
	ext := format
	if ext == "jpeg" {
		ext = "jpg"
	}
	sum := sha256.Sum256(payload)
	checksum := hex.EncodeToString(sum[:])
	day := time.Now().UTC().Format("20060102")
	storageKey := path.Join("images", formatID(apiKeyID), formatID(workID), day, checksum+"."+ext)
	filePath := filepath.Join(MediaStorageDir(cfg), filepath.FromSlash(storageKey))
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		return nil, err
	}
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		if err := os.WriteFile(filePath, payload, 0o644); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}

	return &storedImageAsset{
		URL:             MediaPublicBasePath(cfg) + "/" + storageKey,
		StorageProvider: "local",
		StorageKey:      storageKey,
		FilePath:        filePath,
		ByteSize:        int64(len(payload)),
		ChecksumSHA256:  checksum,
	}, nil
}
