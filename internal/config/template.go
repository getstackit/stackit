package config

import (
	"fmt"
	"strings"
)

// GenerateConfigTemplate generates a .stackit.yaml template with all options commented out.
// The template is generated from Options, ensuring it stays in sync with the codebase.
func GenerateConfigTemplate() string {
	return generateYAML(true, nil)
}

// GenerateYAMLExample generates an example .stackit.yaml with common settings uncommented.
// This is used in documentation to show a complete working example.
func GenerateYAMLExample() string {
	// Overrides for example values that differ from defaults
	overrides := map[string]string{
		SectionTrunks:      "develop, staging",
		"submit.labels":    "needs-review",
		"submit.reviewers": "teammate1",
		"ci.command":       ciCommandExample,
	}
	return generateYAML(false, overrides)
}

// generateYAML generates YAML config output.
// If commented is true, all values are commented out (for templates).
// If commented is false, values are uncommented (for examples).
func generateYAML(commented bool, overrides map[string]string) string {
	var sb strings.Builder

	// Header
	if commented {
		sb.WriteString("# Stackit Team Configuration\n")
		sb.WriteString("# Shared with your team via git. Personal overrides: stackit config set <key> <value>\n")
		sb.WriteString("# Docs: https://getstackit.github.io/stackit/cli/config/\n")
	} else {
		sb.WriteString("# .stackit.yaml - Team-wide defaults\n")
	}

	for _, section := range Sections {
		opts := GetOptionsForSection(section.Name)
		if len(opts) == 0 {
			continue
		}

		sb.WriteString("\n")

		// Section header comment (if any)
		if section.Title != "" {
			fmt.Fprintf(&sb, "# %s\n", section.Title)
		}

		// Write options for this section
		writeYAMLSection(&sb, opts, commented, overrides)
	}

	return sb.String()
}

func writeYAMLSection(sb *strings.Builder, opts []Option, commented bool, overrides map[string]string) {
	// Group by top-level key
	groups := make(map[string][]Option)
	var groupOrder []string

	for _, opt := range opts {
		parts := strings.Split(opt.YAMLPath, ".")
		topLevel := parts[0]
		if _, exists := groups[topLevel]; !exists {
			groupOrder = append(groupOrder, topLevel)
		}
		groups[topLevel] = append(groups[topLevel], opt)
	}

	prefix := ""
	if commented {
		prefix = "# "
	}

	for _, topLevel := range groupOrder {
		groupOpts := groups[topLevel]

		// Single top-level option (like "trunk" or "maxConcurrency")
		if len(groupOpts) == 1 && groupOpts[0].YAMLPath == topLevel {
			opt := groupOpts[0]
			writeSingleOption(sb, opt, topLevel, commented, overrides)
			continue
		}

		// Nested section (like "submit" with "submit.footer", "submit.draft", etc.)
		fmt.Fprintf(sb, "%s%s:\n", prefix, topLevel)

		for _, opt := range groupOpts {
			parts := strings.Split(opt.YAMLPath, ".")
			if len(parts) < 2 {
				continue
			}
			subKey := parts[1]
			writeNestedOption(sb, opt, subKey, commented, overrides)
		}
	}
}

func writeSingleOption(sb *strings.Builder, opt Option, key string, commented bool, overrides map[string]string) {
	prefix := ""
	if commented {
		prefix = "# "
	}

	if commented {
		// Write description comment
		comment := buildOptionComment(opt)
		fmt.Fprintf(sb, "# %s", comment)
		if opt.Comment != "" {
			fmt.Fprintf(sb, "\n# %s", opt.Comment)
		}
		sb.WriteString("\n")
	}

	// Write the value
	value := getOptionValue(opt, overrides)
	if opt.IsArray {
		fmt.Fprintf(sb, "%s%s:\n", prefix, key)
		writeArrayValue(sb, opt, overrides, prefix+"  ")
	} else {
		fmt.Fprintf(sb, "%s%s: %s\n", prefix, key, value)
	}
}

func writeNestedOption(sb *strings.Builder, opt Option, subKey string, commented bool, overrides map[string]string) {
	prefix := ""
	if commented {
		prefix = "#   "
	} else {
		prefix = "  "
	}

	value := getOptionValue(opt, overrides)
	comment := opt.Description

	// If there's a special Comment field (like placeholders), write it as a separate line
	if commented && opt.Comment != "" {
		fmt.Fprintf(sb, "#   # %s\n", opt.Comment)
	}

	if opt.IsArray {
		if hasArrayValues(opt, overrides) {
			fmt.Fprintf(sb, "%s%s:\n", prefix, subKey)
			writeArrayValue(sb, opt, overrides, prefix+"  ")
		} else {
			if commented {
				fmt.Fprintf(sb, "%s%s: []%s\n", prefix, subKey, formatInlineComment(comment))
			} else {
				fmt.Fprintf(sb, "%s%s: []\n", prefix, subKey)
			}
		}
	} else {
		if commented {
			fmt.Fprintf(sb, "%s%s: %s%s\n", prefix, subKey, value, formatInlineComment(comment))
		} else {
			fmt.Fprintf(sb, "%s%s: %s\n", prefix, subKey, value)
		}
	}
}

func writeArrayValue(sb *strings.Builder, opt Option, overrides map[string]string, indent string) {
	// Check for override
	if overrides != nil {
		if override, ok := overrides[opt.YAMLPath]; ok {
			for item := range strings.SplitSeq(override, ",") {
				fmt.Fprintf(sb, "%s- %s\n", indent, strings.TrimSpace(item))
			}
			return
		}
	}

	// Use example if available
	if opt.Example != "" {
		for ex := range strings.SplitSeq(opt.Example, ",") {
			fmt.Fprintf(sb, "%s- %s\n", indent, strings.TrimSpace(ex))
		}
		return
	}

	// Generic array placeholder
	fmt.Fprintf(sb, "%s- \"example\"\n", indent)
}

// getOptionValue returns the formatted value for an option, checking overrides first.
func getOptionValue(opt Option, overrides map[string]string) string {
	// Check for override first
	if overrides != nil {
		if override, ok := overrides[opt.YAMLPath]; ok {
			return formatValue(override)
		}
	}

	// Use default if available
	if opt.Default != nil {
		return formatDefaultValue(opt.Default)
	}

	// Use example if available
	if opt.Example != "" {
		return formatValue(opt.Example)
	}

	return "\"\""
}

// formatValue formats a string value for YAML output.
func formatValue(v string) string {
	if v == "" {
		return "\"\""
	}
	if strings.ContainsAny(v, " {}[]#:") {
		return fmt.Sprintf("\"%s\"", v)
	}
	return v
}

// formatDefaultValue formats a default value for YAML output.
func formatDefaultValue(v any) string {
	switch val := v.(type) {
	case string:
		return formatValue(val)
	case bool:
		return fmt.Sprintf("%v", val)
	case int:
		return fmt.Sprintf("%d", val)
	default:
		return fmt.Sprintf("%v", val)
	}
}

func buildOptionComment(opt Option) string {
	desc := opt.Description

	// Add default value indication if present
	if opt.Default != nil {
		switch v := opt.Default.(type) {
		case string:
			if v != "" {
				desc = fmt.Sprintf("%s (default: %s)", desc, v)
			}
		case bool:
			desc = fmt.Sprintf("%s (default: %v)", desc, v)
		case int:
			desc = fmt.Sprintf("%s (default: %d)", desc, v)
		}
	}

	return desc
}

func formatInlineComment(comment string) string {
	if comment == "" {
		return ""
	}
	return fmt.Sprintf(" # %s", comment)
}

func hasArrayValues(opt Option, overrides map[string]string) bool {
	if overrides != nil {
		if _, ok := overrides[opt.YAMLPath]; ok {
			return true
		}
	}
	return opt.Example != ""
}
