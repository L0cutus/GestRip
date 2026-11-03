package web

import (
	"strconv"
	"strings"
	"testing"

	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/pdf/lettura"
)

// La ricevuta PDF della busta (RIP-S-42), GET /repairs/{id}/receipt.pdf, come ReceiptController e
// receipt_controller_test.exs dello standalone a go-baseline, con le risposte misurate sul banco il
// 03/11/2026 (gestrip-standalone:fab-220).

func percorsoRicevuta(id string) string { return "/repairs/" + id + "/receipt.pdf" }

// inventario: RIP-S-42
// Titolare e dipendente scaricano la ricevuta: un PDF valido di 80×297 mm con la busta, allegato
// «ricevuta-1-26.pdf», application/pdf e private, no-store; ognuno scrive la sua riga
// repair.receipt_generated e nessuna repair.viewed.
func TestRicevutaScaricata(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	id := strconv.FormatInt(bustaScheda(t, d, o, nil), 10)
	for _, b := range []*browser{titolareDentro(t, h), dipendenteDentro(t, h)} {
		r := b.vai("GET", percorsoRicevuta(id), nil, nil)
		if r.codice != 200 || !unaVolta(r, "Content-Type", "application/pdf") || !unaVolta(r, "Cache-Control", "private, no-store") ||
			!unaVolta(r, "Content-Disposition", `attachment; filename="ricevuta-1-26.pdf"`) ||
			r.h.Get("Content-Length") != strconv.Itoa(len(r.corpo)) {
			t.Fatalf("%d %v", r.codice, r.h)
		}
		l, err := lettura.Leggi([]byte(r.corpo))
		if err != nil {
			t.Fatal(err)
		}
		if lettura.Formato(l) != "80x297" || !strings.Contains(lettura.TestoNormalizzato(l), "Busta 1/26 CLIENTE Cliente storico TELEFONO 333 111") {
			t.Errorf("%s: %q", lettura.Formato(l), lettura.TestoNormalizzato(l))
		}
	}
	if n := contaCon(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.receipt_generated' AND esito = 'ok'
		AND id_risorsa = ? AND dati = '{"bag_number":"1/26"}'`, id); n != 2 {
		t.Errorf("righe: %d", n)
	}
	for _, email := range []string{"titolare@esempio.it", "banco@esempio.it"} {
		if n := contaCon(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.receipt_generated' AND attore = ?`, email); n != 1 {
			t.Errorf("%s: %d righe", email, n)
		}
	}
	if n := contaCon(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.viewed'`); n != 0 {
		t.Errorf("repair.viewed: %d", n)
	}
}

// inventario: RIP-S-42
// Senza accesso si va a /login (pipeline :require_authenticated_user) e non si scrive niente.
func TestRicevutaSenzaAccesso(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	id := strconv.FormatInt(bustaScheda(t, d, o, nil), 10)
	if r := nuovoBrowser(t, h).vai("GET", percorsoRicevuta(id), nil, nil); !redirectA(r, "/login") {
		t.Errorf("%d %q", r.codice, r.location)
	}
	if n := contaCon(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.receipt_generated'`); n != 0 {
		t.Errorf("righe: %d", n)
	}
}

// inventario: RIP-S-42
// Le risposte di get_repair!/1 misurate sullo standalone: busta che non c'è, zero o negativa, o nel
// cestino → 404 «Not Found»; id che non è un intero («abc», « 1», «1.0», «1%2F2») → 400 «Bad
// Request». Oltre i 64 bit lo standalone risponde 500, il Go 404 «Not Found» come una busta che non c'è
// (D-56, Stefano 03/11/2026). Segno e zeri davanti aprono la busta.
func TestRicevutaIdComeLoStandalone(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	id := bustaScheda(t, d, o, nil)
	b := titolareDentro(t, h)
	for _, c := range []struct {
		id     string
		codice int
		corpo  string
	}{
		{"999", 404, "Not Found"}, {"0", 404, "Not Found"}, {"-1", 404, "Not Found"},
		{"abc", 400, "Bad Request"}, {"%201", 400, "Bad Request"}, {"1.0", 400, "Bad Request"}, {"1%2F2", 400, "Bad Request"},
		{"99999999999999999999", 404, "Not Found"}, {"9223372036854775808", 404, "Not Found"}, {"-99999999999999999999", 404, "Not Found"},
		{"9223372036854775807", 404, "Not Found"},
	} {
		r := b.vai("GET", percorsoRicevuta(c.id), nil, nil)
		if r.codice != c.codice || r.corpo != c.corpo || !unaVolta(r, "Content-Type", "text/html; charset=utf-8") ||
			!unaVolta(r, "Cache-Control", "max-age=0, private, must-revalidate") || r.h.Get("Content-Disposition") != "" {
			t.Errorf("%s: %d %q %v", c.id, r.codice, r.corpo, r.h)
		}
	}
	for _, s := range []string{"+1", "01"} {
		if r := b.vai("GET", percorsoRicevuta(s), nil, nil); r.codice != 200 {
			t.Errorf("%s: %d", s, r.codice)
		}
	}
	eseguiSQL(t, d, `UPDATE buste SET cancellata_il = '2026-10-30T09:00:00.000000Z' WHERE id = ?`, id)
	if r := b.vai("GET", percorsoRicevuta(strconv.FormatInt(id, 10)), nil, nil); r.codice != 404 || r.corpo != "Not Found" {
		t.Errorf("cestino: %d %q", r.codice, r.corpo)
	}
	if n := contaCon(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.receipt_generated'`); n != 2 {
		t.Errorf("righe: %d", n)
	}
}

// inventario: RIP-S-42
// Se la riga d'audit non si scrive la ricevuta non esce: 503 «PDF non disponibile», senza tipo né
// allegato (send_resp del ramo else del controller).
func TestRicevutaNonDisponibile(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	id := strconv.FormatInt(bustaScheda(t, d, o, nil), 10)
	eseguiSQL(t, d, `CREATE TRIGGER no_ricevuta BEFORE INSERT ON eventi_audit WHEN NEW.azione = 'repair.receipt_generated'
		BEGIN SELECT RAISE(ABORT, 'no'); END`)
	r := titolareDentro(t, h).vai("GET", percorsoRicevuta(id), nil, nil)
	if r.codice != 503 || r.corpo != "PDF non disponibile" || len(r.h.Values("Content-Type")) != 0 ||
		r.h.Get("Content-Disposition") != "" || !unaVolta(r, "Cache-Control", "max-age=0, private, must-revalidate") {
		t.Errorf("%d %q %v", r.codice, r.corpo, r.h)
	}
}

// inventario: RIP-S-42
// Il nome dell'allegato come send_download/3 di Phoenix 1.8.13 (misurato nell'immagine fab-220):
// solo lettere, cifre e «-._~» restano, il resto in %XX maiuscolo, e allora c'è anche filename*.
func TestDisposizioneDellaRicevuta(t *testing.T) {
	for nome, attesa := range map[string]string{
		"ricevuta-1-26.pdf":                 `attachment; filename="ricevuta-1-26.pdf"`,
		"ricevuta-Zz09.pdf":                 `attachment; filename="ricevuta-Zz09.pdf"`,
		"ricevuta-A 1è\"x%.pdf":             `attachment; filename="ricevuta-A%201%C3%A8%22x%25.pdf"; filename*=utf-8''ricevuta-A%201%C3%A8%22x%25.pdf`,
		"ricevuta-<a>;b.pdf":                `attachment; filename="ricevuta-%3Ca%3E%3Bb.pdf"; filename*=utf-8''ricevuta-%3Ca%3E%3Bb.pdf`,
		"ricevuta-~_.+*'()!$&=,:@[]#?.pdf":  `attachment; filename="ricevuta-~_.%2B%2A%27%28%29%21%24%26%3D%2C%3A%40%5B%5D%23%3F.pdf"; filename*=utf-8''ricevuta-~_.%2B%2A%27%28%29%21%24%26%3D%2C%3A%40%5B%5D%23%3F.pdf`,
		"ricevuta-\t\n.pdf":                 `attachment; filename="ricevuta-%09%0A.pdf"; filename*=utf-8''ricevuta-%09%0A.pdf`,
		buste.NomeFileRicevuta("12/26 bis"): `attachment; filename="ricevuta-12-26%20bis.pdf"; filename*=utf-8''ricevuta-12-26%20bis.pdf`,
	} {
		if got := disposizioneAllegato(nome); got != attesa {
			t.Errorf("%q:\n%s\natteso\n%s", nome, got, attesa)
		}
	}
}

// inventario: RIP-S-42
// Sulla scheda, dopo «Modifica» e «⧉ Anteprima PDF», il link «Ricevuta PDF» alla ricevuta della busta,
// come lo standalone (link semplice, stesso stile di contorno degli altri pulsanti dei PDF; l'ordine
// intero della barra è in TestPulsantiDeiPDFSullaScheda).
func TestPulsanteRicevutaSullaScheda(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	id := strconv.FormatInt(bustaScheda(t, d, o, nil), 10)
	r := titolareDentro(t, h).vai("GET", percorsoScheda(mustInt(t, id)), nil, nil)
	contiene(t, "scheda", r.corpo, `⧉ Anteprima PDF</a>`+"\n"+`<a href="/repairs/`+id+`/receipt.pdf" class="pulsante-contorno">Ricevuta PDF</a>`+"\n")
}

func mustInt(t *testing.T, s string) int64 {
	t.Helper()
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	return n
}
