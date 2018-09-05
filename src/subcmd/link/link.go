package link

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"

	"lib/git"
	"lib/jira"
	"lib/storage"
	"subcmd/config"
)

type SubLn struct {
	Drop bool `short:"d" long:"drop" description:"delete provided link"`
	List bool `short:"l" long:"list" description:"print all existing links"`

	Active bool
	Argv   []string
}

func usage() {
	fmt.Fprintf(os.Stderr,
		"To create link between GitLab issue and Jira ticket, use next syntax:\n\t"+
			"jigit ln JIRA-ID GITLAB_PROJECT_NAME#ISSUE_ID\n")
	os.Exit(1)
}

func (ln *SubLn) Execute(v []string) error {
	ln.Active, ln.Argv = true, v
	return process(ln)
}

func getLinkedJiraIssue(disk *storage.Storage, id int) (*jira.Issue, error) {
	issueID, err := disk.GetString(storage.BucketIssueLinks, storage.Itob(id))
	if err != nil {
		return nil, err
	}
	j, err := jira.New()
	if err != nil {
		return nil, err
	}
	return j.Issue(issueID)
}

func getLinkedGitIssue(disk *storage.Storage, id string) (*git.Issue, error) {
	issueID, err := disk.Get(storage.BucketIssueLinks, []byte(id))
	if err != nil {
		return nil, err
	}
	g, err := git.New()
	if err != nil {
		return nil, err
	}
	return g.Issue(storage.Btoi(issueID))
}

func extractIDs(argv []string) (gitlabProject string, issueID int, jiraTicket string) {
	for i := 0; i < len(argv); i++ {
		if !strings.Contains(argv[i], "#") {
			jiraTicket = argv[i]
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

func process(fl *SubLn) error {
	if len(fl.Argv) == 0 && !fl.List {
		usage()
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

	gitProject, gitIID, jiraTicket := extractIDs(fl.Argv)

	if fl.Drop {
		err := disk.DropSymlink(jiraTicket, gitProject, gitIID)
		if err != nil {
			return err
		}
		fmt.Println("Link has been deleted successfully.")
		return nil
	}

	if fl.List {
		return disk.ForEach(storage.BucketIssueLinks, func(k, v []byte) error {
			fmt.Printf("%s %s\n", k, v)
			return nil
		})
	}

	var (
		wg     sync.WaitGroup
		issue  *git.Issue
		ticket *jira.Issue
		errWg  error
	)

	wg.Add(1)
	go func(wg *sync.WaitGroup, disk *storage.Storage) {
		defer wg.Done()

		git, err := git.NewWithStorage(disk)
		if err != nil {
			errWg = err
			return
		}
		p, err := git.Project(gitProject)
		if err != nil {
			errWg = err
			return
		}
		issue, _, err = git.DetailedProjectIssue(p.ID, gitIID)
		if err != nil {
			errWg = err
			return
		}
		// now we are sure that project and issue exists
	}(&wg, disk)

	wg.Add(1)
	go func(wg *sync.WaitGroup, disk *storage.Storage) {
		defer wg.Done()

		jira, err := jira.NewWithStorage(disk)
		if err != nil {
			errWg = err
			return
		}
		ticket, err = jira.Issue(jiraTicket)
		if err != nil {
			errWg = err
			return
		}
		// now we are sure that jira ticket exists
	}(&wg, disk)

	wg.Wait()
	if errWg != nil {
		return errWg
	}

	if err = disk.CreateSymlink(ticket.Key, gitProject, issue.IID); err != nil {
		return err
	}
	fmt.Println("Successfully linked.")
	return nil
}
