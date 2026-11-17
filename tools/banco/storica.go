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

// segretoStandaloneBanco è il secret_key_base sintetico dello standalone del banco (slice 084, D-87):
// fisso, così il banco firma i Phoenix.Token degli artigiani ({phoenix_artigiano:…}, esegui.go) e il Go
// li verifica con lo stesso valore, in secrets/segreto_standalone. Almeno 64 byte, come vuole
// runtime.exs.
const segretoStandaloneBanco = "secret-key-base-SINTETICO-del-banco-non-vero-mai-quello-del-negozio-0123456789"

// nomeSegretoFlaskStandalone è il file del segreto nei segreti dell'istanza (montati in /run/secrets).
const nomeSegretoFlaskStandalone = "legacy_warranty_secret"

// percorsiPubblici sono i percorsi che il Caddy del banco manda alla faccia pubblica del Go (dalla
// slice 065 anche il portale ritirato /m/ e l'informativa /privacy, dalla 084 le pagine dell'artigiano, dalla
// 092 la pagina pubblica del lotto e le sue foto).
var percorsiPubblici = []string{"/r/*", "/stato/*", "/foto/*", "/thumb/*", "/m/*", "/privacy", "/artigiano/*", "/lotto/*"}

// scriviSegretoFlaskGo scrive il segreto sintetico in <dati>/secrets/segreto_flask (cartella 0700,
// file 0600), dove gestrip serve lo cerca per il database <dati>/gestrip.db; dalla 084 anche il
// secret_key_base dello standalone in <dati>/secrets/segreto_standalone.
func scriviSegretoFlaskGo(dati string) error {
	dir := filepath.Join(dati, "secrets")
	if err := os.Mkdir(dir, 0o700); err != nil {
		return err
	}
	if err := scriviPrivato(filepath.Join(dir, "segreto_flask"), segretoFlaskBanco+"\n"); err != nil {
		return err
	}
	return scriviPrivato(filepath.Join(dir, "segreto_standalone"), segretoStandaloneBanco+"\n")
}
