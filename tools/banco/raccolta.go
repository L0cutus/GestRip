package main

import (
	"context"
	"fmt"
	"os"

	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
)

// La raccolta dei dettati accesa nelle due app del banco (slice 113; RIP-H-48), solo con -dati sintetici-import: lo
// scenario browser dettato-lotto (in fondo, prima di dettato-nuova) salva due buste dal dettato e guarda lo spegnimento
// all'ultima unità. Nel Go la pagina che accende la raccolta arriva con la 114, quindi la accende il banco, come per
// Telegram (seminaTelegramGo): le due chiavi dello standalone, scritte dopo la catena (l'import del Go non le porta da
// qui) e prima di avviare le app. Una sola busta rimasta nel lotto: la prima busta di dettato-lotto la consuma.
//
// Solo nel banco: in produzione la raccolta resta spenta finché il titolare non la accende (D-07, D-105).

// rimasteBanco è il lotto con cui partono le due app.
const rimasteBanco = "1"

// seminaRaccoltaStandalone accende la raccolta nello standalone (Settings.put/2, come i test di training_test.exs).
const seminaRaccoltaStandalone = `Application.load(:gestrip_standalone)
endpoint = Application.fetch_env!(:gestrip_standalone, GestripStandaloneWeb.Endpoint)
Application.put_env(:gestrip_standalone, GestripStandaloneWeb.Endpoint, Keyword.put(endpoint, :server, false))
Application.put_env(:gestrip_standalone, :start_chromic_pdf, false)
{:ok, _} = Application.ensure_all_started(:gestrip_standalone)
{:ok, _} = GestripStandalone.Settings.put("assist.training.collect", "1")
{:ok, _} = GestripStandalone.Settings.put("assist.training.remaining", "` + rimasteBanco + `")
IO.puts("Raccolta dei dettati accesa")`

// seminaRaccoltaGo accende la raccolta nel database del Go in percorso (già importato).
func seminaRaccoltaGo(percorso string) error {
	if _, err := os.Lstat(percorso); err != nil {
		return fmt.Errorf("database del Go: %w", err)
	}
	d, err := db.Apri(percorso)
	if err != nil {
		return err
	}
	defer func() { _ = d.Chiudi() }()
	ora := clock.Sistema{}.Adesso().UTC().Format(db.FormatoIstante)
	_, err = d.Scrittura.ExecContext(context.Background(), `INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES (?, '1', ?), (?, ?, ?)
		ON CONFLICT (chiave) DO UPDATE SET valore = excluded.valore, aggiornata_il = excluded.aggiornata_il`,
		buste.ChiaveRaccoltaAccesa, ora, buste.ChiaveRaccoltaRimaste, rimasteBanco, ora)
	return err
}
