package db

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/L0cutus/gestrip/internal/clock"
)

// Il registro delle prove di ripristino (slice 125) e i media rovinati nel registro dei backup (D-116).

var domenicaProva = clock.Data{Anno: 2026, Mese: 11, Giorno: 29}

func provaRiuscita(avvio string, fine time.Time) ProvaRipristino {
	return ProvaRipristino{Avvio: avvio, Settimana: domenicaProva, IniziatoIl: fine.Add(-time.Minute), FinitoIl: fine,
		Riuscita: true, Archivio: "/chiavetta/gestrip-backup-20261129T013000Z.tar.age", SHA256: strings.Repeat("c", 64),
		CatturatoIl: fine.Add(-time.Hour), MediaRovinati: 2}
}

// Le righe che non vanno (avvio, settimana, istanti, motivo, archivio di una prova riuscita, percorso, impronta)
// non si scrivono; il motivo si accorcia a 1000 byte senza spezzare un carattere; anche il CHECK della tabella
// rifiuta una riga «ok» senza archivio scritta a mano.
func TestRegistraProvaRipristino(t *testing.T) {
	d := registroProva(t)
	fine := time.Date(2026, 11, 29, 1, 40, 0, 0, time.UTC)
	buona := provaRiuscita(AvvioProvaAutomatico, fine)
	for nome, cambia := range map[string]func(*ProvaRipristino){
		"avvio":                      func(p *ProvaRipristino) { p.Avvio = "notturno" },
		"settimana":                  func(p *ProvaRipristino) { p.Settimana = clock.Data{} },
		"inizio":                     func(p *ProvaRipristino) { p.IniziatoIl = time.Time{} },
		"fine":                       func(p *ProvaRipristino) { p.FinitoIl = time.Time{} },
		"fallita senza motivo":       func(p *ProvaRipristino) { p.Riuscita, p.Errore = false, "  " },
		"riuscita senza archivio":    func(p *ProvaRipristino) { p.Archivio = "" },
		"percorso relativo":          func(p *ProvaRipristino) { p.Archivio = "chiavetta/a.tar.age" },
		"percorso non pulito":        func(p *ProvaRipristino) { p.Archivio = "/chiavetta/../a.tar.age" },
		"impronta":                   func(p *ProvaRipristino) { p.SHA256 = strings.Repeat("C", 64) },
		"cattura":                    func(p *ProvaRipristino) { p.CatturatoIl = time.Time{} },
		"media rovinati negativi":    func(p *ProvaRipristino) { p.MediaRovinati = -1 },
		"fallita con archivio rotto": func(p *ProvaRipristino) { p.Riuscita, p.Errore, p.SHA256 = false, "x", "" },
	} {
		p := buona
		cambia(&p)
		if _, err := RegistraProvaRipristino(ctx, d, p); !errors.Is(err, ErrProvaRipristino) {
			t.Errorf("%s: %v", nome, err)
		}
	}
	if _, ok, err := UltimaProvaRipristino(ctx, d.Lettura); ok || err != nil {
		t.Fatalf("righe scritte: %v %v", ok, err)
	}
	id, err := RegistraProvaRipristino(ctx, d, buona)
	if err != nil {
		t.Fatal(err)
	}
	u, ok, err := UltimaProvaRipristino(ctx, d.Lettura)
	buona.ID = id
	if err != nil || !ok || fmt.Sprint(u) != fmt.Sprint(buona) {
		t.Errorf("letta %+v %v %v\nscritta %+v", u, ok, err, buona)
	}
	lunga := ProvaRipristino{Avvio: AvvioProvaAMano, Settimana: domenicaProva, IniziatoIl: fine, FinitoIl: fine,
		Errore: strings.Repeat("è", 800)}
	if _, err := RegistraProvaRipristino(ctx, d, lunga); err != nil {
		t.Fatal(err)
	}
	u, _, _ = UltimaProvaRipristino(ctx, d.Lettura)
	if u.Riuscita || len(u.Errore) > 1000 || len(u.Errore) < 998 || !utf8.ValidString(u.Errore) || u.Archivio != "" || !u.CatturatoIl.IsZero() {
		t.Errorf("motivo lungo: %d byte, %+v", len(u.Errore), u)
	}
	if _, err := d.Scrittura.ExecContext(ctx, `INSERT INTO prove_ripristino (avvio, settimana, iniziato_il, finito_il, esito)
		VALUES ('automatico', '2026-11-29', '2026-11-29T01:39:00.000000Z', '2026-11-29T01:40:00.000000Z', 'ok')`); err == nil {
		t.Error("CHECK: riga «ok» senza archivio scritta")
	}
}

// Voce OPS-28, in parte: niente segno «inventario:» (la voce chiude con l'allarme, 126-127, e il fuori sede, 128).
// Lo stato dell'allarme «prova settimanale fallita» (126): senza prove spento; l'ultima prova fallita lo accende,
// automatica o a mano; una riuscita dopo, anche a mano, lo spegne. TentativiProva conta per avvio e settimana.
func TestProvaSettimanaleFallita(t *testing.T) {
	d := registroProva(t)
	fine := time.Date(2026, 11, 29, 1, 40, 0, 0, time.UTC)
	stato := func() (int64, bool) {
		t.Helper()
		p, f, err := ProvaSettimanaleFallita(ctx, d.Lettura)
		if err != nil {
			t.Fatal(err)
		}
		return p.ID, f
	}
	if _, f := stato(); f {
		t.Error("senza prove: acceso")
	}
	if _, err := RegistraProvaRipristino(ctx, d, provaRiuscita(AvvioProvaAutomatico, fine)); err != nil {
		t.Fatal(err)
	}
	if _, f := stato(); f {
		t.Error("dopo una riuscita: acceso")
	}
	fallita := ProvaRipristino{Avvio: AvvioProvaAutomatico, Settimana: domenicaProva, IniziatoIl: fine, FinitoIl: fine.Add(time.Hour),
		Errore: "l'ultimo backup ha più di 8 giorni"}
	id, err := RegistraProvaRipristino(ctx, d, fallita)
	if err != nil {
		t.Fatal(err)
	}
	if g, f := stato(); !f || g != id {
		t.Errorf("dopo una fallita: %v %d", f, g)
	}
	fallita.Avvio = AvvioProvaAMano
	if _, err := RegistraProvaRipristino(ctx, d, fallita); err != nil {
		t.Fatal(err)
	}
	if _, f := stato(); !f {
		t.Error("dopo una fallita a mano: spento")
	}
	if _, err := RegistraProvaRipristino(ctx, d, provaRiuscita(AvvioProvaAMano, fine.Add(2*time.Hour))); err != nil {
		t.Fatal(err)
	}
	if _, f := stato(); f {
		t.Error("dopo una riuscita a mano: acceso")
	}
	tutte, riuscite, ultima, err := TentativiProva(ctx, d.Lettura, AvvioProvaAutomatico, domenicaProva)
	if err != nil || tutte != 2 || riuscite != 1 || !ultima.Equal(fine.Add(time.Hour)) {
		t.Errorf("tentativi automatici: %d %d %v %v", tutte, riuscite, ultima, err)
	}
	tutte, _, ultima, err = TentativiProva(ctx, d.Lettura, AvvioProvaAutomatico, clock.Data{Anno: 2026, Mese: 12, Giorno: 6})
	if err != nil || tutte != 0 || !ultima.IsZero() {
		t.Errorf("altra settimana: %d %v %v", tutte, ultima, err)
	}
}

// D-116: la riga del registro porta quanti media sono rovinati e i primi 100 nomi in ordine; un nome malformato
// non si scrive; MediaRovinati legge l'ultima verifica riuscita sulla chiavetta (non quelle fallite né le cartelle);
// una riga scritta senza (come prima della 125) ha 0.
func TestMediaRovinatiNelRegistro(t *testing.T) {
	d := registroProva(t)
	if n, nomi, err := MediaRovinati(ctx, d.Lettura); n != 0 || nomi != nil || err != nil {
		t.Errorf("senza righe: %d %v %v", n, nomi, err)
	}
	f, sha, n := archivioProva(t, "archivio inventato")
	v := verificaProva(f, sha, n, primaProva)
	v.MediaRovinati = []string{strings.Repeat("a", 63) + "g"}
	if err := RegistraVerificaBackup(ctx, d, v); !errors.Is(err, ErrRegistroBackup) {
		t.Errorf("nome malformato: %v", err)
	}
	var tanti []string
	for i := range 150 {
		tanti = append(tanti, fmt.Sprintf("%064x", 150-i))
	}
	v.MediaRovinati = tanti
	registra(t, d, v)
	k, nomi, err := MediaRovinati(ctx, d.Lettura)
	attesi := slices.Sorted(slices.Values(tanti))[:MaxNomiRovinati]
	if k != 150 || !slices.Equal(nomi, attesi) || err != nil {
		t.Errorf("150 rovinati: %d %d nomi (%v…) %v", k, len(nomi), nomi[:1], err)
	}
	// Una verifica fallita dopo e una cartella dopo non contano.
	fallita := verificaProva(f, sha, n, dopoProva)
	fallita.Riuscita, fallita.Errore = false, "motivo inventato"
	registra(t, d, fallita)
	cartella := verificaProva(f, sha, n, dopoProva)
	cartella.Destinazione = DestinazioneCartella
	registra(t, d, cartella)
	if k, _, _ := MediaRovinati(ctx, d.Lettura); k != 150 {
		t.Errorf("con una fallita e una cartella dopo: %d", k)
	}
	// Una riga senza le colonne nuove (come le righe di prima della 125): 0.
	if _, err := d.Scrittura.ExecContext(ctx, `INSERT INTO verifiche_backup (esito, destinazione, archivio, sha256, byte,
		catturato_il, verificato_il, conteggi) VALUES ('ok', 'chiavetta', ?, ?, ?, ?, ?, '{"buste":3}')`,
		f, sha, n, dopoProva.Add(time.Hour).Format(FormatoIstante), dopoProva.Add(2*time.Hour).Format(FormatoIstante)); err != nil {
		t.Fatal(err)
	}
	if k, nomi, err := MediaRovinati(ctx, d.Lettura); k != 0 || len(nomi) != 0 || err != nil {
		t.Errorf("riga di prima: %d %v %v", k, nomi, err)
	}
	// Il CHECK: più nomi del numero non entrano.
	if _, err := d.Scrittura.ExecContext(ctx, `UPDATE verifiche_backup SET media_rovinati_nomi = '["a","b"]', media_rovinati = 1`); err == nil {
		t.Error("CHECK: due nomi per un media rovinato")
	}
}
