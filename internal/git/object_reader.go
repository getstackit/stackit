package git

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
)

// objectReader wraps a persistent `git cat-file --batch` process, providing
// zero-subprocess-overhead reads for any git object (blob, commit, tag, tree).
// A single instance is shared across all ReadBlob / ReadMetadata calls on a
// runner; the mutex serializes stdin/stdout access.
type objectReader struct {
	mu     sync.Mutex
	dirFn  func() (string, error) // called once when the process is first started
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
}

func newObjectReader(dirFn func() (string, error)) *objectReader {
	r := &objectReader{dirFn: dirFn}
	runtime.SetFinalizer(r, (*objectReader).Close)
	return r
}

func (r *objectReader) start() error {
	dir, err := r.dirFn()
	if err != nil {
		return fmt.Errorf("object reader: could not resolve repo root: %w", err)
	}
	cmd := exec.Command("git", "cat-file", "--batch")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("object reader stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return fmt.Errorf("object reader stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		return fmt.Errorf("object reader start: %w", err)
	}
	r.cmd = cmd
	r.stdin = stdin
	r.stdout = bufio.NewReaderSize(stdout, 64*1024)
	return nil
}

// killLocked terminates the current process and releases its stdin pipe and
// process resources so a restart doesn't leak the pipe's file descriptor or
// leave a zombie process behind. Caller must hold r.mu.
func (r *objectReader) killLocked() {
	_ = r.stdin.Close()
	_ = r.cmd.Process.Kill()
	_ = r.cmd.Wait()
	r.cmd = nil
}

// readResponse reads one response from stdout. Caller must hold r.mu.
func (r *objectReader) readResponse(ref string) (content, sha string, found bool, err error) {
	header, err := r.stdout.ReadString('\n')
	if err != nil {
		return "", "", false, fmt.Errorf("object reader read header for %s: %w", ref, err)
	}
	header = strings.TrimSuffix(header, "\n")

	// Missing: "<ref> missing"
	if strings.HasSuffix(header, " missing") {
		return "", "", false, nil
	}

	// Present: "<sha> <type> <size>"
	parts := strings.Fields(header)
	if len(parts) != 3 {
		return "", "", false, fmt.Errorf("object reader unexpected header %q for %s", header, ref)
	}
	size, err := strconv.Atoi(parts[2])
	if err != nil {
		return "", "", false, fmt.Errorf("object reader bad size in header %q: %w", header, err)
	}

	// Read exactly size bytes + the trailing newline git appends after each object
	buf := make([]byte, size+1)
	if _, err := io.ReadFull(r.stdout, buf); err != nil {
		return "", "", false, fmt.Errorf("object reader read body for %s: %w", ref, err)
	}
	// parts[0] is the resolved object's SHA. Metadata refs point straight at a
	// blob, so this is the value a later compare-and-swap update must match.
	return string(buf[:size]), parts[0], true, nil
}

// ReadObject reads one object by ref name or SHA, restarting the process once on I/O failure.
func (r *objectReader) ReadObject(ref string) (string, bool, error) {
	content, _, found, err := r.ReadObjectWithSHA(ref)
	return content, found, err
}

// ReadObjectWithSHA is ReadObject plus the resolved object's SHA. Callers that
// intend to write the ref back use the SHA to detect another process having
// written it in between.
func (r *objectReader) ReadObjectWithSHA(ref string) (content, sha string, found bool, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cmd == nil {
		if err := r.start(); err != nil {
			return "", "", false, err
		}
	}
	if _, err := fmt.Fprintf(r.stdin, "%s\n", ref); err != nil {
		// Process died; restart and retry once
		r.killLocked()
		if startErr := r.start(); startErr != nil {
			return "", "", false, startErr
		}
		if _, err := fmt.Fprintf(r.stdin, "%s\n", ref); err != nil {
			return "", "", false, fmt.Errorf("object reader write after restart: %w", err)
		}
	}
	content, sha, found, err = r.readResponse(ref)
	if err != nil {
		// Stdout stream is broken; discard the process so the next call
		// restarts, mirroring ReadObjectsBatch.
		r.killLocked()
	}
	return content, sha, found, err
}

// BatchObject is one object's content together with the SHA the ref resolved
// to. Callers that intend to write the ref back keep the SHA so they can detect
// another process having written it in between.
type BatchObject struct {
	Content string
	SHA     string
}

// ReadObjectsBatch sends all refs in one burst and reads all responses in order.
// More efficient than N individual ReadObject calls: one lock acquisition, one
// write loop, one read loop, zero per-item subprocess overhead.
//
// The SHA comes back alongside the content because the batch path is the one
// almost every command actually uses. Dropping it left those reads with no
// compare-and-swap expectation to write against, so the lost-update guard only
// ever engaged for the rare single-branch ReadMetadata caller.
func (r *objectReader) ReadObjectsBatch(refs []string) (map[string]BatchObject, error) {
	if len(refs) == 0 {
		return nil, nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cmd == nil {
		if err := r.start(); err != nil {
			return nil, err
		}
	}
	for _, ref := range refs {
		if _, err := fmt.Fprintf(r.stdin, "%s\n", ref); err != nil {
			// Process died; clear it so the next call restarts, mirroring ReadObject.
			r.killLocked()
			return nil, fmt.Errorf("object reader write: %w", err)
		}
	}
	results := make(map[string]BatchObject, len(refs))
	for _, ref := range refs {
		content, sha, found, err := r.readResponse(ref)
		if err != nil {
			// Stdout stream is broken; discard the process so the next call restarts.
			r.killLocked()
			return nil, err
		}
		if found {
			results[ref] = BatchObject{Content: content, SHA: sha}
		}
	}
	return results, nil
}

// Close terminates the underlying process. Safe to call multiple times.
func (r *objectReader) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cmd != nil {
		_ = r.stdin.Close()
		_ = r.cmd.Wait()
		r.cmd = nil
	}
}
