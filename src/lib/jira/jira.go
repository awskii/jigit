package jira

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"lib/storage"
	"lib/util"
	"subcmd/config"

	"github.com/andygrunwald/go-jira"
	"github.com/pkg/errors"
)

var ErrBadEndpoint = errors.New("bad or empty endpoint")

type Jira struct {
	endpoint string
	cfg      *config.Config
	storage  *storage.Storage
	client   *jira.Client
}

func New() (*Jira, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	storage, err := storage.NewStorage(cfg.Storage.Path)
	if err != nil {
		return nil, err
	}

	return &Jira{cfg: cfg, storage: storage}, nil
}

func NewWithStorage(store *storage.Storage) (*Jira, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	return &Jira{cfg: cfg, storage: store}, nil
}

type Kind uint8

const (
	kindInvalid Kind = iota
	KindBug
	KindFeature
	KindEnhancement
)

type RemoteTag struct {
	ID    string
	Label string
	Kind  Kind
}

func detectKind(k string) Kind {
	switch strings.ToUpper(k) {
	case "BUG":
		return KindBug
	case "FEATURE":
		return KindFeature
	case "ENHANCEMENT":
		return KindEnhancement
	}
	return kindInvalid
}

func (j *Jira) RemoteTags(projectKey string) ([3]*RemoteTag, error) {
	meta, resp, err := j.client.Issue.GetCreateMeta(projectKey)
	if err != nil {
		return [3]*RemoteTag{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return [3]*RemoteTag{}, err
	}

	var rtags [3]*RemoteTag
	var pos int

	for _, pm := range meta.Projects {
		for _, it := range pm.IssueTypes {
			kind := detectKind(it.Name)
			if kind == kindInvalid {
				continue
			}
			rtags[pos] = &RemoteTag{
				ID:    it.Id,
				Label: it.Name,
				Kind:  kind,
			}
			pos++
		}
	}
	return rtags, nil
}

func (j *Jira) User() (*User, error) {
	j.InitClient()
	u, resp, err := j.client.User.GetSelf()
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("got bad response code")
	}
	user := stripUser(u)
	return &user, nil
}

func (j *Jira) InitClient() error {
	ep := j.cfg.Jira.Address
	if ep == "" {
		return ErrBadEndpoint
	}

	//key := askPassphrase()
	key := []byte("key")
	// todo fix cred issuer
	login, pass, err := j.credentials(key)
	if err != nil {
		// Todo sanitize error
		fmt.Println(err)
		// unauthorized
		login, pass = util.AskCredentials(ep)
		//encLogin, _ := persistent.Encrypt(key, login)
		err := j.storage.Set(storage.BucketAuth, storage.KeyGitlabUser, []byte(login))
		if err != nil {
			return err
		}
		//encPass, _ := persistent.Encrypt(key, pass)
		err = j.storage.Set(storage.BucketAuth, storage.KeyGitlabPass, []byte(pass))
		if err != nil {
			return err
		}
	}
	tp := jira.BasicAuthTransport{
		Username: login,
		Password: pass,
	}

	fmt.Printf("Connecting to '%s'\n", ep)
	jrcli, err := jira.NewClient(tp.Client(), ep)
	if err != nil {
		return err
	}
	j.client = jrcli
	return nil
}

func (j *Jira) Endpoint() string {
	return j.endpoint
}

func (j *Jira) Issue(issueID string) (*Issue, error) {
	issue := new(Issue)

	issueRaw, err := j.storage.Get(storage.BucketJiraIssueCache, []byte(issueID))
	if err != nil {
		err := j.InitClient()
		if err != nil {
			return nil, err
		}

		is, resp, err := j.client.Issue.Get(issueID, nil)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			return nil, errors.New("bad status returned")
		}
		issue = stripIssue(is)
		buf := new(bytes.Buffer)

		if err = issue.Encode(buf); err != nil {
			util.Debug("issue encode failed: %s", err)
		} else {
			err = j.storage.Set(storage.BucketJiraIssueCache, []byte(issue.Key), buf.Bytes())
			if err != nil {
				util.Debug("issue store failed: %s", err)
			}
		}
	} else {
		if err = issue.Decode(issueRaw); err != nil {
			util.Debug("issue decode failed: %s", err)
		}

		if issue == nil {
			util.Debug("issue is nil :(")
		}
	}

	return issue, nil
}

func (j *Jira) CreateIssue(issue *Issue) (*Issue, error) {
	meta, resp, err := j.client.Issue.GetCreateMeta("STORAGE")
	if err != nil {
		return nil, err
	}

	m := meta.Projects[0]
	extendedIssue := extendIssue(issue)
	extendedIssue.Fields.Type = jira.IssueType{ID: "3"}
	extendedIssue.Fields.Project = jira.Project{Key: m.Key}

	is, resp, err := j.client.Issue.Create(extendedIssue)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusCreated {
		return nil, errors.New("bad status returned")
	}
	issue.Key = is.Key
	buf := new(bytes.Buffer)

	if err = issue.Encode(buf); err != nil {
		util.Debug("issue encode failed: %s", err)
	} else {
		err = j.storage.Set(storage.BucketJiraIssueCache, []byte(issue.Key), buf.Bytes())
		if err != nil {
			util.Debug("issue store failed: %s", err)
		}
	}
	return issue, nil
}

func (j *Jira) Comment(issueID, message string) error {
	j.InitClient()

	opt := &jira.Comment{Body: message}
	_, resp, err := j.client.Issue.AddComment(issueID, opt)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusCreated {
		return errors.Errorf("unexpected status code returned: %d", resp.StatusCode)
	}
	return nil
}

func (j *Jira) InvalidateCache() {
	j.storage.Invalidate(storage.BucketJiraIssueCache)
}

func (j *Jira) ListProjects() ([]*Project, error) {
	err := j.InitClient()
	if err != nil {
		return nil, err
	}

	list, resp, err := j.client.Project.GetList()
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("jira did not return 200")
	}
	return compactProjects(list), nil
}

func (j *Jira) ListAssignedIssues(projectName string) ([]*Issue, error) {
	err := j.InitClient()
	if err != nil {
		return nil, err
	}

	user, resp, err := j.client.User.GetSelf()
	if err != nil {
		return nil, err
	}
	q := fmt.Sprintf("assignee = %s AND status not in (Closed, Resolved)", user.Key)
	if projectName != "" {
		q += fmt.Sprintf(" AND project = %s", projectName)
	}
	issues, resp, err := j.client.Issue.Search(q, nil)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("bad status returned")
	}
	return compactIssues(issues), nil
}

func (j *Jira) Destruct() {
	j.storage.Close()
}

func (j *Jira) credentials(key []byte) (string, string, error) {
	// TODO fix keys
	loginEnc, err := j.storage.Get(storage.BucketAuth, storage.KeyGitlabUser)
	if err != nil {
		return "", "", err
	}

	// TODO fix keys
	passEnc, err := j.storage.Get(storage.BucketAuth, storage.KeyGitlabPass)
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

type Project struct {
	ID   string
	Key  string
	Name string
}

func stripProject(p *jira.Project) *Project {
	return &Project{
		ID:   p.ID,
		Key:  p.Key,
		Name: p.Name,
	}
}

func compactProjects(p *jira.ProjectList) []*Project {
	res := make([]*Project, len(*p))
	for i := 0; i < len(res); i++ {
		res[i] = &Project{
			Name: (*p)[i].Name,
			Key:  (*p)[i].Key,
			ID:   (*p)[i].ID,
		}
	}
	return res
}

type Issue struct {
	Key          string
	Summary      string
	Description  string
	Created      time.Time
	Subtasks     []Subtask
	IssueLinks   []IssueLink
	Comments     []Comment
	Assignee     User
	Creator      User
	StatusName   string
	ParentKey    string
	PriorityName string
}

func extendIssue(i *Issue) *jira.Issue {
	return &jira.Issue{
		Fields: &jira.IssueFields{
			Summary:     i.Summary,
			Description: i.Description,
			Assignee:    extendUser(&i.Assignee),
			//Status: i.StatusName,
			//Type: i.Type,
		},
	}
}

func compactIssues(li []jira.Issue) []*Issue {
	res := make([]*Issue, len(li))
	for i := 0; i < len(res); i++ {
		res[i] = stripIssue(&li[i])
	}
	return res
}

func stripIssue(i *jira.Issue) (ji *Issue) {
	if i == nil {
		return
	}

	ji = &Issue{
		Key:          i.Key,
		Assignee:     stripUser(i.Fields.Assignee),
		Creator:      stripUser(i.Fields.Creator),
		Summary:      i.Fields.Summary,
		Description:  i.Fields.Description,
		Created:      time.Time(i.Fields.Created),
		StatusName:   i.Fields.Status.Name,
		PriorityName: i.Fields.Priority.Name,
		IssueLinks:   stripIssueLinks(i.Fields.IssueLinks),
	}

	if i.Fields.Parent != nil {
		ji.ParentKey = i.Fields.Parent.Key
	}
	if i.Fields.Comments != nil {
		ji.Comments = stripComments(i.Fields.Comments.Comments)
	}
	if i.Fields.Subtasks != nil {
		ji.Subtasks = stripSubtasks(i.Fields.Subtasks)
	}

	return ji
}

func (i *Issue) Encode(into io.Writer) error {
	return gob.NewEncoder(into).Encode(i)
}

func (i *Issue) Decode(v []byte) error {
	return gob.NewDecoder(bytes.NewBuffer(v)).Decode(i)
}

type IssueLink struct {
	Type struct {
		Inward  string
		Outward string
	}
	OutwardIssue Issue
	InwardIssue  Issue
}

func stripIssueLinks(l []*jira.IssueLink) []IssueLink {
	if l == nil {
		return nil
	}
	il := make([]IssueLink, len(l))
	for i := 0; i < len(l); i++ {
		il[i].Type.Inward = l[i].Type.Inward
		il[i].Type.Outward = l[i].Type.Outward
		if in := stripIssue(l[i].InwardIssue); in != nil {
			il[i].InwardIssue = *in
		}
		if out := stripIssue(l[i].OutwardIssue); out != nil {
			il[i].OutwardIssue = *out
		}
	}
	return il
}

type User struct {
	DisplayName string
	Name        string
}

func stripUser(u *jira.User) User {
	if u == nil {
		return User{}
	}
	return User{
		DisplayName: u.DisplayName,
		Name:        u.Name,
	}
}

func extendUser(u *User) *jira.User {
	return &jira.User{
		DisplayName: u.DisplayName,
		Name:        u.Name,
	}
}

type Subtask struct {
	Key        string
	StatusName string
	Summary    string
}

func stripSubtasks(s []*jira.Subtasks) []Subtask {
	if s == nil {
		return nil
	}
	subtasks := make([]Subtask, len(s))
	for i := 0; i < len(s); i++ {
		subtasks[i] = Subtask{
			Key:        s[i].Key,
			StatusName: s[i].Fields.Status.Name,
			Summary:    s[i].Fields.Summary,
		}
	}
	return subtasks
}

type Comment struct {
	ID      string
	Author  User
	Body    string
	Created string
}

func stripComments(c []*jira.Comment) []Comment {
	if c == nil {
		return nil
	}

	jc := make([]Comment, len(c))
	for i := 0; i < len(c); i++ {
		jc[i].ID = c[i].ID
		jc[i].Created = c[i].Created
		jc[i].Body = c[i].Body
		jc[i].Author = stripUser(&c[i].Author)
	}
	return jc
}
