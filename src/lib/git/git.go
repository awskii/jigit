package git

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"lib/storage"
	"lib/util"
	"subcmd/config"

	"github.com/pkg/errors"
	"github.com/xanzy/go-gitlab"
)

var (
	ErrBadEndpoint = errors.New("bad or empty endpoint")
)

type Git struct {
	endpoint string
	cfg      *config.Config
	storage  *storage.Storage
	client   *gitlab.Client
	ready    bool
}

func New() (*Git, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}

	storage, err := storage.NewStorage(cfg.Storage.Path)
	if err != nil {
		return nil, err
	}

	return &Git{cfg: cfg, storage: storage}, nil
}

func NewWithStorage(store *storage.Storage) (*Git, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	return &Git{cfg: cfg, storage: store}, nil
}

func (git *Git) User() (*User, error) {
	git.InitClient()

	u, resp, err := git.client.Users.CurrentUser()
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("bad response HTTP code")
	}
	return stripUser(u), nil
}

// Lazy gitlab client initialization
func (git *Git) InitClient() error {
	if git.ready {
		return nil
	}
	git.endpoint = git.cfg.GitLab.Address
	if git.endpoint == "" {
		return ErrBadEndpoint
	}

	//key := askPassphrase()
	key := []byte("key")
	login, pass, err := git.credentials(key)
	if err != nil {
		// unauthorized
		login, pass = util.AskCredentials(git.endpoint)
		//encLogin, _ := persistent.Encrypt(key, login)
		err := git.storage.Set(storage.BucketAuth, storage.KeyGitlabUser, []byte(login))
		if err != nil {
			return err
		}
		//encPass, _ := persistent.Encrypt(key, pass)
		err = git.storage.Set(storage.BucketAuth, storage.KeyGitlabPass, []byte(pass))
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

func (git *Git) Project(name string) (*Project, error) {
	fmt.Printf("Fetching GitLab project\n")

	proj, err := git.storage.Get(storage.BucketGitProjectCache, []byte(name))
	if err != nil {
		return git.fetchRemoteProject(name)
	}

	project := new(Project)
	if err := project.Decode(proj); err != nil {
		return nil, err
	}
	return project, nil
}

func (git *Git) Issue(id int) (*Issue, error) {
	fmt.Printf("Fetching GitLab issue '%d'\n", id)
	opt := &gitlab.ListIssuesOptions{IIDs: []int{id}}

	issue, resp, err := git.client.Issues.ListIssues(opt)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("bad status returned")
	}
	if len(issue) > 1 {
		return nil, errors.New("server respond with wrong issues count")
	}
	if len(issue) == 0 {
		return nil, errors.New("issue not found")
	}

	return compactIssues(issue)[0], nil
}

func (git *Git) CreateIssue(issue *Issue) (*Issue, error) {
	util.Debug("Creating issue at project %v", issue.ProjectID)
	opt := &gitlab.CreateIssueOptions{
		Title:       gitlab.String(issue.Title),
		Labels:      gitlab.Labels(issue.Labels),
		Description: gitlab.String(issue.Description),
		//AssigneeIDs: git.InitClient()
	}

	newIssue, resp, err := git.client.Issues.CreateIssue(issue.ProjectID, opt)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusCreated {
		return nil, errors.Errorf("unexpected HTTP status %d returned", resp.StatusCode)
	}
	return compactIssues([]*gitlab.Issue{newIssue})[0], nil
}

func (git *Git) DeleteIssue(pid, iid int) error {
	resp, err := git.client.Issues.DeleteIssue(pid, iid)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return errors.New("bad status returned")
	}
	return nil
}

func (git *Git) UpdateIssue(issue *Issue) (*Issue, error) {
	opt := &gitlab.UpdateIssueOptions{
		Title:       gitlab.String(issue.Title),
		Description: gitlab.String(issue.Description),
		Labels:      issue.Labels,
		StateEvent:  gitlab.String(issue.State),
	}
	newIssue, resp, err := git.client.Issues.UpdateIssue(issue.ProjectID, issue.IID, opt)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("bad status returned")
	}
	return compactIssues([]*gitlab.Issue{newIssue})[0], nil
}

func (git *Git) Comment(pid, issueID int, message string) (int, error) {
	git.InitClient()

	opt := &gitlab.CreateIssueNoteOptions{Body: gitlab.String(message)}
	c, resp, err := git.client.Notes.CreateIssueNote(pid, issueID, opt)
	if err != nil {
		return 0, err
	}
	if resp.StatusCode != http.StatusCreated {
		return 0, errors.Errorf("unexpected response code %d", resp.StatusCode)
	}
	return c.ID, nil
}

func (git *Git) DeleteComment(pid, issueID, commentID int) error {
	git.InitClient()

	resp, err := git.client.Notes.DeleteIssueNote(pid, issueID, commentID)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		fmt.Println(resp.Status)
		return errors.Errorf("unexpected response code %d", resp.StatusCode)
	}
	return nil
}

func (git *Git) fetchRemoteProject(name string) (*Project, error) {
	if err := git.InitClient(); err != nil {
		return nil, err
	}
	p, resp, err := git.client.Projects.GetProject(name)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("bad status returned")
	}
	return newProject(p), nil
}

func (git *Git) ListProjects(limit int, noCache bool) ([]*Project, error) {
	fmt.Printf("Fetching GitLab projects\n")

	projects, err := git.loadProjects()
	if err != nil {
		fmt.Println("cache error:", err)
	}
	if noCache || projects == nil || len(projects) == 0 {
		if err = git.InitClient(); err != nil {
			return nil, err
		}

		opt := &gitlab.ListProjectsOptions{
			//Owned : gitlab.Bool(true),
			Membership: gitlab.Bool(true),
			OrderBy:    gitlab.String("last_activity_at"),
		}
		opt.PerPage = limit // todo make configurable

		proj, resp, err := git.client.Projects.ListProjects(opt)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			fmt.Printf("Request ended with %d %s", resp.StatusCode, resp.Status)
			return nil, errors.New("bad response")
		}
		projects = compactProjects(proj)
		err = git.storeProjects(projects)
		if err != nil {
			fmt.Println("DEBUG: storing error:", err)
		}
		fmt.Printf("Fetched %s.\n\n",
			util.Plural(len(projects), "project", ""))
	}

	return projects, nil
}

func (git *Git) ListProjectIssues(pid int, all bool) ([]*Issue, error) {
	git.InitClient()

	name, _ := git.ProjectNameByID(pid)
	fmt.Printf("Fetching GitLab issues by project %q <%d>\n", name, pid)
	opt := new(gitlab.ListProjectIssuesOptions)
	if !all {
		opt.State = gitlab.String("opened")
	}

	issues, resp, err := git.client.Issues.ListProjectIssues(pid, opt)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		fmt.Printf("Request ended with %d %s", resp.StatusCode, resp.Status)
		return nil, errors.New("bad response")
	}
	fmt.Printf("Fetched %s for project %q.\n\n",
		util.Plural(len(issues), "issue", ""), name)

	return compactIssues(issues), nil
}

func (git *Git) ListAssignedIssues(all bool) ([]*Issue, error) {
	git.InitClient()

	fmt.Printf("Fetching assigned to you GitLab issues\n")
	opt := new(gitlab.ListIssuesOptions)
	if !all {
		opt.State = gitlab.String("opened")
	}
	issues, resp, err := git.client.Issues.ListIssues(opt)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		fmt.Printf("Request ended with %d %s", resp.StatusCode, resp.Status)
		return nil, errors.New("bad response")
	}
	fmt.Printf("Fetched %s.\n\n", util.Plural(len(issues), "issue", "issues"))

	return compactIssues(issues), nil
}

func (git *Git) DetailedProjectIssue(pid int, issueID int) (*Issue, []*Comment, error) {
	git.InitClient()
	projectName, err := git.ProjectNameByID(pid)
	if err != nil {
		util.Debug("project name fetch err: %s", err)
		projectName = "unknown"
	}

	fmt.Printf("Fetching GitLab issue #%d on project %s <%d>\n", issueID, projectName, pid)
	opt := new(gitlab.ListProjectIssuesOptions)
	opt.IIDs = []int{issueID}

	//todo use git.Issues.GetIssue()
	issues, resp, err := git.client.Issues.ListProjectIssues(pid, opt)
	if err != nil {
		return nil, nil, err
	}
	if resp.StatusCode != http.StatusOK {
		fmt.Printf("Request ended with %d %s", resp.StatusCode, resp.Status)
		return nil, nil, errors.New("bad response")
	}
	issue := issues[0]

	notes, resp, err := git.client.Notes.ListIssueNotes(pid, issueID, nil)
	if err != nil {
		return nil, nil, err
	}
	if resp.StatusCode != http.StatusOK {
		fmt.Printf("Request ended with %d %s", resp.StatusCode, resp.Status)
		return nil, nil, errors.New("bad response")
	}

	return newIssue(issue), compactComments(notes), nil
}

func (git *Git) credentials(key []byte) (string, string, error) {
	loginEnc, err := git.storage.Get(storage.BucketAuth, storage.KeyGitlabUser)
	if err != nil {
		return "", "", err
	}

	passEnc, err := git.storage.Get(storage.BucketAuth, storage.KeyGitlabPass)
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
func (git *Git) GetPid(name string, pid int) (int, error) {
	if pid == 0 && name == "" {
		fmt.Fprintln(os.Stderr, "You should provide project name via -p or --project flag or project ID via --pid flag.")
		os.Exit(1)
	}

	if name != "" {
		p, err := git.ProjectByName(name, false, false)
		if err != nil {
			return 0, err
		}
		pid = p.ID
	}
	return pid, nil
}

func (git *Git) loadProjects() ([]*Project, error) {
	p := make([]*Project, 0)
	fn := func(k, v []byte) error {
		util.Debug("[CACHE] decoding project '%s'", string(k))
		gp := new(Project)
		err := gp.Decode(v)
		if err != nil {
			return err
		}
		p = append(p, gp)
		return nil
	}
	err := git.storage.ForEach(storage.BucketGitProjectCache, fn)
	if err != nil {
		return nil, err
	}
	if len(p) != 0 {
		util.Debug("[CACHE] Loaded %d projects\n", len(p))
	}
	return p, nil
}

func (git *Git) storeProjects(projects []*Project) error {
	buf := new(bytes.Buffer)
	for _, p := range projects {
		// save <PID, ProjectName> pair
		git.storage.Set(storage.BucketGitProjectCache,
			[]byte(strconv.Itoa(p.ID)), []byte(p.Name))

		util.Debug("[CACHE] encoding project %q", p.Name)
		err := p.Encode(buf)
		if err != nil {
			return errors.Wrapf(err, "can't encode '%s' project", p.Name)
		}
		err = git.storage.Set(storage.BucketGitProjectCache, []byte(p.Name), buf.Bytes())
		if err != nil {
			return errors.Wrapf(err, "can't store '%s' project", p.Name)
		}
		buf.Reset()
	}
	return nil
}

// Try to get name from storage. If data not found, try to fetch it from remote
func (git *Git) ProjectNameByID(pid int) (string, error) {
	name, err := git.storage.Get(storage.BucketGitProjectCache, []byte(strconv.Itoa(pid)))
	if err == nil { // found
		return string(name), nil
	}
	// not found, check remote
	git.InitClient()
	p, resp, err := git.client.Projects.GetProject(pid, nil)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", errors.Errorf(resp.Status)
	}
	git.storeProjects([]*Project{newProject(p)})
	return p.Name, nil
}

// Try to get project from storage. If no data found, try to fetch it from remote
func (git *Git) ProjectByName(name string, noCache, alike bool) (*Project, error) {
	p := new(Project)
	if !noCache {
		util.Debug("[CACHE] lookup git project by name '%s'", name)
		b, err := git.storage.Get(storage.BucketGitProjectCache, []byte(name))
		if err != nil {
			goto fetchRemote
		}
		err = p.Decode(b)
		if err != nil {
			util.Debug("[CACHE] project '%s' not found", name)
			goto fetchRemote
		}
		if p != nil && p.Name == name {
			util.Debug("[CACHE] project '%s' found", name)
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

	projects := compactProjects(proj)
	err = git.storeProjects(projects)
	if err != nil {
		return nil, err
	}

	for i := 0; i < len(projects); i++ {
		if projects[i].Name == name {
			return projects[i], nil
		}
		if alike && strings.Contains(projects[i].Name, name) {
			return projects[i], nil
		}
	}
	return nil, errors.New("project not found")
}

// Todo make it more granular
func (git *Git) InvalidateCache() {
	git.storage.Invalidate(storage.BucketGitProjectCache)
	git.storage.Invalidate(storage.BucketGitIssueCache)
}

func (git *Git) Destruct() {
	git.storage.Close()
}

// lightweight structure to store only valuable data
type Project struct {
	Name        string
	Link        string
	Description string
	WebURL      string
	ID          int
}

func newProject(p *gitlab.Project) *Project {
	if p == nil {
		return new(Project)
	}
	return &Project{
		Name:        p.Name,
		Link:        p.WebURL,
		Description: p.Description,
		WebURL:      p.WebURL,
		ID:          p.ID,
	}
}

func compactProjects(p []*gitlab.Project) []*Project {
	res := make([]*Project, len(p))
	for i := 0; i < len(p); i++ {
		res[i] = newProject(p[i])
	}
	return res
}

func (p *Project) Encode(into io.Writer) error {
	return gob.NewEncoder(into).Encode(p)
}

func (p *Project) Decode(v []byte) error {
	return gob.NewDecoder(bytes.NewBuffer(v)).Decode(p)
}

type Issue struct {
	IID              int
	Title            string
	State            string
	Labels           []string
	ProjectID        int
	AssigneeName     string
	AssigneeUsername string
	CreatedAt        time.Time
	WebURL           string
	Description      string

	LinkedJiraIssueKey []byte
}

func newIssue(i *gitlab.Issue) *Issue {
	if i == nil {
		return new(Issue)
	}
	return &Issue{
		IID:              i.IID,
		Title:            i.Title,
		State:            i.State,
		Labels:           i.Labels,
		ProjectID:        i.ProjectID,
		AssigneeName:     i.Assignee.Name,
		AssigneeUsername: i.Assignee.Username,
		CreatedAt:        *i.CreatedAt,
		WebURL:           i.WebURL,
		Description:      i.Description,
	}
}

func compactIssues(l []*gitlab.Issue) []*Issue {
	issues := make([]*Issue, len(l))
	for i := 0; i < len(l); i++ {
		issues[i] = newIssue(l[i])
	}
	return issues
}

func (i *Issue) Encode(into io.Writer) error {
	return gob.NewEncoder(into).Encode(i)
}

func (i *Issue) Decode(v []byte) error {
	return gob.NewDecoder(bytes.NewBuffer(v)).Decode(i)
}

type Comment struct {
	ID             int
	AuthorName     string
	AuthorUsername string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	System         bool
	Body           string
}

func newComment(c *gitlab.Note) *Comment {
	if c == nil {
		return new(Comment)
	}
	return &Comment{
		ID:             c.ID,
		AuthorName:     c.Author.Name,
		AuthorUsername: c.Author.Username,
		CreatedAt:      *c.CreatedAt,
		UpdatedAt:      *c.UpdatedAt,
		System:         c.System,
		Body:           c.Body,
	}
}

func compactComments(l []*gitlab.Note) []*Comment {
	comments := make([]*Comment, len(l))
	for i := 0; i < len(l); i++ {
		comments[i] = newComment(l[i])
	}
	return comments
}

func (c *Comment) Encode(into io.Writer) error {
	return gob.NewEncoder(into).Encode(c)
}

func (c *Comment) Decode(v []byte) error {
	return gob.NewDecoder(bytes.NewBuffer(v)).Decode(c)
}

type User struct {
	ID    int
	Name  string
	Login string
	Token string
}

func stripUser(u *gitlab.Session) *User {
	return &User{
		ID:    u.ID,
		Name:  u.Name,
		Login: u.Username,
		Token: u.PrivateToken,
	}
}
