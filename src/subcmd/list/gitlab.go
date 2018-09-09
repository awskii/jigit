package list

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"lib/git"
	"lib/less"
	"lib/util"

	"github.com/olekukonko/tablewriter"
)

func proceedGit(fl Cmd) error {
	git, err := git.New()
	if err != nil {
		return err
	}
	defer git.Destruct()

	if fl.NoCache {
		util.Debug("[CACHE] invalidating git cache")
	}

	switch {
	default:
		pid, err := git.GetPid(fl.ProjectName, fl.ProjectID)
		if err != nil {
			return err
		}
		issues, err := git.ListProjectIssues(pid, fl.All)
		if err != nil {
			return err
		}
		renderGitProjectIssues(issues)
	case len(fl.IssueID) != 0:
		issueID, err := parseIssueID(fl.IssueID)
		if err != nil {
			return err
		}
		pid, err := git.GetPid(fl.ProjectName, fl.ProjectID)
		if err != nil {
			return err
		}
		issue, comments, err := git.DetailedProjectIssue(pid, issueID[0])
		if err != nil {
			return err
		}
		renderGitDetailedIssue(issue, comments)
	case fl.Assigned:
		issues, err := git.ListAssignedIssues(fl.All)
		if err != nil {
			return err
		}
		renderGitAssignedIssues(git, issues)
	case fl.Projects:
		proj, err := git.ListProjects(fl.Limit, fl.NoCache)
		if err != nil {
			return err
		}
		renderGitProjects(proj)
	}
	return nil
}

func renderGitProjects(projects []*git.Project) {
	out := tablewriter.NewWriter(os.Stdout)
	out.SetHeader([]string{"ID", "Name", "Description", "Link"})
	out.SetBorder(false)
	out.SetAutoFormatHeaders(false)
	out.SetAutoWrapText(false)
	for _, p := range projects {
		out.Append([]string{fmt.Sprintf("%d", p.ID), p.Name,
			util.TruncateString(p.Description, 80), p.WebURL})
	}
	out.Render()
}

func renderGitProjectIssues(issues []*git.Issue) {
	table := tablewriter.NewWriter(os.Stdout)
	table.SetAutoWrapText(false)
	table.SetBorder(false)
	for _, issue := range issues {
		iid := fmt.Sprintf("%d", issue.IID)
		table.Append([]string{iid, strings.ToUpper(issue.State), issue.Title})
	}
	table.Render()
	fmt.Printf("\n")
}

func renderGitAssignedIssues(git *git.Git, issues []*git.Issue) {
	table := tablewriter.NewWriter(os.Stdout)
	table.SetAutoWrapText(false)
	table.SetBorder(false)
	table.SetColumnSeparator(" ")

	for _, issue := range issues {
		pname, _ := git.ProjectNameByID(issue.ProjectID)
		iid := fmt.Sprintf("%d", issue.IID)

		table.Append([]string{pname, iid, strings.ToUpper(issue.State), issue.Title})
	}
	table.Render()
	fmt.Printf("\n")

}

func renderGitDetailedIssue(issue *git.Issue, notes []*git.Comment) {
	sort.Sort(GitCommentsTimeSort(notes))

	out, err := less.NewFile()
	if err != nil {
		// fixme shitty error handling
		//out.File = os.Stdout
	}
	defer out.Close()

	// todo show project name
	// todo link jira issue
	fmt.Fprintf(out, " Issue #%d (%s): %s tags: %s\n\n Project:\t%d\n Jira task:\t%d\n"+
		" Assignee:\t%s\n Created at:\t%s (%s)\n Link:\t\t%s\n\n%s%s\n",
		issue.IID, strings.ToUpper(issue.State), issue.Title, issue.Labels,
		issue.ProjectID, 777, issue.AssigneeName, issue.CreatedAt.Format(time.RFC850),
		util.RelativeTime(issue.CreatedAt),
		issue.WebURL, util.StringToFixedWidth(issue.Description, textWidthSize), sepIssue)
	for _, note := range notes {
		fmt.Fprintf(out, " %s @%s wrote %s [id=%d] %s\n\n%s%s",
			note.AuthorName, note.AuthorUsername, util.RelativeTime(note.CreatedAt), note.ID,
			printIfEdited(note.UpdatedAt.Equal(note.CreatedAt)),
			util.StringToFixedWidth(note.Body, textWidthSize), sepComment)
	}
	out.Run()
}

func printIfEdited(cond bool) string {
	if cond {
		return ""
	}
	return "EDITED"
}

func parseIssueID(iid []string) ([]int, error) {
	// projectID should be string because JIRA id contains characters
	// we need to cast ProjectID and IssueID to int's
	issueID := make([]int, 0)
	switch len(iid) {
	case 0:
		fmt.Fprintln(os.Stderr, "You should provide at least one issue ID to fetch with -i or --issue flag.")
		os.Exit(1)
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
			fmt.Fprintf(os.Stderr, "Bad issue ID: %s", err)
			os.Exit(1)
		}
		issueID = append(issueID, id)
	}
	return issueID, nil
}

type GitCommentsTimeSort []*git.Comment

func (c GitCommentsTimeSort) Less(i, j int) bool {
	return c[i].CreatedAt.Before(c[j].CreatedAt)
}

func (c GitCommentsTimeSort) Swap(i, j int) {
	c[i], c[j] = c[j], c[i]
}

func (c GitCommentsTimeSort) Len() int {
	return len(c)
}
