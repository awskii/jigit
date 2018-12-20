package util

import (
	"bufio"
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"syscall"
	"unicode/utf8"

	"golang.org/x/crypto/ssh/terminal"
)

// common helpers
func AskCredentials(site string) (login string, pass string) {
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

func AskPassphrase() []byte {
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

func TruncateString(str string, width int) string {
	l := utf8.RuneCountInString(str)
	mod := fmt.Sprintf("%%.%ds", width)
	str = fmt.Sprintf(mod, str)
	if l > width {
		str += "..."
	}
	return str
}

func StringToFixedWidth(str string, width int) string {
	str = strings.Replace(str, "{noformat}", "", -1)
	s := bufio.NewScanner(strings.NewReader(str))

	buf := new(bytes.Buffer)
	for s.Scan() {
		buf.WriteString(" ")
		line := s.Text()
		if utf8.RuneCountInString(line)-1 < width { // take inserted space into account
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

func Debug(format string, argv ...interface{}) {
	var srcFileInfo string
	if pc, file, line, ok := runtime.Caller(1); ok {
		fnameElems := strings.Split(file, "/")
		funcNameElems := strings.Split(runtime.FuncForPC(pc).Name(), "/")
		srcFileInfo = fmt.Sprintf("[caused by %s:%d %s]",
			strings.Join(fnameElems[len(fnameElems)-3:], "/"), line, funcNameElems[len(funcNameElems)-1])
	}
	fmt.Printf("[DEBUG] "+format+" "+srcFileInfo+"\n", argv...)
}

// Encrypt encrypts provided data by provided key with AES-256.
func Encrypt(key []byte, data string) ([]byte, error) {
	c, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(c)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return []byte(gcm.Seal(nonce, nonce, []byte(data), nil)), nil
}

// Decrypt decrypts provided data by provided key with AES-256.
func Decrypt(key, data []byte) ([]byte, error) {
	c, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(c)
	if err != nil {
		return nil, err
	}

	nonce, ciphertext := data[:gcm.NonceSize()], data[gcm.NonceSize():]
	dec, err := gcm.Open(nil, nonce, ciphertext, nil)
	return []byte(dec), err
}
