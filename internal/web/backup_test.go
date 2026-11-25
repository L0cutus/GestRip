package web

import (
	"context"
	"errors"
	"html"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/L0cutus/gestrip/internal/accounts"
	"github.com/L0cutus/gestrip/internal/backup"
	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
	"github.com/L0cutus/gestrip/internal/ratelimit"
)

// «Esegui adesso» del backup (backup.go, slice 124, D-115): solo il titolare, con il CSRF della catena; ogni esito
// della partenza torna alla pagina del backup con la sua frase. Il backup è finto (conta le chiamate). Dati
// inventati.

type backupFinto struct {
	mu       sync.Mutex
	chiamate int
	err      error
	stato    backup.Stato // lo stato che Stato dà
	errStato error
	letture  int // quante volte lo stato è stato letto
}

func (b *backupFinto) EseguiAdesso() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.chiamate++
	return b.err
}

func (b *backupFinto) Stato(context.Context) (backup.Stato, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.letture++
	return b.stato, b.errStato
}

func (b *backupFinto) quante() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.chiamate
}

func (b *backupFinto) quanteLetture() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.letture
}

func (b *backupFinto) cambia(s backup.Stato, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.stato, b.errStato = s, err
}

func facciaConBackup(t *testing.T, bf *backupFinto) *browser {
	t.Helper()
	h, _ := facciaBackup(t, bf)
	b := titolareDentro(t, h)
	b.vai("GET", "/", nil, nil) // il token della sessione nuova
	return b
}

func facciaBackup(t *testing.T, bf BackupDelNegozio) (http.Handler, *db.DB) {
	t.Helper()
	h, d, _ := facciaBackupOrologio(t, bf)
	return h, d
}

func facciaBackupOrologio(t *testing.T, bf BackupDelNegozio) (http.Handler, *db.DB, *clock.Finto) {
	t.Helper()
	d, err := db.Apri(filepath.Join(t.TempDir(), "gestrip.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Chiudi() })
	o := clock.NuovoFinto(martediPromemoria)
	if _, err := d.Migra(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	inserisciConti(t, d, contiBase)
	dd := dipendenzeProva(&sondaFinta{pronto: true}, nessunRegistro, accounts.NuoviUtenti(d, o))
	dd.Orologio, dd.Limiti = o, ratelimit.Nuovo(o)
	if bf != nil {
		dd.Backup = bf
	}
	return mustInterna(t, dd), d, o
}

const rottaEseguiBackup = "/settings/backup/esegui"

// Voce OPS-28, in parte: niente segno «inventario:» (la voce chiude con 125-127).
// Il titolare fa partire il backup: ritorno alla pagina del backup con «Backup avviato»; ogni rifiuto della partenza
// (già in corso, non configurato, serve che si ferma, un guasto) ha la sua frase e niente «avviato».
func TestEseguiBackupTitolare(t *testing.T) {
	for _, c := range []struct {
		nome  string
		err   error
		frase string
	}{
		{"partito", nil, msgBackupAvviato},
		{"in corso", backup.ErrBackupInCorso, msgBackupInCorso},
		{"in corso, avvolto", errors.Join(errors.New("altro"), backup.ErrBackupInCorso), msgBackupInCorso},
		{"non configurato", backup.ErrNonConfigurato, msgBackupNonConfigurato},
		{"fermato", backup.ErrFermato, msgBackupFermato},
		{"guasto", errors.New("guasto inventato"), msgBackupNonAvviato},
	} {
		t.Run(c.nome, func(t *testing.T) {
			bf := &backupFinto{err: c.err}
			b := facciaConBackup(t, bf)
			r := b.vai("POST", rottaEseguiBackup, url.Values{"_csrf_token": {b.token}}, nil)
			if !redirectA(r, rottaPaginaBackup) || bf.quante() != 1 {
				t.Fatalf("esegui: %d %q, chiamate %d", r.codice, r.location, bf.quante())
			}
			// Il messaggio si vede sulla pagina Backup (nota 10(b): prima della 126 era un 404).
			r = b.vai("GET", rottaPaginaBackup, nil, nil)
			if r.codice != 200 {
				t.Fatalf("pagina dopo esegui: %d", r.codice)
			}
			contiene(t, c.nome, r.corpo, html.EscapeString(c.frase))
			if c.err != nil {
				nonContiene(t, c.nome, r.corpo, html.EscapeString(msgBackupAvviato))
			}
		})
	}
}

// Voce OPS-28, in parte (vedi TestEseguiBackupTitolare).
// Il dipendente torna a / con «Operazione riservata al titolare.» senza far partire niente; senza il token CSRF
// (anche il titolare) 403 e niente; senza sessione il login.
func TestEseguiBackupRuoloECSRF(t *testing.T) {
	bf := &backupFinto{}
	h, _ := facciaBackup(t, bf)
	dip := dipendenteDentro(t, h)
	dip.vai("GET", "/", nil, nil)
	if r := dip.vai("POST", rottaEseguiBackup, url.Values{"_csrf_token": {dip.token}}, nil); r.codice != 302 || r.location != "/" {
		t.Errorf("dipendente: %d %q", r.codice, r.location)
	}
	tit := titolareDentro(t, h)
	tit.vai("GET", "/", nil, nil)
	for nome, v := range map[string]url.Values{"senza token": {}, "token sbagliato": {"_csrf_token": {"inventato"}}} {
		if r := tit.vai("POST", rottaEseguiBackup, v, nil); r.codice != 403 {
			t.Errorf("titolare %s: %d", nome, r.codice)
		}
	}
	fuori := nuovoBrowser(t, h)
	if r := fuori.vai("POST", rottaEseguiBackup, url.Values{}, nil); r.codice == 302 && r.location == rottaPaginaBackup {
		t.Errorf("senza sessione: %d %q", r.codice, r.location)
	}
	if bf.quante() != 0 {
		t.Errorf("chiamate %d", bf.quante())
	}
	// Senza il backup nelle dipendenze azione e pagina rispondono con l'errore, senza panico, e niente banner.
	h2, _ := facciaBackup(t, nil)
	b := titolareDentro(t, h2)
	if r := b.vai("GET", "/", nil, nil); strings.Contains(r.corpo, idBanner) {
		t.Errorf("senza backup: banner")
	}
	if r := b.vai("POST", rottaEseguiBackup, url.Values{"_csrf_token": {b.token}}, nil); r.codice < 400 {
		t.Errorf("senza backup: %d", r.codice)
	}
	if r := b.vai("GET", rottaPaginaBackup, nil, nil); r.codice < 400 {
		t.Errorf("pagina senza backup: %d", r.codice)
	}
}
