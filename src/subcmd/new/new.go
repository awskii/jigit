package new

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"strings"

	"lib/editor"
	libgit "lib/git"
	libjira "lib/jira"
	"lib/storage"
	"subcmd/config"

	bfconf "github.com/kentaro-m/blackfriday-confluence"
	bf "gopkg.in/russross/blackfriday.v2"
)

type Cmd struct {
	// can be passed via argv?
	Project string   `short:"p" long:"project" description:"GitLab project name"`
	Title   string   `short:"t" long:"title" description:"issue title (less than 160 chars)"`
	Body    string   `short:"b" long:"body" description:"issue body"`
	Tags    []string `long:"tags" description:"list of coma-separated tags. Will be set to gitlab issue, if exists"`
}

func (o *Cmd) Execute(v []string) error {
	return exec(o, v)
}

func exec(c *Cmd, argv []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	disk, err := storage.NewStorage(cfg.Storage.Path)
	if err != nil {
		return err
	}
	defer disk.Close()

	projectName := c.Project
	if projectName == "" {
		fmt.Fprintf(os.Stderr,
			"You should specify GitLab project name with -p or --project flag . See --help for details.")
		os.Exit(1)
	}

	if c.Title == "" && cfg.Editor != "" {
		v, err := editor.NewFile(cfg.Editor, "new-issue")
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
		if c.Title == "" {
			r := bufio.NewReader(bytes.NewReader(b))
			t, _, err := r.ReadLine()
			if err != nil {
				return nil
			}
			c.Title = bytes.NewBuffer(t).String()
		}
		c.Body = fmt.Sprintf("%s", b)
	}

	c.Body = strings.Trim(c.Body, "\n ")
	if c.Body == "" {
		fmt.Fprintln(os.Stderr, "Nothing to create. Provide ticket body via -b or --body flag.")
		os.Exit(1)
	}

	git, err := libgit.NewWithStorage(disk)
	if err != nil {
		return err
	}

	p, err := git.ProjectByName(projectName, false, false)
	if err != nil {
		return err
	}
	gitUser, err := git.User()
	if err != nil {
		return err
	}
	jira, err := libjira.NewWithStorage(disk)
	if err != nil {
		return err
	}
	jiraUser, err := jira.User()
	if err != nil {
		return err
	}

	gitIssue, err := git.CreateIssue(&libgit.Issue{
		ProjectID:        p.ID,
		Title:            c.Title,
		Description:      md2jira(c.Body),
		AssigneeName:     gitUser.Name,
		AssigneeUsername: gitUser.Login,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Can't create GitLab ticket: %s", err)
		os.Exit(1)
	}

	jiraIssue, err := jira.CreateIssue(&libjira.Issue{
		Summary:     c.Title,
		Description: c.Body,
		Assignee:    *jiraUser,
		Creator:     *jiraUser,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Can't create Jira ticket: %s", err)

		err = git.DeleteIssue(gitIssue.ProjectID, gitIssue.IID)
		if err != nil {
			fmt.Fprintf(os.Stderr,
				"Can't remove already created git isssue #%s: %s", gitIssue.IID, err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr,
			"Ticket was not creted, gitlab issue has been removed successfully.")
		os.Exit(1)
	}

	err = disk.CreateSymlink(jiraIssue.Key, p.Name, gitIssue.IID)
	if err != nil {
		fmt.Fprintf(os.Stderr,
			"Issue has been created, but was not linked: %s", err)
		fmt.Fprintf(os.Stderr,
			"You can link it by yoursulf. Git: '%s#%d' Jira: '%s'",
			p.Name, gitIssue.IID, jiraIssue.Key)
		return err
	}
	fmt.Printf("Issue '%s#%d'/'%s' has been created and linked successfully.",
		p.Name, gitIssue.IID, jiraIssue.Key)
	return nil
}

func md2jira(msg string) string {
	renderer := &bfconf.Renderer{}
	md := bf.New(bf.WithRenderer(renderer), bf.WithExtensions(bf.CommonExtensions))
	return bytes.NewBuffer(renderer.Render(md.Parse([]byte(msg)))).String()
}
