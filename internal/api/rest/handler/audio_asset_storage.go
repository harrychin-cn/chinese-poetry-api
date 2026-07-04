package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/palemoky/chinese-poetry-api/internal/config"
)

type storedAudioAsset struct {
	URL             string
	StorageProvider string
	StorageKey      string
	FilePath        string
	ByteSize        int64
	ChecksumSHA256  string
}

func storeWorkAudioBytes(cfg config.ImageConfig, apiKeyID, workID int64, folder, ext string, payload []byte) (*storedAudioAsset, error) {
	if len(payload) == 0 {
		return nil, fmt.Errorf("store audio asset: empty payload")
	}
	folder = strings.Trim(strings.ToLower(strings.TrimSpace(folder)), "/")
	if folder == "" {
		folder = "audio"
	}
	ext = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(ext)), ".")
	if ext == "" {
		ext = "mp3"
	}

	sum := sha256.Sum256(payload)
	checksum := hex.EncodeToString(sum[:])
	day := time.Now().UTC().Format("20060102")
	storageKey := path.Join(folder, formatID(apiKeyID), formatID(workID), day, checksum+"."+ext)
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

	return &storedAudioAsset{
		URL:             MediaPublicBasePath(cfg) + "/" + storageKey,
		StorageProvider: "local",
		StorageKey:      storageKey,
		FilePath:        filePath,
		ByteSize:        int64(len(payload)),
		ChecksumSHA256:  checksum,
	}, nil
}
