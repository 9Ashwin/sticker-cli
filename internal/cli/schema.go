package cli

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type commandMetadata struct {
	Path        string
	Use         string
	Summary     string
	Description string
	Effect      string
	Parameters  []parameterMetadata
	Flags       []flagMetadata
	Result      map[string]any
	Errors      []schemaError
	Examples    []string
	Args        cobra.PositionalArgs
}

type parameterMetadata struct {
	Name        string
	Type        string
	Source      string
	Description string
	Required    bool
}

type flagMetadata struct {
	Name        string
	Shorthand   string
	Type        string
	Description string
	Default     any
	Required    bool
}

type schemaError struct {
	Type      string `json:"type"`
	Subtype   string `json:"subtype"`
	ExitCode  int    `json:"exit_code"`
	Retryable bool   `json:"retryable"`
}

type commandRegistry struct {
	root     *cobra.Command
	metadata map[string]commandMetadata
}

func (r *commandRegistry) register(_ *cobra.Command, metadata commandMetadata) {
	r.metadata[metadata.Path] = metadata
}

type schemaDocument struct {
	Commands     []string          `json:"commands,omitempty"`
	Command      string            `json:"command,omitempty"`
	Use          string            `json:"use,omitempty"`
	Summary      string            `json:"summary,omitempty"`
	Description  string            `json:"description,omitempty"`
	Effect       string            `json:"effect,omitempty"`
	Parameters   []schemaParameter `json:"parameters,omitempty"`
	InputSchema  map[string]any    `json:"input_schema,omitempty"`
	ResultSchema map[string]any    `json:"result_schema,omitempty"`
	Errors       []schemaError     `json:"errors,omitempty"`
	Examples     []string          `json:"examples,omitempty"`
}

type schemaParameter struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Source      string `json:"source"`
	Shorthand   string `json:"shorthand,omitempty"`
	Description string `json:"description,omitempty"`
	Default     any    `json:"default,omitempty"`
	Required    bool   `json:"required"`
}

func runSchema(cmd *cobra.Command, registry *commandRegistry, args []string) error {
	if len(args) == 0 {
		commands := make([]string, 0, len(registry.metadata))
		for path := range registry.metadata {
			if path != "" {
				commands = append(commands, path)
			}
		}
		sort.Strings(commands)
		return writeResult(cmd, &rootOptions{format: formatJSON}, schemaDocument{Commands: commands}, true)
	}
	path := strings.Join(args, " ")
	metadata, ok := registry.metadata[path]
	if !ok {
		return validationError("invalid_argument", "unknown schema command: "+path, "Run sticker schema to list available command paths.")
	}
	parameters := make([]schemaParameter, 0, len(metadata.Parameters)+len(metadata.Flags)+3)
	for _, parameter := range metadata.Parameters {
		parameters = append(parameters, schemaParameter{
			Name:        parameter.Name,
			Type:        parameter.Type,
			Source:      parameter.Source,
			Description: parameter.Description,
			Required:    parameter.Required,
		})
	}
	for _, flag := range effectiveFlags(registry.root, path) {
		parameters = append(parameters, schemaParameter{
			Name:        "--" + flag.Name,
			Type:        flag.Type,
			Source:      "flag",
			Shorthand:   flag.Shorthand,
			Description: flag.Description,
			Default:     flag.Default,
			Required:    flag.Required,
		})
	}
	schema := schemaDocument{
		Command:      metadata.Path,
		Use:          metadata.Use,
		Summary:      metadata.Summary,
		Description:  metadata.Description,
		Effect:       metadata.Effect,
		Parameters:   parameters,
		InputSchema:  inputSchema(parameters),
		ResultSchema: metadata.Result,
		Errors:       metadata.Errors,
		Examples:     metadata.Examples,
	}
	return writeResult(cmd, &rootOptions{format: formatJSON}, schema, true)
}

func effectiveFlags(root *cobra.Command, path string) []flagMetadata {
	command := root
	for _, part := range strings.Fields(path) {
		for _, child := range command.Commands() {
			if child.Name() == part {
				command = child
				break
			}
		}
	}
	flags := make(map[string]flagMetadata)
	command.InheritedFlags().VisitAll(func(flag *pflag.Flag) {
		flags[flag.Name] = flagMetadataFromFlag(flag)
	})
	command.Flags().VisitAll(func(flag *pflag.Flag) {
		flags[flag.Name] = flagMetadataFromFlag(flag)
	})
	result := make([]flagMetadata, 0, len(flags))
	for _, flag := range flags {
		result = append(result, flag)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

func flagMetadataFromFlag(flag *pflag.Flag) flagMetadata {
	typeName := flag.Value.Type()
	if typeName == "stringSlice" {
		typeName = "string[]"
	}
	return flagMetadata{
		Name:        flag.Name,
		Shorthand:   flag.Shorthand,
		Type:        typeName,
		Description: flag.Usage,
		Default:     typedDefault(flag.Value.Type(), flag.DefValue),
		Required:    len(flag.Annotations) > 0,
	}
}

func typedDefault(typeName, value string) any {
	switch typeName {
	case "bool":
		return value == "true"
	case "int":
		var result int
		_, _ = fmt.Sscanf(value, "%d", &result)
		return result
	case "stringSlice":
		if value == "" || value == "[]" {
			return []string{}
		}
		return strings.Split(strings.Trim(value, "[]"), ",")
	default:
		return value
	}
}

func inputSchema(parameters []schemaParameter) map[string]any {
	properties := make(map[string]any, len(parameters))
	required := make([]string, 0)
	for _, parameter := range parameters {
		properties[parameter.Name] = map[string]any{
			"type":        jsonType(parameter.Type),
			"description": parameter.Description,
		}
		if parameter.Required {
			required = append(required, parameter.Name)
		}
	}
	sort.Strings(required)
	result := map[string]any{"type": "object", "properties": properties}
	if len(required) > 0 {
		result["required"] = required
	}
	return result
}

func jsonType(typeName string) string {
	if strings.HasSuffix(typeName, "[]") {
		return "array"
	}
	switch typeName {
	case "object":
		return "object"
	case "bool":
		return "boolean"
	case "int", "number":
		return "integer"
	default:
		return "string"
	}
}
