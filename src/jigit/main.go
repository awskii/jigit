package main

import (
	"fmt"
	"os"

	"subcmd/config"
	"subcmd/list"

	"github.com/jessevdk/go-flags"
	"subcmd/link"
)

var cfg struct {
	SubAdd     SubAdd        `command:"add" description:"create new issue"`
	SubLs      list.Subcmd   `command:"ls" description:"list projects or issues at JIRA or GitLab"`
	SubLn      link.SubLn    `command:"ln" description:"link GitLab issue with JIRA ticket (or vice versa)"`
	SubConfig  config.Subcmd `command:"config" description:"configuration stuff"`
	SubCommit  SubCommit     `command:"commit" description:"create, update or delete comments on task"`
	SubVersion SubVersion    `command:"version" description:"print current jigit version"`
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

type SubAdd struct {
	IssueTitle string   `short:"t"`
	IssueTags  []string `long:"tags"`
	IssueBody  string   `short:"m"`
	Priority   int      `short:"p"`
	Issue      string   `short:"i"`
	//ResolutionMessage string   `short:"m"`
}

func (o *SubAdd) Execute(v []string) error {
	fmt.Println("open!", v)
	return nil
}

type SubCommit struct {
	Message string `short:"m"`
	Issue   string `short:"i"`
}

func (c *SubCommit) Execute(v []string) error {
	fmt.Println("ln!", v)
	return nil
}

var version = "0.0.1-alpha"

type SubVersion struct{}

func (c *SubVersion) Execute(v []string) error {
	fmt.Printf("Current jigit version: %s\n", version)
	return nil
}
