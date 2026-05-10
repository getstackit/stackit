package git_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/git"
)

const (
	testBranchMain = "main"
	testScopeAPI   = "api"
	testStackID1   = "stack-1"
)

func TestNewMeta(t *testing.T) {
	t.Parallel()
	m := git.NewMeta()
	require.NotNil(t, m)
	require.Nil(t, m.GetParentBranchName())
	require.Equal(t, git.LockReasonNone, m.GetLockReason())
}

func TestNewMetaFrom(t *testing.T) {
	t.Parallel()

	name := testBranchMain
	rev := "abc123"
	scope := testScopeAPI
	stackID := testStackID1

	m := git.NewMetaFrom(git.MetaFields{
		ParentBranchName:     &name,
		ParentBranchRevision: &rev,
		Scope:                &scope,
		LockReason:           git.LockReasonUser,
		BranchType:           git.BranchTypeUser,
		StackID:              &stackID,
	})

	require.Equal(t, &name, m.GetParentBranchName())
	require.Equal(t, &rev, m.GetParentBranchRevision())
	require.Equal(t, &scope, m.GetScope())
	require.Equal(t, git.LockReasonUser, m.GetLockReason())
	require.Equal(t, git.BranchTypeUser, m.GetBranchType())
	require.Equal(t, &stackID, m.GetStackID())
	require.Nil(t, m.GetPrInfo())
}

func TestMetaGetters(t *testing.T) {
	t.Parallel()

	t.Run("nil receiver returns zero values", func(t *testing.T) {
		t.Parallel()
		var m *git.Meta
		require.Nil(t, m.GetParentBranchName())
		require.Nil(t, m.GetParentBranchRevision())
		require.Nil(t, m.GetPrInfo())
		require.Nil(t, m.GetScope())
		require.Equal(t, git.LockReasonNone, m.GetLockReason())
		require.Equal(t, git.BranchType(""), m.GetBranchType())
		require.Nil(t, m.GetLastModifiedBy())
		require.Nil(t, m.GetLastModifiedAt())
		require.Nil(t, m.GetLocalOnlyHash())
		require.Nil(t, m.GetMergedDownstack())
		require.Nil(t, m.GetStackID())
	})

	t.Run("returns field values", func(t *testing.T) {
		t.Parallel()
		name := testBranchMain
		rev := "abc"
		scope := testScopeAPI
		hash := "xyz"
		stackID := testStackID1
		now := time.Now().Truncate(time.Second)

		m := git.NewMetaFrom(git.MetaFields{
			ParentBranchName:     &name,
			ParentBranchRevision: &rev,
			Scope:                &scope,
			LockReason:           git.LockReasonUser,
			BranchType:           git.BranchTypeUtility,
			LocalOnlyHash:        &hash,
			StackID:              &stackID,
			LastModifiedAt:       &now,
			LastModifiedBy:       &git.ModifiedBy{GitName: "test", GitEmail: "test@test.com"},
			PrInfo:               &git.PrInfoPersistence{},
			MergedDownstack:      []git.MergedParent{{BranchName: "old"}},
		})

		require.Equal(t, &name, m.GetParentBranchName())
		require.Equal(t, &rev, m.GetParentBranchRevision())
		require.Equal(t, &scope, m.GetScope())
		require.Equal(t, git.LockReasonUser, m.GetLockReason())
		require.Equal(t, git.BranchTypeUtility, m.GetBranchType())
		require.Equal(t, &hash, m.GetLocalOnlyHash())
		require.Equal(t, &stackID, m.GetStackID())
		require.NotNil(t, m.GetPrInfo())
		require.NotNil(t, m.GetLastModifiedBy())
		require.NotNil(t, m.GetLastModifiedAt())
		require.Equal(t, now, *m.GetLastModifiedAt())
		require.Len(t, m.GetMergedDownstack(), 1)
	})

	t.Run("complex getters return copies", func(t *testing.T) {
		t.Parallel()
		num := 42
		m := git.NewMetaFrom(git.MetaFields{
			PrInfo:          &git.PrInfoPersistence{Number: &num},
			LastModifiedBy:  &git.ModifiedBy{GitName: "original"},
			MergedDownstack: []git.MergedParent{{BranchName: "a"}, {BranchName: "b"}},
		})

		// Mutating returned PrInfo should not affect the original
		prInfo := m.GetPrInfo()
		newNum := 99
		prInfo.Number = &newNum
		require.Equal(t, &num, m.GetPrInfo().Number)

		// Mutating returned LastModifiedBy should not affect the original
		modBy := m.GetLastModifiedBy()
		modBy.GitName = "mutated"
		require.Equal(t, "original", m.GetLastModifiedBy().GitName)

		// Mutating returned slice should not affect the original
		ds := m.GetMergedDownstack()
		ds[0].BranchName = "mutated"
		require.Equal(t, "a", m.GetMergedDownstack()[0].BranchName)
	})
}

func TestMetaWithMethods(t *testing.T) {
	t.Parallel()

	t.Run("With returns new Meta without mutating original", func(t *testing.T) {
		t.Parallel()
		name := testBranchMain
		original := git.NewMetaFrom(git.MetaFields{
			ParentBranchName: &name,
			LockReason:       git.LockReasonNone,
		})

		newName := "develop"
		modified := original.WithParentBranchName(&newName).WithLockReason(git.LockReasonUser)

		// Original unchanged
		require.Equal(t, &name, original.GetParentBranchName())
		require.Equal(t, git.LockReasonNone, original.GetLockReason())

		// Modified has new values
		require.Equal(t, &newName, modified.GetParentBranchName())
		require.Equal(t, git.LockReasonUser, modified.GetLockReason())
	})

	t.Run("chaining multiple With calls", func(t *testing.T) {
		t.Parallel()
		name := testBranchMain
		rev := "abc"
		scope := testScopeAPI

		m := git.NewMeta().
			WithParentBranchName(&name).
			WithParentBranchRevision(&rev).
			WithScope(&scope).
			WithLockReason(git.LockReasonConsolidating).
			WithBranchType(git.BranchTypeUtility)

		require.Equal(t, &name, m.GetParentBranchName())
		require.Equal(t, &rev, m.GetParentBranchRevision())
		require.Equal(t, &scope, m.GetScope())
		require.Equal(t, git.LockReasonConsolidating, m.GetLockReason())
		require.Equal(t, git.BranchTypeUtility, m.GetBranchType())
	})

	t.Run("all With methods", func(t *testing.T) {
		t.Parallel()
		m := git.NewMeta()

		name := "parent"
		rev := "rev"
		scope := "scope"
		hash := "hash"
		stackID := "sid"
		now := time.Now()
		prInfo := &git.PrInfoPersistence{}
		modBy := &git.ModifiedBy{GitName: "test"}
		merged := []git.MergedParent{{BranchName: "x"}}

		result := m.
			WithParentBranchName(&name).
			WithParentBranchRevision(&rev).
			WithPrInfo(prInfo).
			WithScope(&scope).
			WithLockReason(git.LockReasonUser).
			WithBranchType(git.BranchTypeWorktreeAnchor).
			WithLastModifiedBy(modBy).
			WithLastModifiedAt(&now).
			WithLocalOnlyHash(&hash).
			WithMergedDownstack(merged).
			WithStackID(&stackID)

		require.Equal(t, &name, result.GetParentBranchName())
		require.Equal(t, &rev, result.GetParentBranchRevision())
		require.NotNil(t, result.GetPrInfo())
		require.Equal(t, &scope, result.GetScope())
		require.Equal(t, git.LockReasonUser, result.GetLockReason())
		require.Equal(t, git.BranchTypeWorktreeAnchor, result.GetBranchType())
		require.Equal(t, "test", result.GetLastModifiedBy().GitName)
		require.NotNil(t, result.GetLastModifiedAt())
		require.Equal(t, &hash, result.GetLocalOnlyHash())
		require.Len(t, result.GetMergedDownstack(), 1)
		require.Equal(t, &stackID, result.GetStackID())
	})
}

func TestMetaJSONRoundTrip(t *testing.T) {
	t.Parallel()

	t.Run("empty meta", func(t *testing.T) {
		t.Parallel()
		original := git.NewMeta()

		data, err := json.Marshal(original)
		require.NoError(t, err)
		require.Equal(t, "{}", string(data))

		var restored git.Meta
		err = json.Unmarshal(data, &restored)
		require.NoError(t, err)
		require.Nil(t, restored.GetParentBranchName())
	})

	t.Run("full meta round trip", func(t *testing.T) {
		t.Parallel()
		name := testBranchMain
		rev := "abc123"
		scope := testScopeAPI
		hash := "hash"
		stackID := testStackID1
		now := time.Now().UTC().Truncate(time.Second)
		num := 42
		url := "https://github.com/test/pr/42"
		ghUser := "testuser"

		original := git.NewMetaFrom(git.MetaFields{
			ParentBranchName:     &name,
			ParentBranchRevision: &rev,
			Scope:                &scope,
			LockReason:           git.LockReasonUser,
			BranchType:           git.BranchTypeUtility,
			LocalOnlyHash:        &hash,
			StackID:              &stackID,
			LastModifiedAt:       &now,
			LastModifiedBy: &git.ModifiedBy{
				GitName:        "Test User",
				GitEmail:       "test@example.com",
				GitHubUsername: &ghUser,
			},
			PrInfo: &git.PrInfoPersistence{
				Number: &num,
				URL:    &url,
			},
			MergedDownstack: []git.MergedParent{
				{BranchName: "old-parent"},
			},
		})

		data, err := json.Marshal(original)
		require.NoError(t, err)

		var restored git.Meta
		err = json.Unmarshal(data, &restored)
		require.NoError(t, err)

		require.Equal(t, name, *restored.GetParentBranchName())
		require.Equal(t, rev, *restored.GetParentBranchRevision())
		require.Equal(t, scope, *restored.GetScope())
		require.Equal(t, git.LockReasonUser, restored.GetLockReason())
		require.Equal(t, git.BranchTypeUtility, restored.GetBranchType())
		require.Equal(t, hash, *restored.GetLocalOnlyHash())
		require.Equal(t, stackID, *restored.GetStackID())
		require.Equal(t, now, restored.GetLastModifiedAt().UTC())
		require.Equal(t, "Test User", restored.GetLastModifiedBy().GitName)
		require.Equal(t, "test@example.com", restored.GetLastModifiedBy().GitEmail)
		require.Equal(t, ghUser, *restored.GetLastModifiedBy().GitHubUsername)
		require.Equal(t, num, *restored.GetPrInfo().Number)
		require.Equal(t, url, *restored.GetPrInfo().URL)
		require.Len(t, restored.GetMergedDownstack(), 1)
		require.Equal(t, "old-parent", restored.GetMergedDownstack()[0].BranchName)
	})

	t.Run("JSON backward compatibility with direct field access", func(t *testing.T) {
		t.Parallel()
		// Simulate JSON that was written by old code (direct struct marshaling)
		jsonStr := `{"parentBranchName":"main","lockReason":"user","stackId":"s1"}`

		var m git.Meta
		err := json.Unmarshal([]byte(jsonStr), &m)
		require.NoError(t, err)

		require.Equal(t, "main", *m.GetParentBranchName())
		require.Equal(t, git.LockReasonUser, m.GetLockReason())
		require.Equal(t, "s1", *m.GetStackID())
	})
}
