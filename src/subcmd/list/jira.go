package list

import (
	"bytes"
	"encoding/gob"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"lib/less"
	"lib/persistent"
	"subcmd/config"

	"github.com/andygrunwald/go-jira"
	"github.com/olekukonko/tablewriter"
)

const jiraTime = "2006-01-02T15:04:05.999-0700"

func proceedJira(fl Subcmd) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	storage, err := persistent.NewStorage(cfg.Persistent.Path)
	if err != nil {
		return err
	}
	defer storage.Close()

	jr := &Jira{cfg: cfg, storage: storage}
	if fl.NoCache {
		jr.storage.Invalidate(persistent.BucketJiraIssueCache)
	}

	fl.ProjectName = strings.ToUpper(fl.ProjectName)

	switch {
	default:
		//listGitProjectIssues(git, fl.ProjectID, fl.IssueID, fl.All)
		//case fl.Show:
		if len(fl.IssueID) == 0 {
			fmt.Printf("You should provide issue ID to fetch with -i or --issue flag.\n")
			return ErrBadArg
		}
		return jr.Issue(fl.IssueID[0])
	case fl.Assigned:
		return jr.ListAssignedIssues(fl.ProjectName)
	case fl.Projects:
		return jr.ListProjects()
	}
	return nil
}

type Jira struct {
	endpoint string
	cfg      *config.Config
	storage  *persistent.Storage
	client   *jira.Client
}

func (j *Jira) InitClient() error {
	ep := j.cfg.Jira.Address
	if ep == "" {
		return ErrBadAddress
	}

	//key := askPassphrase()
	key := []byte("key")
	// todo fix cred issuer
	login, pass, err := j.credentials(key)
	if err != nil {
		// Todo sanitize error
		fmt.Println(err)
		// unauthorized
		login, pass = askCredentials(ep)
		//encLogin, _ := persistent.Encrypt(key, login)
		err := j.storage.Set(persistent.BucketAuth, persistent.KeyGitlabUser, []byte(login))
		if err != nil {
			return err
		}
		//encPass, _ := persistent.Encrypt(key, pass)
		err = j.storage.Set(persistent.BucketAuth, persistent.KeyGitlabPass, []byte(pass))
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

func (j *Jira) Issue(issueID string) error {
	var issue *jira.Issue

	issueRaw, err := j.storage.Get(persistent.BucketJiraIssueCache, []byte(issueID))
	if err != nil {
		var (
			resp *jira.Response
			err  error
		)
		j.InitClient()
		issue, resp, err = j.client.Issue.Get(issueID, nil)
		if err != nil {
			return err
		}
		if resp.StatusCode != http.StatusOK {
			return errors.New("bad status returned")
		}
		is := StripJiraIssue(issue)
		buf := new(bytes.Buffer)

		if err = is.Encode(buf); err != nil {
			debug("issue encode failed: %s", err)
		} else {
			err = j.storage.Set(persistent.BucketJiraIssueCache, []byte(issue.Key), buf.Bytes())
			if err != nil {
				debug("issue store failed: %s", err)
			}
		}
	} else {
		is := new(JiraIssue)
		if err = is.Decode(issueRaw); err != nil {
			debug("issue decode failed: %s", err)
		}

		issue = is.Extend()
		if issue == nil {
			debug("issue is nil :(")
		}
	}

	out, err := less.NewFile()
	if err != nil {
		return err
	}
	defer out.Close()

	f := issue.Fields
	fmt.Fprintf(out, "\n %s [%s] %s\n\n", issue.Key, f.Status.Name, f.Summary)

	fmt.Fprintf(out, " Created:\t%s (%s)\n",
		time.Time(f.Created).Format(time.RFC850), RelativeTime(time.Time(f.Created)))
	fmt.Fprintf(out, " Assignee:\t%s (@%s)\n", f.Assignee.DisplayName, f.Assignee.Name)
	fmt.Fprintf(out, " Author:\t%s (@%s)\n", f.Creator.DisplayName, f.Creator.Name)
	fmt.Fprintf(out, " Parent:\t%s\n", printIfNotNil(f.Parent))
	fmt.Fprintf(out, " Priority:\t%s\n", f.Priority.Name)
	fmt.Fprintf(out, " Link:\t\t%sbrowse/%s\n\n", j.cfg.Jira.Address, issue.Key)

	fmt.Fprintf(out, "%s", stringToFixedWidth(f.Description, textWidthSize))
	fmt.Fprintf(out, sepIssue)

	printIssueLinks(out, f.IssueLinks)
	printIssueSubtasks(out, f.Subtasks)
	printIssueComments(out, f.Comments.Comments)

	out.Render()
	out.Wait()

	return nil
}

func (j *Jira) ListProjects() error {
	list, resp, err := j.client.Project.GetList()
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return errors.New("jira did not return 200")
	}

	out, err := less.NewFile()
	if err != nil {
		//out = os.Stdout
	}
	defer out.Close()

	fmt.Fprintf(out, "Fetched %d Jira projects.\n\n", len(*list))
	table := tablewriter.NewWriter(out)

	//table.SetHeader([]string{"ID", "Key", "Name"})
	//table.SetAutoFormatHeaders(false)
	table.SetBorder(false)
	table.SetColumnSeparator("")
	table.SetAutoWrapText(false)
	for _, p := range *list {
		table.Append([]string{p.ID, p.Key, p.Name})
	}
	table.Render()

	out.Render()
	out.Wait()

	return nil
}

func (j *Jira) ListAssignedIssues(projectName string) error {
	user, resp, err := j.client.User.GetSelf()
	if err != nil {
		return err
	}
	q := fmt.Sprintf("assignee = %s AND status not in (Closed, Resolved)", user.Key)
	if projectName != "" {
		q += fmt.Sprintf(" AND project = %s", projectName)
	}
	issues, resp, err := j.client.Issue.Search(q, nil)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return errors.New("bad status returned")
	}

	fmt.Printf("Fetched %s assigned to you.\n\n",
		Plural(len(issues), "issue", ""))

	t := tablewriter.NewWriter(os.Stdout)
	t.SetAutoWrapText(false)
	t.SetColumnSeparator("")
	t.SetBorder(false)
	for _, i := range issues {
		t.Append([]string{i.Key, i.Fields.Status.Name, i.Fields.Summary})
	}
	t.Render()
	return nil
}

func (j *Jira) credentials(key []byte) (string, string, error) {
	// TODO fix keys
	loginEnc, err := j.storage.Get(persistent.BucketAuth, persistent.KeyGitlabUser)
	if err != nil {
		return "", "", err
	}

	// TODO fix keys
	passEnc, err := j.storage.Get(persistent.BucketAuth, persistent.KeyGitlabPass)
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

type JiraIssue struct {
	Key          string
	Summary      string
	Description  string
	Created      time.Time
	Subtasks     []JiraIssue
	IssueLinks   []JiraIssueLink
	Comments     []JiraComment
	Assignee     JiraUser
	Creator      JiraUser
	StatusName   string
	ParentKey    string
	PriorityName string
}

type JiraIssueLink struct {
	Type struct {
		Inward  string
		Outward string
	}
	OutwardIssue JiraIssue
	InwardIssue  JiraIssue
}

func stripIssueLinks(l []*jira.IssueLink) []JiraIssueLink {
	if l == nil {
		return nil
	}
	il := make([]JiraIssueLink, len(l))
	for i := 0; i < len(l); i++ {
		il[i].Type.Inward = l[i].Type.Inward
		il[i].Type.Outward = l[i].Type.Outward
		if in := StripJiraIssue(l[i].InwardIssue); in != nil {
			il[i].InwardIssue = *in
		}
		if out := StripJiraIssue(l[i].OutwardIssue); out != nil {
			il[i].OutwardIssue = *out
		}
	}
	return il
}

func dressIssueLink(il []JiraIssueLink) []*jira.IssueLink {
	links := make([]*jira.IssueLink, len(il))
	for j := 0; j < len(il); j++ {
		switch {
		case il[j].InwardIssue.Key != "":
			links[j] = &jira.IssueLink{
				Type: jira.IssueLinkType{Inward: il[j].Type.Inward},
				InwardIssue: &jira.Issue{
					Key: il[j].InwardIssue.Key,
					Fields: &jira.IssueFields{
						Status:  &jira.Status{Name: il[j].InwardIssue.StatusName},
						Summary: il[j].InwardIssue.Summary,
					},
				},
			}
		case il[j].OutwardIssue.Key != "":
			links[j] = &jira.IssueLink{
				Type: jira.IssueLinkType{Outward: il[j].Type.Outward},
				OutwardIssue: &jira.Issue{
					Key: il[j].OutwardIssue.Key,
					Fields: &jira.IssueFields{
						Status:  &jira.Status{Name: il[j].OutwardIssue.StatusName},
						Summary: il[j].OutwardIssue.Summary,
					},
				},
			}
		}
	}
	return links
}

type JiraUser struct {
	DisplayName string
	Name        string
}

func stripUser(u *jira.User) JiraUser {
	if u == nil {
		return JiraUser{}
	}
	return JiraUser{
		DisplayName: u.DisplayName,
		Name:        u.Name,
	}
}

type JiraSubtask struct {
	Key        string
	StatusName string
	Summary    string
}

func stripSubtasks(s []*jira.Subtasks) []JiraIssue {
	if s == nil {
		return nil
	}
	is := make([]JiraIssue, len(s))
	for i := 0; i < len(s); i++ {
		is[i] = JiraIssue{
			Key:        s[i].Key,
			StatusName: s[i].Fields.Status.Name,
			Summary:    s[i].Fields.Summary,
		}
	}
	return is
}

func dressSubtasks(is []JiraIssue) []*jira.Subtasks {
	sub := make([]*jira.Subtasks, len(is))
	for j := 0; j < len(is); j++ {
		sub[j] = &jira.Subtasks{
			Key: is[j].Key,
			Fields: jira.IssueFields{
				Status:  &jira.Status{Name: is[j].StatusName},
				Summary: is[j].Summary,
			},
		}
	}
	return sub
}

type JiraComment struct {
	ID      string
	Author  JiraUser
	Body    string
	Created string
}

func stripComments(c []*jira.Comment) []JiraComment {
	if c == nil {
		return nil
	}

	jc := make([]JiraComment, len(c))
	for i := 0; i < len(c); i++ {
		jc[i].ID = c[i].ID
		jc[i].Created = c[i].Created
		jc[i].Body = c[i].Body
		jc[i].Author = stripUser(&c[i].Author)
	}
	return jc
}

func dressComments(ic []JiraComment) *jira.Comments {
	comments := new(jira.Comments)
	comments.Comments = make([]*jira.Comment, len(ic))
	for j := 0; j < len(ic); j++ {
		comments.Comments[j] = &jira.Comment{
			ID:      ic[j].ID,
			Body:    ic[j].Body,
			Created: ic[j].Created,
			Author: jira.User{
				Name:        ic[j].Author.Name,
				DisplayName: ic[j].Author.DisplayName,
			},
		}
	}
	return comments
}

func StripJiraIssue(i *jira.Issue) (ji *JiraIssue) {
	if i == nil {
		return
	}

	ji = &JiraIssue{
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

func (i *JiraIssue) Extend() *jira.Issue {
	ji := &jira.Issue{
		Key: i.Key,
		Fields: &jira.IssueFields{
			Assignee: &jira.User{
				Name:        i.Assignee.Name,
				DisplayName: i.Assignee.DisplayName,
			},
			Description: i.Description,
			Creator: &jira.User{
				Name:        i.Creator.Name,
				DisplayName: i.Creator.DisplayName,
			},
			Created:  jira.Time(i.Created),
			Summary:  i.Summary,
			Parent:   &jira.Parent{Key: i.ParentKey},
			Status:   &jira.Status{Name: i.StatusName},
			Priority: &jira.Priority{Name: i.PriorityName},
		},
	}

	if len(i.Comments) != 0 {
		ji.Fields.Comments = dressComments(i.Comments)
	}
	if len(i.Subtasks) != 0 {
		ji.Fields.Subtasks = dressSubtasks(i.Subtasks)
	}
	if len(i.IssueLinks) != 0 {
		ji.Fields.IssueLinks = dressIssueLink(i.IssueLinks)
	}
	return ji
}

func (i *JiraIssue) Encode(into io.Writer) error {
	return gob.NewEncoder(into).Encode(i)
}

func (i *JiraIssue) Decode(v []byte) error {
	return gob.NewDecoder(bytes.NewBuffer(v)).Decode(i)
}

// jira helpers
func printIssueComments(w io.Writer, comments []*jira.Comment) {
	if len(comments) == 0 {
		fmt.Fprintln(w, " No comments")
		return
	}
	fmt.Fprintf(w, "\n Comments:\n\n")
	for _, c := range comments {
		created, _ := time.Parse(jiraTime, c.Created)
		fmt.Fprintf(w, sepComment)
		fmt.Fprintf(w, " [%s] %s (@%s) wrote %s\n\n%s", c.ID, c.Author.DisplayName,
			c.Author.Name, RelativeTime(created), stringToFixedWidth(c.Body, textWidthSize))
	}
	fmt.Fprintf(w, "\n")
}

func printIssueSubtasks(w io.Writer, s []*jira.Subtasks) {
	if len(s) == 0 {
		fmt.Fprintln(w, " No subtasks")
		return
	}
	sep := " ++++++++++++++++++++++++++++++++++++++\n"
	buf := new(bytes.Buffer)
	fmt.Fprintf(w, "\n Subtasks:\n")
	fmt.Fprintf(w, sep)
	for i := 0; i < len(s); i++ {
		fmt.Fprintf(buf, "%s [%s] %s\n", s[i].Key, s[i].Fields.Status.Name,
			truncateString(s[i].Fields.Summary, 80))
	}
	fmt.Fprintf(w, sep)
}

func printIssueLinks(w io.Writer, s []*jira.IssueLink) {
	if len(s) == 0 {
		fmt.Fprintln(w, " No linked issues")
		return
	}

	sep := " ++++++++++++++++++++++++++++++++++++++\n"
	fmt.Fprintf(w, "\n Linked issues:\n")
	fmt.Fprintf(w, sep)
	for i := 0; i < len(s); i++ {
		var issue *jira.Issue
		var relation string

		switch {
		case s[i].InwardIssue != nil:
			issue = s[i].InwardIssue
			relation = s[i].Type.Inward
		case s[i].OutwardIssue != nil:
			issue = s[i].OutwardIssue
			relation = s[i].Type.Outward
		}

		fmt.Fprintf(w, " + %s %s [%s] %s\n", relation, issue.Key, issue.Fields.Status.Name,
			truncateString(issue.Fields.Summary, 80))
	}
	fmt.Fprintf(w, sep)
	fmt.Fprintf(w, "\n")
}

func printIfNotNil(p *jira.Parent) string {
	if p == nil {
		return "--"
	}
	return p.Key
}
