package list

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"lib/less"
	"lib/persistent"
	"subcmd/config"

	"github.com/olekukonko/tablewriter"
	"github.com/pkg/errors"
	"github.com/xanzy/go-gitlab"
)

func proceedGit(fl Subcmd) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	storage, err := persistent.NewStorage(cfg.Persistent.Path)
	if err != nil {
		return err
	}
	defer storage.Close()

	git := &Git{cfg: cfg, storage: storage}

	switch {
	default:
		if fl.ProjectID == "" {
			fmt.Printf("You should provide project ID via -p or --project flag")
		}
		projectID, err := strconv.Atoi(fl.ProjectID)
		if err != nil {
			fmt.Printf("Bad project ID: %s", err)
			return err
		}

		return git.ListProjectIssues(projectID, fl.All)
	case len(fl.IssueID) != 0:
		projectID, err := strconv.Atoi(fl.ProjectID)
		if err != nil {
			fmt.Printf("Bad project ID: %s", err)
			return err
		}
		issueID, err := parseIssueID(fl.IssueID)
		if err != nil {
			return err
		}

		return git.DetailedProjectIssue(projectID, issueID[0])
	case fl.Assigned:
		return git.ListAssignedIssues(fl.All)
	case fl.Projects:
		return git.ListProjects(fl.Limit, fl.NoCache)
	}
	return nil
}

type Git struct {
	endpoint string
	cfg      *config.Config
	storage  *persistent.Storage
	client   *gitlab.Client
}

// Lazy gitlab client initialization
func (git *Git) InitClient() error {
	git.endpoint = git.cfg.GitLab.Address
	if git.endpoint == "" {
		return ErrBadAddress
	}

	//key := askPassphrase()
	key := []byte("key")
	login, pass, err := git.credentials(key)
	if err != nil {
		// unauthorized
		login, pass = askCredentials(git.endpoint)
		//encLogin, _ := persistent.Encrypt(key, login)
		err := git.storage.Set(persistent.BucketAuth, persistent.KeyGitlabUser, []byte(login))
		if err != nil {
			return err
		}
		//encPass, _ := persistent.Encrypt(key, pass)
		err = git.storage.Set(persistent.BucketAuth, persistent.KeyGitlabPass, []byte(pass))
		if err != nil {
			return err
		}
	}

	fmt.Printf("Connecting to '%s'...\n", git.endpoint)
	git.client, err = gitlab.NewBasicAuthClient(nil, git.endpoint, login, pass)
	if err != nil {
		return err
	}
	return nil
}

func (git *Git) ListProjects(limit int, noCache bool) error {
	fmt.Printf("Fetching GitLab projects...\n")

	projects, err := git.loadProjects()
	if err != nil {
		fmt.Println("cache error:", err)
	}
	if noCache || projects == nil || len(projects) == 0 {
		if err = git.InitClient(); err != nil {
			return err
		}

		opt := &gitlab.ListProjectsOptions{
			//Owned : gitlab.Bool(true),
			Membership: gitlab.Bool(true),
			OrderBy:    gitlab.String("last_activity_at"),
		}
		opt.PerPage = limit // todo make configurable

		var resp *gitlab.Response
		projects, resp, err = git.client.Projects.ListProjects(opt)
		if err != nil {
			return err
		}
		if resp.StatusCode != http.StatusOK {
			fmt.Printf("Request ended with %d %s", resp.StatusCode, resp.Status)
			return errors.New("bad response")
		}
		err = git.storeProjects(projects)
		if err != nil {
			fmt.Println("DEBUG: storing error:", err)
		}
		fmt.Printf("Fetched %d projects.\n\n", len(projects))
	}

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

func (git *Git) ListProjectIssues(projectID int, all bool) error {
	git.InitClient()

	fmt.Printf("Fetching GitLab issues by project %d...\n", projectID)
	opt := new(gitlab.ListProjectIssuesOptions)
	if !all {
		opt.State = gitlab.String("opened")
	}

	issues, resp, err := git.client.Issues.ListProjectIssues(projectID, opt)
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

func (git *Git) ListAssignedIssues(all bool) error {
	git.InitClient()
	fmt.Printf("Fetching GitLab issues...\n")
	opt := new(gitlab.ListIssuesOptions)
	if !all {
		opt.State = gitlab.String("opened")
	}
	issues, resp, err := git.client.Issues.ListIssues(opt)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		fmt.Printf("Request ended with %d %s", resp.StatusCode, resp.Status)
		return errors.New("bad response")
	}
	fmt.Printf("Fetched %d %s.\n\n", len(issues),
		PluralWord(len(issues), "issue", "issues"))
	for _, issue := range issues {
		fmt.Printf(" #%5d at [%d] (%s) - %s\n",
			issue.IID, issue.ProjectID, issue.State, issue.Title)
	}

	return nil
}

func (git *Git) DetailedProjectIssue(projectID int, issueID int) error {
	git.InitClient()

	fmt.Printf("Fetching GitLab issue %d@%d...\n", issueID, projectID)
	opt := new(gitlab.ListProjectIssuesOptions)
	opt.IIDs = []int{issueID}

	//todo use git.Issues.GetIssue()
	issues, resp, err := git.client.Issues.ListProjectIssues(projectID, opt)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		fmt.Printf("Request ended with %d %s", resp.StatusCode, resp.Status)
		return errors.New("bad response")
	}
	issue := issues[0]

	notes, resp, err := git.client.Notes.ListIssueNotes(projectID, issueID, nil)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		fmt.Printf("Request ended with %d %s", resp.StatusCode, resp.Status)
		return errors.New("bad response")
	}
	sort.Sort(GitCommentsTimeSort(notes))

	out, err := less.NewFile()
	if err != nil {
		// fixme shitty error handling
		//out.File = os.Stdout
	}

	// todo show project name
	// todo link jira issue

	fmt.Fprintf(out, " Issue #%d: %s (%s) tags: %s\n\n Project:\t%d\n Jira task:\t%d\n"+
		" Assignee:\t%s\n Created at:\t%s (%s)\n Link:\t\t%s\n\n %s%s\n",
		issue.IID, issue.Title, strings.ToUpper(issue.State), issue.Labels,
		issue.ProjectID, 777, issue.Assignee.Name, issue.CreatedAt.Format(time.RFC850),
		RelativeTime(*issue.CreatedAt),
		issue.WebURL, stringToFixedWidth(issue.Description, textWidthSize), sepIssue)
	for _, note := range notes {
		fmt.Fprintf(out, " [%d] %s%s @%s wrote %s\n\n %s\n%s",
			note.ID, printIfEdited(note.UpdatedAt.Equal(*note.CreatedAt)),
			note.Author.Name, note.Author.Username, RelativeTime(*note.CreatedAt),
			stringToFixedWidth(note.Body, textWidthSize), sepComment)
	}
	if err := out.Render(); err != nil {
		return err
	}
	out.Wait()
	out.Close()

	return nil
}

func (git *Git) credentials(key []byte) (string, string, error) {
	loginEnc, err := git.storage.Get(persistent.BucketAuth, persistent.KeyGitlabUser)
	if err != nil {
		return "", "", err
	}

	passEnc, err := git.storage.Get(persistent.BucketAuth, persistent.KeyGitlabPass)
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

func (git *Git) storeProjects(p []*gitlab.Project) error {
	buf := new(bytes.Buffer)

	for i := 0; i < len(p); i++ {
		gp := NewGitProject(p[i])
		err := gob.NewEncoder(buf).Encode(*gp)
		if err != nil {
			return errors.Wrapf(err, "can't encode '%s' project", gp.Name)
		}
		err = git.storage.Set(persistent.BucketGitProjectCache, []byte(gp.Name), buf.Bytes())
		if err != nil {
			return errors.Wrapf(err, "can't store '%s' project", gp.Name)
		}
		buf.Reset()
	}
	return nil
}

func (git *Git) loadProjects() ([]*gitlab.Project, error) {
	p := make([]*gitlab.Project, 0, 8)
	fn := func(k, v []byte) error {
		fmt.Printf("decoding %s\n", string(k))
		gp := new(GitProject)
		buf := bytes.NewBuffer(v)
		if err := gob.NewDecoder(buf).Decode(gp); err != nil {
			return err
		}
		p = append(p, gp.Extend())
		return nil
	}
	err := git.storage.ForEach(persistent.BucketGitProjectCache, fn)
	if err != nil {
		return nil, err
	}
	if len(p) != 0 {
		fmt.Printf("Loaded %d projects from cache.\n\n", len(p))
	}
	return p, nil
}

// lightweight structure to store only valuable data
type GitProject struct {
	Name        string
	Link        string
	Description string
	ID          int
}

func NewGitProject(p *gitlab.Project) *GitProject {
	return &GitProject{
		Name:        p.Name,
		Link:        p.WebURL,
		Description: p.Description,
		ID:          p.ID,
	}
}

// unmarshal to existing object without malloc
func (p *GitProject) Unmarshal(v *gitlab.Project) {
	v.Name = p.Name
	v.WebURL = p.Link
	v.Description = p.Description
	v.ID = p.ID
}

// alloc new *Project and put data into it
func (p *GitProject) Extend() *gitlab.Project {
	return &gitlab.Project{
		Name:        p.Name,
		WebURL:      p.Link,
		Description: p.Description,
		ID:          p.ID,
	}
}

// gitlab helpers
func printIfEdited(cond bool) string {
	if cond {
		return ""
	}
	return "edited "
}

func parseIssueID(iid []string) ([]int, error) {
	// projectID should be string because JIRA id contains characters
	// we need to cast ProjectID and IssueID to int's
	issueID := make([]int, 0)
	switch len(iid) {
	case 0:
		fmt.Println("You should provide at least one issue ID to fetch with -i or --issue flag.")
		return nil, ErrBadArg
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
			return nil, ErrBadArg
		}
		issueID = append(issueID, id)
	}
	return issueID, nil
}

type GitCommentsTimeSort []*gitlab.Note

func (c GitCommentsTimeSort) Less(i, j int) bool {
	return c[i].CreatedAt.Before(*c[j].CreatedAt)
}

func (c GitCommentsTimeSort) Swap(i, j int) {
	c[i], c[j] = c[j], c[i]
}

func (c GitCommentsTimeSort) Len() int {
	return len(c)
}
