package packs

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/9Ashwin/sticker-cli/internal/library"
)

func TestRepairClearsOnlyCorruptState(t *testing.T) {
	fixture := newInstallFixture(t)
	home := filepath.Join(t.TempDir(), "library")
	if _, err := Install(context.Background(), InstallOptions{Home: home, Source: fixture.root, PackID: "curated"}); err != nil {
		t.Fatal(err)
	}

	statePath := filepath.Join(home, ".sticker", "packs", "curated.json")
	stateBytes, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	var state installedState
	if err := json.Unmarshal(stateBytes, &state); err != nil {
		t.Fatal(err)
	}
	state.Revision = strings.Repeat("0", 64)
	corruptBytes, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	writeFixtureFile(t, statePath, corruptBytes)

	planned, err := Repair(context.Background(), RepairOptions{Home: home, PackID: "curated", DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if !planned.Repaired || planned.RetainedBytes != 0 || planned.Committed || !planned.DryRun {
		t.Fatalf("unexpected repair dry-run: %+v", planned)
	}
	if _, err := os.Stat(statePath); err != nil {
		t.Fatalf("dry-run removed corrupt state: %v", err)
	}

	result, err := Repair(context.Background(), RepairOptions{Home: home, PackID: "curated"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Repaired || result.RetainedBytes != 0 || !result.Committed || result.DryRun {
		t.Fatalf("unexpected repair result: %+v", result)
	}
	if _, err := os.Stat(statePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("corrupt state remains after repair: %v", err)
	}
	root, err := library.New(home)
	if err != nil {
		t.Fatal(err)
	}
	if err := root.VerifyItem(context.Background(), fixture.item); err != nil {
		t.Fatalf("repair damaged the original: %v", err)
	}

	installed, err := Install(context.Background(), InstallOptions{Home: home, Source: fixture.root, PackID: "curated"})
	if err != nil {
		t.Fatal(err)
	}
	if installed.Added != 0 || installed.Reused != 1 || installed.DownloadBytes != 0 {
		t.Fatalf("reinstall did not reuse retained original: %+v", installed)
	}
}

func TestRepairLeavesValidStateUntouched(t *testing.T) {
	fixture := newInstallFixture(t)
	home := filepath.Join(t.TempDir(), "library")
	if _, err := Install(context.Background(), InstallOptions{Home: home, Source: fixture.root, PackID: "curated"}); err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(home, ".sticker", "packs", "curated.json")
	before, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}

	result, err := Repair(context.Background(), RepairOptions{Home: home, PackID: "curated"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Repaired || result.RetainedBytes != 0 || result.Committed || result.DryRun {
		t.Fatalf("valid state was unexpectedly repaired: %+v", result)
	}
	after, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("valid installed state changed during repair")
	}
}
