package list

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"config"
	"persistent"

	"github.com/andygrunwald/go-jira"
	"github.com/olekukonko/tablewriter"
	"github.com/xanzy/go-gitlab"
	"golang.org/x/crypto/ssh/terminal"
)

type Subcmd struct {
	JiraMode  bool     `long:"jira" short:"j"`
	Projects  bool     `short:"p"`
	ProjectID string   `short:"P" long:"project"`
	IssueID   []string `short:"i" long:"issue" description:"issue ID(s) for detailed view"`
	//IssueCommentSortRule

	Assigned bool `short:"t"`
	Limit    int  `short:"n" default:"20" description:"limit for entities to show"`
	All      bool `long:"ignore-state" description:"ignore issue state"`
	Show     bool `long:"show"` // todo deprecate
	//ShowLinks bool   `short:"l" long:"links" description:"show web link to entity"`
	List   bool   `short:"l" description:"show output as list instead of piping it to less utility"`
	Search string `short:"s" long:"search"`

	Active bool
	Argv   []string
}

func (ls *Subcmd) Execute(argv []string) error {
	ls.Argv, ls.Active = argv, true
	return nil
}

func Process(fl Subcmd) error {
	if fl.JiraMode {
		return proceedJira(fl)
	}
	return proceedGit(fl)
}

func proceedJira(fl Subcmd) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ep := cfg.Jira.Address
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
	// todo fix cred issuer
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
	tp := jira.BasicAuthTransport{
		Username: login,
		Password: pass,
	}

	fmt.Printf("Connecting to '%s'...\n", ep)
	jrcli, err := jira.NewClient(tp.Client(), ep)
	if err != nil {
		return err
	}

	switch {
	default:
		//listGitProjectIssues(git, fl.ProjectID, fl.IssueID, fl.All)
	case fl.Show:
		if len(fl.IssueID) == 0 {
			fmt.Printf("You should provide issue ID to fetch with -i or --issue flag.\n")
			return ErrBadArg
		}
		return getJiraIssue(jrcli, fl.IssueID[0])
	case fl.Assigned:
		//return listGitIssues(git, fl.All)
		return listJiraProjectIssues(jrcli, fl.ProjectID)
	case fl.Projects:
		return listJiraProjects(jrcli)
	}
	return nil

}

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

func listJiraProjects(jrcli *jira.Client) error {
	list, resp, err := jrcli.Project.GetList()
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return errors.New("jira did not return 200")
	}

	output, err := ioutil.TempFile("", "jigit")
	if err != nil {
		//output = os.Stdout
	}
	defer output.Close()
	defer os.Remove(output.Name())

	fmt.Fprintf(output, "Fetched %d Jira projects.\n\n", len(*list))
	table := tablewriter.NewWriter(output)
	table.SetHeader([]string{"ID", "Key", "Name"})
	table.SetBorder(false)
	table.SetAutoFormatHeaders(false)
	table.SetAutoWrapText(false)
	for _, p := range *list {
		table.Append([]string{p.ID, p.Key, p.Name})
	}
	table.Render()

	less := exec.Command("less", output.Name())
	less.Stdout = os.Stdout
	less.Stderr = os.Stderr
	less.Start()
	less.Wait()

	return nil
}

var textWidthSize = 100

func getJiraIssue(jrcli *jira.Client, issueID string) error {
	issue, resp, err := jrcli.Issue.Get(issueID, nil)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return errors.New("bad status returned")
	}
	jiraAddr := jrcli.GetBaseURL()
	f := issue.Fields

	fmt.Printf("\n %s [%s] %s\n"+
		" Created:\t%s\n"+
		" Assignee:\t%s (@%s)\n"+
		" Author:\t%s (@%s)\n"+
		" Parent:\t%s\n"+
		" Priority:\t%s\n"+
		" Link:\t\t%sbrowse/%s\n\n"+
		" %s"+
		" %s\n"+
		" %s\n"+
		" %s\n",
		issue.Key, f.Status.Name, f.Summary, time.Time(f.Created).Format(time.RFC850),
		f.Assignee.DisplayName, f.Assignee.Name,
		f.Creator.DisplayName, f.Creator.Name,
		printIfNotNil(f.Parent),
		f.Priority.Name,
		(&jiraAddr).String(), issue.Key,
		stringToFixedWidth(f.Description, textWidthSize),
		printIssueLinks(f.IssueLinks),
		printIssueSubtasks(f.Subtasks),
		printIssueComments(f.Comments.Comments),
	)

	return nil
}

const jiraTime = "2006-01-02T15:04:05.999-0700"
const commentTime = time.RFC850

func printIssueComments(comments []*jira.Comment) string {
	if len(comments) == 0 {
		return "No comments"
	}
	sepIssue := " ======================================\n"
	sepComment := " --------------------------------------\n"
	buf := new(bytes.Buffer)
	buf.WriteString("\n Comments:\n")
	buf.WriteString(sepIssue)
	for _, c := range comments {
		created, _ := time.Parse(jiraTime, c.Created)
		fmt.Fprintf(buf, " [%s] %s (@%s) at %s\n\n%s", c.ID, c.Author.DisplayName,
			c.Author.Name, created.Format(commentTime),
			stringToFixedWidth(c.Body, textWidthSize))
		buf.WriteString(sepComment)
	}
	return buf.String()
}

func printIssueSubtasks(s []*jira.Subtasks) string {
	if len(s) == 0 {
		return "No subtasks"
	}
	sep := " ++++++++++++++++++++++++++++++++++++++\n"
	buf := new(bytes.Buffer)
	buf.WriteString("\n Subtasks:\n")
	buf.WriteString(sep)
	for i := 0; i < len(s); i++ {
		fmt.Fprintf(buf, "%s [%s] %s\n", s[i].Key, s[i].Fields.Status.Name,
			truncateString(s[i].Fields.Summary, 80))
	}
	buf.WriteString(sep)
	return buf.String()
}

func printIssueLinks(s []*jira.IssueLink) string {
	if len(s) == 0 {
		return "No linked issues"
	}
	sep := " ++++++++++++++++++++++++++++++++++++++\n"
	buf := new(bytes.Buffer)
	buf.WriteString("\n Linked issues:\n")
	buf.WriteString(sep)
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

		fmt.Fprintf(buf, " + %s %s [%s] %s\n", relation, issue.Key, issue.Fields.Status.Name,
			truncateString(issue.Fields.Summary, 80))
	}
	buf.WriteString(sep)
	return buf.String()
}
func printIfNotNil(p *jira.Parent) string {
	if p == nil {
		return "--"
	}
	return p.Key
}

func listJiraProjectIssues(jrcli *jira.Client, projectName string) error {
	user, resp, err := jrcli.User.GetSelf()
	if err != nil {
		return err
	}
	q := fmt.Sprintf("project = %s AND status not in (Closed, Resolved) AND assignee = %s",
		strings.ToUpper(projectName), user.Key)
	issues, resp, err := jrcli.Issue.Search(q, nil)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return errors.New("bad status returned")
	}
	for _, i := range issues {
		fmt.Printf(" %s [%s] %s\n", i.Key, i.Fields.Status.Name, i.Fields.Summary)
	}
	return nil
}

func fetchJiraCredentials(s *persistent.Storage, key []byte) (string, string, error) {
	loginEnc, err := s.Get(persistent.BucketAuth, persistent.KeyJiraUser)
	if err != nil {
		return "", "", err
	}

	passEnc, err := s.Get(persistent.BucketAuth, persistent.KeyJiraPass)
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

var (
	ErrBadAddress = errors.New("bad address provided")
	ErrBadArg     = errors.New("bad arguments")
)

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
	fmt.Fprintf(output, " Issue #%d: %s (%s)\n\n Jira task:\t%d\n Assignee:\t%s\n"+
		" Created at:\t%s\n Link:\t\t%s\n\n Tags: %s\n\n %s\n%s\n\n",
		issue.IID, issue.Title, strings.ToUpper(issue.State), 777, issue.Assignee.Name,
		issue.CreatedAt.Format(time.RFC850), issue.WebURL, issue.Labels,
		stringToFixedWidth(issue.Description, textWidthSize),
		" ======================================")
	for _, note := range notes {
		fmt.Fprintf(output, " [%d] %s%s @%s at %s\n\n %s\n%s\n\n",
			note.ID, printIfEdited(note.UpdatedAt.Equal(*note.CreatedAt)),
			note.Author.Name, note.Author.Username,
			note.CreatedAt.Format(time.RFC1123),
			stringToFixedWidth(note.Body, textWidthSize),
			" -------------------")
	}
	if err := less.Start(); err != nil {
		return err
	}
	less.Wait()
	output.Close()

	return nil
}

// helpers
func truncateString(str string, width int) string {
	l := utf8.RuneCountInString(str)
	mod := fmt.Sprintf("%%.%ds", width)
	str = fmt.Sprintf(mod, str)
	if l > width {
		str += "..."
	}
	return str
}

func printIfEdited(cond bool) string {
	if cond {
		return ""
	}
	return "edited "
}

func stringToFixedWidth(str string, width int) string {
	str = strings.Replace(str, "{noformat}", "", -1)
	s := bufio.NewScanner(strings.NewReader(str))

	buf := new(bytes.Buffer)
	for s.Scan() {
		line := s.Text()
		if utf8.RuneCountInString(line) < width {
			buf.WriteString(" ")
			buf.WriteString(line)
			buf.WriteString("\n")
			continue
		}
		f := strings.Fields(line)
		tot := 0
		for i := 0; i < len(f); i++ {
			l := utf8.RuneCountInString(f[i])
			if tot+l >= width {
				buf.WriteString("\n ")
				tot = 0
			}
			buf.WriteString(f[i] + " ")
			tot += l + 1
		}
		buf.WriteString("\n")
	}
	return buf.String()
}

func askCredentials(site string) (login string, pass string) {
	fmt.Printf("Username for '%s': ", site)
	fmt.Scanf("%s", &login)
	fmt.Printf("Password for '%s': ", site)
	b, err := terminal.ReadPassword(int(syscall.Stdin))
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	fmt.Printf("\n")
	pass = string(b)
	return
}

func askPassphrase() []byte {
	fmt.Printf("Enter passphrase: ")
	key, err := terminal.ReadPassword(int(syscall.Stdin))
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	fmt.Printf("\n")

	hasher := sha256.New()
	hasher.Write([]byte(key))
	return hasher.Sum(nil)
}
