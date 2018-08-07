package persistent

import (
	"crypto/aes"
	"errors"
	"fmt"

	"github.com/boltdb/bolt"
)

type Storage struct {
	b *bolt.DB
}

var (
	BucketAuth            = []byte("auth")
	BucketGitProjectCache = []byte("git-project-cache")
	BucketGitIssueCache   = []byte("git-issue-cache")
)

var (
	KeyGitlabUser = []byte("gitlab.user")
	KeyGitlabPass = []byte("gitlab.pass")
	KeyJiraUser   = []byte("jira.user")
	KeyJiraPass   = []byte("jira.pass")
)

func NewStorage(filepath string) (*Storage, error) {
	b, err := bolt.Open(filepath, 0600, nil)
	if err != nil {
		return nil, err
	}

	buckets := [][]byte{BucketAuth, BucketGitIssueCache, BucketGitProjectCache}

	for _, key := range buckets {
		fn := func(tx *bolt.Tx) error {
			_, err := tx.CreateBucketIfNotExists(key)
			if err != nil {
				fmt.Printf("can't initalize bucket '%s': %s\n", string(key), err)
			}
			return err
		}
		if err := b.Update(fn); err != nil {
			return nil, err
		}
	}
	return &Storage{b: b}, nil
}

func (s *Storage) Set(bucket, key, value []byte) error {
	fn := func(tx *bolt.Tx) error {
		b := tx.Bucket(bucket)
		return b.Put(key, value)
	}
	return s.b.Update(fn)
}

func (s *Storage) GetString(bucket, key []byte) (string, error) {
	d, err := s.Get(bucket, key)
	return string(d), err
}

var ErrNoData = errors.New("key does not exist")

func (s *Storage) Get(bucket, key []byte) ([]byte, error) {
	var buf []byte
	fn := func(tx *bolt.Tx) error {
		b := tx.Bucket(bucket)
		buf = b.Get(key)
		if buf == nil {
			return ErrNoData
		}
		return nil
	}
	err := s.b.View(fn)
	if err != nil {
		return nil, err
	}
	return buf, err
}

func (s *Storage) ForEach(bucket []byte, f func(k, v []byte) error) error {
	fn := func(tx *bolt.Tx) error {
		return tx.Bucket(bucket).ForEach(f)
	}
	return s.b.View(fn)
}

func (s *Storage) Close() error {
	return s.b.Close()
}

func Encrypt(key []byte, data string) ([]byte, error) {
	cipher, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	enc := make([]byte, 0)
	cipher.Encrypt(enc, []byte(data))
	return enc, nil
}

func Decrypt(key, data []byte) ([]byte, error) {
	cipher, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	dec := make([]byte, 0)
	cipher.Decrypt(dec, data)
	return dec, nil
}
