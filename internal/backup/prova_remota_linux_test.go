package backup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
	"github.com/L0cutus/gestrip/internal/sftpfinto"
)

// La prova mensile della copia remota (slice 129c, risposta E). Orologio finto, database e media inventati, chiavetta
// finta della 123, server SFTP finto (internal/sftpfinto: niente rete); backup, copia remota, download, verifica,
// registro e lucchetto veri dove non si dice altro. Il 06/12/2026 e il 03/01/2027 sono le prime domeniche del mese.

// scenaRemota è un backup notturno con la copia remota configurata sul server finto.
type scenaRemota struct {
	pn  *provaNotturno
	srv *sftpfinto.Server
	d   *db.DB
	// prepara è quello che il test cambia in ogni Remoto (oltre alla connessione al server finto).
	prepara func(*Remoto)
}

// nuovaScenaRemota: configurata due giorni prima di adesso (la prova non aspetta la prima copia).
func nuovaScenaRemota(t *testing.T, adesso time.Time) *scenaRemota {
	t.Helper()
	pn := nuovaProvaNotturno(t, adesso)
	srv := sftpfinto.Nuovo(t)
	if err := os.Mkdir(srv.Locale(cartellaRemota), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, _, err := CreaChiaveSFTP(pn.p.dir); err != nil {
		t.Fatal(err)
	}
	firma, err := LeggiChiaveSFTP(pn.p.dir)
	if err != nil {
		t.Fatal(err)
	}
	srv.Autorizza(firma.PublicKey())
	cfg := ConfigRemota{Host: "u1.storage.invalid", Porta: 23, Utente: "u1-sub1", Cartella: cartellaRemota,
		TipoChiaveServer: ssh.KeyAlgoED25519, Impronta: srv.Impronta()}
	if err := ScriviConfigRemota(context.Background(), pn.p.s.DB, cfg, adesso.Add(-48*time.Hour)); err != nil {
		t.Fatal(err)
	}
	sc := &scenaRemota{pn: pn, srv: srv, d: pn.p.s.DB}
	pn.n.preparaRemoto = sc.preparaRemoto
	return sc
}

func (sc *scenaRemota) preparaRemoto(r *Remoto) {
	r.Componi = sc.srv.Componi
	if sc.prepara != nil {
		sc.prepara(r)
	}
}

// backupConCopia fa un backup vero all'ora at con la copia remota riuscita, e dà la riga «sftp» del registro.
func (sc *scenaRemota) backupConCopia(t *testing.T, at time.Time) db.ArchivioRegistrato {
	t.Helper()
	fattoBackup(t, sc.pn, at)
	u, ok, err := db.UltimoVerificatoRemoto(context.Background(), sc.d.Lettura)
	if err != nil || !ok || !u.CatturatoIl.Equal(at) {
		t.Fatalf("copia remota: %+v %v %v", u, ok, err)
	}
	return u
}

// aMano fa la prova a mano adesso (orologio della scena), con il lucchetto preso e rilasciato qui.
func (sc *scenaRemota) aMano(t *testing.T, ctx context.Context) (db.ProvaRemota, bool, error) {
	t.Helper()
	l, err := PrendiLucchetto(sc.pn.p.dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Rilascia() }()
	var righe strings.Builder
	p, scritta, err := sc.pn.n.ProvaRemotaAMano(ctx, l, sc.preparaRemoto, func(r string) error { righe.WriteString(r); return nil })
	for _, vietato := range []string{"storage.invalid", "u1-sub1", cartellaRemota + "/"} {
		if strings.Contains(righe.String(), vietato) {
			t.Errorf("righe con %q: %q", vietato, righe.String())
		}
	}
	return p, scritta, err
}

// nessunaTemporanea vuole la cartella dei dati senza cartelle temporanee di backup, verifica o download.
func nessunaTemporanea(t *testing.T, dati string) {
	t.Helper()
	if m, _ := filepath.Glob(filepath.Join(dati, ".*backup-*")); len(m) != 0 {
		t.Errorf("cartelle temporanee rimaste: %v", m)
	}
}

func righeProveRemote(t *testing.T, d *db.DB) []string {
	t.Helper()
	rr, err := d.Lettura.QueryContext(context.Background(), `SELECT avvio, mese, esito, coalesce(codice, '') FROM prove_remote ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rr.Close() }()
	var v []string
	for rr.Next() {
		var a, m, e, c string
		if err := rr.Scan(&a, &m, &e, &c); err != nil {
			t.Fatal(err)
		}
		v = append(v, strings.TrimSpace(a+" "+m+" "+e+" "+c))
	}
	return v
}

func rigaProvaRemota(t *testing.T, d *db.DB, avvio, mese string, riuscita bool, fine time.Time) {
	t.Helper()
	p := db.ProvaRemota{Avvio: avvio, Mese: mese, Turno: Turno(fine, OraPredefinita), IniziatoIl: fine.Add(-time.Minute), FinitoIl: fine, Riuscita: riuscita,
		Codice: CodiceRemotoRete, Errore: "motivo inventato"}
	if riuscita {
		p.Codice, p.Errore, p.Archivio, p.SHA256, p.Byte, p.CatturatoIl = "", "", "/backup/a.tar.age", strings.Repeat("a", 64), 10, fine.Add(-time.Hour)
	}
	if _, err := db.RegistraProvaRemota(context.Background(), d, p); err != nil {
		t.Fatal(err)
	}
}

func provaRemotaDovutaAlle(t *testing.T, n *Notturno, o *clock.Finto, at time.Time) bool {
	t.Helper()
	o.Imposta(at)
	v, err := n.ProvaRemotaDovuta(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// Il mese di una prova comincia con la prima domenica: dal primo al sabato prima è ancora il mese prima.
func TestMeseDi(t *testing.T) {
	for _, c := range []struct {
		turno clock.Data
		mese  string
	}{
		{clock.Data{Anno: 2026, Mese: 12, Giorno: 1}, "2026-11"},
		{clock.Data{Anno: 2026, Mese: 12, Giorno: 5}, "2026-11"},
		{clock.Data{Anno: 2026, Mese: 12, Giorno: 6}, "2026-12"},
		{clock.Data{Anno: 2026, Mese: 12, Giorno: 31}, "2026-12"},
		{clock.Data{Anno: 2027, Mese: 1, Giorno: 2}, "2026-12"},
		{clock.Data{Anno: 2027, Mese: 1, Giorno: 3}, "2027-01"},
		{clock.Data{Anno: 2026, Mese: 11, Giorno: 1}, "2026-11"}, // il primo del mese è domenica
	} {
		if m := MeseDi(c.turno); m != c.mese {
			t.Errorf("%v: %s, atteso %s", c.turno, m, c.mese)
		}
	}
}

// Voce OPS-28, in parte (vedi TestProvaRemotaDelMese).
// Il calendario: la prova del mese è dovuta dalla prima domenica, dopo il backup del turno e dopo la prova settimanale
// (riuscita o con i tre tentativi finiti); tentativi a 30 minuti, al più tre per notte, anche dopo un riavvio; un mese
// non riuscito si riprova ogni notte dopo il backup fino alla prima riuscita o alla prima domenica del mese seguente
// (revisione); le prove a mano non contano; spento la prima domenica, si recupera nei giorni dopo;
// configurata da poco e senza copie aspetta 26 ore; senza copia remota configurata, o senza chiavetta, mai.
func TestProvaRemotaDovuta(t *testing.T) {
	sc := nuovaScenaRemota(t, alle(2026, 12, 6, 2, 0))
	n, o, d := sc.pn.n, sc.pn.o, sc.d
	ctx := context.Background()
	rigaProvaRemota(t, d, db.AvvioProvaAutomatico, "2026-11", true, alle(2026, 11, 1, 3, 0))
	riga(t, d, AvvioAutomatico, "2026-12-05", EsitoOK, alle(2026, 12, 5, 2, 35))
	rigaProva(t, d, db.AvvioProvaAutomatico, "2026-11-29", true, alle(2026, 11, 29, 2, 40))
	if provaRemotaDovutaAlle(t, n, o, alle(2026, 12, 6, 2, 29)) {
		t.Error("alle 02:29 è ancora il mese di novembre, fatto")
	}
	if provaRemotaDovutaAlle(t, n, o, alle(2026, 12, 6, 2, 30)) {
		t.Error("prima del backup della domenica")
	}
	riga(t, d, AvvioAutomatico, "2026-12-06", EsitoOK, alle(2026, 12, 6, 2, 35))
	if provaRemotaDovutaAlle(t, n, o, alle(2026, 12, 6, 2, 36)) {
		t.Error("prima della prova settimanale")
	}
	rigaProva(t, d, db.AvvioProvaAMano, "2026-12-06", true, alle(2026, 12, 6, 2, 37))
	if provaRemotaDovutaAlle(t, n, o, alle(2026, 12, 6, 2, 38)) {
		t.Error("la prova settimanale a mano non chiude la settimana")
	}
	rigaProva(t, d, db.AvvioProvaAutomatico, "2026-12-06", true, alle(2026, 12, 6, 2, 40))
	if !provaRemotaDovutaAlle(t, n, o, alle(2026, 12, 6, 2, 41)) {
		t.Error("dopo la prova settimanale")
	}
	rigaProvaRemota(t, d, db.AvvioProvaAMano, "2026-12", true, alle(2026, 12, 6, 2, 42))
	if !provaRemotaDovutaAlle(t, n, o, alle(2026, 12, 6, 2, 43)) {
		t.Error("una prova remota a mano non conta per il calendario")
	}
	rigaProvaRemota(t, d, db.AvvioProvaAutomatico, "2026-12", false, alle(2026, 12, 6, 2, 50))
	if provaRemotaDovutaAlle(t, n, o, alle(2026, 12, 6, 3, 19)) {
		t.Error("meno di 30 minuti dopo un tentativo fallito")
	}
	if !provaRemotaDovutaAlle(t, n, o, alle(2026, 12, 6, 3, 20)) {
		t.Error("30 minuti dopo un tentativo fallito")
	}
	rigaProvaRemota(t, d, db.AvvioProvaAutomatico, "2026-12", false, alle(2026, 12, 6, 3, 21))
	rigaProvaRemota(t, d, db.AvvioProvaAutomatico, "2026-12", false, alle(2026, 12, 6, 3, 52))
	if provaRemotaDovutaAlle(t, n, o, alle(2026, 12, 6, 5, 0)) {
		t.Error("dopo tre tentativi")
	}
	// Riavvio (nessuna memoria): i tre tentativi della notte sono nel registro.
	if provaRemotaDovutaAlle(t, NuovoNotturno(d, nil, sc.pn.s.chiavetta, o), o, alle(2026, 12, 6, 5, 0)) {
		t.Error("dopo il riavvio")
	}
	// Revisione della 129c: il mese fallito si riprova la notte dopo, dopo il backup del turno.
	if provaRemotaDovutaAlle(t, n, o, alle(2026, 12, 7, 2, 40)) {
		t.Error("lunedì prima del backup")
	}
	riga(t, d, AvvioAutomatico, "2026-12-07", EsitoOK, alle(2026, 12, 7, 2, 35))
	if !provaRemotaDovutaAlle(t, n, o, alle(2026, 12, 7, 2, 40)) {
		t.Error("lunedì dopo il backup: il mese fallito si riprova")
	}
	rigaProvaRemota(t, d, db.AvvioProvaAutomatico, "2026-12", true, alle(2026, 12, 7, 2, 50))
	riga(t, d, AvvioAutomatico, "2026-12-08", EsitoOK, alle(2026, 12, 8, 2, 35))
	if provaRemotaDovutaAlle(t, n, o, alle(2026, 12, 7, 3, 30)) || provaRemotaDovutaAlle(t, n, o, alle(2026, 12, 8, 3, 0)) {
		t.Error("mese riuscito il lunedì")
	}
	// Mese fatto fino alla prima domenica di gennaio (sabato 02/01/2027 è ancora dicembre).
	riga(t, d, AvvioAutomatico, "2027-01-02", EsitoOK, alle(2027, 1, 2, 2, 35))
	rigaProva(t, d, db.AvvioProvaAutomatico, "2026-12-27", true, alle(2026, 12, 27, 2, 40))
	if provaRemotaDovutaAlle(t, n, o, alle(2027, 1, 2, 3, 0)) {
		t.Error("sabato 02/01 è ancora il mese di dicembre")
	}
	// Spento la prima domenica di gennaio: lunedì, dopo il backup del turno, si recupera (la settimana del 03/01 ha la
	// sua prova settimanale fatta: tre tentativi finiti).
	riga(t, d, AvvioAutomatico, "2027-01-04", EsitoOK, alle(2027, 1, 4, 10, 10))
	for i := range 3 {
		rigaProva(t, d, db.AvvioProvaAutomatico, "2027-01-03", false, alle(2027, 1, 4, 10, 20+i))
	}
	if !provaRemotaDovutaAlle(t, n, o, alle(2027, 1, 4, 11, 0)) {
		t.Error("recupero di lunedì dopo la prova settimanale fallita tre volte")
	}
	// Fine mese: gennaio non riesce mai; sabato 06/02 si riprova ancora gennaio, domenica 07/02 comincia febbraio.
	for i := range 3 {
		rigaProvaRemota(t, d, db.AvvioProvaAutomatico, "2027-01", false, alle(2027, 1, 4, 11, 0).Add(time.Duration(i)*AttesaFraTentativi))
	}
	if provaRemotaDovutaAlle(t, n, o, alle(2027, 1, 4, 13, 0)) {
		t.Error("tre tentativi nel turno del 04/01")
	}
	riga(t, d, AvvioAutomatico, "2027-02-06", EsitoOK, alle(2027, 2, 6, 2, 35))
	rigaProva(t, d, db.AvvioProvaAutomatico, "2027-01-31", true, alle(2027, 1, 31, 2, 40))
	if !provaRemotaDovutaAlle(t, n, o, alle(2027, 2, 6, 3, 0)) {
		t.Error("sabato 06/02: gennaio si riprova")
	}
	for i := range 3 {
		rigaProvaRemota(t, d, db.AvvioProvaAutomatico, "2027-01", false, alle(2027, 2, 6, 3, 0).Add(time.Duration(i)*AttesaFraTentativi))
	}
	if provaRemotaDovutaAlle(t, n, o, alle(2027, 2, 6, 5, 0)) {
		t.Error("tre tentativi nel turno del 06/02")
	}
	riga(t, d, AvvioAutomatico, "2027-02-07", EsitoOK, alle(2027, 2, 7, 2, 35))
	rigaProva(t, d, db.AvvioProvaAutomatico, "2027-02-07", true, alle(2027, 2, 7, 2, 40))
	if !provaRemotaDovutaAlle(t, n, o, alle(2027, 2, 7, 2, 41)) {
		t.Error("domenica 07/02: comincia febbraio")
	}
	if tt, _, _, err := db.TentativiProvaRemota(ctx, d.Lettura, db.AvvioProvaAutomatico, "2027-02", clock.Data{Anno: 2027, Mese: 2, Giorno: 7}); err != nil || tt != 0 {
		t.Errorf("febbraio: %d %v", tt, err)
	}
	// Senza copia remota configurata, mai; senza chiavetta, mai.
	if err := TogliConfigRemota(ctx, d); err != nil {
		t.Fatal(err)
	}
	if provaRemotaDovutaAlle(t, n, o, alle(2027, 2, 7, 2, 41)) {
		t.Error("copia remota non configurata")
	}
	senza := NuovoNotturno(d, nil, "", o)
	if ok, err := senza.ProvaRemotaDovuta(ctx); ok || err != nil {
		t.Errorf("senza chiavetta: %v %v", ok, err)
	}
	// Configurata da poco e senza copie: aspetta la prima copia notturna (26 ore), poi è dovuta (e fallirà).
	cfg := ConfigRemota{Host: "h.invalid", Porta: 22, Utente: "u", Cartella: cartellaRemota, TipoChiaveServer: ssh.KeyAlgoED25519,
		Impronta: sc.srv.Impronta()}
	if err := ScriviConfigRemota(ctx, d, cfg, alle(2027, 2, 6, 3, 0)); err != nil {
		t.Fatal(err)
	}
	if provaRemotaDovutaAlle(t, n, o, alle(2027, 2, 7, 5, 0)) {
		t.Error("configurata da 26 ore senza copie")
	}
	if !provaRemotaDovutaAlle(t, n, o, alle(2027, 2, 7, 5, 1)) {
		t.Error("configurata da più di 26 ore senza copie")
	}
}

// inventario: OPS-28
// Voce OPS-28 (chiude: notturni fuori sede, conservazione, allarme, prova settimanale e, qui, la verifica intera della
// copia fuori sede; resta solo la prova vera della 129b, che non è codice).
// La prima domenica del mese il lavoro notturno fa nello stesso giro il backup con la copia remota, la prova
// settimanale e la prova remota: scarica dal server finto l'ultima copia, la verifica per intera; riga «ok» con la
// copia del registro, niente cartelle temporanee, lucchetto libero, nessun problema; il giro dopo non la rifà; la
// prova non scrive nel registro dei backup (D-11) e non tocca i file del server.
func TestProvaRemotaDelMese(t *testing.T) {
	sc := nuovaScenaRemota(t, alle(2026, 12, 6, 2, 20))
	d := sc.d
	ctx := context.Background()
	riga(t, d, AvvioAutomatico, "2026-12-05", EsitoOK, alle(2026, 12, 5, 2, 35))
	a := &aspettaFinta{giri: 3}
	sc.pn.n.Lavoro(ctx, func(ctx context.Context, dur time.Duration) bool {
		if dur == AttesaDopoAvvio {
			sc.pn.o.Sposta(dur)
		}
		return a.aspetta(ctx, dur)
	})
	if v := strings.Join(righeProve(t, d), ","); v != "automatico 2026-12-06 ok" {
		t.Errorf("prove settimanali %s", v)
	}
	if v := strings.Join(righeProveRemote(t, d), ","); v != "automatico 2026-12 ok" {
		t.Fatalf("prove remote %s", v)
	}
	p, ok, err := db.UltimaProvaRemota(ctx, d.Lettura)
	u, _, _ := db.UltimoVerificatoRemoto(ctx, d.Lettura)
	if err != nil || !ok || !p.Riuscita || p.Archivio != u.Archivio || p.SHA256 != u.SHA256 || p.Byte != u.Byte ||
		!p.CatturatoIl.Equal(alle(2026, 12, 6, 2, 30)) || path.Dir(p.Archivio) != cartellaRemota {
		t.Errorf("prova %+v %v %v, copia %+v", p, ok, err, u)
	}
	nessunaTemporanea(t, sc.pn.p.dir)
	lucchettoLibero(t, sc.pn.p.dir)
	s, err := sc.pn.n.Stato(ctx)
	if err != nil || len(s.Problemi()) != 0 || !s.CeProvaRemota {
		t.Errorf("stato: %v %v", codici(s.Problemi()), err)
	}
	// Il registro dei backup ha solo le righe del backup (chiavetta e sftp), nessuna del download.
	var righe int
	if err := d.Lettura.QueryRowContext(ctx, `SELECT count(*) FROM verifiche_backup`).Scan(&righe); err != nil || righe != 2 {
		t.Errorf("righe del registro dei backup: %d %v", righe, err)
	}
	if v := voceDi(t, sc.srv.Locale(cartellaRemota)); len(v) != 1 || len(sc.srv.Tolti()) != 0 {
		t.Errorf("server toccato: %v %v", v, sc.srv.Tolti())
	}
	sc.pn.o.Sposta(time.Hour)
	if _, fatta, err := sc.pn.n.GiroProvaRemota(ctx); fatta || err != nil {
		t.Errorf("un'ora dopo: %v %v", fatta, err)
	}
}

// Le prove che falliscono, ognuna con il suo codice e la sua riga, senza cartelle temporanee: non configurata (niente
// riga), nessuna copia, copia vecchia (8 giorni esatti passano), catturata nel futuro, copia tolta dal server, byte
// diversi, archivio corrotto sul server (stessi byte, SHA-256 diverso), archivio corrotto registrato così (passa il
// confronto, non la verifica), download interrotto, server lento con il tempo scaduto, impronta diversa, chiave
// rifiutata, spazio locale insufficiente. Una prova riuscita dopo spegne il problema.
func TestProvaRemotaFallisce(t *testing.T) {
	cattura := alle(2026, 12, 6, 2, 30)
	ctx := context.Background()

	// Nessuna copia nel registro (la configurazione c'è).
	vuota := nuovaScenaRemota(t, cattura)
	p, scritta, err := vuota.aMano(t, ctx)
	if !scritta || CodiceRemoto(err) != CodiceRemotoNessuna || p.Codice != CodiceRemotoNessuna || p.Archivio != "" {
		t.Errorf("nessuna copia: %+v %v %v", p, scritta, err)
	}
	// Non configurata: niente riga.
	if err := TogliConfigRemota(ctx, vuota.d); err != nil {
		t.Fatal(err)
	}
	if _, scritta, err := vuota.aMano(t, ctx); scritta || !errors.Is(err, ErrRemotoNonConfigurato) {
		t.Errorf("non configurata: %v %v", scritta, err)
	}
	if v := righeProveRemote(t, vuota.d); len(v) != 1 {
		t.Errorf("righe %v", v)
	}

	sc := nuovaScenaRemota(t, cattura)
	u := sc.backupConCopia(t, cattura)
	sulServer := sc.srv.Locale(u.Archivio)
	buono, err := os.ReadFile(sulServer)
	if err != nil {
		t.Fatal(err)
	}
	rimetti := func() {
		t.Helper()
		sc.srv.Imposta(sftpfinto.Guasti{})
		sc.prepara = nil
		if err := os.WriteFile(sulServer, buono, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	fallisce := func(nome, codice string, at time.Time) db.ProvaRemota {
		t.Helper()
		sc.pn.o.Imposta(at)
		inizio := time.Now()
		p, scritta, err := sc.aMano(t, ctx)
		if !scritta || err == nil || CodiceRemoto(err) != codice || p.Riuscita || p.Codice != codice || p.Avvio != db.AvvioProvaAMano {
			t.Errorf("%s: %+v %v %v (%s)", nome, p, scritta, err, CodiceRemoto(err))
		}
		if dur := time.Since(inizio); dur > 10*time.Second {
			t.Errorf("%s: %v", nome, dur)
		}
		for _, vietato := range []string{"storage.invalid", "u1-sub1"} {
			if strings.Contains(p.Errore, vietato) {
				t.Errorf("%s: motivo con %q: %q", nome, vietato, p.Errore)
			}
		}
		nessunaTemporanea(t, sc.pn.p.dir)
		lucchettoLibero(t, sc.pn.p.dir)
		rimetti()
		return p
	}
	dopo := cattura.Add(time.Hour)

	if p := fallisce("vecchia", CodiceRemotoVecchia, cattura.Add(EtaMassimaCopiaProvata+time.Second)); p.Archivio != u.Archivio ||
		!strings.Contains(p.Errore, "8 giorni") {
		t.Errorf("vecchia: %+v", p)
	}
	fallisce("nel futuro", CodiceRemotoOrologio, cattura.Add(-time.Minute))
	if err := os.Rename(sulServer, sulServer+".via"); err != nil {
		t.Fatal(err)
	}
	fallisce("tolta dal server", CodiceRemotoAssente, dopo)
	if err := os.Rename(sulServer+".via", sulServer); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sulServer, append(slices.Clone(buono), 'x'), 0o600); err != nil {
		t.Fatal(err)
	}
	if p := fallisce("byte diversi", CodiceRemotoControllo, dopo); !strings.Contains(p.Errore, "sul server") {
		t.Errorf("byte diversi: si vede dalla dimensione sul server, prima del download: %q", p.Errore)
	}
	corrotto := slices.Clone(buono)
	corrotto[len(corrotto)/2] ^= 0xff
	if err := os.WriteFile(sulServer, corrotto, 0o600); err != nil {
		t.Fatal(err)
	}
	fallisce("corrotto sul server", CodiceRemotoControllo, dopo)
	sc.srv.Imposta(sftpfinto.Guasti{LetturaCadeDopo: 200})
	fallisce("download interrotto", CodiceRemotoScaricamento, dopo)
	sc.srv.Imposta(sftpfinto.Guasti{FermoAllaLettura: true})
	sc.prepara = func(r *Remoto) { r.TempoSenzaRisposte = 300 * time.Millisecond }
	if p := fallisce("server lento", CodiceRemotoRete, dopo); !strings.Contains(p.Errore, ErrTempoRemoto.Error()) {
		t.Errorf("server lento: %q", p.Errore)
	}
	sc.srv.Imposta(sftpfinto.Guasti{FermoAllaLettura: true})
	sc.prepara = func(r *Remoto) { r.TempoMassimo = 300 * time.Millisecond }
	fallisce("durata massima", CodiceRemotoRete, dopo)
	altro := sftpfinto.Nuovo(t)
	sc.prepara = func(r *Remoto) { r.Componi = altro.Componi }
	fallisce("impronta diversa", CodiceRemotoImpronta, dopo)
	sc.srv.Autorizza(altro.Chiave.PublicKey())
	fallisce("chiave rifiutata", CodiceRemotoAccesso, dopo)
	firma, _ := LeggiChiaveSFTP(sc.pn.p.dir)
	sc.srv.Autorizza(firma.PublicKey())
	vecchio := spazioLibero
	spazioLibero = func(string) (uint64, error) { return uint64(u.Byte) + MargineSpazio - 1, nil }
	if p := fallisce("spazio locale", CodiceRemotoSpazio, dopo); !strings.Contains(p.Errore, "spazio") {
		t.Errorf("spazio: %q", p.Errore)
	}
	spazioLibero = vecchio
	// Revisione, BASSA 3: spazio che finisce dopo il download, all'estrazione della verifica (ErrSpazio della 121).
	chiamate := 0
	spazioLibero = func(string) (uint64, error) {
		chiamate++
		if chiamate == 1 {
			return uint64(u.Byte) + MargineSpazio, nil
		}
		return 1, nil
	}
	if p := fallisce("spazio all'estrazione", CodiceRemotoSpazio, dopo); !strings.Contains(p.Errore, "estrarre") {
		t.Errorf("spazio all'estrazione: %q", p.Errore)
	}
	spazioLibero = vecchio
	// Disco pieno durante il download (ENOSPC).
	scrittoreScaricamentoPerTest = func(io.Writer) io.Writer {
		return scrittoreSenzaSpazio{&os.PathError{Op: "write", Path: "x", Err: syscall.ENOSPC}}
	}
	if p := fallisce("disco pieno nel download", CodiceRemotoSpazio, dopo); !strings.Contains(p.Errore, "disco pieno") {
		t.Errorf("disco pieno: %q", p.Errore)
	}
	scrittoreScaricamentoPerTest = nil
	// BASSA 2: un server che manda più byte di quelli dichiarati: il tetto ferma il download.
	sc.srv.Imposta(sftpfinto.Guasti{OltreLaFine: 4096})
	if p := fallisce("oltre la fine", CodiceRemotoControllo, dopo); !strings.Contains(p.Errore, ErrProvaRemotaTroppoGrande.Error()) {
		t.Errorf("oltre la fine: %q", p.Errore)
	}
	// BASSA 5: la verifica dà un altro archivio (SHA-256 o ora di cattura diversi dal registro): controllo dopo la verifica.
	sc.pn.n.verifica = func(ctx context.Context, dati, file string) (Verificato, error) {
		v, err := VerificaArchivio(ctx, dati, file)
		v.SHA256 = strings.Repeat("0", 64)
		return v, err
	}
	if p := fallisce("verificata diversa", CodiceRemotoControllo, dopo); !strings.Contains(p.Errore, "verificata è un'altra") {
		t.Errorf("verificata diversa: %q", p.Errore)
	}
	sc.pn.n.verifica = func(ctx context.Context, dati, file string) (Verificato, error) {
		v, err := VerificaArchivio(ctx, dati, file)
		v.CatturatoIl = "2001-01-01T00:00:00.000000Z"
		return v, err
	}
	if p := fallisce("cattura diversa", CodiceRemotoControllo, dopo); !strings.Contains(p.Errore, "ora di cattura") {
		t.Errorf("cattura diversa: %q", p.Errore)
	}
	sc.pn.n.verifica = nil

	// Lo stato: prova remota fallita, testo senza host, utente né cartella.
	sc.pn.o.Imposta(dopo)
	s, err := sc.pn.n.Stato(ctx)
	if err != nil || !slices.Contains(codici(s.Problemi()), ProblemaProvaRemotaFallita) {
		t.Fatalf("stato: %v %v", codici(s.Problemi()), err)
	}
	for _, pr := range s.Problemi() {
		if pr.Codice == ProblemaProvaRemotaFallita && (!strings.Contains(pr.Testo, "non è uguale") ||
			strings.Contains(pr.Testo, "storage.invalid") || strings.Contains(pr.Testo, cartellaRemota)) {
			t.Errorf("testo %q", pr.Testo)
		}
	}

	// Riuscita a mano (8 giorni esatti passano): il problema si spegne. Il file scaricato è 0600 (BASSA 4).
	sc.pn.o.Imposta(cattura.Add(EtaMassimaCopiaProvata))
	sc.pn.n.verifica = func(ctx context.Context, dati, file string) (Verificato, error) {
		if fi, err := os.Stat(file); err != nil || fi.Mode().Perm() != 0o600 {
			t.Errorf("file scaricato: %v %v", fi.Mode(), err)
		}
		return VerificaArchivio(ctx, dati, file)
	}
	if p, scritta, err := sc.aMano(t, ctx); !scritta || err != nil || !p.Riuscita {
		t.Fatalf("riuscita: %+v %v %v", p, scritta, err)
	}
	if s, err := sc.pn.n.Stato(ctx); err != nil || slices.Contains(codici(s.Problemi()), ProblemaProvaRemotaFallita) {
		t.Errorf("dopo la riuscita: %v %v", codici(s.Problemi()), err)
	}
	sc.pn.n.verifica = nil

	// Un archivio corrotto registrato così com'è (una copia remota con lo SHA-256 del file rovinato): il download e
	// il confronto passano, la verifica completa no.
	if err := os.WriteFile(sulServer, corrotto, 0o600); err != nil {
		t.Fatal(err)
	}
	finto := u
	finto.SHA256 = sha256Esadecimale(corrotto)
	if err := db.RegistraVerificaBackup(ctx, sc.d, db.VerificaBackup{Riuscita: true, Destinazione: db.DestinazioneSFTP,
		Archivio: u.Archivio, SHA256: finto.SHA256, Byte: u.Byte, CatturatoIl: u.CatturatoIl.Add(time.Second),
		VerificatoIl: dopo, Conteggi: map[string]int64{"buste": 1}}); err != nil {
		t.Fatal(err)
	}
	fallisce("verifica", CodiceRemotoVerifica, dopo)

	// L'ultima copia del registro sta in un'altra cartella (configurazione cambiata dopo): non si prova.
	if err := db.RegistraVerificaBackup(ctx, sc.d, db.VerificaBackup{Riuscita: true, Destinazione: db.DestinazioneSFTP,
		Archivio: "/altra/" + path.Base(u.Archivio), SHA256: u.SHA256, Byte: u.Byte, CatturatoIl: u.CatturatoIl.Add(2 * time.Second),
		VerificatoIl: dopo, Conteggi: map[string]int64{"buste": 1}}); err != nil {
		t.Fatal(err)
	}
	fallisce("copia in un'altra cartella", CodiceRemotoNessuna, dopo)
	// BASSA 4: una cartella che comincia come quella configurata, e un nome che non è di un archivio del programma.
	for i, a := range []string{cartellaRemota + "2/" + path.Base(u.Archivio), cartellaRemota + "/altro.tar.age"} {
		if err := db.RegistraVerificaBackup(ctx, sc.d, db.VerificaBackup{Riuscita: true, Destinazione: db.DestinazioneSFTP,
			Archivio: a, SHA256: u.SHA256, Byte: u.Byte, CatturatoIl: u.CatturatoIl.Add(time.Duration(3+i) * time.Second),
			VerificatoIl: dopo, Conteggi: map[string]int64{"buste": 1}}); err != nil {
			t.Fatal(err)
		}
		fallisce(a, CodiceRemotoNessuna, dopo)
	}
}

// scrittoreSenzaSpazio dà sempre l'errore err.
type scrittoreSenzaSpazio struct{ err error }

func (s scrittoreSenzaSpazio) Write([]byte) (int, error) { return 0, s.err }

// Revisione della 129c, MEDIA 1: il mese fallito si riprova la notte dopo (dopo il backup) e, riuscito, il problema si
// chiude; i tre tentativi della notte restano tre anche dopo un riavvio.
func TestProvaRemotaFallitaSiRifaLaNotteDopo(t *testing.T) {
	cattura := alle(2026, 12, 6, 2, 30)
	sc := nuovaScenaRemota(t, cattura)
	ctx := context.Background()
	sc.backupConCopia(t, cattura)
	rigaProva(t, sc.d, db.AvvioProvaAutomatico, "2026-12-06", true, cattura.Add(5*time.Minute))
	sc.srv.Imposta(sftpfinto.Guasti{LetturaCadeDopo: 200})
	sc.pn.o.Imposta(cattura.Add(10 * time.Minute))
	for range 4 {
		_, _, _ = sc.pn.n.GiroProvaRemota(ctx)
		sc.pn.o.Sposta(AttesaFraTentativi)
	}
	if v := strings.Join(righeProveRemote(t, sc.d), ","); v != strings.Repeat("automatico 2026-12 fallita scaricamento,", 2)+"automatico 2026-12 fallita scaricamento" {
		t.Fatalf("domenica: %s", v)
	}
	// Riavvio: un Notturno nuovo non rifà la notte.
	nuovo := NuovoNotturno(sc.d, nil, sc.pn.s.chiavetta, sc.pn.o)
	nuovo.preparaRemoto = sc.preparaRemoto
	if _, fatta, err := nuovo.GiroProvaRemota(ctx); fatta || err != nil {
		t.Errorf("dopo il riavvio: %v %v", fatta, err)
	}
	s, err := sc.pn.n.Stato(ctx)
	if err != nil || !slices.Contains(codici(s.Problemi()), ProblemaProvaRemotaFallita) {
		t.Fatalf("stato: %v %v", codici(s.Problemi()), err)
	}
	for _, pr := range s.Problemi() {
		if pr.Codice == ProblemaProvaRemotaFallita && (strings.Contains(pr.Testo, "tre volte") || !strings.Contains(pr.Testo, "notte dopo")) {
			t.Errorf("testo %q", pr.Testo)
		}
	}
	// Lunedì: il server è tornato buono; dopo il backup (con la sua copia) la prova si rifà e riesce.
	sc.srv.Imposta(sftpfinto.Guasti{})
	lunedi := alle(2026, 12, 7, 2, 30)
	sc.backupConCopia(t, lunedi)
	sc.pn.o.Imposta(lunedi.Add(10 * time.Minute))
	p, fatta, err := sc.pn.n.GiroProvaRemota(ctx)
	if !fatta || err != nil || !p.Riuscita || !p.CatturatoIl.Equal(lunedi) {
		t.Fatalf("lunedì: %+v %v %v", p, fatta, err)
	}
	if s, err := sc.pn.n.Stato(ctx); err != nil || len(s.Problemi()) != 0 {
		t.Errorf("problemi dopo la riuscita: %v %v", codici(s.Problemi()), err)
	}
	// Martedì il mese è fatto.
	sc.backupConCopia(t, alle(2026, 12, 8, 2, 30))
	if _, fatta, err := sc.pn.n.GiroProvaRemota(ctx); fatta || err != nil {
		t.Errorf("martedì: %v %v", fatta, err)
	}
}

func sha256Esadecimale(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// Lo stop durante il download (prima lettura) e durante la verifica: nessuna riga, cartella temporanea tolta,
// lucchetto libero, il mese resta da fare. Un panico nella verifica: prova fallita con il dettaglio fisso, cartella
// tolta. Con il lucchetto occupato non fa niente. Una riga che non si scrive si conta in memoria. Una cartella del
// download rimasta da un blackout si toglie alla prova dopo.
func TestProvaRemotaStopPanicoLucchetto(t *testing.T) {
	cattura := alle(2026, 12, 6, 2, 30)
	sc := nuovaScenaRemota(t, cattura)
	sc.backupConCopia(t, cattura)
	rigaProva(t, sc.d, db.AvvioProvaAutomatico, "2026-12-06", true, cattura.Add(6*time.Minute))
	sc.pn.o.Imposta(cattura.Add(10 * time.Minute))
	n := sc.pn.n

	// Stop alla prima lettura del download: il server resta fermo (FermoAllaLettura) finché il client non chiude, e
	// senza risposte il sorvegliante aspetterebbe 2 minuti: se torna subito è lo stop.
	ctx, ferma := context.WithCancel(context.Background())
	sc.srv.Imposta(sftpfinto.Guasti{AllaPrimaLettura: ferma, FermoAllaLettura: true})
	inizio := time.Now()
	if _, fatta, err := n.GiroProvaRemota(ctx); fatta || !errors.Is(err, context.Canceled) {
		t.Errorf("stop nel download: %v %v", fatta, err)
	}
	if time.Since(inizio) > 10*time.Second {
		t.Error("stop lento")
	}
	sc.srv.Imposta(sftpfinto.Guasti{})
	// Stop durante la verifica.
	ctx, ferma = context.WithCancel(context.Background())
	n.verifica = func(c context.Context, dati, file string) (Verificato, error) {
		if m, _ := filepath.Glob(filepath.Join(dati, prefissoScaricamento+"*", "*")); len(m) != 1 {
			t.Errorf("file scaricato: %v", m)
		}
		ferma()
		return Verificato{}, c.Err()
	}
	if _, fatta, err := n.GiroProvaRemota(ctx); fatta || !errors.Is(err, context.Canceled) {
		t.Errorf("stop nella verifica: %v %v", fatta, err)
	}
	if v := righeProveRemote(t, sc.d); len(v) != 0 {
		t.Errorf("righe dopo lo stop: %v", v)
	}
	nessunaTemporanea(t, sc.pn.p.dir)
	lucchettoLibero(t, sc.pn.p.dir)
	if ok, err := n.ProvaRemotaDovuta(context.Background()); !ok || err != nil {
		t.Errorf("mese dopo lo stop: %v %v", ok, err)
	}
	// Con il lucchetto preso: niente.
	l, err := PrendiLucchetto(sc.pn.p.dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, fatta, err := n.GiroProvaRemota(context.Background()); fatta || err != nil {
		t.Errorf("con il lucchetto preso: %v %v", fatta, err)
	}
	_ = l.Rilascia()
	// Panico nella verifica, con una cartella del download rimasta da un blackout: tolta all'inizio.
	resto := filepath.Join(sc.pn.p.dir, prefissoScaricamento+"12345")
	if err := os.Mkdir(resto, 0o700); err != nil {
		t.Fatal(err)
	}
	n.verifica = func(context.Context, string, string) (Verificato, error) { panic("guasto inventato") }
	p, fatta, err := n.GiroProvaRemota(context.Background())
	if !fatta || err == nil || p.Riuscita || p.Errore != DettaglioPanicoProvaRemota || p.Codice != CodiceRemotoAltro {
		t.Errorf("panico: %+v %v %v", p, fatta, err)
	}
	nessunaTemporanea(t, sc.pn.p.dir)
	lucchettoLibero(t, sc.pn.p.dir)
	// Righe che non si scrivono: contate in memoria, al più tre tentativi.
	n.verifica = func(context.Context, string, string) (Verificato, error) {
		return Verificato{}, errors.New("rotto inventato")
	}
	scritture := 0
	n.inserisciProvaRemota = func(context.Context, *db.DB, db.ProvaRemota) (int64, error) {
		scritture++
		return 0, errors.New("database occupato inventato")
	}
	for range 6 {
		sc.pn.o.Sposta(AttesaFraTentativi)
		_, _, _ = n.GiroProvaRemota(context.Background())
	}
	if scritture != 2 {
		t.Errorf("scritture %d, attese 2 (una riga scritta + 2 in memoria)", scritture)
	}
}

// I problemi della prova remota nello stato: fallita (testo per ogni codice, senza host né cartella) e mancante
// (36 giorni dall'ultima prova, o dalla configurazione senza prove: 36 giorni esatti tacciono); niente senza copia
// remota configurata; con il lavoro spento «mancante» tace, «fallita» suona; titoli degli avvisi; la copia spenta
// chiude in silenzio i due problemi aperti.
func TestProblemiProvaRemota(t *testing.T) {
	adesso := alle(2027, 1, 20, 12, 0)
	base := statoBuono()
	base.Adesso, base.Remoto, base.RemotoDal = adesso, true, adesso.Add(-10*24*time.Hour)
	base.CeCopiaBuona, base.CopiaBuona = true, db.CopiaRemota{IniziatoIl: adesso.Add(-time.Hour)}
	base.UltimoBackup.CatturatoIl, base.Prova.FinitoIl = adesso.Add(-time.Hour), adesso.Add(-time.Hour)
	if v := codici(base.Problemi()); len(v) != 0 {
		t.Fatalf("base: %v", v)
	}
	con := func(f func(*Stato)) Stato {
		s := base
		f(&s)
		return s
	}
	for _, c := range []struct {
		nome   string
		s      Stato
		attesi []string
	}{
		{"configurata da 36 giorni esatti", con(func(s *Stato) { s.RemotoDal = adesso.Add(-EtaMassimaProvaRemota) }), nil},
		{"configurata da più di 36 giorni", con(func(s *Stato) { s.RemotoDal = adesso.Add(-EtaMassimaProvaRemota - time.Second) }),
			[]string{ProblemaProvaRemotaMancante}},
		{"ultima prova di 36 giorni", con(func(s *Stato) {
			s.RemotoDal, s.CeProvaRemota = adesso.Add(-400*24*time.Hour), true
			s.ProvaRemota = db.ProvaRemota{Riuscita: true, FinitoIl: adesso.Add(-EtaMassimaProvaRemota)}
		}), nil},
		{"ultima prova di più di 36 giorni", con(func(s *Stato) {
			s.CeProvaRemota = true
			s.ProvaRemota = db.ProvaRemota{Riuscita: true, FinitoIl: adesso.Add(-EtaMassimaProvaRemota - time.Second)}
		}), []string{ProblemaProvaRemotaMancante}},
		{"fallita", con(func(s *Stato) {
			s.CeProvaRemota, s.ProvaRemota = true, db.ProvaRemota{Codice: CodiceRemotoVerifica, FinitoIl: adesso.Add(-time.Hour)}
		}), []string{ProblemaProvaRemotaFallita}},
		{"non configurata", con(func(s *Stato) {
			s.Remoto, s.CeProvaRemota, s.ProvaRemota = false, true, db.ProvaRemota{Codice: CodiceRemotoRete, FinitoIl: adesso}
		}), nil},
	} {
		if v := codici(c.s.Problemi()); !slices.Equal(v, c.attesi) {
			t.Errorf("%s: %v, attesi %v", c.nome, v, c.attesi)
		}
	}
	// Lavoro spento: «mancante» tace, «fallita» suona.
	spento := con(func(s *Stato) { s.RemotoDal, s.LavoroSpento = adesso.Add(-400*24*time.Hour), true })
	if v := codici(spento.Allarme()); len(v) != 0 {
		t.Errorf("mancante con il lavoro spento: %v", v)
	}
	spento.CeProvaRemota, spento.ProvaRemota = true, db.ProvaRemota{Codice: CodiceRemotoControllo, FinitoIl: adesso}
	if v := codici(spento.Allarme()); !slices.Equal(v, []string{ProblemaProvaRemotaFallita}) {
		t.Errorf("fallita con il lavoro spento: %v", v)
	}
	// Un testo per ogni codice, mai muto, mai con l'host o la cartella; quelli conosciuti diversi dal ripiego.
	ripiego := testoProvaRemota("sconosciuto", adesso)
	for _, k := range db.CodiciProvaRemota {
		tt := testoProvaRemota(k, adesso)
		if k != CodiceRemotoAltro && tt == ripiego {
			t.Errorf("%s: testo di ripiego", k)
		}
		if !strings.Contains(tt, "non è riuscita: ") || strings.Contains(tt, "storage") || strings.Contains(reData.ReplaceAllString(tt, ""), "/") {
			t.Errorf("%s: %q", k, tt)
		}
	}
	for _, k := range []string{ProblemaProvaRemotaFallita, ProblemaProvaRemotaMancante} {
		if titoloAvviso(k) == titoloAvviso("sconosciuto-inventato") || ordineCodice(k)[:2] == "99" {
			t.Errorf("%s: titolo %q, ordine %q", k, titoloAvviso(k), ordineCodice(k))
		}
	}
	prima := statoAvvisi{Aperti: map[string]time.Time{ProblemaProvaRemotaFallita: adesso, ProblemaProvaRemotaMancante: adesso,
		ProblemaChiavetta: adesso}, Assenti: map[string]time.Time{}}
	if !chiudiRemotoSpento(&prima, Stato{}) || len(prima.Aperti) != 1 {
		t.Errorf("copia spenta: %v", prima.Aperti)
	}
}

// cambiaCartellaRemota sposta sul server finto la cartella dei backup da prima a dopo (come se la stessa cartella si
// vedesse da un'altra porta) e riscrive la configurazione con dopo.
func (sc *scenaRemota) cambiaCartellaRemota(t *testing.T, prima, dopo string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(sc.srv.Locale(dopo)), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(sc.srv.Locale(prima), sc.srv.Locale(dopo)); err != nil {
		t.Fatal(err)
	}
	sc.scriviCartella(t, dopo)
}

func (sc *scenaRemota) scriviCartella(t *testing.T, cartella string) {
	t.Helper()
	cfg, _, ok, err := LeggiConfigRemota(context.Background(), sc.d.Lettura)
	if err != nil || !ok {
		t.Fatalf("configurazione: %v %v", ok, err)
	}
	cfg.Cartella = cartella
	if err := ScriviConfigRemota(context.Background(), sc.d, cfg, sc.pn.o.Adesso()); err != nil {
		t.Fatal(err)
	}
}

// Slice 133c, voce 19 (deciso da Stefano il 27/11/2026): la prova mensile riconosce l'ultima copia caricata dalla
// porta 22 («/backup/…») con la cartella della 23 («/home/backup»), e viceversa, e la scarica dalla cartella
// configurata; la riga della prova tiene il percorso del registro. Una cartella davvero diversa («/home/altra»,
// «/home/backup/sub», «/homebackup»), anche con lo stesso file dentro, non la riconosce:
// «nessuna».
func TestProvaRemotaDopoIlCambioDiPorta(t *testing.T) {
	cattura := alle(2026, 12, 6, 2, 30)
	ctx := context.Background()
	sc := nuovaScenaRemota(t, cattura)
	u := sc.backupConCopia(t, cattura)
	if path.Dir(u.Archivio) != "/backup" {
		t.Fatalf("copia in %s", u.Archivio)
	}
	riesce := func(nome string, u db.ArchivioRegistrato) {
		t.Helper()
		p, scritta, err := sc.aMano(t, ctx)
		if err != nil || !scritta || !p.Riuscita || p.Archivio != u.Archivio || p.SHA256 != u.SHA256 || p.Byte != u.Byte {
			t.Errorf("%s: %+v %v %v", nome, p, scritta, err)
		}
		nessunaTemporanea(t, sc.pn.p.dir)
	}
	sc.pn.o.Imposta(cattura.Add(time.Hour))
	sc.cambiaCartellaRemota(t, "/backup", "/home/backup")
	riesce("dalla 22 alla 23", u)

	buono, err := os.ReadFile(sc.srv.Locale(path.Join("/home/backup", path.Base(u.Archivio))))
	if err != nil {
		t.Fatal(err)
	}
	// («/home/home/backup» non si può più configurare: cartellaAmbigua, revisione della 133c.)
	for _, diversa := range []string{"/home/altra", "/home/backup/sub", "/homebackup"} {
		if err := os.MkdirAll(sc.srv.Locale(diversa), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(sc.srv.Locale(path.Join(diversa, path.Base(u.Archivio))), buono, 0o600); err != nil {
			t.Fatal(err)
		}
		sc.scriviCartella(t, diversa)
		p, scritta, err := sc.aMano(t, ctx)
		if !scritta || CodiceRemoto(err) != CodiceRemotoNessuna || p.Riuscita || p.Archivio != "" {
			t.Errorf("%s: %+v %v %v", diversa, p, scritta, err)
		}
	}

	// Viceversa: una copia caricata dalla 23 («/home/backup/…»), riconosciuta dalla 22 («/backup»).
	sc.scriviCartella(t, "/home/backup")
	u2 := sc.backupConCopia(t, cattura.Add(24*time.Hour))
	if path.Dir(u2.Archivio) != "/home/backup" {
		t.Fatalf("copia in %s", u2.Archivio)
	}
	sc.pn.o.Imposta(cattura.Add(25 * time.Hour))
	sc.cambiaCartellaRemota(t, "/home/backup", "/backup")
	riesce("dalla 23 alla 22", u2)
}
