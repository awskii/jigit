package list

import (
	"fmt"
	"io"
	"strings"
	"time"

	"lib/jira"
	"lib/less"
	"lib/util"

	"github.com/olekukonko/tablewriter"
	"os"
)

const jiraTime = "2006-01-02T15:04:05.999-0700"

func proceedJira(fl Cmd) error {
	jr, err := jira.New()
	if err != nil {
		return err
	}
	defer jr.Destruct()

	if fl.NoCache {
		jr.InvalidateCache()
	}

	fl.ProjectName = strings.ToUpper(fl.ProjectName)

	switch {
	default:
		//listGitProjectIssues(git, fl.ProjectID, fl.IssueID, fl.All)
		//case fl.Show:
		if len(fl.IssueID) == 0 {
			fmt.Fprintln(os.Stderr, "You should provide issue ID to fetch with -i or --issue flag.")
			os.Exit(1)
		}
		issue, err := jr.Issue(fl.IssueID[0])
		if err != nil {
			return err
		}
		return renderJiraDetailedIssue(issue, jr.Endpoint())
	case fl.Assigned:
		issues, err := jr.ListAssignedIssues(fl.ProjectName)
		if err != nil {
			return err
		}
		renderJiraAssignedIssues(issues)
	case fl.Projects:
		projects, err := jr.ListProjects()
		if err != nil {
			return err
		}
		return renderJiraProjects(projects)
	}
	return nil
}

func renderJiraAssignedIssues(li []*jira.Issue) {
	fmt.Printf("Fetched %s assigned to you.\n\n",
		util.Plural(len(li), "issue", ""))

	t := tablewriter.NewWriter(os.Stdout)
	t.SetAutoWrapText(false)
	t.SetColumnSeparator("")
	t.SetBorder(false)
	for _, i := range li {
		t.Append([]string{i.Key, i.StatusName, i.Summary})
	}
	t.Render()
}
func renderJiraDetailedIssue(i *jira.Issue, endpoint string) error {
	out, err := less.NewFile()
	if err != nil {
		return err
	}
	defer out.Close()

	fmt.Fprintf(out, "\n %s [%s] %s\n\n", i.Key, i.StatusName, i.Summary)

	fmt.Fprintf(out, " Created:\t%s (%s)\n",
		time.Time(i.Created).Format(time.RFC850), util.RelativeTime(time.Time(i.Created)))
	fmt.Fprintf(out, " Assignee:\t%s (@%s)\n", i.Assignee.DisplayName, i.Assignee.Name)
	fmt.Fprintf(out, " Author:\t%s (@%s)\n", i.Creator.DisplayName, i.Creator.Name)
	fmt.Fprintf(out, " Parent:\t%s\n", printIfNotEmpty(i.ParentKey))
	fmt.Fprintf(out, " Priority:\t%s\n", i.PriorityName)
	fmt.Fprintf(out, " Link:\t\t%sbrowse/%s\n\n", endpoint, i.Key)

	fmt.Fprintf(out, "%s", util.StringToFixedWidth(i.Description, textWidthSize))
	fmt.Fprintf(out, sepIssue)

	printIssueLinks(out, i.IssueLinks)
	printIssueSubtasks(out, i.Subtasks)
	printIssueComments(out, i.Comments)

	out.Run()
	return nil
}

func renderJiraProjects(li []*jira.Project) error {
	out, err := less.NewFile()
	if err != nil {
		// todo maybe just print in stdout?
		return err
	}
	defer out.Close()

	fmt.Fprintf(out, "Fetched %d Jira %s.\n\n", len(li),
		util.PluralWord(len(li), "project", ""))

	table := tablewriter.NewWriter(out)
	//table.SetHeader([]string{"ID", "Key", "Name"})
	//table.SetAutoFormatHeaders(false)
	table.SetBorder(false)
	table.SetColumnSeparator("")
	table.SetAutoWrapText(false)
	for _, p := range li {
		table.Append([]string{p.ID, p.Key, p.Name})
	}
	table.Render()

	out.Run()
	return nil
}

// jira helpers
func printIssueComments(w io.Writer, comments []jira.Comment) {
	if len(comments) == 0 {
		fmt.Fprintln(w, " No comments")
		return
	}
	fmt.Fprintf(w, "\n Comments:\n\n")
	for _, c := range comments {
		created, _ := time.Parse(jiraTime, c.Created)
		fmt.Fprintf(w, sepComment)
		fmt.Fprintf(w, " [%s] %s (@%s) wrote %s\n\n%s", c.ID, c.Author.DisplayName,
			c.Author.Name, util.RelativeTime(created), util.StringToFixedWidth(c.Body, textWidthSize))
	}
	fmt.Fprintf(w, "\n")
}

func printIssueSubtasks(w io.Writer, s []jira.Subtask) {
	if len(s) == 0 {
		fmt.Fprintln(w, " No subtasks")
		return
	}
	fmt.Fprintf(w, "\n Subtasks:\n")
	sep := " ++++++++++++++++++++++++++++++++++++++\n"
	fmt.Fprintf(w, sep)
	for i := 0; i < len(s); i++ {
		fmt.Fprintf(w, "%s [%s] %s\n",
			s[i].Key, s[i].StatusName, util.TruncateString(s[i].Summary, 80))
	}
	fmt.Fprintf(w, sep)
}

func printIssueLinks(w io.Writer, s []jira.IssueLink) {
	if len(s) == 0 {
		fmt.Fprintln(w, " No linked issues")
		return
	}

	sep := " ++++++++++++++++++++++++++++++++++++++\n"
	fmt.Fprintf(w, "\n Linked issues:\n")
	fmt.Fprintf(w, sep)
	for i := 0; i < len(s); i++ {
		var issue jira.Issue
		var relation string

		switch {
		case s[i].InwardIssue.Key != "":
			issue = s[i].InwardIssue
			relation = s[i].Type.Inward
		case s[i].OutwardIssue.Key != "":
			issue = s[i].OutwardIssue
			relation = s[i].Type.Outward
		}

		fmt.Fprintf(w, " + %s %s [%s] %s\n", relation, issue.Key, issue.StatusName,
			util.TruncateString(issue.Summary, 80))
	}
	fmt.Fprintf(w, sep)
	fmt.Fprintf(w, "\n")
}

func printIfNotEmpty(p string) string {
	if p == "" {
		return "--"
	}
	return p
}
