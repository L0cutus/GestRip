//go:build !linux

package media

import "context"

// lucchettoFile fuori da Linux (solo sviluppo) non c'è: vale solo il RWMutex del processo. Il backup
// fuori da Linux rifiuta comunque (spazio libero non leggibile), quindi non c'è un secondo processo
// da escludere.
func lucchettoFile(context.Context, string, bool) (func(), error) { return func() {}, nil }
