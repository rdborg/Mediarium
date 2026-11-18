package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/rdborg/mediarium/internal/quality"
)

// Custom formats through the Radarr and Sonarr style API, as Recyclarr writes
// them: GET, POST, PUT and DELETE on /customformat, and the list of condition
// kinds on /customformat/schema.
//
// The conditions are the same ones Mediarium's own custom formats use
// (quality/formats.go), so the TRaSH Guides formats Recyclarr sends are
// stored as they are. A format belongs to the app that wrote it, since the
// numbers of sources and languages are not the same in both: /radarr shows and
// changes the Radarr ones and /sonarr the Sonarr ones, so a name TRaSH uses in
// both guides (HDR, x265 (HD)) is two formats, one for each.

// arrSpecName is the name of a condition kind as the apps list it.
var arrSpecNames = map[string]string{
	quality.SpecReleaseTitle:    "Release Title",
	quality.SpecReleaseGroup:    "Release Group",
	quality.SpecEdition:         "Edition",
	quality.SpecSource:          "Source",
	quality.SpecResolution:      "Resolution",
	quality.SpecQualityModifier: "Quality Modifier",
	quality.SpecLanguage:        "Language",
	quality.SpecReleaseType:     "Release Type",
	quality.SpecSize:            "Size",
	quality.SpecIndexerFlag:     "Indexer Flag",
}

// arrSpecKinds is the kinds in the order the apps list them.
var arrSpecKinds = []string{
	quality.SpecReleaseTitle, quality.SpecLanguage, quality.SpecIndexerFlag, quality.SpecSource, quality.SpecResolution,
	quality.SpecQualityModifier, quality.SpecSize, quality.SpecReleaseGroup, quality.SpecEdition, quality.SpecReleaseType,
}

type arrSpecOut struct {
	ID                 int              `json:"id"`
	Name               string           `json:"name"`
	Implementation     string           `json:"implementation"`
	ImplementationName string           `json:"implementationName"`
	InfoLink           string           `json:"infoLink"`
	Negate             bool             `json:"negate"`
	Required           bool             `json:"required"`
	Fields             []map[string]any `json:"fields"`
}

// arrCustomFormatFull is a custom format with its conditions, the shape of
// /customformat.
type arrCustomFormatFull struct {
	ID                              int64        `json:"id"`
	Name                            string       `json:"name"`
	IncludeCustomFormatWhenRenaming bool         `json:"includeCustomFormatWhenRenaming"`
	Specifications                  []arrSpecOut `json:"specifications"`
}

// arrNumberValue is a condition's value as the number it is: the apps send and
// compare these as numbers, not text. A value that is not a number stays text.
func arrNumberValue(v string) any {
	v = strings.TrimSpace(v)
	if n, err := strconv.ParseInt(v, 10, 64); err == nil {
		return n
	}
	if f, err := strconv.ParseFloat(v, 64); err == nil {
		return f
	}
	return v
}

// arrSpecFields are the settings of one condition, by name.
func arrSpecFields(app arrApp, kind, value string, min, max float64, except bool) []map[string]any {
	field := func(order int, name string, v any) map[string]any {
		return map[string]any{"order": order, "name": name, "value": v, "advanced": false}
	}
	switch kind {
	case quality.SpecReleaseTitle, quality.SpecReleaseGroup, quality.SpecEdition:
		return []map[string]any{field(0, "value", value)}
	case quality.SpecLanguage:
		return []map[string]any{field(0, "value", arrNumberValue(value)), field(1, "exceptLanguage", except)}
	case quality.SpecSize:
		return []map[string]any{field(0, "min", min), field(1, "max", max)}
	default:
		return []map[string]any{field(0, "value", arrNumberValue(value))}
	}
}

func arrInfoLink(app arrApp) string {
	return "https://wiki.servarr.com/" + string(app) + "/settings#custom-formats-2"
}

func toArrCustomFormat(app arrApp, f quality.CustomFormat) arrCustomFormatFull {
	out := arrCustomFormatFull{ID: f.ID, Name: f.Name, IncludeCustomFormatWhenRenaming: f.IncludeInName, Specifications: []arrSpecOut{}}
	for _, s := range f.Specs {
		name := arrSpecNames[s.Kind]
		if name == "" {
			name = s.Kind
		}
		out.Specifications = append(out.Specifications, arrSpecOut{
			Name: s.Name, Implementation: s.Kind, ImplementationName: name, InfoLink: arrInfoLink(app),
			Negate: s.Negate, Required: s.Required, Fields: arrSpecFields(app, s.Kind, s.Value, s.Min, s.Max, s.Except),
		})
	}
	return out
}

// arrCustomFormatsFor answers GET /customformat: every format with its
// conditions.
func (s *Server) arrCustomFormatsFor(app arrApp) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		formats, err := s.FormatRepo.List()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		out := make([]arrCustomFormatFull, 0, len(formats))
		for _, f := range formats {
			if arrFormatIsOf(app, f) {
				out = append(out, toArrCustomFormat(app, f))
			}
		}
		writeJSON(w, http.StatusOK, out)
	}
}

// arrFormatIsOf reports whether a format belongs to the app's side.
func arrFormatIsOf(app arrApp, f quality.CustomFormat) bool {
	return string(f.App) == string(app)
}

// arrOwnFormat reads the format an id in the address names, answering 404
// itself when there is none on this app's side.
func (s *Server) arrOwnFormat(w http.ResponseWriter, r *http.Request, app arrApp) (quality.CustomFormat, bool) {
	id, ok := arrPathID(w, r)
	if !ok {
		return quality.CustomFormat{}, false
	}
	f, err := s.FormatRepo.Get(id)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && !arrFormatIsOf(app, f)) {
		arrNotFound(w)
		return quality.CustomFormat{}, false
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return quality.CustomFormat{}, false
	}
	return f, true
}

// arrGetCustomFormat answers GET /customformat/{id}.
func (s *Server) arrGetCustomFormat(app arrApp) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if f, ok := s.arrOwnFormat(w, r, app); ok {
			writeJSON(w, http.StatusOK, toArrCustomFormat(app, f))
		}
	}
}

// arrCustomFormatSchema answers GET /customformat/schema: the kinds of
// condition and the settings each one has.
func arrCustomFormatSchema(app arrApp) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		out := []arrSpecOut{}
		for _, kind := range arrSpecKinds {
			out = append(out, arrSpecOut{
				Name: "", Implementation: kind, ImplementationName: arrSpecNames[kind], InfoLink: arrInfoLink(app),
				Fields: arrSpecFields(app, kind, "", 0, 0, false),
			})
		}
		writeJSON(w, http.StatusOK, out)
	}
}

// arrFormatProblems turns a refusal to read a format into what the apps report.
func arrFormatProblems(err error) []arrProblem {
	text := sentence(err.Error())
	low := strings.ToLower(text)
	prop := ""
	switch {
	case strings.Contains(low, "has no name"), strings.Contains(low, "name") && strings.Contains(low, "too long"):
		prop = "Name"
	case strings.Contains(low, "no conditions"), strings.Contains(low, "too many conditions"), strings.Contains(low, "condition"):
		prop = "Specifications"
	}
	return []arrProblem{{prop, text}}
}

// arrReadFormat reads the one custom format of a request.
func arrReadFormat(w http.ResponseWriter, r *http.Request, app arrApp) (quality.CustomFormat, bool) {
	var raw json.RawMessage
	if err := decodeJSON(r, &raw); err != nil {
		arrBadRequestList(w, []arrProblem{{"", arrUnreadable}})
		return quality.CustomFormat{}, false
	}
	if t := strings.TrimSpace(string(raw)); !strings.HasPrefix(t, "{") {
		arrBadRequestList(w, []arrProblem{{"", "Send one custom format as an object."}})
		return quality.CustomFormat{}, false
	}
	parsed, err := quality.ParseFormats(raw, quality.FormatApp(app))
	if err != nil {
		arrBadRequestList(w, arrFormatProblems(err))
		return quality.CustomFormat{}, false
	}
	if len(parsed) != 1 {
		arrBadRequestList(w, []arrProblem{{"", "Send one custom format as an object."}})
		return quality.CustomFormat{}, false
	}
	return parsed[0].Format, true
}

func arrFormatSaveError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, quality.ErrFormatName):
		arrBadRequestList(w, []arrProblem{{"Name", "A custom format with that name already exists."}})
	case errors.Is(err, quality.ErrInvalid):
		arrBadRequestList(w, []arrProblem{{"Name", problemText(err, quality.ErrInvalid)}})
	case errors.Is(err, sql.ErrNoRows):
		arrNotFound(w)
	default:
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}

// arrCreateCustomFormat answers POST /customformat.
func (s *Server) arrCreateCustomFormat(app arrApp) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		f, ok := arrReadFormat(w, r, app)
		if !ok {
			return
		}
		f.TrashID, f.TrashScore = "", nil // made by an app: the TRaSH update of Mediarium's own never touches it
		saved, err := s.FormatRepo.Create(f)
		if err != nil {
			arrFormatSaveError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, toArrCustomFormat(app, saved))
	}
}

// arrUpdateCustomFormat answers PUT /customformat/{id}.
func (s *Server) arrUpdateCustomFormat(app arrApp) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		own, ok := s.arrOwnFormat(w, r, app)
		if !ok {
			return
		}
		f, ok := arrReadFormat(w, r, app)
		if !ok {
			return
		}
		saved, err := s.FormatRepo.Replace(own.ID, f)
		if err != nil {
			arrFormatSaveError(w, err)
			return
		}
		writeJSON(w, http.StatusAccepted, toArrCustomFormat(app, saved))
	}
}

// arrDeleteCustomFormat answers DELETE /customformat/{id}. The format also
// leaves every profile that scored it.
func (s *Server) arrDeleteCustomFormat(app arrApp) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		own, ok := s.arrOwnFormat(w, r, app)
		if !ok {
			return
		}
		s.arrDeleteFormat(w, own.ID)
	}
}

func (s *Server) arrDeleteFormat(w http.ResponseWriter, id int64) {
	if err := s.FormatRepo.Delete(id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			arrNotFound(w)
			return
		}
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("delete custom format: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{})
}
