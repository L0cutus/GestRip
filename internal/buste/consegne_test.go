package buste

import (
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/db"
)

// Le consegne all'artigiano (slice 077, ART-05…08, 10, 11), come Workshop.purge_stale_empty_handovers/1,
// open_handover/2, list_artisans_with_open_handovers/0, list_handovers_in_collection/1,
// add_bag_to_handover/2, remove_bag_from_handover/2 e confirm_handover/2 dello standalone: i casi di
// handovers_test.exs, con la D-64 per il numero scritto. L'orologio dei test è fermo al 31/10/2026, le
// 10:00 in negozio: «oggi» comincia il 30/10/2026 alle 23:00 UTC.

const mezzanotteProva = "2026-10-30T23:00:00.000000Z"

// consegnaIl scrive una consegna dell'artigiano con creato_il dato e chiusa_il (vuoto: aperta).
func consegnaIl(t *testing.T, d *db.DB, artigiano int64, creata, chiusa string) int64 {
	t.Helper()
	var c any
	if chiusa != "" {
		c = chiusa
	}
	res, err := d.Scrittura.Exec(`INSERT INTO consegne_artigiani (artigiano_id, chiusa_il, creato_il, aggiornato_il) VALUES (?, ?, ?, ?)`,
		artigiano, c, creata, creata)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	return id
}

// bustaCon crea una busta con quel codice (vuoto: nessuno) e la descrizione; ritorna id e numero.
func bustaCon(t *testing.T, b *Buste, d *db.DB, codice string) (int64, string) {
	t.Helper()
	c := crea(t, b, con(modulo(), func(a *Accettazione) { a.CodiceQR = "" }))
	var qr any
	if codice != "" {
		qr = codice
	}
	esegui(t, d, `UPDATE buste SET codice_qr = ? WHERE id = ?`, qr, c.ID)
	return c.ID, c.Numero
}

// giroDi è «consegna|uscita|rientro|note|artigiano» della busta (NULL come «-», istanti come «si»).
func giroDi(t *testing.T, d *db.DB, id int64) string {
	t.Helper()
	var s string
	if err := d.Lettura.QueryRow(`SELECT coalesce(consegna_artigiano_id, '-') || '|' ||
		CASE WHEN consegnata_artigiano_il IS NULL THEN '-' ELSE 'si' END || '|' ||
		CASE WHEN rientrata_artigiano_il IS NULL THEN '-' ELSE 'si' END || '|' ||
		coalesce(note_artigiano, '-') || '|' || coalesce(artigiano_id, '-') FROM buste WHERE id = ?`, id).Scan(&s); err != nil {
		t.Fatal(err)
	}
	return s
}

func consegneScritte(t *testing.T, d *db.DB) string {
	t.Helper()
	var s sql.NullString
	_ = d.Lettura.QueryRow(`SELECT group_concat(id || ':' || artigiano_id || ':' || coalesce(chiusa_il, 'aperta'), ',') FROM (SELECT * FROM consegne_artigiani ORDER BY id)`).Scan(&s)
	return s.String
}

// inventario: ART-05
// La pulizia all'apertura della lista: via le consegne ancora aperte dei giorni prima senza nessuna busta;
// restano quelle di oggi (dalla mezzanotte del negozio esatta), quelle chiuse e quelle con una busta, anche
// se la busta è nel cestino. Nessuna riga d'audit.
func TestPuliziaConsegneVuote(t *testing.T) {
	b, d, _ := prova(t)
	ieriVuota := consegnaIl(t, d, 1, "2026-10-29T08:00:00.000000Z", "")
	appenaPrima := consegnaIl(t, d, 1, "2026-10-30T22:59:59.999999Z", "")
	mezzanotte := consegnaIl(t, d, 1, mezzanotteProva, "")
	oggi := consegnaIl(t, d, 1, "2026-10-31T08:00:00.000000Z", "")
	chiusa := consegnaIl(t, d, 1, "2026-10-29T08:00:00.000000Z", "2026-10-29T18:00:00.000000Z")
	conBusta := consegnaIl(t, d, 1, "2026-10-29T08:00:00.000000Z", "")
	nelCestino := consegnaIl(t, d, 1, "2026-10-29T08:00:00.000000Z", "")
	b1, _ := bustaCon(t, b, d, "")
	b2, _ := bustaCon(t, b, d, "")
	esegui(t, d, `UPDATE buste SET consegna_artigiano_id = ? WHERE id = ?`, conBusta, b1)
	esegui(t, d, `UPDATE buste SET consegna_artigiano_id = ?, cancellata_il = ? WHERE id = ?`, nelCestino, istanteProva, b2)
	audit := conta(t, d, `SELECT count(*) FROM eventi_audit`)
	n, err := b.PulisciConsegneVuote(ctx)
	if err != nil || n != 2 {
		t.Fatalf("pulite %d, %v", n, err)
	}
	for _, id := range []int64{ieriVuota, appenaPrima} {
		if conta(t, d, `SELECT count(*) FROM consegne_artigiani WHERE id = ?`, id) != 0 {
			t.Errorf("consegna %d rimasta", id)
		}
	}
	for _, id := range []int64{mezzanotte, oggi, chiusa, conBusta, nelCestino} {
		if conta(t, d, `SELECT count(*) FROM consegne_artigiani WHERE id = ?`, id) != 1 {
			t.Errorf("consegna %d cancellata", id)
		}
	}
	if conta(t, d, `SELECT count(*) FROM eventi_audit`) != audit {
		t.Error("la pulizia ha scritto nel registro")
	}
	if n, err := b.PulisciConsegneVuote(ctx); err != nil || n != 0 {
		t.Fatalf("seconda pulizia %d, %v", n, err)
	}
}

// inventario: ART-06
// Aprire una consegna riprende quella di oggi dell'artigiano ancora in raccolta (aperta, senza buste uscite,
// la più recente), altrimenti ne crea una nuova; una di ieri, una chiusa o una già confermata non si
// riprende. L'artigiano che non c'è è «Artigiano non trovato.». Nessuna riga d'audit.
func TestApriConsegna(t *testing.T) {
	b, d, o := prova(t)
	if _, err := b.ApriConsegna(ctx, 999); !errors.Is(err, ErrArtigianoNonTrovato) {
		t.Fatalf("artigiano che non c'è: %v", err)
	}
	if consegneScritte(t, d) != "" {
		t.Fatal("un rifiuto ha scritto")
	}
	consegnaIl(t, d, 1, "2026-10-30T22:59:59.999999Z", "")                            // ieri
	consegnaIl(t, d, 1, "2026-10-31T07:00:00.000000Z", "2026-10-31T08:00:00.000000Z") // chiusa
	prima, err := b.ApriConsegna(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if got := valore(t, d, `SELECT creato_il || '|' || aggiornato_il || '|' || artigiano_id || '|' || coalesce(chiusa_il, '-') || '|' || coalesce(note, '-') FROM consegne_artigiani WHERE id = ?`, prima); got != istanteProva+"|"+istanteProva+"|1|-|-" {
		t.Fatalf("consegna nuova %v", got)
	}
	o.Sposta(time.Hour)
	if di, err := b.ApriConsegna(ctx, 1); err != nil || di != prima {
		t.Fatalf("ripresa: %d (attesa %d), %v", di, prima, err)
	}
	// Due in raccolta oggi: la più recente.
	recente := consegnaIl(t, d, 1, "2026-10-30T23:00:00.000000Z", "")
	if di, _ := b.ApriConsegna(ctx, 1); di != recente {
		t.Fatalf("la più recente è %d, avuta %d", recente, di)
	}
	// Con una busta uscita (confermata) non è più in raccolta, anche se la busta poi va nel cestino.
	busta, _ := bustaCon(t, b, d, "")
	esegui(t, d, `UPDATE buste SET consegna_artigiano_id = ?, consegnata_artigiano_il = ?, cancellata_il = ? WHERE id = ?`, recente, istanteProva, istanteProva, busta)
	if di, _ := b.ApriConsegna(ctx, 1); di != prima {
		t.Fatalf("dopo la conferma della più recente: %d, attesa %d", di, prima)
	}
	altra, _ := bustaCon(t, b, d, "")
	esegui(t, d, `UPDATE buste SET consegna_artigiano_id = ?, consegnata_artigiano_il = ? WHERE id = ?`, prima, istanteProva, altra)
	nuova, err := b.ApriConsegna(ctx, 1)
	if err != nil || nuova == prima || nuova == recente {
		t.Fatalf("attesa una consegna nuova, avuta %d, %v", nuova, err)
	}
	// Di un altro giorno: ieri non conta.
	o.Imposta(time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)) // 01/11 all'una in negozio
	domani, _ := b.ApriConsegna(ctx, 1)
	if domani == nuova {
		t.Fatal("ripresa una consegna di ieri")
	}
	if conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione NOT IN ('repair.created', 'customer.created')`) != 0 {
		t.Error("l'apertura ha scritto nel registro")
	}
}

// inventario: ART-07
// Il menu degli artigiani conta le consegne aperte di qualunque giorno (anche confermate), per nome e id;
// le consegne in raccolta sono quelle di oggi aperte e senza buste uscite, dalla più recente, con il nome
// dell'artigiano e le buste fuori dal cestino.
func TestElenchiDelleConsegne(t *testing.T) {
	b, d, _ := prova(t)
	esegui(t, d, `INSERT INTO artigiani (id, nome) VALUES (2, 'Bottega Zeta'), (3, 'Atelier Alfa')`)
	ieri := consegnaIl(t, d, 1, "2026-10-29T08:00:00.000000Z", "")
	consegnaIl(t, d, 2, "2026-10-29T08:00:00.000000Z", "2026-10-30T08:00:00.000000Z") // chiusa
	oggi1 := consegnaIl(t, d, 2, "2026-10-31T07:05:00.000000Z", "")
	oggi2 := consegnaIl(t, d, 1, "2026-10-31T08:30:00.000000Z", "")
	confermata := consegnaIl(t, d, 3, "2026-10-31T08:40:00.000000Z", "")
	for i, c := range []int64{oggi1, oggi1, oggi1, confermata, ieri} {
		id, _ := bustaCon(t, b, d, "")
		esegui(t, d, `UPDATE buste SET consegna_artigiano_id = ? WHERE id = ?`, c, id)
		switch i {
		case 2:
			esegui(t, d, `UPDATE buste SET cancellata_il = ? WHERE id = ?`, istanteProva, id)
		case 3:
			esegui(t, d, `UPDATE buste SET consegnata_artigiano_il = ? WHERE id = ?`, istanteProva, id)
		}
	}
	menu, err := b.ArtigianiDelMenu(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var righe []string
	for _, a := range menu {
		righe = append(righe, a.Nome+":"+itoa(int64(a.Aperte)))
	}
	if got := strings.Join(righe, ","); got != "Artigiano Esempio:2,Atelier Alfa:1,Bottega Zeta:1" {
		t.Fatalf("menu %s", got)
	}
	if menu[0].ID != 1 || menu[1].ID != 3 {
		t.Fatalf("id %+v", menu)
	}
	cc, err := b.ConsegneInRaccolta(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(cc) != 2 || cc[0].ID != oggi2 || cc[1].ID != oggi1 || cc[0].Artigiano != "Artigiano Esempio" || cc[1].Artigiano != "Bottega Zeta" ||
		cc[0].Buste != 0 || cc[1].Buste != 2 || !cc[1].Aperta.Equal(time.Date(2026, 10, 31, 7, 5, 0, 0, time.UTC)) {
		t.Fatalf("in raccolta %+v", cc)
	}
}

// aggiungi aggiunge con il campo scritto (prima il numero, poi il QR).
func aggiungi(b *Buste, consegna int64, codice string) (Aggiunta, error) {
	return b.AggiungiAllaConsegna(ctx, consegna, codice, LetturaScritta)
}

// inventario: ART-08
// Il campo scritto si prova prima come numero dell'etichetta (D-64: «0045/26», «045/26», «45»; per una busta
// senza etichetta il suo numero interno, anche con gli zeri davanti) e poi come QR (lo stesso codice,
// normalizzato, D-64), e passa al secondo solo con «non trovata»; fra più buste vince la più recente. La
// busta prende consegna e artigiano della sessione; la stessa busta di nuovo è «già in lista» e non cambia
// niente. La scansione (079) prova prima il QR. Nessuna riga d'audit.
func TestAggiungiAllaConsegna(t *testing.T) {
	b, d, _ := prova(t)
	esegui(t, d, `INSERT INTO artigiani (id, nome) VALUES (2, 'Bottega Zeta')`)
	consegna := consegnaIl(t, d, 2, istanteProva, "")
	vecchia, _ := bustaCon(t, b, d, "RIP-2026-0045") // gemella più vecchia, chiusa (la busta di carta riusata)
	esegui(t, d, `UPDATE buste SET stato = 'consegnata' WHERE id = ?`, vecchia)
	etichetta, _ := bustaCon(t, b, d, "RIP-2026-0045")
	senza, numero := bustaCon(t, b, d, "")
	altro, _ := bustaCon(t, b, d, "0045/26") // un codice di altro formato uguale al numero scritto
	codice, _ := bustaCon(t, b, d, " SINT-QR-1 ")
	audit := conta(t, d, `SELECT count(*) FROM eventi_audit`)
	a, err := aggiungi(b, consegna, "  0045/26 ")
	if err != nil || a.Busta.ID != etichetta || a.GiaInLista {
		t.Fatalf("0045/26: %+v, %v", a, err)
	}
	if got := a.Busta.Visibile().Titolo(); got != "#0045/26" {
		t.Fatalf("numero visibile %q", got)
	}
	if giroDi(t, d, etichetta) != itoa(consegna)+"|-|-|-|2" || giroDi(t, d, vecchia) != "-|-|-|-|-" || giroDi(t, d, altro) != "-|-|-|-|-" {
		t.Fatalf("giri %s %s %s", giroDi(t, d, etichetta), giroDi(t, d, vecchia), giroDi(t, d, altro))
	}
	for _, scritto := range []string{"45", "045/26", "0045"} {
		if a, err := aggiungi(b, consegna, scritto); err != nil || a.Busta.ID != etichetta || !a.GiaInLista {
			t.Fatalf("%s: %+v, %v", scritto, a, err)
		}
	}
	n := strings.TrimSuffix(numero, "/26")
	if a, err := aggiungi(b, consegna, "00"+n+"/26"); err != nil || a.Busta.ID != senza || a.GiaInLista || a.Busta.Visibile().Titolo() != "N. "+numero {
		t.Fatalf("numero interno di una busta senza etichetta: %+v, %v", a, err)
	}
	if a, err := aggiungi(b, consegna, "sint-qr-1"); !errors.Is(err, ErrRiparazioneNonTrovata) {
		t.Fatalf("il QR è esatto: %+v, %v", a, err)
	}
	if a, err := aggiungi(b, consegna, "SINT-QR-1"); err != nil || a.Busta.ID != codice {
		t.Fatalf("QR: %+v, %v", a, err)
	}
	if a, err := aggiungi(b, consegna, "rip-2026-0045"); err != nil || a.Busta.ID != etichetta || !a.GiaInLista {
		t.Fatalf("QR normalizzato: %+v, %v", a, err)
	}
	// La scansione prova prima il QR: «0045/26» è il codice dell'altra busta.
	if a, err := b.AggiungiAllaConsegna(ctx, consegna, "0045/26", LetturaScansione); err != nil || a.Busta.ID != altro {
		t.Fatalf("scansione: %+v, %v", a, err)
	}
	// Il numero interno di una busta con l'etichetta non si cerca più (D-64).
	esegui(t, d, `UPDATE buste SET consegna_artigiano_id = NULL WHERE id = ?`, etichetta)
	nEt := valore(t, d, `SELECT numero FROM buste WHERE id = ?`, etichetta).(string)
	for _, scritto := range []string{nEt, "0000" + nEt} {
		if _, err := aggiungi(b, consegna, scritto); !errors.Is(err, ErrRiparazioneNonTrovata) {
			t.Fatalf("numero interno %s di una busta con l'etichetta: %v", scritto, err)
		}
	}
	// Un numero interno che non ha la forma di un'etichetta (più di quattro cifre) vale per le buste senza.
	esegui(t, d, `UPDATE buste SET consegna_artigiano_id = NULL WHERE id = ?`, senza)
	if a, err := aggiungi(b, consegna, "0000"+numero); err != nil || a.Busta.ID != senza || a.GiaInLista {
		t.Fatalf("numero interno lungo: %+v, %v", a, err)
	}
	if conta(t, d, `SELECT count(*) FROM eventi_audit`) != audit {
		t.Error("l'aggiunta ha scritto nel registro")
	}
}

// inventario: ART-08
// I rifiuti nell'ordine dello standalone, con i testi della pagina: consegna chiusa o che non c'è, campo
// vuoto, non trovata (anche una busta nel cestino), compro oro, consegnata o annullata, già fuori in
// un'altra consegna (anche senza consegna), in raccolta in un'altra consegna, corsa persa. Nessun rifiuto
// scrive.
func TestRifiutiDellAggiunta(t *testing.T) {
	b, d, _ := prova(t)
	consegna := consegnaIl(t, d, 1, istanteProva, "")
	altra := consegnaIl(t, d, 1, istanteProva, "")
	chiusa := consegnaIl(t, d, 1, istanteProva, istanteProva)
	busta, _ := bustaCon(t, b, d, "RIP-2026-0001")
	prima := giroDi(t, d, busta)
	casi := []struct {
		nome, codice string
		consegna     int64
		prepara      string
		motivo       error
		messaggio    string
	}{
		{"chiusa", "RIP-2026-0001", chiusa, "", ErrConsegnaNonDisponibile, "Consegna non disponibile («RIP-2026-0001»)"},
		{"chiusa e vuoto", "  ", chiusa, "", ErrConsegnaNonDisponibile, "Consegna non disponibile («»)"},
		{"non c'è", "RIP-2026-0001", 999, "", ErrConsegnaNonDisponibile, "Consegna non disponibile («RIP-2026-0001»)"},
		{"vuoto", " \t", consegna, "", ErrCodiceVuoto, "Numero busta o QR richiesto"},
		{"non trovata", "RIP-2026-0099", consegna, "", ErrRiparazioneNonTrovata, "Riparazione non trovata: nessuna busta con numero o QR «RIP-2026-0099»"},
		{"nel cestino", "0001/26", consegna, `UPDATE buste SET cancellata_il = '` + istanteProva + `' WHERE id = ?`, ErrRiparazioneNonTrovata, "Riparazione non trovata: nessuna busta con numero o QR «0001/26»"},
		{"compro oro", "0001/26", consegna, `UPDATE buste SET compro_oro = 1, stato = 'annullata' WHERE id = ?`, ErrCompraOroAllArtigiano, "Buste compro oro non possono essere date ad un artigiano («0001/26»)"},
		{"consegnata", "0001/26", consegna, `UPDATE buste SET stato = 'consegnata' WHERE id = ?`, nil, "Stato 'consegnata' non adatto («0001/26»)"},
		{"annullata", "0001/26", consegna, `UPDATE buste SET stato = 'annullata', consegna_artigiano_id = ` + itoa(altra) + ` WHERE id = ?`, nil, "Stato 'annullata' non adatto («0001/26»)"},
		{"fuori altrove", "0001/26", consegna, `UPDATE buste SET consegna_artigiano_id = ` + itoa(altra) + `, consegnata_artigiano_il = '` + istanteProva + `' WHERE id = ?`, ErrGiaFuoriBottega, "Busta già fuori bottega in altra consegna («0001/26»)"},
		{"fuori senza consegna", "0001/26", consegna, `UPDATE buste SET consegnata_artigiano_il = '` + istanteProva + `' WHERE id = ?`, ErrGiaFuoriBottega, "Busta già fuori bottega in altra consegna («0001/26»)"},
		{"in raccolta altrove", "0001/26", consegna, `UPDATE buste SET consegna_artigiano_id = ` + itoa(altra) + ` WHERE id = ?`, ErrGiaInRaccolta, "Busta già in raccolta in altra consegna («0001/26»)"},
	}
	for _, c := range casi {
		esegui(t, d, `UPDATE buste SET compro_oro = 0, stato = 'in_lavorazione', cancellata_il = NULL, consegna_artigiano_id = NULL, consegnata_artigiano_il = NULL WHERE id = ?`, busta)
		if c.prepara != "" {
			esegui(t, d, c.prepara, busta)
		}
		scritto := giroDi(t, d, busta)
		_, err := aggiungi(b, c.consegna, c.codice)
		var stato *StatoNonAdatto
		if err == nil || err.Error() != c.messaggio || (c.motivo != nil && !errors.Is(err, c.motivo)) || (c.motivo == nil && !errors.As(err, &stato)) {
			t.Errorf("%s: %v", c.nome, err)
		}
		if giroDi(t, d, busta) != scritto {
			t.Errorf("%s: il rifiuto ha scritto", c.nome)
		}
	}
	esegui(t, d, `UPDATE buste SET stato = 'in_lavorazione', compro_oro = 0, cancellata_il = NULL, consegna_artigiano_id = NULL, consegnata_artigiano_il = NULL WHERE id = ?`, busta)
	if giroDi(t, d, busta) != prima {
		t.Fatal("busta non ripristinata")
	}
	// Corsa persa: fra la lettura e la scrittura la busta è passata a un'altra consegna.
	b.primaDiCollegare = func(tx *sql.Tx) error {
		_, err := tx.Exec(`UPDATE buste SET consegna_artigiano_id = ? WHERE id = ?`, altra, busta)
		return err
	}
	if _, err := aggiungi(b, consegna, "0001/26"); !errors.Is(err, ErrNonAggiungibile) || err.Error() != "Impossibile aggiungere la busta a questa consegna («0001/26»)" {
		t.Fatalf("corsa persa: %v", err)
	}
	if giroDi(t, d, busta) != prima {
		t.Fatalf("la corsa persa ha scritto: %s", giroDi(t, d, busta))
	}
	b.primaDiCollegare = nil
	// Una busta già uscita con questa consegna passa i due controlli, come nello standalone.
	esegui(t, d, `UPDATE buste SET consegna_artigiano_id = ?, consegnata_artigiano_il = ? WHERE id = ?`, consegna, istanteProva, busta)
	if a, err := aggiungi(b, consegna, "0001/26"); err != nil || !a.GiaInLista || giroDi(t, d, busta) != itoa(consegna)+"|si|-|-|1" {
		t.Fatalf("busta uscita con questa consegna: %+v, %v, %s", a, err, giroDi(t, d, busta))
	}
}

// inventario: ART-08
// Una busta rientrata da un giro di prima riparte pulita: uscita, rientro, nota dell'artigiano e consegna
// vecchia azzerati, poi la consegna nuova e il suo artigiano; la consegna vecchia resta com'era (chiusa e
// ora vuota). Se la stessa consegna la riprende, è «già in lista».
func TestAggiuntaRiapreUnGiroFinito(t *testing.T) {
	b, d, _ := prova(t)
	esegui(t, d, `INSERT INTO artigiani (id, nome) VALUES (2, 'Bottega Zeta')`)
	vecchia := consegnaIl(t, d, 1, "2026-10-20T08:00:00.000000Z", "2026-10-25T08:00:00.000000Z")
	busta, _ := bustaCon(t, b, d, "RIP-2026-0007")
	esegui(t, d, `UPDATE buste SET consegna_artigiano_id = ?, artigiano_id = 1, consegnata_artigiano_il = ?, rientrata_artigiano_il = ?, note_artigiano = 'Fatto' WHERE id = ?`,
		vecchia, "2026-10-20T09:00:00.000000Z", "2026-10-25T08:00:00.000000Z", busta)
	nuova := consegnaIl(t, d, 2, istanteProva, "")
	a, err := aggiungi(b, nuova, "7")
	if err != nil || a.GiaInLista {
		t.Fatalf("%+v, %v", a, err)
	}
	if got := giroDi(t, d, busta); got != itoa(nuova)+"|-|-|-|2" {
		t.Fatalf("giro %s", got)
	}
	if got := valore(t, d, `SELECT aggiornato_il FROM buste WHERE id = ?`, busta); got != istanteProva {
		t.Fatalf("aggiornato_il %v", got)
	}
	if consegneScritte(t, d) != itoa(vecchia)+":1:2026-10-25T08:00:00.000000Z,"+itoa(nuova)+":2:aperta" {
		t.Fatalf("consegne %s", consegneScritte(t, d))
	}
	// Rientrata senza uscita (un dato importato strano) e legata a un'altra consegna: non è «in raccolta altrove»
	// (lo standalone guarda la coppia), riparte pulita.
	strana, _ := bustaCon(t, b, d, "RIP-2026-0008")
	esegui(t, d, `UPDATE buste SET consegna_artigiano_id = ?, rientrata_artigiano_il = ? WHERE id = ?`, vecchia, istanteProva, strana)
	if a, err := aggiungi(b, nuova, "8"); err != nil || a.Busta.ID != strana || giroDi(t, d, strana) != itoa(nuova)+"|-|-|-|2" {
		t.Fatalf("rientrata senza uscita: %+v, %v, %s", a, err, giroDi(t, d, strana))
	}
	// Rientrata dalla stessa consegna: già in lista, ma riaperta lo stesso.
	esegui(t, d, `UPDATE buste SET consegnata_artigiano_il = ?, rientrata_artigiano_il = ?, note_artigiano = 'Rifare' WHERE id = ?`, istanteProva, istanteProva, busta)
	if a, err := aggiungi(b, nuova, "RIP-2026-0007"); err != nil || !a.GiaInLista || giroDi(t, d, busta) != itoa(nuova)+"|-|-|-|2" {
		t.Fatalf("stessa consegna: %+v, %v, %s", a, err, giroDi(t, d, busta))
	}
}

// inventario: ART-10
// Togliere scollega una busta della consegna non ancora uscita e lascia l'artigiano; una busta uscita, di
// un'altra consegna, nel cestino o che non c'è non cambia, senza errore. Nessuna riga d'audit.
func TestTogliDallaConsegna(t *testing.T) {
	b, d, o := prova(t)
	consegna := consegnaIl(t, d, 1, istanteProva, "")
	altra := consegnaIl(t, d, 1, istanteProva, "")
	busta, _ := bustaCon(t, b, d, "RIP-2026-0001")
	if _, err := aggiungi(b, consegna, "1"); err != nil {
		t.Fatal(err)
	}
	uscita, _ := bustaCon(t, b, d, "")
	altrui, _ := bustaCon(t, b, d, "")
	cestino, _ := bustaCon(t, b, d, "")
	esegui(t, d, `UPDATE buste SET consegna_artigiano_id = ?, consegnata_artigiano_il = ? WHERE id = ?`, consegna, istanteProva, uscita)
	esegui(t, d, `UPDATE buste SET consegna_artigiano_id = ? WHERE id = ?`, altra, altrui)
	esegui(t, d, `UPDATE buste SET consegna_artigiano_id = ?, cancellata_il = ? WHERE id = ?`, consegna, istanteProva, cestino)
	o.Sposta(time.Minute)
	righe := conta(t, d, `SELECT count(*) FROM eventi_audit`)
	for _, id := range []int64{uscita, altrui, cestino, 999} {
		if err := b.TogliDallaConsegna(ctx, consegna, id); err != nil {
			t.Fatal(err)
		}
	}
	if giroDi(t, d, uscita) != itoa(consegna)+"|si|-|-|-" || giroDi(t, d, altrui) != itoa(altra)+"|-|-|-|-" || giroDi(t, d, cestino) != itoa(consegna)+"|-|-|-|-" {
		t.Fatal("tolta una busta che non si toglie")
	}
	if err := b.TogliDallaConsegna(ctx, altra, busta); err != nil || giroDi(t, d, busta) != itoa(consegna)+"|-|-|-|1" {
		t.Fatal("tolta da un'altra consegna")
	}
	if err := b.TogliDallaConsegna(ctx, consegna, busta); err != nil {
		t.Fatal(err)
	}
	if got := giroDi(t, d, busta); got != "-|-|-|-|1" {
		t.Fatalf("giro dopo «togli» %s", got)
	}
	if got := valore(t, d, `SELECT aggiornato_il FROM buste WHERE id = ?`, busta); got != "2026-10-31T09:01:00.000000Z" {
		t.Fatalf("aggiornato_il %v", got)
	}
	if conta(t, d, `SELECT count(*) FROM eventi_audit`) != righe {
		t.Error("«togli» ha scritto nel registro")
	}
}

// inventario: ART-11
// La conferma timbra l'uscita di ogni busta della consegna non ancora uscita (fuori dal cestino), non chiude la
// consegna e scrive handover.confirmed con quante e quali (id in ordine); una seconda conferma timbra solo le
// buste aggiunte dopo. Una consegna chiusa o che non c'è è «Consegna non disponibile»; una vuota conta zero.
func TestConfermaConsegna(t *testing.T) {
	b, d, o := prova(t)
	consegna := consegnaIl(t, d, 1, istanteProva, "")
	var buste []int64
	for range 3 {
		id, _ := bustaCon(t, b, d, "")
		esegui(t, d, `UPDATE buste SET consegna_artigiano_id = ? WHERE id = ?`, consegna, id)
		buste = append(buste, id)
	}
	esegui(t, d, `UPDATE buste SET cancellata_il = ? WHERE id = ?`, istanteProva, buste[1])
	o.Sposta(time.Minute)
	n, err := b.ConfermaConsegna(ctx, consegna, chi)
	if err != nil || n != 2 {
		t.Fatalf("confermate %d, %v", n, err)
	}
	for i, id := range buste {
		got := valore(t, d, `SELECT coalesce(consegnata_artigiano_il, '-') || '|' || aggiornato_il FROM buste WHERE id = ?`, id)
		atteso := "2026-10-31T09:01:00.000000Z|2026-10-31T09:01:00.000000Z"
		if i == 1 {
			atteso = "-|" + istanteProva
		}
		if got != atteso {
			t.Errorf("busta %d: %v", id, got)
		}
	}
	if got := ultimoAudit(t, d, "handover.confirmed"); got != "handover.confirmed|handover|"+itoa(consegna)+`|ok|{"handed_over":2,"repair_ids":[`+itoa(buste[0])+","+itoa(buste[2])+"]}" {
		t.Fatalf("audit %s", got)
	}
	if got := valore(t, d, `SELECT attore FROM eventi_audit WHERE azione = 'handover.confirmed'`); got != chi.Attore {
		t.Fatalf("attore %v", got)
	}
	if consegneScritte(t, d) != itoa(consegna)+":1:aperta" {
		t.Fatal("la conferma ha chiuso la consegna")
	}
	// Una busta aggiunta dopo: la seconda conferma timbra solo lei.
	nuova, _ := bustaCon(t, b, d, "")
	esegui(t, d, `UPDATE buste SET consegna_artigiano_id = ? WHERE id = ?`, consegna, nuova)
	o.Sposta(time.Minute)
	if n, err := b.ConfermaConsegna(ctx, consegna, chi); err != nil || n != 1 {
		t.Fatalf("seconda conferma %d, %v", n, err)
	}
	if got := ultimoAudit(t, d, "handover.confirmed"); got != "handover.confirmed|handover|"+itoa(consegna)+`|ok|{"handed_over":1,"repair_ids":[`+itoa(nuova)+"]}" {
		t.Fatalf("seconda audit %s", got)
	}
	if got := valore(t, d, `SELECT consegnata_artigiano_il FROM buste WHERE id = ?`, buste[0]); got != "2026-10-31T09:01:00.000000Z" {
		t.Fatalf("ritimbrata: %v", got)
	}
	vuota := consegnaIl(t, d, 1, istanteProva, "")
	if n, err := b.ConfermaConsegna(ctx, vuota, chi); err != nil || n != 0 {
		t.Fatalf("vuota %d, %v", n, err)
	}
	if got := ultimoAudit(t, d, "handover.confirmed"); got != "handover.confirmed|handover|"+itoa(vuota)+`|ok|{"handed_over":0,"repair_ids":[]}` {
		t.Fatalf("audit della vuota %s", got)
	}
	chiusa := consegnaIl(t, d, 1, istanteProva, istanteProva)
	righe := conta(t, d, `SELECT count(*) FROM eventi_audit`)
	for _, id := range []int64{chiusa, 999} {
		if _, err := b.ConfermaConsegna(ctx, id, chi); !errors.Is(err, ErrConsegnaNonDisponibile) || err.Error() != "Consegna non disponibile" {
			t.Fatalf("consegna %d: %v", id, err)
		}
	}
	if conta(t, d, `SELECT count(*) FROM eventi_audit`) != righe {
		t.Fatal("un rifiuto ha scritto nel registro")
	}
}

// inventario: ART-11
// Le uscite e la loro riga d'audit stanno in una transazione: se l'audit non si scrive, nessuna busta esce.
func TestConfermaAuditNellaTransazione(t *testing.T) {
	b, d, _ := prova(t)
	consegna := consegnaIl(t, d, 1, istanteProva, "")
	busta, _ := bustaCon(t, b, d, "")
	esegui(t, d, `UPDATE buste SET consegna_artigiano_id = ? WHERE id = ?`, consegna, busta)
	esegui(t, d, `CREATE TRIGGER niente_audit BEFORE INSERT ON eventi_audit BEGIN SELECT RAISE(ABORT, 'audit rotto'); END`)
	if _, err := b.ConfermaConsegna(ctx, consegna, chi); err == nil {
		t.Fatal("conferma senza audit riuscita")
	}
	if giroDi(t, d, busta) != itoa(consegna)+"|-|-|-|-" {
		t.Fatal("busta uscita senza audit")
	}
}

// inventario: ART-08
// La consegna con il suo artigiano (get_handover/1) e le sue buste fuori dal cestino, dalla più recente
// (list_handover_bags/1); una consegna che non c'è è ErrConsegnaNonTrovata.
func TestConsegnaEBuste(t *testing.T) {
	b, d, _ := prova(t)
	consegna := consegnaIl(t, d, 1, "2026-10-31T07:05:00.000000Z", "2026-10-31T08:00:00.000000Z")
	if _, err := b.ConsegnaArtigiano(ctx, 999); !errors.Is(err, ErrConsegnaNonTrovata) {
		t.Fatalf("999: %v", err)
	}
	c, err := b.ConsegnaArtigiano(ctx, consegna)
	if err != nil || c.ID != consegna || c.ArtigianoID != 1 || c.Artigiano != "Artigiano Esempio" || !c.Chiusa ||
		!c.Aperta.Equal(time.Date(2026, 10, 31, 7, 5, 0, 0, time.UTC)) {
		t.Fatalf("%+v, %v", c, err)
	}
	var ids []int64
	for i := range 3 {
		id, _ := bustaCon(t, b, d, []string{"RIP-2026-0003", "", "SINT"}[i])
		esegui(t, d, `UPDATE buste SET consegna_artigiano_id = ?, descrizione = ? WHERE id = ?`, consegna, "Lavoro "+itoa(int64(i)), id)
		ids = append(ids, id)
	}
	esegui(t, d, `UPDATE buste SET consegnata_artigiano_il = ? WHERE id = ?`, istanteProva, ids[0])
	esegui(t, d, `UPDATE buste SET cancellata_il = ? WHERE id = ?`, istanteProva, ids[1])
	bb, err := b.BusteDellaConsegna(ctx, consegna)
	if err != nil || len(bb) != 2 || bb[0].ID != ids[2] || bb[1].ID != ids[0] || !bb[1].Uscita || bb[0].Uscita ||
		bb[1].Descrizione != "Lavoro 0" || bb[1].Visibile().Titolo() != "#0003/26" || bb[0].Visibile().Titolo() != "N. "+bb[0].Numero {
		t.Fatalf("%+v, %v", bb, err)
	}
}

// inventario: ART-08
// Più buste con il numero scritto (D-64, review della 077): contano solo quelle che si possono dare
// all'artigiano (fuori dal cestino, non compro oro, né consegnate né annullate); con una sola, quella; con più
// e l'anno non scritto, quella dell'anno in corso in negozio; se ne restano più d'una, il rifiuto «Più buste
// corrispondono a «X»: …» senza scrivere niente; se nessuna si può dare, il rifiuto della più recente.
func TestNumeroConPiuBuste(t *testing.T) {
	b, d, _ := prova(t)
	consegna := consegnaIl(t, d, 1, istanteProva, "")
	quest, _ := bustaCon(t, b, d, "RIP-2026-0045")  // aperta, di quest'anno
	scorso, _ := bustaCon(t, b, d, "RIP-2025-0045") // aperta, dell'anno prima, più recente
	if a, err := aggiungi(b, consegna, "45"); err != nil || a.Busta.ID != quest {
		t.Fatalf("«45»: %+v, %v (attesa %d)", a, err, quest)
	}
	if a, err := aggiungi(b, consegna, "0045/25"); err != nil || a.Busta.ID != scorso {
		t.Fatalf("«0045/25»: %+v, %v", a, err)
	}
	// Gemella chiusa più recente: vale quella aperta.
	aperta, _ := bustaCon(t, b, d, "RIP-2026-0046")
	chiusa, _ := bustaCon(t, b, d, "RIP-2026-0046")
	esegui(t, d, `UPDATE buste SET stato = 'consegnata' WHERE id = ?`, chiusa)
	if a, err := aggiungi(b, consegna, "0046/26"); err != nil || a.Busta.ID != aperta {
		t.Fatalf("gemella chiusa: %+v, %v (attesa %d)", a, err, aperta)
	}
	// Due aperte dello stesso anno: rifiuto, niente scritto.
	uno, _ := bustaCon(t, b, d, "RIP-2026-0047")
	due, _ := bustaCon(t, b, d, "RIP-2026-0047")
	for _, scritto := range []string{"0047/26", "47"} {
		_, err := aggiungi(b, consegna, scritto)
		if !errors.Is(err, ErrPiuBuste) || err.Error() != "Più buste corrispondono a «"+scritto+"»: scrivi il numero con l'anno o usa il QR." {
			t.Fatalf("%s: %v", scritto, err)
		}
	}
	if giroDi(t, d, uno) != "-|-|-|-|-" || giroDi(t, d, due) != "-|-|-|-|-" {
		t.Fatal("il rifiuto ha scritto")
	}
	// Il QR resta preciso: la più recente con quel codice.
	if a, err := aggiungi(b, consegna, "RIP-2026-0047"); err != nil || a.Busta.ID != due {
		t.Fatalf("QR: %+v, %v", a, err)
	}
	// Nessuna si può dare: il rifiuto della più recente.
	c1, _ := bustaCon(t, b, d, "RIP-2026-0048")
	c2, _ := bustaCon(t, b, d, "RIP-2026-0048")
	esegui(t, d, `UPDATE buste SET stato = 'consegnata' WHERE id = ?`, c1)
	esegui(t, d, `UPDATE buste SET stato = 'annullata' WHERE id = ?`, c2)
	if _, err := aggiungi(b, consegna, "48"); err == nil || err.Error() != "Stato 'annullata' non adatto («48»)" {
		t.Fatalf("nessuna aggiungibile: %v", err)
	}
	// «N/26» del QR dell'etichetta 76×127 di una busta senza RIP contro l'etichetta RIP-2026-000N: due candidate.
	senza, numero := bustaCon(t, b, d, "")
	n := strings.TrimSuffix(numero, "/26")
	conRIP, _ := bustaCon(t, b, d, "RIP-2026-"+strings.Repeat("0", 4-len(n))+n)
	if _, err := aggiungi(b, consegna, numero); !errors.Is(err, ErrPiuBuste) {
		t.Fatalf("%s interno contro etichetta: %v", numero, err)
	}
	if giroDi(t, d, senza) != "-|-|-|-|-" || giroDi(t, d, conRIP) != "-|-|-|-|-" {
		t.Fatal("il rifiuto ha scritto")
	}
}
