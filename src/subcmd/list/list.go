package list

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"golang.org/x/crypto/ssh/terminal"
)

const commentTime = time.RFC850

var (
	textWidthSize = 100

	sepIssue   = " ======================================\n"
	sepComment = " --------------------------------------\n"

	ErrBadAddress = errors.New("bad address provided")
	ErrBadArg     = errors.New("bad arguments")
)

type Subcmd struct {
	JiraMode    bool     `short:"j" long:"jira" description:"if provided, listings will be fetched from Jira instead of GitLab"`
	Projects    bool     `short:"P" description:"list projects instead of issues"`
	ProjectName string   `short:"p" long:"project" description:"project name to get issues on"`
	ProjectID   int      `long:"pid" description:"project ID to get issues on"`
	IssueID     []string `short:"i" long:"issue" description:"issue ID for detailed view"`
	//IssueCommentSortRule

	Assigned bool `short:"a" long:"assigned" description:"show all issues assigned to me"`
	Limit    int  `short:"n" default:"20" description:"limit for entities to show"`
	All      bool `long:"ignore-state" description:"ignore issue state"`
	Show     bool `long:"show"` // todo deprecate
	//ShowLinks bool   `short:"l" long:"links" description:"show web link to entity"`
	//List    bool   `short:"l" description:"show output as list instead of piping it to less utility"`
	Search  string `short:"s" long:"search"`
	NoCache bool   `short:"c" long:"no-cache" description:"ignore cached data and retrieve fresh data from remote"`

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

// common helpers
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

func truncateString(str string, width int) string {
	l := utf8.RuneCountInString(str)
	mod := fmt.Sprintf("%%.%ds", width)
	str = fmt.Sprintf(mod, str)
	if l > width {
		str += "..."
	}
	return str
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
