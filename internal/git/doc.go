// Package git provides low-level Git operations.
//
// Operations that support multiple inputs expose a single plural API. Collect
// known inputs at the caller and pass them together; calling the plural method
// repeatedly in a loop still incurs repeated I/O. Implementations choose the
// appropriate path for zero, one, or many inputs.
//
// ReadRevisions, ReadCommitInfo, ReadMetadata, and ReadLocalMetadata return
// ReadResults keyed by the requested input. Get and One inspect those results
// without I/O. Missing revisions are errors; missing metadata yields empty
// metadata. Duplicate read inputs share one result.
//
// CreateBlobs returns SHAs in input order, including duplicate inputs. UpdateRefs
// applies one atomic transaction with optional per-ref expectations. PushBranches
// returns per-branch outcomes and permits partial success. An empty input list
// performs no writes, fetches, pushes, or merges.
package git
