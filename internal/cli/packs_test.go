package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestPackListReadsExplicitLocalSource(t *testing.T) {
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "packs.json"), []byte(`{"schema_version":1,"packs":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	var out, errOut bytes.Buffer
	if code := Run(context.Background(), []string{"--home", home, "packs", "list", "--source", source}, &out, &errOut, "dev", "unknown"); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	var envelope struct {
		OK   bool `json:"ok"`
		Data struct {
			Source string `json:"source"`
			Items  []any  `json:"items"`
			Stale  bool   `json:"stale"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if !envelope.OK || envelope.Data.Source != source || len(envelope.Data.Items) != 0 || envelope.Data.Stale {
		t.Fatalf("unexpected result: %s", out.String())
	}
	if errOut.Len() != 0 {
		t.Fatalf("unexpected stderr: %s", errOut.String())
	}
}
