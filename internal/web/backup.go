package web

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/L0cutus/gestrip/internal/backup"
)

// «Esegui adesso» del backup (slice 124, T5.2, D-115), solo del Go: lo standalone fa il backup fuori dal
// pacchetto (OPS-28). Qui c'è solo l'azione del titolare, POST /settings/backup/esegui (CSRF e ruolo dalla
// catena di f.rotta, come gli altri moduli del titolare); la pagina Impostazioni → Backup con il pulsante, lo
// stato e lo storico è della 126, e fino ad allora il ritorno a /settings/backup non ha una pagina.
//
// Il backup dura minuti: l'azione prende subito il lucchetto dei backup (con un backup già in corso, anche del
// comando gestrip backup in un altro processo, rifiuta senza far partire niente), poi il backup gira in
// sottofondo con il contesto dei lavori di serve e l'esito va nello storico dei backup.

// BackupAMano è il backup di serve visto dalla faccia (backup.Notturno).
type BackupAMano interface {
	EseguiAdesso() error
}

const (
	rottaPaginaBackup       = "/settings/backup"
	msgBackupAvviato        = "Backup avviato: ci vuole qualche minuto. L'esito compare nello storico dei backup."
	msgBackupInCorso        = "Un backup è già in corso: riprova quando ha finito."
	msgBackupNonConfigurato = "Il backup non è configurato: manca la chiavetta nel file dell'apparecchio."
	msgBackupFermato        = "Il programma si sta fermando: il backup non è partito."
	msgBackupNonAvviato     = "Non è stato possibile avviare il backup: riprova."
)

// eseguiBackup è «Esegui adesso»: torna sempre alla pagina del backup con l'esito della partenza.
func (f *facciaWeb) eseguiBackup(w http.ResponseWriter, r *http.Request) {
	if f.backup == nil {
		Errore(w, r)
		return
	}
	st := statoDi(r)
	switch err := f.backup.EseguiAdesso(); {
	case err == nil:
		st.dati.Info = msgBackupAvviato
	case errors.Is(err, backup.ErrBackupInCorso):
		st.dati.Errore = msgBackupInCorso
	case errors.Is(err, backup.ErrNonConfigurato):
		st.dati.Errore = msgBackupNonConfigurato
	case errors.Is(err, backup.ErrFermato):
		st.dati.Errore = msgBackupFermato
	default:
		f.registro.Error("backup a mano non partito", "tipo", fmt.Sprintf("%T", err))
		st.dati.Errore = msgBackupNonAvviato
	}
	st.cambiata = true
	f.vaiA(w, r, rottaPaginaBackup)
}

// registraBackup registra l'azione del backup (solo il titolare).
func (f *facciaWeb) registraBackup(mux *http.ServeMux) {
	f.rotta(mux, "POST "+rottaPaginaBackup+"/esegui", soloTitolare, f.eseguiBackup)
}
