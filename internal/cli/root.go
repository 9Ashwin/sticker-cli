// Package cli exposes the local image library command line.
package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"

	"github.com/9Ashwin/sticker-cli/internal/library"
	"github.com/spf13/cobra"
)

type rootOptions struct {
	home   string
	format string
	json   bool
}

// Run executes one invocation without changing process-wide streams or state.
func Run(ctx context.Context, args []string, out, errOut io.Writer, version, commit string) int {
	root := newRoot(out, errOut, version, commit)
	root.SetArgs(args)
	if isHiddenCompletionInvocation(args) {
		return writeError(errOut, validationError("invalid_argument", "shell completion is not part of the CLI contract", "Use sticker help or sticker schema for command discovery."))
	}
	if err := validateHelpInvocation(root, args); err != nil {
		return writeError(errOut, err)
	}
	if err := root.ExecuteContext(ctx); err != nil {
		return writeError(errOut, normalizeError(ctx, err))
	}
	return 0
}

func newRoot(out, errOut io.Writer, version, commit string) *cobra.Command {
	options := &rootOptions{format: formatJSON}
	registry := &commandRegistry{metadata: make(map[string]commandMetadata)}
	rootMetadata := commandMetadata{
		Path:        "",
		Use:         "sticker",
		Summary:     "Install, search and collect local reaction images",
		Description: "Use local reaction images from selectable packs and your personal collection.",
		Effect:      effectRead,
		Result:      objectSchema("help", "string"),
		Examples:    []string{"sticker --help", "sticker schema packs install"},
	}

	root := &cobra.Command{
		Use:           rootMetadata.Use,
		Short:         rootMetadata.Summary,
		Long:          rootMetadata.Description,
		Example:       joinExamples(rootMetadata.Examples),
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return structuredHelp(cmd, options)
		},
	}
	root.SetOut(out)
	root.SetErr(errOut)
	root.CompletionOptions.DisableDefaultCmd = true
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return validationError("invalid_argument", err.Error(), "Check the command help for supported arguments.")
	})
	registry.root = root
	registry.register(root, rootMetadata)

	root.PersistentFlags().StringVar(&options.home, "home", "", "Local data directory (default: platform user config directory)")
	root.PersistentFlags().StringVar(&options.format, "format", formatJSON, "Output format: json or table")
	root.PersistentFlags().BoolVar(&options.json, "json", false, "Alias for --format json")

	root.PersistentPreRunE = func(cmd *cobra.Command, _ []string) error {
		return validateInvocation(cmd, options)
	}

	registerCommands(root, registry, options, version, commit)
	return root
}

const (
	effectRead  = "read"
	effectWrite = "write"
)

func registerCommands(root *cobra.Command, registry *commandRegistry, options *rootOptions, version, commit string) {
	helpCommand := newCommand(helpMetadata())
	helpCommand.Command.RunE = func(cmd *cobra.Command, args []string) error {
		target := findCommand(root, args)
		if target == nil {
			return validationError("invalid_argument", "unknown help topic: "+strings.Join(args, " "), "Run sticker help to list available command paths.")
		}
		helpText, err := renderHelp(target)
		if err != nil {
			return err
		}
		return writeResult(cmd, options, map[string]string{"help": helpText}, false)
	}
	registry.register(root, helpCommand.metadata)
	root.SetHelpCommand(helpCommand.Command)
	root.AddCommand(helpCommand.Command)

	versionCommand := newCommand(commandMetadata{
		Path:     "version",
		Use:      "version",
		Summary:  "Print the CLI build version",
		Effect:   effectRead,
		Result:   objectSchema("version", "string", "commit", "string"),
		Examples: []string{"sticker version", "sticker --format table version"},
	})
	versionCommand.Command.Args = cobra.NoArgs
	versionCommand.Command.RunE = func(cmd *cobra.Command, _ []string) error {
		return writeResult(cmd, options, map[string]string{"version": version, "commit": commit}, false)
	}
	registry.register(root, versionCommand.metadata)
	root.AddCommand(versionCommand.Command)

	schemaCommand := newCommand(commandMetadata{
		Path:        "schema",
		Use:         "schema [command...]",
		Summary:     "Describe commands and their machine-readable contracts",
		Description: "Without a command path, list the discoverable commands. With a path, return its parameters, result shape, errors, examples, and write effect.",
		Effect:      effectRead,
		Parameters: []parameterMetadata{{
			Name:        "command",
			Type:        "string[]",
			Source:      "argument",
			Description: "Optional command path to describe",
		}},
		Result:   objectSchema("commands", "string[]"),
		Errors:   defaultErrors(),
		Examples: []string{"sticker schema", "sticker schema packs install"},
	})
	schemaCommand.Command.Args = cobra.ArbitraryArgs
	schemaCommand.Command.RunE = func(cmd *cobra.Command, args []string) error {
		return runSchema(cmd, registry, args)
	}
	registry.register(root, schemaCommand.metadata)
	root.AddCommand(schemaCommand.Command)

	packs := newCommand(commandMetadata{
		Path:    "packs",
		Use:     "packs",
		Summary: "Discover and manage selectable image packs",
		Effect:  effectRead,
		Result:  objectSchema("items", "object[]"),
	})
	packs.Command.RunE = func(cmd *cobra.Command, _ []string) error {
		return structuredHelp(cmd, options)
	}
	root.AddCommand(packs.Command)

	addPackCommand(packs.Command, registry, packListMetadata(), func(cmd *cobra.Command, _ []string) error {
		return runPackList(cmd, options)
	})
	addPackCommand(packs.Command, registry, packInstallMetadata(), func(cmd *cobra.Command, args []string) error {
		if err := validatePackID(args[0]); err != nil {
			return err
		}
		return placeholder(cmd, "packs install")
	})
	addPackCommand(packs.Command, registry, packUpdateMetadata(), func(cmd *cobra.Command, args []string) error {
		if err := validatePackID(args[0]); err != nil {
			return err
		}
		return placeholder(cmd, "packs update")
	})
	addPackCommand(packs.Command, registry, packRemoveMetadata(), func(cmd *cobra.Command, args []string) error {
		if err := validatePackID(args[0]); err != nil {
			return err
		}
		return placeholder(cmd, "packs remove")
	})

	search := newCommand(searchMetadata())
	search.Command.RunE = func(cmd *cobra.Command, args []string) error {
		if err := validateSearch(cmd); err != nil {
			return err
		}
		return runSearch(cmd.Context(), cmd, options, args)
	}
	registry.register(root, search.metadata)
	root.AddCommand(search.Command)

	get := newCommand(getMetadata())
	get.Command.RunE = func(cmd *cobra.Command, args []string) error {
		if err := validateMD5ID(args[0], "ID"); err != nil {
			return err
		}
		return placeholder(cmd, "get")
	}
	registry.register(root, get.metadata)
	root.AddCommand(get.Command)

	setup := newCommand(setupMetadata())
	setup.Command.RunE = func(cmd *cobra.Command, _ []string) error {
		if err := validateSetup(cmd); err != nil {
			return err
		}
		return placeholder(cmd, "setup")
	}
	registry.register(root, setup.metadata)
	root.AddCommand(setup.Command)

	favorites := newCommand(commandMetadata{
		Path:    "favorites",
		Use:     "favorites",
		Summary: "Maintain your personal image collection",
		Effect:  effectWrite,
		Result:  objectSchema("items", "object[]"),
	})
	favorites.Command.RunE = func(cmd *cobra.Command, _ []string) error {
		return structuredHelp(cmd, options)
	}
	root.AddCommand(favorites.Command)

	collections := newCommand(commandMetadata{
		Path:    "favorites collections",
		Use:     "collections",
		Summary: "Organize personal favorite collections",
		Effect:  effectWrite,
		Result:  objectSchema("collections", "object[]"),
	})
	collections.Command.RunE = func(cmd *cobra.Command, _ []string) error {
		return structuredHelp(cmd, options)
	}
	favorites.Command.AddCommand(collections.Command)

	addFavoriteCommand(collections.Command, registry, favoriteCollectionsListMetadata(), func(cmd *cobra.Command, _ []string) error {
		return placeholder(cmd, "favorites collections list")
	})
	addFavoriteCommand(collections.Command, registry, favoriteCollectionsCreateMetadata(), func(cmd *cobra.Command, args []string) error {
		if err := validateCollectionName(args[0]); err != nil {
			return err
		}
		return placeholder(cmd, "favorites collections create")
	})
	addFavoriteCommand(collections.Command, registry, favoriteCollectionsRenameMetadata(), func(cmd *cobra.Command, args []string) error {
		if err := validateCollectionID(args[0]); err != nil {
			return err
		}
		if err := validateCollectionName(args[1]); err != nil {
			return err
		}
		return placeholder(cmd, "favorites collections rename")
	})
	addFavoriteCommand(collections.Command, registry, favoriteCollectionsRemoveMetadata(), func(cmd *cobra.Command, args []string) error {
		if err := validateCollectionID(args[0]); err != nil {
			return err
		}
		return placeholder(cmd, "favorites collections remove")
	})

	organize := newCommand(favoriteOrganizeMetadata())
	organize.Command.RunE = func(cmd *cobra.Command, _ []string) error {
		if err := validateOrganize(cmd); err != nil {
			return err
		}
		return placeholder(cmd, "favorites organize")
	}
	registry.register(root, organize.metadata)
	favorites.Command.AddCommand(organize.Command)

	addFavoriteCommand(favorites.Command, registry, favoriteAddMetadata(), func(cmd *cobra.Command, args []string) error {
		if err := validateFavoriteAdd(cmd, args); err != nil {
			return err
		}
		return placeholder(cmd, "favorites add")
	})
	addFavoriteCommand(favorites.Command, registry, favoriteListMetadata(), func(cmd *cobra.Command, _ []string) error {
		if err := validateFavoriteList(cmd); err != nil {
			return err
		}
		return placeholder(cmd, "favorites list")
	})
	addFavoriteCommand(favorites.Command, registry, favoriteDescribeMetadata(), func(cmd *cobra.Command, args []string) error {
		if err := validateMD5ID(args[0], "ID"); err != nil {
			return err
		}
		if !cmd.Flags().Changed("caption") {
			return validationError("invalid_argument", "--caption is required", "Provide the new personal caption.")
		}
		return placeholder(cmd, "favorites describe")
	})
	addFavoriteCommand(favorites.Command, registry, favoriteRemoveMetadata(), func(cmd *cobra.Command, args []string) error {
		if err := validateMD5IDs(args, "ID"); err != nil {
			return err
		}
		return placeholder(cmd, "favorites remove")
	})
	addFavoriteCommand(favorites.Command, registry, favoriteImportMetadata(), func(cmd *cobra.Command, _ []string) error {
		return placeholder(cmd, "favorites import")
	})
	addFavoriteCommand(favorites.Command, registry, favoriteExportMetadata(), func(cmd *cobra.Command, _ []string) error {
		return placeholder(cmd, "favorites export")
	})
}

type command struct {
	Command  *cobra.Command
	metadata commandMetadata
}

func newCommand(metadata commandMetadata) command {
	if len(metadata.Errors) == 0 {
		metadata.Errors = defaultErrors()
	}
	cobraCommand := &cobra.Command{
		Use:         metadata.Use,
		Short:       metadata.Summary,
		Long:        metadata.Description,
		Example:     joinExamples(metadata.Examples),
		Annotations: map[string]string{"effect": metadata.Effect},
	}
	for _, flag := range metadata.Flags {
		registerFlag(command{Command: cobraCommand}, flag)
	}
	cobraCommand.Args = metadata.Args
	if cobraCommand.Args == nil {
		cobraCommand.Args = cobra.NoArgs
	}
	return command{Command: cobraCommand, metadata: metadata}
}

func addPackCommand(parent *cobra.Command, registry *commandRegistry, metadata commandMetadata, run func(*cobra.Command, []string) error) {
	child := newCommand(metadata)
	child.Command.RunE = run
	registry.register(parent.Root(), child.metadata)
	parent.AddCommand(child.Command)
}

func addFavoriteCommand(parent *cobra.Command, registry *commandRegistry, metadata commandMetadata, run func(*cobra.Command, []string) error) {
	child := newCommand(metadata)
	child.Command.RunE = run
	registry.register(parent.Root(), child.metadata)
	parent.AddCommand(child.Command)
}

func validateInvocation(_ *cobra.Command, options *rootOptions) error {
	if options.format != formatJSON && options.format != formatTable {
		return validationError("invalid_argument", "unsupported output format", "Use --format json or --format table.")
	}
	if options.json && options.format == formatTable {
		return validationError("invalid_argument", "--json conflicts with --format table", "Use --json or --format table, not both.")
	}
	return nil
}

func validateFavoriteAdd(cmd *cobra.Command, args []string) error {
	id, _ := cmd.Flags().GetString("id")
	if len(args) == 0 && id == "" {
		return validationError("invalid_argument", "one of PATH or --id is required", "Provide a local path or an installed item ID.")
	}
	if len(args) > 0 && id != "" {
		return validationError("invalid_argument", "PATH and --id cannot be used together", "Choose exactly one source for the favorite.")
	}
	if id != "" {
		return validateMD5ID(id, "--id")
	}
	return nil
}

func validateSearch(cmd *cobra.Command) error {
	if err := validatePageFlags(cmd); err != nil {
		return err
	}
	pack, _ := cmd.Flags().GetString("pack")
	if pack != "" {
		return validatePackID(pack)
	}
	return nil
}

func validateSetup(cmd *cobra.Command) error {
	pack, _ := cmd.Flags().GetString("pack")
	switch pack {
	case "curated", "all":
		return nil
	default:
		return validationError("invalid_argument", "--pack must be curated or all", "Choose curated by default or explicitly select all.")
	}
}

func validateFavoriteList(cmd *cobra.Command) error {
	if err := validatePageFlags(cmd); err != nil {
		return err
	}
	sortOrder, _ := cmd.Flags().GetString("sort")
	switch sortOrder {
	case "manual", "added", "caption", "md5":
	default:
		return validationError("invalid_argument", "--sort must be manual, added, caption, or md5", "Choose one of the supported favorite ordering modes.")
	}
	collection, _ := cmd.Flags().GetString("collection")
	if collection != "" {
		return validateCollectionID(collection)
	}
	return nil
}

func validateOrganize(cmd *cobra.Command) error {
	collection, _ := cmd.Flags().GetString("collection")
	if err := validateCollectionID(collection); err != nil {
		return err
	}
	ids, _ := cmd.Flags().GetStringSlice("ids")
	order, _ := cmd.Flags().GetStringSlice("order")
	moveTo, _ := cmd.Flags().GetString("move-to")
	if len(ids) == 0 && len(order) == 0 && moveTo == "" {
		return validationError("invalid_argument", "one of --ids, --move-to, or --order is required", "Provide a collection operation to preview or apply.")
	}
	for _, id := range append(ids, order...) {
		if err := validateMD5ID(id, "favorite ID"); err != nil {
			return err
		}
	}
	if moveTo != "" {
		return validateCollectionID(moveTo)
	}
	return nil
}

func validatePageFlags(cmd *cobra.Command) error {
	limit, _ := cmd.Flags().GetInt("limit")
	if limit < 1 || limit > 100 {
		return validationError("invalid_argument", "--limit must be between 1 and 100", "Choose a page size from 1 through 100.")
	}
	offset, _ := cmd.Flags().GetInt("offset")
	if offset < 0 {
		return validationError("invalid_argument", "--offset cannot be negative", "Choose an offset of 0 or greater.")
	}
	return nil
}

func validateMD5ID(value, label string) error {
	if len(value) != 32 {
		return validationError("invalid_argument", label+" must be a 32-character lowercase MD5", "Provide the complete lowercase hexadecimal item ID.")
	}
	for _, character := range value {
		if !isLowerHex(character) {
			return validationError("invalid_argument", label+" must be a 32-character lowercase MD5", "Provide the complete lowercase hexadecimal item ID.")
		}
	}
	return nil
}

func validateMD5IDs(values []string, label string) error {
	for _, value := range values {
		if err := validateMD5ID(value, label); err != nil {
			return err
		}
	}
	return nil
}

func validatePackID(value string) error {
	if len(value) == 0 || len(value) > 64 || value[0] < 'a' || value[0] > 'z' {
		return validationError("invalid_argument", "pack ID must start with a lowercase letter and be at most 64 characters", "Use lowercase letters, digits, hyphens, or underscores in the pack ID.")
	}
	for _, character := range value[1:] {
		if !isPackIDCharacter(character) {
			return validationError("invalid_argument", "pack ID contains an unsupported character", "Use lowercase letters, digits, hyphens, or underscores in the pack ID.")
		}
	}
	return nil
}

func validateCollectionID(value string) error {
	if value == "" {
		return validationError("invalid_argument", "collection ID is required", "Provide a collection ID.")
	}
	return validatePackID(value)
}

func validateCollectionName(value string) error {
	if len(value) == 0 || len(value) > 128 {
		return validationError("invalid_argument", "collection name must be between 1 and 128 bytes", "Provide a non-empty collection name up to 128 bytes.")
	}
	return nil
}

func isLowerHex(character rune) bool {
	return (character >= '0' && character <= '9') || (character >= 'a' && character <= 'f')
}

func isPackIDCharacter(character rune) bool {
	return (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') || character == '-' || character == '_'
}

func isHiddenCompletionInvocation(args []string) bool {
	for index := 0; index < len(args); index++ {
		argument := args[index]
		switch {
		case argument == "--home" || argument == "--format":
			if index+1 < len(args) {
				index++
				continue
			}
		case strings.HasPrefix(argument, "--home=") || strings.HasPrefix(argument, "--format=") || argument == "--json":
			continue
		case strings.HasPrefix(argument, "-"):
			continue
		default:
			return argument == "__complete" || argument == "__completeNoDesc"
		}
	}
	return false
}

func validateHelpInvocation(root *cobra.Command, args []string) error {
	helpIndex := -1
scan:
	for index, argument := range args {
		if argument == "--" {
			break
		}
		if argument == "--help" || argument == "-h" {
			helpIndex = index
			break
		}
		if strings.HasPrefix(argument, "--help=") {
			switch strings.TrimPrefix(argument, "--help=") {
			case "true":
				helpIndex = index
				break scan
			case "false":
				continue
			default:
				return validationError("invalid_argument", "--help must be true or false", "Use --help or --help=false.")
			}
		}
	}
	if helpIndex < 0 {
		return nil
	}
	if helpIndex+1 < len(args) {
		return validationError("invalid_argument", "arguments are not allowed after --help", "Place --help after the complete command and its arguments.")
	}
	options, err := helpOptions(args[:helpIndex])
	if err != nil {
		return err
	}
	if err := validateInvocation(nil, options); err != nil {
		return err
	}

	command, positionals, err := commandAndPositionals(root, args[:helpIndex])
	if err != nil {
		return err
	}
	if !helpAllowsPositionals(command, len(positionals)) {
		return validationError("invalid_argument", "too many arguments for "+command.CommandPath(), "Check the command help for the supported arguments.")
	}
	return nil
}

func helpOptions(args []string) (*rootOptions, error) {
	options := &rootOptions{format: formatJSON}
	for index := 0; index < len(args); index++ {
		argument := args[index]
		switch {
		case argument == "--json":
			options.json = true
		case strings.HasPrefix(argument, "--json="):
			switch strings.TrimPrefix(argument, "--json=") {
			case "true":
				options.json = true
			case "false":
				options.json = false
			default:
				return nil, validationError("invalid_argument", "--json must be true or false", "Use --json or --json=false.")
			}
		case argument == "--format":
			if index+1 >= len(args) {
				return nil, validationError("invalid_argument", "flag requires a value: --format", "Provide json or table before --help.")
			}
			options.format = args[index+1]
			index++
		case strings.HasPrefix(argument, "--format="):
			options.format = strings.TrimPrefix(argument, "--format=")
		case argument == "--home":
			if index+1 >= len(args) {
				return nil, validationError("invalid_argument", "flag requires a value: --home", "Provide a data directory before --help.")
			}
			index++
		}
	}
	return options, nil
}

func commandAndPositionals(root *cobra.Command, args []string) (*cobra.Command, []string, error) {
	command := root
	positionals := make([]string, 0)
	for index := 0; index < len(args); index++ {
		argument := args[index]
		if argument == "--" {
			positionals = append(positionals, args[index+1:]...)
			break
		}
		if strings.HasPrefix(argument, "-") {
			if argument == "-h" {
				continue
			}
			name, inline := splitLongFlag(argument)
			if name == "" {
				return nil, nil, validationError("invalid_argument", "unsupported flag before --help: "+argument, "Check the command help for supported flags.")
			}
			takesValue, known := flagValueRequirement(command, name)
			if !known {
				return nil, nil, validationError("invalid_argument", "unknown flag: "+name, "Check the command help for supported flags.")
			}
			if takesValue && !inline {
				if index+1 >= len(args) {
					return nil, nil, validationError("invalid_argument", "flag requires a value: "+name, "Provide a value before --help.")
				}
				index++
			}
			continue
		}
		if len(positionals) == 0 {
			for _, child := range command.Commands() {
				if child.Name() == argument && !child.Hidden {
					command = child
					goto nextArgument
				}
			}
		}
		positionals = append(positionals, argument)
	nextArgument:
	}
	return command, positionals, nil
}

func splitLongFlag(argument string) (string, bool) {
	if !strings.HasPrefix(argument, "--") {
		return "", false
	}
	name := strings.TrimPrefix(argument, "--")
	if name == "" {
		return "", false
	}
	if separator := strings.IndexByte(name, '='); separator >= 0 {
		return "--" + name[:separator], true
	}
	return "--" + name, false
}

func flagValueRequirement(command *cobra.Command, name string) (takesValue, known bool) {
	flag := command.Flags().Lookup(strings.TrimPrefix(name, "--"))
	if flag == nil {
		flag = command.InheritedFlags().Lookup(strings.TrimPrefix(name, "--"))
	}
	if flag == nil {
		flag = command.Root().PersistentFlags().Lookup(strings.TrimPrefix(name, "--"))
	}
	if flag == nil {
		return false, false
	}
	return flag.NoOptDefVal == "", true
}

func helpAllowsPositionals(command *cobra.Command, count int) bool {
	parts := strings.Fields(command.Use)
	if len(parts) <= 1 {
		return count == 0
	}
	maximum := len(parts) - 1
	for _, part := range parts[1:] {
		if strings.HasSuffix(part, "...") {
			return true
		}
	}
	return count <= maximum
}

func findCommand(root *cobra.Command, args []string) *cobra.Command {
	command := root
	for _, part := range args {
		if part == "" {
			return nil
		}
		var next *cobra.Command
		for _, child := range command.Commands() {
			if child.Name() == part && !child.Hidden {
				next = child
				break
			}
		}
		if next == nil {
			return nil
		}
		command = next
	}
	return command
}

func renderHelp(command *cobra.Command) (string, error) {
	var output bytes.Buffer
	original := command.OutOrStdout()
	command.SetOut(&output)
	err := command.Help()
	command.SetOut(original)
	if err != nil {
		return "", &cliError{Type: "internal", Subtype: "unexpected", Message: "failed to render help", Hint: err.Error(), ExitCode: 1}
	}
	return strings.TrimSpace(output.String()), nil
}

func structuredHelp(command *cobra.Command, options *rootOptions) error {
	helpText, err := renderHelp(command)
	if err != nil {
		return err
	}
	return writeResult(command, options, map[string]string{"help": helpText}, false)
}

func placeholder(_ *cobra.Command, path string) error {
	return &cliError{
		Type:      "internal",
		Subtype:   "unimplemented",
		Message:   path + " is not implemented yet",
		Hint:      "Use schema or --help to inspect the command contract.",
		ExitCode:  1,
		Retryable: false,
	}
}

func normalizeError(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(ctx.Err(), context.Canceled) || errors.Is(err, context.Canceled) {
		return &cliError{Type: "cancelled", Subtype: "interrupted", Message: "operation cancelled", ExitCode: 130}
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		return &cliError{Type: "cancelled", Subtype: "interrupted", Message: "operation deadline exceeded", ExitCode: 130}
	}
	if isArgumentError(err) {
		return validationError("invalid_argument", err.Error(), "Check the command help for required arguments and flags.")
	}
	var libraryErr *library.Error
	if errors.As(err, &libraryErr) {
		return &cliError{
			Type:      libraryErr.Kind,
			Subtype:   libraryErr.Subtype,
			Message:   libraryErr.Message,
			Hint:      libraryErr.Hint,
			ExitCode:  libraryErrorExitCode(libraryErr.Kind),
			Retryable: libraryErr.Retryable,
		}
	}
	return err
}

func libraryErrorExitCode(kind string) int {
	switch kind {
	case "validation":
		return 2
	case "not_found":
		return 3
	case "network":
		return 4
	case "integrity":
		return 5
	case "conflict":
		return 6
	case "io":
		return 7
	case "cancelled":
		return 130
	default:
		return 1
	}
}

func isArgumentError(err error) bool {
	message := strings.ToLower(err.Error())
	for _, prefix := range []string{
		"unknown command",
		"unknown flag",
		"unknown shorthand flag",
		"required flag",
		"requires ",
		"accepts ",
		"arg(s),",
		"does not accept",
	} {
		if strings.Contains(message, prefix) {
			return true
		}
	}
	return false
}
