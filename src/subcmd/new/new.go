package new

import "fmt"

type Cmd struct {
	IssueTitle string   `short:"t"`
	IssueTags  []string `long:"tags"`
	IssueBody  string   `short:"m"`
	Priority   int      `short:"p"`
	Issue      string   `short:"i"`
}

func (o *Cmd) Execute(v []string) error {
	fmt.Println("open!", v)
	return nil
}
