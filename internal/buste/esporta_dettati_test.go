package buste

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/L0cutus/gestrip/internal/assist"
)

// L'esportazione dei dettati raccolti (slice 117; RIP-H-52, OPS-09), come TrainingExport.run/2 dello standalone a
// go-baseline (training_export_test.exs, slice 183 e 184). Nomi, telefoni (333 000 …), oreficerie e dettati inventati.

// propostaEsportazione è la proposta salvata di un dettato di nome e telefono dati.
func propostaEsportazione(nome, tel string, altro string) string {
	return `{"data":"31/10/2026","nome":"` + nome + `","telefono":"` + tel + `","descrizione":"Anello da stringere",` +
		`"note":"","preventivo":"","data_consegna":"","oreficeria":"","oreficeria_id":"","compro_oro":0` + altro + `}`
}

// bustaConDettato salva una busta del cliente dato (numero dato, "" per il prossimo) e il suo dettato raccolto.
func bustaConDettato(t *testing.T, b *Buste, qr, numero, nome, tel, testo, proposta string) Creata {
	t.Helper()
	c := crea(t, b, con(modulo(), func(a *Accettazione) {
		a.CodiceQR, a.Numero, a.NomeCliente, a.TelefonoCliente = qr, numero, nome, tel
	}))
	esegui(t, b.d, `INSERT INTO dettati_addestramento (busta_id, testo, proposta, modello, creato_il) VALUES (?, ?, ?, 'modello-di-prova', ?)`,
		c.ID, testo, proposta, istanteProva)
	return c
}

func fileEsportazione(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "dettati.jsonl")
}

func righeDelFile(t *testing.T, p string) []map[string]any {
	t.Helper()
	j, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var out []map[string]any
	for _, l := range strings.Split(strings.TrimSuffix(string(j), "\n"), "\n") {
		if l == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			t.Fatalf("riga non JSON: %v", err)
		}
		out = append(out, m)
	}
	return out
}

func righeExport(t *testing.T, b *Buste) []string {
	t.Helper()
	rows, err := b.d.Lettura.Query(`SELECT attore || '|' || risorsa || '|' || id_risorsa || '|' || esito || '|' || dati FROM eventi_audit WHERE azione = 'assist.training_exported' ORDER BY id`)
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

// due dettati: uno che si esporta e uno scartato (nome non trovato nel testo).
func dueDettati(t *testing.T, b *Buste) (Creata, Creata) {
	t.Helper()
	a := bustaConDettato(t, b, "QR-E1", "77/26", "Mario Rossi", "333 000 1234",
		"Cliente Mario Rossi 333 000 1234 anello da stringere", propostaEsportazione("Mario Rossi", "333 000 1234", ""))
	s := bustaConDettato(t, b, "QR-E2", "", "Giacomo Ferretti", "333 000 5678",
		"Busta 45 anello da stringere di una misura", propostaEsportazione("", "", ""))
	return a, s
}

// inventario: RIP-H-52
// inventario: OPS-09
// Le righe lette, scritte e scartate, si cancellano dopo il file; i conteggi sono quelli dello standalone e l'audit li
// ripete, solo numeri, con l'attore «export»; il numero di busta non è nel file.
func TestEsportaDettati(t *testing.T) {
	b, _, _ := prova(t)
	a, _ := dueDettati(t, b)
	p := fileEsportazione(t)
	c, err := b.EsportaDettati(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	if c.Totali != 2 || c.Esportati != 1 || c.Cancellati != 2 || len(c.Scartati) != 1 || c.Scartati["nome_non_trovato"] != 1 {
		t.Errorf("conteggi %+v", c)
	}
	if got := strings.Join(c.Resoconto(), "\n"); got != "totali=2\nesportati=1\nscartati=1\ncancellati=2\nscartati_nome_non_trovato=1" {
		t.Errorf("resoconto:\n%s", got)
	}
	if righeRaccolta(t, b) != 0 {
		t.Errorf("righe rimaste %d", righeRaccolta(t, b))
	}
	r := righeDelFile(t, p)
	if len(r) != 1 || r[0]["modello"] != "modello-di-prova" {
		t.Fatalf("file %v", r)
	}
	j, _ := os.ReadFile(p)
	if strings.Contains(string(j), a.Numero) || strings.Contains(string(j), "77/26") {
		t.Error("il numero di busta è nel file")
	}
	if got := righeExport(t, b); len(got) != 1 || got[0] != `export|assist|training|ok|{"cancellati":2,"esportati":1,"scartati":{"nome_non_trovato":1},"totali":2}` {
		t.Errorf("audit %v", got)
	}
	// Un dettato raccolto dopo resta.
	bustaConDettato(t, b, "QR-E3", "", "Lucia Bianchi", "333 000 4321", "Cliente Lucia Bianchi 333 000 4321 collana", propostaEsportazione("Lucia Bianchi", "333 000 4321", ""))
	if righeRaccolta(t, b) != 1 {
		t.Error("la riga nuova non c'è")
	}
}

// inventario: OPS-09
// Senza dettati il file nasce vuoto, l'audit c'è con gli zeri (come lo standalone).
func TestEsportaDettatiNessuno(t *testing.T) {
	b, _, _ := prova(t)
	p := fileEsportazione(t)
	c, err := b.EsportaDettati(ctx, p)
	if err != nil || c.Totali != 0 || c.Cancellati != 0 {
		t.Fatalf("%+v %v", c, err)
	}
	if j, err := os.ReadFile(p); err != nil || len(j) != 0 {
		t.Errorf("file %q %v", j, err)
	}
	if got := righeExport(t, b); len(got) != 1 || !strings.HasSuffix(got[0], `{"cancellati":0,"esportati":0,"scartati":{},"totali":0}`) {
		t.Errorf("audit %v", got)
	}
	if got := strings.Join(c.Resoconto(), "\n"); got != "totali=0\nesportati=0\nscartati=0\ncancellati=0" {
		t.Errorf("resoconto %s", got)
	}
}

// inventario: OPS-09
// Un file che esiste (anche vuoto, anche un collegamento simbolico a un file che non c'è) non si tocca: niente scritto,
// niente cancellato, niente audit.
func TestEsportaDettatiFileEsistente(t *testing.T) {
	b, _, _ := prova(t)
	dueDettati(t, b)
	p := fileEsportazione(t)
	if err := os.WriteFile(p, []byte("già qui\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := b.EsportaDettati(ctx, p); !errors.Is(err, ErrFileEsistente) {
		t.Fatalf("errore %v", err)
	}
	if j, _ := os.ReadFile(p); string(j) != "già qui\n" {
		t.Errorf("file cambiato: %q", j)
	}
	if fi, _ := os.Stat(p); fi.Mode().Perm() != 0o644 {
		t.Errorf("permessi cambiati %v", fi.Mode())
	}
	link := filepath.Join(t.TempDir(), "collegamento.jsonl")
	dest := filepath.Join(t.TempDir(), "destinazione.jsonl")
	if err := os.Symlink(dest, link); err != nil {
		t.Fatal(err)
	}
	if _, err := b.EsportaDettati(ctx, link); !errors.Is(err, ErrFileEsistente) {
		t.Fatalf("collegamento: %v", err)
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Error("il collegamento ha creato il file")
	}
	if righeRaccolta(t, b) != 2 || len(righeExport(t, b)) != 0 {
		t.Error("cancellato o scritto l'audit")
	}
}

// inventario: OPS-09
// Il file è del solo proprietario (0600) anche con una umask che lascerebbe leggere a tutti.
func TestEsportaDettatiPermessi(t *testing.T) {
	b, _, _ := prova(t)
	dueDettati(t, b)
	vecchia := syscall.Umask(0)
	defer syscall.Umask(vecchia)
	p := fileEsportazione(t)
	if _, err := b.EsportaDettati(ctx, p); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(p); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("permessi %v %v", fi.Mode(), err)
	}
}

// fileGuasto è il file dell'esportazione con un guasto: la scrittura dopo dopo byte, o la sync.
type fileGuasto struct {
	*os.File
	dopo      int
	scritti   int
	syncRotta bool
	primaSync func()
}

func (f *fileGuasto) Write(p []byte) (int, error) {
	if f.dopo >= 0 && f.scritti+len(p) > f.dopo {
		n, _ := f.File.Write(p[:f.dopo-f.scritti])
		f.scritti += n
		return n, syscall.ENOSPC
	}
	n, err := f.File.Write(p)
	f.scritti += n
	return n, err
}

func (f *fileGuasto) Sync() error {
	if f.primaSync != nil {
		f.primaSync()
	}
	if f.syncRotta {
		return syscall.EIO
	}
	return f.File.Sync()
}

// inventario: OPS-09
// Un errore di scrittura a metà, della sync del file o della sync della cartella: niente cancellato, nessun audit, il
// file a metà tolto; l'errore non porta niente del dettato.
func TestEsportaDettatiErrori(t *testing.T) {
	casi := []struct {
		nome    string
		file    func(*os.File) fileDettati
		cartell func(string) error
		atteso  error
	}{
		{nome: "scrittura a metà", file: func(f *os.File) fileDettati { return &fileGuasto{File: f, dopo: 30} }, atteso: syscall.ENOSPC},
		{nome: "sync del file", file: func(f *os.File) fileDettati { return &fileGuasto{File: f, dopo: -1, syncRotta: true} }, atteso: syscall.EIO},
		{nome: "sync della cartella", cartell: func(string) error { return syscall.EIO }, atteso: syscall.EIO},
	}
	for _, c := range casi {
		t.Run(c.nome, func(t *testing.T) {
			b, _, _ := prova(t)
			// Due righe: la prima è già più lunga di 30 byte, quindi la scrittura si rompe dentro la prima riga.
			dueDettati(t, b)
			b.fileEsportazione, b.syncCartellaEsportazione = c.file, c.cartell
			p := fileEsportazione(t)
			_, err := b.EsportaDettati(ctx, p)
			if !errors.Is(err, c.atteso) {
				t.Fatalf("errore %v", err)
			}
			for _, s := range []string{"Rossi", "Mario", "333", "anello"} {
				if strings.Contains(err.Error(), s) {
					t.Errorf("l'errore porta %q: %v", s, err)
				}
			}
			if _, err := os.Stat(p); !os.IsNotExist(err) {
				t.Errorf("il file è rimasto: %v", err)
			}
			if righeRaccolta(t, b) != 2 || len(righeExport(t, b)) != 0 {
				t.Error("cancellato o scritto l'audit")
			}
		})
	}
}

// inventario: OPS-09
// Un dettato raccolto mentre l'esportazione scrive (qui durante la sync) non è stato letto: resta.
func TestEsportaDettatiRigaDurante(t *testing.T) {
	b, _, _ := prova(t)
	bustaConDettato(t, b, "QR-D1", "", "Mario Rossi", "333 000 1234", "Cliente Mario Rossi 333 000 1234 anello", propostaEsportazione("Mario Rossi", "333 000 1234", ""))
	b.fileEsportazione = func(f *os.File) fileDettati {
		return &fileGuasto{File: f, dopo: -1, primaSync: func() {
			bustaConDettato(t, b, "QR-D2", "", "Lucia Bianchi", "333 000 4321", "Cliente Lucia Bianchi 333 000 4321 collana", propostaEsportazione("Lucia Bianchi", "333 000 4321", ""))
		}}
	}
	c, err := b.EsportaDettati(ctx, fileEsportazione(t))
	if err != nil || c.Totali != 1 || c.Cancellati != 1 {
		t.Fatalf("%+v %v", c, err)
	}
	if righeRaccolta(t, b) != 1 {
		t.Errorf("righe %d", righeRaccolta(t, b))
	}
}

// inventario: RIP-H-52
// La risposta è la busta com'è adesso: descrizione, preventivo e consegna cambiati dopo il dettato; la proposta resta
// quella del modello; il preventivo salvato solo in centesimi si legge come nel modulo.
func TestEsportaDettatiDatiAttuali(t *testing.T) {
	b, d, _ := prova(t)
	c := bustaConDettato(t, b, "QR-A1", "", "Mario Rossi", "333 000 1234", "Cliente Mario Rossi 333 000 1234 orologio da revisionare",
		propostaEsportazione("Mario Rossi", "333 000 1234", ""))
	esegui(t, d, `UPDATE buste SET descrizione = 'Orologio da revisionare, cambio pila', preventivo_testo = NULL, preventivo_centesimi = 3550,
		data_prevista = '2026-11-05', data_prevista_testo = NULL, note_pubbliche = 'da lucidare', oreficeria_id = 2 WHERE id = ?`, c.ID)
	p := fileEsportazione(t)
	if _, err := b.EsportaDettati(ctx, p); err != nil {
		t.Fatal(err)
	}
	r := righeDelFile(t, p)
	if len(r) != 1 {
		t.Fatalf("righe %d", len(r))
	}
	risposta, proposta := r[0]["risposta"].(map[string]any), r[0]["proposta"].(map[string]any)
	if risposta["descrizione"] != "Orologio da revisionare, cambio pila" || risposta["preventivo"] != "35.50" ||
		risposta["data_consegna"] != "05/11/2026" || risposta["note"] != "da lucidare" || risposta["oreficeria"] != "Oreficeria Esempio" ||
		risposta["data"] != "31/10/2026" {
		t.Errorf("risposta %v", risposta)
	}
	if proposta["descrizione"] != "Anello da stringere" {
		t.Errorf("proposta %v", proposta)
	}
	var campi []string
	for _, v := range r[0]["campi_corretti"].([]any) {
		campi = append(campi, v.(string))
	}
	if strings.Join(campi, ",") != "descrizione,note,preventivo,data_consegna,oreficeria" {
		t.Errorf("campi corretti %v", campi)
	}
	// La consegna scritta a parole vince sulla data (date_source/2).
	esegui(t, d, `INSERT INTO dettati_addestramento (busta_id, testo, proposta, modello, creato_il) VALUES (?, 'Cliente Mario Rossi 333 000 1234 orologio', ?, NULL, ?)`,
		c.ID, propostaEsportazione("Mario Rossi", "333 000 1234", ""), istanteProva)
	esegui(t, d, `UPDATE buste SET data_prevista_testo = 'fine mese', preventivo_testo = '40/50' WHERE id = ?`, c.ID)
	p = fileEsportazione(t)
	if _, err := b.EsportaDettati(ctx, p); err != nil {
		t.Fatal(err)
	}
	r = righeDelFile(t, p)
	if risposta := r[0]["risposta"].(map[string]any); risposta["data_consegna"] != "fine mese" || risposta["preventivo"] != "40/50" || r[0]["modello"] != nil {
		t.Errorf("riga %v", r[0])
	}
}

// inventario: RIP-H-52
// Una busta passata dopo la raccolta a un cliente che si è opposto: la riga si scarta (cliente_escluso) e si cancella;
// una busta nel cestino si scarta (busta_nel_cestino) e si cancella, come nello standalone.
func TestEsportaDettatiEsclusoECestino(t *testing.T) {
	b, d, _ := prova(t)
	c := bustaConDettato(t, b, "QR-X1", "", "Mario Rossi", "333 000 1234", "Cliente Mario Rossi 333 000 1234 anello", propostaEsportazione("Mario Rossi", "333 000 1234", ""))
	altra := crea(t, b, con(modulo(), func(a *Accettazione) {
		a.CodiceQR, a.NomeCliente, a.TelefonoCliente = "QR-X2", "Anna Verdi", "333 000 1112"
	}))
	esegui(t, d, `INSERT INTO dettati_esclusioni (cliente_id, creato_il) VALUES (?, ?)`, altra.ClienteID, istanteProva)
	esegui(t, d, `UPDATE buste SET cliente_id = ? WHERE id = ?`, altra.ClienteID, c.ID)
	cestino := bustaConDettato(t, b, "QR-X3", "", "Lucia Bianchi", "333 000 4321", "Cliente Lucia Bianchi 333 000 4321 collana", propostaEsportazione("Lucia Bianchi", "333 000 4321", ""))
	esegui(t, d, `UPDATE buste SET cancellata_il = ? WHERE id = ?`, istanteProva, cestino.ID)
	p := fileEsportazione(t)
	n, err := b.EsportaDettati(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	if n.Totali != 2 || n.Esportati != 0 || n.Cancellati != 2 || n.Scartati["cliente_escluso"] != 1 || n.Scartati["busta_nel_cestino"] != 1 {
		t.Errorf("conteggi %+v", n)
	}
	if j, _ := os.ReadFile(p); len(j) != 0 {
		t.Errorf("file %q", j)
	}
	if righeRaccolta(t, b) != 0 {
		t.Error("righe rimaste")
	}
}

// inventario: RIP-H-52
// I nomi degli altri: un cognome di un altro cliente dell'anagrafica o un nome proprio comune scarta la riga
// (altro_nome); le parole delle oreficerie no. Nessun nome o telefono vero nel file.
func TestEsportaDettatiAltriNomi(t *testing.T) {
	b, d, _ := prova(t)
	esegui(t, d, `INSERT INTO clienti (nome, telefono, creato_il, aggiornato_il) VALUES ('Teresa Zanchetta', '', ?, ?), ('Bottari Cliente', '', ?, ?)`,
		istanteProva, istanteProva, istanteProva, istanteProva)
	esegui(t, d, `INSERT INTO oreficerie (nome) VALUES ('Oreficeria Bottari')`)
	bustaConDettato(t, b, "QR-N1", "", "Mario Rossi", "333 000 1234", "Cliente Mario Rossi 333 000 1234 anello, lo ritira la signora zanchetta",
		propostaEsportazione("Mario Rossi", "333 000 1234", ""))
	bustaConDettato(t, b, "QR-N2", "", "Lucia Bianchi", "333 000 4321", "Cliente Lucia Bianchi 333 000 4321 collana per il figlio Giovanni",
		propostaEsportazione("Lucia Bianchi", "333 000 4321", ""))
	bustaConDettato(t, b, "QR-N3", "", "Gino Dalla Lana", "0423 000765", "Cliente Gino Dalla Lana 0423 000765 oreficeria Bottari busta 118 anello di Lana",
		propostaEsportazione("Gino Dalla Lana", "0423 000765", ""))
	p := fileEsportazione(t)
	n, err := b.EsportaDettati(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	if n.Totali != 3 || n.Esportati != 1 || n.Scartati["altro_nome"] != 2 {
		t.Errorf("conteggi %+v", n)
	}
	j, _ := os.ReadFile(p)
	s := strings.ToLower(string(j))
	for _, w := range []string{"zanchetta", "giovanni", "rossi", "mario", "lucia", "bianchi", "gino", "lana"} {
		if strings.Contains(s, w) {
			t.Errorf("nel file c'è %q", w)
		}
	}
	cifre := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, s)
	for _, tel := range []string{"3330001234", "3330004321", "0423000765"} {
		if strings.Contains(cifre, tel) {
			t.Errorf("nel file c'è il telefono %s", tel)
		}
	}
	if !strings.Contains(string(j), "oreficeria Bottari busta 118") {
		t.Errorf("l'oreficeria e la busta dettata non ci sono: %s", j)
	}
}

// inventario: OPS-09
// D-112 (a): un errore nella transazione finale (qui l'audit rifiutato) con le righe ancora tutte nel database (verificato con
// una lettura nuova): niente cancellato, nessun audit, e il file, pur finito, tolto.
func TestEsportaDettatiTransazioneRifiutata(t *testing.T) {
	b, d, _ := prova(t)
	dueDettati(t, b)
	esegui(t, d, `CREATE TRIGGER audit_rifiutato BEFORE INSERT ON eventi_audit WHEN NEW.azione = 'assist.training_exported'
		BEGIN SELECT RAISE(ABORT, 'rifiutato'); END`)
	p := fileEsportazione(t)
	if _, err := b.EsportaDettati(ctx, p); err == nil {
		t.Fatal("nessun errore")
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Errorf("il file è rimasto: %v", err)
	}
	if righeRaccolta(t, b) != 2 || len(righeExport(t, b)) != 0 {
		t.Error("cancellato o scritto l'audit")
	}
}

// inventario: OPS-09
// D-112 (b): la transazione finale dà errore ma il commit è riuscito (le righe non ci sono più): il file resta, perché è
// l'unica copia di quei dettati, e l'errore è dedicato, fatto di soli numeri.
func TestEsportaDettatiCommitConErrore(t *testing.T) {
	b, _, _ := prova(t)
	dueDettati(t, b)
	b.dopoCommitEsportazione = func() error { return errors.New("commit con errore") }
	p := fileEsportazione(t)
	_, err := b.EsportaDettati(ctx, p)
	var tenuto *ErrFileTenuto
	if !errors.As(err, &tenuto) || tenuto.Mancanti != 2 || tenuto.Lette != 2 || tenuto.VerificaFallita {
		t.Fatalf("errore %#v", err)
	}
	if err.Error() != "la cancellazione dei dettati ha dato errore, ma 2 righe su 2 non ci sono più: il file resta" {
		t.Errorf("messaggio %q", err.Error())
	}
	if len(righeDelFile(t, p)) != 1 {
		t.Error("il file non c'è più")
	}
	if righeRaccolta(t, b) != 0 {
		t.Errorf("righe %d", righeRaccolta(t, b))
	}
}

// inventario: OPS-09
// D-112: la transazione dà errore e la lettura di verifica fallisce (qui la tabella non c'è più): il file resta, con
// l'errore dedicato.
func TestEsportaDettatiVerificaFallita(t *testing.T) {
	b, d, _ := prova(t)
	dueDettati(t, b)
	b.dopoCommitEsportazione = func() error {
		esegui(t, d, `DROP TABLE dettati_addestramento`)
		return errors.New("commit con errore")
	}
	p := fileEsportazione(t)
	_, err := b.EsportaDettati(ctx, p)
	var tenuto *ErrFileTenuto
	if !errors.As(err, &tenuto) || !tenuto.VerificaFallita {
		t.Fatalf("errore %#v", err)
	}
	if err.Error() != "la cancellazione dei dettati ha dato errore e la verifica non è riuscita: il file resta" {
		t.Errorf("messaggio %q", err.Error())
	}
	if _, err := os.Stat(p); err != nil {
		t.Errorf("il file non c'è più: %v", err)
	}
}

// fileChiusuraRotta è il file dell'esportazione la cui chiusura dà errore (dopo aver chiuso il file vero).
type fileChiusuraRotta struct{ *os.File }

func (f fileChiusuraRotta) Close() error {
	_ = f.File.Close()
	return syscall.EIO
}

// inventario: OPS-09
// Un errore della chiusura del file: niente cancellato, nessun audit, file tolto.
func TestEsportaDettatiChiusuraRotta(t *testing.T) {
	b, _, _ := prova(t)
	dueDettati(t, b)
	b.fileEsportazione = func(f *os.File) fileDettati { return fileChiusuraRotta{f} }
	p := fileEsportazione(t)
	if _, err := b.EsportaDettati(ctx, p); !errors.Is(err, syscall.EIO) {
		t.Fatalf("errore %v", err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Errorf("il file è rimasto: %v", err)
	}
	if righeRaccolta(t, b) != 2 || len(righeExport(t, b)) != 0 {
		t.Error("cancellato o scritto l'audit")
	}
}

// fileConAnnullo annulla il contesto alla prima scrittura vera (il primo svuotamento del buffer) e conta le sync.
type fileConAnnullo struct {
	*os.File
	annulla func()
	sync    int
}

func (f *fileConAnnullo) Write(p []byte) (int, error) {
	f.annulla()
	return f.File.Write(p)
}

func (f *fileConAnnullo) Sync() error {
	f.sync++
	return f.File.Sync()
}

// tanteBuste salva n buste con il loro dettato raccolto, tutte esportabili.
func tanteBuste(t *testing.T, b *Buste, n int) {
	t.Helper()
	for i := range n {
		bustaConDettato(t, b, "QR-M"+itoa(int64(i)), "", "Mario Rossi", "333 000 1234",
			"Cliente Mario Rossi 333 000 1234 anello da stringere di una misura, lucidare e controllare le griffe della pietra "+itoa(int64(i)),
			propostaEsportazione("Mario Rossi", "333 000 1234", ""))
	}
}

// inventario: OPS-09
// Più di 500 righe: si cancellano tutte (a blocchi di 500); un contesto annullato a metà della scrittura (al primo
// svuotamento del buffer, con altre righe ancora da scrivere) ferma tutto prima della sync: niente cancellato, file tolto.
func TestEsportaDettatiMoltiEAnnullo(t *testing.T) {
	b, _, _ := prova(t)
	tanteBuste(t, b, 501)
	conAnnullo, annulla := context.WithCancel(ctx)
	defer annulla()
	var f *fileConAnnullo
	b.fileEsportazione = func(o *os.File) fileDettati {
		f = &fileConAnnullo{File: o, annulla: annulla}
		return f
	}
	p := fileEsportazione(t)
	if _, err := b.EsportaDettati(conAnnullo, p); !errors.Is(err, context.Canceled) {
		t.Fatalf("errore %v", err)
	}
	if f.sync != 0 {
		t.Error("sync dopo l'annullo")
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Errorf("il file è rimasto: %v", err)
	}
	if righeRaccolta(t, b) != 501 || len(righeExport(t, b)) != 0 {
		t.Error("cancellato o scritto l'audit")
	}
	b.fileEsportazione = nil
	p = fileEsportazione(t)
	c, err := b.EsportaDettati(ctx, p)
	if err != nil || c.Totali != 501 || c.Esportati != 501 || c.Cancellati != 501 {
		t.Fatalf("%+v %v", c, err)
	}
	if righeRaccolta(t, b) != 0 || len(righeDelFile(t, p)) != 501 {
		t.Errorf("righe rimaste %d", righeRaccolta(t, b))
	}
}

// inventario: OPS-09
// Il resoconto con più scarti dello stesso motivo: il totale e la riga del motivo li contano tutti.
func TestEsportaDettatiResoconto(t *testing.T) {
	c := ConteggiEsportazione{Totali: 4, Esportati: 1, Cancellati: 4, Scartati: map[string]int64{"nome_non_trovato": 1, "altro_nome": 2}}
	if got := strings.Join(c.Resoconto(), "\n"); got != "totali=4\nesportati=1\nscartati=3\ncancellati=4\nscartati_altro_nome=2\nscartati_nome_non_trovato=1" {
		t.Errorf("resoconto:\n%s", got)
	}
}

// inventario: RIP-H-52
// Una proposta che non è un oggetto JSON: la riga è «errore» e si cancella come le altre lette.
func TestEsportaDettatiPropostaNonOggetto(t *testing.T) {
	b, _, _ := prova(t)
	bustaConDettato(t, b, "QR-P1", "", "Mario Rossi", "333 000 1234", "Cliente Mario Rossi 333 000 1234 anello", `[1]`)
	c, err := b.EsportaDettati(ctx, fileEsportazione(t))
	if err != nil || c.Scartati[string(assist.ScartoErrore)] != 1 || c.Cancellati != 1 || righeRaccolta(t, b) != 0 {
		t.Fatalf("%+v %v", c, err)
	}
}
