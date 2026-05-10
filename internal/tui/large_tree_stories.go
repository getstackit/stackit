package tui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/getstackit/stackit/internal/tui/components/tree"
)

func registerLargeTreeStories() {
	RegisterStory(Story{
		Name:        "Large Tree",
		Category:    "Tree",
		Description: "A large, deep, and wide tree for testing scrolling and collapsing",
		CreateModel: func() tea.Model {
			const trunk = "main"
			mock := &tree.MockTreeData{
				CurrentVal:  "feature-5-1-1",
				TrunkVal:    trunk,
				ChildrenMap: make(map[string][]string),
				ParentsMap:  make(map[string]string),
				FixedMap:    make(map[string]bool),
			}

			// Generate a deep and wide tree
			// main -> feature-1, feature-2, feature-3
			// feature-1 -> feature-1-1, feature-1-2
			// ... and so on
			mock.ChildrenMap[trunk] = []string{"feature-1", "feature-2", "feature-3"}
			mock.ParentsMap["feature-1"] = trunk
			mock.ParentsMap["feature-2"] = trunk
			mock.ParentsMap["feature-3"] = trunk
			mock.FixedMap[trunk] = true

			for i := 1; i <= 3; i++ {
				parentName := fmt.Sprintf("feature-%d", i)
				mock.FixedMap[parentName] = true
				var children []string
				for j := 1; j <= 3; j++ {
					childName := fmt.Sprintf("feature-%d-%d", i, j)
					children = append(children, childName)
					mock.ParentsMap[childName] = parentName
					mock.FixedMap[childName] = true

					var grandChildren []string
					for k := 1; k <= 3; k++ {
						grandChildName := fmt.Sprintf("feature-%d-%d-%d", i, j, k)
						grandChildren = append(grandChildren, grandChildName)
						mock.ParentsMap[grandChildName] = childName
						mock.FixedMap[grandChildName] = k%2 == 0 // Some need restack
					}
					mock.ChildrenMap[childName] = grandChildren
				}
				mock.ChildrenMap[parentName] = children
			}

			renderer := tree.NewRenderer(mock)

			// Add annotations to various branches
			allBranches := []string{trunk}
			var collect func(string)
			collect = func(name string) {
				children := mock.ChildrenMap[name]
				for _, child := range children {
					allBranches = append(allBranches, child)
					collect(child)
				}
			}
			collect(trunk)

			scopes := []string{"CORE", "API", "UI", "DB", "AUTH", "CLI"}
			for i, name := range allBranches {
				if name == trunk {
					continue
				}

				prNum := 200 + i
				scope := scopes[i%len(scopes)]
				ann := tree.BranchAnnotation{
					PRNumber:      &prNum,
					Scope:         scope,
					ExplicitScope: scope,
					CommitCount:   (i % 5) + 1,
					LinesAdded:    (i % 10) * 10,
					LinesDeleted:  (i % 5) * 2,
					CheckStatus:   tree.CheckStatusPassing,
				}

				// Variety in status
				switch {
				case i%7 == 0:
					ann.CheckStatus = tree.CheckStatusFailing
					ann.ReviewStatus = "Changes Requested"
				case i%11 == 0:
					ann.CheckStatus = tree.CheckStatusPending
				case i%13 == 0:
					ann.ReviewStatus = "Approved"
				case i%17 == 0:
					ann.PRState = "MERGED"
				case i%19 == 0:
					ann.IsDraft = true
				}

				renderer.SetAnnotation(name, ann)
			}

			return tree.NewModel(renderer)
		},
	})
}
