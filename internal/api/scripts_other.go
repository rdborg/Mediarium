//go:build !unix

package api

import "os/exec"

func ownProcessGroup(*exec.Cmd)  {}
func killProcessGroup(*exec.Cmd) {}
