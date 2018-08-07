package less

import (
	"io/ioutil"
	"os"
	"os/exec"
)

// Temporary file, will removed after Close will be called.
// Helps to easily pipe file contents to less utility
type File struct {
	*os.File
	l *exec.Cmd
}

func NewFile() (*File, error) {
	f, err := ioutil.TempFile("", "jigit")
	if err != nil {
		return nil, err
	}

	less := exec.Command("less", f.Name())
	less.Stdout = os.Stdout
	less.Stderr = os.Stderr

	return &File{File: f, l: less}, nil
}

func (f *File) Render() error {
	return f.l.Start()
}

func (f *File) Wait() error {
	return f.l.Wait()
}

func (f *File) Close() error {
	defer os.Remove(f.Name())
	return f.File.Close()
}
