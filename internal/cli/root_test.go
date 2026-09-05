package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
)

func TestVersion(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := Run(context.Background(), []string{"version"}, &out, &errOut, "v0.1.0", "abc123"); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	var got struct {
		OK   bool `json:"ok"`
		Data struct {
			Version string `json:"version"`
			Commit  string `json:"commit"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.OK || got.Data.Version != "v0.1.0" || got.Data.Commit != "abc123" || errOut.Len() != 0 {
		t.Fatalf("unexpected streams: %s / %s", out.String(), errOut.String())
	}
}

func TestUnknownCommand(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := Run(context.Background(), []string{"missing"}, &out, &errOut, "dev", "unknown"); code == 0 || out.Len() != 0 || !json.Valid(errOut.Bytes()) {
		t.Fatalf("exit %d: %s / %s", code, out.String(), errOut.String())
	}
}
