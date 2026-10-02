package core

import (
	"fmt"
	"path/filepath"
	"strings"
)

// All paths are relative to the notebook root.

type RenameOpts struct {
	OldPath string
	NewPath string
	// Whether to apply the edit, or just make and verify a plan.
	DryRun bool
	// Whether to overwrite files at the destination.
	Force bool
}

type RenamePlan struct {
	OldPath string
	NewPath string
	Edits   []FileEdit
}

type FileEdit struct {
	// Path that the edit applies to
	Path string
	// Sorted by EditStart field.
	Edits []Edit
}

type Edit struct {
	// The content before the edit
	// (used for checking and rolling back.)
	OldContent string
	// The content to replace OldContent with
	NewContent string
	// The byte offset of the first byte of OldContent
	EditStart int
	// The byte offset of the byte *after* the last byte of OldContent.
	// (inclusive-exclusive range)
	EditEnd int
}

func (n *Notebook) Rename(opts RenameOpts) (RenamePlan, error) {
	renamePlan := RenamePlan{}
	renamePlan.OldPath = filepath.Clean(opts.OldPath)
	renamePlan.NewPath = filepath.Clean(opts.NewPath)
	if renamePlan.OldPath == renamePlan.NewPath {
		return RenamePlan{}, fmt.Errorf("rename: old path and new path are the same, nothing to move")
	}
	if renamePlan.OldPath == "." {
		return RenamePlan{}, fmt.Errorf("rename: old path is notebook root")
	}
	if renamePlan.NewPath == "." {
		return RenamePlan{}, fmt.Errorf("rename: new path is notebook root")
	}

	oldPathIsFile, err := n.fs.FileExists(filepath.Join(n.Path, renamePlan.OldPath))
	if err != nil {
		return RenamePlan{}, fmt.Errorf("rename: %w", err)
	}
	oldPathIsDir, err := n.fs.DirExists(filepath.Join(n.Path, renamePlan.OldPath))
	if err != nil {
		return RenamePlan{}, fmt.Errorf("rename: %w", err)
	}
	if !oldPathIsFile && !oldPathIsDir {
		return RenamePlan{}, fmt.Errorf("rename: %s: does not exist", renamePlan.OldPath)
	}

	if !opts.Force {
		newPathIsFile, err := n.fs.FileExists(filepath.Join(n.Path, renamePlan.NewPath))
		if err != nil {
			return RenamePlan{}, fmt.Errorf("rename: %w", err)
		}
		newPathIsDir, err := n.fs.DirExists(filepath.Join(n.Path, renamePlan.NewPath))
		if err != nil {
			return RenamePlan{}, fmt.Errorf("rename: %w", err)
		}
		if newPathIsFile || newPathIsDir {
			return RenamePlan{}, fmt.Errorf("rename: %s: path already exists", renamePlan.NewPath)
		}
	}

	err = n.index.Commit(func(idx NoteIndex) error {
		toMove, err := findMoveSet(idx, renamePlan.OldPath, oldPathIsFile)
		if err != nil {
			return err
		}
		_=toMove
		return nil
	})
	if err != nil {
		return RenamePlan{}, fmt.Errorf("rename: %w", err)
	}
	return renamePlan, nil

}

func findMoveSet(idx NoteIndex, oldPath string, oldPathIsFile bool) ([]MinimalNote, error) {
	candidates, err := idx.FindMinimal(NoteFindOpts{
		IncludeHrefs:      []string{oldPath},
		AllowPartialHrefs: false,
	})
	if err != nil {
		return nil, err
	}
	prefix := oldPath + string(filepath.Separator)
	toMove := []MinimalNote{}
	for _, candidate := range candidates {
		if oldPathIsFile {
			if candidate.Path == oldPath {
				toMove = append(toMove, candidate)
				break
			}
		} else {
			if strings.HasPrefix(candidate.Path, prefix) {
				toMove = append(toMove, candidate)
			}
		}
	}
	if len(toMove) == 0 {
		return nil, fmt.Errorf("%s: no indexed notes to move", oldPath)
	}
	return toMove, nil
}
