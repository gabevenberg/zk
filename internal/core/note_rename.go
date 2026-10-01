package core

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
	Path  string
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
