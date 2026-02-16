// Package install copies bundled skill and agent definitions to a target
// directory (typically the user's opencode configuration). The embedded
// assets are compiled into the binary via go:embed in assets_embed.go.
package install

import (
	"bytes"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
)

const (
	// maxEmbeddedAssets is an explicit ceiling on the number of files the
	// embedded FS may contain. Raise deliberately if the asset set grows.
	maxEmbeddedAssets = 256

	// dirPermissions is used when creating parent directories.
	dirPermissions fs.FileMode = 0755
	// filePermissions is used when writing asset files.
	filePermissions fs.FileMode = 0644
)

// Action describes what will happen (or did happen) to a single file.
type Action int

const (
	// ActionWrite means the file will be created or overwritten.
	ActionWrite Action = iota
	// ActionSkip means the file is already up to date.
	ActionSkip
)

// String returns a human-readable label for the action.
func (a Action) String() string {
	switch a {
	case ActionWrite:
		return "write"
	case ActionSkip:
		return "skip"
	default:
		return fmt.Sprintf("Action(%d)", int(a))
	}
}

// FileAction describes a planned or completed operation on a single file.
type FileAction struct {
	// RelPath is the path relative to the assets root (e.g. "skills/review-auto/SKILL.md").
	RelPath string
	// TargetPath is the absolute path where the file will be written.
	TargetPath string
	// Action is what will happen (or did happen).
	Action Action
	// Err is non-nil if the write failed.
	Err error

	// srcData holds the embedded file content, populated by Plan and consumed
	// by Execute. This avoids reading the embedded FS twice.
	srcData []byte
}

// Result summarizes the outcome of an install operation.
type Result struct {
	Written int
	Skipped int
	Errors  int
}

// Plan walks the embedded assets and determines what actions would be taken
// against targetDir. It does not write any files. The returned FileActions
// carry the source bytes so Execute can write them without re-reading.
func Plan(targetDir string) ([]FileAction, error) {
	if targetDir == "" {
		panic("install.Plan: targetDir must not be empty")
	}

	var actions []FileAction

	err := fs.WalkDir(assetsFS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		srcData, readErr := fs.ReadFile(assetsFS, path)
		if readErr != nil {
			return fmt.Errorf("reading embedded %s: %w", path, readErr)
		}

		targetPath := filepath.Join(targetDir, path)

		action := ActionWrite
		existing, readErr := os.ReadFile(targetPath)
		if readErr == nil && bytes.Equal(existing, srcData) {
			action = ActionSkip
		}

		actions = append(actions, FileAction{
			RelPath:    path,
			TargetPath: targetPath,
			Action:     action,
			srcData:    srcData,
		})

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walking embedded assets: %w", err)
	}

	// The embedded FS is compiled in — zero files means a build/packaging error.
	if len(actions) == 0 {
		panic("install.Plan: embedded assets produced zero actions — build is broken")
	}
	if len(actions) > maxEmbeddedAssets {
		panic(fmt.Sprintf("install.Plan: embedded assets (%d) exceed limit of %d — accidental embed?", len(actions), maxEmbeddedAssets))
	}

	slog.Debug("plan complete", "target", targetDir, "total", len(actions))

	return actions, nil
}

// Execute writes files to disk using the pre-computed plan from Plan.
// It attempts all files even if some fail, collecting errors per-file.
// Callers should check result.Errors and inspect individual FileAction.Err
// for details on failures.
func Execute(actions []FileAction) *Result {
	if len(actions) == 0 {
		panic("install.Execute: actions must not be empty")
	}

	result := &Result{}

	for i := range actions {
		fa := &actions[i]

		// Precondition: every action must have been produced by Plan.
		if fa.RelPath == "" || fa.TargetPath == "" {
			panic(fmt.Sprintf("install.Execute: action[%d] has empty path (rel=%q, target=%q)", i, fa.RelPath, fa.TargetPath))
		}
		if fa.Action == ActionWrite && len(fa.srcData) == 0 {
			panic(fmt.Sprintf("install.Execute: action[%d] (%s) is ActionWrite with empty srcData", i, fa.RelPath))
		}

		if fa.Action == ActionSkip {
			result.Skipped++
			slog.Debug("skip (up to date)", "path", fa.RelPath, "target", fa.TargetPath)
			continue
		}

		// Create parent directory.
		dir := filepath.Dir(fa.TargetPath)
		if mkErr := os.MkdirAll(dir, dirPermissions); mkErr != nil {
			fa.Err = fmt.Errorf("creating directory %s: %w", dir, mkErr)
			result.Errors++
			slog.Error("mkdir failed", "dir", dir, "err", mkErr)
			continue
		}

		// Write the file using bytes carried from Plan.
		if writeErr := os.WriteFile(fa.TargetPath, fa.srcData, filePermissions); writeErr != nil {
			fa.Err = writeErr
			result.Errors++
			slog.Error("write failed", "path", fa.RelPath, "target", fa.TargetPath, "err", writeErr)
			continue
		}

		// Paired assertion: verify what we wrote to disk matches source.
		written, readBackErr := os.ReadFile(fa.TargetPath)
		if readBackErr != nil {
			fa.Err = fmt.Errorf("read-back verification failed for %s: %w", fa.TargetPath, readBackErr)
			result.Errors++
			continue
		}
		if !bytes.Equal(written, fa.srcData) {
			fa.Err = fmt.Errorf("write verification failed for %s: content mismatch after write", fa.TargetPath)
			result.Errors++
			continue
		}

		result.Written++
		slog.Debug("wrote", "path", fa.RelPath, "target", fa.TargetPath, "bytes", len(fa.srcData))
	}

	// Postcondition: counters must account for every action.
	total := result.Written + result.Skipped + result.Errors
	if total != len(actions) {
		panic(fmt.Sprintf(
			"install.Execute: counter mismatch: written(%d) + skipped(%d) + errors(%d) = %d, but len(actions) = %d",
			result.Written, result.Skipped, result.Errors, total, len(actions),
		))
	}

	return result
}
