package tree

import "github.com/getstackit/stackit/internal/engine"

// MockTreeData provides test data for tree rendering.
// It implements the TreeData interface and is exported to be used
// in both tests and the TUI storyboard.
type MockTreeData struct {
	// CurrentVal is the current branch name
	CurrentVal string
	// TrunkVal is the trunk branch name
	TrunkVal string
	// ChildrenMap maps branch name to child branch names
	ChildrenMap map[string][]string
	// ParentsMap maps branch name to parent branch name
	ParentsMap engine.ParentMap
	// FixedMap maps branch name to whether it's fixed (up-to-date)
	FixedMap map[string]bool
}

// NewMockTreeData creates a new MockTreeData with sample data.
func NewMockTreeData() *MockTreeData {
	return &MockTreeData{
		CurrentVal: "feature-2",
		TrunkVal:   "main",
		ChildrenMap: map[string][]string{
			"main":      {"feature-1"},
			"feature-1": {"feature-2"},
			"feature-2": {},
		},
		ParentsMap: engine.ParentMap{
			"feature-1": "main",
			"feature-2": "feature-1",
		},
		FixedMap: map[string]bool{
			"main":      true,
			"feature-1": true,
			"feature-2": true,
		},
	}
}

// TreeData interface implementation

// CurrentBranch implements TreeData.CurrentBranch
func (m *MockTreeData) CurrentBranch() string {
	return m.CurrentVal
}

// Trunk implements TreeData.Trunk
func (m *MockTreeData) Trunk() string {
	return m.TrunkVal
}

// Children implements TreeData.Children
func (m *MockTreeData) Children(branchName string) []string {
	return m.ChildrenMap[branchName]
}

// Parent implements TreeData.Parent
func (m *MockTreeData) Parent(branchName string) string {
	return m.ParentsMap.Parent(branchName)
}

// IsTrunk implements TreeData.IsTrunk
func (m *MockTreeData) IsTrunk(branchName string) bool {
	return branchName == m.TrunkVal
}

// IsFixed implements TreeData.IsFixed
func (m *MockTreeData) IsFixed(branchName string) bool {
	return m.FixedMap[branchName]
}
