package api

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/rdborg/mediarium/internal/fileperms"
	"github.com/rdborg/mediarium/internal/settings"
)

// fileRoutes registers the routes of the library file features: permissions,
// moving titles between library folders, collections, and NFO and artwork
// files. They live in this file's neighbours; the access level of each is
// decided here.
func (s *Server) fileRoutes(member, manage, admin routeGroup) {
	// File and folder permissions (Settings > Library and files > Folders).
	admin.HandleFunc("GET /api/settings/file-permissions", s.handleGetFilePermissions)
	admin.HandleFunc("PUT /api/settings/file-permissions", s.handlePutFilePermissions)

	// Moving titles to another library folder, with or without their files.
	admin.HandleFunc("POST /api/library/bulk/folder", s.handleBulkFolder)
	admin.HandleFunc("POST /api/library/move-files/preview", s.handleMoveFilesPreview)
	admin.HandleFunc("POST /api/library/move-files", s.handleMoveFiles)
	admin.HandleFunc("GET /api/library/move-files/status", s.handleMoveFilesStatus)
	admin.HandleFunc("POST /api/library/move-files/cancel", s.handleCancelMoveFiles)

	// Movie collections: browse them as a member, add and follow them as an administrator.
	member.HandleFunc("GET /api/collections", s.handleListCollections)
	member.HandleFunc("GET /api/collections/{id}", s.handleGetCollection)
	admin.HandleFunc("POST /api/collections/{id}/add", s.handleAddCollection)
	admin.HandleFunc("PUT /api/collections/{id}/follow", s.handleFollowCollection)

	// NFO and artwork files for media servers.
	admin.HandleFunc("GET /api/settings/metadata-files", s.handleGetMetadataFiles)
	admin.HandleFunc("PUT /api/settings/metadata-files", s.handlePutMetadataFiles)
	admin.HandleFunc("POST /api/library/metadata-files/run", s.handleRunMetadataFiles)
	admin.HandleFunc("GET /api/library/metadata-files/status", s.handleMetadataFilesStatus)
	admin.HandleFunc("POST /api/library/metadata-files/cancel", s.handleCancelMetadataFiles)
}

// applyFilePerms loads the saved file and folder modes into the package that
// applies them. A saved value that doesn't pass the checks any more is ignored.
func (s *Server) applyFilePerms() {
	var m fileperms.Modes
	if v, _ := s.Settings.Get(settings.KeyFileMode); v != "" {
		if mode, err := fileperms.Parse(v); err == nil {
			if _, err := fileperms.Check(mode, false); err == nil {
				m.File = mode
			}
		}
	}
	if v, _ := s.Settings.Get(settings.KeyFolderMode); v != "" {
		if mode, err := fileperms.Parse(v); err == nil {
			if _, err := fileperms.Check(mode, true); err == nil {
				m.Dir = mode
			}
		}
	}
	fileperms.Set(m)
}

type filePermissionsPayload struct {
	// FileMode and FolderMode are three octal digits, "" while not set (the
	// container's umask decides then).
	FileMode   string `json:"fileMode"`
	FolderMode string `json:"folderMode"`
	// Umask is the UMASK the container was started with ("" when not set).
	Umask string `json:"umask"`
}

type filePermissionsRequest struct {
	FileMode   *string `json:"fileMode,omitempty"`
	FolderMode *string `json:"folderMode,omitempty"`
	// ConfirmWorldWritable has to be true to save a mode that lets every user
	// on the machine change the files.
	ConfirmWorldWritable bool `json:"confirmWorldWritable,omitempty"`
}

func (s *Server) filePermissionsPayload() filePermissionsPayload {
	f, _ := s.Settings.Get(settings.KeyFileMode)
	d, _ := s.Settings.Get(settings.KeyFolderMode)
	return filePermissionsPayload{FileMode: f, FolderMode: d, Umask: strings.TrimSpace(os.Getenv("UMASK"))}
}

// handleGetFilePermissions is GET /api/settings/file-permissions.
func (s *Server) handleGetFilePermissions(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.filePermissionsPayload())
}

// worldWritableError and badModeError are whole sentences for the file mode
// and the folder mode, so that each is one string to translate.
func worldWritableError(dir bool, mode string) string {
	if dir {
		return fmt.Sprintf("The folder mode %s is world-writable. That lets every user on this machine change or delete the files (the last digit is 2, 3, 6 or 7). Tick \"I understand\" to save it anyway. 664 and 775 are the usual choice when a group needs to write.", mode)
	}
	return fmt.Sprintf("The file mode %s is world-writable. That lets every user on this machine change or delete the files (the last digit is 2, 3, 6 or 7). Tick \"I understand\" to save it anyway. 664 and 775 are the usual choice when a group needs to write.", mode)
}

func badModeError(dir bool, detail string) string {
	if dir {
		return fmt.Sprintf("The folder mode isn't valid: %s.", detail)
	}
	return fmt.Sprintf("The file mode isn't valid: %s.", detail)
}

// handlePutFilePermissions is PUT /api/settings/file-permissions. Both modes
// are checked before either is saved. An empty value goes back to the default.
func (s *Server) handlePutFilePermissions(w http.ResponseWriter, r *http.Request) {
	var req filePermissionsRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that request. Reload the page and try again.")
		return
	}
	type change struct {
		key string
		dir bool
		in  *string
		out string
	}
	changes := []*change{
		{key: settings.KeyFileMode, in: req.FileMode},
		{key: settings.KeyFolderMode, dir: true, in: req.FolderMode},
	}
	for _, c := range changes {
		if c.in == nil {
			continue
		}
		mode, err := fileperms.Parse(*c.in)
		if err == nil {
			var world bool
			world, err = fileperms.Check(mode, c.dir)
			if err == nil && world && !req.ConfirmWorldWritable {
				writeError(w, http.StatusBadRequest, worldWritableError(c.dir, fileperms.Format(mode)))
				return
			}
		}
		if err != nil {
			if errors.Is(err, fileperms.ErrBadMode) {
				writeError(w, http.StatusBadRequest, badModeError(c.dir, strings.TrimPrefix(err.Error(), fileperms.ErrBadMode.Error()+": ")))
				return
			}
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		c.out = fileperms.Format(mode)
	}
	for _, c := range changes {
		if c.in == nil {
			continue
		}
		if err := s.Settings.Set(c.key, c.out, false); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	s.applyFilePerms()
	writeJSON(w, http.StatusOK, s.filePermissionsPayload())
}
