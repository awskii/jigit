package config

import (
	"errors"
	"fmt"
	"os"
	"path"

	"github.com/BurntSushi/toml"
	"strconv"
)

type Subcmd struct {
	Show bool `long:"show"`
	List bool `long:"list" description:"list all valid configuration keys"`
	Set  bool `long:"set" description:"save key value pair"`

	Active bool
	Argv   []string
}

func (s *Subcmd) Execute(argv []string) error {
	s.Active, s.Argv = true, argv
	return nil
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

var keysWithUsage = map[string]string{
	"gitlab.address":           "<string> address to your GitLab installation",
	"jira.address":             "<string> address to your JIRA installation",
	"persistent.path":          "<string> path to persistent storage file",
	"persistent.disable_cache": "<bool>   disables projects and issue caches if true",
	"persistent.encrypt":       "<bool>   defines if sensitive data (your tokens at least) should be encrypted",
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

var configName = path.Join(".", ".jigit")

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

func (c *Config) save() error {
	w, err := os.Create(configName)
	if err != nil {
		return fmt.Errorf("save: %s", err)
	}
	return toml.NewEncoder(w).Encode(c)
}

var (
	ErrBadArgc    = errors.New("not enough arguments")
	ErrUnknownKey = errors.New("unknown configuration key")
)

func Process(fl Subcmd) error {
	cfg, err := Load()
	if err != nil {
		//fmt.Println("Configuration file was not found. Initialize new one.")
		cfg = new(Config)
	}

	switch {
	// Todo: implement show
	case fl.Set:
		if len(fl.Argv) < 2 {
			msg := "you should provide configuration key and value pair:\n\n" +
				"\tjigit config --set key value\n"
			fmt.Printf(msg)
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
	case fl.List:
		fmt.Printf("Next items available for configuration:\n\n")
		for key, usage := range keysWithUsage {
			fmt.Printf(" %30s - %s\n", key, usage)
		}

		fmt.Printf("\nConfiguration file available at '%s'\n", configName)
		return nil
	default:
		// todo print valid usage
		fmt.Printf("usage\n\n")
		return ErrBadArgc
	}
	return cfg.save()
}
