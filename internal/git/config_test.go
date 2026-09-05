package git_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/testhelpers"
)

func TestConfigStore_GetSet(t *testing.T) {
	t.Parallel()

	t.Run("sets and gets a string value", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)

		store := git.NewConfigStore(scene.Dir)

		err := store.Set("stackit.test.key", "test-value")
		require.NoError(t, err)

		val, err := store.Get("stackit.test.key")
		require.NoError(t, err)
		require.Equal(t, "test-value", val)
	})

	t.Run("returns empty string for non-existent key", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)

		store := git.NewConfigStore(scene.Dir)

		val, err := store.Get("stackit.nonexistent")
		require.NoError(t, err)
		require.Empty(t, val)
	})

	t.Run("overwrites existing value", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)

		store := git.NewConfigStore(scene.Dir)

		err := store.Set("stackit.test.key", "first")
		require.NoError(t, err)

		err = store.Set("stackit.test.key", "second")
		require.NoError(t, err)

		val, err := store.Get("stackit.test.key")
		require.NoError(t, err)
		require.Equal(t, "second", val)
	})

	t.Run("round-trips a value containing newlines", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)

		store := git.NewConfigStore(scene.Dir)

		// stackit.ci.command is free-form user text and gates merge CI
		// validation, so a multi-line value silently truncated to its first
		// line would run half the command and report a clean gate.
		multiline := "make lint\nmake test"
		err := store.Set("stackit.ci.command", multiline)
		require.NoError(t, err)

		val, err := store.Get("stackit.ci.command")
		require.NoError(t, err)
		require.Equal(t, multiline, val)
	})
}

func TestConfigStore_Bool(t *testing.T) {
	t.Parallel()

	t.Run("sets and gets true", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)

		store := git.NewConfigStore(scene.Dir)

		err := store.SetBool("stackit.test.flag", true)
		require.NoError(t, err)

		val, err := store.GetBool("stackit.test.flag")
		require.NoError(t, err)
		require.True(t, val)
	})

	t.Run("sets and gets false", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)

		store := git.NewConfigStore(scene.Dir)

		err := store.SetBool("stackit.test.flag", false)
		require.NoError(t, err)

		val, err := store.GetBool("stackit.test.flag")
		require.NoError(t, err)
		require.False(t, val)
	})

	t.Run("returns false for non-existent key", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)

		store := git.NewConfigStore(scene.Dir)

		val, err := store.GetBool("stackit.nonexistent")
		require.NoError(t, err)
		require.False(t, val)
	})

	t.Run("GetBoolWithDefault returns default for non-existent key", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)

		store := git.NewConfigStore(scene.Dir)

		val := store.GetBoolWithDefault("stackit.nonexistent", true)
		require.True(t, val)

		val = store.GetBoolWithDefault("stackit.nonexistent", false)
		require.False(t, val)
	})

	t.Run("GetBoolWithDefault returns actual value when set", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)

		store := git.NewConfigStore(scene.Dir)

		err := store.SetBool("stackit.test.flag", false)
		require.NoError(t, err)

		val := store.GetBoolWithDefault("stackit.test.flag", true)
		require.False(t, val)
	})
}

func TestConfigStore_Int(t *testing.T) {
	t.Parallel()

	t.Run("sets and gets positive integer", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)

		store := git.NewConfigStore(scene.Dir)

		err := store.SetInt("stackit.test.count", 42)
		require.NoError(t, err)

		val, err := store.GetInt("stackit.test.count")
		require.NoError(t, err)
		require.Equal(t, 42, val)
	})

	t.Run("sets and gets zero", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)

		store := git.NewConfigStore(scene.Dir)

		err := store.SetInt("stackit.test.count", 0)
		require.NoError(t, err)

		val, err := store.GetInt("stackit.test.count")
		require.NoError(t, err)
		require.Equal(t, 0, val)
	})

	t.Run("returns 0 for non-existent key", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)

		store := git.NewConfigStore(scene.Dir)

		val, err := store.GetInt("stackit.nonexistent")
		require.NoError(t, err)
		require.Equal(t, 0, val)
	})

	t.Run("GetIntWithDefault returns default for non-existent key", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)

		store := git.NewConfigStore(scene.Dir)

		val := store.GetIntWithDefault("stackit.nonexistent", 100)
		require.Equal(t, 100, val)
	})

	t.Run("GetIntWithDefault returns actual value when set", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)

		store := git.NewConfigStore(scene.Dir)

		err := store.SetInt("stackit.test.count", 50)
		require.NoError(t, err)

		val := store.GetIntWithDefault("stackit.test.count", 100)
		require.Equal(t, 50, val)
	})

	t.Run("GetIntWithDefault returns 0 when explicitly set to 0", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)

		store := git.NewConfigStore(scene.Dir)

		err := store.SetInt("stackit.test.count", 0)
		require.NoError(t, err)

		// Should return 0, not the default of 100
		val := store.GetIntWithDefault("stackit.test.count", 100)
		require.Equal(t, 0, val)
	})
}

func TestConfigStore_MultiValue(t *testing.T) {
	t.Parallel()

	t.Run("adds and gets multiple values", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)

		store := git.NewConfigStore(scene.Dir)

		err := store.Add("stackit.test.list", "first")
		require.NoError(t, err)

		err = store.Add("stackit.test.list", "second")
		require.NoError(t, err)

		err = store.Add("stackit.test.list", "third")
		require.NoError(t, err)

		vals, err := store.GetAll("stackit.test.list")
		require.NoError(t, err)
		require.Equal(t, []string{"first", "second", "third"}, vals)
	})

	t.Run("returns nil for non-existent multi-value key", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)

		store := git.NewConfigStore(scene.Dir)

		vals, err := store.GetAll("stackit.nonexistent")
		require.NoError(t, err)
		require.Nil(t, vals)
	})

	t.Run("round-trips a value containing newlines without splitting it", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)

		store := git.NewConfigStore(scene.Dir)

		// stackit.hooks.approved.* stores free-form approved hook commands.
		// A multi-line command split into fragments here would never match
		// the original string again, so approval would never stick.
		multiline := "make lint\nmake test"
		err := store.Add("stackit.test.list", "first")
		require.NoError(t, err)

		err = store.Add("stackit.test.list", multiline)
		require.NoError(t, err)

		err = store.Add("stackit.test.list", "third")
		require.NoError(t, err)

		vals, err := store.GetAll("stackit.test.list")
		require.NoError(t, err)
		require.Equal(t, []string{"first", multiline, "third"}, vals)
	})
}

func TestConfigStore_Unset(t *testing.T) {
	t.Parallel()

	t.Run("removes existing key", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)

		store := git.NewConfigStore(scene.Dir)

		err := store.Set("stackit.test.key", "value")
		require.NoError(t, err)

		err = store.Unset("stackit.test.key")
		require.NoError(t, err)

		val, err := store.Get("stackit.test.key")
		require.NoError(t, err)
		require.Empty(t, val)
	})

	t.Run("does not error for non-existent key", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)

		store := git.NewConfigStore(scene.Dir)

		err := store.Unset("stackit.nonexistent")
		require.NoError(t, err)
	})

	t.Run("removes all values for multi-value key", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)

		store := git.NewConfigStore(scene.Dir)

		err := store.Add("stackit.test.list", "first")
		require.NoError(t, err)

		err = store.Add("stackit.test.list", "second")
		require.NoError(t, err)

		err = store.Unset("stackit.test.list")
		require.NoError(t, err)

		vals, err := store.GetAll("stackit.test.list")
		require.NoError(t, err)
		require.Nil(t, vals)
	})
}

func TestConfigStore_Exists(t *testing.T) {
	t.Parallel()

	t.Run("returns true for existing key", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)

		store := git.NewConfigStore(scene.Dir)

		err := store.Set("stackit.test.key", "value")
		require.NoError(t, err)

		require.True(t, store.Exists("stackit.test.key"))
	})

	t.Run("returns false for non-existent key", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)

		store := git.NewConfigStore(scene.Dir)

		require.False(t, store.Exists("stackit.nonexistent"))
	})
}
