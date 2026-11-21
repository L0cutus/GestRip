package buste

import (
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
)

// La raccolta dei dettati al salvataggio della busta (slice 113; RIP-H-48), come Training.record/3 chiamato da
// Workshop.create_repair/4 dello standalone a go-baseline; i casi da assist/training_test.exs («a row is written»,
// «the budget», «the objection of a customer»). Dettati, nomi e telefoni inventati.

// dettatoProva è una cattura come la tiene la pagina dopo un'estrazione riuscita.
func dettatoProva() *Dettato {
	return &Dettato{
		Testo:    "Cliente Ofelia Inventata 333 000 0113 anello da stringere, preventivo 40",
		Proposta: []byte(`{"data":"31/10/2026","nome":"Ofelia Inventata","telefono":"3330000113","descrizione":"anello da stringere","note":"","preventivo":"40","data_consegna":"","oreficeria":"","compro_oro":0,"oreficeria_id":""}`),
		Modello:  "modello-di-prova-v1",
	}
}

// accendi è la raccolta accesa con rimaste buste nel lotto (set_collect(true) e Settings.put del test standalone).
func accendi(t *testing.T, b *Buste, rimaste string) {
	t.Helper()
	esegui(t, b.d, `INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES ('assist.training.collect', '1', ?), ('assist.training.remaining', ?, ?)`,
		istanteProva, rimaste, istanteProva)
}

func impostazione(t *testing.T, b *Buste, chiave string) string {
	t.Helper()
	var v string
	if err := b.d.Lettura.QueryRow(`SELECT coalesce((SELECT valore FROM impostazioni WHERE chiave = ?), '(assente)')`, chiave).Scan(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

func righeRaccolta(t *testing.T, b *Buste) int {
	t.Helper()
	return conta(t, b.d, `SELECT count(*) FROM dettati_addestramento`)
}

// conDettato è il modulo di base con la cattura di un'estrazione e un QR suo (le buste si sommano).
func conDettato(qr string) Accettazione {
	return con(modulo(), func(a *Accettazione) { a.CodiceQR, a.Dettato = qr, dettatoProva() })
}

// inventario: RIP-H-48
// Con la raccolta accesa la riga nasce nel salvataggio della busta: testo, proposta, modello, istante; il lotto scende di
// uno; nessuna riga d'audit della raccolta finché il lotto non finisce.
func TestRaccoltaRigaScritta(t *testing.T) {
	b, d, _ := prova(t)
	accendi(t, b, "500")
	got := crea(t, b, conDettato("QR-R1"))
	if got.Raccolta != RaccoltaRegistrata {
		t.Errorf("esito %v", got.Raccolta)
	}
	var busta int64
	var testo, proposta, modello, istante string
	if err := d.Lettura.QueryRow(`SELECT busta_id, testo, proposta, modello, creato_il FROM dettati_addestramento`).Scan(&busta, &testo, &proposta, &modello, &istante); err != nil {
		t.Fatal(err)
	}
	p := dettatoProva()
	if busta != got.ID || testo != p.Testo || proposta != string(p.Proposta) || modello != p.Modello || istante != istanteProva {
		t.Errorf("riga %d %q %q %q %q", busta, testo, proposta, modello, istante)
	}
	if r := impostazione(t, b, ChiaveRaccoltaRimaste); r != "499" {
		t.Errorf("rimaste %s", r)
	}
	if a := impostazione(t, b, ChiaveRaccoltaAccesa); a != "1" {
		t.Errorf("accesa %s", a)
	}
	if n := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione LIKE 'assist.%'`); n != 0 {
		t.Errorf("righe d'audit della raccolta: %d", n)
	}
	// Un modello vuoto si scrive NULL (modello è facoltativo nello schema dello standalone).
	crea(t, b, con(conDettato("QR-R2"), func(a *Accettazione) { a.Dettato.Modello = "" }))
	if n := conta(t, d, `SELECT count(*) FROM dettati_addestramento WHERE modello IS NULL`); n != 1 {
		t.Errorf("modello vuoto: %d righe NULL", n)
	}
}

// inventario: RIP-H-48
// Spenta, che è com'è un'installazione nuova: niente riga, niente lotto toccato; anche con la chiave assente o un valore
// diverso da «1».
func TestRaccoltaSpenta(t *testing.T) {
	for _, c := range []struct{ nome, accesa string }{{"mai accesa", ""}, {"spenta", "0"}, {"valore strano", "true"}, {"spazi", " 1"}} {
		t.Run(c.nome, func(t *testing.T) {
			b, d, _ := prova(t)
			if c.accesa != "" {
				esegui(t, d, `INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES ('assist.training.collect', ?, ?), ('assist.training.remaining', '500', ?)`,
					c.accesa, istanteProva, istanteProva)
			}
			got := crea(t, b, conDettato("QR-S"))
			if got.Raccolta != RaccoltaSpenta || righeRaccolta(t, b) != 0 {
				t.Errorf("esito %v, righe %d", got.Raccolta, righeRaccolta(t, b))
			}
			if c.accesa != "" && impostazione(t, b, ChiaveRaccoltaRimaste) != "500" {
				t.Errorf("rimaste %s", impostazione(t, b, ChiaveRaccoltaRimaste))
			}
			if n := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione LIKE 'assist.%'`); n != 0 {
				t.Errorf("audit %d", n)
			}
		})
	}
}

// inventario: RIP-H-48
// Una busta compilata a mano non passa nessuna cattura: niente riga e lotto intero.
func TestRaccoltaBustaAMano(t *testing.T) {
	b, _, _ := prova(t)
	accendi(t, b, "500")
	got := crea(t, b, modulo())
	if got.Raccolta != RaccoltaNessuna || righeRaccolta(t, b) != 0 || impostazione(t, b, ChiaveRaccoltaRimaste) != "500" {
		t.Errorf("esito %v, righe %d, rimaste %s", got.Raccolta, righeRaccolta(t, b), impostazione(t, b, ChiaveRaccoltaRimaste))
	}
}

// inventario: RIP-H-48
// Il lotto: l'ultima unità spegne subito la raccolta, con la riga d'audit dell'attore «system» e soli numeri; dopo, niente
// più righe. Un interruttore rimasto acceso su un lotto finito (o senza lotto) si spegne al salvataggio dopo, sempre con
// l'audit «system», e non scrive.
func TestRaccoltaLotto(t *testing.T) {
	b, d, _ := prova(t)
	accendi(t, b, "2")
	if crea(t, b, conDettato("QR-L1")).Raccolta != RaccoltaRegistrata || impostazione(t, b, ChiaveRaccoltaAccesa) != "1" {
		t.Fatal("prima busta")
	}
	if got := crea(t, b, conDettato("QR-L2")); got.Raccolta != RaccoltaRegistrata {
		t.Errorf("seconda busta %v", got.Raccolta)
	}
	if a, r := impostazione(t, b, ChiaveRaccoltaAccesa), impostazione(t, b, ChiaveRaccoltaRimaste); a != "0" || r != "0" {
		t.Errorf("dopo l'ultima: accesa %s rimaste %s", a, r)
	}
	if s := ultimoAudit(t, d, "assist.training_collect"); s != `assist.training_collect|assist|training|ok|{"on":false,"remaining":0}` {
		t.Errorf("audit %q", s)
	}
	if n := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'assist.training_collect' AND attore = 'system' AND ip IS NULL AND user_agent IS NULL`); n != 1 {
		t.Errorf("righe system: %d", n)
	}
	if got := crea(t, b, conDettato("QR-L3")); got.Raccolta != RaccoltaSpenta {
		t.Errorf("terza busta %v", got.Raccolta)
	}
	if n := righeRaccolta(t, b); n != 2 {
		t.Errorf("righe %d", n)
	}
	if n := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'assist.training_collect'`); n != 1 {
		t.Errorf("audit dopo lo spegnimento: %d", n)
	}
	if v := valore(t, d, `SELECT aggiornata_il FROM impostazioni WHERE chiave = 'assist.training.remaining'`); v != istanteProva {
		t.Errorf("aggiornata_il %v", v)
	}

	for _, c := range []struct{ nome, rimaste string }{{"lotto finito", "0"}, {"senza lotto", ""}, {"lotto illeggibile", "tanti"}} {
		t.Run(c.nome, func(t *testing.T) {
			b, d, _ := prova(t)
			esegui(t, d, `INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES ('assist.training.collect', '1', ?)`, istanteProva)
			if c.rimaste != "" {
				esegui(t, d, `INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES ('assist.training.remaining', ?, ?)`, c.rimaste, istanteProva)
			}
			got := crea(t, b, conDettato("QR-F"))
			if got.Raccolta != RaccoltaSpenta || righeRaccolta(t, b) != 0 || impostazione(t, b, ChiaveRaccoltaAccesa) != "0" {
				t.Errorf("esito %v, righe %d, accesa %s", got.Raccolta, righeRaccolta(t, b), impostazione(t, b, ChiaveRaccoltaAccesa))
			}
			if s := ultimoAudit(t, d, "assist.training_collect"); s != `assist.training_collect|assist|training|ok|{"on":false,"remaining":0}` {
				t.Errorf("audit %q", s)
			}
			want := c.rimaste
			if want == "" {
				want = "(assente)"
			}
			if r := impostazione(t, b, ChiaveRaccoltaRimaste); r != want {
				t.Errorf("rimaste %s", r)
			}
		})
	}
}

// inventario: RIP-H-48
// Due salvataggi insieme sull'ultima unità: una riga sola, una sola riga d'audit «system», tutte e due le buste salvate.
func TestRaccoltaConcorrenzaUltimaUnita(t *testing.T) {
	for giro := 0; giro < 5; giro++ {
		b, d, _ := prova(t)
		accendi(t, b, "1")
		var wg sync.WaitGroup
		esiti := make([]EsitoRaccolta, 2)
		errori := make([]error, 2)
		for i := range 2 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				a := conDettato(fmt.Sprintf("QR-C%d", i))
				a.GemelleConfermate = a.CodiceQR
				c, err := b.Crea(ctx, a, chi)
				esiti[i], errori[i] = c.Raccolta, err
			}()
		}
		wg.Wait()
		for _, err := range errori {
			if err != nil {
				t.Fatal(err)
			}
		}
		if n := conta(t, d, `SELECT count(*) FROM buste`); n != 2 {
			t.Errorf("buste %d", n)
		}
		if n := righeRaccolta(t, b); n != 1 {
			t.Errorf("giro %d: righe %d", giro, n)
		}
		registrate := 0
		for _, e := range esiti {
			if e == RaccoltaRegistrata {
				registrate++
			}
		}
		if registrate != 1 {
			t.Errorf("giro %d: esiti %v", giro, esiti)
		}
		if n := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'assist.training_collect'`); n != 1 {
			t.Errorf("giro %d: audit %d", giro, n)
		}
		if r := impostazione(t, b, ChiaveRaccoltaRimaste); r != "0" {
			t.Errorf("rimaste %s", r)
		}
	}
}

// inventario: RIP-H-48
// Il cliente che si è opposto (dettati_esclusioni): nessuna riga e lotto non consumato; gli altri clienti sì.
func TestRaccoltaClienteEscluso(t *testing.T) {
	b, d, _ := prova(t)
	accendi(t, b, "500")
	primo := crea(t, b, con(modulo(), func(a *Accettazione) { a.CodiceQR = "QR-E0" }))
	esegui(t, d, `INSERT INTO dettati_esclusioni (cliente_id, creato_il) VALUES (?, ?)`, primo.ClienteID, istanteProva)
	got := crea(t, b, conDettato("QR-E1")) // stesso nome e telefono: lo stesso cliente
	if got.ClienteID != primo.ClienteID {
		t.Fatalf("cliente %d invece di %d", got.ClienteID, primo.ClienteID)
	}
	if got.Raccolta != RaccoltaEsclusa || righeRaccolta(t, b) != 0 || impostazione(t, b, ChiaveRaccoltaRimaste) != "500" {
		t.Errorf("esito %v, righe %d, rimaste %s", got.Raccolta, righeRaccolta(t, b), impostazione(t, b, ChiaveRaccoltaRimaste))
	}
	altro := crea(t, b, con(conDettato("QR-E2"), func(a *Accettazione) { a.NomeCliente, a.TelefonoCliente = "Altro Inventato", "333 000 0999" }))
	if altro.Raccolta != RaccoltaRegistrata || righeRaccolta(t, b) != 1 || impostazione(t, b, ChiaveRaccoltaRimaste) != "499" {
		t.Errorf("altro cliente: esito %v, righe %d", altro.Raccolta, righeRaccolta(t, b))
	}
	// Escluso anche con il lotto finito: l'esclusione viene prima, la raccolta resta accesa.
	esegui(t, d, `UPDATE impostazioni SET valore = '0' WHERE chiave = 'assist.training.remaining'`)
	if got := crea(t, b, conDettato("QR-E3")); got.Raccolta != RaccoltaEsclusa || impostazione(t, b, ChiaveRaccoltaAccesa) != "1" {
		t.Errorf("escluso a lotto finito: %v, accesa %s", got.Raccolta, impostazione(t, b, ChiaveRaccoltaAccesa))
	}
}

// inventario: RIP-H-48
// Una busta rifiutata non lascia la riga e non consuma il lotto: stessa transazione, stessa sorte.
func TestRaccoltaBustaRifiutata(t *testing.T) {
	b, _, _ := prova(t)
	accendi(t, b, "500")
	_, err := b.Crea(ctx, con(conDettato("QR-X"), func(a *Accettazione) { a.Descrizione = " " }), chi)
	if err == nil {
		t.Fatal("busta senza lavorazione salvata")
	}
	if righeRaccolta(t, b) != 0 || impostazione(t, b, ChiaveRaccoltaRimaste) != "500" {
		t.Errorf("righe %d, rimaste %s", righeRaccolta(t, b), impostazione(t, b, ChiaveRaccoltaRimaste))
	}
	// Anche l'avviso delle gemelle (la busta non si salva ancora) non scrive niente.
	crea(t, b, con(modulo(), func(a *Accettazione) { a.CodiceQR = "QR-GEM" }))
	if _, err := b.Crea(ctx, con(conDettato("QR-GEM"), func(a *Accettazione) { a.GemelleConfermate = "" }), chi); err == nil {
		t.Fatal("nessun avviso delle gemelle")
	}
	if righeRaccolta(t, b) != 0 || impostazione(t, b, ChiaveRaccoltaRimaste) != "500" {
		t.Errorf("dopo l'avviso: righe %d, rimaste %s", righeRaccolta(t, b), impostazione(t, b, ChiaveRaccoltaRimaste))
	}
}

// inventario: RIP-H-48
// D-108: un guasto provocato in ogni punto della raccolta (lettura delle impostazioni e delle esclusioni, consumo del lotto,
// INSERT della riga, spegnimento all'ultima unità). Un errore nel salvare la riga non blocca mai la busta: la busta e le sue righe d'audit si salvano, la raccolta resta com'era
// (lotto non consumato, niente spegnimento), e l'errore non porta mai il dettato.
func TestRaccoltaErroreNonBloccaLaBusta(t *testing.T) {
	casi := []struct {
		nome    string
		prepara func(t *testing.T, b *Buste)
		a       func(a *Accettazione)
		dopo    func(t *testing.T, b *Buste) // rimette a posto il guasto prima dei controlli
	}{
		{"proposta non JSON", nil, func(a *Accettazione) { a.Dettato.Proposta = []byte("Ofelia Inventata non è JSON") }, nil},
		{"inserimento rifiutato", func(t *testing.T, b *Buste) {
			esegui(t, b.d, `CREATE TRIGGER rifiuta_dettato BEFORE INSERT ON dettati_addestramento BEGIN SELECT RAISE(ABORT, 'no'); END`)
		}, nil, nil},
		{"riga già presente per la busta", func(t *testing.T, b *Buste) {
			// Una riga orfana con il prossimo id di busta: l'UNIQUE di busta_id fallisce.
			esegui(t, b.d, `PRAGMA foreign_keys = OFF`)
			esegui(t, b.d, `INSERT INTO dettati_addestramento (busta_id, testo, proposta, creato_il) VALUES (1, 'altro', '{}', ?)`, istanteProva)
			esegui(t, b.d, `PRAGMA foreign_keys = ON`)
		}, nil, nil},
		{"lotto non aggiornabile", func(t *testing.T, b *Buste) {
			esegui(t, b.d, `CREATE TRIGGER rifiuta_lotto BEFORE UPDATE ON impostazioni BEGIN SELECT RAISE(ABORT, 'no'); END`)
		}, nil, nil},
		{"esclusioni illeggibili", func(t *testing.T, b *Buste) {
			esegui(t, b.d, `DROP TABLE dettati_esclusioni`)
		}, nil, nil},
		// D-108: la lettura delle impostazioni della raccolta che fallisce (Crea non legge impostazioni per conto suo).
		{"impostazioni illeggibili", func(t *testing.T, b *Buste) {
			esegui(t, b.d, `ALTER TABLE impostazioni RENAME TO impostazioni_nascoste`)
		}, nil, func(t *testing.T, b *Buste) {
			esegui(t, b.d, `ALTER TABLE impostazioni_nascoste RENAME TO impostazioni`)
		}},
		// D-108: lo spegnimento all'ultima unità che non si scrive (dopo il consumo e la riga): si annulla tutto.
		{"spegnimento non scritto", func(t *testing.T, b *Buste) {
			esegui(t, b.d, `CREATE TRIGGER rifiuta_spegnimento BEFORE INSERT ON impostazioni BEGIN SELECT RAISE(ABORT, 'no'); END`)
		}, nil, nil},
	}
	for _, c := range casi {
		t.Run(c.nome, func(t *testing.T) {
			b, d, _ := prova(t)
			accendi(t, b, "1")
			if c.prepara != nil {
				c.prepara(t, b)
			}
			a := conDettato("QR-ERR")
			if c.a != nil {
				c.a(&a)
			}
			a.GemelleConfermate = a.CodiceQR
			got, err := b.Crea(ctx, a, chi)
			if c.dopo != nil {
				c.dopo(t, b)
			}
			if err != nil {
				t.Fatalf("la busta non si è salvata: %v", err)
			}
			if got.Raccolta != RaccoltaNonScritta {
				t.Errorf("esito %v", got.Raccolta)
			}
			if n := conta(t, d, `SELECT count(*) FROM buste WHERE id = ?`, got.ID); n != 1 {
				t.Error("busta assente")
			}
			if ultimoAudit(t, d, "repair.created") == "" {
				t.Error("audit della busta assente")
			}
			if n := conta(t, d, `SELECT count(*) FROM dettati_addestramento WHERE busta_id = ? AND testo <> 'altro'`, got.ID); n != 0 {
				t.Errorf("righe %d", n)
			}
			if r, acc := impostazione(t, b, ChiaveRaccoltaRimaste), impostazione(t, b, ChiaveRaccoltaAccesa); r != "1" || acc != "1" {
				t.Errorf("rimaste %s accesa %s", r, acc)
			}
			if n := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'assist.training_collect'`); n != 0 {
				t.Errorf("audit di spegnimento %d", n)
			}
			// Dopo l'errore la transazione è sana: la busta dopo scrive (se la causa non c'è più).
		})
	}
}

// inventario: RIP-H-48
// La cattura non va mai nel registro né negli errori: %v, %+v, %#v e slog la nascondono.
func TestRaccoltaDettatoNascosto(t *testing.T) {
	p := dettatoProva()
	a := conDettato("QR-P")
	var buf strings.Builder
	slog.New(slog.NewTextHandler(&buf, nil)).Info("prova", "dettato", p, "accettazione", a)
	for _, s := range []string{fmt.Sprintf("%v", p), fmt.Sprintf("%+v", p), fmt.Sprintf("%#v", p), fmt.Sprintf("%v", *p), fmt.Sprintf("%+v", a), fmt.Sprintf("%#v", a), buf.String()} {
		for _, segreto := range []string{"Ofelia", "0113", "modello-di-prova-v1", "preventivo 40"} {
			if strings.Contains(s, segreto) {
				t.Errorf("%q in %q", segreto, s)
			}
		}
	}
}
