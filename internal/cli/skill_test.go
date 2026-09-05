package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestAgentSkillDocumentsDiscoverableCommands(t *testing.T) {
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("could not locate the test source")
	}
	skillPath := filepath.Join(filepath.Dir(sourceFile), "..", "..", "skills", "sticker", "SKILL.md")
	skill, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(skill)
	for _, example := range []string{
		"sticker setup --pack curated",
		"sticker setup --pack all",
		"sticker packs install curated",
		"sticker search",
		"sticker get <id> --preview",
		"sticker favorites add --id <id>",
		"sticker favorites import",
		"sticker favorites collections create work",
		"sticker favorites organize",
		"sticker favorites list --collection work --sort manual",
	} {
		if !strings.Contains(text, example) {
			t.Errorf("skill is missing command example %q", example)
		}
	}
	if !strings.Contains(text, "does not send an image to an external chat") {
		t.Error("skill must describe the local display boundary")
	}

	for _, command := range []string{
		"setup",
		"packs install",
		"get",
		"favorites add",
		"favorites import",
		"favorites collections create",
		"favorites organize",
		"favorites list",
	} {
		var out, errOut bytes.Buffer
		if code := Run(context.Background(), append([]string{"schema"}, strings.Fields(command)...), &out, &errOut, "test", "test"); code != 0 {
			t.Errorf("skill command %q is not discoverable from schema: exit %d: %s", command, code, errOut.String())
		}
	}
}
