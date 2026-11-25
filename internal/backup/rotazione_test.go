package backup

import (
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/clock"
)

// La scelta della rotazione 7 + 4 (D-114), solo sui nomi: giorni e settimane nel fuso del negozio, con il
// nome in UTC. Date inventate.

// alle è l'istante delle h:mm di Roma del giorno g.
func alle(a int, m time.Month, g, h, mm int) time.Time {
	return time.Date(a, m, g, h, mm, 0, 0, clock.Roma).UTC()
}

// ogniNotte dà un backup alle 02:30 di Roma per ogni giorno da da a a (compresi).
func ogniNotte(da, a time.Time) []time.Time {
	var v []time.Time
	for g := da.In(clock.Roma); !g.After(a); g = g.AddDate(0, 0, 1) { // giorni di Roma
		v = append(v, time.Date(g.Year(), g.Month(), g.Day(), 2, 30, 0, 0, clock.Roma).UTC())
	}
	return v
}

// scelti dà i giorni di Roma («2026-11-25 02:30») degli archivi che daTenere tiene, dal più nuovo.
func scelti(tt []time.Time, nonVerificati ...time.Time) []string {
	var aa []archivioInCartella
	for _, t := range tt {
		aa = append(aa, archivioInCartella{voce: voceCartella{nome: NomeArchivio(t)}, t: t, verificato: true, registrato: true})
	}
	for _, t := range nonVerificati {
		aa = append(aa, archivioInCartella{voce: voceCartella{nome: NomeArchivio(t)}, t: t, registrato: true})
	}
	sort.Slice(aa, func(i, j int) bool { return aa[i].t.After(aa[j].t) })
	tieni := daTenere(aa, clock.Roma)
	var v []string
	for _, a := range aa {
		if tieni[a.voce.nome] {
			v = append(v, a.t.In(clock.Roma).Format("2006-01-02 15:04"))
		}
	}
	return v
}

// Voce OPS-28, in parte: niente segno «inventario:» (la voce chiude con il lavoro notturno, 124-125).
// D-114: 7 giornalieri, poi l'ultimo di ognuna delle 4 settimane ISO prima di quella del più vecchio
// giornaliero; fra mesi, al cambio dell'ora legale (25/10/2026 e 29/3/2026) e a cavallo dell'anno (la
// settimana 2026-W53 va dal 28/12/2026 al 3/1/2027).
func TestRotazioneScelta(t *testing.T) {
	casi := []struct {
		nome          string
		archivi       []time.Time
		nonVerificati []time.Time
		attesi        string
	}{
		{"tutte le notti da settembre a novembre (ottobre-novembre, ora legale finita il 25/10)",
			ogniNotte(alle(2026, 9, 1, 0, 0), alle(2026, 11, 25, 0, 0)), nil,
			"2026-11-25 02:30,2026-11-24 02:30,2026-11-23 02:30,2026-11-22 02:30,2026-11-21 02:30,2026-11-20 02:30,2026-11-19 02:30," +
				"2026-11-15 02:30,2026-11-08 02:30,2026-11-01 02:30,2026-10-25 02:30"},
		{"a cavallo dell'anno",
			ogniNotte(alle(2026, 12, 1, 0, 0), alle(2027, 1, 10, 0, 0)), nil,
			"2027-01-10 02:30,2027-01-09 02:30,2027-01-08 02:30,2027-01-07 02:30,2027-01-06 02:30,2027-01-05 02:30,2027-01-04 02:30," +
				"2027-01-03 02:30,2026-12-27 02:30,2026-12-20 02:30,2026-12-13 02:30"},
		{"giornalieri a cavallo dell'anno, settimana 53 dentro i giornalieri",
			ogniNotte(alle(2026, 12, 1, 0, 0), alle(2027, 1, 3, 0, 0)), nil,
			"2027-01-03 02:30,2027-01-02 02:30,2027-01-01 02:30,2026-12-31 02:30,2026-12-30 02:30,2026-12-29 02:30,2026-12-28 02:30," +
				"2026-12-27 02:30,2026-12-20 02:30,2026-12-13 02:30,2026-12-06 02:30"},
		{"due backup nella notte del cambio d'ora: 02:30 CEST e 02:30 CET sono lo stesso giorno",
			[]time.Time{alle(2026, 10, 24, 2, 30), time.Date(2026, 10, 25, 0, 30, 0, 0, time.UTC), time.Date(2026, 10, 25, 1, 30, 0, 0, time.UTC)}, nil,
			"2026-10-25 02:30,2026-10-24 02:30"},
		{"inizio dell'ora legale: le 00:30 del 29/3 (23:30Z del 28) e le 03:30 sono dello stesso giorno",
			[]time.Time{alle(2026, 3, 28, 1, 30), time.Date(2026, 3, 28, 23, 30, 0, 0, time.UTC), alle(2026, 3, 29, 3, 30), alle(2026, 3, 30, 2, 30)}, nil,
			"2026-03-30 02:30,2026-03-29 03:30,2026-03-28 01:30"},
		{"nome del giorno prima in UTC: 00:30 di Roma è del giorno dopo",
			[]time.Time{time.Date(2026, 11, 24, 23, 30, 0, 0, time.UTC), alle(2026, 11, 25, 2, 30), alle(2026, 11, 24, 2, 30)}, nil,
			"2026-11-25 02:30,2026-11-24 02:30"},
		{"pochi backup: tutti", []time.Time{alle(2026, 11, 1, 2, 30), alle(2026, 6, 1, 2, 30), alle(2025, 1, 1, 2, 30)}, nil,
			"2026-11-01 02:30,2026-06-01 02:30,2025-01-01 02:30"},
		{"giorni saltati (apparecchio spento): contano i giorni con un backup",
			[]time.Time{alle(2026, 11, 25, 2, 30), alle(2026, 11, 20, 2, 30), alle(2026, 11, 10, 2, 30), alle(2026, 11, 9, 2, 30),
				alle(2026, 11, 2, 2, 30), alle(2026, 10, 20, 2, 30), alle(2026, 10, 19, 2, 30), alle(2026, 10, 12, 2, 30),
				alle(2026, 10, 11, 2, 30), alle(2026, 10, 5, 2, 30), alle(2026, 9, 1, 2, 30), alle(2026, 8, 1, 2, 30), alle(2026, 7, 1, 2, 30)},
			nil,
			"2026-11-25 02:30,2026-11-20 02:30,2026-11-10 02:30,2026-11-09 02:30,2026-11-02 02:30,2026-10-20 02:30,2026-10-19 02:30," +
				"2026-10-12 02:30,2026-10-11 02:30,2026-09-01 02:30,2026-08-01 02:30"},
		{"verifiche fallite: non contano fra i 7, restano solo dentro la finestra dei giornalieri",
			ogniNotte(alle(2026, 11, 1, 0, 0), alle(2026, 11, 8, 0, 0)),
			[]time.Time{alle(2026, 11, 8, 3, 30), alle(2026, 11, 9, 2, 30), alle(2026, 11, 2, 0, 10), alle(2026, 11, 1, 23, 50), alle(2026, 10, 20, 2, 30)},
			"2026-11-09 02:30,2026-11-08 03:30,2026-11-08 02:30,2026-11-07 02:30,2026-11-06 02:30,2026-11-05 02:30,2026-11-04 02:30," +
				"2026-11-03 02:30,2026-11-02 02:30,2026-11-02 00:10,2026-11-01 02:30"},
		{"nessun verificato: niente", nil, []time.Time{alle(2026, 11, 9, 2, 30)}, ""},
	}
	for _, c := range casi {
		t.Run(c.nome, func(t *testing.T) {
			if v := strings.Join(scelti(c.archivi, c.nonVerificati...), ","); v != c.attesi {
				t.Errorf("tenuti\n%s\nattesi\n%s", v, c.attesi)
			}
		})
	}
	// Mai più di 7 + 4.
	if n := len(scelti(ogniNotte(alle(2024, 1, 1, 0, 0), alle(2026, 11, 25, 0, 0)))); n != GiornalieriDaTenere+SettimanaliDaTenere {
		t.Errorf("tenuti %d", n)
	}
}
