package list

import (
	"config"
	"errors"
	"fmt"
	"github.com/olekukonko/tablewriter"
	"github.com/xanzy/go-gitlab"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"persistent"
	"strconv"
	"strings"
	"time"
)

func proceedGit(fl Subcmd) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ep := cfg.GitLab.Address
	if ep == "" {
		return ErrBadAddress
	}

	storage, err := persistent.NewStorage(cfg.Persistent.Path)
	if err != nil {
		return err
	}
	defer storage.Close()

	//key := askPassphrase()
	key := []byte("key")
	login, pass, err := fetchGitCredsFromStorage(storage, key)
	if err != nil {
		// Todo sanitize error
		fmt.Println(err)
		// unauthorized
		login, pass = askCredentials(ep)
		//encLogin, _ := persistent.Encrypt(key, login)
		err := storage.Set(persistent.BucketAuth, persistent.KeyGitlabUser, []byte(login))
		if err != nil {
			return err
		}
		//encPass, _ := persistent.Encrypt(key, pass)
		err = storage.Set(persistent.BucketAuth, persistent.KeyGitlabPass, []byte(pass))
		if err != nil {
			return err
		}
	}

	fmt.Printf("Connecting to '%s'...\n", ep)
	git, err := gitlab.NewBasicAuthClient(nil, ep, login, pass)
	if err != nil {
		return err
	}

	switch {
	default:
		projectID, issueID, err := parseProjectAndIssueID(fl.ProjectID, fl.IssueID)
		if err != nil {
			return err
		}

		return listGitProjectIssues(git, projectID, issueID, fl.All)
	case fl.Show:
		projectID, issueID, err := parseProjectAndIssueID(fl.ProjectID, fl.IssueID)
		if err != nil {
			return err
		}

		return getGitFullProjectIssue(git, projectID, issueID[0])
	case fl.Assigned:
		return listGitIssues(git, fl.All)
	case fl.Projects:
		return listGitProjects(git, fl.Limit)
	}
	return nil
}

func fetchGitCredsFromStorage(s *persistent.Storage, key []byte) (string, string, error) {
	loginEnc, err := s.Get(persistent.BucketAuth, persistent.KeyGitlabUser)
	if err != nil {
		return "", "", err
	}

	passEnc, err := s.Get(persistent.BucketAuth, persistent.KeyGitlabPass)
	if err != nil {
		return "", "", err
	}
	//login, err := persistent.Decrypt(key, loginEnc)
	//if err != nil {
	//	return "", "", err
	//}
	//pass, err := persistent.Decrypt(key, passEnc)
	//if err != nil {
	//	return "", "", err
	//}
	//return string(login), string(pass), nil
	return string(loginEnc), string(passEnc), nil
}

func listGitProjects(git *gitlab.Client, limit int) error {
	fmt.Printf("Fetching GitLab projects...\n")

	opt := new(gitlab.ListProjectsOptions)
	//opt.Owned = gitlab.Bool(true)
	opt.Membership = gitlab.Bool(true)
	opt.OrderBy = gitlab.String("last_activity_at")
	// todo make configurable
	opt.PerPage = limit

	projects, resp, err := git.Projects.ListProjects(opt)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		fmt.Printf("Request ended with %d %s", resp.StatusCode, resp.Status)
		return errors.New("bad response")
	}
	fmt.Printf("Fetched %d projects.\n\n", len(projects))

	out := tablewriter.NewWriter(os.Stdout)
	out.SetHeader([]string{"ID", "Name", "Description", "Link"})
	out.SetBorder(false)
	out.SetAutoFormatHeaders(false)
	out.SetAutoWrapText(false)
	for _, p := range projects {
		out.Append([]string{fmt.Sprintf("%d", p.ID), p.Name,
			truncateString(p.Description, 80), p.WebURL})
	}
	out.Render()

	return nil
}

func listGitProjectIssues(git *gitlab.Client, projectID int, issueID []int, all bool) error {
	fmt.Printf("Fetching GitLab issues by project %d...\n", projectID)
	opt := new(gitlab.ListProjectIssuesOptions)
	if len(issueID) != 0 {
		opt.IIDs = issueID
	}
	if !all {
		opt.State = gitlab.String("opened")
	}

	issues, resp, err := git.Issues.ListProjectIssues(projectID, opt)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		fmt.Printf("Request ended with %d %s", resp.StatusCode, resp.Status)
		return errors.New("bad response")
	}
	fmt.Printf("Fetched %d issues.\n\n", len(issues))
	for _, issue := range issues {
		fmt.Printf(" %5d [%d] (%s) | %s\n",
			issue.IID, issue.ProjectID, issue.State, issue.Title)
	}

	return nil
}

func listGitIssues(git *gitlab.Client, all bool) error {
	fmt.Printf("Fetching GitLab issues...\n")
	opt := new(gitlab.ListIssuesOptions)
	if !all {
		opt.State = gitlab.String("opened")
	}
	issues, resp, err := git.Issues.ListIssues(opt)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		fmt.Printf("Request ended with %d %s", resp.StatusCode, resp.Status)
		return errors.New("bad response")
	}
	fmt.Printf("Fetched %d issues.\n\n", len(issues))
	for _, issue := range issues {
		fmt.Printf(" %5d [%d] (%s) | %s\n",
			issue.IID, issue.ProjectID, issue.State, issue.Title)
	}

	return nil
}

func getGitFullProjectIssue(git *gitlab.Client, projectID int, issueID int) error {
	fmt.Printf("Fetching GitLab issue %d@%d...\n", issueID, projectID)
	opt := new(gitlab.ListProjectIssuesOptions)
	opt.IIDs = []int{issueID}

	//todo use git.Issues.GetIssue()
	issues, resp, err := git.Issues.ListProjectIssues(projectID, opt)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		fmt.Printf("Request ended with %d %s", resp.StatusCode, resp.Status)
		return errors.New("bad response")
	}
	issue := issues[0]

	notes, resp, err := git.Notes.ListIssueNotes(projectID, issueID, nil)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		fmt.Printf("Request ended with %d %s", resp.StatusCode, resp.Status)
		return errors.New("bad response")
	}
	output, err := ioutil.TempFile("", "jigit")
	if err != nil {
		//output = os.Stdout
	}
	defer os.Remove(output.Name())

	less := exec.Command("less", output.Name())
	less.Stdout = os.Stdout
	less.Stderr = os.Stderr

	// todo show project name
	// todo link jira issue

	fmt.Fprintf(output, " Issue #%d: %s (%s)\n\n Project:\t%d\n\n Jira task:\t%d\n"+
		" Assignee:\t%s\n Created at:\t%s\n Link:\t\t%s\n\n Tags: %s\n\n %s\n%s\n\n",
		issue.IID, issue.Title, strings.ToUpper(issue.State), issue.ProjectID,
		777, issue.Assignee.Name, issue.CreatedAt.Format(time.RFC850),
		issue.WebURL, issue.Labels,
		stringToFixedWidth(issue.Description, textWidthSize), sepIssue)
	for _, note := range notes {
		fmt.Fprintf(output, " [%d] %s%s @%s at %s\n\n %s\n%s\n",
			note.ID, printIfEdited(note.UpdatedAt.Equal(*note.CreatedAt)),
			note.Author.Name, note.Author.Username,
			note.CreatedAt.Format(commentTime),
			stringToFixedWidth(note.Body, textWidthSize), sepComment)
	}
	if err := less.Start(); err != nil {
		return err
	}
	less.Wait()
	output.Close()

	return nil
}

// gitlab helpers
func printIfEdited(cond bool) string {
	if cond {
		return ""
	}
	return "edited "
}

func parseProjectAndIssueID(pid string, iid []string) (int, []int, error) {
	// projectID should be string because JIRA id contains characters
	// we need to cast ProjectID and IssueID to int's
	projectID, err := strconv.Atoi(pid)
	if err != nil {
		fmt.Printf("Bad project ID: %s", err)
		return 0, nil, ErrBadArg
	}
	issueID := make([]int, 0)
	switch len(iid) {
	case 0:
		fmt.Println("You should provide at least one issue ID to fetch with -i or --issue flag.")
		return 0, nil, ErrBadArg
	case 1:
		// if it some id's but coma-separated?
		sp := strings.Split(iid[0], ",")
		if len(sp) != len(iid) {
			iid = sp
		}
	}
	for i := 0; i < len(iid); i++ {
		id, err := strconv.Atoi(strings.TrimSpace(iid[0]))
		if err != nil {
			fmt.Printf("Bad issue ID: %s", err)
			return 0, nil, ErrBadArg
		}
		issueID = append(issueID, id)
	}
	return projectID, issueID, nil
}
