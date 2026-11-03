package media

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
)

// I casi di media_cleanup_test.exs e repair_photos_test.exs dello standalone (go-baseline) che
// riguardano la coda delle cancellazioni (MediaStore.delete_pending/0, queue_deletions/1,
// referenced?/1) e i temporanei «.upload-» dell'archivio.

const istantePulizia = "2026-11-03T09:00:00.000000Z"

type banco struct {
	p         *Pulizia
	d         *db.DB
	a         *Archivio
	m         *Miniature
	media     string
	miniature string
	sync      atomic.Int64
}

func bancoPulizia(t *testing.T) *banco {
	t.Helper()
	dbp := filepath.Join(t.TempDir(), "gestrip.db")
	d, err := db.Apri(dbp)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Chiudi() })
	if _, err := d.Migra(context.Background(), clock.NuovoFinto(time.Date(2026, 11, 3, 9, 0, 0, 0, time.UTC))); err != nil {
		t.Fatal(err)
	}
	b := &banco{d: d, media: CartellaDi(dbp), miniature: MiniatureDi(dbp)}
	b.a = Nuovo(b.media)
	b.a.sincronizza = func(dir string) error {
		if dir != b.media {
			t.Errorf("sincronizzata %q, attesa la cartella media", dir)
		}
		b.sync.Add(1)
		return sincronizzaCartella(dir)
	}
	b.m = NuoveMiniature(b.a, b.miniature, nil)
	b.p = NuovaPulizia(d, b.a, b.m)
	if err := os.MkdirAll(b.media, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(b.miniature, 0o700); err != nil {
		t.Fatal(err)
	}
	return b
}

// oggetto salva contenuto nell'archivio, con la sua miniatura in cache, e ne dà la chiave.
func (b *banco) oggetto(t *testing.T, contenuto string) string {
	t.Helper()
	k, err := b.a.Salva([]byte(contenuto))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(b.miniature, k+".webp"), webpFinto(), 0o600); err != nil {
		t.Fatal(err)
	}
	return k
}

func webpFinto() []byte {
	return []byte("RIFF\x04\x00\x00\x00WEBP")
}

func (b *banco) accoda(t *testing.T, chiavi ...string) {
	t.Helper()
	tx, err := b.d.Scrittura.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := Accoda(context.Background(), tx, chiavi, time.Date(2026, 11, 3, 9, 0, 0, 0, time.UTC)); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func (b *banco) coda(t *testing.T) []string {
	t.Helper()
	r, err := b.d.Lettura.Query(`SELECT chiave FROM cancellazioni_media ORDER BY chiave`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()
	v := []string{}
	for r.Next() {
		var k string
		if err := r.Scan(&k); err != nil {
			t.Fatal(err)
		}
		v = append(v, k)
	}
	if err := r.Err(); err != nil {
		t.Fatal(err)
	}
	return v
}

func (b *banco) esegui(t *testing.T, q string, args ...any) {
	t.Helper()
	if _, err := b.d.Scrittura.Exec(q, args...); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
}

func presente(t *testing.T, p string) bool {
	t.Helper()
	_, err := os.Lstat(p)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	return err == nil
}

// basi sono le righe a cui si appoggiano le righe che nominano un oggetto: un cliente, una busta,
// una vendita e la sua revisione, tutti inventati.
func (b *banco) basi(t *testing.T) {
	t.Helper()
	sha := bytes.Repeat([]byte{0xab}, 32)
	b.esegui(t, `INSERT INTO clienti (id, nome, telefono, creato_il, aggiornato_il) VALUES (1, 'Cliente Prova', '3330000000', ?1, ?1)`, istantePulizia)
	b.esegui(t, `INSERT INTO buste (id, cliente_id, nome_cliente, telefono_cliente, numero, data_accettazione, descrizione,
		hash_token_pubblico, token_pubblico_scade_il, creato_il, aggiornato_il)
		VALUES (1, 1, 'Cliente Prova', '3330000000', '1/26', '2026-11-03', 'anello', ?1, ?2, ?2, ?2)`, sha, istantePulizia)
	b.esegui(t, `INSERT INTO vendite (id, data_vendita, canale, descrizione, acquirente_nome, creato_il, aggiornato_il)
		VALUES (1, '2026-11-03', 'Sito', 'orologio', 'Acquirente Prova', ?1, ?1)`, istantePulizia)
	b.esegui(t, `INSERT INTO revisioni_vendite (id, vendita_id, numero_revisione, anno_documento, numero_documento, data_vendita, canale,
		descrizione, mesi_garanzia, garanzia_dal, garanzia_al, acquirente_nome, venditore_nome, condizioni_garanzia,
		hash_token_pubblico, token_pubblico_scade_il, emessa_il, creato_il)
		VALUES (1, 1, 1, 2026, 1, '2026-11-03', 'Sito', 'orologio', 24, '2026-11-03', '2028-11-03', 'Acquirente Prova',
		'Negozio Prova', '24 mesi', ?1, ?2, ?2, ?2)`, sha, istantePulizia)
}

// usa scrive una riga che nomina k nella colonna r.
func (b *banco) usa(t *testing.T, r Riferimento, k string) {
	t.Helper()
	sha, err := hex.DecodeString(k)
	if err != nil {
		t.Fatal(err)
	}
	switch r {
	case Riferimento{"foto_buste", "chiave"}:
		b.esegui(t, `INSERT INTO foto_buste (busta_id, posizione, chiave, nome_file, byte, sha256, visibile_cliente, visibile_artigiano,
			visibile_oreficeria, creato_il, aggiornato_il) VALUES (1, 1, ?1, 'a.jpg', 10, ?2, 1, 1, 1, ?3, ?3)`, k, sha, istantePulizia)
	case Riferimento{"buste", "firma_chiave"}:
		b.esegui(t, `UPDATE buste SET firma_chiave = ?1, firma_byte = 10, firma_il = ?2 WHERE id = 1`, k, istantePulizia)
	case Riferimento{"eventi_firma_consegna", "chiave"}:
		b.esegui(t, `INSERT INTO eventi_firma_consegna (busta_id, evento, chiave, avvenuto_il, creato_il, aggiornato_il)
			VALUES (1, 'reset', ?1, ?2, ?2, ?2)`, k, istantePulizia)
	case Riferimento{"foto_vendite", "chiave"}:
		b.esegui(t, `INSERT INTO foto_vendite (vendita_id, posizione, chiave, nome_file, tipo_mime, byte, sha256, creato_il)
			VALUES (1, 1, ?1, 'a.jpg', 'image/jpeg', 10, ?2, ?3)`, k, sha, istantePulizia)
	case Riferimento{"foto_revisioni_vendite", "chiave"}:
		b.esegui(t, `INSERT INTO foto_revisioni_vendite (revisione_vendita_id, posizione, chiave, nome_file, tipo_mime, byte, sha256, creato_il)
			VALUES (1, 1, ?1, 'a.jpg', 'image/jpeg', 10, ?2, ?3)`, k, sha, istantePulizia)
	case Riferimento{"documenti_storici", "chiave"}:
		b.esegui(t, `INSERT INTO documenti_storici (nome_file_flask, chiave, byte, sha256, creato_il) VALUES ('1_26.pdf', ?1, 10, ?2, ?3)`,
			k, sha, istantePulizia)
	case Riferimento{"immagini_negozio", "chiave"}:
		b.esegui(t, `INSERT INTO immagini_negozio (tipo, chiave, nome_file, tipo_mime, byte, sha256, importata_il)
			VALUES ('logo', ?1, 'logo.png', 'image/png', 10, ?2, ?3)`, k, sha, istantePulizia)
	default:
		t.Fatalf("manca in questo test la riga che usa %s.%s", r.Tabella, r.Colonna)
	}
}

// inventario: RIP-S-57
// Una chiave in coda che nessuna riga usa: via il file, la sua miniatura e la riga della coda;
// la cartella si sincronizza dopo la rimozione. Le altre foto e le loro miniature restano.
func TestPuliziaCancellaLaChiaveNonUsata(t *testing.T) {
	b := bancoPulizia(t)
	via, resta := b.oggetto(t, "foto da togliere"), b.oggetto(t, "foto che resta")
	b.accoda(t, via)
	prima := b.sync.Load()
	if err := b.p.CancellaInCoda(context.Background()); err != nil {
		t.Fatal(err)
	}
	if presente(t, filepath.Join(b.media, via)) || presente(t, filepath.Join(b.miniature, via+".webp")) {
		t.Error("il file o la miniatura della chiave in coda sono rimasti")
	}
	if !presente(t, filepath.Join(b.media, resta)) || !presente(t, filepath.Join(b.miniature, resta+".webp")) {
		t.Error("tolta una foto che non era in coda")
	}
	if c := b.coda(t); len(c) != 0 {
		t.Errorf("coda %v, attesa vuota", c)
	}
	if n := b.sync.Load() - prima; n != 1 {
		t.Errorf("cartella sincronizzata %d volte, attesa 1", n)
	}
}

// inventario: RIP-S-57
// Una chiave in coda che una riga usa ancora (in ognuna delle colonne di Riferimenti) non si
// cancella: il file e la miniatura restano, e la riga della coda se ne va (referenced?/1 dentro
// delete_one_pending/1). Senza riga che la usa, la stessa chiave si cancella.
func TestPuliziaNonCancellaUnaChiaveUsata(t *testing.T) {
	for _, r := range Riferimenti {
		t.Run(r.Tabella+"."+r.Colonna, func(t *testing.T) {
			b := bancoPulizia(t)
			b.basi(t)
			k := b.oggetto(t, "oggetto usato da "+r.Tabella)
			b.usa(t, r, k)
			b.accoda(t, k)
			if err := b.p.CancellaInCoda(context.Background()); err != nil {
				t.Fatal(err)
			}
			if !presente(t, filepath.Join(b.media, k)) || !presente(t, filepath.Join(b.miniature, k+".webp")) {
				t.Errorf("cancellato un oggetto usato da %s.%s", r.Tabella, r.Colonna)
			}
			if c := b.coda(t); len(c) != 0 {
				t.Errorf("coda %v, attesa vuota", c)
			}
		})
	}
	if len(Riferimenti) != 7 {
		t.Errorf("%d colonne protette, attese 7 (quelle di referenced?/1)", len(Riferimenti))
	}
}

// inventario: RIP-S-57
// Una firma azzerata (buste.firma_chiave NULL dopo «reset») resta protetta dal suo evento.
func TestPuliziaFirmaAzzerataProtettaDalSuoEvento(t *testing.T) {
	b := bancoPulizia(t)
	b.basi(t)
	k := b.oggetto(t, "firma inventata")
	b.usa(t, Riferimento{"eventi_firma_consegna", "chiave"}, k)
	b.esegui(t, `UPDATE buste SET firma_chiave = NULL WHERE id = 1`)
	b.accoda(t, k)
	if err := b.p.CancellaInCoda(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !presente(t, filepath.Join(b.media, k)) {
		t.Error("cancellata la firma azzerata che ha ancora il suo evento")
	}
}

// inventario: RIP-S-57
// «a missing object keeps its durable job until the directory can be synced»: senza la cartella
// media la rimozione (che non trova il file) non si può rendere durevole, quindi la riga resta; con
// la cartella di nuovo lì la riga se ne va.
func TestPuliziaFileMancanteTieneIlLavoroFinoAllaSincronizzazione(t *testing.T) {
	b := bancoPulizia(t)
	k := strings.Repeat("a", 64)
	b.accoda(t, k)
	if err := os.RemoveAll(b.media); err != nil {
		t.Fatal(err)
	}
	if err := b.p.CancellaInCoda(context.Background()); !errors.Is(err, ErrSalvataggio) {
		t.Fatalf("errore %v, atteso ErrSalvataggio", err)
	}
	if c := b.coda(t); !slices.Equal(c, []string{k}) {
		t.Fatalf("coda %v, attesa la chiave ancora lì", c)
	}
	if err := os.Mkdir(b.media, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := b.p.CancellaInCoda(context.Background()); err != nil {
		t.Fatal(err)
	}
	if c := b.coda(t); len(c) != 0 {
		t.Errorf("coda %v, attesa vuota", c)
	}
}

// inventario: RIP-S-57
// «startup cleanup removes abandoned upload aliases durably»: i temporanei «.upload-…» rimasti da
// un salvataggio interrotto si tolgono (anche con la coda vuota) e la cartella si sincronizza una
// volta; il resto della cartella non si tocca. Senza temporanei niente sincronizzazione.
func TestPuliziaTemporaneiAbbandonati(t *testing.T) {
	b := bancoPulizia(t)
	k := b.oggetto(t, "oggetto che resta")
	for _, n := range []string{".upload-abbandonato", ".upload-"} {
		if err := os.WriteFile(filepath.Join(b.media, n), []byte("abbandonato"), 0o400); err != nil {
			t.Fatal(err)
		}
	}
	altro := filepath.Join(b.media, "x.upload-non-temporaneo")
	if err := os.WriteFile(altro, []byte("altro"), 0o400); err != nil {
		t.Fatal(err)
	}
	prima := b.sync.Load()
	if err := b.p.CancellaInCoda(context.Background()); err != nil {
		t.Fatal(err)
	}
	if v := elenco(t, b.media); !slices.Equal(v, []string{k, "x.upload-non-temporaneo"}) {
		t.Errorf("nella cartella %v", v)
	}
	if n := b.sync.Load() - prima; n != 1 {
		t.Errorf("cartella sincronizzata %d volte, attesa 1", n)
	}
	prima = b.sync.Load()
	if err := b.p.CancellaInCoda(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n := b.sync.Load() - prima; n != 0 {
		t.Errorf("senza temporanei cartella sincronizzata %d volte", n)
	}
}

// inventario: RIP-S-57
// Un temporaneo che non si toglie (qui una cartella non vuota con quel nome) ferma il giro prima
// della coda: ErrSalvataggio, la coda intatta. Una cartella media che manca non ha temporanei.
func TestPuliziaTemporaneoCheNonSiToglieFermaIlGiro(t *testing.T) {
	b := bancoPulizia(t)
	k := b.oggetto(t, "in coda")
	b.accoda(t, k)
	if err := os.MkdirAll(filepath.Join(b.media, ".upload-cartella", "dentro"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := b.p.CancellaInCoda(context.Background()); !errors.Is(err, ErrSalvataggio) {
		t.Fatalf("errore %v, atteso ErrSalvataggio", err)
	}
	if !presente(t, filepath.Join(b.media, k)) || !slices.Equal(b.coda(t), []string{k}) {
		t.Error("la coda è andata avanti dopo un temporaneo che non si toglie")
	}
}

// inventario: RIP-S-57
// Un crash fra la rimozione del file e quella della riga (qui un errore iniettato) non annulla la
// cancellazione già fatta: il file non torna, la riga resta e il giro dopo la toglie (il file che
// manca va bene).
func TestPuliziaCrashFraFileERiga(t *testing.T) {
	b := bancoPulizia(t)
	k := b.oggetto(t, "foto del crash")
	b.accoda(t, k)
	guasto := errors.New("crash finto")
	b.p.traFileERiga = func() error { return guasto }
	if err := b.p.CancellaInCoda(context.Background()); !errors.Is(err, guasto) {
		t.Fatalf("errore %v, atteso il crash finto", err)
	}
	if presente(t, filepath.Join(b.media, k)) || presente(t, filepath.Join(b.miniature, k+".webp")) {
		t.Error("il file è tornato o la miniatura è rimasta")
	}
	if c := b.coda(t); !slices.Equal(c, []string{k}) {
		t.Fatalf("coda %v, attesa la chiave ancora lì", c)
	}
	b.p.traFileERiga = nil
	if err := b.p.CancellaInCoda(context.Background()); err != nil {
		t.Fatal(err)
	}
	if c := b.coda(t); len(c) != 0 {
		t.Errorf("coda %v, attesa vuota", c)
	}
}

// inventario: RIP-S-57
// Le chiavi si fanno in ordine e il giro si ferma al primo errore (reduce_while): un file che non
// si toglie (qui una cartella non vuota con il nome della chiave) lascia la sua riga e quelle dopo;
// le chiavi prima sono già cancellate. Una chiave in coda malformata è ErrChiave e resta.
func TestPuliziaSiFermaAlPrimoErrore(t *testing.T) {
	b := bancoPulizia(t)
	prima := b.oggetto(t, "foto numero uno")
	dopo := b.oggetto(t, "foto numero due")
	if prima > dopo {
		prima, dopo = dopo, prima
	}
	// una chiave che sta fra le due, con al posto del file una cartella non vuota
	mezzo := prima[:63] + "f"
	if mezzo <= prima || mezzo >= dopo {
		mezzo = dopo[:62] + "00"
		if mezzo <= prima || mezzo >= dopo {
			t.Skip("chiavi troppo vicine per questo test")
		}
	}
	if err := os.MkdirAll(filepath.Join(b.media, mezzo, "dentro"), 0o700); err != nil {
		t.Fatal(err)
	}
	b.accoda(t, prima, mezzo, dopo)
	if err := b.p.CancellaInCoda(context.Background()); !errors.Is(err, ErrSalvataggio) {
		t.Fatalf("errore %v, atteso ErrSalvataggio", err)
	}
	if presente(t, filepath.Join(b.media, prima)) || !presente(t, filepath.Join(b.media, dopo)) {
		t.Error("ordine o arresto sbagliati")
	}
	if c := b.coda(t); !slices.Equal(c, []string{mezzo, dopo}) {
		t.Errorf("coda %v, attese le due chiavi dall'errore in poi", c)
	}
	b.esegui(t, `INSERT INTO cancellazioni_media (chiave, creato_il) VALUES ('0-non-una-chiave', ?1)`, istantePulizia)
	if err := b.p.CancellaInCoda(context.Background()); !errors.Is(err, ErrChiave) {
		t.Fatalf("errore %v, atteso ErrChiave", err)
	}
	if c := b.coda(t); len(c) != 3 {
		t.Errorf("coda %v", c)
	}
}

// inventario: RIP-S-57
// La pulizia prende il lucchetto dell'archivio (with_lock/1): con il lucchetto preso da un
// caricamento non tocca niente finché non viene lasciato.
func TestPuliziaAspettaIlLucchetto(t *testing.T) {
	b := bancoPulizia(t)
	k := b.oggetto(t, "foto sotto lucchetto")
	b.accoda(t, k)
	preso, lascia := make(chan struct{}), make(chan struct{})
	go func() {
		_ = b.a.ConLucchetto(func() error {
			close(preso)
			<-lascia
			return nil
		})
	}()
	<-preso
	finito := make(chan error, 1)
	go func() { finito <- b.p.CancellaInCoda(context.Background()) }()
	select {
	case err := <-finito:
		t.Fatalf("pulizia senza lucchetto: %v", err)
	case <-time.After(200 * time.Millisecond):
	}
	if !presente(t, filepath.Join(b.media, k)) {
		t.Fatal("file tolto con il lucchetto preso da altri")
	}
	close(lascia)
	if err := <-finito; err != nil {
		t.Fatal(err)
	}
	if presente(t, filepath.Join(b.media, k)) {
		t.Error("file rimasto dopo il lucchetto")
	}
}

// inventario: RIP-S-57
// Accoda è queue_deletions/1: dentro la transazione di chi chiama (un rollback non lascia niente),
// le chiavi ripetute contano una volta, una chiave già in coda non è un errore e tiene il suo
// istante; una chiave malformata è ErrChiave e non accoda niente.
func TestAccoda(t *testing.T) {
	b := bancoPulizia(t)
	k1, k2 := strings.Repeat("1", 64), strings.Repeat("2", 64)
	tx, err := b.d.Scrittura.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := Accoda(context.Background(), tx, []string{k1}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if c := b.coda(t); len(c) != 0 {
		t.Fatalf("coda %v dopo il rollback", c)
	}
	b.accoda(t, k2, k1, k2)
	tx, err = b.d.Scrittura.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := Accoda(context.Background(), tx, []string{k1}, time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := b.d.Lettura.QueryRow(`SELECT count(*) FROM cancellazioni_media WHERE creato_il = ?`, istantePulizia).Scan(&n); err != nil || n != 2 {
		t.Errorf("righe con l'istante della prima volta %d (%v), attese 2", n, err)
	}
	tx, err = b.d.Scrittura.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := Accoda(context.Background(), tx, []string{strings.Repeat("3", 64), "../fuori"}, time.Now()); !errors.Is(err, ErrChiave) {
		t.Errorf("chiave malformata: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if c := b.coda(t); !slices.Equal(c, []string{k1, k2}) {
		t.Errorf("coda %v", c)
	}
}

// registroFinto raccoglie le righe del registro.
type registroFinto struct{ righe bytes.Buffer }

func (r *registroFinto) logger() *slog.Logger {
	return slog.New(slog.NewTextHandler(&r.righe, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

// inventario: RIP-S-57
// Il lavoro di serve: un giro subito (anche i temporanei dell'avvio), uno dopo ogni attesa, fino a
// quando l'attesa dice di smettere. Un errore non ferma il lavoro: va nel registro con la causa e
// mai la chiave o un percorso, e la coda resta da riprovare; il giro dopo la finisce.
func TestPuliziaLavoro(t *testing.T) {
	b := bancoPulizia(t)
	k := b.oggetto(t, "foto del lavoro")
	b.accoda(t, k)
	tmp := filepath.Join(b.media, ".upload-avvio")
	if err := os.WriteFile(tmp, []byte("x"), 0o400); err != nil {
		t.Fatal(err)
	}
	b.p.traFileERiga = func() error { return ErrSalvataggio }
	var reg registroFinto
	giri := 0
	b.p.Lavoro(context.Background(), func(ctx context.Context) bool {
		giri++
		switch giri {
		case 1:
			if presente(t, tmp) {
				t.Error("temporaneo rimasto dopo il primo giro")
			}
			if c := b.coda(t); !slices.Equal(c, []string{k}) {
				t.Errorf("coda %v dopo il giro guasto", c)
			}
			b.p.traFileERiga = nil
			return true
		default:
			return false
		}
	}, reg.logger())
	if giri != 2 {
		t.Errorf("%d attese, attese 2", giri)
	}
	if c := b.coda(t); len(c) != 0 {
		t.Errorf("coda %v, attesa vuota dopo il secondo giro", c)
	}
	s := reg.righe.String()
	if !strings.Contains(s, "level=WARN") || !strings.Contains(s, "causa=archivio") {
		t.Errorf("registro senza l'avviso del giro guasto: %q", s)
	}
	if strings.Contains(s, k) || strings.Contains(s, k[:16]) || strings.Contains(s, b.media) {
		t.Errorf("chiave o percorso nel registro: %q", s)
	}
	if strings.Count(s, "\n") != 1 {
		t.Errorf("righe del registro: %q, attesa una sola", s)
	}
}

// inventario: RIP-S-57
// Un panico nel giro non ferma il lavoro: nel registro il tipo, mai il valore; il giro dopo
// riprova. Le cause: chiave malformata e database, oltre all'archivio.
func TestPuliziaLavoroPanicoECause(t *testing.T) {
	b := bancoPulizia(t)
	k := b.oggetto(t, "foto del panico")
	b.accoda(t, k)
	b.p.traFileERiga = func() error { panic("valore segreto " + k) }
	var reg registroFinto
	giri := 0
	b.p.Lavoro(context.Background(), func(context.Context) bool {
		giri++
		switch giri {
		case 1:
			b.p.traFileERiga = nil
			b.esegui(t, `INSERT INTO cancellazioni_media (chiave, creato_il) VALUES ('0-non-una-chiave', ?1)`, istantePulizia)
			return true
		case 2:
			b.esegui(t, `DELETE FROM cancellazioni_media`)
			b.esegui(t, `DROP TABLE cancellazioni_media`)
			return true
		default:
			return false
		}
	}, reg.logger())
	s := reg.righe.String()
	for _, atteso := range []string{"tipo=string", "causa=chiave", "causa=database"} {
		if !strings.Contains(s, atteso) {
			t.Errorf("manca %q nel registro: %q", atteso, s)
		}
	}
	if strings.Contains(s, "segreto") || strings.Contains(s, k) {
		t.Errorf("valore del panico nel registro: %q", s)
	}
}

// inventario: RIP-S-57
// Il lavoro si ferma con il contesto: un giro interrotto dallo stop non è un avviso.
func TestPuliziaLavoroFermatoDalContesto(t *testing.T) {
	b := bancoPulizia(t)
	b.accoda(t, strings.Repeat("c", 64))
	ctx, ferma := context.WithCancel(context.Background())
	ferma()
	var reg registroFinto
	giri := 0
	b.p.Lavoro(ctx, func(ctx context.Context) bool {
		giri++
		return ctx.Err() == nil
	}, reg.logger())
	if giri != 1 {
		t.Errorf("%d attese", giri)
	}
	if s := reg.righe.String(); s != "" {
		t.Errorf("registro dopo lo stop: %q", s)
	}
}

// inventario: RIP-S-57
// Senza la cartella media e con la coda vuota non c'è niente da fare: nessun errore (File.ls con
// :enoent nello standalone).
func TestPuliziaSenzaCartellaECodaVuota(t *testing.T) {
	b := bancoPulizia(t)
	if err := os.RemoveAll(b.media); err != nil {
		t.Fatal(err)
	}
	if err := b.p.CancellaInCoda(context.Background()); err != nil {
		t.Errorf("errore %v senza cartella e senza coda", err)
	}
}

// inventario: RIP-S-57
// Se non si riesce a sapere se una riga usa la chiave (qui una tabella di Riferimenti che manca),
// il file non si tocca, la riga della coda resta e il giro è un errore.
func TestPuliziaControlloGuastoNonCancella(t *testing.T) {
	b := bancoPulizia(t)
	k := b.oggetto(t, "foto con il controllo guasto")
	b.accoda(t, k)
	b.esegui(t, `DROP TABLE immagini_negozio`)
	if err := b.p.CancellaInCoda(context.Background()); err == nil {
		t.Error("controllo guasto senza errore")
	}
	if !presente(t, filepath.Join(b.media, k)) || !slices.Equal(b.coda(t), []string{k}) {
		t.Error("cancellato senza sapere se la chiave è usata")
	}
}

func (b *banco) importato(t *testing.T) {
	t.Helper()
	b.esegui(t, `INSERT INTO import_standalone (id, modalita, sha256_sorgente, byte_sorgente, importato_il)
		VALUES (1, 'sintetico', zeroblob(32), 4096, ?1)`, istantePulizia)
}

// inventario: RIP-S-57
// Dopo un import dallo standalone, finché non c'è un backup verificato, la coda non si tocca
// (domanda aperta 20, via prudente come la conservazione dell'audit, ADM-17): i file e le righe
// restano; i temporanei «.upload-» si tolgono comunque. Senza il blocco (oggi: senza la riga
// dell'import, finché il registro dei backup verificati non c'è) la coda si svuota.
func TestPuliziaCodaAspettaIlBackupDopoUnImport(t *testing.T) {
	b := bancoPulizia(t)
	b.importato(t)
	k := b.oggetto(t, "foto in coda dopo l'import")
	b.accoda(t, k)
	tmp := filepath.Join(b.media, ".upload-dopo-import")
	if err := os.WriteFile(tmp, []byte("x"), 0o400); err != nil {
		t.Fatal(err)
	}
	if err := b.p.CancellaInCoda(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !presente(t, filepath.Join(b.media, k)) || !presente(t, filepath.Join(b.miniature, k+".webp")) || !slices.Equal(b.coda(t), []string{k}) {
		t.Error("coda toccata dopo un import senza backup verificato")
	}
	if presente(t, tmp) {
		t.Error("temporaneo rimasto dopo un import")
	}
	b.esegui(t, `DELETE FROM import_standalone`)
	if err := os.WriteFile(tmp, []byte("x"), 0o400); err != nil {
		t.Fatal(err)
	}
	if err := b.p.CancellaInCoda(context.Background()); err != nil {
		t.Fatal(err)
	}
	if presente(t, filepath.Join(b.media, k)) || len(b.coda(t)) != 0 || presente(t, tmp) {
		t.Error("senza il blocco la coda non si è svuotata")
	}
}

// inventario: RIP-S-57
// Con la coda in attesa del backup verificato il registro lo dice una volta per avvio (non a ogni
// giro), senza chiavi né percorsi, e non come errore.
func TestPuliziaLavoroAvvisoUnaVoltaDopoUnImport(t *testing.T) {
	b := bancoPulizia(t)
	b.importato(t)
	k := b.oggetto(t, "foto in coda nel lavoro dopo l'import")
	b.accoda(t, k)
	var reg registroFinto
	giri := 0
	b.p.Lavoro(context.Background(), func(context.Context) bool {
		giri++
		return giri < 3
	}, reg.logger())
	s := reg.righe.String()
	if strings.Count(s, "\n") != 1 || !strings.Contains(s, "backup verificato") || strings.Contains(s, "level=ERROR") {
		t.Errorf("registro %q, attesa una sola riga sul backup verificato", s)
	}
	if strings.Contains(s, k[:16]) || strings.Contains(s, b.media) {
		t.Errorf("chiave o percorso nel registro: %q", s)
	}
	if !slices.Equal(b.coda(t), []string{k}) {
		t.Error("coda toccata")
	}
}

// inventario: RIP-S-57
// Se il segno dell'import non si legge, il giro è un errore e la coda non si tocca.
func TestPuliziaSegnoDellImportGuasto(t *testing.T) {
	b := bancoPulizia(t)
	k := b.oggetto(t, "foto con il segno dell'import guasto")
	b.accoda(t, k)
	b.esegui(t, `DROP TABLE import_standalone`)
	if err := b.p.CancellaInCoda(context.Background()); err == nil {
		t.Error("segno dell'import illeggibile senza errore")
	}
	if !presente(t, filepath.Join(b.media, k)) || !slices.Equal(b.coda(t), []string{k}) {
		t.Error("coda toccata senza sapere se il database viene da un import")
	}
}
