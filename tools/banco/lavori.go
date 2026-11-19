package main

// Il passo dei lavori spenti (slice 100; scopo e limiti rispetto alla nota 8 di docs/domande-aperte.md più sotto): per ultimo, nello standalone ancora
// fermo e dopo i token, i tre lavori automatici (ritardi, solleciti, briefing) si spengono **nelle impostazioni**
// dello standalone, con le funzioni sue (Settings.put/3, la stessa chiave e lo stesso valore che scrive la pagina
// «Promemoria e job»): jobs.overdue_enabled, jobs.chases_enabled e jobs.briefing_enabled a «0».
//
// Perché: lo scheduler dello standalone (GestripStandalone.Jobs.Scheduler) parte con l'applicazione e ogni minuto
// chiede a Jobs.due?/2 se un lavoro è dovuto; nel banco, senza canali, ogni lavoro acceso che ha raggiunto la sua ora
// scrive una riga «Saltato» e una riga di audit (Jobs.run/3, record/5, fino a tre tentativi al giorno, solo dopo l'ora
// del lavoro). Quelle righe dipendono dall'ora del giro (nota 4 della 094): spente, il confronto è più stabile. **Non è il
// rimedio della nota 8**: quattro comparse su nove cadono prima delle 09:00, quando lo scheduler non scrive; la causa della
// nota 8 resta aperta (sospetto solo ragionato: `default_transaction_mode: :immediate` in config/runtime.exs dello
// standalone, per cui anche una transazione di sola lettura, come AuditLive.handle_params, prende il blocco di scrittura).
// Fermare l'intero scheduler si può anche con un'altra via, misurata sulla fab-225: montare in sola lettura una copia di
// releases/0.1.0/sys.config con tick_interval_ms a nil e RELEASE_SYS_CONFIG=/run/syscfg/sys. Si è scelto lo spegnimento
// nei dati perché non tocca file generati della release ed è lo stesso gesto della casella «Attivo». Spento il lavoro,
// Jobs.due?/2 dice no alla prima domanda (Config.enabled?/2) e run_due/1 non scrive niente: restano le sole letture del
// tick, ogni minuto.
//
// Il Go dell'istanza e quello con Telegram nascono da questo database (l'import porta le jobs.*_enabled): le tre caselle
// «Attivo» della pagina sono spente in tutte e due le app e il confronto non ha differenze. Il Go con Telegram invece
// riaccende ritardi e solleciti nella sua copia (seminaTelegramGo): lo scenario telegram-promemoria li esegue a mano
// e vuole «Job eseguito.».
//
// Lo script controlla che dopo la scrittura Config.get/0 dica «spento» per i tre lavori e che Jobs.due?/2 sia
// falso per ciascuno, con l'ora di adesso. L'esito è una sola riga GR_LAVORI:.

import (
	"errors"
	"fmt"
	"strings"
)

const passoLavori = "lavori"

// chiaviLavoriBanco sono le impostazioni dello standalone che il passo scrive a «0».
var chiaviLavoriBanco = []string{"jobs.overdue_enabled", "jobs.chases_enabled", "jobs.briefing_enabled"}

// scriptLavori è lo script del passo.
func scriptLavori() string {
	var chiavi []string
	for _, c := range chiaviLavoriBanco {
		chiavi = append(chiavi, fmt.Sprintf("%q", c))
	}
	return `GestripStandalone.LegacyImport.Release.quiet!()
{:ok, _} = Application.ensure_all_started(:gestrip_standalone)
alias GestripStandalone.{Jobs, Repo, Settings}
alias GestripStandalone.Jobs.Config
spegni = fn chiave ->
  case Settings.put(Repo, chiave, "0") do
    {:ok, _} -> :ok
    errore -> {:errore, chiave, inspect(errore)}
  end
end
scritture = Enum.reject(Enum.map([` + strings.Join(chiavi, ", ") + `], spegni), &(&1 == :ok))
config = Config.get()
adesso = DateTime.utc_now()
accesi = Enum.filter(Jobs.jobs(), &Config.enabled?(config, &1))
dovuti = Enum.filter(Jobs.jobs(), &Jobs.due?(&1, adesso))
case {scritture, accesi, dovuti} do
  {[], [], []} -> IO.puts("GR_LAVORI:ok")
  altro -> IO.puts("GR_LAVORI:errore " <> inspect(altro)); System.halt(1)
end`
}

// comandoLavori gira come gli altri passi: stessa immagine, senza rete, stesse protezioni e stessi dati
// dell'istanza, e nient'altro.
func comandoLavori(n nomi, img string, env []string) []string {
	a := append(passoSenzaRete(n, passoLavori), protezioniStandalone(n)...)
	a = append(a, montaggiStandalone(n)...)
	a = append(a, env...)
	return append(a, img, "bin/gestrip_standalone", "eval", scriptLavori())
}

// leggiEsitoLavori vuole una sola riga GR_LAVORI:, riuscita.
func leggiEsitoLavori(out string) error {
	righe := righeMarcatore(out, "GR_LAVORI:")
	switch {
	case len(righe) != 1:
		return fmt.Errorf("lavori nello standalone: %d righe GR_LAVORI, ne serve una", len(righe))
	case righe[0] != "ok":
		return fmt.Errorf("lavori nello standalone: %s", righe[0])
	}
	return nil
}

// lavoriSpentiStandalone fa il passo dei lavori spenti e ne controlla l'esito.
func lavoriSpentiStandalone(n nomi, img string, env []string) error {
	fmt.Println("catena: lavori automatici spenti nello standalone (niente scheduler che scrive durante il giro)…")
	out, errDocker := dockerTiene(comandoLavori(n, img, env)...)
	if err := leggiEsitoLavori(out); err != nil {
		return errors.Join(err, errDocker)
	}
	if errDocker != nil {
		return fmt.Errorf("lavori nello standalone: %w", errDocker)
	}
	return nil
}
