package main

import (
	"os"
	"path/filepath"
)

// La stampante finta del banco (slice 046): il Go del banco riceve -comandi-stampa /stampa-finta,
// una cartella in sola lettura con un lp, un lpstat e un lpoptions finti (sh). Il container del Go non
// ha CUPS né una rete verso fuori: nessun lavoro può arrivare a una stampante vera. Lo standalone non
// ha la stampa diretta (D-08, D-09): i passi che stampano sono solo_go.
//
// Le code finte:
//   - Etichette_QR: accetta, con le opzioni MediaTracking e Darkness;
//   - Buste-76x127: accetta, con il solo PageSize;
//   - Rifiutata: lpstat la elenca ma lp la rifiuta con il messaggio di CUPS («lp: The printer or class
//     does not exist.»), come una coda tolta fra l'elenco e la stampa.
//
// L'id del lavoro è «<coda>-<n>», con n contato in /tmp del container (tmpfs): il primo lavoro di
// un'istanza nuova è il 1.

// cartellaStampaFinta è dove il container del Go vede i comandi finti.
const cartellaStampaFinta = "/stampa-finta"

var comandiStampaFinta = map[string]string{
	"lpstat": `#!/bin/sh
# lpstat finto del banco di GestRip (slice 046): nessuna coda vera.
[ "$#" -eq 1 ] && [ "$1" = "-e" ] || { echo "lpstat: finto: solo -e" >&2; exit 1; }
printf 'Etichette_QR\nBuste-76x127\nRifiutata\n'
`,
	"lpoptions": `#!/bin/sh
# lpoptions finto del banco di GestRip (slice 046).
[ "$#" -eq 3 ] && [ "$1" = "-p" ] && [ "$3" = "-l" ] || { echo "lpoptions: finto: solo -p <coda> -l" >&2; exit 1; }
case "$2" in
Etichette_QR) printf 'MediaTracking/Media Tracking: Continuous *Gap Mark\nDarkness/Darkness: 1 2 3 4 5 6 7 *8 9 10 11 12 13 14 15\n' ;;
*) printf 'PageSize/Media Size: *w216h360 Custom.WIDTHxHEIGHT\n' ;;
esac
`,
	"lp": `#!/bin/sh
# lp finto del banco di GestRip (slice 046): legge il PDF e lo butta, risponde come lp.c di CUPS.
coda=""
while [ "$#" -gt 0 ]; do
  case "$1" in
  -d) coda="$2"; shift 2 ;;
  -t|-o) shift 2 ;;
  *) echo "lp: finto: argomento inatteso" >&2; exit 1 ;;
  esac
done
cat > /dev/null
case "$coda" in
Etichette_QR|Buste-76x127) ;;
*) echo "lp: The printer or class does not exist." >&2; exit 1 ;;
esac
n=$(( $(cat /tmp/lp-finto.n 2>/dev/null || echo 0) + 1 ))
echo "$n" > /tmp/lp-finto.n
echo "request id is $coda-$n (0 file(s))"
`,
}

// scriviStampaFinta scrive i comandi finti nella cartella dir (nuova), eseguibili e in sola lettura.
func scriviStampaFinta(dir string) error {
	if err := os.Mkdir(dir, 0o700); err != nil {
		return err
	}
	for nome, testo := range comandiStampaFinta {
		if err := os.WriteFile(filepath.Join(dir, nome), []byte(testo), 0o500); err != nil {
			return err
		}
	}
	return nil
}
