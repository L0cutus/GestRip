package db

import (
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/L0cutus/gestrip/internal/clock"
)

// Il registro delle prove mensili della copia remota (slice 129c, migrazione 0025).

var turnoProva = clock.Data{Anno: 2026, Mese: 12, Giorno: 6}

func provaRemotaRiuscita(avvio string, fine time.Time) ProvaRemota {
	return ProvaRemota{Avvio: avvio, Mese: "2026-12", Turno: turnoProva, IniziatoIl: fine.Add(-time.Minute), FinitoIl: fine, Riuscita: true,
		Archivio: "/backup/gestrip-backup-20261206T013000Z.tar.age", SHA256: strings.Repeat("d", 64), Byte: 4096,
		CatturatoIl: fine.Add(-time.Hour), MediaRovinati: 1}
}

// Voce OPS-28, in parte (la voce chiude con TestProvaRemotaDelMese, internal/backup).
// Le righe che non vanno non si scrivono (anche un codice che la copia notturna ha ma il download no); il motivo si
// accorcia a 1000 byte; il CHECK della tabella rifiuta da solo una riga «ok» senza copia e un codice sconosciuto;
// le letture tornano quello scritto, e TentativiProvaRemota conta per avvio e mese.
func TestRegistraProvaRemota(t *testing.T) {
	d := registroProva(t)
	fine := time.Date(2026, 12, 6, 1, 50, 0, 0, time.UTC)
	buona := provaRemotaRiuscita(AvvioProvaAutomatico, fine)
	for nome, cambia := range map[string]func(*ProvaRemota){
		"avvio":                   func(p *ProvaRemota) { p.Avvio = "notturno" },
		"mese vuoto":              func(p *ProvaRemota) { p.Mese = "" },
		"mese 13":                 func(p *ProvaRemota) { p.Mese = "2026-13" },
		"mese con il giorno":      func(p *ProvaRemota) { p.Mese = "2026-12-06" },
		"inizio":                  func(p *ProvaRemota) { p.IniziatoIl = time.Time{} },
		"turno":                   func(p *ProvaRemota) { p.Turno = clock.Data{} },
		"fine":                    func(p *ProvaRemota) { p.FinitoIl = time.Time{} },
		"riuscita con codice":     func(p *ProvaRemota) { p.Codice = "rete" },
		"riuscita con motivo":     func(p *ProvaRemota) { p.Errore = "x" },
		"fallita senza motivo":    func(p *ProvaRemota) { p.Riuscita, p.Codice, p.Errore = false, "rete", " " },
		"fallita senza codice":    func(p *ProvaRemota) { p.Riuscita, p.Errore = false, "x" },
		"codice della sola copia": func(p *ProvaRemota) { p.Riuscita, p.Codice, p.Errore = false, "rotazione", "x" },
		"riuscita senza copia":    func(p *ProvaRemota) { p.Archivio = "" },
		"percorso relativo":       func(p *ProvaRemota) { p.Archivio = "backup/a.tar.age" },
		"percorso non pulito":     func(p *ProvaRemota) { p.Archivio = "/backup/../a.tar.age" },
		"impronta":                func(p *ProvaRemota) { p.SHA256 = strings.Repeat("D", 64) },
		"byte":                    func(p *ProvaRemota) { p.Byte = 0 },
		"cattura":                 func(p *ProvaRemota) { p.CatturatoIl = time.Time{} },
		"media rovinati negativi": func(p *ProvaRemota) { p.MediaRovinati = -1 },
		"fallita con copia rotta": func(p *ProvaRemota) { p.Riuscita, p.Codice, p.Errore, p.SHA256 = false, "rete", "x", "" },
	} {
		p := buona
		cambia(&p)
		if _, err := RegistraProvaRemota(ctx, d, p); !errors.Is(err, ErrProvaRemota) {
			t.Errorf("%s: %v", nome, err)
		}
	}
	if _, ok, err := UltimaProvaRemota(ctx, d.Lettura); ok || err != nil {
		t.Fatalf("righe scritte: %v %v", ok, err)
	}
	id, err := RegistraProvaRemota(ctx, d, buona)
	if err != nil {
		t.Fatal(err)
	}
	buona.ID = id
	u, ok, err := UltimaProvaRemota(ctx, d.Lettura)
	if err != nil || !ok || u.ID != id || u.Avvio != buona.Avvio || u.Mese != buona.Mese || u.Turno != turnoProva || !u.IniziatoIl.Equal(buona.IniziatoIl) ||
		!u.FinitoIl.Equal(buona.FinitoIl) || !u.Riuscita || u.Codice != "" || u.Errore != "" || u.Archivio != buona.Archivio ||
		u.SHA256 != buona.SHA256 || u.Byte != buona.Byte || !u.CatturatoIl.Equal(buona.CatturatoIl) || u.MediaRovinati != 1 {
		t.Fatalf("riletta %+v %v %v", u, ok, err)
	}
	// Una fallita senza copia (nessuna copia nel registro) con un motivo lungo: accorciato senza spezzare.
	fallita := ProvaRemota{Avvio: AvvioProvaAMano, Mese: "2026-12", Turno: turnoProva, IniziatoIl: fine, FinitoIl: fine.Add(time.Minute),
		Codice: "nessuna", Errore: strings.Repeat("è", 600)}
	if _, err := RegistraProvaRemota(ctx, d, fallita); err != nil {
		t.Fatal(err)
	}
	u, _, _ = UltimaProvaRemota(ctx, d.Lettura)
	if u.Riuscita || u.Codice != "nessuna" || len(u.Errore) > 1000 || !utf8.ValidString(u.Errore) || u.Archivio != "" || u.Byte != 0 {
		t.Errorf("fallita %+v", u)
	}
	tutte, riuscite, ultima, err := TentativiProvaRemota(ctx, d.Lettura, AvvioProvaAutomatico, "2026-12", turnoProva)
	if err != nil || tutte != 1 || riuscite != 1 || !ultima.Equal(fine) {
		t.Errorf("tentativi automatici: %d %d %v %v", tutte, riuscite, ultima, err)
	}
	if tutte, _, _, err := TentativiProvaRemota(ctx, d.Lettura, AvvioProvaAMano, "2026-12", turnoProva); err != nil || tutte != 1 {
		t.Errorf("tentativi a mano: %d %v", tutte, err)
	}
	if tutte, _, ultima, err := TentativiProvaRemota(ctx, d.Lettura, AvvioProvaAutomatico, "2026-11", turnoProva); err != nil || tutte != 0 || !ultima.IsZero() {
		t.Errorf("altro mese: %d %v %v", tutte, ultima, err)
	}
	// Un altro turno dello stesso mese: nessun tentativo in quel turno, ma il mese ha la sua riuscita.
	if tutte, riuscite, ultima, err := TentativiProvaRemota(ctx, d.Lettura, AvvioProvaAutomatico, "2026-12",
		clock.Data{Anno: 2026, Mese: 12, Giorno: 7}); err != nil || tutte != 0 || riuscite != 1 || !ultima.IsZero() {
		t.Errorf("altro turno: %d %d %v %v", tutte, riuscite, ultima, err)
	}
	// Il CHECK da solo.
	for nome, q := range map[string]string{
		"ok senza copia": `INSERT INTO prove_remote (avvio, mese, turno, iniziato_il, finito_il, esito) VALUES ('automatico',
			'2026-12', '2026-12-06', '2026-12-06T01:00:00.000000Z', '2026-12-06T01:00:00.000000Z', 'ok')`,
		"codice sconosciuto": `INSERT INTO prove_remote (avvio, mese, turno, iniziato_il, finito_il, esito, codice, errore) VALUES
			('automatico', '2026-12', '2026-12-06', '2026-12-06T01:00:00.000000Z', '2026-12-06T01:00:00.000000Z', 'fallita', 'rotazione', 'x')`,
		"copia a metà": `INSERT INTO prove_remote (avvio, mese, turno, iniziato_il, finito_il, esito, codice, errore, archivio) VALUES
			('automatico', '2026-12', '2026-12-06', '2026-12-06T01:00:00.000000Z', '2026-12-06T01:00:00.000000Z', 'fallita', 'rete', 'x', '/b/a')`,
	} {
		if _, err := d.Scrittura.ExecContext(ctx, q); err == nil {
			t.Errorf("CHECK: %s scritta", nome)
		}
	}
}
