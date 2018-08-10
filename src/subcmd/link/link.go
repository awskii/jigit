package link

type SubLn struct {
	JiraTicket string `short:"j"`
	GitIssue   string `short:"g"`

	Active bool
	Argv   []string
}

func (ln *SubLn) Execute(v []string) error {
	ln.Active, ln.Argv = true, v
	return nil
}

//func process(fl *SubLn) error {
//
//}
