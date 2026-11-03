package buste

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

// I dati del report multi-scheda A4 (RIP-S-46), come RepairReportController (named_ids/1, from_ids/1,
// audit/2), RepairIndex.selection_by_ids/1 e RepairReport.render_html/3 dello standalone a go-baseline.

func reportFinto(b []byte, err error) func(DatiReport) ([]byte, error) {
	return func(DatiReport) ([]byte, error) { return b, err }
}

// inventario: RIP-S-46
// ids=3,1,2 come named_ids/1: le voci divise sulle virgole, senza spazi ai bordi (anche gli spazi
// Unicode, String.trim/1), le vuote tolte, ognuna un id positivo di Id.positive/1 (segno e zeri
// davanti ammessi, fino al massimo dei 64 bit; oltre, 0, negativi, decimali o lettere no: qui lo
// standalone dà 422, non 500, e D-56 non vale), i doppioni tolti tenendo il primo. La prima voce sbagliata dà «Selezione non valida: «X» non è un numero di
// riparazione.» con la voce senza spazi; dopo le voci, più di 50 id diversi danno «Troppe riparazioni
// per un solo report: N selezionate, il massimo è 50. Restringi la selezione.» (prima di cercare le
// buste). Nessuna voce: è la variante dei filtri della home.
func TestIdsDelReport(t *testing.T) {
	for _, c := range []struct {
		in  string
		ids []int64
	}{
		{"3,1,2", []int64{3, 1, 2}},
		{"1,1,01,+1", []int64{1}},
		{" 1 , 2", []int64{1, 2}},
		{" 1\n", []int64{1}},
		{"2,,1,", []int64{2, 1}},
		{"9223372036854775807", []int64{9223372036854775807}},
	} {
		ids, err := IdsDelReport(c.in)
		if err != nil || !reflect.DeepEqual(ids, c.ids) {
			t.Errorf("%q: %v %v", c.in, ids, err)
		}
	}
	for in, voce := range map[string]string{
		"abc": "abc", "1,abc,zz": "abc", "0": "0", "-1": "-1", "-99999999999999999999": "-99999999999999999999",
		"99999999999999999999": "99999999999999999999", "1,+009223372036854775808": "+009223372036854775808",
		"1.0": "1.0", "1;2": "1;2", "1 2": "1 2", `"x\`: `"x\`, "è": "è", " 7a ,1": "7a", "0x1": "0x1", "1_0": "1_0",
	} {
		_, err := IdsDelReport(in)
		var s SelezioneNonValida
		if !errors.As(err, &s) || s.Valore != voce ||
			err.Error() != "Selezione non valida: «"+voce+"» non è un numero di riparazione." {
			t.Errorf("%q: %v", in, err)
		}
	}
	for _, in := range []string{"", ",,", " , ", " "} {
		if _, err := IdsDelReport(in); !errors.Is(err, ErrSelezioneDaiFiltri) {
			t.Errorf("%q: %v", in, err)
		}
	}
	var molti []string
	for i := 1; i <= 51; i++ {
		molti = append(molti, itoa(int64(i)))
	}
	_, err := IdsDelReport(strings.Join(molti, ","))
	var tr TroppeSchede
	if !errors.As(err, &tr) || tr.N != 51 ||
		err.Error() != "Troppe riparazioni per un solo report: 51 selezionate, il massimo è 50. Restringi la selezione." {
		t.Errorf("51: %v", err)
	}
	// Cinquanta diverse più i doppioni passano; una voce sbagliata dopo la cinquantunesima conta prima.
	if ids, err := IdsDelReport(strings.Join(molti[:50], ",") + ",1,01"); err != nil || len(ids) != 50 {
		t.Errorf("50 con doppioni: %d %v", len(ids), err)
	}
	if _, err := IdsDelReport(strings.Join(molti, ",") + ",x"); !errors.As(err, new(SelezioneNonValida)) {
		t.Errorf("51 e una voce sbagliata: %v", err)
	}
	if MaxSchedeReport != 50 || NomeFileReport(1) != "schede_1.pdf" || NomeFileReport(12) != "schede_12.pdf" {
		t.Error("limite o nome del file")
	}
}

// inventario: RIP-S-46
// Il report stampa esattamente le buste chieste, nell'ordine chiesto, con il nome del negozio (o
// «Negozio») e la data di oggi nel fuso del negozio; scrive una riga repair.report_generated per busta,
// nell'ordine, con il numero, solo dopo il PDF fatto. Una busta che non c'è (o nel cestino) dà «Riparazione
// non trovata: 999, 998. Nessun report generato.» con gli id mancanti nell'ordine chiesto: niente PDF e
// niente righe. Un PDF non fatto non scrive righe.
func TestReportBusteERighe(t *testing.T) {
	b, d, o := prova(t)
	var ids []int64
	for range 3 {
		ids = append(ids, crea(t, b, modulo()).ID)
	}
	o.Imposta(time.Date(2026, 12, 31, 23, 30, 0, 0, time.UTC)) // in negozio è già l'1/1
	ordine := []int64{ids[2], ids[0], ids[1]}
	pdf, dati, err := b.Report(ctx, ordine, chi, func(dr DatiReport) ([]byte, error) {
		if n := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.report_generated'`); n != 0 {
			t.Error("righe prima del PDF")
		}
		return []byte("%PDF-finto"), nil
	})
	if err != nil || string(pdf) != "%PDF-finto" {
		t.Fatal(err, pdf)
	}
	if dati.NomeNegozio != "Negozio" || dati.Oggi != "01/01/2027" || len(dati.Schede) != 3 {
		t.Fatalf("%+v", dati)
	}
	for i, s := range dati.Schede {
		if s.ID != ordine[i] {
			t.Errorf("scheda %d: %d", i, s.ID)
		}
	}
	righe, err := d.Lettura.Query(`SELECT id_risorsa, dati, attore, risorsa, esito FROM eventi_audit
		WHERE azione = 'repair.report_generated' ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	var viste []string
	for righe.Next() {
		var id, dati, attore, risorsa, esito string
		if err := righe.Scan(&id, &dati, &attore, &risorsa, &esito); err != nil {
			t.Fatal(err)
		}
		viste = append(viste, id+" "+dati+" "+attore+" "+risorsa+" "+esito)
	}
	_ = righe.Close() // solo lettura
	var attese []string
	for i, id := range ordine {
		attese = append(attese, itoa(id)+` {"bag_number":"`+dati.Schede[i].Numero+`"} `+chi.Attore+" repair ok")
	}
	if !reflect.DeepEqual(viste, attese) {
		t.Errorf("righe:\n%q\nattese\n%q", viste, attese)
	}
	if n := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.viewed'`); n != 0 {
		t.Error("repair.viewed")
	}

	esegui(t, d, `INSERT INTO profilo_negozio (nome, condizioni_garanzia, creato_il, aggiornato_il) VALUES ('Bottega Esempio', '', '2026-01-01T00:00:00.000000Z', '2026-01-01T00:00:00.000000Z')`)
	if _, dati, _ = b.Report(ctx, ids[:1], chi, reportFinto([]byte("%PDF-x"), nil)); dati.NomeNegozio != "Bottega Esempio" {
		t.Errorf("negozio: %q", dati.NomeNegozio)
	}
	esegui(t, d, `UPDATE profilo_negozio SET nome = ''`)
	if _, dati, _ = b.Report(ctx, ids[:1], chi, reportFinto([]byte("%PDF-x"), nil)); dati.NomeNegozio != "Negozio" {
		t.Errorf("negozio vuoto: %q", dati.NomeNegozio)
	}

	prima := conta(t, d, `SELECT count(*) FROM eventi_audit`)
	esegui(t, d, `UPDATE buste SET cancellata_il = ? WHERE id = ?`, istanteProva, ids[1])
	mai := func(DatiReport) ([]byte, error) { t.Error("PDF fatto"); return nil, nil }
	for _, c := range []struct {
		chiesti, mancanti []int64
	}{
		{[]int64{ids[0], 999999}, []int64{999999}},
		{[]int64{999, 998, ids[0]}, []int64{999, 998}},
		{[]int64{ids[1], ids[0]}, []int64{ids[1]}},
	} {
		_, _, err := b.Report(ctx, c.chiesti, chi, mai)
		var nt SchedeNonTrovate
		if !errors.As(err, &nt) || !reflect.DeepEqual(nt.ID, c.mancanti) {
			t.Errorf("%v: %v", c.chiesti, err)
		}
	}
	_, _, err = b.Report(ctx, []int64{999, 998, ids[0]}, chi, mai)
	if err == nil || err.Error() != "Riparazione non trovata: 999, 998. Nessun report generato." {
		t.Errorf("testo: %v", err)
	}
	guasto := errors.New("motore guasto")
	if _, _, err := b.Report(ctx, ids[:1], chi, reportFinto(nil, guasto)); !errors.Is(err, ErrPDFNonFatto) || !errors.Is(err, guasto) {
		t.Errorf("motore: %v", err)
	}
	if dopo := conta(t, d, `SELECT count(*) FROM eventi_audit`); dopo != prima {
		t.Errorf("righe scritte: %d → %d", prima, dopo)
	}
	// La prima riga che non si scrive ferma il documento (audit/2 con reduce_while): le righe già
	// scritte restano, come nello standalone.
	esegui(t, d, `CREATE TRIGGER no_report BEFORE INSERT ON eventi_audit WHEN NEW.azione = 'repair.report_generated'
		AND NEW.id_risorsa = '`+itoa(ids[2])+`' BEGIN SELECT RAISE(ABORT, 'no'); END`)
	if _, _, err := b.Report(ctx, []int64{ids[0], ids[2]}, chi, reportFinto([]byte("%PDF-x"), nil)); !errors.Is(err, ErrPDFNonFatto) {
		t.Errorf("riga non scritta: %v", err)
	}
	if dopo := conta(t, d, `SELECT count(*) FROM eventi_audit`); dopo != prima+1 {
		t.Errorf("righe dopo il guasto: %d → %d", prima, dopo)
	}
}

// inventario: RIP-S-46
// I campi di una scheda come render_html/3: numero, cliente e telefono salvati sulla busta («—» se
// vuoti), lo stato con gli spazi (la classe con i trattini; lo schema del Go non ammette lo stato vuoto, il «—» di
// status_badge/1 non si raggiunge), la data della riparazione
// e la consegna (il testo scritto, la data, «—») gg/mm/aaaa, oreficeria e artigiano solo con il nome,
// preventivo e prezzo finale «€ testo» o «€ 12,50» dai soli centesimi, con «→» solo se ci sono tutti e
// due, la descrizione («—»), note e note interne solo se non vuote, le date di pronta e ritiro.
func TestReportCampiDellaScheda(t *testing.T) {
	b, d, _ := prova(t)
	c := crea(t, b, modulo())
	esegui(t, d, `UPDATE buste SET stato = 'in_attesa_materiali', data_prevista = '2026-11-20', oreficeria_id = 2, artigiano_id = 1,
		preventivo_testo = '400/500 salvo pietre', prezzo_centesimi = 45000, note_pubbliche = 'Pronta venerdì', note_interne = ' ',
		data_pronta = '2026-11-08', data_consegna = '2026-11-10' WHERE id = ?`, c.ID)
	_, dati, err := b.Report(ctx, []int64{c.ID}, chi, reportFinto([]byte("%PDF-x"), nil))
	if err != nil {
		t.Fatal(err)
	}
	atteso := SchedaReport{ID: c.ID, Numero: c.Numero, NomeCliente: "Cliente Prova", Telefono: "333 000 0001",
		Stato: "in attesa materiali", ClasseStato: "in-attesa-materiali", DataRiparazione: "31/10/2026", Consegna: "20/11/2026",
		Oreficeria: "Oreficeria Esempio", Artigiano: "Artigiano Esempio", Importi: "€ 400/500 salvo pietre → € 450,00",
		Descrizione: "Anello da stringere", Note: "Pronta venerdì", NoteInterne: " ", Pronta: "08/11/2026", Ritirata: "10/11/2026"}
	if dati.Schede[0] != atteso {
		t.Errorf("%+v\natteso\n%+v", dati.Schede[0], atteso)
	}

	esegui(t, d, `UPDATE buste SET stato = 'annullata', data_prevista_testo = 'fine mese', oreficeria_id = NULL, nome_cliente = '',
		telefono_cliente = '', descrizione = '', preventivo_testo = NULL, preventivo_centesimi = 12345, prezzo_centesimi = NULL,
		note_pubbliche = '', note_interne = NULL, data_pronta = NULL WHERE id = ?`, c.ID)
	esegui(t, d, `UPDATE artigiani SET nome = '' WHERE id = 1`)
	_, dati, _ = b.Report(ctx, []int64{c.ID}, chi, reportFinto([]byte("%PDF-x"), nil))
	atteso = SchedaReport{ID: c.ID, Numero: c.Numero, NomeCliente: "—", Telefono: "—", Stato: "annullata", ClasseStato: "annullata",
		DataRiparazione: "31/10/2026", Consegna: "fine mese", Importi: "€ 123,45", Descrizione: "—", Ritirata: "10/11/2026"}
	if dati.Schede[0] != atteso {
		t.Errorf("%+v\natteso\n%+v", dati.Schede[0], atteso)
	}

	esegui(t, d, `UPDATE buste SET data_prevista_testo = '', data_prevista = NULL, preventivo_centesimi = NULL,
		prezzo_testo = 'da definire', oreficeria_id = 1 WHERE id = ?`, c.ID)
	_, dati, _ = b.Report(ctx, []int64{c.ID}, chi, reportFinto([]byte("%PDF-x"), nil))
	if s := dati.Schede[0]; s.Consegna != "—" || s.Importi != "€ da definire" || s.Oreficeria != "Il negozio di prova" {
		t.Errorf("%+v", s)
	}
	// Un testo vuoto non è un prezzo (price_source/2): valgono i centesimi.
	esegui(t, d, `UPDATE buste SET preventivo_testo = '', preventivo_centesimi = 500, prezzo_testo = '', prezzo_centesimi = 700
		WHERE id = ?`, c.ID)
	if _, dati, _ = b.Report(ctx, []int64{c.ID}, chi, reportFinto([]byte("%PDF-x"), nil)); dati.Schede[0].Importi != "€ 5,00 → € 7,00" {
		t.Errorf("testi vuoti: %q", dati.Schede[0].Importi)
	}
	esegui(t, d, `UPDATE buste SET preventivo_centesimi = NULL, prezzo_testo = NULL, prezzo_centesimi = NULL WHERE id = ?`, c.ID)
	if _, dati, _ = b.Report(ctx, []int64{c.ID}, chi, reportFinto([]byte("%PDF-x"), nil)); dati.Schede[0].Importi != "" {
		t.Errorf("senza prezzi: %q", dati.Schede[0].Importi)
	}
}
