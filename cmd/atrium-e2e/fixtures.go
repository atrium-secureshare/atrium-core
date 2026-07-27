package main

import (
	"sync"
	"time"

	"github.com/atrium-secureshare/atrium-core/internal/provider"
)

// Canonical test identities: the Tier-1 dataset is scoped to these two so specs
// can assert identity binding.
const (
	recipientEmail = "recipient@example.test"
	otherEmail     = "other@example.test"
)

type node struct {
	entry    provider.FolderEntry
	content  []byte
	children []*node
}

// shareState reuses provider.Share/FolderEntry so the JSON the stub emits is
// exactly what the real provider client decodes.
type shareState struct {
	recipient string
	share     provider.Share
	content   []byte
	root      *node
}

// store is reset to the canonical fixture between tests so every spec starts
// from a known state despite in-test mutations.
type store struct {
	mu     sync.Mutex
	shares map[string]*shareState
}

func newStore() *store {
	s := &store{}
	s.seed()
	return s
}

func i64(v int64) *int64 { return &v }
func iptr(v int) *int    { return &v }

// seed resets the dataset to the canonical fixture covering every case the
// Tier-1 specs exercise.
func (s *store) seed() {
	now := time.Now().UTC()
	soon := now.Add(48 * time.Hour)

	shares := map[string]*shareState{
		"file-basic": {
			recipient: recipientEmail,
			content:   []byte("Quartalsbericht Inhalt\n"),
			share: provider.Share{
				ID: "file-basic", RecipientEmail: recipientEmail,
				FileName: "Quartalsbericht.pdf", Size: i64(23), Mode: 0,
				CreatedAt: &now, ExpiresAt: &soon,
			},
		},
		"file-limited": {
			recipient: recipientEmail,
			content:   []byte("nur einmal\n"),
			share: provider.Share{
				ID: "file-limited", RecipientEmail: recipientEmail,
				FileName: "Limitiert.pdf", Size: i64(11), Mode: 0,
				MaxDownloads: iptr(1), CreatedAt: &now,
			},
		},
		"folder-readall": {
			recipient: recipientEmail,
			share: provider.Share{
				ID: "folder-readall", RecipientEmail: recipientEmail,
				DisplayName: "Projekt", IsFolder: true, Mode: 2, CreatedAt: &now,
			},
			root: &node{children: []*node{
				{entry: provider.FolderEntry{ID: "plan", Name: "Plan.pdf", Size: i64(10)}, content: []byte("Planinhalt")},
				{entry: provider.FolderEntry{ID: "unterlagen", Name: "Unterlagen", IsFolder: true}, children: []*node{
					{entry: provider.FolderEntry{ID: "detail", Name: "Detail.txt", Size: i64(12)}, content: []byte("Detailinhalt")},
				}},
			}},
		},
		"folder-readonly": {
			recipient: recipientEmail,
			share: provider.Share{
				ID: "folder-readonly", RecipientEmail: recipientEmail,
				DisplayName: "NurLesen", IsFolder: true, Mode: 0, CreatedAt: &now,
			},
			root: &node{children: []*node{
				{entry: provider.FolderEntry{ID: "liste", Name: "Liste.csv", Size: i64(5)}, content: []byte("a,b,c")},
			}},
		},
		"folder-readown": {
			recipient: recipientEmail,
			share: provider.Share{
				ID: "folder-readown", RecipientEmail: recipientEmail,
				DisplayName: "MeineUploads", IsFolder: true, Mode: 1, CreatedAt: &now,
			},
			root: &node{},
		},
		"folder-dropzone": {
			recipient: recipientEmail,
			share: provider.Share{
				ID: "folder-dropzone", RecipientEmail: recipientEmail,
				DisplayName: "Briefkasten", IsFolder: true, Mode: 3, CreatedAt: &now,
			},
			root: &node{},
		},
		"file-other": {
			recipient: otherEmail,
			content:   []byte("geheim\n"),
			share: provider.Share{
				ID: "file-other", RecipientEmail: otherEmail,
				FileName: "Fremd.pdf", Size: i64(7), Mode: 0, CreatedAt: &now,
			},
		},
	}

	s.mu.Lock()
	s.shares = shares
	s.mu.Unlock()
}

func (s *store) find(id string) *shareState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.shares[id]
}

func (s *store) listFor(email string) []provider.Share {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]provider.Share, 0)
	// Stable order so the listing is deterministic across runs.
	for _, id := range []string{
		"file-basic", "file-limited", "folder-readall",
		"folder-readonly", "folder-readown", "folder-dropzone", "file-other",
	} {
		if st, ok := s.shares[id]; ok && st.recipient == email {
			out = append(out, st.share)
		}
	}
	return out
}
