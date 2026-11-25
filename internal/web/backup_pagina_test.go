package web

import (
	"context"
	"errors"
	"html"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/accounts"
	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/backup"
	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
	"github.com/L0cutus/gestrip/internal/media"
	"github.com/L0cutus/gestrip/internal/ratelimit"
)

// La pagina Impostazioni → Backup e il banner rosso del titolare (backup.go, slice 126, tema E, risposta H). Il
// backup è finto (backup.Stato scritto a mano); le condizioni dei problemi le provano i test di internal/backup
// (stato_test.go), qui si prova chi vede cosa e dove. Dati inventati.

const idBanner = `id="backup-alarm"`

// statoSano è un backup configurato senza problemi, a martediPromemoria.
func statoSano() backup.Stato {
	adesso := martediPromemoria
	return backup.Stato{Adesso: adesso, Configurato: true, Ora: 2*time.Hour + 30*time.Minute,
		Prossimo:     time.Date(2026, 11, 18, 1, 30, 0, 0, time.UTC),
		Esecuzioni:   []backup.Esecuzione{{ID: 1, Avvio: backup.AvvioAutomatico, IniziatoIl: adesso.Add(-7 * time.Hour), FinitoIl: adesso.Add(-7*time.Hour + 4*time.Minute), Esito: backup.EsitoOK}},
		UltimoBackup: db.ArchivioRegistrato{CatturatoIl: adesso.Add(-7 * time.Hour)}, CeBackup: true,
		PrimoBackup: adesso.Add(-30 * 24 * time.Hour),
		Prova:       db.ProvaRipristino{Avvio: db.AvvioProvaAutomatico, Riuscita: true, FinitoIl: adesso.Add(-2 * 24 * time.Hour)}, CeProva: true}
}

// statiConProblema sono uno stato per ogni condizione dell'allarme, ognuno con il suo codice.
func statiConProblema() map[string]backup.Stato {
	casi := map[string]backup.Stato{}
	s := statoSano()
	s.Configurato = false
	casi[backup.ProblemaNonConfigurato] = s
	s = statoSano()
	s.Chiavetta = backup.ErrNonMontata
	casi[backup.ProblemaChiavetta] = s
	s = statoSano()
	s.Esecuzioni = []backup.Esecuzione{{ID: 2, Avvio: backup.AvvioAMano, IniziatoIl: martediPromemoria.Add(-time.Hour), FinitoIl: martediPromemoria.Add(-time.Hour), Esito: backup.EsitoFallito, Errore: "motivo inventato"}}
	casi[backup.ProblemaBackupFallito] = s
	s = statoSano()
	s.UltimoBackup.CatturatoIl = martediPromemoria.Add(-27 * time.Hour)
	casi[backup.ProblemaBackupVecchio] = s
	s = statoSano()
	s.PiuAvanti = martediPromemoria.Add(48 * time.Hour)
	casi[backup.ProblemaOrologio] = s
	s = statoSano()
	s.Prova.Riuscita, s.Prova.Errore = false, "prova inventata non riuscita"
	casi[backup.ProblemaProvaFallita] = s
	s = statoSano()
	s.Prova.FinitoIl = martediPromemoria.Add(-9 * 24 * time.Hour)
	casi[backup.ProblemaProvaMancante] = s
	s = statoSano()
	s.Rovinati, s.NomiRovinati = 1, []string{strings.Repeat("ab", 32)}
	casi[backup.ProblemaMediaRovinati] = s
	return casi
}

// testoProblema è il testo del problema con il codice c nello stato s (html come lo scrive il template).
func testoProblema(t *testing.T, s backup.Stato, c string) string {
	t.Helper()
	for _, p := range s.Problemi() {
		if p.Codice == c {
			return html.EscapeString(p.Testo)
		}
	}
	t.Fatalf("lo stato non ha il problema %s: %v", c, s.Problemi())
	return ""
}

// Voce OPS-28, in parte: niente segno «inventario:» (la voce chiude con l'avviso della 127 e la copia fuori sede
// della 128).
// Ogni condizione dell'allarme accende il banner del titolare su ogni pagina interna con il suo testo, mai per il
// dipendente; uno stato senza problemi non lo accende; la pagina Backup non ripete il banner ma ha i problemi.
func TestBannerBackupPerCondizioneERuolo(t *testing.T) {
	for codice, s := range statiConProblema() {
		t.Run(codice, func(t *testing.T) {
			bf := &backupFinto{stato: s}
			h, _ := facciaBackup(t, bf)
			tit := titolareDentro(t, h)
			testo := testoProblema(t, s, codice)
			for _, p := range []string{"/", "/settings/utenti", "/audit"} {
				r := tit.vai("GET", p, nil, nil)
				if r.codice != 200 {
					t.Fatalf("%s: %d", p, r.codice)
				}
				contiene(t, "titolare "+p, r.corpo, idBanner, `aria-labelledby="backup-alarm-title"`, testo, `href="/settings/backup"`)
			}
			r := tit.vai("GET", rottaPaginaBackup, nil, nil)
			contiene(t, "pagina", r.corpo, `id="backup-problems"`, testo)
			nonContiene(t, "pagina", r.corpo, idBanner, `id="backup-ok"`)
			dip := dipendenteDentro(t, h)
			for _, p := range []string{"/", "/nuova"} {
				r := dip.vai("GET", p, nil, nil)
				if r.codice != 200 {
					t.Fatalf("dipendente %s: %d", p, r.codice)
				}
				nonContiene(t, "dipendente "+p, r.corpo, idBanner, testo)
			}
		})
	}
	bf := &backupFinto{stato: statoSano()}
	h, _ := facciaBackup(t, bf)
	tit := titolareDentro(t, h)
	nonContiene(t, "sano", tit.vai("GET", "/", nil, nil).corpo, idBanner)
	r := tit.vai("GET", rottaPaginaBackup, nil, nil)
	contiene(t, "sano: pagina", r.corpo, `id="backup-ok"`)
	nonContiene(t, "sano: pagina", r.corpo, `id="backup-problems"`)
}

// Voce OPS-28, in parte (vedi TestBannerBackupPerCondizioneERuolo).
// Con i lavori automatici spenti (sviluppo e banco) tacciono solo «non configurato», «backup vecchio» e «prova
// mancante»; la pagina dice perché e li mostra. Chiavetta, esecuzione fallita e stato illeggibile suonano comunque.
// Con lo stato illeggibile la pagina lo dice senza i dati, e con la nota dei lavori spenti se li conosce.
func TestBannerBackupLavoroSpentoEGuasto(t *testing.T) {
	s := statoSano()
	s.Configurato, s.LavoroSpento = false, true
	bf := &backupFinto{stato: s}
	h, _ := facciaBackup(t, bf)
	tit := titolareDentro(t, h)
	nonContiene(t, "spento", tit.vai("GET", "/", nil, nil).corpo, idBanner)
	r := tit.vai("GET", rottaPaginaBackup, nil, nil)
	contiene(t, "spento: pagina", r.corpo, html.EscapeString(msgLavoroSpento), `id="backup-problems"`,
		testoProblema(t, s, backup.ProblemaNonConfigurato))
	vecchio := statoSano()
	vecchio.LavoroSpento, vecchio.CeBackup, vecchio.CeProva = true, false, false
	bf.cambia(vecchio, nil)
	tit.vai("GET", rottaPaginaBackup, nil, nil) // rinnova la cache
	nonContiene(t, "spento, nessun backup", tit.vai("GET", "/", nil, nil).corpo, idBanner)
	for codice, st := range statiConProblema() {
		if codice == backup.ProblemaNonConfigurato || codice == backup.ProblemaBackupVecchio || codice == backup.ProblemaProvaMancante {
			continue
		}
		st.LavoroSpento = true
		bf.cambia(st, nil)
		tit.vai("GET", rottaPaginaBackup, nil, nil)
		contiene(t, "spento, "+codice, tit.vai("GET", "/", nil, nil).corpo, idBanner, testoProblema(t, st, codice))
	}

	bf = &backupFinto{stato: backup.Stato{LavoroSpento: true}, errStato: errors.New("guasto inventato")}
	h, _ = facciaBackup(t, bf)
	tit = titolareDentro(t, h)
	contiene(t, "spento e guasto", tit.vai("GET", "/", nil, nil).corpo, idBanner, html.EscapeString(msgStatoBackupIlleggibile))
	r = tit.vai("GET", rottaPaginaBackup, nil, nil)
	contiene(t, "spento e guasto: pagina", r.corpo, `id="backup-unreadable"`, html.EscapeString(msgLavoroSpento))

	bf = &backupFinto{errStato: errors.New("guasto inventato")}
	h, _ = facciaBackup(t, bf)
	tit = titolareDentro(t, h)
	contiene(t, "guasto", tit.vai("GET", "/", nil, nil).corpo, idBanner, html.EscapeString(msgStatoBackupIlleggibile))
	r = tit.vai("GET", rottaPaginaBackup, nil, nil)
	contiene(t, "guasto: pagina", r.corpo, `id="backup-unreadable"`, `id="backup-run-button"`)
	nonContiene(t, "guasto: pagina", r.corpo, "guasto inventato", `id="backup-runs"`, `id="backup-jobs-off"`)
}

// Voce OPS-28, in parte (vedi TestBannerBackupPerCondizioneERuolo).
// Il banner non è un .flash (app.js toglie e rimette i .flash quando rilegge una pagina, il banco legge
// .flash-errore@1), non è un role="alert" (si annuncerebbe a ogni pagina) e il suo titolo non è un heading prima
// dell'h1; le section della pagina Backup sono chiuse tutte (la sezione delle impostazioni compresa).
func TestBannerBackupForma(t *testing.T) {
	s := statoSano()
	s.Chiavetta = backup.ErrNonMontata
	h, _ := facciaBackup(t, &backupFinto{stato: s})
	tit := titolareDentro(t, h)
	r := tit.vai("GET", "/audit", nil, nil)
	i := strings.Index(r.corpo, `<aside id="backup-alarm"`)
	if i < 0 {
		t.Fatalf("banner mancante")
	}
	fine := strings.Index(r.corpo[i:], "</aside>")
	banner := r.corpo[i : i+fine]
	tag := banner[:strings.Index(banner, ">")]
	nonContiene(t, "banner", tag, "flash", "role=")
	nonContiene(t, "banner", banner, "<h1", "<h2", "<h3")
	if h1 := strings.Index(r.corpo, "<h1"); h1 >= 0 && h1 < i {
		t.Errorf("banner dopo l'h1")
	}
	p := tit.vai("GET", rottaPaginaBackup, nil, nil).corpo
	if a, c := strings.Count(p, "<section"), strings.Count(p, "</section>"); a != c {
		t.Errorf("pagina Backup: %d <section e %d </section>", a, c)
	}
}

// Voce OPS-28, in parte (vedi TestBannerBackupPerCondizioneERuolo).
// La pagina di firma (il titolare passa il tablet al cliente) non ha il banner, nemmeno con un problema; la scheda
// della stessa busta sì.
func TestBannerBackupNonSullaFirma(t *testing.T) {
	dir := t.TempDir()
	d, err := db.Apri(filepath.Join(dir, "gestrip.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Chiudi() })
	o := clock.NuovoFinto(time.Date(2026, 10, 31, 9, 0, 0, 0, time.UTC))
	if _, err := d.Migra(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	inserisciConti(t, d, contiBase)
	a := media.Nuovo(media.CartellaDi(filepath.Join(dir, "gestrip.db")))
	b := buste.Nuove(d, o).ConFoto(a, normalizzaQui{})
	dd := dipendenzeProva(&sondaFinta{pronto: true}, nessunRegistro, accounts.NuoviUtenti(d, o))
	dd.Orologio, dd.Limiti, dd.Buste = o, ratelimit.Nuovo(o), b
	s := statoSano()
	s.Configurato = false
	dd.Backup = &backupFinto{stato: s}
	tit := titolareDentro(t, mustInterna(t, dd))
	id := bustaScheda(t, d, o, nil)
	if _, err := b.CambiaStato(context.Background(), id, "pronta", audit.Contesto{Attore: "titolare@esempio.it"}); err != nil {
		t.Fatal(err)
	}
	r := tit.vai("GET", percorsoFirma(id), nil, nil)
	if r.codice != 200 {
		t.Fatalf("firma: %d", r.codice)
	}
	nonContiene(t, "firma", r.corpo, idBanner)
	contiene(t, "scheda", tit.vai("GET", percorsoScheda(id), nil, nil).corpo, idBanner)
}

// Voce OPS-28, in parte (vedi TestBannerBackupPerCondizioneERuolo).
// Una lettura appesa (chiavetta che non risponde): allo scadere del tempo il banner dice «non si legge» e la pagina
// lo stesso; intanto le altre pagine non aprono un'altra lettura (una sola alla volta) e mostrano «non si legge»
// (o l'ultimo banner, se c'era); finita la lettura, il suo stato va in cache. Una richiesta già cancellata (il browser
// che chiude) non cambia la lettura.
func TestBannerBackupLetturaAppesa(t *testing.T) {
	vecchio := tempoStatoBackup
	tempoStatoBackup = 400 * time.Millisecond
	t.Cleanup(func() { tempoStatoBackup = vecchio })
	sblocca := make(chan struct{})
	bf := &backupAppeso{backupFinto: backupFinto{stato: statoSano()}, sblocca: sblocca}
	h, _ := facciaBackup(t, bf)
	tit := titolareDentro(t, h)
	illeggibile := html.EscapeString(msgStatoBackupIlleggibile)
	contiene(t, "prima pagina", tit.vai("GET", "/", nil, nil).corpo, idBanner, illeggibile)
	// Con la lettura in corso la seconda pagina non la aspetta.
	inizio := time.Now()
	contiene(t, "seconda pagina", tit.vai("GET", "/audit", nil, nil).corpo, idBanner, illeggibile)
	if d := time.Since(inizio); d >= tempoStatoBackup {
		t.Errorf("la seconda pagina ha aspettato la lettura in corso: %v", d)
	}
	contiene(t, "pagina Backup", tit.vai("GET", rottaPaginaBackup, nil, nil).corpo, `id="backup-unreadable"`)
	if n := bf.quanteLetture(); n != 1 {
		t.Errorf("letture con una appesa: %d", n)
	}
	close(sblocca)
	// Finita la lettura, il suo stato (sano) va in cache: il banner sparisce senza un'altra lettura.
	sparito := false
	for range 200 {
		if !strings.Contains(tit.vai("GET", "/", nil, nil).corpo, idBanner) {
			sparito = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !sparito {
		t.Error("il banner non sparisce dopo la lettura")
	}
	if n := bf.quanteLetture(); n != 1 {
		t.Errorf("letture dopo: %d", n)
	}
}

// backupAppeso è un backup finto la cui lettura dello stato resta appesa finché sblocca non si chiude.
type backupAppeso struct {
	backupFinto
	sblocca chan struct{}
}

func (b *backupAppeso) Stato(ctx context.Context) (backup.Stato, error) {
	s, err := b.backupFinto.Stato(ctx)
	<-b.sblocca
	return s, err
}

// Voce OPS-28, in parte (vedi TestBannerBackupPerCondizioneERuolo).
// La lettura non usa la cancellazione della richiesta: un browser che chiude non lascia «non si legge» in cache.
func TestBannerBackupRichiestaCancellata(t *testing.T) {
	bf := &backupCancellabile{backupFinto: backupFinto{stato: statoSano()}}
	dd := dipendenzeProva(&sondaFinta{pronto: true}, nessunRegistro, &utentiFinti{attivo: true})
	dd.Backup = bf
	_, f, err := costruisciInterna(dd, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, annulla := context.WithCancel(context.Background())
	annulla()
	r := httptest.NewRequest("GET", "/", nil).WithContext(ctx)
	if v := f.allarmeBackup(r); v != nil {
		t.Errorf("richiesta cancellata: banner %v", v.Voci)
	}
	if v, ok := f.allarme.valida(f.orologio.Adesso()); !ok || v != nil {
		t.Errorf("cache dopo la richiesta cancellata: %v %v", v, ok)
	}
}

// backupCancellabile dà l'errore del contesto se è finito, come una lettura vera del database.
type backupCancellabile struct{ backupFinto }

func (b *backupCancellabile) Stato(ctx context.Context) (backup.Stato, error) {
	if err := ctx.Err(); err != nil {
		return backup.Stato{}, err
	}
	return b.backupFinto.Stato(ctx)
}

// Voce OPS-28, in parte (vedi TestBannerBackupPerCondizioneERuolo).
// Il banner non compare sulle pagine pubbliche né su quelle d'ingresso, nemmeno con un problema.
func TestBannerBackupMaiFuoriDallePagineInterne(t *testing.T) {
	s := statoSano()
	s.Configurato = false
	dd := dipendenzeProva(&sondaFinta{pronto: true}, nessunRegistro, &utentiFinti{attivo: true})
	dd.Backup = &backupFinto{stato: s}
	hp, err := Pubblica(dd)
	if err != nil {
		t.Fatal(err)
	}
	fuori := nuovoBrowser(t, hp)
	for _, p := range []string{"/r/SINTETICO-inesistente-xxxxxxxxxxxxxxxxxx", "/non-esiste"} {
		nonContiene(t, "pubblica "+p, fuori.vai("GET", p, nil, nil).corpo, idBanner)
	}
	hi := mustInterna(t, dd)
	ingresso := nuovoBrowser(t, hi)
	for _, p := range []string{"/login", "/reset-request"} {
		nonContiene(t, "ingresso "+p, ingresso.vai("GET", p, nil, nil).corpo, idBanner)
	}
}

// Voce OPS-28, in parte (vedi TestBannerBackupPerCondizioneERuolo).
// Lo stato del banner si legge al più una volta al minuto (cache); passato il minuto, o con l'orologio tornato
// indietro, si rilegge. La pagina Backup lo legge sempre e rinnova il banner: sistemato il problema, il banner
// sparisce subito dopo averla aperta.
func TestBannerBackupCache(t *testing.T) {
	s := statoSano()
	s.Chiavetta = backup.ErrNonMontata
	bf := &backupFinto{stato: s}
	h, _, o := facciaBackupOrologio(t, bf)
	tit := titolareDentro(t, h)
	tit.vai("GET", "/", nil, nil)
	prima := bf.quanteLetture()
	if prima != 1 {
		t.Fatalf("letture dopo la prima pagina: %d", prima)
	}
	contiene(t, "prima", tit.vai("GET", "/audit", nil, nil).corpo, idBanner)
	o.Sposta(durataAllarme - time.Second)
	bf.cambia(statoSano(), nil)
	contiene(t, "in cache", tit.vai("GET", "/", nil, nil).corpo, idBanner)
	if bf.quanteLetture() != 1 {
		t.Errorf("letture in cache: %d", bf.quanteLetture())
	}
	o.Sposta(time.Second)
	nonContiene(t, "dopo il minuto", tit.vai("GET", "/", nil, nil).corpo, idBanner)
	if bf.quanteLetture() != 2 {
		t.Errorf("letture dopo il minuto: %d", bf.quanteLetture())
	}
	// Orologio indietro: si rilegge.
	bf.cambia(s, nil)
	o.Sposta(-10 * time.Second)
	contiene(t, "orologio indietro", tit.vai("GET", "/", nil, nil).corpo, idBanner)
	if bf.quanteLetture() != 3 {
		t.Errorf("letture con l'orologio indietro: %d", bf.quanteLetture())
	}
	// La pagina Backup rilegge sempre e rinnova la cache.
	bf.cambia(statoSano(), nil)
	tit.vai("GET", rottaPaginaBackup, nil, nil)
	nonContiene(t, "dopo la pagina", tit.vai("GET", "/", nil, nil).corpo, idBanner)
	if bf.quanteLetture() != 4 {
		t.Errorf("letture dopo la pagina: %d", bf.quanteLetture())
	}
	// Il dipendente non legge lo stato.
	dip := dipendenteDentro(t, h)
	o.Sposta(2 * durataAllarme) // la cache è scaduta: se il dipendente leggesse lo stato, si vedrebbe
	if r := dip.vai("GET", "/", nil, nil); r.codice != 200 {
		t.Fatalf("dipendente: %d", r.codice)
	}
	if bf.quanteLetture() != 4 {
		t.Errorf("letture del dipendente: %d", bf.quanteLetture())
	}
}

// Voce OPS-28, in parte (vedi TestBannerBackupPerCondizioneERuolo).
// La pagina con i dati: stato, chiavetta, ora e prossimo giro, il modulo «Esegui adesso» con il CSRF, le ultime
// esecuzioni con avvio, esito e motivo, il registro con destinazione ed esito (dell'archivio solo il nome del file),
// l'ultima prova, i media rovinati; la scheda «Backup» aperta nel menu delle impostazioni.
func TestPaginaBackupConDati(t *testing.T) {
	s := statoSano()
	adesso := martediPromemoria
	s.Esecuzioni = []backup.Esecuzione{
		{ID: 3, Avvio: backup.AvvioAMano, IniziatoIl: adesso.Add(-time.Hour), FinitoIl: adesso.Add(-time.Hour + 90*time.Second), Esito: backup.EsitoAvviso, Errore: "rotazione inventata non finita"},
		{ID: 2, Avvio: backup.AvvioAutomatico, IniziatoIl: adesso.Add(-31 * time.Hour), FinitoIl: adesso.Add(-31*time.Hour + 5*time.Minute), Esito: backup.EsitoFallito, Errore: "motivo inventato <b>"},
	}
	s.Verifiche = []db.RigaRegistroBackup{
		{ID: 5, Riuscita: true, Destinazione: db.DestinazioneChiavetta, Archivio: "/media/chiavetta-inventata/gestrip-20261117T013000Z.tar.age",
			Byte: 3 * 1024 * 1024 / 2, CatturatoIl: adesso.Add(-7 * time.Hour), VerificatoIl: adesso.Add(-7 * time.Hour), MediaRovinati: 2},
		{ID: 4, Riuscita: false, Destinazione: db.DestinazioneCartella, Archivio: "/tmp/inventata/b.tar.age", Byte: 10,
			CatturatoIl: adesso.Add(-50 * time.Hour), VerificatoIl: adesso.Add(-50 * time.Hour), Errore: "verifica inventata"},
	}
	s.Rovinati, s.NomiRovinati = 102, []string{strings.Repeat("ab", 32), strings.Repeat("cd", 32)}
	s.Prova.CatturatoIl = adesso.Add(-50 * time.Hour)
	bf := &backupFinto{stato: s}
	b := facciaConBackup(t, bf)
	r := b.vai("GET", rottaPaginaBackup, nil, nil)
	if r.codice != 200 {
		t.Fatalf("pagina: %d", r.codice)
	}
	contiene(t, "pagina", r.corpo, "<h1>Backup</h1>", `id="settings-tab-backup"`, `aria-current="page" class="settings-tab">Backup`,
		`<form id="backup-run" action="/settings/backup/esegui" method="post"`, `name="_csrf_token" type="hidden" value="`+b.token+`"`,
		`id="backup-usb">inserita e pronta`, `id="backup-hour">02:30`, `id="backup-next">mercoledì 18/11/2026 02:30`,
		"17/11/2026 09:00", "a mano · durata 1 min", "riuscito, con un avviso", "badge-attesa", "Motivo: rotazione inventata non finita",
		"automatico · durata 5 min", "non riuscito", "motivo inventato &lt;b&gt;",
		"destinazione: chiavetta · 1,5 MB · gestrip-20261117T013000Z.tar.age · media rovinati: 2",
		"destinazione: cartella (a mano)", "verifica non riuscita", "Motivo: verifica inventata",
		`id="backup-restore-test"`, "riuscita", "backup del 15/11/2026 08:00",
		`id="backup-damaged"`, "trovato 102 file", strings.Repeat("ab", 32), "… e altri 100.")
	nonContiene(t, "pagina", r.corpo, "/media/chiavetta-inventata", "/tmp/inventata", "motivo inventato <b>")
}

// Voce OPS-28, in parte (vedi TestBannerBackupPerCondizioneERuolo).
// La pagina senza dati: nessun backup, nessuna verifica, nessuna prova, niente media rovinati; con la chiavetta
// configurata il prossimo giro dice «entro pochi minuti» se il turno è da fare.
func TestPaginaBackupSenzaDati(t *testing.T) {
	bf := &backupFinto{stato: backup.Stato{Adesso: martediPromemoria}}
	b := facciaConBackup(t, bf)
	r := b.vai("GET", rottaPaginaBackup, nil, nil)
	contiene(t, "vuota", r.corpo, "Nessun backup ancora.", "Nessun backup verificato ancora.", "Nessuna prova ancora",
		`id="backup-usb">non configurata`, "nessuno: il backup non è configurato", `id="backup-run-button"`)
	nonContiene(t, "vuota", r.corpo, `id="backup-damaged"`, `id="backup-jobs-off"`)
	bf.cambia(backup.Stato{Adesso: martediPromemoria, Configurato: true, DaFare: true, Chiavetta: backup.ErrNonMontata}, nil)
	r = b.vai("GET", rottaPaginaBackup, nil, nil)
	contiene(t, "da fare", r.corpo, "entro pochi minuti", `id="backup-usb">non risponde`)
	bf.cambia(backup.Stato{Adesso: martediPromemoria, Configurato: true, LavoroSpento: true}, nil)
	r = b.vai("GET", rottaPaginaBackup, nil, nil)
	contiene(t, "spento", r.corpo, "nessuno: i lavori automatici sono spenti")
}

// Voce OPS-28, in parte (vedi TestBannerBackupPerCondizioneERuolo).
// La pagina è solo del titolare: il dipendente torna a / con «Operazione riservata al titolare.» senza che lo
// stato si legga; senza sessione, il login.
func TestPaginaBackupRuolo(t *testing.T) {
	bf := &backupFinto{stato: statoSano()}
	h, _ := facciaBackup(t, bf)
	dip := dipendenteDentro(t, h)
	r := dip.vai("GET", rottaPaginaBackup, nil, nil)
	if !redirectA(r, "/") {
		t.Errorf("dipendente: %d %q", r.codice, r.location)
	}
	contiene(t, "dipendente", dip.vai("GET", "/", nil, nil).corpo, "Operazione riservata al titolare.")
	fuori := nuovoBrowser(t, h)
	if r := fuori.vai("GET", rottaPaginaBackup, nil, nil); r.codice != 302 || r.location == rottaPaginaBackup || r.location == "/" {
		t.Errorf("senza sessione: %d %q", r.codice, r.location)
	}
	if bf.quanteLetture() != 0 {
		t.Errorf("letture: %d", bf.quanteLetture())
	}
}
