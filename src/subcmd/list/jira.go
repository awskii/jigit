package list

import (
	"bytes"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

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
	login, pass, err := fetchJiraCredentials(storage, key)
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
		return listJiraProjectIssues(jrcli, fl.ProjectName)
	case fl.Projects:
		return listJiraProjects(jrcli)
	}
	return nil

}

func fetchJiraCredentials(s *persistent.Storage, key []byte) (string, string, error) {
	// TODO fix keys
	loginEnc, err := s.Get(persistent.BucketAuth, persistent.KeyGitlabUser)
	if err != nil {
		return "", "", err
	}

	// TODO fix keys
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

// jira helpers
func printIssueComments(comments []*jira.Comment) string {
	if len(comments) == 0 {
		return "No comments"
	}
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
