package commit

import (
	"fmt"
	"lib/git"
	"lib/jira"
	"lib/storage"
	"os"
	"strconv"
	"strings"
	"subcmd/config"
)

type Cmd struct {
	Message string `short:"m" long:"message" description:"commit message"`
	Issue   string `short:"i" description:"gitlab issue id to commit on"`
	Status  string `short:"s" long:"status" description:"new issue status.Changes both"`

	Active bool
	Argv   []string
}

func (c *Cmd) Execute(v []string) error {
	c.Active, c.Argv = true, v
	return process(c)
}

func extractIDs(argv []string) (gitlabProject string, issueID int) {
	for i := 0; i < len(argv); i++ {
		if !strings.Contains(argv[i], "#") {
			continue
		}
		parts := strings.Split(argv[i], "#")
		gitlabProject = parts[0]

		var err error
		issueID, err = strconv.Atoi(parts[1])
		if err != nil {
			fmt.Fprintf(os.Stderr, "Bad issue id '%s': %s\n", parts[1], err)
			os.Exit(1)
		}
	}
	return
}

func process(c *Cmd) error {
	//if c.Issue == "" {
	//	fmt.Fprintln(os.Stderr, "You should provide issue ID to commit.")
	//	os.Exit(1)
	//}
	if c.Message == "" {
		fmt.Fprintln(os.Stderr, "Nothing to commit. Provide commit message via -m or --message flag.")
		os.Exit(1)
	}
	if len(c.Argv) != 1 {
		fmt.Fprintln(os.Stderr, "Wrong argc.")
		os.Exit(1)
	}

	projectName, issueID := extractIDs(c.Argv)

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	disk, err := storage.NewStorage(cfg.Storage.Path)
	if err != nil {
		return err
	}
	defer disk.Close()

	ticketID, err := disk.GetString(storage.BucketIssueLinks, []byte(c.Argv[0]))
	if err != nil {
		return err
	}

	jira, err := jira.NewWithStorage(disk)
	if err != nil {
		return err
	}
	if err := jira.Comment(ticketID, c.Message); err != nil {
		return err
	}

	git, err := git.NewWithStorage(disk)
	if err != nil {
		return err
	}

	p, err := git.ProjectByName(projectName, false, false)
	if err != nil {
		return err
	}

	if err := git.Comment(p.ID, issueID, c.Message); err != nil {
		return err
	}
	return nil
}
