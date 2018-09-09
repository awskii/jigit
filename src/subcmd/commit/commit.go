package commit

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"strconv"
	"strings"

	"lib/editor"
	"lib/git"
	"lib/jira"
	"lib/storage"
	"subcmd/config"

	bfconf "github.com/kentaro-m/blackfriday-confluence"
	bf "gopkg.in/russross/blackfriday.v2"
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

func parseNameID(argv []string) (gitlabProject string, issueID int) {
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
	if len(c.Argv) < 1 {
		fmt.Fprintln(os.Stderr,
			"Provide issue ID to commit on with next syntax: project_name#issue_id or via -i flag.")
		os.Exit(1)
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	disk, err := storage.NewStorage(cfg.Storage.Path)
	if err != nil {
		return err
	}
	defer disk.Close()

	projectName, issueID := parseNameID(c.Argv)
	if projectName == "" || issueID == 0 {
		fmt.Fprintf(os.Stderr, "You should specify on which issue you want to commit on. See --help for details.")
		os.Exit(1)
	}

	ticketID, err := disk.GetString(storage.BucketIssueLinks, []byte(c.Argv[0]))
	if err != nil {
		fmt.Fprintf(os.Stderr,
			"Linked ticket was not not found for issue %s#%d, continue commit only in GitLab (y/n)?\n",
			projectName, issueID)
		ok, err := promptYN()
		if err != nil {
			return err
		}
		if !ok {
			fmt.Println("Aborted.")
			return nil
		}
	}

	if cfg.Editor != "" {
		v, err := editor.NewFile(cfg.Editor, "new-commit")
		if err != nil {
			return err
		}

		if err = v.Run(); err != nil {
			return err
		}

		b, err := v.Contents()
		if err != nil {
			return err
		}
		c.Message = fmt.Sprintf("%s", b)
	}

	c.Message = strings.Trim(c.Message, "\n ")
	if c.Message == "" {
		fmt.Fprintln(os.Stderr, "Nothing to commit. Provide commit message via -m or --message flag.")
		os.Exit(1)
	}

	jiraText := md2jira(c.Message)
	git, err := git.NewWithStorage(disk)
	if err != nil {
		return err
	}

	p, err := git.ProjectByName(projectName, false, false)
	if err != nil {
		return err
	}

	jira, err := jira.NewWithStorage(disk)
	if err != nil {
		return err
	}

	cid, err := git.Comment(p.ID, issueID, c.Message)
	if err != nil {
		return err
	}

	if err := jira.Comment(ticketID, jiraText); err != nil {
		fmt.Fprintf(os.Stderr, "can't create Jira ticket: %s", err)
		// rollback gitlab commit
		if err = git.DeleteComment(p.ID, issueID, cid); err != nil {
			fmt.Fprintf(os.Stderr, "can't remove GitLab commit: %s", err)
		}
	}

	return nil
}

// returns true if user respond with Y
func promptYN() (bool, error) {
	var (
		i  = 0
		rd = bufio.NewReader(os.Stdin)
	)

	for {
		yn, _, err := rd.ReadLine()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return false, err
		}

		switch strings.ToLower(string(yn)) {
		case "n":
			return false, nil
		case "y":
			return true, nil
		default:
			fmt.Printf("Prompt only y or n%s\n", strings.Repeat("!", i))
			i++
		}
	}
	return false, nil
}

func md2jira(msg string) string {
	renderer := &bfconf.Renderer{}
	md := bf.New(bf.WithRenderer(renderer), bf.WithExtensions(bf.CommonExtensions))
	return bytes.NewBuffer(renderer.Render(md.Parse([]byte(msg)))).String()
}
