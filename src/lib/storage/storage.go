package storage

import (
	"crypto/aes"
	"encoding/binary"
	"fmt"

	"github.com/boltdb/bolt"
	"github.com/pkg/errors"
)

type Storage struct {
	b *bolt.DB
}

var (
	ErrBucketNotExist = errors.New("bucket does not exist")
	ErrNoData         = errors.New("key does not exist")
)

var (
	BucketAuth            = []byte("auth")
	BucketGitProjectCache = []byte("git-project-cache")
	BucketGitIssueCache   = []byte("git-issue-cache")
	BucketJiraIssueCache  = []byte("jira-issue-cache")
	BucketIssueLinks      = []byte("issue-links")

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

	buckets := [][]byte{
		BucketAuth,
		BucketGitIssueCache,
		BucketGitProjectCache,
		BucketJiraIssueCache,
		BucketIssueLinks,
	}

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

func (s *Storage) CreateSymlink(jiraKey, gitProject string, gitIssueID int) error {
	fn := func(tx *bolt.Tx) error {
		b := tx.Bucket(BucketIssueLinks)
		if b == nil {
			return errors.New("bucket did not initialized")
		}
		var (
			jira = []byte(jiraKey)
			git  = []byte(fmt.Sprintf("%s#%d", gitProject, gitIssueID))
		)

		err := b.Put(jira, git)
		if err != nil {
			return err
		}
		return b.Put(git, jira)
	}
	return s.b.Update(fn)
}

func (s *Storage) DropSymlink(jiraKey, gitProject string, gitIssueID int) error {
	fn := func(tx *bolt.Tx) error {
		b := tx.Bucket(BucketIssueLinks)
		if b == nil {
			return errors.Errorf("bucket '%s' did not initialized", string(BucketIssueLinks))
		}
		var (
			jira = []byte(jiraKey)
			git  = []byte(fmt.Sprintf("%s#%d", gitProject, gitIssueID))
		)

		err := b.Delete(jira)
		if err != nil {
			return err
		}
		return b.Delete(git)
	}
	return s.b.Update(fn)
}

func (s *Storage) GetString(bucket, key []byte) (string, error) {
	d, err := s.Get(bucket, key)
	return string(d), err
}

func (s *Storage) Get(bucket, key []byte) ([]byte, error) {
	var buf []byte
	fn := func(tx *bolt.Tx) error {
		b := tx.Bucket(bucket)
		if b == nil {
			return ErrBucketNotExist
		}

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

func (s *Storage) Invalidate(bucket []byte) error {
	fn := func(tx *bolt.Tx) error {
		err := tx.DeleteBucket(bucket)
		if err != nil {
			return err
		}
		_, err = tx.CreateBucket(bucket)
		return err
	}
	return s.b.Update(fn)
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

func Itob(v int) []byte {
	res := make([]byte, 8)
	binary.BigEndian.PutUint64(res, uint64(v))
	return res
}

func Btoi(b []byte) int {
	return int(binary.BigEndian.Uint64(b))
}
