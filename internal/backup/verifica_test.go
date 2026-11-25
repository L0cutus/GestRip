package backup

import (
	"archive/tar"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"filippo.io/age"

	"github.com/L0cutus/gestrip/internal/db"
	"github.com/L0cutus/gestrip/internal/media"
)

// La verifica completa (slice 121). Tutto inventato: il database e le foto di nuovaProva, chiavi age
// generate nel test. Gli archivi ostili si costruiscono a partire da uno buono: si scompone il tar, si
// cambia una voce, si ricompone e si cifra con la chiave di prova.

// oraVoci è l'ora che Scrivi dà alle voci in nuovaProva (la sua ora di cattura, al secondo).
var oraVoci = time.Date(2026, 11, 25, 2, 30, 0, 0, time.UTC)

// vocePiana è una voce del tar: intestazione e contenuto.
type vocePiana struct {
	h tar.Header
	b []byte
}

// chiaroBuono è il tar in chiaro di un backup vero di p.
func chiaroBuono(t *testing.T, p *prova) []byte {
	t.Helper()
	var b bytes.Buffer
	if _, err := Scrivi(context.Background(), p.s, &b); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func scomponi(t *testing.T, chiaro []byte) []vocePiana {
	t.Helper()
	tr := tar.NewReader(bytes.NewReader(chiaro))
	var v []vocePiana
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return v
		}
		if err != nil {
			t.Fatal(err)
		}
		b, err := io.ReadAll(tr)
		if err != nil {
			t.Fatal(err)
		}
		v = append(v, vocePiana{h: *h, b: b})
	}
}

// ricomponi scrive le voci in un tar chiuso come lo chiude Scrivi; la dimensione è quella del
// contenuto, tranne per i collegamenti e le cartelle.
func ricomponi(t *testing.T, v []vocePiana) []byte {
	t.Helper()
	var b bytes.Buffer
	tw := tar.NewWriter(&b)
	for _, x := range v {
		h := x.h
		h.Size = int64(len(x.b))
		if h.ModTime.IsZero() {
			// Le voci aggiunte dal test prendono l'ora delle altre: così le ferma la guardia che si prova.
			h.ModTime = oraVoci
		}
		if len(h.PAXRecords) != 0 {
			h.Format = tar.FormatPAX
		}
		if err := tw.WriteHeader(&h); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(x.b); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// rinnovaInventario rifà inventory.json dalle voci (tranne i due json), come lo scrive Scrivi: così
// un archivio ostile non si ferma all'inventario ma alla guardia che si prova.
func rinnovaInventario(t *testing.T, v []vocePiana) []vocePiana {
	t.Helper()
	inv := map[string]string{}
	for _, x := range v {
		if x.h.Name != "inventory.json" && x.h.Name != "snapshot.json" {
			inv[x.h.Name] = impronta(x.b)
		}
	}
	j, err := json.Marshal(inv)
	if err != nil {
		t.Fatal(err)
	}
	return cambia(t, v, "inventory.json", func(*tar.Header, []byte) []byte { return j })
}

// cambia sostituisce il contenuto (e se serve l'intestazione) della voce n.
func cambia(t *testing.T, v []vocePiana, n string, f func(h *tar.Header, b []byte) []byte) []vocePiana {
	t.Helper()
	v = slices.Clone(v)
	for i := range v {
		if v[i].h.Name == n {
			v[i].b = f(&v[i].h, slices.Clone(v[i].b))
			return v
		}
	}
	t.Fatalf("voce %q non trovata", n)
	return nil
}

// cambiaFoto cambia snapshot.json e la riscrive come Scrivi.
func cambiaFoto(t *testing.T, v []vocePiana, f func(*Fotografia)) []vocePiana {
	t.Helper()
	return cambia(t, v, "snapshot.json", func(_ *tar.Header, b []byte) []byte {
		var x Fotografia
		if err := json.Unmarshal(b, &x); err != nil {
			t.Fatal(err)
		}
		f(&x)
		j, err := json.Marshal(x)
		if err != nil {
			t.Fatal(err)
		}
		return j
	})
}

func togli(v []vocePiana, n string) []vocePiana {
	return slices.DeleteFunc(slices.Clone(v), func(x vocePiana) bool { return x.h.Name == n })
}

// inserisci mette x prima della voce n ("" per la fine).
func inserisci(t *testing.T, v []vocePiana, n string, x vocePiana) []vocePiana {
	t.Helper()
	if n == "" {
		return append(slices.Clone(v), x)
	}
	i := slices.IndexFunc(v, func(y vocePiana) bool { return y.h.Name == n })
	if i < 0 {
		t.Fatalf("voce %q non trovata", n)
	}
	return slices.Insert(slices.Clone(v), i, x)
}

func voceFile(n string, b []byte) vocePiana {
	return vocePiana{h: tar.Header{Typeflag: tar.TypeReg, Name: n, Mode: 0o600}, b: b}
}

func cifraPer(t *testing.T, r age.Recipient, chiaro []byte) []byte {
	t.Helper()
	var b bytes.Buffer
	w, err := age.Encrypt(&b, r)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(chiaro); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// verificaCifrato scrive cifrato in un file fuori dai dati e lo verifica; controlla sempre che la
// cartella temporanea della verifica non resti.
func verificaCifrato(t *testing.T, p *prova, cifrato []byte) (Verificato, error) {
	t.Helper()
	f := filepath.Join(t.TempDir(), "prova.tar.age")
	if err := os.WriteFile(f, cifrato, 0o600); err != nil {
		t.Fatal(err)
	}
	v, err := VerificaArchivio(context.Background(), p.dir, f)
	senzaTemporanee(t, p.dir)
	return v, err
}

func senzaTemporanee(t *testing.T, dati string) {
	t.Helper()
	m, err := filepath.Glob(filepath.Join(dati, prefissoVerifica+"*"))
	if err != nil || len(m) != 0 {
		t.Errorf("cartelle temporanee della verifica rimaste: %v %v", m, err)
	}
}

// provaConChiave è nuovaProva con una chiave vera dei backup, il tar buono e le sue voci.
func provaConChiave(t *testing.T) (*prova, age.Recipient, []vocePiana) {
	t.Helper()
	p := nuovaProva(t)
	c := conChiave(t, p)
	r, err := age.ParseX25519Recipient(c.Pubblica)
	if err != nil {
		t.Fatal(err)
	}
	return p, r, scomponi(t, chiaroBuono(t, p))
}

// Voce OPS-16 e OPS-18, in parte: niente segno «inventario:» (OPS-16 chiude con chiavetta, lavoro
// notturno e SFTP, 123-128; OPS-18 con il ripristino, 130).
// Giro completo: Cifra, poi VerificaArchivio dà lo stesso SHA-256, gli stessi byte, la stessa
// fotografia e lo stesso inventario; il database si estrae in una cartella 0700 del volume dei dati,
// in un file 0600, e la cartella non resta.
func TestVerificaGiroCompleto(t *testing.T) {
	p := nuovaProva(t)
	conChiave(t, p)
	r, err := Cifra(context.Background(), p.s, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	visto := false
	dopoEstrazione = func(f string) {
		visto = true
		dir := filepath.Dir(f)
		if filepath.Dir(dir) != p.dir || !strings.HasPrefix(filepath.Base(dir), prefissoVerifica) {
			t.Errorf("database estratto in %s, atteso in una %s* della cartella dei dati", f, prefissoVerifica)
		}
		for n, m := range map[string]os.FileMode{dir: os.ModeDir | 0o700, f: 0o600} {
			if fi, err := os.Lstat(n); err != nil || fi.Mode() != m {
				t.Errorf("%s: %v %v, atteso %v", n, fi.Mode(), err, m)
			}
		}
	}
	t.Cleanup(func() { dopoEstrazione = nil })
	v, err := VerificaArchivio(context.Background(), p.dir, r.File)
	if err != nil {
		t.Fatal(err)
	}
	if !visto {
		t.Error("database mai estratto")
	}
	senzaTemporanee(t, p.dir)
	if v.File != r.File || v.SHA256 != r.SHA256 || v.ByteCifrati != r.ByteCifrati {
		t.Errorf("verificato %s %s %d, scritto %s %s %d", v.File, v.SHA256, v.ByteCifrati, r.File, r.SHA256, r.ByteCifrati)
	}
	if !maps.Equal(v.Inventario, r.Inventario) || !maps.Equal(v.Conteggi, r.Conteggi) ||
		!slices.Equal(v.Migrazioni, r.Migrazioni) || v.CatturatoIl != r.CatturatoIl || v.Versione != r.Versione {
		t.Errorf("verificato %+v\nscritto %+v", v.Fotografia, r.Fotografia)
	}
	if v.Conteggi["cancellazioni_media"] != 3 {
		t.Errorf("conteggi %v", v.Conteggi)
	}
}

// Il tar ricomposto senza cambiamenti passa: le prove qui sotto cambiano una cosa sola.
func TestVerificaRicompostoPassa(t *testing.T) {
	p, r, v := provaConChiave(t)
	if _, err := verificaCifrato(t, p, cifraPer(t, r, ricomponi(t, v))); err != nil {
		t.Fatal(err)
	}
}

// Voce OPS-18, in parte (senza segno «inventario:», vedi TestVerificaGiroCompleto).
// Percorsi che escono, assoluti, collegamenti, file speciali, duplicati, voci fuori posto o inattese:
// non verificato, anche con l'inventario rifatto apposta.
func TestVerificaRifiutaVoci(t *testing.T) {
	p, r, buone := provaConChiave(t)
	foto := "media/" + p.foto[0]
	altra := strings.Repeat("e", 64)
	link := func(tipo byte, n, a string) vocePiana {
		return vocePiana{h: tar.Header{Typeflag: tipo, Name: n, Linkname: a, Mode: 0o600}}
	}
	casi := []struct {
		nome string
		v    []vocePiana
	}{
		{"punto punto", inserisci(t, buone, "inventory.json", voceFile("../fuori", []byte("x")))},
		{"punto punto dentro", inserisci(t, buone, "inventory.json", voceFile("media/../../fuori", []byte("x")))},
		{"assoluto", inserisci(t, buone, "inventory.json", voceFile("/etc/fuori", []byte("x")))},
		{"sottocartella", inserisci(t, buone, "inventory.json", voceFile("media/a/"+altra, []byte("x")))},
		{"nome non pulito", inserisci(t, buone, "inventory.json", voceFile("media//"+altra, []byte("x")))},
		{"collegamento simbolico", inserisci(t, buone, "inventory.json", link(tar.TypeSymlink, "media/"+altra, "/etc/passwd"))},
		{"collegamento fisico", inserisci(t, buone, "inventory.json", link(tar.TypeLink, "media/"+altra, "data/gestrip.db"))},
		{"cartella", inserisci(t, buone, foto, vocePiana{h: tar.Header{Typeflag: tar.TypeDir, Name: "media/", Mode: 0o700}})},
		// Al loro posto nell'ordine, senza Linkname, con l'impronta del contenuto vuoto: solo il tipo li
		// ferma.
		{"fifo", inserisci(t, buone, "secrets/segreto_flask", vocePiana{h: tar.Header{Typeflag: tar.TypeFifo, Name: "media/" + strings.Repeat("f", 64), Mode: 0o600}})},
		{"dispositivo", inserisci(t, buone, "secrets/segreto_flask", vocePiana{h: tar.Header{Typeflag: tar.TypeChar, Name: "media/" + strings.Repeat("f", 64), Mode: 0o600}})},
		{"duplicato", inserisci(t, buone, "inventory.json", voceFile(foto, []byte("foto inventata uno")))},
		{"database duplicato", inserisci(t, buone, foto, buone[0])},
		{"voce dopo snapshot", inserisci(t, buone, "", voceFile("media/"+altra, []byte("x")))},
		{"database dopo i media", append(togli(buone, "data/gestrip.db")[:2:2], append([]vocePiana{buone[0]}, togli(buone, "data/gestrip.db")[2:]...)...)},
		{"media fuori ordine", func() []vocePiana {
			v := slices.Clone(buone)
			v[1], v[2] = v[2], v[1]
			return v
		}()},
		{"nome sconosciuto in data", inserisci(t, buone, foto, voceFile("data/altro.db", []byte("x")))},
		{"nome non a indirizzo di contenuto", inserisci(t, buone, "inventory.json", voceFile("media/foto.jpg", []byte("x")))},
		{"segreto con una chiave age", cambia(t, buone, "secrets/segreto_flask", func(*tar.Header, []byte) []byte {
			return []byte("age-secret-key-1inventata")
		})},
		{"permessi", cambia(t, buone, foto, func(h *tar.Header, b []byte) []byte { h.Mode = 0o644; return b })},
		{"campo PAX", cambia(t, buone, foto, func(h *tar.Header, b []byte) []byte {
			h.PAXRecords = map[string]string{"comment": "in più"}
			return b
		})},
		{"attributo esteso", cambia(t, buone, foto, func(h *tar.Header, b []byte) []byte {
			h.PAXRecords = map[string]string{"SCHILY.xattr.user.x": "y"}
			return b
		})},
		{"senza database", togli(buone, "data/gestrip.db")},
		{"senza inventario", togli(buone, "inventory.json")},
		{"senza fotografia", togli(buone, "snapshot.json")},
		{"tar vuoto", nil},
	}
	for _, c := range casi {
		t.Run(c.nome, func(t *testing.T) {
			v := c.v
			if slices.ContainsFunc(v, func(x vocePiana) bool { return x.h.Name == "inventory.json" }) {
				v = rinnovaInventario(t, v)
			}
			_, err := verificaCifrato(t, p, cifraPer(t, r, ricomponi(t, v)))
			if !errors.Is(err, ErrVoce) {
				t.Fatalf("errore %v, atteso ErrVoce", err)
			}
			t.Log(err)
		})
	}
}

// Voce OPS-16 e OPS-18, in parte (senza segno «inventario:», vedi TestVerificaGiroCompleto).
// Un file in più, un file mancante, un'impronta sbagliata, un inventario malformato: non verificato.
func TestVerificaInventario(t *testing.T) {
	p, r, buone := provaConChiave(t)
	foto := "media/" + p.foto[0]
	inv := func(f func(m map[string]string)) []vocePiana {
		return cambia(t, buone, "inventory.json", func(_ *tar.Header, b []byte) []byte {
			var m map[string]string
			if err := json.Unmarshal(b, &m); err != nil {
				t.Fatal(err)
			}
			f(m)
			j, err := json.Marshal(m)
			if err != nil {
				t.Fatal(err)
			}
			return j
		})
	}
	testo := func(s string) []vocePiana {
		return cambia(t, buone, "inventory.json", func(*tar.Header, []byte) []byte { return []byte(s) })
	}
	casi := []struct {
		nome string
		v    []vocePiana
	}{
		// In fondo ai media (nessuna chiave inventata comincia con 64 «f»), al suo posto nell'ordine.
		{"file in più", inserisci(t, buone, "secrets/segreto_flask", voceFile("media/"+strings.Repeat("f", 64), []byte("x")))},
		{"file mancante", togli(buone, foto)},
		{"voce mancante nell'inventario", inv(func(m map[string]string) { delete(m, foto) })},
		{"impronta sbagliata", cambia(t, buone, foto, func(_ *tar.Header, b []byte) []byte { b[0] ^= 1; return b })},
		{"database cambiato", cambia(t, buone, "data/gestrip.db", func(_ *tar.Header, b []byte) []byte { b[len(b)-1] ^= 1; return b })},
		{"segreto cambiato", cambia(t, buone, "secrets/segreto_flask", func(_ *tar.Header, b []byte) []byte { return append(b, 'x') })},
		{"impronta maiuscola", inv(func(m map[string]string) { m[foto] = strings.ToUpper(m[foto]) })},
		{"impronta corta", inv(func(m map[string]string) { m[foto] = m[foto][:63] })},
		{"non JSON", testo("non json")},
		{"lista", testo(`["a"]`)},
		{"null", testo(`null`)},
		{"oggetto vuoto", testo(`{}`)},
		{"valore numero", testo(`{"data/gestrip.db":1}`)},
		{"spazio in più", cambia(t, buone, "inventory.json", func(_ *tar.Header, b []byte) []byte { return append(b, '\n') })},
		{"JSON dopo il JSON", cambia(t, buone, "inventory.json", func(_ *tar.Header, b []byte) []byte { return append(b, []byte("{}")...) })},
		{"chiave doppia", cambia(t, buone, "inventory.json", func(_ *tar.Header, b []byte) []byte {
			// La stessa voce due volte, con la stessa impronta: json.Unmarshal la prenderebbe.
			var m map[string]string
			if err := json.Unmarshal(b, &m); err != nil {
				t.Fatal(err)
			}
			return []byte(strings.Replace(string(b), "{", `{"`+foto+`":"`+m[foto]+`",`, 1))
		})},
	}
	for _, c := range casi {
		t.Run(c.nome, func(t *testing.T) {
			_, err := verificaCifrato(t, p, cifraPer(t, r, ricomponi(t, c.v)))
			if !errors.Is(err, ErrInventario) {
				t.Fatalf("errore %v, atteso ErrInventario", err)
			}
		})
	}
}

// Voce OPS-16, in parte (senza segno «inventario:», vedi TestVerificaGiroCompleto).
// snapshot.json malformata, sconosciuta o diversa dal database: non verificato.
func TestVerificaFotografia(t *testing.T) {
	p, r, buone := provaConChiave(t)
	foto := func(f func(*Fotografia)) []vocePiana { return cambiaFoto(t, buone, f) }
	testo := func(f func(string) string) []vocePiana {
		return cambia(t, buone, "snapshot.json", func(_ *tar.Header, b []byte) []byte { return []byte(f(string(b))) })
	}
	casi := []struct {
		nome string
		v    []vocePiana
	}{
		{"conteggio diverso", foto(func(f *Fotografia) { f.Conteggi["cancellazioni_media"]++ })},
		{"conteggio a zero", foto(func(f *Fotografia) { f.Conteggi["cancellazioni_media"] = 0 })},
		{"tabella in più", foto(func(f *Fotografia) { f.Conteggi["tabella_inventata"] = 0 })},
		{"tabella mancante", foto(func(f *Fotografia) { delete(f.Conteggi, "cancellazioni_media") })},
		{"conteggio negativo", foto(func(f *Fotografia) { f.Conteggi["tabella_inventata"] = -1 })},
		{"senza conteggi", foto(func(f *Fotografia) { f.Conteggi = nil })},
		{"migrazione mancante", foto(func(f *Fotografia) { f.Migrazioni = f.Migrazioni[:len(f.Migrazioni)-1] })},
		{"migrazioni fuori sequenza", foto(func(f *Fotografia) { f.Migrazioni[0] = 0 })},
		{"senza migrazioni", foto(func(f *Fotografia) { f.Migrazioni = nil })},
		{"formato sconosciuto", foto(func(f *Fotografia) { f.Formato = Formato + 1 })},
		{"senza versione", foto(func(f *Fotografia) { f.Versione = " " })},
		{"ora di cattura sbagliata", foto(func(f *Fotografia) { f.CatturatoIl = "2026-11-25" })},
		{"ora di cattura non UTC", foto(func(f *Fotografia) { f.CatturatoIl = "2026-11-25T02:30:00.000000+01:00" })},
		{"campo sconosciuto", testo(func(s string) string { return strings.Replace(s, "{", `{"in_piu":1,`, 1) })},
		{"campo doppio", testo(func(s string) string { return strings.Replace(s, "{", `{"formato":1,`, 1) })},
		{"spazio in più", testo(func(s string) string { return s + " " })},
		{"numero scritto diverso", testo(func(s string) string { return strings.Replace(s, `"formato":1`, `"formato":1.0`, 1) })},
		{"non JSON", testo(func(string) string { return "{" })},
	}
	for _, c := range casi {
		t.Run(c.nome, func(t *testing.T) {
			_, err := verificaCifrato(t, p, cifraPer(t, r, ricomponi(t, rinnovaInventario(t, c.v))))
			if !errors.Is(err, ErrFotografia) {
				t.Fatalf("errore %v, atteso ErrFotografia", err)
			}
		})
	}
}

// guastaIndice rovina una pagina di un indice con righe: le tabelle restano leggibili (i conteggi
// tornano), integrity_check no.
func guastaIndice(t *testing.T, dbb []byte) []byte {
	t.Helper()
	f := filepath.Join(t.TempDir(), "g.db")
	if err := os.WriteFile(f, dbb, 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := sql.Open("sqlite", db.URIDi(f)+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	var radice, pagina int64
	err = c.QueryRow(`SELECT rootpage FROM sqlite_schema WHERE type = 'index' AND tbl_name = 'cancellazioni_media' AND rootpage > 0`).Scan(&radice)
	if err == nil {
		err = c.QueryRow(`PRAGMA page_size`).Scan(&pagina)
	}
	if errC := c.Close(); err == nil {
		err = errC
	}
	if err != nil {
		t.Fatal(err)
	}
	b := slices.Clone(dbb)
	fine := radice * pagina
	for i := fine - 40; i < fine; i++ {
		b[i] ^= 0x55
	}
	return b
}

// Voce OPS-16, in parte (senza segno «inventario:», vedi TestVerificaGiroCompleto).
// Una pagina rovinata (con l'impronta rifatta: il backup l'avrebbe copiata così) non passa
// integrity_check; un file che non è un database nemmeno.
func TestVerificaIntegrita(t *testing.T) {
	p, r, buone := provaConChiave(t)
	casi := []struct {
		nome string
		f    func(b []byte) []byte
	}{
		{"pagina dell'indice rovinata", func(b []byte) []byte { return guastaIndice(t, b) }},
		{"non è un database", func(b []byte) []byte { return bytes.Repeat([]byte("x"), len(b)) }},
		{"intestazione rovinata", func(b []byte) []byte { b[20] = 0; b[21] = 0; return b }},
	}
	for _, c := range casi {
		t.Run(c.nome, func(t *testing.T) {
			v := cambia(t, buone, "data/gestrip.db", func(_ *tar.Header, b []byte) []byte { return c.f(b) })
			_, err := verificaCifrato(t, p, cifraPer(t, r, ricomponi(t, rinnovaInventario(t, v))))
			if !errors.Is(err, ErrIntegrita) {
				t.Fatalf("errore %v, atteso ErrIntegrita", err)
			}
		})
	}
	// Un database vuoto passa integrity_check ma non ha migrazioni né tabelle.
	v := cambia(t, buone, "data/gestrip.db", func(*tar.Header, []byte) []byte { return nil })
	if _, err := verificaCifrato(t, p, cifraPer(t, r, ricomponi(t, rinnovaInventario(t, v)))); !errors.Is(err, ErrFotografia) {
		t.Fatalf("database vuoto: %v", err)
	}
}

// ritocca cambia con f il blocco grezzo di un'intestazione tar e ne rifà la somma di controllo.
func ritocca(blocco []byte, f func([]byte)) {
	f(blocco)
	copy(blocco[148:156], "        ")
	somma := 0
	for _, c := range blocco[:512] {
		somma += int(c)
	}
	copy(blocco[148:156], fmt.Sprintf("%06o\x00 ", somma))
}

// bloccoDi è il blocco grezzo dell'intestazione della voce n nel tar in chiaro.
func bloccoDi(t *testing.T, chiaro []byte, n string) []byte {
	t.Helper()
	for i := 0; i+512 <= len(chiaro); i += 512 {
		if bytes.HasPrefix(chiaro[i:], append([]byte(n), 0)) && bytes.Equal(chiaro[i+257:i+263], []byte("ustar\x00")) {
			return chiaro[i : i+512]
		}
	}
	t.Fatalf("intestazione di %q non trovata", n)
	return nil
}

// Voce OPS-18, in parte (senza segno «inventario:», vedi TestVerificaGiroCompleto).
// Un segreto che non è nella lista bianca non passa nemmeno al suo posto nell'ordine (prima di
// segreto_flask): lo ferma la lista, non l'ordine. Così la chiave dei backup.
func TestVerificaSegretiFuoriLista(t *testing.T) {
	p, r, buone := provaConChiave(t)
	for _, n := range []string{"secrets/" + NomeChiaveBackup, "secrets/altro_segreto"} {
		v := rinnovaInventario(t, inserisci(t, buone, "secrets/segreto_flask", voceFile(n, []byte("segreto inventato"))))
		_, err := verificaCifrato(t, p, cifraPer(t, r, ricomponi(t, v)))
		if !errors.Is(err, ErrVoce) || !strings.Contains(err.Error(), "non è un file del backup") {
			t.Errorf("%s: errore %v, atteso il rifiuto della lista dei segreti", n, err)
		}
	}
}

// Voce OPS-18, in parte (senza segno «inventario:», vedi TestVerificaGiroCompleto).
// Intestazioni diverse da quelle di Scrivi, con il contenuto giusto e al loro posto: il vecchio tipo
// '\x00' (il lettore lo dà come TypeReg), un collegamento su un file normale, utente o gruppo con nome
// o numero, formato GNU, un'ora diversa. Tutte fermate dal controllo dell'intestazione.
func TestVerificaIntestazioni(t *testing.T) {
	p, r, buone := provaConChiave(t)
	foto := "media/" + p.foto[0]
	h := func(f func(h *tar.Header)) []vocePiana {
		return cambia(t, buone, foto, func(h *tar.Header, b []byte) []byte { f(h); return b })
	}
	casi := map[string][]byte{
		"tipo vecchio": func() []byte {
			c := ricomponi(t, buone)
			ritocca(bloccoDi(t, c, foto), func(b []byte) { b[156] = 0 })
			return c
		}(),
		"collegamento su un file normale": ricomponi(t, h(func(h *tar.Header) { h.Linkname = "data/gestrip.db" })),
		"utente con nome":                 ricomponi(t, h(func(h *tar.Header) { h.Uname = "root" })),
		"gruppo con nome":                 ricomponi(t, h(func(h *tar.Header) { h.Gname = "root" })),
		"utente":                          ricomponi(t, h(func(h *tar.Header) { h.Uid = 1000 })),
		"gruppo":                          ricomponi(t, h(func(h *tar.Header) { h.Gid = 1000 })),
		"formato GNU":                     ricomponi(t, h(func(h *tar.Header) { h.Format = tar.FormatGNU })),
		"ora diversa":                     ricomponi(t, h(func(h *tar.Header) { h.ModTime = oraVoci.Add(time.Second) })),
		"ora di tutte diversa dalla cattura": ricomponi(t, func() []vocePiana {
			v := slices.Clone(buone)
			for i := range v {
				v[i].h.ModTime = oraVoci.Add(time.Hour)
			}
			return v
		}()),
	}
	for nome, c := range casi {
		t.Run(nome, func(t *testing.T) {
			_, err := verificaCifrato(t, p, cifraPer(t, r, c))
			if !errors.Is(err, ErrVoce) {
				t.Fatalf("errore %v, atteso ErrVoce", err)
			}
			t.Log(err)
		})
	}
}

// Un database oltre 8 GiB: tar.Writer (come lo usa Scrivi, formato PAX) scrive la dimensione in un
// campo PAX «size», perché il campo USTAR non ci sta. Senza scrivere 8 GiB: si scrive la sola
// intestazione e la si rilegge come la legge la verifica. Ammesso solo per il database e solo con la
// dimensione giusta; ogni altro campo PAX no.
func TestVerificaDatabaseGrande(t *testing.T) {
	const grande = 9 << 30
	leggi := func(n string) (*tar.Header, []byte) {
		t.Helper()
		var b bytes.Buffer
		tw := tar.NewWriter(&b)
		if err := tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: n, Size: grande, Mode: 0o600,
			ModTime: oraVoci, Format: tar.FormatPAX}); err != nil {
			t.Fatal(err)
		}
		z := &lettoreCoda{r: bytes.NewReader(b.Bytes())}
		h, err := tar.NewReader(z).Next()
		if err != nil {
			t.Fatal(err)
		}
		return h, slices.Clone(z.blocco[:])
	}
	h, blocco := leggi("data/gestrip.db")
	if h.Size != grande || h.PAXRecords["size"] != "9663676416" || len(h.PAXRecords) != 1 {
		t.Fatalf("intestazione letta: %d %v", h.Size, h.PAXRecords)
	}
	if err := controllaIntestazione(h, blocco); err != nil {
		t.Fatalf("database di 9 GiB rifiutato: %v", err)
	}
	hm, bm := leggi("media/" + strings.Repeat("a", 64))
	if err := controllaIntestazione(hm, bm); !errors.Is(err, ErrVoce) {
		t.Errorf("campo size su un media: %v", err)
	}
	for nome, f := range map[string]func(h *tar.Header){
		"dimensione diversa": func(h *tar.Header) { h.PAXRecords = map[string]string{"size": "1"} },
		"campo in più":       func(h *tar.Header) { h.PAXRecords = map[string]string{"size": "9663676416", "comment": "x"} },
		"formato USTAR":      func(h *tar.Header) { h.Format = tar.FormatUSTAR },
	} {
		x := *h
		x.PAXRecords = maps.Clone(h.PAXRecords)
		f(&x)
		if err := controllaIntestazione(&x, blocco); !errors.Is(err, ErrVoce) {
			t.Errorf("%s: %v", nome, err)
		}
	}
	// Un database piccolo senza campi PAX resta USTAR.
	x := *h
	x.PAXRecords, x.Size = nil, 4096
	if err := controllaIntestazione(&x, blocco); !errors.Is(err, ErrVoce) {
		t.Errorf("database senza PAX in formato PAX: %v", err)
	}
}

// Un archivio cifrato per la chiave dei backup e per un altro destinatario non è nostro: rifiutato
// anche se la chiave lo apre.
func TestVerificaAltriDestinatari(t *testing.T) {
	p, r, buone := provaConChiave(t)
	altra, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	w, err := age.Encrypt(&b, r, altra.Recipient())
	if err == nil {
		_, err = w.Write(ricomponi(t, buone))
	}
	if err == nil {
		err = w.Close()
	}
	if err != nil {
		t.Fatal(err)
	}
	if _, err := verificaCifrato(t, p, b.Bytes()); !errors.Is(err, ErrDestinatari) {
		t.Fatalf("errore %v, atteso ErrDestinatari", err)
	}
}

// Il database estratto si apre in sola lettura e con trusted_schema spento. Il driver non ha funzioni
// non innocue da far girare dallo schema (provato: CHECK, colonne generate e indici con funzioni non
// deterministiche danno lo stesso esito con e senza), quindi si controlla l'apertura stessa.
func TestVerificaAperturaDelDatabase(t *testing.T) {
	f := filepath.Join(t.TempDir(), "v.db")
	c, err := sql.Open("sqlite", db.URIDi(f))
	if err == nil {
		_, err = c.Exec(`CREATE TABLE t (a)`)
	}
	if errC := c.Close(); err == nil {
		err = errC
	}
	if err != nil {
		t.Fatal(err)
	}
	c, err = sql.Open("sqlite", dsnVerifica(f))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	var fidato int
	if err := c.QueryRow(`PRAGMA trusted_schema`).Scan(&fidato); err != nil || fidato != 0 {
		t.Errorf("trusted_schema %d %v, atteso 0", fidato, err)
	}
	if _, err := c.Exec(`INSERT INTO t VALUES (1)`); err == nil {
		t.Error("il database estratto si scrive")
	}
}

// inizioPezzi è dove cominciano i pezzi di 64 KiB del testo cifrato di age: dopo l'intestazione
// (finisce con la riga «--- mac») e i 16 byte del nonce.
func inizioPezzi(t *testing.T, b []byte) int {
	t.Helper()
	i := bytes.Index(b, []byte("\n--- "))
	if i < 0 {
		t.Fatal("intestazione age non trovata")
	}
	j := bytes.IndexByte(b[i+1:], '\n')
	if n := i + 1 + j + 1 + 16 + 2*(64<<10+16); j < 0 || len(b) < n {
		t.Fatalf("archivio di prova troppo corto (%d byte) per due pezzi interi", len(b))
	}
	return i + 1 + j + 1 + 16
}

// Voce OPS-18, in parte (senza segno «inventario:», vedi TestVerificaGiroCompleto).
// Testo cifrato troncato o toccato, chiave sbagliata, byte dopo la fine, file che non è age, un
// collegamento al posto del file: non verificato.
func TestVerificaCifratura(t *testing.T) {
	p, r, buone := provaConChiave(t)
	buono := cifraPer(t, r, ricomponi(t, buone))
	altra, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	gira := func(n int, f func(b []byte) []byte) []byte { b := slices.Clone(buono); b[n] ^= 1; return f(b) }
	casi := map[string][]byte{
		"troncato di un byte":            buono[:len(buono)-1],
		"troncato a un pezzo intero":     buono[:inizioPezzi(t, buono)+64<<10+16],
		"solo l'intestazione":            buono[:200],
		"vuoto":                          nil,
		"byte toccato nel mezzo":         gira(len(buono)/2, func(b []byte) []byte { return b }),
		"byte toccato in fondo":          gira(len(buono)-1, func(b []byte) []byte { return b }),
		"byte toccato nell'intestazione": gira(60, func(b []byte) []byte { return b }),
		"byte in più dopo la fine":       append(slices.Clone(buono), 0),
		"pezzo in più dopo la fine":      append(slices.Clone(buono), buono[len(buono)-100:]...),
		"chiave sbagliata":               cifraPer(t, altra.Recipient(), ricomponi(t, buone)),
		"non è age":                      ricomponi(t, buone),
	}
	for nome, c := range casi {
		t.Run(nome, func(t *testing.T) {
			_, err := verificaCifrato(t, p, c)
			if err == nil {
				t.Fatal("verificato")
			}
			t.Log(err)
		})
	}
	// Un collegamento simbolico all'archivio buono non si segue; una cartella non è un archivio.
	dir := t.TempDir()
	buonoF := filepath.Join(dir, "buono.tar.age")
	if err := os.WriteFile(buonoF, buono, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := VerificaArchivio(context.Background(), p.dir, buonoF); err != nil {
		t.Fatalf("l'archivio buono non passa: %v", err)
	}
	if err := os.Symlink(buonoF, filepath.Join(dir, "link.tar.age")); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{filepath.Join(dir, "link.tar.age"), dir, filepath.Join(dir, "manca.tar.age")} {
		if _, err := VerificaArchivio(context.Background(), p.dir, f); err == nil {
			t.Errorf("%s verificato", f)
		}
	}
	senzaTemporanee(t, p.dir)
}

// Voce OPS-18, in parte (senza segno «inventario:», vedi TestVerificaGiroCompleto).
// La fine del tar è quella che scrive Scrivi: due blocchi vuoti e niente dopo.
func TestVerificaFineDelTar(t *testing.T) {
	p, r, buone := provaConChiave(t)
	chiaro := ricomponi(t, buone)
	casi := map[string][]byte{
		"spazzatura dopo il tar": append(slices.Clone(chiaro), []byte("spazzatura")...),
		"un blocco vuoto in più": append(slices.Clone(chiaro), make([]byte, 512)...),
		"un byte zero in più":    append(slices.Clone(chiaro), 0),
		"un solo blocco vuoto":   chiaro[:len(chiaro)-512],
		"senza blocchi vuoti":    chiaro[:len(chiaro)-1024],
		"blocco vuoto a metà":    chiaro[:len(chiaro)-256],
		"un tar dopo il tar":     append(slices.Clone(chiaro), chiaro...),
	}
	for nome, c := range casi {
		t.Run(nome, func(t *testing.T) {
			_, err := verificaCifrato(t, p, cifraPer(t, r, c))
			if err == nil {
				t.Fatal("verificato")
			}
			t.Log(err)
		})
	}
	// I due che il lettore tar di Go accetterebbe li ferma il controllo della fine.
	for _, c := range [][]byte{chiaro[:len(chiaro)-512], chiaro[:len(chiaro)-1024], append(slices.Clone(chiaro), make([]byte, 512)...)} {
		if _, err := verificaCifrato(t, p, cifraPer(t, r, c)); !errors.Is(err, ErrFineArchivio) {
			t.Errorf("errore %v, atteso ErrFineArchivio", err)
		}
	}
}

// Voce OPS-18, in parte (senza segno «inventario:», vedi TestVerificaGiroCompleto).
// Limiti contro un archivio costruito apposta: voci troppe o troppo grandi, intestazioni enormi.
func TestVerificaLimiti(t *testing.T) {
	p, r, buone := provaConChiave(t)
	controlla := func(t *testing.T, v []vocePiana, atteso error) {
		t.Helper()
		_, err := verificaCifrato(t, p, cifraPer(t, r, ricomponi(t, rinnovaInventario(t, v))))
		if !errors.Is(err, atteso) {
			t.Fatalf("errore %v, atteso %v", err, atteso)
		}
	}
	t.Run("troppe voci", func(t *testing.T) {
		vecchio := maxVoci
		maxVoci = len(buone) - 1
		t.Cleanup(func() { maxVoci = vecchio })
		controlla(t, buone, ErrVoce)
	})
	t.Run("voci al limite", func(t *testing.T) {
		vecchio := maxVoci
		maxVoci = len(buone)
		t.Cleanup(func() { maxVoci = vecchio })
		controlla(t, buone, nil)
	})
	t.Run("inventario troppo grande", func(t *testing.T) {
		vecchio := maxInventario
		maxInventario = int64(len(rinnovaInventario(t, buone)[len(buone)-2].b)) - 1
		t.Cleanup(func() { maxInventario = vecchio })
		controlla(t, buone, ErrVoce)
	})
	t.Run("fotografia troppo grande", func(t *testing.T) {
		vecchio := maxFotografia
		maxFotografia = int64(len(buone[len(buone)-1].b)) - 1
		t.Cleanup(func() { maxFotografia = vecchio })
		controlla(t, buone, ErrVoce)
	})
	t.Run("segreto troppo grande", func(t *testing.T) {
		controlla(t, cambia(t, buone, "secrets/segreto_flask", func(*tar.Header, []byte) []byte {
			return bytes.Repeat([]byte("s"), maxSegreto+1)
		}), ErrVoce)
	})
	t.Run("media troppo grande", func(t *testing.T) {
		b := bytes.Repeat([]byte("m"), media.MaxByte+1)
		v := cambia(t, buone, "media/"+p.foto[0], func(*tar.Header, []byte) []byte { return b })
		controlla(t, v, ErrVoce)
	})
	t.Run("intestazione PAX grande", func(t *testing.T) {
		controlla(t, cambia(t, buone, "media/"+p.foto[0], func(h *tar.Header, b []byte) []byte {
			h.PAXRecords = map[string]string{"comment": strings.Repeat("c", 900<<10)}
			return b
		}), ErrVoce)
	})
	t.Run("intestazione PAX oltre il limite del lettore", func(t *testing.T) {
		// Il tar.Writer non la scrive: una voce normale di 2 MiB diventa un'intestazione «x» cambiando
		// il tipo e rifacendo la somma di controllo del blocco.
		x := voceFile("PaxHeaders/x", bytes.Repeat([]byte("c"), 2<<20))
		chiaro := ricomponi(t, append([]vocePiana{x}, rinnovaInventario(t, buone)...))
		ritocca(chiaro[:512], func(b []byte) { b[156] = tar.TypeXHeader })
		_, err := verificaCifrato(t, p, cifraPer(t, r, chiaro))
		if !errors.Is(err, ErrVoce) || !errors.Is(err, tar.ErrFieldTooLong) {
			t.Fatalf("errore %v, atteso ErrVoce con ErrFieldTooLong", err)
		}
	})
	t.Run("nome lunghissimo", func(t *testing.T) {
		controlla(t, inserisci(t, buone, "inventory.json", voceFile("media/"+strings.Repeat("a", 4096), []byte("x"))), ErrVoce)
	})
}

// Il volume dei dati senza spazio per estrarre il database, o con uno spazio che non si legge: non
// verificato, prima di scrivere.
func TestVerificaSpazio(t *testing.T) {
	p, r, buone := provaConChiave(t)
	cifrato := cifraPer(t, r, ricomponi(t, buone))
	dbSize := uint64(len(buone[0].b))
	for _, c := range []struct {
		libero uint64
		err    error
		ok     bool
	}{
		{dbSize + MargineSpazio - 1, nil, false},
		{dbSize + MargineSpazio, nil, true},
		{1 << 50, errors.New("statfs inventato"), false},
	} {
		spazioLibero = func(string) (uint64, error) { return c.libero, c.err }
		scritto := false
		dopoEstrazione = func(string) { scritto = true }
		_, err := verificaCifrato(t, p, cifrato)
		dopoEstrazione = nil
		if c.ok != (err == nil) || scritto != c.ok {
			t.Errorf("libero %d (%v): errore %v, estratto %v", c.libero, c.err, err, scritto)
		}
		if c.err == nil && !c.ok && !errors.Is(err, ErrSpazio) {
			t.Errorf("errore %v, atteso ErrSpazio", err)
		}
	}
}

// Un contesto finito ferma la verifica (prima o durante) e la cartella temporanea si toglie; senza la
// chiave dei backup non si verifica niente.
func TestVerificaContestoEChiave(t *testing.T) {
	p, r, buone := provaConChiave(t)
	f := filepath.Join(t.TempDir(), "a.tar.age")
	if err := os.WriteFile(f, cifraPer(t, r, ricomponi(t, buone)), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, annulla := context.WithCancel(context.Background())
	annulla()
	if _, err := VerificaArchivio(ctx, p.dir, f); !errors.Is(err, context.Canceled) {
		t.Errorf("contesto finito prima: %v", err)
	}
	senzaTemporanee(t, p.dir)
	ctx, annulla = context.WithCancel(context.Background())
	defer annulla()
	dopoEstrazione = func(string) { annulla() }
	t.Cleanup(func() { dopoEstrazione = nil })
	if _, err := VerificaArchivio(ctx, p.dir, f); !errors.Is(err, context.Canceled) {
		t.Errorf("contesto finito durante: %v", err)
	}
	dopoEstrazione = nil
	senzaTemporanee(t, p.dir)
	if err := os.Remove(FileChiave(p.dir)); err != nil {
		t.Fatal(err)
	}
	if _, err := VerificaArchivio(context.Background(), p.dir, f); !errors.Is(err, ErrChiaveAssente) {
		t.Errorf("senza chiave: %v", err)
	}
}
