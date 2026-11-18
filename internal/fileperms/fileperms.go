// Package fileperms holds the file and folder modes Mediarium gives to what it
// creates in the library (Settings > Library and files > Folders), and the
// small helpers that apply them.
//
// With no mode set (the default) nothing is changed: files and folders get
// whatever the process umask leaves, exactly as before. A mode that is set is
// applied with chmod after the file or folder is created, so it does not depend
// on the umask. Only what Mediarium creates is touched, never a folder that was
// already there.
package fileperms

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
)

// Modes are the permission bits for new files and new folders. Zero means
// "not set": leave it to the umask.
type Modes struct {
	File os.FileMode
	Dir  os.FileMode
}

var current atomic.Pointer[Modes]

// Set makes m the modes used from now on.
func Set(m Modes) { current.Store(&m) }

// Get returns the modes in use (both zero until Set is called).
func Get() Modes {
	if m := current.Load(); m != nil {
		return *m
	}
	return Modes{}
}

// ErrBadMode is wrapped by every error Parse and Check return, so a caller can
// tell "the person typed something wrong" from a real failure.
var ErrBadMode = errors.New("bad permission mode")

// Parse reads a mode written in octal, such as 644, 0644 or 755. Empty text
// means "not set" and gives 0. Anything that is not three octal digits (with an
// optional leading 0) is refused; the setuid, setgid and sticky bits are not
// supported on purpose.
func Parse(text string) (os.FileMode, error) {
	t := strings.TrimSpace(text)
	if t == "" {
		return 0, nil
	}
	t = strings.TrimPrefix(strings.TrimPrefix(t, "0o"), "0O")
	if len(t) == 4 && t[0] == '0' {
		t = t[1:]
	}
	if len(t) != 3 {
		return 0, fmt.Errorf("%w: use three digits, like 644 or 755", ErrBadMode)
	}
	for _, c := range t {
		if c < '0' || c > '7' {
			return 0, fmt.Errorf("%w: only the digits 0 to 7 are allowed, like 644 or 755", ErrBadMode)
		}
	}
	n, err := strconv.ParseUint(t, 8, 32)
	if err != nil {
		return 0, fmt.Errorf("%w: use three digits, like 644 or 755", ErrBadMode)
	}
	return os.FileMode(n), nil
}

// Format writes a mode as three octal digits ("" for not set).
func Format(m os.FileMode) string {
	if m == 0 {
		return ""
	}
	return fmt.Sprintf("%03o", uint32(m.Perm()))
}

// Check says whether a mode is usable. The owner (the user Mediarium runs as)
// must keep read and write on files, and read, write and enter on folders, or
// Mediarium could not read, replace or remove what it made. World-writable
// modes are allowed here but reported through the first result so the caller
// can ask the person to confirm.
func Check(m os.FileMode, dir bool) (worldWritable bool, err error) {
	if m == 0 {
		return false, nil
	}
	if m&^os.ModePerm != 0 {
		return false, fmt.Errorf("%w: use three digits, like 644 or 755", ErrBadMode)
	}
	if dir && m&0o700 != 0o700 {
		return false, fmt.Errorf("%w: the owner has to keep full access to folders (the first digit has to be 7), or Mediarium couldn't manage them", ErrBadMode)
	}
	if !dir && m&0o600 != 0o600 {
		return false, fmt.Errorf("%w: the owner has to keep read and write on files (the first digit has to be 6 or 7), or Mediarium couldn't manage them", ErrBadMode)
	}
	return m&0o002 != 0, nil
}

// MkdirAll creates path and any missing parents (like os.MkdirAll with 0755
// before the umask), then applies the folder mode to the folders it just
// created. A folder that already existed is never touched.
func MkdirAll(path string) error {
	var created []string
	p := filepath.Clean(path)
	for {
		if _, err := os.Stat(p); err == nil {
			break
		}
		created = append(created, p)
		up := filepath.Dir(p)
		if up == p {
			break
		}
		p = up
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		return err
	}
	if m := Get(); m.Dir != 0 {
		for _, c := range created {
			applyDirMode(c, m.Dir)
		}
	}
	return nil
}

// ApplyDir gives a folder that Mediarium just created the folder mode.
func ApplyDir(path string) {
	if m := Get(); m.Dir != 0 {
		applyDirMode(path, m.Dir)
	}
}

func applyDirMode(path string, mode os.FileMode) {
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode().Perm() == mode {
		return
	}
	if err := os.Chmod(path, mode); err != nil {
		slog.Warn("couldn't set the folder permissions", "path", path, "mode", Format(mode), "err", err)
	}
}

// ApplyFile gives a file that Mediarium just placed in the library the file
// mode. A link is skipped (chmod would change the file it points at), and a
// failure only logs a warning: the file is in place, and a mode that can't be
// set, for example on a share that doesn't support it, must not undo an import.
func ApplyFile(path string) error {
	m := Get()
	if m.File == 0 {
		return nil
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() == m.File {
		return nil
	}
	if err := os.Chmod(path, m.File); err != nil {
		slog.Warn("couldn't set the file permissions", "path", path, "mode", Format(m.File), "err", err)
		return err
	}
	return nil
}
