package config

import (
	"fmt"
	"strings"
)

// docSectionMapping maps section names to their documentation groupings.
// Some sections are grouped together in docs (e.g., "undo" and "concurrency" -> "Other settings").
var docSectionMapping = map[string]string{
	SectionConcurrency: SectionUndo, // Group with "Other settings"
}

// GenerateConfigDocs generates markdown documentation for all configuration options.
// This is used to generate the config reference documentation.
func GenerateConfigDocs() string {
	var sb strings.Builder

	// Track which sections we've already written
	written := make(map[string]bool)

	for _, section := range Sections {
		// Skip sections without docs title
		if section.DocsTitle == "" {
			continue
		}

		// Skip if already written (due to grouping)
		if written[section.Name] {
			continue
		}

		fmt.Fprintf(&sb, "## %s\n\n", section.DocsTitle)

		// Get options for this section
		opts := GetOptionsForSection(section.Name)

		// Also include options from sections that map to this one
		for mappedSection, target := range docSectionMapping {
			if target == section.Name {
				opts = append(opts, GetOptionsForSection(mappedSection)...)
				written[mappedSection] = true
			}
		}

		for _, opt := range opts {
			// Skip hooks since they're documented separately
			if opt.Section == SectionHooks {
				continue
			}
			writeOptionDocs(&sb, &opt)
		}

		written[section.Name] = true
	}

	return sb.String()
}

func writeOptionDocs(sb *strings.Builder, opt *Option) {
	fmt.Fprintf(sb, "### %s\n\n", opt.YAMLPath)
	fmt.Fprintf(sb, "%s\n\n", opt.Description)

	// Add comment as additional info if present
	if opt.Comment != "" {
		fmt.Fprintf(sb, "**%s**\n\n", opt.Comment)
	}

	// Default value
	if opt.Default != nil {
		fmt.Fprintf(sb, "**Default**: `%v`\n\n", opt.Default)
	} else if opt.Example != "" {
		sb.WriteString("**Default**: (not set)\n\n")
	}

	// Valid values for enums
	if len(opt.ValidValues) > 0 {
		sb.WriteString("**Options**: ")
		quoted := make([]string, len(opt.ValidValues))
		for i, v := range opt.ValidValues {
			quoted[i] = "`" + v + "`"
		}
		sb.WriteString(strings.Join(quoted, ", "))
		sb.WriteString("\n\n")
	}

	// Example usage
	cliKey := opt.YAMLPath
	exampleValue := formatDocExampleValue(opt)
	if exampleValue != "" {
		sb.WriteString("**Example**:\n\n")
		sb.WriteString("```bash\n")
		fmt.Fprintf(sb, "stackit config set %s %s\n", cliKey, exampleValue)
		sb.WriteString("```\n\n")
	}
}

func formatDocExampleValue(opt *Option) string {
	// Use example if provided
	if opt.Example != "" {
		if strings.ContainsAny(opt.Example, " {}") {
			return "\"" + opt.Example + "\""
		}
		return opt.Example
	}

	// Use a sensible example based on type
	if opt.Default != nil {
		switch v := opt.Default.(type) {
		case bool:
			// Show opposite of default as example
			return fmt.Sprintf("%v", !v)
		case int:
			return fmt.Sprintf("%d", v)
		case string:
			if v != "" {
				return v
			}
		}
	}

	// For enums, use first valid value
	if len(opt.ValidValues) > 0 {
		return opt.ValidValues[0]
	}

	return ""
}
