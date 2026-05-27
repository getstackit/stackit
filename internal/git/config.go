// Package git provides low-level Git operations, including repository access,
// branch operations, commit information, PR operations, and metadata management.
package git

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	gogitconfig "github.com/go-git/go-git/v6/config"
	format "github.com/go-git/go-git/v6/plumbing/format/config"
)

// GetCurrentDate returns the current date and time in yyyyMMddHHmmss format in UTC
func GetCurrentDate() string {
	now := time.Now().UTC()
	return now.Format("20060102150405")
}

// ConfigStore provides typed access to git config with a simpler interface
// than the full Runner. It operates directly on the repository via git commands.
type ConfigStore struct {
	repoRoot string
}

// NewConfigStore creates a new ConfigStore for the given repository root.
func NewConfigStore(repoRoot string) *ConfigStore {
	return &ConfigStore{repoRoot: repoRoot}
}

type configKey struct {
	section    string
	subsection string
	name       string
}

func parseConfigKey(key string) (configKey, error) {
	parts := strings.Split(key, ".")
	switch {
	case len(parts) == 2:
		return configKey{section: parts[0], name: parts[1]}, nil
	case len(parts) >= 3:
		return configKey{
			section:    parts[0],
			subsection: strings.Join(parts[1:len(parts)-1], "."),
			name:       parts[len(parts)-1],
		}, nil
	default:
		return configKey{}, fmt.Errorf("invalid config key %q", key)
	}
}

func (c *ConfigStore) loadConfig() (*Repository, *format.Config, error) {
	repo, err := OpenRepository(c.repoRoot)
	if err != nil {
		return nil, nil, err
	}

	cfg, err := repo.Config()
	if err != nil {
		_ = repo.Close()
		return nil, nil, err
	}
	if cfg.Raw == nil {
		cfg.Raw = format.New()
	}
	return repo, cfg.Raw, nil
}

// Get retrieves a single config value from local git config.
// Returns empty string if the key doesn't exist.
func (c *ConfigStore) Get(key string) (string, error) {
	parsed, err := parseConfigKey(key)
	if err != nil {
		return "", err
	}

	repo, cfg, err := c.loadConfig()
	if err != nil {
		return "", err
	}
	defer func() { _ = repo.Close() }()
	if parsed.subsection == "" {
		return cfg.Section(parsed.section).Option(parsed.name), nil
	}
	return cfg.Section(parsed.section).Subsection(parsed.subsection).Option(parsed.name), nil
}

// GetMerged retrieves a config value applying git's standard precedence:
// local > global > system. Returns empty string if the key is not set in any
// scope. Use this for keys (like user.name) whose canonical home is typically
// the user's global config rather than the repository.
func (c *ConfigStore) GetMerged(key string) (string, error) {
	val, err := c.Get(key)
	if err != nil {
		return "", err
	}
	if val != "" {
		return val, nil
	}

	for _, scope := range []gogitconfig.Scope{gogitconfig.GlobalScope, gogitconfig.SystemScope} {
		val, err := readFromScope(key, scope)
		if err != nil {
			return "", err
		}
		if val != "" {
			return val, nil
		}
	}
	return "", nil
}

func readFromScope(key string, scope gogitconfig.Scope) (string, error) {
	parsed, err := parseConfigKey(key)
	if err != nil {
		return "", err
	}
	cfg, err := gogitconfig.LoadConfig(scope)
	if err != nil {
		return "", err
	}
	if cfg == nil || cfg.Raw == nil {
		return "", nil
	}
	if parsed.subsection == "" {
		return cfg.Raw.Section(parsed.section).Option(parsed.name), nil
	}
	return cfg.Raw.Section(parsed.section).Subsection(parsed.subsection).Option(parsed.name), nil
}

// GetAll retrieves all values for a multi-value config key.
// Returns empty slice if the key doesn't exist.
func (c *ConfigStore) GetAll(key string) ([]string, error) {
	parsed, err := parseConfigKey(key)
	if err != nil {
		return nil, err
	}

	repo, cfg, err := c.loadConfig()
	if err != nil {
		return nil, err
	}
	defer func() { _ = repo.Close() }()
	var values []string
	if parsed.subsection == "" {
		values = cfg.Section(parsed.section).OptionAll(parsed.name)
	} else {
		values = cfg.Section(parsed.section).Subsection(parsed.subsection).OptionAll(parsed.name)
	}
	if len(values) == 0 {
		return nil, nil
	}
	return values, nil
}

// Set sets a config value in local git config.
func (c *ConfigStore) Set(key, value string) error {
	parsed, err := parseConfigKey(key)
	if err != nil {
		return err
	}

	repo, raw, err := c.loadConfig()
	if err != nil {
		return err
	}
	defer func() { _ = repo.Close() }()
	raw.SetOption(parsed.section, parsed.subsection, parsed.name, value)
	return repo.SetConfigRaw(raw)
}

// SetBool sets a boolean config value.
func (c *ConfigStore) SetBool(key string, value bool) error {
	return c.Set(key, strconv.FormatBool(value))
}

// SetInt sets an integer config value.
func (c *ConfigStore) SetInt(key string, value int) error {
	return c.Set(key, strconv.Itoa(value))
}

// Add adds a value to a multi-value config key.
func (c *ConfigStore) Add(key, value string) error {
	parsed, err := parseConfigKey(key)
	if err != nil {
		return err
	}

	repo, raw, err := c.loadConfig()
	if err != nil {
		return err
	}
	defer func() { _ = repo.Close() }()
	raw.AddOption(parsed.section, parsed.subsection, parsed.name, value)
	return repo.SetConfigRaw(raw)
}

// Unset removes all values for a config key.
// Does not return an error if the key doesn't exist.
func (c *ConfigStore) Unset(key string) error {
	parsed, err := parseConfigKey(key)
	if err != nil {
		return err
	}

	repo, raw, err := c.loadConfig()
	if err != nil {
		return err
	}
	defer func() { _ = repo.Close() }()
	if parsed.subsection == "" {
		raw.Section(parsed.section).RemoveOption(parsed.name)
	} else {
		raw.Section(parsed.section).Subsection(parsed.subsection).RemoveOption(parsed.name)
	}
	return repo.SetConfigRaw(raw)
}

// GetBool retrieves a boolean config value.
// Returns false and no error if the key doesn't exist.
func (c *ConfigStore) GetBool(key string) (bool, error) {
	val, err := c.Get(key)
	if err != nil || val == "" {
		return false, err
	}
	return strconv.ParseBool(val)
}

// GetBoolWithDefault retrieves a boolean config value with a default.
func (c *ConfigStore) GetBoolWithDefault(key string, defaultValue bool) bool {
	val, err := c.GetBool(key)
	if err != nil {
		return defaultValue
	}
	// If key doesn't exist, Get returns empty string, GetBool returns false
	// We need to check if the key actually exists
	raw, _ := c.Get(key)
	if raw == "" {
		return defaultValue
	}
	return val
}

// GetInt retrieves an integer config value.
// Returns 0 and no error if the key doesn't exist.
func (c *ConfigStore) GetInt(key string) (int, error) {
	val, err := c.Get(key)
	if err != nil || val == "" {
		return 0, err
	}
	return strconv.Atoi(val)
}

// GetIntWithDefault retrieves an integer config value with a default.
func (c *ConfigStore) GetIntWithDefault(key string, defaultValue int) int {
	raw, _ := c.Get(key)
	if raw == "" {
		return defaultValue
	}
	val, err := c.GetInt(key)
	if err != nil {
		return defaultValue
	}
	return val
}

// Exists checks if a config key exists.
func (c *ConfigStore) Exists(key string) bool {
	val, err := c.Get(key)
	return err == nil && val != ""
}
