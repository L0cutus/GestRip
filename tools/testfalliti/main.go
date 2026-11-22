// Comando testfalliti: dall'uscita di `go test -json` (go-test.json della CI) stampa i test falliti, con pacchetto,
// nome e le loro righe di output, e i pacchetti che non si compilano. Serve quando il primo giro dei test della CI
// fallisce e il secondo passa: senza queste righe il test instabile non si riconosce (slice 114, approvata da Stefano il
// 21/11/2026). Legge solo il file; esce sempre con 0 se lo legge (l'esito lo decide il passo della CI).
package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "uso: testfalliti go-test.json")
		os.Exit(2)
	}
	f, err := os.Open(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, "testfalliti:", err)
		os.Exit(1)
	}
	defer func() { _ = f.Close() }()
	if err := stampaFalliti(f, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "testfalliti:", err)
		os.Exit(1)
	}
}
