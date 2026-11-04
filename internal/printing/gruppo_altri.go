//go:build !unix

package printing

import "os/exec"

// inGruppo fuori da unix non fa niente: CUPS e l'apparecchio sono Linux.
func inGruppo(*exec.Cmd) {}
