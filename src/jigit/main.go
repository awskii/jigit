package main

import (
	"fmt"
	"list"

	"config"
	"github.com/jessevdk/go-flags"
	"os"
)

var version = "0.0.1-alpha"

func main() {
	if _, err := flags.Parse(&cfg); err != nil {
		os.Exit(1)
	}

	var err error
	switch {
	case cfg.Version:
		fmt.Println(version)
	case cfg.SubLs.Active:
		err = list.Process(cfg.SubLs)
	case cfg.SubConfig.Active:
		err = config.Process(cfg.SubConfig)
	}

	if err != nil {
		fmt.Println(err)
	}
}

type SubIssue struct {
	IssueTitle string   `short:"t"`
	IssueTags  []string `long:"tags"`
	IssueBody  string   `short:"m"`
	Priority   int      `short:"p"`
	Issue      string   `short:"i"`
	//ResolutionMessage string   `short:"m"`
}

func (o *SubIssue) Execute(v []string) error {
	fmt.Println("open!", v)
	return nil
}

//type SubClose struct {
//}
//
//func (c *SubClose) Execute(v []string) error {
//	fmt.Println("open!", v)
//	return nil
//}

type SubLn struct {
	JiraTicket string `short:"j"`
	GitIssue   string `short:"g"`
}

func (ln *SubLn) Execute(v []string) error {
	fmt.Println("ln!", v)
	return nil
}

type SubTag struct {
	List    bool   `long:"ls"`
	Touch   string `long:"touch"`
	Rm      string `long:"rm"`
	Migrate string
}

func (t *SubTag) Execute(v []string) error {
	fmt.Println("ln!", v)
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

type SubStatus struct{}

func (c *SubStatus) Execute(v []string) error {
	return nil
}

var cfg struct {
	Version   bool          `short:"v" long:"version" description:"print current jigit version"`
	SubLs     list.Subcmd   `command:"ls" description:"list projects or issues at Jira or GitLab"`
	SubLn     SubLn         `command:"ln" description:"link GitLab issue with Jira ticket (or vice versa)"`
	SubIssue  SubIssue      `command:"issue" description:"open new issue and ticket"`
	SubConfig config.Subcmd `command:"config" description:"configuration stuff"`
	SubTag    SubTag        `command:"tag" description:"create, list, migrate or delete tags"`
	SubCommit SubCommit     `command:"commit" description:"create, list, update or delete comments on task"`
	//SubClose  SubClose  `command:"close" description:"close issue and ticket"`
}
