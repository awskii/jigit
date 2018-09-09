package main

import (
	"fmt"
	"os"

	"subcmd/commit"
	"subcmd/config"
	"subcmd/link"
	"subcmd/list"
	newp "subcmd/new"

	"github.com/jessevdk/go-flags"
)

var cfg struct {
	SubAdd     SubAdd           `command:"add" description:"create new issue"`
	SubLs      list.Subcmd      `command:"ls" description:"list projects or issues at JIRA or GitLab"`
	SubLn      link.SubLn       `command:"ln" description:"link GitLab issue with JIRA ticket (or vice versa)"`
	SubConfig  config.Subcmd    `command:"config" description:"configuration stuff"`
	SubCommit  commit.SubCommit `command:"commit" description:"create, update or delete comments on task"`
	SubVersion SubVersion       `command:"version" description:"print current jigit version"`
	SubAdd     newp.Cmd   `command:"add" description:"create new issue"`
}

func main() {
	if _, err := flags.Parse(&cfg); err != nil {
		os.Exit(1)
	}

	var err error
	switch {
	case cfg.SubLs.Active:
		err = list.Process(cfg.SubLs)
	case cfg.SubConfig.Active:
		err = config.Process(cfg.SubConfig)
	}

	if err != nil {
		fmt.Println(err)
	}
}

var version = "0.0.1-alpha"

type SubVersion struct{}

func (c *SubVersion) Execute(v []string) error {
	fmt.Printf("Version: %s\n", version)
	return nil
}
