package problems

import (
	"errors"
	"io/fs"
	"strings"
	"syscall"
)

// hasAny reports whether s (already lower case) contains any of the words.
func hasAny(s string, words ...string) bool {
	for _, w := range words {
		if strings.Contains(s, w) {
			return true
		}
	}
	return false
}

// CodeFor picks the code that describes err, judging by what it wraps and by
// the words in its message. The steps of the download pipeline wrap their
// errors with a fixed phrase ("couldn't unpack the download", "repairing the
// download failed"), which is what most of these match. When nothing fits it
// returns fallback.
func CodeFor(err error, fallback string) string {
	if err == nil {
		return fallback
	}
	msg := strings.ToLower(err.Error())
	switch {
	case errors.Is(err, fs.ErrPermission), hasAny(msg, "permission denied", "access is denied", "operation not permitted", "read-only file system"):
		return CodeFolderPermission
	case errors.Is(err, syscall.ENOSPC), hasAny(msg, "no space left", "not enough free disk space", "not enough space", "disk full", "disk quota exceeded"):
		return CodeDiskFull
	case hasAny(msg, "couldn't get the nzb file", "couldn't get the torrent"):
		// The file comes from the indexer, not from the Usenet provider,
		// so a network failure here is the indexer's problem.
		return CodeIndexerFetchFailed
	case hasAny(msg, "too many connections", "connection limit", "max connections"):
		return CodeUsenetTooMany
	case hasAny(msg, "refused this username and password", "nntp 481", "nntp 482", "authentication failed", "nntp authenticate"):
		return CodeUsenetAuthRefused
	case hasAny(msg, "the download failed") && hasAny(msg, "dial tcp", "no such host", "connection refused", "i/o timeout"):
		return CodeUsenetUnreachable
	case hasAny(msg, "couldn't be found on your usenet servers"):
		return CodeUsenetMissingParts
	case hasAny(msg, "password protected", "needs a password"):
		return CodeUnpackPassword
	case hasAny(msg, "7z tool is not installed"):
		return CodeUnpackToolMissing
	case hasAny(msg, "repairing the download failed", "par2 repair", "par2 verify"):
		return CodePar2Failed
	case hasAny(msg, "couldn't unpack"):
		return CodeUnpackFailed
	case hasAny(msg, "couldn't find the movie file", "couldn't find the episode", "no video file", "couldn't find any music"):
		return CodeImportNoVideo
	case hasAny(msg, "a file already exists at"):
		return CodeImportFileExists
	case hasAny(msg, "couldn't move the file into your library"):
		return CodeImportMoveFailed
	case errors.Is(err, fs.ErrNotExist), hasAny(msg, "no such file or directory", "cannot find the path", "the system cannot find"):
		return CodeFolderMissing
	}
	return fallback
}

// IndexerCode says why an indexer failed: it limited the requests, it
// refused the key, it could not be reached, or it gave some other error.
func IndexerCode(err error) string {
	if err == nil {
		return CodeIndexerFailed
	}
	msg := strings.ToLower(err.Error())
	switch {
	case hasAny(msg, "429", "rate limit", "rate-limit", "too many requests", "limit reached", "limit exceeded", "api limit", "daily limit", "request limit"):
		return CodeIndexerRateLimited
	case hasAny(msg, "401", "403", "unauthorized", "forbidden", "incorrect user credentials", "invalid api key", "invalid apikey", "wrong api key", "api key", "apikey", "login failed", "invalid login"):
		return CodeIndexerAuthRefused
	case hasAny(msg, "timeout", "timed out", "deadline exceeded", "no such host", "connection refused", "connection reset", "unreachable", "network is down", "unexpected eof", ": eof", "dial ", "tls:", "certificate"):
		return CodeIndexerUnreachable
	}
	return CodeIndexerFailed
}

// MediaServerCode says why a media server could not be asked to refresh.
func MediaServerCode(err error) string {
	if err == nil {
		return CodeMediaServerRefresh
	}
	msg := strings.ToLower(err.Error())
	switch {
	case hasAny(msg, "401", "403", "unauthorized", "forbidden", "invalid token", "invalid api key"):
		return CodeMediaServerAuth
	case hasAny(msg, "could not reach", "couldn't reach", "timeout", "timed out", "deadline exceeded", "no such host", "connection refused", "connection reset", "unreachable", "network is down", "unexpected eof", ": eof", "dial ", "tls:", "certificate"):
		return CodeMediaServerDown
	}
	return CodeMediaServerRefresh
}

// UsenetCode says why a news server login could not be used: the provider
// refused it, the limit of connections was reached or the server could not be
// reached.
func UsenetCode(err error) string {
	if err == nil {
		return CodeUsenetUnreachable
	}
	msg := strings.ToLower(err.Error())
	switch {
	case hasAny(msg, "too many connections", "connection limit", "max connections"):
		return CodeUsenetTooMany
	case hasAny(msg, "quota", "bandwidth", "limit reached", "limit exceeded"):
		return CodeUsenetQuota
	case hasAny(msg, "no such host", "connection refused", "connection reset", "i/o timeout", "timed out", "timeout", "unexpected eof", ": eof", "dial tcp", "network is unreachable", "tls:", "certificate"):
		return CodeUsenetUnreachable
	case hasAny(msg, "refused this username", "nntp 481", "nntp 482", "nntp 502", "authenticat", "unauthorized", "access denied", "invalid login", "login failed"):
		return CodeUsenetAuthRefused
	}
	return CodeUsenetUnreachable
}
