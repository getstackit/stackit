package engine

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/git"
)

func TestMetaSerialization(t *testing.T) {
	t.Parallel()
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
	require.NoError(t, err)

	// Unmarshal
	var meta2 git.Meta
	err = json.Unmarshal(data, &meta2)
	require.NoError(t, err)

	require.Equal(t, meta.GetParentBranchName(), meta2.GetParentBranchName())
	require.Equal(t, meta.GetScope(), meta2.GetScope())
	require.Equal(t, meta.GetLockReason(), meta2.GetLockReason())
	require.Equal(t, meta.GetBranchType(), meta2.GetBranchType())
	require.Equal(t, meta.GetLastModifiedBy().GitName, meta2.GetLastModifiedBy().GitName)
	require.Equal(t, meta.GetLastModifiedBy().GitEmail, meta2.GetLastModifiedBy().GitEmail)
	require.True(t, meta.GetLastModifiedAt().Equal(*meta2.GetLastModifiedAt()))
}

func TestLocalMetaSerialization(t *testing.T) {
	t.Parallel()
	meta := &git.LocalMeta{
		Frozen: true,
	}

	// Marshal
	data, err := json.Marshal(meta)
	require.NoError(t, err)

	// Unmarshal
	var meta2 git.LocalMeta
	err = json.Unmarshal(data, &meta2)
	require.NoError(t, err)

	require.Equal(t, meta.Frozen, meta2.Frozen)
}

func TestMetaBackwardCompatibility(t *testing.T) {
	t.Parallel()
	// Old metadata format (no new fields)
	jsonData := `{"parentBranchName":"main","lockReason":"locked"}`

	var meta git.Meta
	err := json.Unmarshal([]byte(jsonData), &meta)
	require.NoError(t, err)

	require.Equal(t, "main", *meta.GetParentBranchName())
	require.Equal(t, git.LockReason("locked"), meta.GetLockReason())
	require.Equal(t, git.BranchType(""), meta.GetBranchType()) // Should be empty/default
	require.Nil(t, meta.GetLastModifiedBy())
	require.Nil(t, meta.GetLastModifiedAt())
}
