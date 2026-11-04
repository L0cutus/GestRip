package buste

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/db"
	"github.com/L0cutus/gestrip/internal/media"
)

// La firma di consegna (RIP-S-36…40), come delivery_signature_test.exs e
// delivery_signature_controller_test.exs dello standalone a go-baseline.

// pngFirma è un PNG vero di 1×1 (lo stesso dei test dello standalone).
var pngFirma = func() []byte {
	b, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR4nGNgYGBgAAAABQABh6FO1AAAAABJRU5ErkJggg==")
	if err != nil {
		panic(err)
	}
	return b
}()

// pngAltro è un altro PNG di 1×1 (other_png/0 dei test dello standalone).
var pngAltro = func() []byte {
	b, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAIAAACQd1PeAAAADElEQVR4nGP4f50BAASvAdfA0BA+AAAAAElFTkSuQmCC")
	if err != nil {
		panic(err)
	}
	return b
}()

func dataURL(b []byte) string { return "data:image/png;base64," + base64.StdEncoding.EncodeToString(b) }

// provaFirma è il database di prova con l'archivio dei media e una busta pronta.
type provaFirma struct {
	b     *Buste
	d     *db.DB
	a     *media.Archivio
	dir   string
	busta int64
}

func conFirma(t *testing.T) *provaFirma {
	t.Helper()
	b, d, _ := prova(t)
	dir := filepath.Join(t.TempDir(), media.Cartella)
	a := media.Nuovo(dir)
	b.ConFoto(a, nil)
	p := &provaFirma{b: b, d: d, a: a, dir: dir}
	p.busta = p.pronta(t)
	return p
}

// pronta crea una busta e la porta a «pronta» (data di pronta oggi).
func (p *provaFirma) pronta(t *testing.T) int64 {
	t.Helper()
	id := crea(t, p.b, modulo()).ID
	if _, err := p.b.CambiaStato(ctx, id, "pronta", chi); err != nil {
		t.Fatal(err)
	}
	return id
}

// fileNellArchivio sono i nomi dei file nella cartella media (nessuno se la cartella non c'è).
func (p *provaFirma) fileNellArchivio(t *testing.T) []string {
	t.Helper()
	ds, err := os.ReadDir(p.dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var n []string
	for _, x := range ds {
		n = append(n, x.Name())
	}
	return n
}

// rigaFirma è quello che la busta dice della sua firma e della consegna.
type rigaFirma struct {
	Chiave, Il, RitirataDa, Byte, Stato, Consegna, Conservazione any
}

func (p *provaFirma) riga(t *testing.T, id int64) rigaFirma {
	t.Helper()
	var r rigaFirma
	if err := p.d.Lettura.QueryRow(`SELECT firma_chiave, firma_il, firma_ritirata_da, firma_byte, stato, data_consegna,
		conservazione_iniziata_il FROM buste WHERE id = ?`, id).Scan(&r.Chiave, &r.Il, &r.RitirataDa, &r.Byte, &r.Stato, &r.Consegna,
		&r.Conservazione); err != nil {
		t.Fatal(err)
	}
	return r
}

// inventario: RIP-S-38
// decode/1: solo il data URL del riquadro, alla lettera, con un base64 come Base.decode64/1 (misurato
// nell'immagine fab-222: padding obbligatorio, niente spazi né a capo, i bit di riempimento non
// contano) e un PNG (i byte magici) di al più 500 KiB.
func TestDecodificaFirma(t *testing.T) {
	if b, err := DecodificaFirma(dataURL(pngFirma)); err != nil || !bytes.Equal(b, pngFirma) {
		t.Fatalf("%v %x", err, b)
	}
	alLimite := append(append([]byte{}, magicoPNG...), make([]byte, MaxByteFirma-len(magicoPNG))...)
	if b, err := DecodificaFirma(dataURL(alLimite)); err != nil || len(b) != MaxByteFirma {
		t.Errorf("500 KiB: %v %d", err, len(b))
	}
	for nome, v := range map[string]string{
		"vuoto":              "",
		"solo il prefisso":   "data:image/png;base64,",
		"base64 senza URL":   base64.StdEncoding.EncodeToString(pngFirma),
		"JPEG":               "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(pngFirma),
		"maiuscole":          "DATA:image/png;base64," + base64.StdEncoding.EncodeToString(pngFirma),
		"spazio davanti":     " " + dataURL(pngFirma),
		"non un PNG":         "data:image/png;base64,bm9uLXBuZw==",
		"senza padding":      strings.TrimRight(dataURL(pngFirma), "="),
		"a capo":             dataURL(pngFirma)[:40] + "\n" + dataURL(pngFirma)[40:],
		"ritorno a capo":     dataURL(pngFirma)[:40] + "\r" + dataURL(pngFirma)[40:],
		"spazio":             dataURL(pngFirma)[:40] + " " + dataURL(pngFirma)[40:],
		"base64 URL":         "data:image/png;base64," + base64.URLEncoding.EncodeToString(append(append([]byte{}, magicoPNG...), 0xfb, 0xff)),
		"padding mancante":   "data:image/png;base64,iVBORw0KGgo",
		"oltre 500 KiB":      dataURL(append(alLimite, 0)),
		"magici incompleti":  dataURL(magicoPNG[:7]),
		"un byte dei magici": dataURL(append([]byte("\x89PNG\r\n\x1a\x0a")[:7:7], 'x')),
	} {
		if b, err := DecodificaFirma(v); !errors.Is(err, ErrFirmaNonValida) || b != nil {
			t.Errorf("%s: %v %x", nome, err, b)
		}
	}
	if b, err := DecodificaFirma("data:image/png;base64," + "iVBORw0KGgp="); err != nil || !bytes.Equal(b, []byte("\x89PNG\r\n\x1a\n")) {
		// «Cgp=» → i bit di riempimento di «p» non sono zero: accettato, come Base.decode64/1.
		t.Errorf("riempimento non a zero: %v %q", err, b)
	}
}

// inventario: RIP-S-36, RIP-S-38
// collection_changes/2: lo stato «ritirata» è consegnata (riparazioni) o chiusa (compro oro); ci si
// arriva solo con il grafo; chi c'è già può firmare senza cambi.
func TestRitirabile(t *testing.T) {
	for _, c := range []struct {
		stato     string
		oro       bool
		cambia, k bool
	}{
		{"pronta", false, true, true}, {"consegnata", false, false, true},
		{"in_lavorazione", false, false, false}, {"preventivo", false, false, false},
		{"in_attesa_materiali", false, false, false}, {"annullata", false, false, false},
		{"perfezionata", true, true, true}, {"chiusa", true, false, true},
		{"in_valutazione", true, false, false}, {"valutata", true, false, false}, {"annullata", true, false, false},
		{"chiusa", false, false, false}, {"consegnata", true, false, false}, {"", false, false, false},
	} {
		if cambia, ok := Ritirabile(c.stato, c.oro); cambia != c.cambia || ok != c.k {
			t.Errorf("%s (oro %v): %v %v", c.stato, c.oro, cambia, ok)
		}
	}
}

// inventario: RIP-S-38
// La firma di una busta pronta è la consegna, nella stessa transazione: i byte com'erano nel file con
// il nome del loro SHA-256, la busta consegnata con la data di oggi e l'orologio della conservazione,
// la riga «captured» del registro e repair.signature_captured (se qualcun altro ha ritirato, mai chi).
// Nessuna repair.status_changed e nessuna notifica.
func TestCatturaFirmaConsegna(t *testing.T) {
	p := conFirma(t)
	id := itoa(p.busta)
	if err := p.b.CatturaFirma(ctx, id, dataURL(pngFirma), "  Mario Rossi  ", chi); err != nil {
		t.Fatal(err)
	}
	k := sha(pngFirma)
	r := p.riga(t, p.busta)
	if r.Chiave != k || r.Il != istanteProva || r.RitirataDa != "Mario Rossi" || r.Byte != int64(len(pngFirma)) ||
		r.Stato != "consegnata" || r.Consegna != "2026-10-31" || r.Conservazione != istanteProva {
		t.Fatalf("%+v", r)
	}
	if got, err := p.a.Leggi(k); err != nil || !bytes.Equal(got, pngFirma) {
		t.Fatalf("archivio: %v", err)
	}
	if n := conta(t, p.d, `SELECT count(*) FROM eventi_firma_consegna WHERE busta_id = ? AND evento = 'captured' AND chiave = ?
		AND ritirata_da = 'Mario Rossi' AND attore = ? AND avvenuto_il = ?`, p.busta, k, chi.Attore, istanteProva); n != 1 {
		t.Errorf("registro: %d", n)
	}
	if n := conta(t, p.d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.signature_captured' AND esito = 'ok'
		AND id_risorsa = ? AND attore = ? AND dati = ?`, id, chi.Attore, `{"named_collector":true,"storage_key":"`+k+`"}`); n != 1 {
		t.Errorf("audit: %d", n)
	}
	if n := conta(t, p.d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.status_changed' AND id_risorsa = ?`, id); n != 1 {
		t.Errorf("status_changed: %d (solo quello di pronta)", n) // la firma non scrive repair.status_changed
	}
	if v, err := p.b.Versione(ctx, p.busta); err != nil || v != 2 {
		t.Errorf("versione: %d %v", v, err) // pronta e consegnata: il trigger conta anche la firma
	}
}

// sha è la chiave dell'archivio: lo SHA-256 dei byte in esadecimale minuscolo.
func sha(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// inventario: RIP-S-38
// «Ritirato da»: spazi Unicode ai bordi tolti, i primi 120 grafemi, vuoto o di soli spazi è nessuno.
func TestCatturaFirmaRitiratoDa(t *testing.T) {
	p := conFirma(t)
	lungo := strings.Repeat("é", 125) // 125 grafemi di due rune
	for _, c := range []struct {
		dato   string
		atteso any
		nomina bool
	}{
		{"", nil, false}, {" \t\n ", nil, false}, {lungo, strings.Repeat("é", 120), true}, {" Bea ", "Bea", true},
	} {
		id := p.pronta(t)
		if err := p.b.CatturaFirma(ctx, itoa(id), dataURL(pngFirma), c.dato, chi); err != nil {
			t.Fatal(err)
		}
		if r := p.riga(t, id); r.RitirataDa != c.atteso {
			t.Errorf("%q: %v", c.dato, r.RitirataDa)
		}
		dati := valore(t, p.d, `SELECT dati FROM eventi_audit WHERE azione = 'repair.signature_captured' AND id_risorsa = ?`, itoa(id))
		if want := `{"named_collector":` + map[bool]string{true: "true", false: "false"}[c.nomina]; !strings.HasPrefix(dati.(string), want) {
			t.Errorf("%q: %v", c.dato, dati)
		}
	}
}

// inventario: RIP-S-38
// Una busta già consegnata si firma («Aggiungi firma»): stato, data di consegna e conservazione
// restano come sono. Un compro oro perfezionato va a «chiusa» senza data di consegna, con la
// conservazione; uno chiuso si firma e resta com'è.
func TestCatturaFirmaSuBustaGiaRitirata(t *testing.T) {
	p := conFirma(t)
	id := p.pronta(t)
	esegui(t, p.d, `UPDATE buste SET stato = 'consegnata', data_consegna = '2026-01-02', conservazione_iniziata_il = '2026-01-02T10:00:00.000000Z'
		WHERE id = ?`, id)
	if err := p.b.CatturaFirma(ctx, itoa(id), dataURL(pngFirma), "", chi); err != nil {
		t.Fatal(err)
	}
	if r := p.riga(t, id); r.Stato != "consegnata" || r.Consegna != "2026-01-02" || r.Conservazione != "2026-01-02T10:00:00.000000Z" ||
		r.Chiave == nil {
		t.Errorf("%+v", r)
	}
	oro := crea(t, p.b, con(modulo(), func(a *Accettazione) { a.CompraOro = "1" })).ID
	esegui(t, p.d, `UPDATE buste SET stato = 'perfezionata' WHERE id = ?`, oro)
	if err := p.b.CatturaFirma(ctx, itoa(oro), dataURL(pngFirma), "", chi); err != nil {
		t.Fatal(err)
	}
	if r := p.riga(t, oro); r.Stato != "chiusa" || r.Consegna != nil || r.Conservazione != istanteProva {
		t.Errorf("compro oro: %+v", r)
	}
	chiuso := crea(t, p.b, con(modulo(), func(a *Accettazione) { a.CompraOro = "1" })).ID
	esegui(t, p.d, `UPDATE buste SET stato = 'chiusa' WHERE id = ?`, chiuso)
	if err := p.b.CatturaFirma(ctx, itoa(chiuso), dataURL(pngFirma), "", chi); err != nil {
		t.Fatal(err)
	}
	if r := p.riga(t, chiuso); r.Stato != "chiusa" || r.Conservazione != nil {
		t.Errorf("compro oro chiuso: %+v", r)
	}
}

// inventario: RIP-S-38
// Uno stato da cui non si consegna (grafo RIP-S-13) rifiuta e non cambia niente nel database (la
// transazione si annulla); il file salvato prima della transazione resta nell'archivio, come quello
// di una foto la cui riga non si scrive. La firma già presente vince sul controllo dello stato.
func TestCatturaFirmaStatoNonConsegnabile(t *testing.T) {
	p := conFirma(t)
	for _, stato := range []string{"in_lavorazione", "preventivo", "in_attesa_materiali", "annullata"} {
		id := crea(t, p.b, modulo()).ID
		esegui(t, p.d, `UPDATE buste SET stato = ? WHERE id = ?`, stato, id)
		prima := scatta(t, p.d)
		eventi := conta(t, p.d, `SELECT count(*) FROM eventi_firma_consegna`)
		if err := p.b.CatturaFirma(ctx, itoa(id), dataURL(pngFirma), "Bea", chi); !errors.Is(err, ErrNonConsegnabile) {
			t.Fatalf("%s: %v", stato, err)
		}
		uguali(t, stato, prima, scatta(t, p.d))
		if r := p.riga(t, id); r.Chiave != nil || r.Stato != stato || r.Consegna != nil {
			t.Errorf("%s: %+v", stato, r)
		}
		if n := conta(t, p.d, `SELECT count(*) FROM eventi_firma_consegna`); n != eventi {
			t.Errorf("%s: registro %d", stato, n)
		}
	}
	if f := p.fileNellArchivio(t); len(f) != 1 || f[0] != sha(pngFirma) {
		t.Errorf("archivio: %v", f)
	}
	// Firmata e poi riaperta (o un import): la firma già presente vince.
	id := crea(t, p.b, modulo()).ID
	esegui(t, p.d, `UPDATE buste SET firma_chiave = ?, firma_byte = 1 WHERE id = ?`, sha(pngFirma), id)
	if err := p.b.CatturaFirma(ctx, itoa(id), dataURL(pngAltro), "", chi); !errors.Is(err, ErrFirmaGiaPresente) {
		t.Errorf("già firmata e in lavorazione: %v", err)
	}
}

// inventario: RIP-S-38
// Una sola firma per busta: la seconda è rifiutata e la prima resta (chiave e nome).
func TestCatturaFirmaSecondaRifiutata(t *testing.T) {
	p := conFirma(t)
	id := itoa(p.busta)
	if err := p.b.CatturaFirma(ctx, id, dataURL(pngFirma), "Ada", chi); err != nil {
		t.Fatal(err)
	}
	prima := p.riga(t, p.busta)
	if err := p.b.CatturaFirma(ctx, id, dataURL(pngAltro), "Bea", chi); !errors.Is(err, ErrFirmaGiaPresente) ||
		err.Error() != "La firma è già stata registrata su questa busta." {
		t.Fatalf("%v", err)
	}
	if r := p.riga(t, p.busta); r != prima {
		t.Errorf("%+v ≠ %+v", r, prima)
	}
	if n := conta(t, p.d, `SELECT count(*) FROM eventi_firma_consegna`); n != 1 {
		t.Errorf("registro: %d", n)
	}
}

// inventario: RIP-S-38
// Due conferme quasi contemporanee (doppio tocco, due telefoni): una firma e un rifiuto, mai due
// firme né una consegna senza firma. Si prova più volte, con firme diverse.
func TestCatturaFirmaConcorrente(t *testing.T) {
	p := conFirma(t)
	for giro := 0; giro < 10; giro++ {
		id := p.pronta(t)
		var wg sync.WaitGroup
		errs := make([]error, 2)
		via := make(chan struct{})
		for i, png := range [][]byte{pngFirma, pngAltro} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-via
				errs[i] = p.b.CatturaFirma(ctx, itoa(id), dataURL(png), "", audit.Contesto{Attore: "chi"})
			}()
		}
		close(via)
		wg.Wait()
		ok, rifiuti := 0, 0
		for _, err := range errs {
			switch {
			case err == nil:
				ok++
			case errors.Is(err, ErrFirmaGiaPresente):
				rifiuti++
			default:
				t.Fatalf("giro %d: %v", giro, err)
			}
		}
		if ok != 1 || rifiuti != 1 {
			t.Fatalf("giro %d: %v", giro, errs)
		}
		if n := conta(t, p.d, `SELECT count(*) FROM eventi_firma_consegna WHERE busta_id = ?`, id); n != 1 {
			t.Errorf("giro %d: righe %d", giro, n)
		}
		if n := conta(t, p.d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.signature_captured' AND id_risorsa = ?`, itoa(id)); n != 1 {
			t.Errorf("giro %d: audit %d", giro, n)
		}
		r := p.riga(t, id)
		vinta := pngFirma
		if errs[0] != nil {
			vinta = pngAltro
		}
		if r.Chiave != sha(vinta) || r.Stato != "consegnata" {
			t.Errorf("giro %d: %+v", giro, r)
		}
	}
}

// inventario: RIP-S-38
// Un campo non valido non scrive niente, nemmeno il file; una busta che non c'è, nel cestino o con un
// id non valido è «non trovata» e non scrive niente.
func TestCatturaFirmaRifiutiSenzaScritture(t *testing.T) {
	p := conFirma(t)
	cestino := p.pronta(t)
	esegui(t, p.d, `UPDATE buste SET cancellata_il = ? WHERE id = ?`, istanteProva, cestino)
	prima := scatta(t, p.d)
	for _, v := range []string{"", "data:image/png;base64,bm9uLXBuZw==", base64.StdEncoding.EncodeToString(pngFirma)} {
		if err := p.b.CatturaFirma(ctx, itoa(p.busta), v, "", chi); !errors.Is(err, ErrFirmaNonValida) || err.Error() != "Firma mancante o non valida." {
			t.Errorf("%q: %v", v, err)
		}
	}
	for _, id := range []string{"999", "0", "-1", "abc", " 1", "1.0", itoa(cestino), "99999999999999999999"} {
		if err := p.b.CatturaFirma(ctx, id, dataURL(pngFirma), "", chi); !errors.Is(err, ErrBustaNonTrovata) {
			t.Errorf("%q: %v", id, err)
		}
	}
	uguali(t, "rifiuti", prima, scatta(t, p.d))
	if f := p.fileNellArchivio(t); len(f) != 0 {
		t.Errorf("archivio: %v", f)
	}
	if r := p.riga(t, p.busta); r.Chiave != nil || r.Stato != "pronta" {
		t.Errorf("%+v", r)
	}
}

// inventario: RIP-S-38
// Tutto nella stessa transazione: se la riga d'audit o quella del registro non si scrivono, la busta
// resta pronta e senza firma.
func TestCatturaFirmaTransazione(t *testing.T) {
	for _, trigger := range []string{
		`CREATE TRIGGER no_audit BEFORE INSERT ON eventi_audit WHEN NEW.azione = 'repair.signature_captured' BEGIN SELECT RAISE(ABORT, 'no'); END`,
		`CREATE TRIGGER no_registro BEFORE INSERT ON eventi_firma_consegna BEGIN SELECT RAISE(ABORT, 'no'); END`,
	} {
		p := conFirma(t)
		esegui(t, p.d, trigger)
		prima := scatta(t, p.d)
		err := p.b.CatturaFirma(ctx, itoa(p.busta), dataURL(pngFirma), "Bea", chi)
		if err == nil || errors.Is(err, ErrFirmaNonValida) || errors.Is(err, ErrFirmaGiaPresente) {
			t.Fatalf("%s: %v", trigger, err)
		}
		uguali(t, trigger, prima, scatta(t, p.d))
		if r := p.riga(t, p.busta); r.Chiave != nil || r.Stato != "pronta" || r.Consegna != nil || r.Conservazione != nil {
			t.Errorf("%+v", r)
		}
		if n := conta(t, p.d, `SELECT count(*) FROM eventi_firma_consegna`); n != 0 {
			t.Errorf("registro: %d", n)
		}
	}
}

// inventario: RIP-S-38
// Senza archivio dei media la firma non si salva (nessuna scrittura).
func TestCatturaFirmaSenzaArchivio(t *testing.T) {
	b, d, _ := prova(t)
	id := crea(t, b, modulo()).ID
	if _, err := b.CambiaStato(ctx, id, "pronta", chi); err != nil {
		t.Fatal(err)
	}
	if err := b.CatturaFirma(ctx, itoa(id), dataURL(pngFirma), "", chi); !errors.Is(err, ErrSenzaArchivio) {
		t.Errorf("%v", err)
	}
	if n := conta(t, d, `SELECT count(*) FROM buste WHERE firma_chiave IS NOT NULL`); n != 0 {
		t.Error("firma scritta")
	}
}

// inventario: RIP-S-38
// Una firma che consegna l'ultima busta di un lotto inviato lo chiude (slice 212 dello standalone).
func TestCatturaFirmaChiudeIlLotto(t *testing.T) {
	p := conFirma(t)
	esegui(t, p.d, `INSERT INTO lotti_avviso (id, oreficeria_id, token, inviato_il, creato_il, aggiornato_il)
		VALUES (1, 2, 'tok-lotto', ?1, ?1, ?1)`, istanteProva)
	esegui(t, p.d, `UPDATE buste SET lotto_avviso_id = 1 WHERE id = ?`, p.busta)
	if err := p.b.CatturaFirma(ctx, itoa(p.busta), dataURL(pngFirma), "", chi); err != nil {
		t.Fatal(err)
	}
	if v := valore(t, p.d, `SELECT chiuso_il FROM lotti_avviso WHERE id = 1`); v != istanteProva {
		t.Errorf("lotto: %v", v)
	}
}

// inventario: RIP-S-38
// Una firma aggiunta a una busta già consegnata («Aggiungi firma») non consegna niente, quindi non
// chiude il lotto, anche se il lotto inviato ha tutte le buste consegnate ed è ancora aperto
// (`if changes != []` di capture/4; review della 047).
func TestCatturaFirmaAggiuntaNonChiudeIlLotto(t *testing.T) {
	p := conFirma(t)
	esegui(t, p.d, `INSERT INTO lotti_avviso (id, oreficeria_id, token, inviato_il, creato_il, aggiornato_il)
		VALUES (1, 2, 'tok-lotto', ?1, ?1, ?1)`, istanteProva)
	esegui(t, p.d, `UPDATE buste SET lotto_avviso_id = 1, stato = 'consegnata', data_consegna = '2026-03-02' WHERE id = ?`, p.busta)
	if err := p.b.CatturaFirma(ctx, itoa(p.busta), dataURL(pngFirma), "", chi); err != nil {
		t.Fatal(err)
	}
	if v := valore(t, p.d, `SELECT chiuso_il FROM lotti_avviso WHERE id = 1`); v != nil {
		t.Errorf("lotto chiuso da una firma aggiunta: %v", v)
	}
}

// inventario: RIP-S-40
// L'azzeramento toglie i quattro campi della firma e lascia stato e data di consegna; il file resta
// leggibile e il registro aggiunge una riga «reset» che lo nomina, con il nome di chi aveva ritirato;
// repair.signature_reset. Poi si può firmare di nuovo («Aggiungi firma»); senza firma: ErrNessunaFirma.
func TestAzzeraFirma(t *testing.T) {
	p := conFirma(t)
	id := itoa(p.busta)
	if err := p.b.CatturaFirma(ctx, id, dataURL(pngFirma), "Ada", chi); err != nil {
		t.Fatal(err)
	}
	k := sha(pngFirma)
	if err := p.b.AzzeraFirma(ctx, id, chi); err != nil {
		t.Fatal(err)
	}
	r := p.riga(t, p.busta)
	if r.Chiave != nil || r.Il != nil || r.RitirataDa != nil || r.Byte != nil || r.Stato != "consegnata" || r.Consegna != "2026-10-31" ||
		r.Conservazione != istanteProva {
		t.Fatalf("%+v", r)
	}
	if got, err := p.a.Leggi(k); err != nil || !bytes.Equal(got, pngFirma) {
		t.Errorf("il file della firma azzerata: %v", err)
	}
	if n := conta(t, p.d, `SELECT count(*) FROM eventi_firma_consegna WHERE busta_id = ? AND evento = 'reset' AND chiave = ?
		AND ritirata_da = 'Ada' AND attore = ?`, p.busta, k, chi.Attore); n != 1 {
		t.Errorf("registro: %d", n)
	}
	if n := conta(t, p.d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.signature_reset' AND id_risorsa = ? AND esito = 'ok'
		AND dati = ?`, id, `{"named_collector":true,"storage_key":"`+k+`"}`); n != 1 {
		t.Errorf("audit: %d", n)
	}
	prima := scatta(t, p.d)
	if err := p.b.AzzeraFirma(ctx, id, chi); !errors.Is(err, ErrNessunaFirma) || err.Error() != "Su questa busta non c'è nessuna firma da azzerare." {
		t.Errorf("di nuovo: %v", err)
	}
	uguali(t, "senza firma", prima, scatta(t, p.d))
	if pg, err := p.b.ApriFirma(ctx, id); err != nil || pg.ID != p.busta {
		t.Errorf("pagina dopo l'azzeramento: %v", err)
	}
	if err := p.b.CatturaFirma(ctx, id, dataURL(pngAltro), "", chi); err != nil {
		t.Fatal(err)
	}
	if r := p.riga(t, p.busta); r.Chiave != sha(pngAltro) || r.Stato != "consegnata" || r.Consegna != "2026-10-31" {
		t.Errorf("rifirmata: %+v", r)
	}
	var eventi []string
	righe, err := p.d.Lettura.Query(`SELECT evento FROM eventi_firma_consegna WHERE busta_id = ? ORDER BY id`, p.busta)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = righe.Close() }()
	for righe.Next() {
		var e string
		if err := righe.Scan(&e); err != nil {
			t.Fatal(err)
		}
		eventi = append(eventi, e)
	}
	if strings.Join(eventi, ",") != "captured,reset,captured" {
		t.Errorf("registro: %v", eventi)
	}
	for _, x := range []string{"999", "abc", "0"} {
		if err := p.b.AzzeraFirma(ctx, x, chi); !errors.Is(err, ErrBustaNonTrovata) {
			t.Errorf("%s: %v", x, err)
		}
	}
}

// inventario: RIP-S-40
// Se la riga del registro o dell'audit non si scrive, la firma resta.
func TestAzzeraFirmaTransazione(t *testing.T) {
	p := conFirma(t)
	if err := p.b.CatturaFirma(ctx, itoa(p.busta), dataURL(pngFirma), "", chi); err != nil {
		t.Fatal(err)
	}
	esegui(t, p.d, `CREATE TRIGGER no_reset BEFORE INSERT ON eventi_audit WHEN NEW.azione = 'repair.signature_reset' BEGIN SELECT RAISE(ABORT, 'no'); END`)
	if err := p.b.AzzeraFirma(ctx, itoa(p.busta), chi); err == nil || errors.Is(err, ErrNessunaFirma) {
		t.Fatalf("%v", err)
	}
	if r := p.riga(t, p.busta); r.Chiave != sha(pngFirma) {
		t.Errorf("%+v", r)
	}
	if n := conta(t, p.d, `SELECT count(*) FROM eventi_firma_consegna WHERE evento = 'reset'`); n != 0 {
		t.Errorf("registro: %d", n)
	}
}

// inventario: RIP-S-39
// La lettura dà i byte originali e scrive repair.signature_viewed «ok» per il personale; una busta
// senza firma, un file che manca, cambiato (impronta diversa) o una chiave non valida danno nil con
// la riga «denied»; una busta che non c'è nil senza riga. Se la riga non si scrive, niente byte.
func TestLeggiFirma(t *testing.T) {
	p := conFirma(t)
	id := itoa(p.busta)
	vista := func(esito string) int {
		return conta(t, p.d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.signature_viewed' AND id_risorsa = ? AND esito = ?
			AND dati = '{"audience":"staff"}' AND attore = ?`, id, esito, chi.Attore)
	}
	if b, err := p.b.LeggiFirma(ctx, id, chi); err != nil || b != nil || vista("denied") != 1 {
		t.Fatalf("senza firma: %v %x", err, b)
	}
	if err := p.b.CatturaFirma(ctx, id, dataURL(pngFirma), "", chi); err != nil {
		t.Fatal(err)
	}
	if b, err := p.b.LeggiFirma(ctx, id, chi); err != nil || !bytes.Equal(b, pngFirma) || vista("ok") != 1 {
		t.Fatalf("firmata: %v %x", err, b)
	}
	for _, x := range []string{"999", "abc", "0"} {
		if b, err := p.b.LeggiFirma(ctx, x, chi); err != nil || b != nil {
			t.Errorf("%s: %v %x", x, err, b)
		}
	}
	if n := conta(t, p.d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.signature_viewed'`); n != 2 {
		t.Errorf("righe: %d", n)
	}
	// L'impronta si ricontrolla a ogni lettura: un file cambiato non si serve.
	k := sha(pngFirma)
	f := filepath.Join(p.dir, k)
	if err := os.Chmod(f, 0o600); err != nil {
		t.Fatal(err)
	}
	cambiato := append(append([]byte{}, pngFirma...), 0)
	if err := os.WriteFile(f, cambiato, 0o600); err != nil {
		t.Fatal(err)
	}
	if b, err := p.b.LeggiFirma(ctx, id, chi); err != nil || b != nil || vista("denied") != 2 {
		t.Errorf("file cambiato: %v %x", err, b)
	}
	if err := os.Remove(f); err != nil {
		t.Fatal(err)
	}
	if b, err := p.b.LeggiFirma(ctx, id, chi); err != nil || b != nil || vista("denied") != 3 {
		t.Errorf("file mancante: %v %x", err, b)
	}
	esegui(t, p.d, `UPDATE buste SET firma_chiave = '../fuori' WHERE id = ?`, p.busta)
	if b, err := p.b.LeggiFirma(ctx, id, chi); err != nil || b != nil || vista("denied") != 4 {
		t.Errorf("chiave non valida: %v %x", err, b)
	}
	esegui(t, p.d, `UPDATE buste SET firma_chiave = ? WHERE id = ?`, k, p.busta)
	esegui(t, p.d, `CREATE TRIGGER no_vista BEFORE INSERT ON eventi_audit WHEN NEW.azione = 'repair.signature_viewed' BEGIN SELECT RAISE(ABORT, 'no'); END`)
	if _, err := p.a.Salva(pngFirma); err != nil {
		t.Fatal(err)
	}
	if b, err := p.b.LeggiFirma(ctx, id, chi); err == nil || b != nil {
		t.Errorf("senza riga: %v %x", err, b)
	}
}

// inventario: RIP-S-36, RIP-S-39, RIP-S-40
// Una firma importata dallo standalone (le stesse colonne, il file nell'archivio con il suo SHA-256,
// la riga «captured» del registro; firma_il può mancare): la scheda la mostra «(data non
// registrata)», la lettura dà i byte, l'azzeramento la toglie e nomina il file nel registro.
func TestFirmaImportata(t *testing.T) {
	p := conFirma(t)
	finta := []byte("\x89PNG\r\n\x1a\nsynthetic-signature") // la firma della cattura sintetica: non decodificabile
	k, err := p.a.Salva(finta)
	if err != nil {
		t.Fatal(err)
	}
	id := crea(t, p.b, modulo()).ID
	esegui(t, p.d, `UPDATE buste SET stato = 'consegnata', data_consegna = '2026-01-09', firma_chiave = ?, firma_ritirata_da = 'Cliente 3',
		firma_byte = ? WHERE id = ?`, k, len(finta), id)
	esegui(t, p.d, `INSERT INTO eventi_firma_consegna (busta_id, evento, chiave, avvenuto_il, creato_il, aggiornato_il)
		VALUES (?, 'captured', ?, ?3, ?3, ?3)`, id, k, istanteProva)
	s, err := p.b.Scheda(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if s.Firma == nil || s.Firma.Timbro() != "Firmata (data non registrata) — ritirata da Cliente 3" {
		t.Fatalf("%+v", s.Firma)
	}
	if b, err := p.b.LeggiFirma(ctx, itoa(id), chi); err != nil || !bytes.Equal(b, finta) {
		t.Fatalf("%v %x", err, b)
	}
	if _, err := p.b.ApriFirma(ctx, itoa(id)); !errors.Is(err, ErrFirmaGiaPresente) {
		t.Errorf("pagina: %v", err)
	}
	if err := p.b.AzzeraFirma(ctx, itoa(id), chi); err != nil {
		t.Fatal(err)
	}
	if n := conta(t, p.d, `SELECT count(*) FROM eventi_firma_consegna WHERE busta_id = ? AND evento = 'reset' AND chiave = ? AND ritirata_da = 'Cliente 3'`,
		id, k); n != 1 {
		t.Errorf("registro: %d", n)
	}
	if r := p.riga(t, id); r.Chiave != nil || r.Stato != "consegnata" || r.Consegna != "2026-01-09" {
		t.Errorf("%+v", r)
	}
}

// inventario: RIP-S-36, RIP-S-37
// La pagina di firma: busta che non c'è (anche nel cestino o con un id non valido), già firmata
// (prima dello stato), che non può arrivare allo stato «ritirata», o i dati della pagina.
func TestApriFirma(t *testing.T) {
	p := conFirma(t)
	pg, err := p.b.ApriFirma(ctx, itoa(p.busta))
	if err != nil || pg.ID != p.busta || pg.Numero == "" || pg.NomeCliente != "Cliente Prova" || pg.Descrizione != "Anello da stringere" {
		t.Fatalf("%+v %v", pg, err)
	}
	lavoro := crea(t, p.b, modulo()).ID
	if _, err := p.b.ApriFirma(ctx, itoa(lavoro)); !errors.Is(err, ErrNonConsegnabile) ||
		err.Error() != "Questa busta non può essere consegnata da dove si trova: firma non disponibile." {
		t.Errorf("in lavorazione: %v", err)
	}
	esegui(t, p.d, `UPDATE buste SET firma_chiave = ?, firma_byte = 1 WHERE id = ?`, sha(pngFirma), lavoro)
	if _, err := p.b.ApriFirma(ctx, itoa(lavoro)); !errors.Is(err, ErrFirmaGiaPresente) {
		t.Errorf("firmata in lavorazione: %v", err)
	}
	esegui(t, p.d, `UPDATE buste SET cancellata_il = ? WHERE id = ?`, istanteProva, p.busta)
	for _, x := range []string{itoa(p.busta), "999", "abc", "-1", "0"} {
		if _, err := p.b.ApriFirma(ctx, x); !errors.Is(err, ErrBustaNonTrovata) {
			t.Errorf("%s: %v", x, err)
		}
	}
}

// inventario: RIP-S-36
// Il timbro della scheda: nel fuso del negozio (anche a cavallo della mezzanotte), « — ritirata da »
// solo con un nome.
func TestTimbroDellaFirma(t *testing.T) {
	p := conFirma(t)
	if err := p.b.CatturaFirma(ctx, itoa(p.busta), dataURL(pngFirma), "Mario Rossi", chi); err != nil {
		t.Fatal(err)
	}
	esegui(t, p.d, `UPDATE buste SET firma_il = '2026-07-15T22:30:00.000000Z' WHERE id = ?`, p.busta)
	s, err := p.b.Scheda(ctx, p.busta)
	if err != nil {
		t.Fatal(err)
	}
	if s.Firma == nil || s.Firma.Timbro() != "Firmata il 16/07/2026 00:30 — ritirata da Mario Rossi" || s.Firma.Chiave != sha(pngFirma) {
		t.Fatalf("%+v", s.Firma)
	}
	esegui(t, p.d, `UPDATE buste SET firma_ritirata_da = NULL WHERE id = ?`, p.busta)
	if s, _ := p.b.Scheda(ctx, p.busta); s.Firma.Timbro() != "Firmata il 16/07/2026 00:30" {
		t.Errorf("%q", s.Firma.Timbro())
	}
	if s, _ := p.b.Scheda(ctx, crea(t, p.b, modulo()).ID); s.Firma != nil {
		t.Errorf("senza firma: %+v", s.Firma)
	}
}

// inventario: RIP-S-38
// Il blocco è la scrittura, mai una lettura prima: una firma arrivata fra la lettura della busta e
// la transazione (qui scriviFirma chiamata su una busta già firmata, senza lucchetto) è un rifiuto e
// non tocca la prima.
func TestCatturaFirmaBloccoNellaScrittura(t *testing.T) {
	p := conFirma(t)
	if err := p.b.CatturaFirma(ctx, itoa(p.busta), dataURL(pngFirma), "Ada", chi); err != nil {
		t.Fatal(err)
	}
	prima := p.riga(t, p.busta)
	if err := p.b.scriviFirma(ctx, p.busta, sha(pngAltro), int64(len(pngAltro)), "Bea", chi); !errors.Is(err, ErrFirmaGiaPresente) {
		t.Fatalf("%v", err)
	}
	if r := p.riga(t, p.busta); r != prima {
		t.Errorf("%+v", r)
	}
}

// inventario: RIP-S-38
// Una busta pronta che ha già una data di consegna (consegnata, riaperta e di nuovo pronta, come la
// 15/26 del banco) torna consegnata con la sua data di prima: la data storica non si sovrascrive.
func TestCatturaFirmaTieneLaDataDiConsegna(t *testing.T) {
	p := conFirma(t)
	esegui(t, p.d, `UPDATE buste SET data_consegna = '2026-03-02' WHERE id = ?`, p.busta)
	if err := p.b.CatturaFirma(ctx, itoa(p.busta), dataURL(pngFirma), "", chi); err != nil {
		t.Fatal(err)
	}
	if r := p.riga(t, p.busta); r.Stato != "consegnata" || r.Consegna != "2026-03-02" || r.Conservazione != istanteProva {
		t.Errorf("%+v", r)
	}
}
