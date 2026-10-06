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

		affectedLinks, err := n.findAffected(idx, toMove)
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

// resolveLink resolves a Link into a ResolvedLink by resolving its href.
// Returns nil, nil if the link cannot be resolved to an internal note or does not have a valid offset.
func resolveLink(idx NoteIndex, link Link, note MinimalNote) (*ResolvedLink, error) {
	if link.IsExternal || link.LinkStart < 0 || link.LinkEnd < 0 {
		return nil, nil
	}
	partial := link.Type == LinkTypeWikiLink
	destNotes, err := idx.FindMinimal(NoteFindOpts{
		IncludeHrefs:      []string{link.Href},
		AllowPartialHrefs: partial,
		Limit:             1,
	})
	if err != nil {
		return nil, err
	}
	if len(destNotes) == 0 {
		return nil, nil
	}

	// The LinkID is not filled in, as that would require a schema change and invasive changes in link_dao.go.
	// Its also not needed, as LinkID is not used in renaming.
	return &ResolvedLink{
		Link:       link,
		SourceID:   note.ID,
		SourcePath: note.Path,
		TargetID:   destNotes[0].ID,
		TargetPath: destNotes[0].Path,
	}, nil
}

// findAffected finds all ResolvedLinks that need to be changed,
// grouped by the path for easy edit batching.
// This is a separate step, as after the database repoint,
// notes no longer resolve to their old hrefs.
func (n *Notebook) findAffected(idx NoteIndex, movedNotes []MinimalNote) (map[string][]ResolvedLink, error) {
	paths := make([]string, 0, len(movedNotes))
	for _, note := range movedNotes {
		paths = append(paths, note.Path)
	}

	candidates, err := idx.FindMinimal(NoteFindOpts{
		LinkTo:            &LinkFilter{Hrefs: paths},
		AllowPartialHrefs: false,
	})
	if err != nil {
		return nil, err
	}

	// Moved notes have different rules for including in the rewrite set than notes outside the move.
	movedSet := make(map[NoteID]struct{}, len(movedNotes))
	// Dedup the candidates by path.
	candidateSet := make(map[string]MinimalNote, len(candidates))
	for _, note := range movedNotes {
		candidateSet[note.Path] = note
		movedSet[note.ID] = struct{}{}
	}
	for _, c := range candidates {
		candidateSet[c.Path] = c
	}

	affected := make(map[string][]ResolvedLink)
	for _, candidate := range candidateSet {
		parsedContent, err := n.ParseNoteAt(filepath.Join(n.Path, candidate.Path))
		if err != nil {
			return nil, err
		}
		_, moved := movedSet[candidate.ID]
		for _, link := range parsedContent.Links {
			resolvedLink, err := resolveLink(idx, link, candidate)
			if err != nil {
				return nil, err
			}
			if resolvedLink == nil {
				continue
			}
			_, resolvesToMoved := movedSet[resolvedLink.TargetID]
			// links that point a note being moved always need to be rewritten.
			// Markdown links in notes that are being moved may have relative paths that must be rewritten.
			if resolvesToMoved || (moved && link.Type == LinkTypeMarkdown) {
				affected[candidate.Path] = append(affected[candidate.Path], *resolvedLink)
			}
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
