package tui

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestShippableStoriesRegistered(t *testing.T) {
	t.Parallel()
	// Verify shippable stories are registered
	shippableCount := 0
	mergeFlowCount := 0

	for _, s := range Stories {
		switch s.Category {
		case "Shippable":
			shippableCount++
		case "Merge Flow":
			mergeFlowCount++
		}
	}

	require.GreaterOrEqual(t, shippableCount, 7, "Expected at least 7 Shippable stories")
	require.GreaterOrEqual(t, mergeFlowCount, 3, "Expected at least 3 Merge Flow stories")
}

func TestShippableStoryModelCreation(t *testing.T) {
	t.Parallel()
	// Verify each scenario creates a valid model
	for _, scenario := range shippableScenarios {
		model := newShippableStoryModel(scenario)
		require.NotNil(t, model, "Model should be created for scenario: %s", scenario.name)

		// Run Init
		cmd := model.Init()
		require.NotNil(t, cmd, "Init should return a command")
	}
}

func TestMergeProgressSimulationCreation(t *testing.T) {
	t.Parallel()
	scenarios := []mergeScenarioType{
		mergeScenarioHappyPath,
		mergeScenarioWaitingCI,
		mergeScenarioFailure,
	}

	for _, scenario := range scenarios {
		model := newMergeProgressSimulation(scenario)
		require.NotNil(t, model, "Model should be created for scenario")
		require.Len(t, model.groups, 4, "Should have 4 merge groups")
		require.Len(t, model.steps, 4, "Should have 4 steps")
	}
}
