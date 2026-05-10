package engine

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/getstackit/stackit/internal/git"
)

func TestMetaSerialization(t *testing.T) {
	parent := "main"
	scope := "feat/xyz"
	now := time.Now().UTC().Truncate(time.Second) // JSON unmarshaling might lose sub-second precision

	meta := git.NewMetaFrom(git.MetaFields{
		ParentBranchName: &parent,
		Scope:            &scope,
		LockReason:       git.LockReasonUser,
		BranchType:       git.BranchTypeUser,
		LastModifiedBy: &git.ModifiedBy{
			GitName:  "John Doe",
			GitEmail: "john@example.com",
		},
		LastModifiedAt: &now,
	})

	// Marshal
	data, err := json.Marshal(meta)
	assert.NoError(t, err)

	// Unmarshal
	var meta2 git.Meta
	err = json.Unmarshal(data, &meta2)
	assert.NoError(t, err)

	assert.Equal(t, meta.GetParentBranchName(), meta2.GetParentBranchName())
	assert.Equal(t, meta.GetScope(), meta2.GetScope())
	assert.Equal(t, meta.GetLockReason(), meta2.GetLockReason())
	assert.Equal(t, meta.GetBranchType(), meta2.GetBranchType())
	assert.Equal(t, meta.GetLastModifiedBy().GitName, meta2.GetLastModifiedBy().GitName)
	assert.Equal(t, meta.GetLastModifiedBy().GitEmail, meta2.GetLastModifiedBy().GitEmail)
	assert.True(t, meta.GetLastModifiedAt().Equal(*meta2.GetLastModifiedAt()))
}

func TestLocalMetaSerialization(t *testing.T) {
	meta := &git.LocalMeta{
		Frozen: true,
	}

	// Marshal
	data, err := json.Marshal(meta)
	assert.NoError(t, err)

	// Unmarshal
	var meta2 git.LocalMeta
	err = json.Unmarshal(data, &meta2)
	assert.NoError(t, err)

	assert.Equal(t, meta.Frozen, meta2.Frozen)
}

func TestMetaBackwardCompatibility(t *testing.T) {
	// Old metadata format (no new fields)
	jsonData := `{"parentBranchName":"main","lockReason":"locked"}`

	var meta git.Meta
	err := json.Unmarshal([]byte(jsonData), &meta)
	assert.NoError(t, err)

	assert.Equal(t, "main", *meta.GetParentBranchName())
	assert.Equal(t, git.LockReason("locked"), meta.GetLockReason())
	assert.Equal(t, git.BranchType(""), meta.GetBranchType()) // Should be empty/default
	assert.Nil(t, meta.GetLastModifiedBy())
	assert.Nil(t, meta.GetLastModifiedAt())
}
