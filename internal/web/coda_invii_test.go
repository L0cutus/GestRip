package web

import (
	"bytes"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// La coda delle foto in background (slice 044b, D-58): ogni foto della coda porta un identificativo
// casuale nell'intestazione x-upload-id, e il server tiene traccia degli invii riusciti (tabella
// invii_foto). Un nuovo invio con lo stesso identificativo per la stessa busta (la pagina ricaricata
// o cambiata a metà invio, una seconda scheda) risponde come il primo, con la foto già salvata, senza
// crearne un'altra né scrivere un'altra riga d'audit.

// conInvio è x-upload-id con l'identificativo dato, più il nome.
func conInvio(id, nome string) map[string]string {
	h := conNome(nome)
	h["X-Upload-Id"] = id
	return h
}

const (
	invioUno = "AAAAAAAAAAAAAAAAAAAAAA"
	invioDue = "bbbbbbbbbbbbbbbbbbbbb_"
)

// inventario: RIP-H-40, RIP-S-35
// Lo stesso identificativo due volte sulla stessa busta: 201 tutte e due con la stessa foto, una
// foto sola, una riga repair.photo_uploaded, la traccia legata alla foto; niente resti nei
// caricamenti. Un identificativo diverso è un'altra foto; lo stesso su un'altra busta anche.
func TestCodaFotoStessoInvioUnaFotoSola(t *testing.T) {
	p := facciaCaricamento(t, nil)
	altra := bustaScheda(t, p.d, p.o, nil)
	b := pronto(titolareDentro(t, p.h))
	prima := accolta(t, "primo invio", b.inviaFoto(percorsoCoda(p.busta), []byte(pngPixel), conInvio(invioUno, "a.png")))
	diNuovo := accolta(t, "secondo invio", b.inviaFoto(percorsoCoda(p.busta), []byte(pngPixel), conInvio(invioUno, "a.png")))
	if prima != diNuovo {
		t.Errorf("photo_id %d e poi %d: il secondo invio deve rispondere con la foto del primo", prima, diNuovo)
	}
	if f := p.foto(t); len(f) != 1 || p.caricate(t) != 1 {
		t.Fatalf("foto %d, righe d'audit %d: attese 1 e 1", len(f), p.caricate(t))
	}
	if n := contaRighe(t, p.d, `SELECT count(*) FROM invii_foto WHERE busta_id = `+strconv.FormatInt(p.busta, 10)+
		` AND identificativo = '`+invioUno+`' AND foto_id = `+strconv.FormatInt(prima, 10)); n != 1 {
		t.Errorf("tracce dell'invio: %d", n)
	}
	altraFoto := accolta(t, "altro identificativo", b.inviaFoto(percorsoCoda(p.busta), []byte(pngPixel), conInvio(invioDue, "b.png")))
	if altraFoto == prima || len(p.foto(t)) != 2 {
		t.Errorf("un altro identificativo è un'altra foto: %d, foto %d", altraFoto, len(p.foto(t)))
	}
	suAltra := accolta(t, "altra busta", b.inviaFoto(percorsoCoda(altra), []byte(pngPixel), conInvio(invioUno, "a.png")))
	if g, _ := p.b.ElencoFoto(t.Context(), altra); len(g) != 1 || g[0].ID != suAltra {
		t.Errorf("lo stesso identificativo su un'altra busta: %+v", g)
	}
	if !p.caricamentiVuoti(t) {
		t.Error("resti nei caricamenti")
	}
}

// inventario: RIP-H-40, RIP-S-35
// Due invii insieme con lo stesso identificativo (la pagina vecchia che finisce mentre quella nuova
// riparte): una foto sola, e tutte e due le risposte la nominano.
func TestCodaFotoInviiInsiemeUnaFotoSola(t *testing.T) {
	p := facciaCaricamento(t, nil)
	b := pronto(titolareDentro(t, p.h))
	const n = 4
	var wg sync.WaitGroup
	risposte := make([]risposta, n)
	for i := range n {
		wg.Go(func() {
			risposte[i] = b.inviaFoto(percorsoCoda(p.busta), []byte(pngPixel), conInvio(invioUno, "a.png"))
		})
	}
	wg.Wait()
	ids := map[int64]bool{}
	for i, r := range risposte {
		ids[accolta(t, "invio "+strconv.Itoa(i), r)] = true
	}
	if len(ids) != 1 || len(p.foto(t)) != 1 || p.caricate(t) != 1 {
		t.Errorf("id diversi %d, foto %d, righe d'audit %d: attesi 1, 1, 1", len(ids), len(p.foto(t)), p.caricate(t))
	}
}

// inventario: RIP-H-40, RIP-S-35
// La traccia c'è solo per un invio riuscito: una foto rifiutata (422) e poi lo stesso identificativo
// con una foto buona salva la foto buona. Tolta la foto dalla scheda la traccia resta, senza foto
// (ON DELETE SET NULL): un nuovo invio con quell'identificativo (la risposta del primo persa) risponde
// 201 con photo_id null, «già fatto», e la foto tolta non torna; nessuna riga d'audit in più.
func TestCodaFotoTracciaSoloDegliInviiRiusciti(t *testing.T) {
	p := facciaCaricamento(t, nil)
	b := pronto(titolareDentro(t, p.h))
	rispostaJSON(t, "rifiutata", b.inviaFoto(percorsoCoda(p.busta), []byte("not an image"), conInvio(invioUno, "a.png")),
		http.StatusUnprocessableEntity, rifiutoCoda("Foto rifiutata."))
	if n := contaRighe(t, p.d, `SELECT count(*) FROM invii_foto`); n != 0 {
		t.Fatalf("tracce dopo un rifiuto: %d", n)
	}
	prima := accolta(t, "buona", b.inviaFoto(percorsoCoda(p.busta), []byte(pngPixel), conInvio(invioUno, "a.png")))
	if err := p.b.RimuoviFoto(t.Context(), p.busta, prima, contestoProva); err != nil {
		t.Fatal(err)
	}
	if n := contaRighe(t, p.d, `SELECT count(*) FROM invii_foto WHERE foto_id IS NULL AND identificativo = '`+invioUno+`'`); n != 1 {
		t.Errorf("tracce senza foto dopo aver tolto la foto: %d", n)
	}
	audit := p.caricate(t)
	rispostaJSON(t, "dopo", b.inviaFoto(percorsoCoda(p.busta), []byte(pngPixel), conInvio(invioUno, "a.png")),
		http.StatusCreated, `{"ok":true,"photo_id":null}`)
	if len(p.foto(t)) != 0 || p.caricate(t) != audit {
		t.Errorf("la foto tolta è tornata: foto %d, righe d'audit %d (prima %d)", len(p.foto(t)), p.caricate(t), audit)
	}
	if !p.caricamentiVuoti(t) {
		t.Error("resti nei caricamenti")
	}
}

// inventario: RIP-S-35
// Conta solo la prima intestazione x-upload-id: con due valori la traccia è del primo; un primo
// malformato non conta anche se il secondo è buono.
func TestCodaFotoSoloLaPrimaIntestazione(t *testing.T) {
	p := facciaCaricamento(t, nil)
	b := pronto(titolareDentro(t, p.h))
	invia := func(ids ...string) risposta {
		req, _ := http.NewRequest("POST", b.srv.URL+percorsoCoda(p.busta), bytes.NewReader([]byte(pngPixel)))
		intestazioniCoda(req, b.token, conNome("a.png"))
		for _, id := range ids {
			req.Header.Add("X-Upload-Id", id)
		}
		r, err := b.c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = r.Body.Close() }()
		c, _ := io.ReadAll(r.Body) // un errore lascia il corpo a metà: i controlli lo vedono
		return risposta{r.StatusCode, r.Header.Get("Location"), string(c), r.Header}
	}
	prima := accolta(t, "due valori", invia(invioUno, invioDue))
	if di := accolta(t, "il primo", invia(invioUno)); di != prima {
		t.Errorf("il primo valore non ha la traccia: %d e %d", prima, di)
	}
	if di := accolta(t, "il secondo", invia(invioDue)); di == prima {
		t.Error("il secondo valore ha avuto la traccia")
	}
	accolta(t, "primo malformato", invia("corto", "cccccccccccccccccccccc"))
	if n := contaRighe(t, p.d, `SELECT count(*) FROM invii_foto WHERE identificativo = 'cccccccccccccccccccccc'`); n != 0 {
		t.Errorf("traccia del secondo valore dopo un primo malformato: %d", n)
	}
}

// inventario: RIP-S-35
// Se la traccia non si scrive, la transazione della foto non si chiude: niente foto né audit, 500
// «Foto non salvata.» (D-51), e la coda riprova.
func TestCodaFotoTracciaCheNonSiScrive(t *testing.T) {
	p := facciaCaricamento(t, nil)
	eseguiSQL(t, p.d, `CREATE TRIGGER prova_traccia BEFORE INSERT ON invii_foto BEGIN SELECT RAISE(ABORT, 'traccia inventata'); END`)
	b := pronto(titolareDentro(t, p.h))
	rispostaJSON(t, "traccia", b.inviaFoto(percorsoCoda(p.busta), []byte(pngPixel), conInvio(invioUno, "a.png")),
		http.StatusInternalServerError, rifiutoCoda("Foto non salvata."))
	if len(p.foto(t)) != 0 || p.caricate(t) != 0 {
		t.Errorf("foto %d, righe d'audit %d: attese 0", len(p.foto(t)), p.caricate(t))
	}
}

// inventario: RIP-S-35
// La chiave primaria (busta, identificativo) tiene anche senza il lucchetto dell'archivio: una seconda
// traccia uguale si rifiuta; un identificativo che non è di 22 caratteri base64url anche.
func TestCodaFotoChiaveDellaTraccia(t *testing.T) {
	p := facciaCaricamento(t, nil)
	b := pronto(titolareDentro(t, p.h))
	id := accolta(t, "foto", b.inviaFoto(percorsoCoda(p.busta), []byte(pngPixel), conInvio(invioUno, "a.png")))
	ins := `INSERT INTO invii_foto (busta_id, identificativo, foto_id, creato_il) VALUES (?, ?, ?, '2026-11-03T09:00:00.000000Z')`
	if _, err := p.d.Scrittura.Exec(ins, p.busta, invioUno, id); err == nil || !strings.Contains(err.Error(), "UNIQUE") {
		t.Errorf("traccia doppia: %v", err)
	}
	for _, x := range []string{"corto", invioUno + "A", "AAAAAAAAAAAAAAAAAAAAA+"} {
		if _, err := p.d.Scrittura.Exec(ins, p.busta, x, id); err == nil || !strings.Contains(err.Error(), "CHECK") {
			t.Errorf("identificativo %q: %v", x, err)
		}
	}
}

// inventario: RIP-S-35
// Un identificativo malformato (non 22 caratteri base64url) si ignora, come lo standalone ignora
// un'intestazione che non conosce: la foto si salva come senza intestazione, e ogni invio è una foto.
// Nel registro una riga, senza il valore.
func TestCodaFotoIdentificativoMalformatoIgnorato(t *testing.T) {
	p := facciaCaricamento(t, nil)
	b := pronto(titolareDentro(t, p.h))
	defer func() {
		// Una riga nel registro per ogni valore ignorato, mai il valore.
		reg := p.reg.String()
		if n := strings.Count(reg, "x-upload-id malformato, ignorato"); n != 12 {
			t.Errorf("righe nel registro %d, attese 12: %s", n, reg)
		}
		if strings.Contains(reg, "corto") || strings.Contains(reg, invioUno) {
			t.Errorf("il valore nel registro: %s", reg)
		}
	}()
	for _, id := range []string{"corto", invioUno + "A", "AAAAAAAAAAAAAAAAAAAAA+", "AAAAAAAAAAAAAAAAAAAAA=", " " + invioUno[1:], strings.Repeat("é", 11)} {
		for i := range 2 {
			accolta(t, id+" "+strconv.Itoa(i), b.inviaFoto(percorsoCoda(p.busta), []byte(pngPixel), conInvio(id, "a.png")))
		}
	}
	if n := len(p.foto(t)); n != 12 {
		t.Errorf("foto %d, attese 12", n)
	}
	if n := contaRighe(t, p.d, `SELECT count(*) FROM invii_foto`); n != 0 {
		t.Errorf("tracce di identificativi malformati: %d", n)
	}
}

// inventario: RIP-H-40
// Le pagine interne dicono chi è l'utente della sessione (l'id, non un segreto), perché la coda
// mandi solo le foto di chi le ha scelte; il login e le pagine pubbliche no.
func TestPagineConLUtenteDellaCoda(t *testing.T) {
	p := facciaCaricamento(t, nil)
	idDi := func(email string) string {
		var id int64
		if err := p.d.Lettura.QueryRow(`SELECT id FROM utenti WHERE email = ?`, email).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return strconv.FormatInt(id, 10)
	}
	tit := titolareDentro(t, p.h)
	dip := dipendenteDentro(t, p.h)
	for _, c := range []struct {
		b     *browser
		email string
	}{{tit, "titolare@esempio.it"}, {dip, "banco@esempio.it"}} {
		for _, pagina := range []string{"/", "/nuova", percorsoScheda(p.busta)} {
			r := c.b.vai("GET", pagina, nil, nil)
			contiene(t, c.email+" "+pagina, r.corpo, `<meta name="gestrip-utente" content="`+idDi(c.email)+`">`)
		}
	}
	fuori := nuovoBrowser(t, p.h)
	if r := fuori.vai("GET", "/login", nil, nil); strings.Contains(r.corpo, "gestrip-utente") {
		t.Error("il login dice l'utente")
	}
}
