package config

import (
	"errors"
	"fmt"
	"os"
	"path"
	"strconv"

	"github.com/BurntSushi/toml"
)

type Subcmd struct {
	Set bool `long:"set" description:"save key value pair"`

	Active bool
	Argv   []string
}

type Config struct {
	GitLab struct {
		Address string `toml:"address"`
	} `toml:"gitlab"`
	Jira struct {
		Address string `toml:"address"`
	} `toml:"jira"`
	Persistent struct {
		Path         string `toml:"path"`
		DisableCache bool   `toml:"disable_cache"`
		Encrypt      bool   `toml:"encrypt"`
	} `toml:"persistent"`
}

func Process(fl Subcmd) error {
	cfg, err := Load()
	if err != nil {
		cfg = new(Config)
	}

	switch {
	case fl.Set:
		if len(fl.Argv) < 2 {
			fmt.Printf("You should provide configuration key and value pair:\n\n" +
				"\tjigit config --set key value\n")
			return ErrBadArgc
		}
		err := cfg.setValue(fl.Argv[0], fl.Argv[1])
		if err != nil {
			if err != ErrUnknownKey {
				return err
			}
			msg := "Unknown configuration key '%s'. To list all available keys use\n\n" +
				"\tjigit config --list\n\n"
			fmt.Printf(msg, fl.Argv[0])
		}
	default:
		fmt.Printf("Next items available for configuration:\n\n")
		for _, item := range usages {
			fmt.Printf(" %s\n", item)
		}

		fmt.Printf("\nConfiguration file available at '%s'\n", configName)
		return nil
	}
	return cfg.save()
}

func (s *Subcmd) Execute(argv []string) error {
	s.Active, s.Argv = true, argv
	return nil
}

var (
	configName = path.Join(".", ".jigit")

	ErrBadArgc    = errors.New("not enough arguments")
	ErrUnknownKey = errors.New("unknown configuration key")

	usages = []string{
		"URLs configuration\n",
		"gitlab.address - <string> address to your GitLab installation",
		"jira.address   - <string> address to your JIRA installation",
		"\n Cache and persistent storage configuration\n",
		"persistent.path      - <string> path to persistent storage file",
		"persistent.encrypt   - <bool>   defines if sensitive data (your tokens at least) should be encrypted",
		"persistent.off_cache - <bool>   disables projects and issue caches if true",
	}
)

func initDefaultConfig() *Config {
	c := new(Config)
	c.Persistent.Encrypt = true
	c.Persistent.Path = "/var/local/jigit"
	return c
}

// Load configuration from file or return default configuration
// if any error occurred
func Load() (*Config, error) {
	c := initDefaultConfig()
	_, err := toml.DecodeFile(configName, c)
	if err != nil {
		return initDefaultConfig(), err
	}
	return c, nil
}

func (c *Config) setValue(key, value string) error {
	switch key {
	case "gitlab.address":
		c.GitLab.Address = value
	case "jira.address":
		c.Jira.Address = value
	case "persistent.path":
		c.Persistent.Path = value
	case "persistent.use_cache":
		b, err := strconv.ParseBool(value)
		if err != nil {
			return err
		}
		c.Persistent.DisableCache = b
	case "persistent.encrypt":
		b, err := strconv.ParseBool(value)
		if err != nil {
			return err
		}
		c.Persistent.Encrypt = b
	default:
		return ErrUnknownKey
	}
	return nil
}

func (c *Config) save() error {
	w, err := os.Create(configName)
	if err != nil {
		return fmt.Errorf("save: %s", err)
	}
	return toml.NewEncoder(w).Encode(c)
}
