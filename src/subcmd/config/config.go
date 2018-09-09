package config

import (
	"errors"
	"fmt"
	"os"
	"os/user"
	"path"
	"strconv"

	"github.com/BurntSushi/toml"
)

type Cmd struct {
	Set  bool `long:"set" description:"save key value pair"`
	Show bool `long:"show" description:"show current config values"`

	Active bool
	Argv   []string
}

type Config struct {
	Editor string
	GitLab struct {
		Address string `toml:"address"`
	} `toml:"gitlab"`
	Jira struct {
		Address string `toml:"address"`
	} `toml:"jira"`
	Storage struct {
		Path         string `toml:"path"`
		DisableCache bool   `toml:"disable_cache"`
		Encrypt      bool   `toml:"encrypt"`
	} `toml:"storage"`
}

func Process(fl Cmd) error {
	cfg, err := Load()
	if err != nil {
		cfg = new(Config)
	}

	switch {
	case fl.Show:
		fmt.Println("Current config values are:")
		fmt.Printf("\teditor: %s\n", cfg.Editor)
		fmt.Printf("\tgitlab.address: %s\n", cfg.GitLab.Address)
		fmt.Printf("\tjira.address: %s\n", cfg.Jira.Address)
		fmt.Println()
		fmt.Printf("\tstorage.path: %s\n", cfg.Storage.Path)
		fmt.Printf("\tstorage.disable_cache: %t\n", cfg.Storage.DisableCache)
		fmt.Printf("\tstorage.encrypt: %t\n", cfg.Storage.Encrypt)
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

func (s *Cmd) Execute(argv []string) error {
	s.Active, s.Argv = true, argv
	return nil
}

var (
	configName = expandConfigPath()

	ErrBadArgc    = errors.New("not enough arguments")
	ErrUnknownKey = errors.New("unknown configuration key")

	usages = []string{
		"URLs configuration\n",
		"  gitlab.address - <string> address to your GitLab installation",
		"  jira.address   - <string> address to your JIRA installation",
		"\n Cache and storage storage configuration\n",
		"  storage.path      - <string> path to storage storage file",
		"  storage.encrypt   - <bool>   defines if sensitive data (your tokens at least) should be encrypted",
		"  storage.off_cache - <bool>   disables projects and issue caches if true",
		"\n Misc\n",
		"  editor - <string> same as $EDITOR environment variable",
	}
)

func initDefaultConfig() *Config {
	c := new(Config)
	c.Storage.Encrypt = true
	c.Storage.Path = "/var/local/jigit/cache"
	return c
}

func expandConfigPath() string {
	u, err := user.Current()
	if err != nil {
		panic(err)
	}
	return path.Join(u.HomeDir, ".jigit")
}

// Load configuration from file or return default configuration
// if any error occurred
func Load() (*Config, error) {
	c := initDefaultConfig()
	_, err := toml.DecodeFile(configName, c)
	if err != nil {
		return initDefaultConfig(), err
	}
	if c.Editor == "" {
		c.Editor = os.Getenv("EDITOR")
	}
	if !path.IsAbs(c.Storage.Path) {
		return nil, errors.New("please, specify full path to cache file")
	}
	return c, nil
}

func (c *Config) setValue(key, value string) error {
	switch key {
	case "editor":
		c.Editor = value
	case "gitlab.address":
		c.GitLab.Address = value
	case "jira.address":
		c.Jira.Address = value
	case "storage.path":
		c.Storage.Path = value
	case "storage.use_cache":
		b, err := strconv.ParseBool(value)
		if err != nil {
			return err
		}
		c.Storage.DisableCache = b
	case "storage.encrypt":
		b, err := strconv.ParseBool(value)
		if err != nil {
			return err
		}
		c.Storage.Encrypt = b
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
