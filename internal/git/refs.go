package git

// BranchRefPrefix is the namespace Git stores local branches under.
const BranchRefPrefix = "refs/heads/"

// BranchRefName returns the fully qualified ref name for a local branch.
func BranchRefName(branchName string) string {
	return BranchRefPrefix + branchName
}

// RemoteBranchRefName returns the remote-tracking ref name for a branch on
// remote, e.g. refs/remotes/origin/main.
func RemoteBranchRefName(remote, branchName string) string {
	return "refs/remotes/" + remote + "/" + branchName
}
