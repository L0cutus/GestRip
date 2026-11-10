package main

// I link storici della vecchia app Flask (slice 064): il banco dà alle due app lo stesso segreto
// Flask **sintetico**, inventato e chiaramente finto (mai quello vero del negozio), così gli scenari
// pagina-storica possono aprire /stato/<busta>/<anno>?t=&exp= e le foto /foto e /thumb firmate:
//   - allo standalone con LEGACY_WARRANTY_SECRET_FILE (config/runtime.exs), il file
//     secrets/legacy_warranty_secret dell'istanza montato in /run/secrets;
//   - al Go in secrets/segreto_flask accanto al database (go-data/secrets/segreto_flask), dove lo
//     legge gestrip serve.
// Le firme degli scenari (bench/scenari/pagina-storica*.json) sono calcolate fuori dal banco con
// questo segreto e scadenze fisse (2100), e TestFirmeScenariStorici le ricalcola.

import (
	"os"
	"path/filepath"
)

// segretoFlaskBanco è il segreto Flask sintetico del banco.
const segretoFlaskBanco = "segreto-flask-SINTETICO-del-banco-non-vero"

// nomeSegretoFlaskStandalone è il file del segreto nei segreti dell'istanza (montati in /run/secrets).
const nomeSegretoFlaskStandalone = "legacy_warranty_secret"

// percorsiPubblici sono i percorsi che il Caddy del banco manda alla faccia pubblica del Go.
var percorsiPubblici = []string{"/r/*", "/stato/*", "/foto/*", "/thumb/*"}

// scriviSegretoFlaskGo scrive il segreto sintetico in <dati>/secrets/segreto_flask (cartella 0700,
// file 0600), dove gestrip serve lo cerca per il database <dati>/gestrip.db.
func scriviSegretoFlaskGo(dati string) error {
	dir := filepath.Join(dati, "secrets")
	if err := os.Mkdir(dir, 0o700); err != nil {
		return err
	}
	return scriviPrivato(filepath.Join(dir, "segreto_flask"), segretoFlaskBanco+"\n")
}
