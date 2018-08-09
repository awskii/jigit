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

	fl.ProjectName = strings.ToUpper(fl.ProjectName)

	jr.InitClient()
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
	issue, resp, err := j.client.Issue.Get(issueID, nil)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return errors.New("bad status returned")
	}
	jiraAddr := j.client.GetBaseURL()
	f := issue.Fields

	out, err := less.NewFile()
	if err != nil {
		return err
	}
	defer out.Close()

	fmt.Fprintf(out, "\n %s [%s] %s\n\n", issue.Key, f.Status.Name, f.Summary)

	fmt.Fprintf(out, " Created:\t%s (%s)\n",
		time.Time(f.Created).Format(time.RFC850), RelativeTime(time.Time(f.Created)))
	fmt.Fprintf(out, " Assignee:\t%s (@%s)\n", f.Assignee.DisplayName, f.Assignee.Name)
	fmt.Fprintf(out, " Author:\t%s (@%s)\n", f.Creator.DisplayName, f.Creator.Name)
	fmt.Fprintf(out, " Parent:\t%s\n", printIfNotNil(f.Parent))
	fmt.Fprintf(out, " Priority:\t%s\n", f.Priority.Name)
	fmt.Fprintf(out, " Link:\t\t%sbrowse/%s\n\n", (&jiraAddr).String(), issue.Key)

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
		il[i].InwardIssue = *StripJiraIssue(l[i].InwardIssue)
		il[i].OutwardIssue = *StripJiraIssue(l[i].OutwardIssue)
	}
	return il
}

type JiraUser struct {
	DisplayName string
	Name        string
}

func stripUser(u *jira.User) JiraUser {
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

type JiraComment struct {
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
		jc[i].Created = c[i].Created
		jc[i].Body = c[i].Body
		jc[i].Author = stripUser(&c[i].Author)
	}
	return jc
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
	ji := new(jira.Issue)

	ji.Key = i.Key
	ji.Fields.Assignee.Name = i.Assignee.Name
	ji.Fields.Assignee.DisplayName = i.Assignee.DisplayName
	ji.Fields.Creator.Name = i.Creator.Name
	ji.Fields.Creator.DisplayName = i.Creator.DisplayName
	ji.Fields.Created = jira.Time(i.Created)
	ji.Fields.Summary = i.Summary
	ji.Fields.Description = i.Description
	ji.Fields.Parent.Key = i.ParentKey
	ji.Fields.Status.Name = i.StatusName
	ji.Fields.Priority.Name = i.PriorityName

	if len(i.Comments) != 0 {
		ji.Fields.Comments.Comments = make([]*jira.Comment, len(i.Comments))
		for j := 0; j < len(i.Comments); j++ {
			ji.Fields.Comments.Comments[j].Body = i.Comments[j].Body
			ji.Fields.Comments.Comments[j].Author.Name = i.Comments[j].Author.Name
			ji.Fields.Comments.Comments[j].Author.DisplayName = i.Comments[j].Author.DisplayName
			ji.Fields.Comments.Comments[j].Created = i.Comments[j].Created
		}
	}
	if len(i.Subtasks) != 0 {
		ji.Fields.Subtasks = make([]*jira.Subtasks, len(i.Subtasks))
		for j := 0; j < len(i.Subtasks); j++ {
			ji.Fields.Subtasks[j].Key = i.Subtasks[j].Key
			ji.Fields.Subtasks[j].Fields.Status.Name = i.Subtasks[j].StatusName
			ji.Fields.Subtasks[j].Fields.Summary = i.Subtasks[j].Summary
		}
	}
	if len(i.IssueLinks) != 0 {
		ji.Fields.IssueLinks = make([]*jira.IssueLink, len(i.IssueLinks))
		for j := 0; j < len(i.IssueLinks); j++ {
			// ji.Fields.IssueLinks[j].InwardIssue = i.IssueLinks[j].InwardIssue
			// ji.Fields.IssueLinks[j].OutwardIssue = i.IssueLinks[j].OutwardIssue
			// ji.Fields.IssueLinks[j].Type.Inward = i.IssueLinks[j].Type.Inward
			// ji.Fields.IssueLinks[j].Type.Outward = i.IssueLinks[j].Type.Outward
		}

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
