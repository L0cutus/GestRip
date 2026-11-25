package backup

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
)

// Lo stato dei backup e i problemi dell'allarme (stato.go, slice 126, tema E, risposta H). Orologio finto, database
// inventato, chiavetta finta della 123; per le condizioni uno Stato scritto a mano.

var adessoStato = time.Date(2026, 11, 25, 9, 0, 0, 0, time.UTC)

// reData è una data gg/mm/aaaa dei testi (l'unica barra ammessa).
var reData = regexp.MustCompile(`[0-9]{2}/[0-9]{2}/[0-9]{4}`)

// statoBuono è un backup configurato senza problemi.
func statoBuono() Stato {
	return Stato{Adesso: adessoStato, Configurato: true,
		Esecuzioni:   []Esecuzione{{ID: 1, Avvio: AvvioAutomatico, IniziatoIl: adessoStato.Add(-8 * time.Hour), Esito: EsitoOK}},
		UltimoBackup: db.ArchivioRegistrato{CatturatoIl: adessoStato.Add(-8 * time.Hour)}, CeBackup: true,
		PrimoBackup: adessoStato.Add(-60 * 24 * time.Hour), PiuAvanti: adessoStato.Add(-8 * time.Hour),
		Prova: db.ProvaRipristino{Riuscita: true, FinitoIl: adessoStato.Add(-3 * 24 * time.Hour)}, CeProva: true}
}

func codici(pp []Problema) []string {
	var v []string
	for _, p := range pp {
		v = append(v, p.Codice)
	}
	return v
}

// Voce OPS-28, in parte: niente segno «inventario:» (la voce chiude con gli avvisi della 127 e la copia fuori sede
// della 128).
// Ogni condizione dell'allarme con i suoi confini: 26 ore esatte no, un secondo dopo sì; prova settimanale fallita;
// prova mancante (8 giorni esatti no, un secondo dopo sì; mai fatta con il primo backup di più di 8 giorni); un
// istante del registro nel futuro (nota 8(a)); chiavetta; ultimo backup fallito (un «avviso» no); media rovinati;
// senza configurazione solo quella. Con il lavoro spento il banner è vuoto, i problemi restano.
func TestProblemi(t *testing.T) {
	casi := []struct {
		nome   string
		cambia func(*Stato)
		attesi []string
	}{
		{"sano", func(*Stato) {}, nil},
		{"non configurato", func(s *Stato) {
			s.Configurato, s.CeBackup, s.CeProva, s.Chiavetta = false, false, false, ErrNonMontata
		}, []string{ProblemaNonConfigurato}},
		{"chiavetta assente", func(s *Stato) { s.Chiavetta = fmt.Errorf("%w: /media/x", ErrNonMontata) }, []string{ProblemaChiavetta}},
		{"chiavetta in sola lettura", func(s *Stato) { s.Chiavetta = ErrChiavettaSolaLettura }, []string{ProblemaChiavetta}},
		{"ultimo fallito", func(s *Stato) {
			s.Esecuzioni = []Esecuzione{{Esito: EsitoFallito, IniziatoIl: adessoStato}, {Esito: EsitoOK}}
		}, []string{ProblemaBackupFallito}},
		{"fallito prima di un riuscito", func(s *Stato) {
			s.Esecuzioni = []Esecuzione{{Esito: EsitoOK}, {Esito: EsitoFallito}}
		}, nil},
		{"ultimo con un avviso", func(s *Stato) { s.Esecuzioni = []Esecuzione{{Esito: EsitoAvviso}} }, nil},
		{"nessuna esecuzione", func(s *Stato) { s.Esecuzioni = nil }, nil},
		{"26 ore esatte", func(s *Stato) { s.UltimoBackup.CatturatoIl = adessoStato.Add(-EtaMassimaBackup) }, nil},
		{"26 ore e un secondo", func(s *Stato) {
			s.UltimoBackup.CatturatoIl = adessoStato.Add(-EtaMassimaBackup - time.Second)
		}, []string{ProblemaBackupVecchio}},
		{"nessun backup", func(s *Stato) {
			s.CeBackup, s.UltimoBackup, s.PrimoBackup, s.CeProva = false, db.ArchivioRegistrato{}, time.Time{}, false
		}, []string{ProblemaBackupVecchio}},
		{"orologio: cattura nel futuro", func(s *Stato) {
			s.UltimoBackup.CatturatoIl, s.PiuAvanti = adessoStato.Add(time.Hour), adessoStato.Add(time.Hour)
		}, []string{ProblemaOrologio}},
		{"orologio: adesso esatto", func(s *Stato) { s.PiuAvanti = adessoStato }, nil},
		{"orologio: un secondo avanti", func(s *Stato) { s.PiuAvanti = adessoStato.Add(time.Second) }, []string{ProblemaOrologio}},
		{"prova fallita", func(s *Stato) { s.Prova.Riuscita = false }, []string{ProblemaProvaFallita}},
		{"prova fallita e vecchia", func(s *Stato) {
			s.Prova.Riuscita, s.Prova.FinitoIl = false, adessoStato.Add(-20*24*time.Hour)
		}, []string{ProblemaProvaFallita}},
		{"prova di 8 giorni esatti", func(s *Stato) { s.Prova.FinitoIl = adessoStato.Add(-EtaMassimaProva) }, nil},
		{"prova di 8 giorni e un secondo", func(s *Stato) {
			s.Prova.FinitoIl = adessoStato.Add(-EtaMassimaProva - time.Second)
		}, []string{ProblemaProvaMancante}},
		{"prova mai fatta, primo backup di 8 giorni", func(s *Stato) {
			s.CeProva, s.Prova, s.PrimoBackup = false, db.ProvaRipristino{}, adessoStato.Add(-EtaMassimaProva)
		}, nil},
		{"prova mai fatta, primo backup di 8 giorni e un secondo", func(s *Stato) {
			s.CeProva, s.Prova, s.PrimoBackup = false, db.ProvaRipristino{}, adessoStato.Add(-EtaMassimaProva-time.Second)
		}, []string{ProblemaProvaMancante}},
		{"media rovinati", func(s *Stato) { s.Rovinati = 3 }, []string{ProblemaMediaRovinati}},
		{"tutto insieme", func(s *Stato) {
			s.Chiavetta, s.Esecuzioni = ErrNonMontata, []Esecuzione{{Esito: EsitoFallito}}
			s.UltimoBackup.CatturatoIl, s.PiuAvanti = adessoStato.Add(-30*time.Hour), adessoStato.Add(time.Minute)
			s.Prova.Riuscita, s.Rovinati = false, 1
		}, []string{ProblemaChiavetta, ProblemaBackupFallito, ProblemaBackupVecchio, ProblemaOrologio, ProblemaProvaFallita, ProblemaMediaRovinati}},
	}
	for _, c := range casi {
		t.Run(c.nome, func(t *testing.T) {
			s := statoBuono()
			c.cambia(&s)
			pp := s.Problemi()
			if got := codici(pp); !slices.Equal(got, c.attesi) {
				t.Fatalf("problemi %v, attesi %v", got, c.attesi)
			}
			if got := codici(s.Allarme()); !slices.Equal(got, c.attesi) {
				t.Errorf("allarme %v, attesi %v", got, c.attesi)
			}
			for _, p := range pp {
				// Il testo dice cosa fare, senza percorsi (solo le date gg/mm/aaaa hanno la barra) né motivi tecnici.
				senzaDate := reData.ReplaceAllString(p.Testo, "")
				if p.Testo == "" || strings.Contains(senzaDate, "/") || strings.Contains(p.Testo, "backup_chiavetta") {
					t.Errorf("%s: testo %q", p.Codice, p.Testo)
				}
			}
			// Con il lavoro spento tacciono solo «non configurato», «backup vecchio» e «prova mancante».
			s.LavoroSpento = true
			var restano []string
			for _, c := range c.attesi {
				if c != ProblemaNonConfigurato && c != ProblemaBackupVecchio && c != ProblemaProvaMancante {
					restano = append(restano, c)
				}
			}
			if got := codici(s.Allarme()); !slices.Equal(got, restano) {
				t.Errorf("con il lavoro spento: allarme %v, attesi %v", got, restano)
			}
			if got := codici(s.Problemi()); !slices.Equal(got, c.attesi) {
				t.Errorf("con il lavoro spento i problemi cambiano: %v", got)
			}
		})
	}
	// I testi della chiavetta: assente, sola lettura, un altro disco.
	for err, frase := range map[error]string{ErrNonMontata: "non c'è o non risponde", ErrDestinazione: "non c'è o non risponde",
		ErrChiavettaSolaLettura: "non si può scrivere", ErrStessoDisco: "qualcos'altro", ErrNonChiavetta: "qualcos'altro"} {
		if got := testoChiavetta(fmt.Errorf("avvolto: %w", err)); !strings.Contains(got, frase) {
			t.Errorf("%v: %q", err, got)
		}
	}
	// Un errore della cartella dei dati o della tabella dei montaggi non dice che la chiavetta manca.
	if got := testoChiavetta(errors.New("backup: cartella dei dati: inventato")); strings.Contains(got, "non c'è") ||
		!strings.Contains(got, "problema dell'apparecchio") {
		t.Errorf("errore dei dati: %q", got)
	}
	// Le 26 ore della risposta H come numero, non solo come costante; senza backup il testo lo dice.
	s := statoBuono()
	s.UltimoBackup.CatturatoIl = adessoStato.Add(-26*time.Hour - 30*time.Minute)
	if got := codici(s.Problemi()); !slices.Equal(got, []string{ProblemaBackupVecchio}) {
		t.Errorf("26 ore e mezza: %v", got)
	}
	s = statoBuono()
	s.CeBackup, s.UltimoBackup = false, db.ArchivioRegistrato{}
	if p := s.Problemi(); len(p) != 1 || !strings.Contains(p[0].Testo, "non c'è ancora nessun backup buono") {
		t.Errorf("nessun backup: %v", p)
	}
	s = statoBuono()
	s.Rovinati = 1
	if p := s.Problemi(); len(p) != 1 || !strings.Contains(p[0].Testo, "1 foto o file rovinato ") {
		t.Errorf("un media rovinato: %v", p)
	}
}

// Voce OPS-28, in parte (vedi TestProblemi).
// ProssimoGiro: oggi se l'ora non è passata, domani se è passata o è adesso esatto; sull'ora del muro di Roma anche
// nei giorni del cambio d'ora.
func TestProssimoGiro(t *testing.T) {
	ora := 2*time.Hour + 30*time.Minute
	roma := func(a int, m time.Month, g, h, mi int) time.Time {
		return time.Date(a, m, g, h, mi, 0, 0, clock.Roma).UTC()
	}
	for _, c := range []struct {
		nome           string
		adesso, atteso time.Time
	}{
		{"prima, stesso giorno", roma(2026, 11, 25, 1, 0), roma(2026, 11, 25, 2, 30)},
		{"esatto", roma(2026, 11, 25, 2, 30), roma(2026, 11, 26, 2, 30)},
		{"dopo", roma(2026, 11, 25, 9, 0), roma(2026, 11, 26, 2, 30)},
		{"fine dell'anno", roma(2026, 12, 31, 23, 0), roma(2027, 1, 1, 2, 30)},
		{"verso l'ora legale", roma(2026, 3, 28, 12, 0), time.Date(2026, 3, 29, 1, 30, 0, 0, time.UTC)},
		{"verso l'ora solare", roma(2026, 10, 24, 12, 0), time.Date(2026, 10, 25, 0, 30, 0, 0, time.UTC)},
	} {
		if got := ProssimoGiro(c.adesso, ora); !got.Equal(c.atteso) {
			t.Errorf("%s: %v, atteso %v", c.nome, got, c.atteso)
		}
	}
}

// Voce OPS-28, in parte (vedi TestProblemi).
// Stato su un database vero: senza righe (configurato, chiavetta finta montata: solo «nessun backup»), poi con un
// backup vero, una prova, un'esecuzione fallita a mano e una riga del registro nel futuro. Senza chiavetta
// configurata: solo «non configurato». ConLavoroSpento spegne il banner.
func TestStatoVero(t *testing.T) {
	pn := nuovaProvaNotturno(t, adessoStato)
	ctx := context.Background()
	s, err := pn.n.Stato(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !s.Configurato || s.Chiavetta != nil || s.CeBackup || s.CeProva || !s.DaFare || s.Ora != OraPredefinita ||
		!s.Prossimo.Equal(time.Date(2026, 11, 26, 1, 30, 0, 0, time.UTC)) || len(s.Esecuzioni) != 0 || len(s.Verifiche) != 0 {
		t.Fatalf("vuoto: %+v", s)
	}
	if got := codici(s.Problemi()); !slices.Equal(got, []string{ProblemaBackupVecchio}) {
		t.Errorf("vuoto: %v", got)
	}
	e := fattoBackup(t, pn, adessoStato)
	rigaProva(t, pn.p.s.DB, db.AvvioProvaAMano, "2026-11-22", true, adessoStato.Add(time.Minute))
	pn.o.Imposta(adessoStato.Add(2 * time.Minute))
	riga(t, pn.p.s.DB, AvvioAMano, "2026-11-24", EsitoFallito, adessoStato.Add(2*time.Minute))
	if s, err = pn.n.Stato(ctx); err != nil {
		t.Fatal(err)
	}
	if !s.CeBackup || !s.UltimoBackup.CatturatoIl.Equal(s.PrimoBackup) || s.UltimoBackup.Archivio != e.Archivio ||
		!s.CeProva || !s.Prova.Riuscita || len(s.Esecuzioni) != 2 || s.Esecuzioni[0].Esito != EsitoFallito ||
		len(s.Verifiche) != 1 || !s.Verifiche[0].Riuscita || s.Verifiche[0].Destinazione != db.DestinazioneChiavetta ||
		s.DaFare || s.Rovinati != 0 || s.PiuAvanti.After(s.Adesso) {
		t.Fatalf("con i dati: %+v", s)
	}
	if got := codici(s.Problemi()); !slices.Equal(got, []string{ProblemaBackupFallito}) {
		t.Errorf("con i dati: %v", got)
	}
	// Una riga del registro nel futuro (nota 8(a)).
	if err := db.RegistraVerificaBackup(ctx, pn.p.s.DB, db.VerificaBackup{Riuscita: false, Destinazione: db.DestinazioneCartella,
		Archivio: "/inventata/futuro.tar.age", SHA256: strings.Repeat("e", 64), Byte: 1, CatturatoIl: adessoStato.Add(72 * time.Hour),
		VerificatoIl: adessoStato.Add(73 * time.Hour), Errore: "inventata"}); err != nil {
		t.Fatal(err)
	}
	if s, err = pn.n.Stato(ctx); err != nil {
		t.Fatal(err)
	}
	if !s.PiuAvanti.Equal(adessoStato.Add(73*time.Hour)) || len(s.Verifiche) != 2 || s.Verifiche[0].Riuscita {
		t.Errorf("futuro: %v %+v", s.PiuAvanti, s.Verifiche)
	}
	if !slices.Contains(codici(s.Problemi()), ProblemaOrologio) {
		t.Errorf("futuro: %v", codici(s.Problemi()))
	}
	// La chiavetta tolta: il montaggio sparisce dalla tabella.
	pn.s.sis.info = rigaRadice()
	if s, err = pn.n.Stato(ctx); err != nil || !errors.Is(s.Chiavetta, ErrNonMontata) {
		t.Errorf("chiavetta tolta: %v %v", s.Chiavetta, err)
	}
	pn.n.ConLavoroSpento()
	if s, err = pn.n.Stato(ctx); err != nil || !s.LavoroSpento || !slices.Contains(codici(s.Allarme()), ProblemaChiavetta) {
		t.Errorf("lavoro spento: %v %v %v", s.LavoroSpento, codici(s.Allarme()), err)
	}
	// Senza chiavetta configurata.
	n := NuovoNotturno(pn.p.s.DB, pn.p.s.Archivio, "", pn.o)
	if s, err = n.Stato(ctx); err != nil || s.Configurato || s.Chiavetta != nil || s.DaFare {
		t.Fatalf("non configurato: %+v %v", s, err)
	}
	if got := codici(s.Problemi()); !slices.Equal(got, []string{ProblemaNonConfigurato}) {
		t.Errorf("non configurato: %v", got)
	}
	// Un database chiuso: errore, con Configurato e LavoroSpento.
	_ = pn.p.s.DB.Chiudi()
	if s, err = pn.n.Stato(ctx); err == nil || !s.Configurato || !s.LavoroSpento {
		t.Errorf("database chiuso: %+v %v", s, err)
	}
}
