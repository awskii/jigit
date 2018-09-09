package editor

import (
	"io/ioutil"
	"os"
	"os/exec"
)

// Temporary file, will removed after Close will be called.
// Helps to easily edit text in $EDITOR
type File struct {
	*os.File
	ed *exec.Cmd
}

func NewFile(editor, suffix string) (*File, error) {
	f, err := ioutil.TempFile("", "jigit-"+suffix)
	if err != nil {
		return nil, err
	}

	ed := exec.Command(editor, f.Name())
	ed.Stdout = os.Stdout
	ed.Stderr = os.Stderr
	ed.Stdin = os.Stdin
	return &File{File: f, ed: ed}, nil
}

func (f *File) Run() error {
	return f.ed.Run()
}

func (f *File) Contents() ([]byte, error) {
	if err := f.File.Close(); err != nil {
		return nil, err
	}
	buf, err := ioutil.ReadFile(f.Name())
	if err != nil {
		return nil, err
	}
	os.Remove(f.Name())
	return buf, nil
}
