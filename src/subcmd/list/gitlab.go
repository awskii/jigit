package list

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"io"
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

func debug(format string, argv ...interface{}) {
	fmt.Printf("[DEBUG] "+format+"\n", argv...)
}

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

	if fl.NoCache {
		debug("[CACHE] invalidating git cache")
		git.storage.Invalidate(persistent.BucketGitProjectCache)
		git.storage.Invalidate(persistent.BucketGitIssueCache)
	}

	switch {
	default:
		pid, err := git.getPid(fl.ProjectName, fl.ProjectID)
		if err != nil {
			return err
		}
		return git.ListProjectIssues(pid, fl.All)
	case len(fl.IssueID) != 0:
		issueID, err := parseIssueID(fl.IssueID)
		if err != nil {
			return err
		}
		pid, err := git.getPid(fl.ProjectName, fl.ProjectID)
		if err != nil {
			return err
		}

		return git.DetailedProjectIssue(pid, issueID[0])
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
	ready    bool
}

// Lazy gitlab client initialization
func (git *Git) InitClient() error {
	if git.ready {
		return nil
	}
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

	fmt.Printf("Connecting to '%s'\n", git.endpoint)
	git.client, err = gitlab.NewBasicAuthClient(nil, git.endpoint, login, pass)
	if err != nil {
		return err
	}
	git.ready = true
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

func (git *Git) ListProjectIssues(pid int, all bool) error {
	git.InitClient()

	name, _ := git.projectNameByID(pid)
	fmt.Printf("Fetching GitLab issues by project %q <%d>\n", name, pid)
	opt := new(gitlab.ListProjectIssuesOptions)
	if !all {
		opt.State = gitlab.String("opened")
	}

	issues, resp, err := git.client.Issues.ListProjectIssues(pid, opt)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		fmt.Printf("Request ended with %d %s", resp.StatusCode, resp.Status)
		return errors.New("bad response")
	}
	fmt.Printf("Fetched %d %s for project %q.\n\n", len(issues),
		PluralWord(len(issues), "issue", ""), name)

	for _, issue := range issues {
		fmt.Printf(" %5d (%s) | %s\n", issue.IID, strings.ToUpper(issue.State), issue.Title)
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

func (git *Git) DetailedProjectIssue(pid int, issueID int) error {
	git.InitClient()

	fmt.Printf("Fetching GitLab issue %d@%d...\n", issueID, pid)
	opt := new(gitlab.ListProjectIssuesOptions)
	opt.IIDs = []int{issueID}

	//todo use git.Issues.GetIssue()
	issues, resp, err := git.client.Issues.ListProjectIssues(pid, opt)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		fmt.Printf("Request ended with %d %s", resp.StatusCode, resp.Status)
		return errors.New("bad response")
	}
	issue := issues[0]

	notes, resp, err := git.client.Notes.ListIssueNotes(pid, issueID, nil)
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

// If name is empty, provided pid will be returned.
// Pid validation will be made on further stages.
func (git *Git) getPid(name string, pid int) (int, error) {
	if pid == 0 && name == "" {
		fmt.Printf("You should provide project name via -p or --project flag\n\n\t\tor\n\n" +
			"project ID via --pid flag")
		return 0, ErrBadArg
	}

	if name != "" {
		p, err := git.projectByName(name, false, false)
		if err != nil {
			return 0, err
		}
		pid = p.ID
	}
	return pid, nil
}

func (git *Git) loadProjects() ([]*gitlab.Project, error) {
	p := make([]*gitlab.Project, 0)
	fn := func(k, v []byte) error {
		debug("[CACHE] decoding project '%s'", string(k))
		gp := new(GitProject)
		err := gp.Decode(v)
		if err != nil {
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
		debug("[CACHE] Loaded %d projects\n", len(p))
	}
	return p, nil
}

func (git *Git) storeProjects(projects []*gitlab.Project) error {
	buf := new(bytes.Buffer)
	for _, p := range projects {
		// save <PID, ProjectName> pair
		git.storage.Set(persistent.BucketGitProjectCache,
			[]byte(strconv.Itoa(p.ID)), []byte(p.Name))

		debug("[CACHE] encoding project %q", p.Name)
		err := NewGitProject(p).Encode(buf)
		if err != nil {
			return errors.Wrapf(err, "can't encode '%s' project", p.Name)
		}
		err = git.storage.Set(persistent.BucketGitProjectCache, []byte(p.Name), buf.Bytes())
		if err != nil {
			return errors.Wrapf(err, "can't store '%s' project", p.Name)
		}
		buf.Reset()
	}
	return nil
}

func (git *Git) projectNameByID(pid int) (string, error) {
	name, err := git.storage.Get(persistent.BucketGitProjectCache,
		[]byte(strconv.Itoa(pid)))
	if err != nil {
		return "", err
	}
	return string(name), nil
}

func (git *Git) projectByName(name string, noCache, alike bool) (*gitlab.Project, error) {
	var p *gitlab.Project
	if !noCache {
		debug("[CACHE] lookup git project by name '%s'", name)
		b, err := git.storage.Get(persistent.BucketGitProjectCache, []byte(name))
		if err != nil {
			goto fetchRemote
		}
		gp := new(GitProject)
		err = gp.Decode(b)
		if err != nil {
			debug("[CACHE] project '%s' not found", name)
			goto fetchRemote
		}
		p = gp.Extend()
		if p != nil && p.Name == name {
			debug("[CACHE] project '%s' found", name)
			return p, nil
		}
	}

fetchRemote:
	git.InitClient()
	opt := &gitlab.ListProjectsOptions{Search: gitlab.String(name)}
	proj, resp, err := git.client.Projects.ListProjects(opt, nil)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("bad response code")
	}

	err = git.storeProjects(proj)
	if err != nil {
		return nil, err
	}

	for i := 0; i < len(proj); i++ {
		if proj[i].Name == name {
			return proj[i], nil
		}
		if alike && strings.Contains(proj[i].Name, name) {
			return proj[i], nil
		}
	}
	return nil, errors.New("project not found")
}

// lightweight structure to store only valuable data
type GitProject struct {
	Name        string
	Link        string
	Description string
	ID          int
}

func NewGitProject(p *gitlab.Project) *GitProject {
	if p == nil {
		return new(GitProject)
	}
	return &GitProject{
		Name:        p.Name,
		Link:        p.WebURL,
		Description: p.Description,
		ID:          p.ID,
	}
}

func (p *GitProject) Extend() *gitlab.Project {
	return &gitlab.Project{
		Name:        p.Name,
		WebURL:      p.Link,
		Description: p.Description,
		ID:          p.ID,
	}
}

func (p *GitProject) Encode(into io.Writer) error {
	return gob.NewEncoder(into).Encode(p)
}

func (p *GitProject) Decode(v []byte) error {
	return gob.NewDecoder(bytes.NewBuffer(v)).Decode(p)
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
