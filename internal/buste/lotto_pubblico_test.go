package buste

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
	"github.com/L0cutus/gestrip/internal/media"
)

// La pagina pubblica del lotto e le sue foto (slice 092, ORE-14, ORE-15, ORE-16), come ReadyLots.get_public_lot/1,
// public_members/1, close_if_complete/2, member/2 e RepairPhotos.read_public_media/5 dello standalone: i casi di
// public_lot_controller_test.exs e di ready_lots_test.exs («the public lot», «closing a lot»). Le buste sono
// dell'oreficeria 2 di prova, accettate il 31/10/2026 (1/26, 2/26, … senza etichetta). Gettoni inventati.

// gettoneFlaskProva ha la forma di secrets.token_urlsafe(24) del Flask: 32 caratteri url-safe.
const gettoneFlaskProva = "SINTETICOflaskLotto_32caratteri-"

// lottoInviatoCon è un lotto dell'oreficeria 2 inviato, con il gettone dato e le buste.
func lottoInviatoCon(t *testing.T, b *Buste, gettone string, buste ...int64) int64 {
	t.Helper()
	l := lotto(t, b, true, buste...)
	esegui(t, b.d, `UPDATE lotti_avviso SET token = ? WHERE id = ?`, gettone, l)
	return l
}

func consegnaProva(t *testing.T, b *Buste, ids ...int64) {
	t.Helper()
	for _, id := range ids {
		if _, err := b.CambiaStato(ctx, id, "consegnata", chi); err != nil {
			t.Fatal(err)
		}
	}
}

// inventario: ORE-14
// Il lotto si cerca per uguaglianza esatta del gettone, solo inviato e con l'accesso pubblico acceso: un gettone
// che non c'è, vuoto, con un carattere in più o in meno o di maiuscole diverse, un lotto non inviato e un lotto
// importato spento («disabled-import-…») sono lo stesso rifiuto, e nessuno scrive niente. Il gettone di 32
// caratteri del Flask apre il suo lotto.
func TestLottoPubblicoSoloInviatoEAcceso(t *testing.T) {
	b, d, _ := prova(t)
	uno := prontaDiOreficeria(t, b)
	l := lottoInviatoCon(t, b, gettoneFlaskProva, uno)
	if len(gettoneFlaskProva) != 32 {
		t.Fatal("il gettone di prova deve avere 32 caratteri")
	}
	p, err := b.PaginaLottoPubblica(ctx, gettoneFlaskProva)
	if err != nil || p.ID != l || p.Oreficeria != "Oreficeria Esempio" || len(p.Buste) != 1 || p.Buste[0].ID != uno {
		t.Fatalf("lotto inviato: %+v %v", p, err)
	}
	aperto := lotto(t, b, false, prontaDiOreficeria(t, b))
	esegui(t, d, `UPDATE lotti_avviso SET token = 'gettone-aperto' WHERE id = ?`, aperto)
	spento := lottoInviatoCon(t, b, "disabled-import-7", prontaDiOreficeria(t, b))
	esegui(t, d, `UPDATE lotti_avviso SET accesso_pubblico_spento_il = ? WHERE id = ?`, istanteProva, spento)
	prima := scattaInvio(t, b)
	for _, g := range []string{"", "nessuno", gettoneFlaskProva + "x", gettoneFlaskProva[:31], strings.ToUpper(gettoneFlaskProva),
		" " + gettoneFlaskProva, "gettone-aperto", "disabled-import-7", "%"} {
		if _, err := b.PaginaLottoPubblica(ctx, g); !errors.Is(err, ErrLottoNonDisponibile) {
			t.Errorf("%q: %v", g, err)
		}
	}
	nienteScritto(t, "rifiuti", prima, scattaInvio(t, b))
}

// inventario: ORE-14
// Le buste della pagina: solo quelle ancora dell'oreficeria del lotto e fuori dal cestino, dalla più recente, con il
// numero visibile (D-64), la data di pronta e di ritiro. Il prezzo mostrato è il finale se c'è (con «€» davanti a un
// importo o a un intervallo), altrimenti il preventivo com'è scritto (o «€ 12,50» se ha solo i centesimi). Il totale
// somma solo gli importi esatti e conta a parte le righe senza (un intervallo, nessun prezzo): mai letti come zero.
func TestLottoPubblicoBusteEPrezzi(t *testing.T) {
	b, d, _ := prova(t)
	finale, intervallo, niente, preventivo, soloCent, altra, cestino := prontaDiOreficeria(t, b), prontaDiOreficeria(t, b),
		prontaDiOreficeria(t, b), prontaDiOreficeria(t, b), prontaDiOreficeria(t, b), prontaDiOreficeria(t, b), prontaDiOreficeria(t, b)
	esegui(t, d, `UPDATE buste SET prezzo_testo = '40,00', prezzo_centesimi = 4000, preventivo_testo = NULL, preventivo_centesimi = NULL WHERE id = ?`, finale)
	esegui(t, d, `UPDATE buste SET prezzo_testo = '400/500', prezzo_centesimi = NULL, preventivo_testo = '30', preventivo_centesimi = 3000 WHERE id = ?`, intervallo)
	esegui(t, d, `UPDATE buste SET prezzo_testo = NULL, prezzo_centesimi = NULL, preventivo_testo = NULL, preventivo_centesimi = NULL WHERE id = ?`, niente)
	esegui(t, d, `UPDATE buste SET prezzo_testo = '', prezzo_centesimi = NULL, preventivo_testo = '77', preventivo_centesimi = 7700 WHERE id = ?`, preventivo)
	esegui(t, d, `UPDATE buste SET prezzo_testo = NULL, prezzo_centesimi = NULL, preventivo_testo = NULL, preventivo_centesimi = 1250 WHERE id = ?`, soloCent)
	esegui(t, d, `UPDATE buste SET codice_qr = 'RIP-2026-0045' WHERE id = ?`, finale)
	l := lottoInviatoCon(t, b, "gettone-prezzi", finale, intervallo, niente, preventivo, soloCent, altra, cestino)
	esegui(t, d, `INSERT INTO oreficerie (id, nome) VALUES (3, 'Altra Oreficeria')`)
	esegui(t, d, `UPDATE buste SET oreficeria_id = 3 WHERE id = ?`, altra)
	esegui(t, d, `UPDATE buste SET cancellata_il = ? WHERE id = ?`, istanteProva, cestino)
	consegnaProva(t, b, niente)
	p, err := b.PaginaLottoPubblica(ctx, "gettone-prezzi")
	if err != nil || p.ID != l {
		t.Fatal(err)
	}
	var ids []int64
	etichette := map[int64]string{}
	for _, x := range p.Buste {
		ids = append(ids, x.ID)
		etichette[x.ID] = x.EtichettaPrezzo()
	}
	if want := []int64{soloCent, preventivo, niente, intervallo, finale}; !ugualiID(ids, want) {
		t.Fatalf("buste %v, attese %v", ids, want)
	}
	for id, want := range map[int64]string{finale: "€ 40,00", intervallo: "€ 400/500", niente: "", preventivo: "77", soloCent: "€ 12,50"} {
		if etichette[id] != want {
			t.Errorf("prezzo di %d: %q, atteso %q", id, etichette[id], want)
		}
	}
	if c, senza := p.Totale(); c != 4000+7700+1250 || senza != 2 || !p.ConTotale() {
		t.Errorf("totale %d, senza importo %d", c, senza)
	}
	if x := p.Buste[4]; x.Numero != "0045/26" || x.DataPronta == nil || *x.DataPronta != "2026-10-31" || x.Ritirata() {
		t.Errorf("busta con etichetta: %+v", x)
	}
	if x := p.Buste[2]; x.Numero != "N. 3/26" || !x.Ritirata() || x.DataRitiro == nil || *x.DataRitiro != "2026-10-31" {
		t.Errorf("busta ritirata: %+v", x)
	}
	if p.DaRitirare() != 4 || p.Completato {
		t.Errorf("da ritirare %d, completato %v", p.DaRitirare(), p.Completato)
	}
	// Un lotto di una busta riparata gratis ha un totale, zero; uno di sole righe senza importo no.
	zero := prontaDiOreficeria(t, b)
	esegui(t, d, `UPDATE buste SET prezzo_testo = '0', prezzo_centesimi = 0 WHERE id = ?`, zero)
	lottoInviatoCon(t, b, "gettone-zero", zero)
	if p, _ := b.PaginaLottoPubblica(ctx, "gettone-zero"); !p.ConTotale() {
		t.Error("totale zero nascosto")
	}
	vuoto := prontaDiOreficeria(t, b)
	esegui(t, d, `UPDATE buste SET prezzo_testo = NULL, prezzo_centesimi = NULL, preventivo_testo = 'da vedere', preventivo_centesimi = NULL WHERE id = ?`, vuoto)
	lottoInviatoCon(t, b, "gettone-senza", vuoto)
	if p, _ := b.PaginaLottoPubblica(ctx, "gettone-senza"); p.ConTotale() {
		t.Error("totale senza importi")
	}
}

func ugualiID(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// inventario: ORE-15
// Leggere la pagina chiude il lotto quando tutte le sue buste sono consegnate (anche quelle del cestino contano), con
// l'istante di adesso e una volta sola: una seconda lettura non cambia la data. A metà non chiude; una busta nel
// cestino non consegnata tiene aperto il lotto anche se le visibili sono tutte ritirate, e la pagina non dice
// «completato». Un lotto vuoto non si chiude. Nessuna riga d'audit.
func TestLottoPubblicoChiudeAllaLettura(t *testing.T) {
	b, d, o := prova(t)
	uno, due := prontaDiOreficeria(t, b), prontaDiOreficeria(t, b)
	l := lottoInviatoCon(t, b, "gettone-chiusura", uno, due)
	consegnaProva(t, b, uno)
	if p, err := b.PaginaLottoPubblica(ctx, "gettone-chiusura"); err != nil || p.Completato || p.DaRitirare() != 1 || chiusoIl(t, b, l) != nil {
		t.Fatalf("a metà: %+v %v %v", p, err, chiusoIl(t, b, l))
	}
	// La consegna dell'ultima busta chiude già (slice 212): per provare la lettura il lotto torna aperto, come uno
	// completato prima della 212 (nessun backfill).
	consegnaProva(t, b, due)
	esegui(t, d, `UPDATE lotti_avviso SET chiuso_il = NULL WHERE id = ?`, l)
	o.Sposta(3600 * 1e9)
	audit := conta(t, d, `SELECT count(*) FROM eventi_audit`)
	p, err := b.PaginaLottoPubblica(ctx, "gettone-chiusura")
	if err != nil || !p.Completato || chiusoIl(t, b, l) != "2026-10-31T10:00:00.000000Z" ||
		valore(t, d, `SELECT aggiornato_il FROM lotti_avviso WHERE id = ?`, l) != "2026-10-31T10:00:00.000000Z" {
		t.Fatalf("completo: %+v %v %v", p, err, chiusoIl(t, b, l))
	}
	o.Sposta(3600 * 1e9)
	if p, err := b.PaginaLottoPubblica(ctx, "gettone-chiusura"); err != nil || !p.Completato || chiusoIl(t, b, l) != "2026-10-31T10:00:00.000000Z" {
		t.Fatalf("seconda lettura: %+v %v", p, err)
	}
	if n := conta(t, d, `SELECT count(*) FROM eventi_audit`); n != audit {
		t.Errorf("righe d'audit: %d, prima %d", n, audit)
	}
	// Una busta nel cestino non consegnata tiene aperto il lotto.
	tre, quattro := prontaDiOreficeria(t, b), prontaDiOreficeria(t, b)
	m := lottoInviatoCon(t, b, "gettone-cestino", tre, quattro)
	consegnaProva(t, b, tre)
	esegui(t, d, `UPDATE buste SET cancellata_il = ? WHERE id = ?`, istanteProva, quattro)
	if p, err := b.PaginaLottoPubblica(ctx, "gettone-cestino"); err != nil || p.Completato || p.DaRitirare() != 0 || len(p.Buste) != 1 || chiusoIl(t, b, m) != nil {
		t.Fatalf("cestino: %+v %v", p, err)
	}
	// Un lotto inviato senza buste non si chiude e non ne mostra.
	vuoto := lottoInviatoCon(t, b, "gettone-vuoto")
	if p, err := b.PaginaLottoPubblica(ctx, "gettone-vuoto"); err != nil || p.Completato || len(p.Buste) != 0 || chiusoIl(t, b, vuoto) != nil {
		t.Fatalf("vuoto: %+v %v", p, err)
	}
}

// inventario: ORE-15
// Dodici letture insieme di un lotto completo scrivono una sola data di chiusura e rispondono tutte.
func TestLottoPubblicoChiusuraInsieme(t *testing.T) {
	b, d, _ := prova(t)
	uno := prontaDiOreficeria(t, b)
	l := lottoInviatoCon(t, b, "gettone-gara", uno)
	consegnaProva(t, b, uno)
	esegui(t, d, `UPDATE lotti_avviso SET chiuso_il = NULL WHERE id = ?`, l)
	var wg sync.WaitGroup
	errori := make(chan error, 12)
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p, err := b.PaginaLottoPubblica(ctx, "gettone-gara")
			if err == nil && !p.Completato {
				err = errors.New("non completato")
			}
			errori <- err
		}()
	}
	wg.Wait()
	close(errori)
	for err := range errori {
		if err != nil {
			t.Error(err)
		}
	}
	if chiusoIl(t, b, l) != istanteProva {
		t.Errorf("chiuso il %v", chiusoIl(t, b, l))
	}
}

// fotoDelLotto mette nel lotto le foto vere di prova: l'archivio, poi una foto per busta.
func fotoDelLotto(t *testing.T, b *Buste, busta int64, nome string) Foto {
	t.Helper()
	in := filepath.Join(t.TempDir(), nome)
	if err := os.WriteFile(in, png1, 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := b.AllegaFoto(ctx, busta, []Caricamento{{Percorso: in, NomeOriginale: nome}}, nil, chi)
	if err != nil || len(f) != 1 {
		t.Fatalf("AllegaFoto: %v", err)
	}
	return f[0]
}

// inventario: ORE-16
// La foto di una busta del lotto si legge con il gettone del lotto se l'oreficeria la può vedere (la sua colonna, non
// quella del cliente), con la riga repair.public_photo_viewed «ok» e il pubblico «orefice». Una foto nascosta
// all'oreficeria, di un'altra busta o con un id finto dà niente con la riga «denied»; un gettone sbagliato, un lotto
// non inviato, una busta fuori dal lotto, passata a un'altra oreficeria, tornata al negozio o nel cestino non danno
// niente e non scrivono righe. La prima foto della pagina è la prima visibile all'oreficeria.
func TestFotoLottoPubblico(t *testing.T) {
	b, d, _ := provaConFoto(t)
	uno, due, fuori := prontaDiOreficeria(t, b), prontaDiOreficeria(t, b), prontaDiOreficeria(t, b)
	prima, seconda := fotoDelLotto(t, b, uno, "busta.png"), fotoDelLotto(t, b, uno, "oggetto.png")
	diDue, diFuori := fotoDelLotto(t, b, due, "due.png"), fotoDelLotto(t, b, fuori, "fuori.png")
	if !prima.Oreficeria || prima.Cliente {
		t.Fatalf("visibilità iniziale: %+v", prima)
	}
	l := lottoInviatoCon(t, b, "gettone-foto", uno, due)
	c := audit.Contesto{Attore: "public", IP: "203.0.113.5"}
	leggi := func(g string, busta int64, foto string, v Variante) *MediaFoto {
		t.Helper()
		m, err := b.LeggiFotoLotto(ctx, g, itoa(busta), foto, v, c)
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	if m := leggi("gettone-foto", uno, itoa(prima.ID), VarianteIntera); m == nil || m.Variante != VarianteIntera {
		t.Fatalf("foto: %+v", m)
	}
	if m := leggi("gettone-foto", uno, itoa(prima.ID), VarianteMiniatura); m == nil {
		t.Fatal("miniatura")
	}
	righe := righeAudit(t, b, "repair.public_photo_viewed")
	if len(righe) != 2 || !strings.HasPrefix(righe[0], itoa(uno)+`|{"audience":"orefice","photo_id":`+itoa(prima.ID)+`}`) ||
		!strings.Contains(righe[1], `"variant":"`) {
		t.Fatalf("righe: %v", righe)
	}
	p, err := b.PaginaLottoPubblica(ctx, "gettone-foto")
	if err != nil || p.Buste[1].FotoID == nil || *p.Buste[1].FotoID != prima.ID || p.Buste[0].FotoID == nil || *p.Buste[0].FotoID != diDue.ID {
		t.Fatalf("prime foto: %+v %v", p.Buste, err)
	}
	// Rifiuti con la riga «denied»: la foto di un'altra busta del lotto, un id finto, una foto nascosta.
	esegui(t, d, `UPDATE foto_buste SET visibile_oreficeria = 0 WHERE id = ?`, prima.ID)
	for _, foto := range []string{itoa(diDue.ID), "zero", "0", itoa(prima.ID)} {
		if m := leggi("gettone-foto", uno, foto, VarianteIntera); m != nil {
			t.Errorf("foto %s servita", foto)
		}
	}
	if righe := righeAudit(t, b, "repair.public_photo_viewed"); len(righe) != 6 || !strings.Contains(righe[3], `"photo_id":"invalid"`) {
		t.Errorf("righe denied: %v", righe)
	}
	if n := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.public_photo_viewed' AND esito = 'denied'`); n != 4 {
		t.Errorf("denied: %d", n)
	}
	// Nascosta la prima, la pagina mostra la seconda.
	if p, _ := b.PaginaLottoPubblica(ctx, "gettone-foto"); p.Buste[1].FotoID == nil || *p.Buste[1].FotoID != seconda.ID {
		t.Errorf("prima foto dopo averla nascosta: %+v", p.Buste[1])
	}
	// Rifiuti senza righe.
	prima2 := conta(t, d, `SELECT count(*) FROM eventi_audit`)
	for nome, x := range map[string]struct {
		g     string
		busta string
		foto  int64
	}{
		"gettone sbagliato":  {"nessuno", itoa(uno), seconda.ID},
		"busta fuori":        {"gettone-foto", itoa(fuori), diFuori.ID},
		"busta finta":        {"gettone-foto", "abc", seconda.ID},
		"busta zero":         {"gettone-foto", "0", seconda.ID},
		"gettone del lotto ": {"gettone-foto ", itoa(uno), seconda.ID},
	} {
		if m, err := b.LeggiFotoLotto(ctx, x.g, x.busta, itoa(x.foto), VarianteIntera, c); m != nil || err != nil {
			t.Errorf("%s: %v %v", nome, m, err)
		}
	}
	if n := conta(t, d, `SELECT count(*) FROM eventi_audit`); n != prima2 {
		t.Errorf("righe scritte dai rifiuti senza busta: %d, prima %d", n, prima2)
	}
	esegui(t, d, `INSERT INTO oreficerie (id, nome) VALUES (3, 'Altra Oreficeria')`)
	// Una busta di un secondo lotto inviato, della stessa oreficeria o di un'altra: con il gettone del lotto A niente e
	// nessuna riga; con il gettone del suo lotto la foto si legge (la barriera è il lotto, non solo l'oreficeria).
	stessa, altraOref := prontaDiOreficeria(t, b), prontaDiOreficeria(t, b)
	fStessa, fAltra := fotoDelLotto(t, b, stessa, "stessa.png"), fotoDelLotto(t, b, altraOref, "altra.png")
	lottoInviatoCon(t, b, "gettone-secondo", stessa)
	terzo := lottoInviatoCon(t, b, "gettone-terzo", altraOref)
	esegui(t, d, `UPDATE lotti_avviso SET oreficeria_id = 3 WHERE id = ?`, terzo)
	esegui(t, d, `UPDATE buste SET oreficeria_id = 3 WHERE id = ?`, altraOref)
	okPrima := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.public_photo_viewed' AND esito = 'ok'`)
	tutte := conta(t, d, `SELECT count(*) FROM eventi_audit`)
	for _, x := range []struct{ busta, foto int64 }{{stessa, fStessa.ID}, {altraOref, fAltra.ID}} {
		for _, v := range []Variante{VarianteIntera, VarianteMiniatura} {
			if m, err := b.LeggiFotoLotto(ctx, "gettone-foto", itoa(x.busta), itoa(x.foto), v, c); m != nil || err != nil {
				t.Errorf("busta %d di un altro lotto col gettone del lotto A: %v %v", x.busta, m, err)
			}
		}
	}
	if n := conta(t, d, `SELECT count(*) FROM eventi_audit`); n != tutte {
		t.Errorf("righe scritte per le buste di un altro lotto: %d, prima %d", n, tutte)
	}
	for g, x := range map[string]struct{ busta, foto int64 }{"gettone-secondo": {stessa, fStessa.ID}, "gettone-terzo": {altraOref, fAltra.ID}} {
		if m, err := b.LeggiFotoLotto(ctx, g, itoa(x.busta), itoa(x.foto), VarianteIntera, c); m == nil || err != nil {
			t.Errorf("%s: la foto del proprio lotto non si legge: %v", g, err)
		}
	}
	if n := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.public_photo_viewed' AND esito = 'ok'`); n != okPrima+2 {
		t.Errorf("righe ok: %d, attese %d", n, okPrima+2)
	}
	prima2 = conta(t, d, `SELECT count(*) FROM eventi_audit`) // dopo le buste, le foto e le letture qui sopra
	for nome, sql := range map[string]string{
		"altra oreficeria": `UPDATE buste SET oreficeria_id = 3 WHERE id = ?`,
		"al negozio":       `UPDATE buste SET oreficeria_id = NULL WHERE id = ?`,
		"nel cestino":      `UPDATE buste SET cancellata_il = '` + istanteProva + `' WHERE id = ?`,
	} {
		esegui(t, d, sql, due)
		if m, err := b.LeggiFotoLotto(ctx, "gettone-foto", itoa(due), itoa(diDue.ID), VarianteIntera, c); m != nil || err != nil {
			t.Errorf("%s: %v %v", nome, m, err)
		}
		esegui(t, d, `UPDATE buste SET oreficeria_id = 2, cancellata_il = NULL WHERE id = ?`, due)
	}
	esegui(t, d, `UPDATE lotti_avviso SET inviato_il = NULL WHERE id = ?`, l)
	if m, err := b.LeggiFotoLotto(ctx, "gettone-foto", itoa(uno), itoa(seconda.ID), VarianteIntera, c); m != nil || err != nil {
		t.Errorf("lotto non inviato: %v %v", m, err)
	}
	if n := conta(t, d, `SELECT count(*) FROM eventi_audit`); n != prima2 {
		t.Errorf("righe scritte dai rifiuti senza busta: %d, prima %d", n, prima2)
	}
}

// provaConFoto è prova con l'archivio delle foto (normalizzazione nel processo del test, niente miniature).
func provaConFoto(t *testing.T) (*Buste, *db.DB, *clock.Finto) {
	t.Helper()
	b, d, o := prova(t)
	b.ConFoto(media.Nuovo(filepath.Join(t.TempDir(), media.Cartella)), nelProcesso{})
	return b, d, o
}

// inventario: ORE-15
// Solo del Go, senza effetti visibili: la lettura di un lotto che non si chiude (a metà, o già chiuso) non prende la
// connessione di scrittura, quindi risponde anche mentre un'altra scrittura del negozio la tiene.
func TestLottoPubblicoNonPrendeLaScritturaSenzaChiudere(t *testing.T) {
	b, d, _ := prova(t)
	uno, due := prontaDiOreficeria(t, b), prontaDiOreficeria(t, b)
	lottoInviatoCon(t, b, "gettone-a-meta", uno, due)
	consegnaProva(t, b, uno)
	tre := prontaDiOreficeria(t, b)
	lottoInviatoCon(t, b, "gettone-chiuso", tre)
	consegnaProva(t, b, tre)
	tx, err := d.Scrittura.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	c, fine := context.WithTimeout(ctx, 2*time.Second)
	defer fine()
	for _, g := range []string{"gettone-a-meta", "gettone-chiuso"} {
		if _, err := b.PaginaLottoPubblica(c, g); err != nil {
			t.Errorf("%s con la scrittura occupata: %v", g, err)
		}
	}
}
