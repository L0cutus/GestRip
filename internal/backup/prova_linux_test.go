package backup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
)

// La prova di ripristino settimanale (slice 125, risposta G, D-15) e i media rovinati (D-116). Orologio finto,
// database e media inventati, chiavetta finta della 123; backup, verifica, registro e lucchetto veri dove non si
// dice altro. Il 29/11/2026 e il 06/12/2026 sono domeniche.

// rigaProva scrive a mano una riga del registro delle prove (per il calendario, senza prove vere).
func rigaProva(t *testing.T, d *db.DB, avvio, settimana string, riuscita bool, fine time.Time) {
	t.Helper()
	g, err := time.Parse(time.DateOnly, settimana)
	if err != nil {
		t.Fatal(err)
	}
	p := db.ProvaRipristino{Avvio: avvio, Settimana: clock.Data{Anno: g.Year(), Mese: g.Month(), Giorno: g.Day()},
		IniziatoIl: fine.Add(-time.Minute), FinitoIl: fine, Riuscita: riuscita, Errore: "motivo inventato"}
	if riuscita {
		p.Errore, p.Archivio, p.SHA256, p.CatturatoIl = "", "/chiavetta/a.tar.age", strings.Repeat("a", 64), fine.Add(-time.Hour)
	}
	if _, err := db.RegistraProvaRipristino(context.Background(), d, p); err != nil {
		t.Fatal(err)
	}
}

func provaDovutaAlle(t *testing.T, n *Notturno, o *clock.Finto, at time.Time) bool {
	t.Helper()
	o.Imposta(at)
	v, err := n.ProvaDovuta(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// righeProve dà le righe del registro delle prove, dalla prima: avvio, settimana, esito.
func righeProve(t *testing.T, d *db.DB) []string {
	t.Helper()
	rr, err := d.Lettura.QueryContext(context.Background(), `SELECT avvio, settimana, esito FROM prove_ripristino ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rr.Close() }()
	var v []string
	for rr.Next() {
		var a, s, e string
		if err := rr.Scan(&a, &s, &e); err != nil {
			t.Fatal(err)
		}
		v = append(v, a+" "+s+" "+e)
	}
	return v
}

func lucchettoLibero(t *testing.T, dati string) {
	t.Helper()
	l, err := PrendiLucchetto(dati)
	if err != nil {
		t.Fatalf("lucchetto: %v", err)
	}
	_ = l.Rilascia()
}

// La settimana di un turno è la sua domenica: la domenica stessa, poi fino al sabato la domenica prima (anche a
// cavallo dell'anno e del cambio dell'ora).
func TestSettimanaDi(t *testing.T) {
	for turno, attesa := range map[clock.Data]string{
		{Anno: 2026, Mese: 11, Giorno: 29}: "2026-11-29", // domenica
		{Anno: 2026, Mese: 11, Giorno: 30}: "2026-11-29", // lunedì
		{Anno: 2026, Mese: 12, Giorno: 5}:  "2026-11-29", // sabato
		{Anno: 2026, Mese: 12, Giorno: 6}:  "2026-12-06",
		{Anno: 2027, Mese: 1, Giorno: 1}:   "2026-12-27", // venerdì
		{Anno: 2026, Mese: 3, Giorno: 29}:  "2026-03-29", // domenica del salto dell'ora
		{Anno: 2026, Mese: 10, Giorno: 31}: "2026-10-25",
	} {
		if g := SettimanaDi(turno).String(); g != attesa {
			t.Errorf("%s: %s, attesa %s", turno, g, attesa)
		}
	}
}

// Voce OPS-28, in parte: niente segno «inventario:» (la voce chiude con l'allarme, 126-127, e il fuori sede, 128).
// Il calendario della prova, con le righe scritte a mano: la domenica alle 02:30 la prova aspetta il backup della
// domenica; fatto il backup (o finiti i suoi tre tentativi) è dovuta; fallita si riprova dopo 30 minuti, al più
// tre volte; riuscita, la settimana è fatta fino alla domenica dopo. Le prove a mano non contano. Con
// l'apparecchio spento la domenica la prova si fa al primo giro utile della settimana, dopo il backup di
// recupero. Senza chiavetta mai.
func TestProvaDovuta(t *testing.T) {
	pn := nuovaProvaNotturno(t, alle(2026, 11, 29, 2, 0))
	d, n, o := pn.p.s.DB, pn.n, pn.o
	// Sabato fatto, settimana del 22/11 fatta.
	riga(t, d, AvvioAutomatico, "2026-11-28", EsitoOK, alle(2026, 11, 28, 2, 35))
	rigaProva(t, d, db.AvvioProvaAutomatico, "2026-11-22", true, alle(2026, 11, 22, 2, 40))
	if provaDovutaAlle(t, n, o, alle(2026, 11, 29, 2, 29)) {
		t.Error("domenica 02:29: dovuta")
	}
	if provaDovutaAlle(t, n, o, alle(2026, 11, 29, 2, 30)) {
		t.Error("domenica 02:30, prima del backup: dovuta")
	}
	riga(t, d, AvvioAutomatico, "2026-11-29", EsitoOK, alle(2026, 11, 29, 2, 35))
	if !provaDovutaAlle(t, n, o, alle(2026, 11, 29, 2, 35)) {
		t.Error("dopo il backup della domenica: non dovuta")
	}
	// A mano non conta.
	rigaProva(t, d, db.AvvioProvaAMano, "2026-11-29", true, alle(2026, 11, 29, 2, 36))
	if !provaDovutaAlle(t, n, o, alle(2026, 11, 29, 2, 37)) {
		t.Error("dopo una prova a mano: non dovuta")
	}
	// Fallita: 30 minuti, tre volte.
	rigaProva(t, d, db.AvvioProvaAutomatico, "2026-11-29", false, alle(2026, 11, 29, 2, 40))
	if provaDovutaAlle(t, n, o, alle(2026, 11, 29, 3, 9)) {
		t.Error("29 minuti dopo una prova fallita: dovuta")
	}
	if !provaDovutaAlle(t, n, o, alle(2026, 11, 29, 3, 10)) {
		t.Error("30 minuti dopo una prova fallita: non dovuta")
	}
	rigaProva(t, d, db.AvvioProvaAutomatico, "2026-11-29", false, alle(2026, 11, 29, 3, 10))
	rigaProva(t, d, db.AvvioProvaAutomatico, "2026-11-29", false, alle(2026, 11, 29, 3, 40))
	if provaDovutaAlle(t, n, o, alle(2026, 11, 29, 12, 0)) {
		t.Error("tre prove fallite: dovuta")
	}
	riga(t, d, AvvioAutomatico, "2026-12-02", EsitoOK, alle(2026, 12, 2, 2, 35))
	if provaDovutaAlle(t, n, o, alle(2026, 12, 2, 12, 0)) {
		t.Error("mercoledì dopo tre prove fallite: dovuta")
	}
	// La domenica dopo, dopo il backup: di nuovo dovuta; riuscita, la settimana è fatta.
	if provaDovutaAlle(t, n, o, alle(2026, 12, 6, 2, 30)) {
		t.Error("domenica dopo, prima del backup: dovuta")
	}
	riga(t, d, AvvioAutomatico, "2026-12-06", EsitoOK, alle(2026, 12, 6, 2, 35))
	if !provaDovutaAlle(t, n, o, alle(2026, 12, 6, 2, 36)) {
		t.Error("domenica dopo, dopo il backup: non dovuta")
	}
	rigaProva(t, d, db.AvvioProvaAutomatico, "2026-12-06", true, alle(2026, 12, 6, 2, 40))
	riga(t, d, AvvioAutomatico, "2026-12-07", EsitoOK, alle(2026, 12, 7, 2, 35))
	if provaDovutaAlle(t, n, o, alle(2026, 12, 7, 12, 0)) {
		t.Error("lunedì dopo una prova riuscita: dovuta")
	}
	// Spento la domenica 13/12 e il lunedì; acceso martedì alle 15:00: prima il backup del martedì, poi la prova
	// della settimana del 13/12.
	if provaDovutaAlle(t, n, o, alle(2026, 12, 15, 15, 0)) {
		t.Error("martedì, prima del backup di recupero: dovuta")
	}
	riga(t, d, AvvioAutomatico, "2026-12-15", EsitoOK, alle(2026, 12, 15, 15, 10))
	if !provaDovutaAlle(t, n, o, alle(2026, 12, 15, 15, 11)) {
		t.Error("martedì, dopo il backup di recupero: non dovuta")
	}
	// Un backup che non riesce per tre volte chiude il turno: la prova si fa lo stesso (e dirà se l'ultimo
	// archivio è vecchio).
	for i, h := range []int{2, 3, 4} {
		riga(t, d, AvvioAutomatico, "2026-12-20", EsitoFallito, alle(2026, 12, 20, h, 30))
		if v := provaDovutaAlle(t, n, o, alle(2026, 12, 20, h, 31)); v != (i == 2) {
			t.Errorf("dopo %d backup falliti: dovuta %v", i+1, v)
		}
	}
	// Senza chiavetta mai.
	senza := NuovoNotturno(d, pn.p.s.Archivio, "", o)
	if provaDovutaAlle(t, senza, o, alle(2026, 12, 20, 12, 0)) {
		t.Error("senza chiavetta: dovuta")
	}
}

// Voce OPS-28, in parte (vedi TestProvaDovuta).
// Il lavoro notturno della domenica: il backup delle 02:30 e, nello stesso giro, la prova sull'archivio appena
// scritto; riga «ok» con archivio, SHA-256 e ora di cattura del registro; nessuna cartella temporanea resta, il
// lucchetto è libero, l'allarme è spento. Ai giri dopo niente.
func TestProvaDopoIlBackupDellaDomenica(t *testing.T) {
	pn := nuovaProvaNotturno(t, alle(2026, 11, 29, 2, 20))
	d := pn.p.s.DB
	riga(t, d, AvvioAutomatico, "2026-11-28", EsitoOK, alle(2026, 11, 28, 2, 35))
	a := &aspettaFinta{giri: 3}
	// L'attesa dopo l'avvio porta l'orologio alle 02:30.
	pn.n.Lavoro(context.Background(), func(ctx context.Context, dur time.Duration) bool {
		if dur == AttesaDopoAvvio {
			pn.o.Sposta(dur)
		}
		return a.aspetta(ctx, dur)
	})
	if v := strings.Join(pn.righeStorico(t), ","); v != "automatico 2026-11-28 ok,automatico 2026-11-29 ok" {
		t.Errorf("storico %s", v)
	}
	if v := strings.Join(righeProve(t, d), ","); v != "automatico 2026-11-29 ok" {
		t.Errorf("prove %s", v)
	}
	ctx := context.Background()
	p, ok, err := db.UltimaProvaRipristino(ctx, d.Lettura)
	u, _, _ := db.UltimoVerificatoSullaChiavetta(ctx, d.Lettura)
	if err != nil || !ok || !p.Riuscita || p.Archivio != u.Archivio || p.SHA256 != u.SHA256 || !p.CatturatoIl.Equal(alle(2026, 11, 29, 2, 30)) ||
		p.MediaRovinati != 0 || filepath.Dir(p.Archivio) != pn.s.chiavetta {
		t.Errorf("prova %+v %v %v, registro %+v", p, ok, err, u)
	}
	if _, fallita, err := db.ProvaSettimanaleFallita(ctx, d.Lettura); fallita || err != nil {
		t.Errorf("allarme: %v %v", fallita, err)
	}
	if m, _ := filepath.Glob(filepath.Join(pn.p.dir, ".*backup-*")); len(m) != 0 {
		t.Errorf("cartelle temporanee rimaste: %v", m)
	}
	lucchettoLibero(t, pn.p.dir)
	pn.o.Sposta(time.Hour)
	if _, fatta, err := pn.n.GiroProva(ctx); fatta || err != nil {
		t.Errorf("un'ora dopo: %v %v", fatta, err)
	}
}

// fattoBackup fa un backup vero all'ora at (cattura at) e chiude il suo turno.
func fattoBackup(t *testing.T, pn *provaNotturno, at time.Time) Esecuzione {
	t.Helper()
	pn.o.Imposta(at)
	e, fatto, err := pn.n.Giro(context.Background())
	if err != nil || !fatto || e.Esito != EsitoOK {
		t.Fatalf("backup: %+v %v %v", e, fatto, err)
	}
	return e
}

// Voce OPS-28, in parte (vedi TestProvaDovuta).
// Risposta G: la prova fallisce se l'ultimo archivio verificato sulla chiavetta ha più di 8 giorni (otto giorni
// esatti passano), anche se l'archivio è buono; poi l'allarme è acceso. Una prova a mano riuscita lo spegne. Con
// la cattura nel futuro (orologio tornato indietro) fallisce.
func TestProvaArchivioVecchio(t *testing.T) {
	pn := nuovaProvaNotturno(t, alle(2026, 11, 21, 2, 30))
	d := pn.p.s.DB
	ctx := context.Background()
	fattoBackup(t, pn, alle(2026, 11, 21, 2, 30)) // sabato
	// I backup dopo non sono riusciti (chiavetta piena, per esempio): le righe dicono i turni chiusi.
	riga(t, d, AvvioAutomatico, "2026-11-29", EsitoFallito, alle(2026, 11, 29, 2, 31))
	riga(t, d, AvvioAutomatico, "2026-11-29", EsitoFallito, alle(2026, 11, 29, 3, 1))
	riga(t, d, AvvioAutomatico, "2026-11-29", EsitoFallito, alle(2026, 11, 29, 3, 31))
	pn.o.Imposta(alle(2026, 11, 29, 3, 32)) // 8 giorni, 1 ora e 2 minuti dopo
	p, fatta, err := pn.n.GiroProva(ctx)
	if !fatta || !errors.Is(err, ErrProvaVecchio) || p.Riuscita || !strings.Contains(p.Errore, "più di 8 giorni") || p.Archivio == "" {
		t.Fatalf("prova: %+v %v %v", p, fatta, err)
	}
	f, fallita, err := db.ProvaSettimanaleFallita(ctx, d.Lettura)
	if !fallita || err != nil || f.ID != p.ID || f.Errore != p.Errore {
		t.Errorf("allarme: %+v %v %v", f, fallita, err)
	}
	l, err := PrendiLucchetto(pn.p.dir)
	if err != nil {
		t.Fatal(err)
	}
	// Otto giorni esatti: passa (a mano, conta per l'allarme e non per il calendario).
	pn.o.Imposta(alle(2026, 11, 21, 2, 30).Add(EtaMassimaProva))
	var righe strings.Builder
	p, fatta, err = pn.n.ProvaAMano(ctx, l, func(r string) error { righe.WriteString(r); return nil })
	_ = l.Rilascia()
	if !fatta || err != nil || !p.Riuscita || p.Avvio != db.AvvioProvaAMano || p.Settimana.String() != "2026-11-29" {
		t.Fatalf("otto giorni esatti: %+v %v %v", p, fatta, err)
	}
	if !strings.HasPrefix(righe.String(), "prova di ripristino di "+p.Archivio+"\ncatturato il: 2026-11-21T01:30:00.000000Z\nverificato: decifrato") {
		t.Errorf("righe:\n%s", righe.String())
	}
	if _, fallita, err := db.ProvaSettimanaleFallita(ctx, d.Lettura); fallita || err != nil {
		t.Errorf("allarme dopo la prova a mano: %v %v", fallita, err)
	}
	// Un secondo in più: fallisce.
	l, _ = PrendiLucchetto(pn.p.dir)
	pn.o.Imposta(alle(2026, 11, 21, 2, 30).Add(EtaMassimaProva + time.Second))
	if _, _, err := pn.n.ProvaAMano(ctx, l, nil); !errors.Is(err, ErrProvaVecchio) {
		t.Errorf("otto giorni e un secondo: %v", err)
	}
	// Cattura nel futuro.
	pn.o.Imposta(alle(2026, 11, 21, 2, 29))
	if _, _, err := pn.n.ProvaAMano(ctx, l, nil); !errors.Is(err, db.ErrOrologioBackup) {
		t.Errorf("cattura nel futuro: %v", err)
	}
	_ = l.Rilascia()
	if v := strings.Join(righeProve(t, d), ","); v != "automatico 2026-11-29 fallita,a_mano 2026-11-29 ok,a_mano 2026-11-29 fallita,a_mano 2026-11-15 fallita" {
		t.Errorf("prove %s", v)
	}
}

// Voce OPS-28, in parte (vedi TestProvaDovuta).
// La prova fallisce, con una riga e il motivo, senza un backup verificato nel registro, con la chiavetta non
// montata, con l'archivio tolto o cambiato sulla chiavetta, con una chiave dei backup diversa, e con l'ultimo
// backup verificato registrato fuori dalla chiavetta configurata.
func TestProvaFallisce(t *testing.T) {
	ctx := context.Background()
	prova := func(t *testing.T, pn *provaNotturno) (db.ProvaRipristino, error) {
		t.Helper()
		l, err := PrendiLucchetto(pn.p.dir)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = l.Rilascia() }()
		p, fatta, err := pn.n.ProvaAMano(ctx, l, nil)
		if !fatta || p.Riuscita || p.Errore == "" {
			t.Fatalf("prova: %+v %v %v", p, fatta, err)
		}
		return p, err
	}
	t.Run("senza backup", func(t *testing.T) {
		pn := nuovaProvaNotturno(t, alle(2026, 11, 29, 3, 0))
		if p, err := prova(t, pn); !errors.Is(err, ErrProvaSenzaBackup) || p.Archivio != "" {
			t.Errorf("%+v %v", p, err)
		}
	})
	t.Run("chiavetta non montata", func(t *testing.T) {
		pn := nuovaProvaNotturno(t, alle(2026, 11, 29, 2, 30))
		fattoBackup(t, pn, alle(2026, 11, 29, 2, 30))
		pn.s.sis.info = rigaRadice()
		if _, err := prova(t, pn); !errors.Is(err, ErrNonMontata) {
			t.Errorf("%v", err)
		}
	})
	t.Run("archivio tolto", func(t *testing.T) {
		pn := nuovaProvaNotturno(t, alle(2026, 11, 29, 2, 30))
		e := fattoBackup(t, pn, alle(2026, 11, 29, 2, 30))
		if err := os.Remove(e.Archivio); err != nil {
			t.Fatal(err)
		}
		if _, err := prova(t, pn); !errors.Is(err, ErrChiavettaCambiata) {
			t.Errorf("%v", err)
		}
	})
	t.Run("archivio cambiato", func(t *testing.T) {
		pn := nuovaProvaNotturno(t, alle(2026, 11, 29, 2, 30))
		e := fattoBackup(t, pn, alle(2026, 11, 29, 2, 30))
		b, err := os.ReadFile(e.Archivio)
		if err != nil {
			t.Fatal(err)
		}
		b[len(b)/2] ^= 1
		if err := os.WriteFile(e.Archivio, b, 0o600); err != nil {
			t.Fatal(err)
		}
		if p, err := prova(t, pn); err == nil || p.Archivio != e.Archivio || p.MediaRovinati != 0 {
			t.Errorf("%+v %v", p, err)
		}
	})
	t.Run("altro archivio con lo stesso nome", func(t *testing.T) {
		// Un archivio buono di un altro momento al posto di quello registrato: la verifica passa, ma SHA-256 e byte
		// non sono quelli del registro.
		pn := nuovaProvaNotturno(t, alle(2026, 11, 29, 2, 30))
		e := fattoBackup(t, pn, alle(2026, 11, 29, 2, 30))
		vero := pn.n.verifica
		pn.n.verifica = func(ctx context.Context, dati, file string) (Verificato, error) {
			v, err := VerificaArchivio(ctx, dati, file)
			v.SHA256 = strings.Repeat("b", 64)
			return v, err
		}
		if p, err := prova(t, pn); !errors.Is(err, db.ErrArchivioCambiato) || p.Archivio != e.Archivio {
			t.Errorf("%+v %v", p, err)
		}
		pn.n.verifica = func(ctx context.Context, dati, file string) (Verificato, error) {
			v, err := VerificaArchivio(ctx, dati, file)
			v.CatturatoIl = "2026-11-29T01:29:00.000000Z"
			return v, err
		}
		if _, err := prova(t, pn); !errors.Is(err, db.ErrArchivioCambiato) {
			t.Errorf("ora di cattura: %v", err)
		}
		pn.n.verifica = vero
	})
	t.Run("chiave diversa", func(t *testing.T) {
		pn := nuovaProvaNotturno(t, alle(2026, 11, 29, 2, 30))
		fattoBackup(t, pn, alle(2026, 11, 29, 2, 30))
		conChiave(t, pn.p) // toglie la chiave e ne crea un'altra
		if _, err := prova(t, pn); err == nil || !strings.Contains(err.Error(), "decifratura") {
			t.Errorf("%v", err)
		}
	})
	t.Run("fuori dalla chiavetta", func(t *testing.T) {
		pn := nuovaProvaNotturno(t, alle(2026, 11, 29, 2, 30))
		e := fattoBackup(t, pn, alle(2026, 11, 29, 2, 30))
		altra := filepath.Join(t.TempDir(), filepath.Base(e.Archivio))
		if _, err := pn.p.s.DB.Scrittura.ExecContext(ctx, `UPDATE verifiche_backup SET archivio = ?`, altra); err != nil {
			t.Fatal(err)
		}
		if _, err := prova(t, pn); !errors.Is(err, ErrProvaFuoriChiavetta) {
			t.Errorf("%v", err)
		}
	})
	t.Run("non configurata", func(t *testing.T) {
		pn := nuovaProvaNotturno(t, alle(2026, 11, 29, 2, 30))
		pn.n = NuovoNotturno(pn.p.s.DB, pn.p.s.Archivio, "", pn.o)
		if _, err := prova(t, pn); !errors.Is(err, ErrNonConfigurato) {
			t.Errorf("%v", err)
		}
	})
}

// Voce OPS-28, in parte (vedi TestProvaDovuta).
// Lo stop di serve durante la prova (a metà della verifica): niente riga, la settimana resta da fare, lucchetto
// libero, nessuna cartella temporanea. Un panico è una prova fallita con la frase fissa. Con il lucchetto preso da
// un altro backup la prova aspetta il giro dopo, senza righe.
func TestProvaStopPanicoLucchetto(t *testing.T) {
	pn := nuovaProvaNotturno(t, alle(2026, 11, 29, 2, 30))
	d := pn.p.s.DB
	fattoBackup(t, pn, alle(2026, 11, 29, 2, 30))
	pn.o.Sposta(time.Minute)
	vf := nuovaVerificaFerma()
	pn.n.verifica = vf.verifica
	ctx, ferma := context.WithCancel(context.Background())
	fatto := make(chan error, 1)
	go func() {
		_, _, err := pn.n.GiroProva(ctx)
		fatto <- err
	}()
	vf.aspettaPartita(t)
	ferma()
	if err := <-fatto; !errors.Is(err, context.Canceled) {
		t.Errorf("prova fermata: %v", err)
	}
	if v := righeProve(t, d); len(v) != 0 {
		t.Errorf("righe dopo lo stop: %v", v)
	}
	if m, _ := filepath.Glob(filepath.Join(pn.p.dir, ".*backup-*")); len(m) != 0 {
		t.Errorf("cartelle temporanee rimaste: %v", m)
	}
	lucchettoLibero(t, pn.p.dir)
	if ok, err := pn.n.ProvaDovuta(context.Background()); !ok || err != nil {
		t.Errorf("settimana dopo lo stop: %v %v", ok, err)
	}
	// Con il lucchetto preso: niente.
	l, err := PrendiLucchetto(pn.p.dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, fatta, err := pn.n.GiroProva(context.Background()); fatta || err != nil {
		t.Errorf("con il lucchetto preso: %v %v", fatta, err)
	}
	_ = l.Rilascia()
	// Panico.
	pn.n.verifica = func(context.Context, string, string) (Verificato, error) { panic("guasto inventato") }
	p, fatta, err := pn.n.GiroProva(context.Background())
	if !fatta || err == nil || p.Riuscita || p.Errore != DettaglioPanicoProva {
		t.Errorf("panico: %+v %v %v", p, fatta, err)
	}
	lucchettoLibero(t, pn.p.dir)
}

// Come il backup (revisione della 124): se la riga della prova non si scrive, il tentativo si conta in memoria; una
// prova riuscita senza riga chiude la settimana, prove fallite al più tre.
func TestProvaRigaNonScritta(t *testing.T) {
	pn := nuovaProvaNotturno(t, alle(2026, 11, 29, 2, 30))
	fattoBackup(t, pn, alle(2026, 11, 29, 2, 30))
	scritture := 0
	pn.n.inserisciProva = func(context.Context, *db.DB, db.ProvaRipristino) (int64, error) {
		scritture++
		return 0, errors.New("database occupato inventato")
	}
	ctx := context.Background()
	if _, fatta, err := pn.n.GiroProva(ctx); fatta || err == nil || scritture != 1 {
		t.Fatalf("prima prova: %v %v, scritture %d", fatta, err, scritture)
	}
	for _, dur := range []time.Duration{time.Minute, AttesaFraTentativi, time.Hour} {
		pn.o.Sposta(dur)
		if _, _, err := pn.n.GiroProva(ctx); err != nil || scritture != 1 {
			t.Errorf("dopo %v: %v, scritture %d", dur, err, scritture)
		}
	}
	// La domenica dopo, chiavetta smontata (backup e prove falliscono): tre prove e basta.
	pn.s.sis.info = rigaRadice()
	riga(t, pn.p.s.DB, AvvioAutomatico, "2026-12-06", EsitoOK, alle(2026, 12, 6, 2, 31))
	pn.o.Imposta(alle(2026, 12, 6, 2, 32))
	for range 6 {
		_, _, _ = pn.n.GiroProva(ctx)
		pn.o.Sposta(AttesaFraTentativi)
	}
	if scritture != 4 {
		t.Errorf("scritture %d, attese 1 + 3", scritture)
	}
}

// rovina riscrive il media k con un altro contenuto (stesso nome): un file rovinato sul disco.
func rovina(t *testing.T, p *prova, k string) {
	t.Helper()
	f := filepath.Join(p.s.Archivio.Cartella(), k)
	if err := os.Remove(f); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f, []byte("foto rovinata dal disco"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// D-116 (scelta A di Stefano): un media il cui contenuto non ha lo SHA-256 del nome non fa fallire il backup. Il
// giro è riuscito (riga «ok» nello storico e nel registro, conta per D-11 e la rotazione) e dice il numero; la riga
// del registro ha il numero e il nome; db.MediaRovinati lo dà alla 126. La prova settimanale lo ritrova e resta
// riuscita. Un backup dopo, con il media sistemato, torna a zero.
func TestMediaRovinatoNonFaFallire(t *testing.T) {
	pn := nuovaProvaNotturno(t, alle(2026, 11, 29, 2, 30))
	d := pn.p.s.DB
	ctx := context.Background()
	k := pn.p.foto[0]
	rovina(t, pn.p, k)
	var righe strings.Builder
	l, err := PrendiLucchetto(pn.p.dir)
	if err != nil {
		t.Fatal(err)
	}
	g := Giro{Lucchetto: l, DB: d, Archivio: pn.p.s.Archivio, Chiavetta: pn.s.chiavetta, Cattura: pn.o, Registro: pn.o,
		Dire: func(r string) error { righe.WriteString(r); return nil }}
	eg, err := g.Esegui(ctx)
	_ = l.Rilascia()
	if err != nil || !eg.Riuscito {
		t.Fatalf("giro: %+v %v", eg, err)
	}
	if !strings.Contains(righe.String(), "ATTENZIONE: 1 media con il contenuto diverso dal nome") || !strings.HasSuffix(righe.String(), "backup riuscito\n") {
		t.Errorf("righe:\n%s", righe.String())
	}
	v, err := VerificaArchivio(ctx, pn.p.dir, eg.Risultato.File)
	if err != nil || len(v.MediaRovinati) != 1 || v.MediaRovinati[0] != k {
		t.Errorf("verifica: %v %v", v.MediaRovinati, err)
	}
	u, ok, err := db.UltimoVerificatoSullaChiavetta(ctx, d.Lettura)
	if err != nil || !ok || u.Archivio != eg.Risultato.File {
		t.Fatalf("registro: %+v %v %v", u, ok, err)
	}
	if n, nomi, err := db.MediaRovinati(ctx, d.Lettura); n != 1 || len(nomi) != 1 || nomi[0] != k || err != nil {
		t.Errorf("media rovinati nel registro: %d %v %v", n, nomi, err)
	}
	// La prova li ritrova e resta riuscita.
	riga(t, d, AvvioAutomatico, "2026-11-29", EsitoOK, alle(2026, 11, 29, 2, 31))
	pn.o.Imposta(alle(2026, 11, 29, 2, 32))
	p, fatta, err := pn.n.GiroProva(ctx)
	if !fatta || err != nil || !p.Riuscita || p.MediaRovinati != 1 {
		t.Errorf("prova: %+v %v %v", p, fatta, err)
	}
	// Il media sistemato (ricaricato): il backup dopo torna a zero.
	f := filepath.Join(pn.p.s.Archivio.Cartella(), k)
	if err := os.Remove(f); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f, []byte("foto inventata uno"), 0o600); err != nil {
		t.Fatal(err)
	}
	fattoBackup(t, pn, alle(2026, 11, 30, 2, 30))
	if n, nomi, err := db.MediaRovinati(ctx, d.Lettura); n != 0 || len(nomi) != 0 || err != nil {
		t.Errorf("dopo il media sistemato: %d %v %v", n, nomi, err)
	}
}

// Revisione della 125 (M13, M14 del revisore): la prova vuole il lucchetto dei backup della sua cartella dei dati
// (nil, rilasciato o di un'altra cartella: ErrSenzaLucchetto, niente righe) e, con il lucchetto, toglie le
// cartelle temporanee rimaste e lo dice. Un'uscita che non si scrive (`| head`) non fa fallire la prova.
func TestProvaLucchettoRestiEUscita(t *testing.T) {
	pn := nuovaProvaNotturno(t, alle(2026, 11, 29, 2, 30))
	d := pn.p.s.DB
	ctx := context.Background()
	fattoBackup(t, pn, alle(2026, 11, 29, 2, 30))
	pn.o.Sposta(time.Minute)
	p := Prova{DB: d, Chiavetta: pn.s.chiavetta, Adesso: pn.o.Adesso()}
	if _, err := p.Esegui(ctx); !errors.Is(err, ErrSenzaLucchetto) {
		t.Errorf("senza lucchetto: %v", err)
	}
	altra := t.TempDir()
	la, err := PrendiLucchetto(altra)
	if err != nil {
		t.Fatal(err)
	}
	resto := filepath.Join(pn.p.dir, ".verifica-backup-77")
	if err := os.Mkdir(resto, 0o700); err != nil {
		t.Fatal(err)
	}
	p.Lucchetto = la
	if _, err := p.Esegui(ctx); !errors.Is(err, ErrSenzaLucchetto) {
		t.Errorf("lucchetto di un'altra cartella dei dati: %v", err)
	}
	_ = la.Rilascia()
	if _, err := os.Lstat(resto); err != nil {
		t.Errorf("cartella temporanea tolta senza il lucchetto giusto: %v", err)
	}
	l, err := PrendiLucchetto(pn.p.dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Rilascia() }()
	// L'uscita si chiude alla prima riga: la prova va avanti, riesce e si registra.
	var righe []string
	pr, fatta, err := pn.n.ProvaAMano(ctx, l, func(r string) error {
		righe = append(righe, r)
		return errors.New("broken pipe inventato")
	})
	if !fatta || err != nil || !pr.Riuscita {
		t.Fatalf("con l'uscita chiusa: %+v %v %v", pr, fatta, err)
	}
	if len(righe) < 3 || righe[0] != "tolta "+resto+", cartella temporanea rimasta da un backup interrotto\n" {
		t.Errorf("righe %q", righe)
	}
	if _, err := os.Lstat(resto); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("cartella temporanea rimasta: %v", err)
	}
	if v := strings.Join(righeProve(t, d), ","); v != "a_mano 2026-11-29 ok" {
		t.Errorf("prove %s", v)
	}
}

// Revisione della 125: le domeniche del cambio dell'ora (Europe/Rome). Il 29/03/2026 le 02:30 non esistono: il
// turno della domenica parte alle 03:00 CEST, e la prova della settimana del 29/03 dopo quel backup (vera, sul
// lavoro). Il 25/10/2026 le 02:30 vengono due volte: alla prima backup e prova; nell'ora ripetuta (02:29 CET) il
// turno torna al sabato e la sua settimana (18/10) è già fatta; alla seconda 02:30 niente.
func TestProvaCambioDOra(t *testing.T) {
	pn := nuovaProvaNotturno(t, time.Date(2026, 3, 29, 0, 59, 0, 0, time.UTC))
	d, n, o := pn.p.s.DB, pn.n, pn.o
	ctx := context.Background()
	riga(t, d, AvvioAutomatico, "2026-03-28", EsitoOK, time.Date(2026, 3, 28, 1, 35, 0, 0, time.UTC))
	rigaProva(t, d, db.AvvioProvaAutomatico, "2026-03-22", true, time.Date(2026, 3, 22, 1, 40, 0, 0, time.UTC))
	if provaDovutaAlle(t, n, o, time.Date(2026, 3, 29, 0, 59, 0, 0, time.UTC)) { // 01:59 CET
		t.Error("29/03 01:59 CET: dovuta")
	}
	fattoBackup(t, pn, time.Date(2026, 3, 29, 1, 0, 0, 0, time.UTC)) // 03:00 CEST
	p, fatta, err := n.GiroProva(ctx)
	if !fatta || err != nil || !p.Riuscita || p.Settimana.String() != "2026-03-29" {
		t.Fatalf("29/03 03:00 CEST: %+v %v %v", p, fatta, err)
	}
	if provaDovutaAlle(t, n, o, time.Date(2026, 3, 29, 2, 0, 0, 0, time.UTC)) {
		t.Error("29/03 04:00 CEST: dovuta di nuovo")
	}
	riga(t, d, AvvioAutomatico, "2026-10-24", EsitoOK, time.Date(2026, 10, 24, 0, 35, 0, 0, time.UTC))
	rigaProva(t, d, db.AvvioProvaAutomatico, "2026-10-18", true, time.Date(2026, 10, 18, 0, 40, 0, 0, time.UTC))
	if provaDovutaAlle(t, n, o, time.Date(2026, 10, 25, 0, 29, 0, 0, time.UTC)) { // 02:29 CEST
		t.Error("25/10 02:29 CEST: dovuta")
	}
	if provaDovutaAlle(t, n, o, time.Date(2026, 10, 25, 0, 30, 0, 0, time.UTC)) { // 02:30 CEST, prima del backup
		t.Error("25/10 02:30 CEST prima del backup: dovuta")
	}
	riga(t, d, AvvioAutomatico, "2026-10-25", EsitoOK, time.Date(2026, 10, 25, 0, 35, 0, 0, time.UTC))
	if !provaDovutaAlle(t, n, o, time.Date(2026, 10, 25, 0, 36, 0, 0, time.UTC)) {
		t.Error("25/10 dopo il backup: non dovuta")
	}
	rigaProva(t, d, db.AvvioProvaAutomatico, "2026-10-25", true, time.Date(2026, 10, 25, 0, 40, 0, 0, time.UTC))
	if provaDovutaAlle(t, n, o, time.Date(2026, 10, 25, 1, 29, 0, 0, time.UTC)) { // 02:29 CET, ora ripetuta
		t.Error("25/10 02:29 CET: dovuta")
	}
	if provaDovutaAlle(t, n, o, time.Date(2026, 10, 25, 1, 30, 0, 0, time.UTC)) { // 02:30 CET, la seconda volta
		t.Error("25/10 02:30 CET: dovuta")
	}
}
