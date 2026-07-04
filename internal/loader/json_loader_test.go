package loader

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAllKeepsTangSongFileDynasties(t *testing.T) {
	root := t.TempDir()
	loaderDir := filepath.Join(root, "loader")
	dataDir := filepath.Join(root, "全唐诗")
	if err := os.MkdirAll(loaderDir, 0o755); err != nil {
		t.Fatalf("mkdir loader dir: %v", err)
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("mkdir data dir: %v", err)
	}

	config := `{
		"cp_path": "./",
		"datasets": {
			"tangsong": {
				"name": "全唐诗全宋诗",
				"id": 3,
				"path": "全唐诗/",
				"tag": "paragraphs"
			}
		}
	}`
	if err := os.WriteFile(filepath.Join(loaderDir, "datas.json"), []byte(config), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	tangJSON := `[{"title":"静夜思","author":"李白","paragraphs":["床前明月光。"]}]`
	if err := os.WriteFile(filepath.Join(dataDir, "poet.tang.0.json"), []byte(tangJSON), 0o644); err != nil {
		t.Fatalf("write tang json: %v", err)
	}

	songJSON := `[{"title":"示儿","author":"陆游","paragraphs":["死去元知万事空。"]}]`
	if err := os.WriteFile(filepath.Join(dataDir, "poet.song.0.json"), []byte(songJSON), 0o644); err != nil {
		t.Fatalf("write song json: %v", err)
	}

	jsonLoader, err := NewJSONLoader(filepath.Join(loaderDir, "datas.json"))
	if err != nil {
		t.Fatalf("new loader: %v", err)
	}
	poems, err := jsonLoader.LoadAll()
	if err != nil {
		t.Fatalf("load all: %v", err)
	}

	got := make(map[string]string, len(poems))
	for _, poem := range poems {
		got[poem.Title] = poem.Dynasty
	}

	if got["静夜思"] != "唐" {
		t.Fatalf("静夜思 dynasty = %q, want 唐", got["静夜思"])
	}
	if got["示儿"] != "宋" {
		t.Fatalf("示儿 dynasty = %q, want 宋", got["示儿"])
	}
}

func TestInferDynastyForTangSongFiles(t *testing.T) {
	tests := []struct {
		name     string
		dataset  string
		fileName string
		fallback string
		want     string
	}{
		{name: "song poem file", dataset: "tangsong", fileName: "poet.song.123000.json", fallback: "唐", want: "宋"},
		{name: "tang poem file", dataset: "tangsong", fileName: "poet.tang.123000.json", fallback: "宋", want: "唐"},
		{name: "other tangsong file", dataset: "tangsong", fileName: "other.json", fallback: "唐", want: "唐"},
		{name: "unmixed dataset", dataset: "songci", fileName: "poet.song.0.json", fallback: "宋", want: "宋"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := inferDynastyForFile(tt.dataset, tt.fileName, tt.fallback); got != tt.want {
				t.Fatalf("inferDynastyForFile() = %q, want %q", got, tt.want)
			}
		})
	}
}
