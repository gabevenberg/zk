package core

import (
	"fmt"
	"path/filepath"
	"strings"
)

// RenameOpts holds a set of options for renaming notes.
type RenameOpts struct {
	// File/dir to be moved, relative to notebook root.
	OldPath string
	// Destination to move the file/dir to, relative to notebook root.
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
	// Path that the edit applies to relative to notebook root.
	Path string
	// List of edits to make to that file,
	// Sorted by EditStart field.
	// Guaranteed to be non-overlapping.
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

// Rename renames a note or directory of notes, keeping note links intact.
// TODO: Currently does not work with non-note links, such as image links.
// Doing so requires much more invasive changes
// If DryRun is set, only returns a RenamePlan.
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

		affectedLinks, err := findAffected(idx, toMove)
		if err != nil {
			return err
		}

		_ = affectedLinks

		return nil
	})
	if err != nil {
		return RenamePlan{}, fmt.Errorf("rename: %w", err)
	}
	return renamePlan, nil

}

// findAffected finds all ResolvedLinks that need to be changed,
// grouped by the path for easy edit batching.
// This is a separate step,
// as a repoint alters SourcePath.
func findAffected(idx NoteIndex, movedNotes []MinimalNote) (map[string][]ResolvedLink, error) {
	ids := make([]NoteID, 0, len(movedNotes))
	for _, note := range movedNotes {
		ids = append(ids, note.ID)
	}
	candidates, err := idx.FindLinksTouchingNotes(ids)
	if err != nil {
		return nil, err
	}
	// Moved notes have different rules for including in the rewrite set than notes outside the move.
	movedSet := make(map[NoteID]struct{}, len(movedNotes))
	for _, id := range ids {
		movedSet[id] = struct{}{}
	}

	affected := make(map[string][]ResolvedLink)
	for _, candidate := range candidates {
		// check for links with invalid IDs (external/broken) or offsets.
		if (!candidate.SourceID.IsValid() || !candidate.TargetID.IsValid()) || (candidate.LinkStart < 0 || candidate.LinkEnd < 0) {
			continue
		}

		_, moved := movedSet[candidate.SourceID]
		_, targetsMoved := movedSet[candidate.TargetID]

		// links that point a note being moved always need to be rewritten.
		// Markdown links in notes that are being moved may have relative paths that must be rewritten.
		if targetsMoved || (moved && candidate.Type == LinkTypeMarkdown) {
			affected[candidate.SourcePath] = append(affected[candidate.SourcePath], candidate)
		}
	}
	return affected, nil
}

// findMoveSet finds all notes that will be moved.
// In the trivial case of moving a note, will just return that note.
// In the case of moving a directory, will return all notes in that dir and subdirectories.
func findMoveSet(idx NoteIndex, oldPath string, oldPathIsFile bool) ([]MinimalNote, error) {
	// even with AllowPartialHrefs false, prefixes match.
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
			// We use HasPrefix here because FileStorage.IsDescendantOf canonicalizes first,
			// so things get weird with symlinks in a way that can break moving in a notebook with them.
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
