package list

import (
	"errors"
	"time"
)

const commentTime = time.RFC850

var (
	textWidthSize = 100

	sepIssue   = " ======================================\n"
	sepComment = " --------------------------------------\n"

	ErrBadAddress = errors.New("bad address provided")
)

type Subcmd struct {
	JiraMode    bool     `short:"j" long:"jira" description:"if provided, listings will be fetched from Jira instead of GitLab"`
	Assigned    bool     `short:"a" long:"assigned" description:"show all issues assigned to me"`
	Projects    bool     `short:"P" description:"list projects instead of issues"`
	ProjectName string   `short:"p" long:"project" description:"project name to get issues on"`
	ProjectID   int      `long:"pid" description:"project ID to get issues on"`
	IssueID     []string `short:"i" long:"issue" description:"issue ID for detailed view"`

	// Todo Limit parameter and All parameter
	Limit   int  `short:"n" default:"20" description:"limit for entities to show"`
	All     bool `short:"S" long:"ignore-state" description:"ignore issue status"`
	NoCache bool `short:"c" long:"no-cache" description:"invalidate cache and retrieve fresh data from remote"`

	Active bool
	Argv   []string
}

func (ls *Subcmd) Execute(argv []string) error {
	ls.Argv, ls.Active = argv, true
	return nil
}

func Process(fl Subcmd) error {
	if fl.JiraMode {
		return proceedJira(fl)
	}
	return proceedGit(fl)
}
