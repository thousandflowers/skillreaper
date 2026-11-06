// Package prune quarantines unused items reversibly. Nothing is ever
// deleted: files move to <claudeDir>/reaped/, config entries are
// backed up and their payload stored in a manifest so Restore can
// re-insert them.
package prune

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/thousandflowers/skillreaper/internal/atomicfile"
	"github.com/thousandflowers/skillreaper/internal/jsonedit"
	"github.com/thousandflowers/skillreaper/internal/safepath"
	"github.com/thousandflowers/skillreaper/internal/scan"
)

// ErrAlreadyGone reports that an item selected for quarantine no longer exists
// by the time the move is attempted. It is not a failure of this run: another
// prune, or a manual removal, got there first. Callers should skip the item
// rather than abort, since the remaining candidates are still valid.
var ErrAlreadyGone = errors.New("item already moved by another process")

const manifestVersion = 1
const maxManifestFileSize = 10 << 20
const maxConfigFileSize = 10 << 20

// Manifest wraps the list of prune entries together with format
// versioning. When the manifest format changes, bump manifestVersion
// and handle migration in LoadManifest.
type Manifest struct {
	Version       int       `json:"version"`
	QuarantinedAt time.Time `json:"quarantined_at"`
	Entries       []Entry   `json:"entries"`
}

// Entry records one reversible prune action.
type Entry struct {
	ID       string `json:"id"`
	Category string `json:"category"`
	Name     string `json:"name"`
	// Platform is the tool the item belonged to. Empty on older entries.
	Platform string `json:"platform,omitempty"`
	// Source is where the item came from, as the report names it. A plugin item
	// back after an update has a new path, and this is how it is known again.
	Source string `json:"source,omitempty"`
	From   string `json:"from,omitempty"` // original path (file/dir moves)
	// Root is the platform config directory From lived under, recorded at prune
	// time so restore can bound the write to the same tree the item came from.
	// Empty in manifests written before multi-platform pruning; those entries
	// fall back to the Claude directory, which is where they came from.
	Root string `json:"root,omitempty"`
	To   string `json:"to,omitempty"` // quarantine path
	// Link marks a pruned linked skill: To is the moved symlink, whose target
	// lies outside the Claude directory, so restore checks the link itself.
	Link bool `json:"link,omitempty"`
	// FilesFrom/FilesTo: for a skill reinvest wrote, its folder, moved into
	// the quarantine next to the link so it is gone but restorable.
	FilesFrom  string          `json:"filesFrom,omitempty"`
	FilesTo    string          `json:"filesTo,omitempty"`
	ConfigPath string          `json:"configPath,omitempty"`
	JSONScope  string          `json:"jsonScope,omitempty"` // "" = top-level mcpServers, else project path
	Payload    json.RawMessage `json:"payload,omitempty"`   // removed MCP server config
	// Backup is the config file as it was before this removal, and AfterSHA the
	// hash of the file right after it. A file still matching AfterSHA at restore
	// time gets Backup back byte for byte.
	Backup   string `json:"backup,omitempty"`
	AfterSHA string `json:"afterSha,omitempty"`
	// Tokens is what the item weighed per session when it was pruned.
	Tokens    int       `json:"tokens,omitempty"`
	Timestamp time.Time `json:"timestamp"`
	Restored  bool      `json:"restored"`
}

// Note is what the caller knows about an entry that the prune itself does not.
type Note struct {
	Tokens   int
	Platform string
	Source   string
}

// RecordNotes adds the caller's notes to entries by id.
func RecordNotes(claudeDir string, notes map[string]Note) error {
	if len(notes) == 0 {
		return nil
	}
	entries, err := LoadManifest(claudeDir)
	if err != nil {
		return err
	}
	for i, e := range entries {
		if n, ok := notes[e.ID]; ok {
			entries[i].Tokens = n.Tokens
			entries[i].Platform = n.Platform
			entries[i].Source = n.Source
		}
	}
	return saveManifest(claudeDir, entries)
}

func reapedDir(claudeDir string) string { return filepath.Join(claudeDir, "reaped") }
func manifestPath(claudeDir string) string {
	return filepath.Join(reapedDir(claudeDir), "manifest.json")
}

// LoadManifest returns all recorded prune actions (empty when none).
// Transparently handles the legacy flat-array format (v0) by wrapping
// into a Manifest on read.
func LoadManifest(claudeDir string) ([]Entry, error) {
	b, err := safepath.ReadRegularFileWithin(claudeDir, manifestPath(claudeDir), maxManifestFileSize)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(b, &m); err == nil && m.Version > 0 {
		return m.Entries, nil
	}
	var legacy []Entry
	if err := json.Unmarshal(b, &legacy); err != nil {
		return nil, fmt.Errorf("corrupt manifest %s: %w", manifestPath(claudeDir), err)
	}
	return legacy, nil
}

func saveManifest(claudeDir string, entries []Entry) error {
	if err := safepath.ParentWithinForWrite(claudeDir, manifestPath(claudeDir)); err != nil {
		return err
	}
	m := Manifest{
		Version:       manifestVersion,
		QuarantinedAt: time.Now(),
		Entries:       entries,
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return safepath.AtomicWriteFileWithin(claudeDir, manifestPath(claudeDir), b, 0o600)
}

func nextID(entries []Entry) string {
	return fmt.Sprintf("%03d", len(entries)+1)
}

// QuarantineItem moves a skill directory or agent file into the
// quarantine area and records it in the manifest. It is the door for a run that
// picked its own items, so it takes only what is Removable.
func QuarantineItem(claudeDir string, it scan.Item) (Entry, error) {
	if !it.Removable {
		return Entry{}, fmt.Errorf("%s %q is not removable", it.Category, it.Name)
	}
	root := it.RootDir
	if root == "" {
		root = claudeDir
	}
	return quarantine(claudeDir, it, root)
}

// QuarantineChosen is QuarantineItem for one item the user named: Removable does
// not apply, and every other check does.
func QuarantineChosen(claudeDir string, it scan.Item) (Entry, error) {
	if it.FolderLink {
		return Entry{}, errFolderLink(it)
	}
	root := it.RootDir
	if root == "" {
		root = claudeDir
	}
	return quarantine(claudeDir, it, chosenRoot(root, itemPath(it)))
}

func errFolderLink(it scan.Item) error {
	return fmt.Errorf("%s is reached through a link that is its whole skills folder (%s); reap will not remove that link, it would take every skill there", it.Name, filepath.Dir(itemPath(it)))
}

// chosenRoot is the tree a named item is checked against: its platform's root, or
// the parent of the shared skills folder it lies in.
func chosenRoot(root, src string) string {
	for _, home := range safepath.LinkedSkillHomes(root) {
		if safepath.WithinDir(home, src) {
			return filepath.Dir(home)
		}
	}
	// A plugin's path is recorded resolved, so a root reached through a link has to
	// be too, or restore would find the item outside it.
	if !safepath.WithinDir(root, src) {
		if real, err := filepath.EvalSymlinks(root); err == nil {
			return real
		}
	}
	return root
}

// itemPath is what an item is on disk. A skill is its whole directory, not just
// SKILL.md.
func itemPath(it scan.Item) string {
	if filepath.Base(it.Path) == "SKILL.md" {
		return filepath.Dir(it.Path)
	}
	return it.Path
}

// quarantine does the move. The source is checked against root, the platform the
// item came from, not against the Claude directory: with several platforms scanned
// at once those are not the same tree. The quarantine and the manifest still live
// in claudeDir, which is checked separately below.
func quarantine(claudeDir string, it scan.Item, root string) (Entry, error) {
	src := itemPath(it)
	// Existence is probed before the containment check, not after. The item was
	// on disk when the report was built and can be gone now — overwhelmingly
	// because a second prune is running against the same directory, since
	// there is no lock and both runs compute the same candidate list. The
	// containment check resolves symlinks along the way, so on a vanished path
	// it fails first, with "lstat …: no such file or directory": a message
	// that describes the symptom and blames the wrong thing.
	//
	// This is a bare existence probe on a path already held, not a traversal
	// decision, so it does not weaken the ordering that matters: containment is
	// still proven before anything is written or moved.
	info, err := os.Lstat(src)
	if err != nil {
		if os.IsNotExist(err) {
			return Entry{}, fmt.Errorf("%w: %s", ErrAlreadyGone, src)
		}
		return Entry{}, err
	}
	// A linked skill is the link: that is what Claude Code loads, so that is
	// what moves. Its target is outside root by design and is never touched.
	isLink := it.LinkTarget != "" || (it.ViaLink != "" && info.Mode()&os.ModeSymlink != 0)
	if isLink {
		if err := safepath.LinkWithin(root, src); err != nil {
			return Entry{}, err
		}
	} else if _, err := safepath.ExistingPathWithin(root, src); err != nil {
		if os.IsNotExist(err) {
			return Entry{}, fmt.Errorf("%w: %s", ErrAlreadyGone, src)
		}
		return Entry{}, err
	}

	entries, err := LoadManifest(claudeDir)
	if err != nil {
		return Entry{}, err
	}

	dest := quarantinePath(claudeDir, it)
	if err := safepath.ParentWithinForWrite(claudeDir, dest); err != nil {
		return Entry{}, err
	}
	filesFrom, filesTo := "", ""
	if it.Written() {
		if filesFrom, filesTo, err = planWrittenFiles(claudeDir, root, it, dest); err != nil {
			return Entry{}, err
		}
	}

	// A record of a move that never happened (a run killed between its record
	// and its move) must not go on claiming this item is in quarantine.
	entries = settleUnmoved(entries, src)
	e := Entry{
		ID:        nextID(entries),
		Category:  string(it.Category),
		Name:      it.Name,
		Platform:  it.Platform,
		From:      src,
		Root:      root,
		To:        dest,
		Link:      isLink,
		FilesFrom: filesFrom,
		FilesTo:   filesTo,
		Timestamp: time.Now(),
	}
	// Recorded before the move: a run killed between the two leaves a record
	// restore can settle, the other way round it leaves a file nothing points to.
	if err := saveManifest(claudeDir, append(entries, e)); err != nil {
		return Entry{}, fmt.Errorf("save manifest: %w", err)
	}
	if err := moveRecorded(claudeDir, entries, src, dest, filesFrom, filesTo); err != nil {
		return Entry{}, err
	}
	return e, nil
}

// moveRecorded carries out a move already in the manifest. On failure it puts
// back what moved and takes the record out again.
func moveRecorded(claudeDir string, before []Entry, src, dest, filesFrom, filesTo string) error {
	err := rename(src, dest)
	if err == nil && filesTo != "" {
		if err = rename(filesFrom, filesTo); err != nil {
			if rbErr := rename(dest, src); rbErr != nil {
				// The record stays: it is the only pointer to what is in quarantine.
				return fmt.Errorf("%w (rollback also failed: %v; item left at %s)", err, rbErr, dest)
			}
		}
	}
	if err == nil {
		return nil
	}
	if os.IsNotExist(err) {
		// The rename is the atomic step, so this is where the race is finally
		// decided: another process completed the same move in the window between
		// the check and the rename.
		err = fmt.Errorf("%w: %s", ErrAlreadyGone, src)
	}
	if saveErr := saveManifest(claudeDir, before); saveErr != nil {
		return fmt.Errorf("%w (and its record could not be removed: %v)", err, saveErr)
	}
	return err
}

// osRename is a variable so a test can stand in for a second disk.
var osRename = os.Rename

// rename moves a file or folder, explaining the one failure a person cannot
// read from the system's wording.
func rename(from, to string) error {
	err := osRename(from, to)
	if errors.Is(err, syscall.EXDEV) {
		return fmt.Errorf("%s is on a different disk from %s, and reap only moves within one disk so that nothing can be lost halfway: %w", from, filepath.Dir(to), err)
	}
	return err
}

// quarantinePath picks a path nothing is at. Renaming onto a file replaces it,
// so a taken path has to be seen, and seen without following a moved link, whose
// target no longer resolves. The caller holds the prune lock, and the suffix is
// checked rather than derived from the clock, which repeats within a second.
func quarantinePath(claudeDir string, it scan.Item) string {
	base := filepath.Join(reapedDir(claudeDir), string(it.Category), safepath.Sanitize(it.Name))
	for n := 1; ; n++ {
		dest := base
		if n > 1 {
			dest = fmt.Sprintf("%s.%d", base, n)
		}
		if !lexists(dest) && !lexists(dest+".files") {
			return dest
		}
	}
}

func lexists(path string) bool {
	_, err := os.Lstat(path)
	return !os.IsNotExist(err)
}

// NeverMoved reports a record whose item is still where it was: the run stopped
// between writing the record and the move, or the item was already put back.
func NeverMoved(e Entry) bool {
	return !e.Restored && len(e.Payload) == 0 && e.To != "" && !lexists(e.To) && e.From != "" && lexists(e.From)
}

// settleUnmoved returns entries with any stale record of src closed.
func settleUnmoved(entries []Entry, src string) []Entry {
	out := slices.Clone(entries)
	for i, e := range entries {
		if e.From == src && NeverMoved(e) {
			out[i].Restored = true
		}
	}
	return out
}

// planWrittenFiles finds the folder of a skill reinvest wrote and where it goes
// in the quarantine, beside its link. The folder must sit directly in the written
// skill home of the item's root, so a tampered link cannot move anything else.
func planWrittenFiles(claudeDir, root string, it scan.Item, linkDest string) (string, string, error) {
	home, err := safepath.ExistingPathWithin(root, safepath.WrittenSkillHome(root))
	if err != nil {
		return "", "", err
	}
	from, err := safepath.ExistingPathWithin(home, it.LinkTarget)
	if err != nil || filepath.Dir(from) != home {
		return "", "", fmt.Errorf("refusing to remove %s: not a folder in %s", it.LinkTarget, home)
	}
	to := linkDest + ".files"
	if err := safepath.ParentWithinForWrite(claudeDir, to); err != nil {
		return "", "", err
	}
	// Recorded unresolved, like From: restore bounds it lexically to the home.
	return filepath.Join(safepath.WrittenSkillHome(root), filepath.Base(from)), to, nil
}

// DeleteItem removes a skill folder or agent file for good. It takes the same
// containment checks as QuarantineItem and leaves no record. A link is removed
// and what it points to stays: neither the check nor the removal follows it.
// Only ever one item the user named, so Removable does not apply.
func DeleteItem(it scan.Item) error {
	if it.FolderLink {
		return errFolderLink(it)
	}
	src := itemPath(it)
	if it.RootDir == "" {
		return fmt.Errorf("%s %q has no root to check it against", it.Category, it.Name)
	}
	root := chosenRoot(it.RootDir, src)
	info, err := os.Lstat(src)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("%w: %s", ErrAlreadyGone, src)
		}
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		if err := safepath.LinkWithin(root, src); err != nil {
			return err
		}
		return os.Remove(src)
	}
	resolved, err := safepath.ExistingPathWithin(root, src)
	if err != nil {
		return err
	}
	// One level under the root is skills/ or agents/ itself: a SKILL.md lying in
	// the wrong place must not take the whole folder with it.
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(realRoot, resolved)
	if err != nil || len(strings.Split(rel, string(filepath.Separator))) < 2 {
		return fmt.Errorf("refusing to delete %s: it is not an item inside a skills or agents folder", src)
	}
	return os.RemoveAll(src)
}

// DeleteMCP removes one server from a config file for good, changing nothing else
// in the file and keeping no copy.
func DeleteMCP(claudeDir, configPath, scope, name string) error {
	_, out, _, err := readConfigWithout(claudeDir, configPath, scope, name)
	if err != nil {
		return err
	}
	info, err := os.Stat(configPath)
	if err != nil {
		return err
	}
	return atomicfile.Write(configPath, out, info.Mode().Perm())
}

// serverPath is where a server sits in a config file. scope "" targets the
// top-level mcpServers; otherwise it names a project path under "projects".
func serverPath(scope, name string) []string {
	if scope == "" {
		return []string{"mcpServers", name}
	}
	return []string{"projects", scope, "mcpServers", name}
}

// CheckMCP says whether RemoveMCP could remove this server, without touching
// anything, so a plan can leave out what is bound to fail.
func CheckMCP(claudeDir, configPath, scope, name string) error {
	_, _, _, err := readConfigWithout(claudeDir, configPath, scope, name)
	return err
}

// readConfigWithout returns the config, the same file without the server, and
// the server's value. A server no longer there is ErrAlreadyGone.
func readConfigWithout(claudeDir, configPath, scope, name string) (raw, without, payload []byte, err error) {
	if _, err = safepath.ExistingRegularFileWithin(filepath.Dir(claudeDir), configPath); err != nil {
		if os.IsNotExist(err) {
			err = fmt.Errorf("%w: %s", ErrAlreadyGone, configPath)
		}
		return nil, nil, nil, err
	}
	raw, err = safepath.ReadRegularFileWithin(filepath.Dir(claudeDir), configPath, maxConfigFileSize)
	if err != nil {
		return nil, nil, nil, err
	}
	if !json.Valid(raw) {
		return nil, nil, nil, fmt.Errorf("%s is not plain JSON (comments, perhaps), so reap will not edit it; remove %s from it by hand", configPath, name)
	}
	without, payload, err = jsonedit.Delete(raw, serverPath(scope, name)...)
	if errors.Is(err, jsonedit.ErrNotFound) {
		return nil, nil, nil, fmt.Errorf("%w: mcp server %s in %s", ErrAlreadyGone, name, configPath)
	}
	if err != nil {
		return nil, nil, nil, err
	}
	return raw, without, payload, nil
}

func sha(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// RemoveMCP removes one MCP server from a Claude config file, changing nothing
// else in it. The whole file is backed up first and the removed payload is
// stored in the manifest for restore.
func RemoveMCP(claudeDir, configPath, scope, name string) (Entry, error) {
	raw, out, payload, err := readConfigWithout(claudeDir, configPath, scope, name)
	if err != nil {
		return Entry{}, err
	}
	info, err := os.Stat(configPath)
	if err != nil {
		return Entry{}, err
	}
	// Read the manifest before anything changes so a manifest-read failure
	// cannot leave the server removed with no record.
	entries, err := LoadManifest(claudeDir)
	if err != nil {
		return Entry{}, err
	}
	backup := filepath.Join(reapedDir(claudeDir), "backups",
		fmt.Sprintf("%s.%d", filepath.Base(configPath), time.Now().UnixNano()))
	if err := safepath.AtomicWriteFileWithin(claudeDir, backup, raw, 0o600); err != nil {
		return Entry{}, err
	}
	e := Entry{
		ID:         nextID(entries),
		Category:   string(scan.CatMCP),
		Name:       name,
		ConfigPath: configPath,
		JSONScope:  scope,
		Payload:    payload,
		Backup:     backup,
		AfterSHA:   sha(out),
		Timestamp:  time.Now(),
	}
	// Recorded first, as for files: a run killed here leaves a server still in
	// its config and a record that restore settles.
	if err := saveManifest(claudeDir, append(entries, e)); err != nil {
		_ = os.Remove(backup)
		return Entry{}, fmt.Errorf("save manifest: %w", err)
	}
	if err := atomicfile.Write(configPath, out, info.Mode().Perm()); err != nil {
		if saveErr := saveManifest(claudeDir, entries); saveErr != nil {
			return Entry{}, fmt.Errorf("%w (and its record could not be removed: %v)", err, saveErr)
		}
		_ = os.Remove(backup)
		return Entry{}, err
	}
	return e, nil
}

// Restore undoes one prune action by manifest ID.
func Restore(claudeDir, id string) error {
	entries, err := LoadManifest(claudeDir)
	if err != nil {
		return err
	}
	for i := range entries {
		if entries[i].ID != id {
			continue
		}
		if entries[i].Restored {
			return fmt.Errorf("entry %s already restored", id)
		}
		if NeverMoved(entries[i]) {
			entries[i].Restored = true
			return saveManifest(claudeDir, entries)
		}
		if err := restoreEntry(claudeDir, &entries[i]); err != nil {
			return err
		}
		return saveManifest(claudeDir, entries)
	}
	return fmt.Errorf("no manifest entry with id %s", id)
}

// Failure is one entry a bulk restore had to leave.
type Failure struct {
	Entry Entry
	Err   error
}

// RestoreError lists what a bulk restore could not do. The rest was done.
type RestoreError struct{ Failed []Failure }

func (e *RestoreError) Error() string {
	msgs := make([]string, len(e.Failed))
	for i, f := range e.Failed {
		msgs[i] = fmt.Sprintf("%s (%s): %v", f.Entry.Name, f.Entry.ID, f.Err)
	}
	return strings.Join(msgs, "; ")
}

// RestoreAll undoes every non-restored prune action it can, newest first: each
// removal from a config file knows the file as it was right before it, so going
// backwards gives the file back byte for byte. What cannot be restored stays
// listed and is returned as a *RestoreError, and does not hold up the others.
func RestoreAll(claudeDir string) (int, error) {
	entries, err := LoadManifest(claudeDir)
	if err != nil {
		return 0, err
	}
	n := 0
	var failed []Failure
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].Restored {
			continue
		}
		if NeverMoved(entries[i]) {
			entries[i].Restored = true
			continue
		}
		if err := restoreEntry(claudeDir, &entries[i]); err != nil {
			failed = append(failed, Failure{entries[i], err})
			continue
		}
		n++
	}
	if err := saveManifest(claudeDir, entries); err != nil {
		return n, err
	}
	if len(failed) > 0 {
		slices.Reverse(failed)
		return n, &RestoreError{failed}
	}
	return n, nil
}

func restoreEntry(claudeDir string, e *Entry) error {
	if len(e.Payload) > 0 {
		return restoreServer(claudeDir, e)
	}

	// File move. The quarantine side always stays inside the Claude directory,
	// which is where the reaped store lives. The destination is bounded by the
	// platform root recorded when the item was pruned, because an item from
	// another platform does not belong under the Claude directory and pinning
	// it there is what made multi-platform prune impossible. Entries written
	// before Root existed fall back to the old bound.
	if !lexists(e.To) {
		return fmt.Errorf("its quarantined copy is gone from %s, so it cannot come back", e.To)
	}
	if e.Link {
		if err := safepath.LinkWithin(claudeDir, e.To); err != nil {
			return err
		}
	} else if _, err := safepath.ExistingPathWithin(claudeDir, e.To); err != nil {
		return err
	}
	restoreRoot := e.Root
	if restoreRoot == "" {
		restoreRoot = claudeDir
	}
	if err := safepath.ParentWithinForWrite(restoreRoot, e.From); err != nil {
		return err
	}
	// A rename would silently replace a file here. What is there now is
	// the user's, so it is theirs to move.
	if lexists(e.From) {
		return fmt.Errorf("%s exists again; move it away, then run reap restore %s", e.From, e.ID)
	}
	// A run killed between the two moves can leave the folder where it was.
	if e.FilesTo != "" && lexists(e.FilesTo) {
		if _, err := safepath.ExistingPathWithin(claudeDir, e.FilesTo); err != nil {
			return err
		}
		if err := safepath.ParentWithinForWrite(safepath.WrittenSkillHome(restoreRoot), e.FilesFrom); err != nil {
			return err
		}
		if lexists(e.FilesFrom) {
			return fmt.Errorf("%s exists again; move it away, then run reap restore %s", e.FilesFrom, e.ID)
		}
		if err := rename(e.FilesTo, e.FilesFrom); err != nil {
			return err
		}
	}
	if err := rename(e.To, e.From); err != nil {
		return err
	}
	e.Restored = true
	return nil
}

// restoreServer puts a removed server back. A file unchanged since the removal
// gets its old bytes; a changed one gets the server added and keeps the changes.
// A server of the same name defined since then is the user's and is not replaced.
func restoreServer(claudeDir string, e *Entry) error {
	// Bound the config write to the install tree (covers ~/.claude.json and
	// plugin .mcp.json under the home dir) so a tampered manifest cannot
	// redirect the write to an arbitrary file.
	if _, err := safepath.ExistingPathWithin(filepath.Dir(claudeDir), e.ConfigPath); err != nil {
		return err
	}
	b, err := safepath.ReadRegularFileWithin(filepath.Dir(claudeDir), e.ConfigPath, maxConfigFileSize)
	if err != nil {
		return err
	}
	info, err := os.Stat(e.ConfigPath)
	if err != nil {
		return err
	}
	path := serverPath(e.JSONScope, e.Name)
	out, err := serverRestored(claudeDir, e, b, path)
	if err != nil {
		return err
	}
	if out != nil {
		if err := atomicfile.Write(e.ConfigPath, out, info.Mode().Perm()); err != nil {
			return err
		}
	}
	if e.Backup != "" && safepath.WithinDir(filepath.Join(reapedDir(claudeDir), "backups"), e.Backup) {
		_ = os.Remove(e.Backup)
	}
	e.Restored = true
	return nil
}

// serverRestored returns the config with the server back, or nil when it needs
// no write because that very server is already in it.
func serverRestored(claudeDir string, e *Entry, b []byte, path []string) ([]byte, error) {
	if e.AfterSHA == sha(b) && safepath.WithinDir(filepath.Join(reapedDir(claudeDir), "backups"), e.Backup) {
		if orig, err := safepath.ReadRegularFileWithin(claudeDir, e.Backup, maxConfigFileSize); err == nil {
			return orig, nil
		}
	}
	if !json.Valid(b) {
		return nil, fmt.Errorf("%s is not plain JSON any more, so reap will not edit it; server %s is kept in the manifest", e.ConfigPath, e.Name)
	}
	cur, err := jsonedit.Get(b, path...)
	if err == nil {
		if sameJSON(cur, e.Payload) {
			return nil, nil
		}
		return nil, fmt.Errorf("a different %s is defined in %s now; reap will not replace it. Remove it, then run reap restore %s", e.Name, e.ConfigPath, e.ID)
	}
	if !errors.Is(err, jsonedit.ErrNotFound) {
		return nil, err
	}
	return jsonedit.Set(b, e.Payload, path...)
}

func sameJSON(a, b []byte) bool {
	var x, y bytes.Buffer
	return json.Compact(&x, a) == nil && json.Compact(&y, b) == nil && bytes.Equal(x.Bytes(), y.Bytes())
}
