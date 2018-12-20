package util

import (
	"crypto/sha256"
	"testing"
)

var ciphertext []byte

func hash(s string) []byte {
	hasher := sha256.New()
	hasher.Write([]byte(s))
	return hasher.Sum(nil)
}

func TestEncrypt(t *testing.T) {
	key := hash("123")
	data := "oh my oh my oh my oh my oh my oh my"

	c, err := Encrypt([]byte(key), data)
	if err != nil {
		t.Fatalf("can't encode data by key: %v", err)
	}
	if len(c) == 0 {
		t.Fatal("can't encode data by key: cyphertext length is 0")
	}
	t.Logf("%d", len(c))

	ciphertext = c
	t.Logf("%s -> %v", string(data), c)
}

func TestDecrypt(t *testing.T) {
	key := hash("123")
	data := "oh my oh my oh my oh my oh my oh my"

	c, err := Decrypt([]byte(key), ciphertext)
	if err != nil {
		t.Fatalf("can't decode data: %v", err)
	}
	if string(c) != data {
		t.Fatalf("decoded message does not match with ethalon: %s != %s", string(c), data)
	}

	t.Logf("%v -> %s", ciphertext, string(c))
}
