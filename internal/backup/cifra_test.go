package backup

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
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

// La chiave dei backup e la cifratura age (slice 120). Dati inventati (nuovaProva), chiavi age generate
// nel test e buttate con la sua cartella temporanea.

var istanteChiave = time.Date(2026, 11, 25, 2, 0, 0, 0, time.UTC)

// conChiave toglie la chiave finta di nuovaProva e ne crea una vera.
func conChiave(t *testing.T, p *prova) ChiaveNuova {
	t.Helper()
	if err := os.Remove(filepath.Join(p.dir, CartellaSegreti, NomeChiaveBackup)); err != nil {
		t.Fatal(err)
	}
	c, err := CreaChiave(p.dir, istanteChiave)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// voceDi dà i nomi dei file di dir (archivi, .pending, chiavi).
func voceDi(t *testing.T, dir string) []string {
	t.Helper()
	v, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var n []string
	for _, e := range v {
		n = append(n, e.Name())
	}
	return n
}

// La chiave nasce in secrets/chiave_backup, 0600 in una cartella 0700, nel formato di age-keygen; la
// privata data una volta è quella del file, la pubblica è la sua.
func TestChiaveCreata(t *testing.T) {
	dir := t.TempDir()
	c, err := CreaChiave(dir, istanteChiave)
	if err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(dir, CartellaSegreti, NomeChiaveBackup)
	if c.File != f {
		t.Errorf("file %q, atteso %q", c.File, f)
	}
	fi, err := os.Lstat(f)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode() != 0o600 {
		t.Errorf("permessi della chiave %v, attesi 0600", fi.Mode())
	}
	di, err := os.Stat(filepath.Dir(f))
	if err != nil {
		t.Fatal(err)
	}
	if di.Mode().Perm() != 0o700 {
		t.Errorf("permessi di secrets/ %v, attesi 0700", di.Mode().Perm())
	}
	id, err := LeggiChiave(dir)
	if err != nil {
		t.Fatal(err)
	}
	if id.String() != c.Privata || id.Recipient().String() != c.Pubblica {
		t.Error("la chiave data non è quella del file")
	}
	if !strings.HasPrefix(c.Privata, "AGE-SECRET-KEY-1") || !strings.HasPrefix(c.Pubblica, "age1") {
		t.Errorf("forma delle chiavi inattesa: %q", c.Pubblica)
	}
	b, err := os.ReadFile(f)
	if err != nil {
		t.Fatal(err)
	}
	if want := "# created: 2026-11-25T02:00:00Z\n# public key: " + c.Pubblica + "\n" + c.Privata + "\n"; string(b) != want {
		t.Errorf("contenuto %q, atteso il formato di age-keygen", b)
	}
	// Niente temporanei rimasti in secrets/: un file sconosciuto lì fermerebbe il backup.
	if v := voceDi(t, filepath.Dir(f)); len(v) != 1 {
		t.Errorf("in secrets/ %v, attesa solo la chiave", v)
	}
}

// Una chiave che c'è già (anche rovinata) non si sovrascrive mai: i backup fatti con lei resterebbero
// illeggibili.
func TestChiaveNonSovrascrive(t *testing.T) {
	dir := t.TempDir()
	primo, err := CreaChiave(dir, istanteChiave)
	if err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(dir, CartellaSegreti, NomeChiaveBackup)
	prima, err := os.ReadFile(f)
	if err != nil {
		t.Fatal(err)
	}
	c, err := CreaChiave(dir, istanteChiave.Add(time.Hour))
	if !errors.Is(err, ErrChiaveEsistente) || c.Privata != "" {
		t.Fatalf("seconda creazione: %v (privata data: %v), atteso ErrChiaveEsistente", err, c.Privata != "")
	}
	dopo, err := os.ReadFile(f)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(prima, dopo) || !strings.Contains(string(dopo), primo.Privata) {
		t.Fatal("la chiave è cambiata")
	}
	// Anche un file che non è una chiave resta com'è.
	if err := os.WriteFile(f, []byte("rovinata"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := CreaChiave(dir, istanteChiave); !errors.Is(err, ErrChiaveEsistente) {
		t.Fatalf("su un file rovinato: %v", err)
	}
	if b, _ := os.ReadFile(f); string(b) != "rovinata" {
		t.Error("il file rovinato è stato sovrascritto")
	}
	if v := voceDi(t, filepath.Dir(f)); len(v) != 1 {
		t.Errorf("in secrets/ %v, attesa solo la chiave", v)
	}
	// Lo stesso se la chiave compare fra il controllo e la scrittura (due comandi insieme).
	dir2 := t.TempDir()
	primaDiPubblicare = func(finale string) {
		if err := os.WriteFile(finale, []byte("l'altro comando"), 0o600); err != nil {
			t.Error(err)
		}
	}
	defer func() { primaDiPubblicare = nil }()
	if _, err := CreaChiave(dir2, istanteChiave); !errors.Is(err, ErrChiaveEsistente) {
		t.Fatalf("chiave comparsa a metà: %v", err)
	}
	f2 := filepath.Join(dir2, CartellaSegreti, NomeChiaveBackup)
	if b, _ := os.ReadFile(f2); string(b) != "l'altro comando" {
		t.Error("la chiave comparsa a metà è stata sovrascritta")
	}
	if v := voceDi(t, filepath.Dir(f2)); len(v) != 1 {
		t.Errorf("in secrets/ %v: il temporaneo con la chiave privata è rimasto", v)
	}
}

// La chiave si legge solo se è un file normale 0600 (o più stretto) con una sola identità X25519.
func TestLeggiChiave(t *testing.T) {
	dir := t.TempDir()
	if _, err := LeggiChiave(dir); !errors.Is(err, ErrChiaveAssente) {
		t.Fatalf("senza chiave: %v", err)
	}
	if _, err := CreaChiave(dir, istanteChiave); err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(dir, CartellaSegreti, NomeChiaveBackup)
	for _, m := range []os.FileMode{0o640, 0o604, 0o660, 0o607} {
		if err := os.Chmod(f, m); err != nil {
			t.Fatal(err)
		}
		if _, err := LeggiChiave(dir); !errors.Is(err, ErrChiavePermessi) {
			t.Errorf("permessi %v: %v, atteso ErrChiavePermessi", m, err)
		}
	}
	if err := os.Chmod(f, 0o400); err != nil {
		t.Fatal(err)
	}
	if _, err := LeggiChiave(dir); err != nil {
		t.Errorf("permessi 0400: %v", err)
	}
	if err := os.Chmod(f, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, c := range []string{"", "non una chiave\n", "AGE-SECRET-KEY-1XYZ\n"} {
		if err := os.WriteFile(f, []byte(c), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := LeggiChiave(dir); err == nil {
			t.Errorf("contenuto %q accettato", c)
		}
	}
	// Due identità: quale cifra? Nessuna.
	a, _ := age.GenerateX25519Identity()
	b, _ := age.GenerateX25519Identity()
	if err := os.WriteFile(f, []byte(a.String()+"\n"+b.String()+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LeggiChiave(dir); err == nil {
		t.Error("due identità accettate")
	}
	// Un collegamento al posto del file non si segue.
	if err := os.Remove(f); err != nil {
		t.Fatal(err)
	}
	altrove := filepath.Join(t.TempDir(), "chiave")
	if err := os.WriteFile(altrove, []byte(a.String()+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(altrove, f); err != nil {
		t.Fatal(err)
	}
	if _, err := LeggiChiave(dir); err == nil {
		t.Error("collegamento seguito")
	}
}

// Voce OPS-16, in parte (senza segno «inventario:», vedi TestArchivioContenuto).
// Giro completo: l'archivio cifrato si decifra con la chiave dei backup e il tar dentro ha ogni file
// con lo SHA-256 di inventory.json; il file è 0600, ha il nome con data e ora, nessun .pending resta.
func TestCifraGiroCompleto(t *testing.T) {
	p := nuovaProva(t)
	c := conChiave(t, p)
	dest := t.TempDir()
	r, err := Cifra(context.Background(), p.s, dest)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dest, "gestrip-backup-20261125T023000Z.tar.age"); r.File != want {
		t.Fatalf("archivio %q, atteso %q", r.File, want)
	}
	if v := voceDi(t, dest); len(v) != 1 {
		t.Fatalf("nella destinazione %v, atteso solo l'archivio", v)
	}
	fi, err := os.Lstat(r.File)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode() != 0o600 {
		t.Errorf("permessi dell'archivio %v, attesi 0600", fi.Mode())
	}
	cifrato, err := os.ReadFile(r.File)
	if err != nil {
		t.Fatal(err)
	}
	if int64(len(cifrato)) != r.ByteCifrati || fi.Size() != r.ByteCifrati {
		t.Errorf("byte cifrati %d, file %d", r.ByteCifrati, len(cifrato))
	}
	h := sha256.Sum256(cifrato)
	if hex.EncodeToString(h[:]) != r.SHA256 {
		t.Error("SHA-256 dell'archivio diverso da quello dato")
	}
	if bytes.Contains(cifrato, []byte("foto inventata")) || bytes.Contains(cifrato, []byte(p.flask)) {
		t.Fatal("dati in chiaro nell'archivio cifrato")
	}
	id, err := age.ParseX25519Identity(c.Privata)
	if err != nil {
		t.Fatal(err)
	}
	in, err := age.Decrypt(bytes.NewReader(cifrato), id)
	if err != nil {
		t.Fatal(err)
	}
	chiaro, err := io.ReadAll(in)
	if err != nil {
		t.Fatal(err)
	}
	if int64(len(chiaro)) != r.Byte {
		t.Errorf("tar di %d byte, Scrivi ne ha dati %d", len(chiaro), r.Byte)
	}
	m, _ := voci(t, chiaro)
	var inv map[string]string
	if err := json.Unmarshal(m["inventory.json"], &inv); err != nil {
		t.Fatal(err)
	}
	if len(inv) != len(m)-2 {
		t.Errorf("inventario di %d voci, tar di %d file oltre ai json", len(inv), len(m)-2)
	}
	for n, c := range m {
		if n == "inventory.json" || n == "snapshot.json" {
			continue
		}
		if inv[n] != sha(c) {
			t.Errorf("%s: SHA-256 diverso dall'inventario", n)
		}
		if r.Inventario[n] != inv[n] {
			t.Errorf("%s: inventario dato diverso da inventory.json", n)
		}
	}
	for _, k := range p.foto {
		if _, ok := m["media/"+k]; !ok {
			t.Errorf("foto %s mancante", k)
		}
	}
	if _, ok := m["data/gestrip.db"]; !ok {
		t.Error("database mancante")
	}
	// La chiave dei backup non entra (D-113), in nessuna voce.
	if _, ok := m["secrets/"+NomeChiaveBackup]; ok {
		t.Error("la chiave dei backup è nell'archivio")
	}
	if bytes.Contains(chiaro, []byte(c.Privata)) || bytes.Contains(chiaro, []byte("AGE-SECRET-KEY-")) {
		t.Error("la chiave privata è dentro il tar")
	}
	// Una chiave diversa non lo apre.
	altra, _ := age.GenerateX25519Identity()
	if _, err := age.Decrypt(bytes.NewReader(cifrato), altra); err == nil {
		t.Error("l'archivio si apre con un'altra chiave")
	}
}

// Un errore a metà (qui un file inatteso in secrets/ comparso dopo la stima, un contesto finito, la
// destinazione che si riempie) toglie il .pending e non lascia archivi.
func TestCifraErroreTogliePending(t *testing.T) {
	casi := []struct {
		nome    string
		prepara func(t *testing.T, p *prova) context.Context
		atteso  error
	}{
		{"file inatteso comparso a metà", func(t *testing.T, p *prova) context.Context {
			dopoCifratura = func() { p.segreto(t, "sconosciuto", "x") }
			return context.Background()
		}, ErrFileInatteso},
		{"contesto finito a metà", func(t *testing.T, p *prova) context.Context {
			ctx, annulla := context.WithCancel(context.Background())
			dopoCifratura = func() { annulla() }
			return ctx
		}, context.Canceled},
		{"scrittura che fallisce", func(t *testing.T, p *prova) context.Context {
			erroreScrittura = errors.New("disco pieno inventato")
			return context.Background()
		}, nil},
	}
	for _, c := range casi {
		t.Run(c.nome, func(t *testing.T) {
			p := nuovaProva(t)
			conChiave(t, p)
			dest := t.TempDir()
			t.Cleanup(func() { dopoCifratura, erroreScrittura = nil, nil })
			var visto bool
			primaDiTogliere = func(pending string) {
				if _, err := os.Lstat(pending); err == nil {
					visto = true
				}
			}
			t.Cleanup(func() { primaDiTogliere = nil })
			ctx := c.prepara(t, p)
			_, err := Cifra(ctx, p.s, dest)
			if err == nil || (c.atteso != nil && !errors.Is(err, c.atteso)) {
				t.Fatalf("errore %v, atteso %v", err, c.atteso)
			}
			if !visto {
				t.Error("il .pending non c'era: l'errore non è arrivato a metà")
			}
			if v := voceDi(t, dest); len(v) != 0 {
				t.Errorf("nella destinazione restano %v", v)
			}
		})
	}
}

// Un archivio con lo stesso nome non si sovrascrive mai, nemmeno se compare durante la scrittura;
// il .pending si toglie e l'archivio che c'era resta com'è.
func TestCifraNonSovrascriveUnArchivio(t *testing.T) {
	p := nuovaProva(t)
	conChiave(t, p)
	dest := t.TempDir()
	nome := filepath.Join(dest, NomeArchivio(p.s.Orologio.Adesso()))
	if err := os.WriteFile(nome, []byte("archivio di prima"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Cifra(context.Background(), p.s, dest); !errors.Is(err, ErrArchivioEsistente) {
		t.Fatalf("errore %v, atteso ErrArchivioEsistente", err)
	}
	if err := os.Remove(nome); err != nil {
		t.Fatal(err)
	}
	primaDiPubblicare = func(finale string) {
		if err := os.WriteFile(finale, []byte("archivio comparso a metà"), 0o600); err != nil {
			t.Error(err)
		}
	}
	defer func() { primaDiPubblicare = nil }()
	if _, err := Cifra(context.Background(), p.s, dest); !errors.Is(err, ErrArchivioEsistente) {
		t.Fatalf("errore %v, atteso ErrArchivioEsistente", err)
	}
	if b, _ := os.ReadFile(nome); string(b) != "archivio comparso a metà" {
		t.Error("l'archivio che c'era è stato sovrascritto")
	}
	if v := voceDi(t, dest); len(v) != 1 {
		t.Errorf("nella destinazione %v, atteso solo l'archivio di prima", v)
	}
}

// Senza spazio nella destinazione (la stima dell'archivio cifrato più il margine) non si scrive niente;
// lo spazio esatto basta; uno spazio che non si legge è un rifiuto.
func TestCifraSpazioDestinazione(t *testing.T) {
	p := nuovaProva(t)
	conChiave(t, p)
	dest := t.TempDir()
	serve, err := spazioDestinazione(p.s)
	if err != nil {
		t.Fatal(err)
	}
	if serve < MargineDestinazione+uint64(len("foto inventata uno")) {
		t.Fatalf("stima %d troppo piccola", serve)
	}
	vecchio := spazioLibero
	defer func() { spazioLibero = vecchio }()
	libero := func(n uint64, e error) {
		spazioLibero = func(dir string) (uint64, error) {
			if dir == dest {
				return n, e
			}
			return 1 << 50, nil
		}
	}
	libero(serve-1, nil)
	if _, err := Cifra(context.Background(), p.s, dest); !errors.Is(err, ErrSpazioDestinazione) {
		t.Fatalf("un byte in meno: %v, atteso ErrSpazioDestinazione", err)
	}
	if v := voceDi(t, dest); len(v) != 0 {
		t.Fatalf("senza spazio nella destinazione restano %v", v)
	}
	libero(0, errors.New("statfs inventato"))
	if _, err := Cifra(context.Background(), p.s, dest); err == nil {
		t.Fatal("spazio non leggibile accettato")
	}
	libero(serve, nil)
	r, err := Cifra(context.Background(), p.s, dest)
	if err != nil {
		t.Fatalf("spazio esatto: %v", err)
	}
	if uint64(r.ByteCifrati) > serve-MargineDestinazione {
		t.Errorf("archivio di %d byte oltre la stima %d", r.ByteCifrati, serve-MargineDestinazione)
	}
}

// Senza chiave, o con la chiave leggibile da altri, il backup non parte e non scrive niente.
func TestCifraSenzaChiaveBuona(t *testing.T) {
	p := nuovaProva(t)
	dest := t.TempDir()
	if err := os.Remove(filepath.Join(p.dir, CartellaSegreti, NomeChiaveBackup)); err != nil {
		t.Fatal(err)
	}
	if _, err := Cifra(context.Background(), p.s, dest); !errors.Is(err, ErrChiaveAssente) {
		t.Fatalf("senza chiave: %v", err)
	}
	if _, err := CreaChiave(p.dir, istanteChiave); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(p.dir, CartellaSegreti, NomeChiaveBackup), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Cifra(context.Background(), p.s, dest); !errors.Is(err, ErrChiavePermessi) {
		t.Fatalf("chiave 0644: %v", err)
	}
	if v := voceDi(t, dest); len(v) != 0 {
		t.Fatalf("restano %v", v)
	}
}

// La destinazione è una cartella che c'è, fuori dalla cartella dei dati (un archivio in media/ o in
// secrets/ fermerebbe i backup dopo, e sullo stesso disco non serve).
func TestCifraDestinazione(t *testing.T) {
	p := nuovaProva(t)
	conChiave(t, p)
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	collegamento := filepath.Join(t.TempDir(), "dati")
	if err := os.Symlink(p.dir, collegamento); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{"", filepath.Join(t.TempDir(), "manca"), file, p.dir,
		filepath.Join(p.dir, "media"), filepath.Join(p.dir, CartellaSegreti), collegamento} {
		if _, err := Cifra(context.Background(), p.s, d); err == nil {
			t.Errorf("destinazione %q accettata", d)
		}
	}
	if v := voceDi(t, filepath.Join(p.dir, "media")); len(v) != 3 { // due foto e il temporaneo .upload-
		t.Errorf("in media/ %v", v)
	}
}

func TestNomeArchivio(t *testing.T) {
	roma := time.FixedZone("CET", 3600)
	if n := NomeArchivio(time.Date(2026, 11, 25, 3, 30, 5, 999, roma)); n != "gestrip-backup-20261125T023005Z.tar.age" {
		t.Errorf("nome %q", n)
	}
}

// Il tar si scrive legato al contesto anche dentro la copia di un file: finito il contesto, la
// scrittura successiva si ferma (difetto 5a della review della 119).
func TestArchivioContestoDentroLaCopia(t *testing.T) {
	p := nuovaProva(t)
	// Una foto grande, che io.Copy scrive a pezzi.
	if _, err := p.s.Archivio.Salva(bytes.Repeat([]byte("x"), 4<<20)); err != nil {
		t.Fatal(err)
	}
	ctx, annulla := context.WithCancel(context.Background())
	w := &scrittoreCheAnnulla{annulla: annulla, dopo: 3}
	_, err := Scrivi(ctx, p.s, w)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("errore %v, atteso context.Canceled", err)
	}
	if w.n > 3 {
		t.Errorf("%d scritture dopo l'annullamento, attesa nessuna", w.n-3)
	}
}

type scrittoreCheAnnulla struct {
	annulla func()
	dopo, n int
}

func (s *scrittoreCheAnnulla) Write(b []byte) (int, error) {
	s.n++
	if s.n == s.dopo {
		s.annulla()
	}
	return len(b), nil
}

// La stima dello spazio della destinazione conta il database, ogni media e ogni segreto con il loro
// pezzo di tar, i byte fissi e i 16 byte di age ogni 64 KiB (review della 120, B1).
func TestStimaDellaDestinazione(t *testing.T) {
	p := nuovaProva(t)
	conChiave(t, p)
	// Una foto da 200 KiB: senza di lei, o senza l'aggiunta di age, la stima cambia.
	grande, err := p.s.Archivio.Salva(bytes.Repeat([]byte("y"), 200<<10))
	if err != nil {
		t.Fatal(err)
	}
	dimensione := func(n string) uint64 {
		fi, err := os.Stat(n)
		if errors.Is(err, os.ErrNotExist) {
			return 0
		}
		if err != nil {
			t.Fatal(err)
		}
		return uint64(fi.Size())
	}
	var tar uint64 = 256<<10 + 3*512 + 512 + 256 // fissi e il database come file
	tar += dimensione(p.s.DB.Percorso()) + dimensione(p.s.DB.Percorso()+"-wal")
	for _, k := range append(slices.Clone(p.foto), grande) {
		tar += dimensione(filepath.Join(p.s.Archivio.Cartella(), k)) + 3*512 + 512 + 256
	}
	for _, n := range []string{"segreto_flask", "segreto_standalone"} {
		tar += dimensione(filepath.Join(p.dir, CartellaSegreti, n)) + 3*512 + 512 + 256
	}
	atteso := tar + (tar/(64<<10)+1)*16 + 64<<20
	got, err := spazioDestinazione(p.s)
	if err != nil {
		t.Fatal(err)
	}
	if got != atteso {
		t.Errorf("stima %d, attesa %d (differenza %d)", got, atteso, int64(got)-int64(atteso))
	}
}

// Un .pending con lo stesso nome (un backup nello stesso secondo, o un resto) non si tronca: il backup
// rifiuta e il file resta com'è (review della 120, B2).
func TestCifraNonToccaUnPendingEsistente(t *testing.T) {
	p := nuovaProva(t)
	conChiave(t, p)
	dest := t.TempDir()
	pending := filepath.Join(dest, NomeArchivio(p.s.Orologio.Adesso())) + SuffissoInCorso
	if err := os.WriteFile(pending, []byte("backup di un altro processo, a metà"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Cifra(context.Background(), p.s, dest); !errors.Is(err, os.ErrExist) {
		t.Fatalf("errore %v, atteso un file esistente", err)
	}
	if b, _ := os.ReadFile(pending); string(b) != "backup di un altro processo, a metà" {
		t.Errorf("il .pending dell'altro è stato toccato: %q", b)
	}
}

// Con la cartella dei dati raggiunta da un collegamento, una destinazione dentro la cartella vera resta
// rifiutata (review della 120, B3).
func TestCifraDatiDietroUnCollegamento(t *testing.T) {
	p := nuovaProva(t)
	conChiave(t, p)
	collegamento := filepath.Join(t.TempDir(), "dati")
	if err := os.Symlink(p.dir, collegamento); err != nil {
		t.Fatal(err)
	}
	d, err := db.Apri(filepath.Join(collegamento, "gestrip.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Chiudi() })
	s := Sorgente{DB: d, Archivio: media.Nuovo(media.CartellaDi(d.Percorso())), Orologio: p.s.Orologio}
	dentro := filepath.Join(p.dir, "copie")
	if err := os.Mkdir(dentro, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := Cifra(context.Background(), s, dentro); !errors.Is(err, ErrDestinazione) {
		t.Fatalf("destinazione dentro i dati veri: %v, atteso ErrDestinazione", err)
	}
	if v := voceDi(t, dentro); len(v) != 0 {
		t.Errorf("restano %v", v)
	}
}

// Il file della chiave si legge fino a maxChiave byte; uno più grande è rifiutato (review della 120, B4).
func TestChiaveTroppoGrande(t *testing.T) {
	dir := t.TempDir()
	c, err := CreaChiave(dir, istanteChiave)
	if err != nil {
		t.Fatal(err)
	}
	riga := c.Privata + "\n"
	scrivi := func(n int) {
		t.Helper()
		commento := "# " + strings.Repeat("x", n-len(riga)-3) + "\n"
		b := commento + riga
		if len(b) != n {
			t.Fatalf("file di %d byte, attesi %d", len(b), n)
		}
		if err := os.WriteFile(FileChiave(dir), []byte(b), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	scrivi(maxChiave)
	if _, err := LeggiChiave(dir); err != nil {
		t.Errorf("file di maxChiave byte rifiutato: %v", err)
	}
	scrivi(maxChiave + 1)
	if _, err := LeggiChiave(dir); err == nil {
		t.Error("file di maxChiave+1 byte accettato")
	}
}

// Un temporaneo di chiave-backup rimasto da un'interruzione ferma il backup con un messaggio che dice
// cosa fare, e non entra nell'archivio (review della 120, 6d).
func TestTemporaneoDellaChiaveRimasto(t *testing.T) {
	p := nuovaProva(t)
	conChiave(t, p)
	p.segreto(t, "."+NomeChiaveBackup+"-123456", "AGE-SECRET-KEY-1FINTA")
	var buf bytes.Buffer
	_, err := Scrivi(context.Background(), p.s, &buf)
	if !errors.Is(err, ErrFileInatteso) || !strings.Contains(err.Error(), "chiave-backup» interrotto") {
		t.Fatalf("errore %v", err)
	}
	if buf.Len() != 0 {
		t.Error("scritto qualcosa prima del rifiuto")
	}
}
