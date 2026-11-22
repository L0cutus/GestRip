package buste

import (
	"errors"
	"strings"
	"testing"

	"github.com/L0cutus/gestrip/internal/audit"
)

// La pagina della raccolta dei dettati (slice 114; RIP-H-49, RIP-H-50), come Training.status/0, set_collect/2, refill/1,
// exclude/2 ed exclude_customer/2 dello standalone a go-baseline; i casi da assist/training_test.exs («the budget», «a new
// lot of the budget», «the row goes with the bag», «the objection of a customer»). Dettati, nomi e telefoni inventati.

// azioniRaccolta sono le righe d'audit della pagina, nell'ordine: azione|risorsa|id|esito|dati|attore.
func azioniRaccolta(t *testing.T, b *Buste) []string {
	t.Helper()
	rows, err := b.d.Lettura.Query(`SELECT azione || '|' || risorsa || '|' || id_risorsa || '|' || esito || '|' || dati || '|' || attore
		FROM eventi_audit WHERE azione LIKE 'assist.training%' ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		out = append(out, s)
	}
	return out
}

func statoRaccolta(t *testing.T, b *Buste) StatoRaccolta {
	t.Helper()
	s, err := b.StatoRaccolta(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// inventario: RIP-H-49
// L'interruttore: spenta all'inizio con il lotto intero (la chiave del lotto non c'è ancora); la prima accensione scrive il
// lotto di 500; spegnere e riaccendere non lo ricarica; ogni scatto è nell'audit con l'attore e soli numeri.
func TestRaccoltaInterruttore(t *testing.T) {
	b, _, _ := prova(t)
	if s := statoRaccolta(t, b); s != (StatoRaccolta{Accesa: false, Rimaste: 500, Lotto: 500}) {
		t.Errorf("stato iniziale %+v", s)
	}
	if impostazione(t, b, ChiaveRaccoltaRimaste) != "(assente)" {
		t.Fatal("lotto già scritto")
	}
	if err := b.ImpostaRaccolta(ctx, true, chi); err != nil {
		t.Fatal(err)
	}
	if a, r := impostazione(t, b, ChiaveRaccoltaAccesa), impostazione(t, b, ChiaveRaccoltaRimaste); a != "1" || r != "500" {
		t.Errorf("dopo l'accensione: accesa %s, rimaste %s", a, r)
	}
	crea(t, b, conDettato("QR-I1"))
	if s := statoRaccolta(t, b); s != (StatoRaccolta{Accesa: true, Rimaste: 499, Lotto: 500, Raccolti: 1}) {
		t.Errorf("dopo una busta %+v", s)
	}
	if err := b.ImpostaRaccolta(ctx, false, chi); err != nil {
		t.Fatal(err)
	}
	crea(t, b, conDettato("QR-I2"))
	if err := b.ImpostaRaccolta(ctx, true, chi); err != nil {
		t.Fatal(err)
	}
	if s := statoRaccolta(t, b); s != (StatoRaccolta{Accesa: true, Rimaste: 499, Lotto: 500, Raccolti: 1}) {
		t.Errorf("spenta e riaccesa %+v", s)
	}
	atteso := []string{
		`assist.training_collect|assist|training|ok|{"on":true,"remaining":500}|titolare@esempio.test`,
		`assist.training_collect|assist|training|ok|{"on":false,"remaining":499}|titolare@esempio.test`,
		`assist.training_collect|assist|training|ok|{"on":true,"remaining":499}|titolare@esempio.test`,
	}
	if got := azioniRaccolta(t, b); strings.Join(got, "\n") != strings.Join(atteso, "\n") {
		t.Errorf("audit:\n%s\natteso:\n%s", strings.Join(got, "\n"), strings.Join(atteso, "\n"))
	}
}

// inventario: RIP-H-49
// L'accensione è rifiutata a lotto esaurito (anche con un lotto illeggibile o negativo, che vale 0, come remaining/0):
// niente si scrive. Spegnere si può sempre, anche a lotto esaurito.
func TestRaccoltaAccensioneLottoEsaurito(t *testing.T) {
	for _, rimaste := range []string{"0", "-3", "tanti", " 5", "", "00"} {
		t.Run(rimaste, func(t *testing.T) {
			b, _, _ := prova(t)
			esegui(t, b.d, `INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES ('assist.training.remaining', ?, ?)`, rimaste, istanteProva)
			if err := b.ImpostaRaccolta(ctx, true, chi); !errors.Is(err, ErrLottoEsaurito) {
				t.Fatalf("accensione: %v", err)
			}
			if a := impostazione(t, b, ChiaveRaccoltaAccesa); a != "(assente)" || len(azioniRaccolta(t, b)) != 0 {
				t.Errorf("rifiuto che scrive: accesa %s, audit %v", a, azioniRaccolta(t, b))
			}
			if s := statoRaccolta(t, b); s.Rimaste != 0 || s.Accesa {
				t.Errorf("stato %+v", s)
			}
			if err := b.ImpostaRaccolta(ctx, false, chi); err != nil {
				t.Fatal(err)
			}
			if got := azioniRaccolta(t, b); len(got) != 1 || !strings.Contains(got[0], `{"on":false,"remaining":0}`) {
				t.Errorf("spegnimento: %v", got)
			}
		})
	}
	// Un numero con il segno + è un numero (Integer.parse/1).
	b, _, _ := prova(t)
	esegui(t, b.d, `INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES ('assist.training.remaining', '+7', ?)`, istanteProva)
	if err := b.ImpostaRaccolta(ctx, true, chi); err != nil {
		t.Errorf("+7: %v", err)
	}
	if s := statoRaccolta(t, b); s.Rimaste != 7 {
		t.Errorf("+7: %+v", s)
	}
}

// inventario: RIP-H-49
// L'ultima busta del lotto spegne la raccolta; l'accensione è rifiutata; il nuovo lotto (solo a lotto finito, mai prima del
// primo) rimette 500 e accende, con l'audit assist.training_refill e soli numeri.
func TestRaccoltaNuovoLotto(t *testing.T) {
	b, _, _ := prova(t)
	if err := b.NuovoLottoRaccolta(ctx, chi); !errors.Is(err, ErrLottoInCorso) {
		t.Errorf("prima del primo lotto: %v", err)
	}
	if err := b.ImpostaRaccolta(ctx, true, chi); err != nil {
		t.Fatal(err)
	}
	if err := b.NuovoLottoRaccolta(ctx, chi); !errors.Is(err, ErrLottoInCorso) {
		t.Errorf("con 500 rimaste: %v", err)
	}
	esegui(t, b.d, `UPDATE impostazioni SET valore = '1' WHERE chiave = 'assist.training.remaining'`)
	if err := b.NuovoLottoRaccolta(ctx, chi); !errors.Is(err, ErrLottoInCorso) {
		t.Errorf("con 1 rimasta: %v", err)
	}
	crea(t, b, conDettato("QR-L1"))
	if s := statoRaccolta(t, b); s.Accesa || s.Rimaste != 0 {
		t.Fatalf("lotto finito: %+v", s)
	}
	if err := b.ImpostaRaccolta(ctx, true, chi); !errors.Is(err, ErrLottoEsaurito) {
		t.Errorf("accensione a lotto finito: %v", err)
	}
	prima := len(azioniRaccolta(t, b))
	if err := b.NuovoLottoRaccolta(ctx, chi); err != nil {
		t.Fatal(err)
	}
	if s := statoRaccolta(t, b); s != (StatoRaccolta{Accesa: true, Rimaste: 500, Lotto: 500, Raccolti: 1}) {
		t.Errorf("nuovo lotto %+v", s)
	}
	got := azioniRaccolta(t, b)
	if len(got) != prima+1 || got[prima] != `assist.training_refill|assist|training|ok|{"on":true,"remaining":500}|titolare@esempio.test` {
		t.Errorf("audit %v", got)
	}
	crea(t, b, conDettato("QR-L2"))
	if s := statoRaccolta(t, b); s.Rimaste != 499 || s.Raccolti != 2 {
		t.Errorf("dopo il nuovo lotto %+v", s)
	}
}

// inventario: RIP-H-50
// L'esclusione di una busta per numero (art. 21): spazi e zeri davanti tolti (slice 220), anche nel cestino; il dettato
// si cancella, la busta resta; una busta senza dettato è «non raccolta», una che non c'è «non trovata», e nessuno dei due
// scrive. Audit assist.training_excluded sulla busta, senza dati.
func TestRaccoltaEscludiBusta(t *testing.T) {
	b, d, _ := prova(t)
	accendi(t, b, "500")
	una := crea(t, b, conDettato("QR-X1"))
	altra := crea(t, b, conDettato("QR-X2"))
	amano := crea(t, b, con(modulo(), func(a *Accettazione) { a.CodiceQR = "QR-X3" }))
	cestino := crea(t, b, conDettato("QR-X4"))
	esegui(t, d, `UPDATE buste SET cancellata_il = ? WHERE id = ?`, istanteProva, cestino.ID)

	if _, err := b.EscludiBustaDallaRaccolta(ctx, " \t0"+una.Numero+" ", chi); err != nil {
		t.Fatalf("esclusione: %v", err)
	}
	if n := conta(t, d, `SELECT count(*) FROM dettati_addestramento WHERE busta_id = ?`, una.ID); n != 0 {
		t.Error("dettato rimasto")
	}
	if conta(t, d, `SELECT count(*) FROM buste WHERE id = ?`, una.ID) != 1 || righeRaccolta(t, b) != 2 {
		t.Errorf("busta sparita o altre righe toccate: %d righe", righeRaccolta(t, b))
	}
	if _, err := b.EscludiBustaDallaRaccolta(ctx, cestino.Numero, chi); err != nil {
		t.Errorf("nel cestino: %v", err)
	}
	for _, c := range []struct {
		numero string
		err    error
	}{{una.Numero, ErrBustaNonRaccolta}, {amano.Numero, ErrBustaNonRaccolta}, {"9999/99", ErrBustaNonTrovata}, {"", ErrBustaNonTrovata},
		{"  ", ErrBustaNonTrovata}, {"QR-X2", ErrBustaNonTrovata}} {
		if _, err := b.EscludiBustaDallaRaccolta(ctx, c.numero, chi); !errors.Is(err, c.err) {
			t.Errorf("%q: %v invece di %v", c.numero, err, c.err)
		}
	}
	if righeRaccolta(t, b) != 1 || conta(t, d, `SELECT count(*) FROM dettati_addestramento WHERE busta_id = ?`, altra.ID) != 1 {
		t.Error("la busta dell'altra riga ha perso il dettato")
	}
	atteso := []string{
		"assist.training_excluded|repair|" + itoa(una.ID) + "|ok|{}|titolare@esempio.test",
		"assist.training_excluded|repair|" + itoa(cestino.ID) + "|ok|{}|titolare@esempio.test",
	}
	if got := azioniRaccolta(t, b); strings.Join(got, "\n") != strings.Join(atteso, "\n") {
		t.Errorf("audit %v", got)
	}
	if n := conta(t, d, `SELECT count(*) FROM dettati_esclusioni`); n != 0 {
		t.Errorf("esclusione di una busta che esclude il cliente: %d", n)
	}
}

// inventario: RIP-H-50
// L'opposizione di un cliente, trovato dal numero di una sua busta (anche nel cestino): i dettati di tutte le sue buste (anche
// quelle nel cestino) si cancellano, gli altri restano; la sua prossima busta non si raccoglie e non consuma il lotto; una
// seconda volta non cancella niente; un numero che non c'è è «non trovata». Audit assist.training_customer_excluded sul
// cliente con il solo numero dei cancellati.
func TestRaccoltaEscludiCliente(t *testing.T) {
	b, d, _ := prova(t)
	accendi(t, b, "500")
	prima := crea(t, b, conDettato("QR-C1"))
	seconda := crea(t, b, conDettato("QR-C2"))
	nelCestino := crea(t, b, conDettato("QR-C3"))
	esegui(t, d, `UPDATE buste SET cancellata_il = ? WHERE id = ?`, istanteProva, nelCestino.ID)
	altro := crea(t, b, con(conDettato("QR-C4"), func(a *Accettazione) { a.NomeCliente, a.TelefonoCliente = "Altro Inventato", "333 000 0999" }))
	if prima.ClienteID != seconda.ClienteID || prima.ClienteID != nelCestino.ClienteID || altro.ClienteID == prima.ClienteID {
		t.Fatal("clienti delle buste di prova")
	}

	_, n, err := b.EscludiClienteDallaRaccolta(ctx, " 0"+seconda.Numero+" ", chi)
	if err != nil || n != 3 {
		t.Fatalf("esclusione: %d %v", n, err)
	}
	if righeRaccolta(t, b) != 1 || conta(t, d, `SELECT count(*) FROM dettati_addestramento WHERE busta_id = ?`, altro.ID) != 1 {
		t.Errorf("righe rimaste %d", righeRaccolta(t, b))
	}
	if conta(t, d, `SELECT count(*) FROM buste WHERE id IN (?, ?, ?)`, prima.ID, seconda.ID, nelCestino.ID) != 3 {
		t.Error("buste sparite")
	}
	if s := statoRaccolta(t, b); s.Esclusi != 1 || s.Raccolti != 1 {
		t.Errorf("stato %+v", s)
	}
	// La prossima busta del cliente non si raccoglie e non consuma il lotto.
	rimaste := impostazione(t, b, ChiaveRaccoltaRimaste)
	if got := crea(t, b, conDettato("QR-C5")); got.Raccolta != RaccoltaEsclusa || impostazione(t, b, ChiaveRaccoltaRimaste) != rimaste {
		t.Errorf("busta futura: %v, rimaste %s", got.Raccolta, impostazione(t, b, ChiaveRaccoltaRimaste))
	}
	// Dal numero di una busta nel cestino; una seconda volta: niente da cancellare, nessuna seconda esclusione.
	if _, n, err := b.EscludiClienteDallaRaccolta(ctx, nelCestino.Numero, chi); err != nil || n != 0 {
		t.Errorf("seconda volta: %d %v", n, err)
	}
	for _, numero := range []string{"9999/99", "", "QR-C1"} {
		if _, _, err := b.EscludiClienteDallaRaccolta(ctx, numero, chi); !errors.Is(err, ErrBustaNonTrovata) {
			t.Errorf("%q: %v", numero, err)
		}
	}
	if n := conta(t, d, `SELECT count(*) FROM dettati_esclusioni`); n != 1 {
		t.Errorf("esclusioni %d", n)
	}
	atteso := []string{
		"assist.training_customer_excluded|customer|" + itoa(prima.ClienteID) + `|ok|{"cancellati":3}|titolare@esempio.test`,
		"assist.training_customer_excluded|customer|" + itoa(prima.ClienteID) + `|ok|{"cancellati":0}|titolare@esempio.test`,
	}
	if got := azioniRaccolta(t, b); strings.Join(got, "\n") != strings.Join(atteso, "\n") {
		t.Errorf("audit %v", got)
	}
}

// inventario: RIP-H-50
// Le cancellazioni sono irreversibili: tutto in una transazione. Se l'audit non si scrive, niente è cancellato, nessun
// cliente è escluso e l'errore torna a chi chiama; lo stesso per l'interruttore e il nuovo lotto.
func TestRaccoltaTuttoONiente(t *testing.T) {
	b, d, _ := prova(t)
	accendi(t, b, "5")
	due := crea(t, b, conDettato("QR-T2"))
	if righeRaccolta(t, b) != 1 {
		t.Fatalf("righe %d", righeRaccolta(t, b))
	}
	esegui(t, d, `UPDATE impostazioni SET valore = '0' WHERE chiave = 'assist.training.remaining'`)
	esegui(t, d, `CREATE TRIGGER niente_audit BEFORE INSERT ON eventi_audit WHEN NEW.azione LIKE 'assist.training%' BEGIN SELECT RAISE(ABORT, 'no'); END`)
	if _, err := b.EscludiBustaDallaRaccolta(ctx, due.Numero, chi); err == nil || errors.Is(err, ErrBustaNonRaccolta) {
		t.Errorf("busta: %v", err)
	}
	if _, _, err := b.EscludiClienteDallaRaccolta(ctx, due.Numero, chi); err == nil {
		t.Error("cliente senza errore")
	}
	if righeRaccolta(t, b) != 1 || conta(t, d, `SELECT count(*) FROM dettati_esclusioni`) != 0 {
		t.Errorf("cancellazione a metà: righe %d", righeRaccolta(t, b))
	}
	if err := b.ImpostaRaccolta(ctx, false, chi); err == nil {
		t.Error("spegnimento senza errore")
	}
	if err := b.NuovoLottoRaccolta(ctx, chi); err == nil {
		t.Error("nuovo lotto senza errore")
	}
	if a, r := impostazione(t, b, ChiaveRaccoltaAccesa), impostazione(t, b, ChiaveRaccoltaRimaste); a != "1" || r != "0" {
		t.Errorf("impostazioni cambiate: accesa %s, rimaste %s", a, r)
	}
}

// inventario: RIP-H-50
// Solo numeri, mai un dettato: né nell'audit né negli errori; la busta si cerca per numero, non per codice QR o testo.
func TestRaccoltaPaginaSoloNumeri(t *testing.T) {
	b, d, _ := prova(t)
	accendi(t, b, "500")
	c := crea(t, b, conDettato("QR-N1"))
	altro := audit.Contesto{Attore: "titolare@esempio.test"}
	if _, err := b.EscludiBustaDallaRaccolta(ctx, c.Numero, altro); err != nil {
		t.Fatal(err)
	}
	_, _, err := b.EscludiClienteDallaRaccolta(ctx, "Ofelia Inventata", altro)
	if err == nil || strings.Contains(err.Error(), "Ofelia") {
		t.Errorf("errore %v", err)
	}
	for _, s := range []string{"Ofelia", "anello", "333", "modello-di-prova"} {
		if n := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione LIKE 'assist.%' AND (dati LIKE '%' || ? || '%' OR id_risorsa LIKE '%' || ? || '%')`, s, s); n != 0 {
			t.Errorf("%q nell'audit", s)
		}
	}
}

// bustaD109 è una busta raccolta con il numero interno e il codice QR dati, di un cliente suo (nome e telefono diversi).
func bustaD109(t *testing.T, b *Buste, numero, qr, telefono string) Creata {
	t.Helper()
	return crea(t, b, con(conDettato(qr), func(a *Accettazione) {
		a.Numero, a.NomeCliente, a.TelefonoCliente = numero, "Cliente "+telefono, "333 000 "+telefono
		a.GemelleConfermate = EtichettaScritta(qr, AnnoDiAccettazione(a.DataAccettazione, b.o.Adesso()))
	}))
}

func righeDi(t *testing.T, b *Buste, id int64) int {
	t.Helper()
	return conta(t, b.d, `SELECT count(*) FROM dettati_addestramento WHERE busta_id = ?`, id)
}

// inventario: RIP-H-50
// D-109 (Stefano, 22/11/2026): il numero scritto è quello dell'etichetta («0045/26», «0045», «45»), come nella consegna e
// nel lotto (D-89, D-95); il numero interno vale solo per le buste senza etichetta RIP-…. Un numero d'etichetta uguale al
// numero interno di un'altra busta tocca solo la busta con l'etichetta; più buste che corrispondono (gemelle) sono un
// rifiuto e nessuna cancellazione; l'esito dice il numero visibile della busta trovata.
func TestRaccoltaEsclusioneNumeroEtichetta(t *testing.T) {
	b, _, _ := prova(t)
	accendi(t, b, "500")
	conEtichetta := bustaD109(t, b, "3/26", "RIP-2026-0045", "0001")
	interno45 := bustaD109(t, b, "45/26", "RIP-2026-0007", "0002") // numero interno uguale all'etichetta dell'altra
	senza := bustaD109(t, b, "12/26", "SINT-D109-SENZA", "0003")
	gemella1 := bustaD109(t, b, "6/26", "RIP-2026-0060", "0004")
	gemella2 := bustaD109(t, b, "7/26", "RIP-2026-0060", "0005")

	// (a) gemelle: rifiuto con la frase della D-89, niente cancellato, nessun audit; anche per il cliente.
	for _, scritto := range []string{"0060/26", " 0060 ", "60"} {
		atteso := "Più buste corrispondono a «" + strings.TrimSpace(scritto) + "»: scrivi il numero con l'anno o usa il QR."
		if _, err := b.EscludiBustaDallaRaccolta(ctx, scritto, chi); !errors.Is(err, ErrPiuBuste) || err.Error() != atteso {
			t.Errorf("busta %q: %v", scritto, err)
		}
		if _, _, err := b.EscludiClienteDallaRaccolta(ctx, scritto, chi); !errors.Is(err, ErrPiuBuste) || err.Error() != atteso {
			t.Errorf("cliente %q: %v", scritto, err)
		}
	}
	if righeDi(t, b, gemella1.ID) != 1 || righeDi(t, b, gemella2.ID) != 1 || len(azioniRaccolta(t, b)) != 0 ||
		conta(t, b.d, `SELECT count(*) FROM dettati_esclusioni`) != 0 {
		t.Fatal("le gemelle hanno perso qualcosa")
	}
	// (b) «0045/26» e «45/26» sono l'etichetta della prima busta, non il numero interno della seconda.
	visibile, err := b.EscludiBustaDallaRaccolta(ctx, "0045/26", chi)
	if err != nil || visibile != "0045/26" {
		t.Fatalf("0045/26: %q %v", visibile, err)
	}
	if righeDi(t, b, conEtichetta.ID) != 0 || righeDi(t, b, interno45.ID) != 1 {
		t.Error("toccata la busta sbagliata")
	}
	if visibile, err := b.EscludiBustaDallaRaccolta(ctx, "45/26", chi); !errors.Is(err, ErrBustaNonRaccolta) || visibile != "0045/26" {
		t.Errorf("45/26: %q %v", visibile, err)
	}
	if righeDi(t, b, interno45.ID) != 1 {
		t.Error("45/26 ha toccato la busta con il numero interno 45/26")
	}
	// Il numero interno di una busta con l'etichetta non la trova più.
	if _, err := b.EscludiBustaDallaRaccolta(ctx, "3/26", chi); !errors.Is(err, ErrBustaNonTrovata) {
		t.Errorf("3/26: %v", err)
	}
	// (c) una busta senza etichetta si trova con il numero interno, e l'esito lo dice con «N.».
	if visibile, err := b.EscludiBustaDallaRaccolta(ctx, " 012/26 ", chi); err != nil || visibile != "N. 12/26" {
		t.Errorf("senza etichetta: %q %v", visibile, err)
	}
	if righeDi(t, b, senza.ID) != 0 {
		t.Error("dettato della busta senza etichetta rimasto")
	}
	// (d) il cliente dalla sua etichetta scritta corta: il numero visibile e il conto.
	visibile, n, err := b.EscludiClienteDallaRaccolta(ctx, "7", chi)
	if err != nil || visibile != "0007/26" || n != 1 {
		t.Errorf("cliente: %q %d %v", visibile, n, err)
	}
	if righeDi(t, b, interno45.ID) != 0 || righeDi(t, b, gemella1.ID) != 1 || righeDi(t, b, gemella2.ID) != 1 {
		t.Error("esclusione del cliente sulle buste sbagliate")
	}
}

// inventario: RIP-H-50
// D-109 aggiornata (Stefano, 22/11/2026): nella raccolta più buste che corrispondono sono sempre un rifiuto, anche con il
// numero senza anno e una sola gemella dell'anno in corso (niente scelta dell'anno); con l'anno scritto si trova quella
// giusta. La consegna e il lotto (D-89, D-95) continuano a scegliere l'anno in corso.
func TestRaccoltaGemelleDiAnniDiversi(t *testing.T) {
	b, d, _ := prova(t)
	accendi(t, b, "500")
	del25 := bustaD109(t, b, "8/25", "RIP-2025-0045", "0011")
	del26 := bustaD109(t, b, "9/26", "RIP-2026-0045", "0012")
	for _, scritto := range []string{"45", "0045"} {
		atteso := "Più buste corrispondono a «" + scritto + "»: scrivi il numero con l'anno o usa il QR."
		if _, err := b.EscludiBustaDallaRaccolta(ctx, scritto, chi); err == nil || err.Error() != atteso {
			t.Errorf("busta %q: %v", scritto, err)
		}
		if _, _, err := b.EscludiClienteDallaRaccolta(ctx, scritto, chi); err == nil || err.Error() != atteso {
			t.Errorf("cliente %q: %v", scritto, err)
		}
	}
	if righeDi(t, b, del25.ID) != 1 || righeDi(t, b, del26.ID) != 1 || conta(t, d, `SELECT count(*) FROM dettati_esclusioni`) != 0 {
		t.Fatal("cancellato con il numero senza anno")
	}
	if v, err := b.EscludiBustaDallaRaccolta(ctx, "0045/26", chi); err != nil || v != "0045/26" || righeDi(t, b, del26.ID) != 0 || righeDi(t, b, del25.ID) != 1 {
		t.Errorf("0045/26: %q %v", v, err)
	}
	// Consegna (bustaPerNumero) e lotto (bustaPerNumeroCon, la stessa regola con il controllo del lotto) scelgono il 2026.
	for _, scritto := range []string{"45", "0045"} {
		if id, err := b.bustaPerNumero(ctx, d.Lettura, scritto); err != nil || id != del26.ID {
			t.Errorf("consegna %q: %d %v", scritto, id, err)
		}
		if id, err := b.bustaPerNumeroCon(ctx, d.Lettura, scritto, sempreBuona); err != nil || id != del26.ID {
			t.Errorf("lotto %q: %d %v", scritto, id, err)
		}
	}
}

// inventario: RIP-H-50
// Nella raccolta lo stato della busta non conta: gemelle con lo stesso numero, una consegnata (o annullata) e una aperta,
// sono un rifiuto e niente si cancella (la consegna conterebbe solo quella aperta).
func TestRaccoltaGemelleInStatiDiversi(t *testing.T) {
	for _, stato := range []string{"consegnata", "annullata"} {
		t.Run(stato, func(t *testing.T) {
			b, d, _ := prova(t)
			accendi(t, b, "500")
			chiusa := bustaD109(t, b, "6/26", "RIP-2026-0060", "0021")
			aperta := bustaD109(t, b, "7/26", "RIP-2026-0060", "0022")
			esegui(t, d, `UPDATE buste SET stato = ? WHERE id = ?`, stato, chiusa.ID)
			if _, err := b.EscludiBustaDallaRaccolta(ctx, "0060/26", chi); !errors.Is(err, ErrPiuBuste) {
				t.Errorf("busta: %v", err)
			}
			if _, _, err := b.EscludiClienteDallaRaccolta(ctx, "0060/26", chi); !errors.Is(err, ErrPiuBuste) {
				t.Errorf("cliente: %v", err)
			}
			if righeDi(t, b, chiusa.ID) != 1 || righeDi(t, b, aperta.ID) != 1 {
				t.Error("cancellato fra le gemelle")
			}
			if id, err := b.bustaPerNumero(ctx, d.Lettura, "0060/26"); err != nil || id != aperta.ID {
				t.Errorf("la consegna non sceglie l'aperta: %d %v", id, err)
			}
		})
	}
}

// inventario: RIP-H-49
// Il nuovo lotto solo con il valore salvato «0»: «00» e un valore illeggibile (che per l'accensione valgono 0) sono rifiutati,
// come refill/1 dello standalone (Settings.get(@remaining) != "0"), e niente si scrive.
func TestRaccoltaNuovoLottoSoloConZero(t *testing.T) {
	for _, v := range []string{"00", "tanti", " 0", "-0"} {
		b, _, _ := prova(t)
		esegui(t, b.d, `INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES ('assist.training.remaining', ?, ?)`, v, istanteProva)
		if err := b.NuovoLottoRaccolta(ctx, chi); !errors.Is(err, ErrLottoInCorso) {
			t.Errorf("%q: %v", v, err)
		}
		if r, a := impostazione(t, b, ChiaveRaccoltaRimaste), impostazione(t, b, ChiaveRaccoltaAccesa); r != v || a != "(assente)" || len(azioniRaccolta(t, b)) != 0 {
			t.Errorf("%q: scritto rimaste %q accesa %q", v, r, a)
		}
	}
}

// inventario: RIP-H-50
// Il ramo del numero interno (un numero che non ha la forma di un'etichetta, cinque cifre): la raccolta trova anche una
// busta nel cestino, la consegna no.
func TestRaccoltaNumeroInternoNelCestino(t *testing.T) {
	b, d, _ := prova(t)
	accendi(t, b, "500")
	c := bustaD109(t, b, "12345/26", "SINT-D109-CESTINO", "0031")
	esegui(t, d, `UPDATE buste SET cancellata_il = ? WHERE id = ?`, istanteProva, c.ID)
	if id, err := b.bustaPerNumero(ctx, d.Lettura, "12345/26"); err != nil || id != 0 {
		t.Errorf("la consegna trova una busta nel cestino: %d %v", id, err)
	}
	if v, err := b.EscludiBustaDallaRaccolta(ctx, " 012345/26 ", chi); err != nil || v != "N. 12345/26" || righeDi(t, b, c.ID) != 0 {
		t.Errorf("raccolta: %q %v", v, err)
	}
}
