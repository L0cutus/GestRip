package backup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
)

// La rotazione sulla chiavetta finta (slice 123, D-114): file veri in una cartella temporanea, registro vero
// in un database inventato. Gli archivi sono byte inventati: la rotazione non li apre.

// ruotaProva è una chiavetta finta con il registro di nuovaProva.
type ruotaProva struct {
	*scena
	d *db.DB
}

func nuovaRuotaProva(t *testing.T) *ruotaProva {
	t.Helper()
	p := nuovaProva(t)
	return &ruotaProva{scena: nuovaScena(t, "chiavetta"), d: p.s.DB}
}

// archivio scrive sulla chiavetta un archivio finto catturato a t e, se registra, la sua riga (ok o
// fallita) con destinazione chiavetta.
func (r *ruotaProva) archivio(t *testing.T, at time.Time, registra, ok bool) Risultato {
	t.Helper()
	f := filepath.Join(r.chiavetta, NomeArchivio(at))
	b := []byte("archivio inventato " + at.Format(time.RFC3339))
	if err := os.WriteFile(f, b, 0o600); err != nil {
		t.Fatal(err)
	}
	h := sha256.Sum256(b)
	ris := Risultato{File: f, ByteCifrati: int64(len(b)), SHA256: hex.EncodeToString(h[:])}
	if registra {
		r.riga(t, ris, at, ok)
	}
	return ris
}

func (r *ruotaProva) riga(t *testing.T, ris Risultato, at time.Time, ok bool) {
	t.Helper()
	v := db.VerificaBackup{Riuscita: ok, Destinazione: db.DestinazioneChiavetta, Archivio: ris.File, SHA256: ris.SHA256,
		Byte: ris.ByteCifrati, CatturatoIl: at, VerificatoIl: at.Add(time.Minute), Conteggi: map[string]int64{"buste": 1}}
	if !ok {
		v.Errore, v.Conteggi = "verifica finta fallita", nil
	}
	if err := db.RegistraVerificaBackup(context.Background(), r.d, v); err != nil {
		t.Fatal(err)
	}
}

func (r *ruotaProva) apri(t *testing.T) *Destinazione {
	t.Helper()
	d, err := ApriChiavetta(r.chiavetta, r.dati)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Chiudi() })
	return d
}

// archiviRimasti dà i giorni di Roma degli archivi (nome esatto, file normale) rimasti sulla chiavetta.
func (r *ruotaProva) archiviRimasti(t *testing.T) []string {
	t.Helper()
	var v []string
	for _, n := range voceDi(t, r.chiavetta) {
		at, ok := istanteDelNome(reArchivio, n)
		fi, err := os.Lstat(filepath.Join(r.chiavetta, n))
		if ok && err == nil && fi.Mode().IsRegular() {
			v = append(v, at.In(clock.Roma).Format("2006-01-02 15:04"))
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(v)))
	return v
}

// Voce OPS-28, in parte (senza segno «inventario:», vedi TestRotazioneScelta); OPS-16 in parte.
// Il giro vero: tre mesi di backup ogni notte sulla chiavetta, il nuovo appena registrato. Restano i 7 + 4 di
// D-114 e tutto quello che non è nostro: file con altri nomi, .pending, collegamenti e cartelle con il nome di
// un archivio (e il file a cui punta il collegamento), un archivio con il nostro nome che il registro non
// conosce o con byte diversi da quelli registrati. Un archivio la cui verifica era fallita si toglie.
func TestRuotaChiavetta(t *testing.T) {
	r := nuovaRuotaProva(t)
	notti := ogniNotte(alle(2026, 9, 1, 0, 0), alle(2026, 11, 25, 0, 0))
	var nuovo Risultato
	for _, at := range notti {
		nuovo = r.archivio(t, at, true, true)
	}
	r.archivio(t, alle(2026, 10, 1, 3, 0), true, false)  // verifica fallita, vecchia: si toglie
	r.archivio(t, alle(2026, 11, 24, 3, 0), true, false) // verifica fallita, recente: resta (revisione)
	r.archivio(t, alle(2026, 10, 2, 3, 0), false, false) // senza riga: resta
	diverso := r.archivio(t, alle(2026, 10, 3, 3, 0), false, false)
	diverso.ByteCifrati++
	r.riga(t, diverso, alle(2026, 10, 3, 3, 0), true) // riga con altri byte: resta
	fuori := filepath.Join(t.TempDir(), "file di un altro")
	if err := os.WriteFile(fuori, []byte("da non toccare"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := NomeArchivio(alle(2026, 10, 4, 3, 0))
	if err := os.Symlink(fuori, filepath.Join(r.chiavetta, link)); err != nil {
		t.Fatal(err)
	}
	cart := NomeArchivio(alle(2026, 10, 5, 3, 0))
	if err := os.Mkdir(filepath.Join(r.chiavetta, cart), 0o700); err != nil {
		t.Fatal(err)
	}
	estranei := []string{"foto.jpg", NomeArchivio(alle(2026, 10, 6, 3, 0)) + SuffissoInCorso, "gestrip-backup-20261007T030000Z.tar.age.bak",
		"gestrip-backup-20261340T030000Z.tar.age"}
	for _, n := range estranei {
		if err := os.WriteFile(filepath.Join(r.chiavetta, n), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	d := r.apri(t)
	e, err := d.Ruota(context.Background(), r.d, nuovo, notti[len(notti)-1].Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	attesi := "2026-11-25 02:30,2026-11-24 03:00,2026-11-24 02:30,2026-11-23 02:30,2026-11-22 02:30,2026-11-21 02:30,2026-11-20 02:30,2026-11-19 02:30," +
		"2026-11-15 02:30,2026-11-08 02:30,2026-11-01 02:30,2026-10-25 02:30,2026-10-03 03:00,2026-10-02 03:00"
	if v := strings.Join(r.archiviRimasti(t), ","); v != attesi {
		t.Errorf("rimasti\n%s\nattesi\n%s", v, attesi)
	}
	if len(e.Tolti) != len(notti)-11+1 || len(e.Tenuti) != 12 || len(e.Lasciati) != 4 {
		t.Errorf("tolti %d, tenuti %d, lasciati %v", len(e.Tolti), len(e.Tenuti), e.Lasciati)
	}
	for _, n := range append(estranei, link, cart) {
		if _, err := os.Lstat(filepath.Join(r.chiavetta, n)); err != nil {
			t.Errorf("%s: %v", n, err)
		}
	}
	if b, err := os.ReadFile(fuori); err != nil || string(b) != "da non toccare" {
		t.Errorf("file fuori: %q %v", b, err)
	}
	// Un secondo giro non toglie altro.
	if e, err := d.Ruota(context.Background(), r.d, nuovo, notti[len(notti)-1].Add(time.Hour)); err != nil || len(e.Tolti) != 0 {
		t.Errorf("secondo giro: %v %v", e.Tolti, err)
	}
}

// dieciNotti scrive dieci notti di backup registrati (16-25/11): la rotazione toglierebbe 16, 17 e 18.
func dieciNotti(t *testing.T, r *ruotaProva) (Risultato, time.Time) {
	t.Helper()
	var nuovo Risultato
	var at time.Time
	for _, at = range ogniNotte(alle(2026, 11, 16, 0, 0), alle(2026, 11, 25, 0, 0)) {
		nuovo = r.archivio(t, at, true, true)
	}
	return nuovo, at.Add(time.Hour)
}

// Voce OPS-28, in parte (senza segno «inventario:», vedi TestRotazioneScelta).
// Ogni dubbio è «non cancello niente»: il registro che non dice che il nuovo archivio è l'ultimo verificato
// sulla chiavetta (o lo dice fallito), il file del nuovo con altri byte, un archivio nel futuro o più nuovo
// del nuovo, una destinazione che non è una chiavetta, il ctx finito.
func TestRuotaNonCancellaConUnDubbio(t *testing.T) {
	casi := []struct {
		nome   string
		cambia func(t *testing.T, r *ruotaProva, nuovo *Risultato, adesso *time.Time) *Destinazione
	}{
		{"nel registro l'ultimo verificato è un altro", func(t *testing.T, r *ruotaProva, nuovo *Risultato, adesso *time.Time) *Destinazione {
			altro := *nuovo
			altro.File = filepath.Join(r.sopra, "altrove", filepath.Base(nuovo.File))
			r.riga(t, altro, adesso.Add(-time.Minute), true)
			return nil
		}},
		{"nuovo con un altro SHA-256", func(t *testing.T, r *ruotaProva, nuovo *Risultato, adesso *time.Time) *Destinazione {
			nuovo.SHA256 = strings.Repeat("0", 64)
			return nil
		}},
		{"nuovo con la verifica fallita", func(t *testing.T, r *ruotaProva, nuovo *Risultato, adesso *time.Time) *Destinazione {
			*nuovo = r.archivio(t, adesso.Add(-time.Minute), true, false)
			return nil
		}},
		{"nuovo mai registrato", func(t *testing.T, r *ruotaProva, nuovo *Risultato, adesso *time.Time) *Destinazione {
			*nuovo = r.archivio(t, adesso.Add(-time.Minute), false, false)
			return nil
		}},
		{"il file del nuovo con altri byte", func(t *testing.T, r *ruotaProva, nuovo *Risultato, adesso *time.Time) *Destinazione {
			if err := os.WriteFile(nuovo.File, []byte("toccato"), 0o600); err != nil {
				t.Fatal(err)
			}
			return nil
		}},
		{"il nuovo tolto", func(t *testing.T, r *ruotaProva, nuovo *Risultato, adesso *time.Time) *Destinazione {
			if err := os.Remove(nuovo.File); err != nil {
				t.Fatal(err)
			}
			return nil
		}},
		{"un archivio registrato nel futuro", func(t *testing.T, r *ruotaProva, nuovo *Risultato, adesso *time.Time) *Destinazione {
			r.archivio(t, adesso.Add(time.Hour), true, false)
			return nil
		}},
		{"un archivio più nuovo del nuovo", func(t *testing.T, r *ruotaProva, nuovo *Risultato, adesso *time.Time) *Destinazione {
			r.archivio(t, adesso.Add(-time.Second), true, false)
			return nil
		}},
		{"una cartella qualsiasi", func(t *testing.T, r *ruotaProva, nuovo *Risultato, adesso *time.Time) *Destinazione {
			d, err := BloccaDestinazione(r.chiavetta, r.dati)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = d.Chiudi() })
			return d
		}},
		{"il nuovo fuori dalla chiavetta", func(t *testing.T, r *ruotaProva, nuovo *Risultato, adesso *time.Time) *Destinazione {
			nuovo.File = filepath.Join(r.sopra, filepath.Base(nuovo.File))
			return nil
		}},
	}
	for _, c := range casi {
		t.Run(c.nome, func(t *testing.T) {
			r := nuovaRuotaProva(t)
			nuovo, adesso := dieciNotti(t, r)
			d := c.cambia(t, r, &nuovo, &adesso)
			if d == nil {
				d = r.apri(t)
			}
			prima := r.archiviRimasti(t)
			e, err := d.Ruota(context.Background(), r.d, nuovo, adesso)
			if !errors.Is(err, ErrRotazione) || len(e.Tolti) != 0 {
				t.Errorf("errore %v, tolti %v", err, e.Tolti)
			}
			if dopo := r.archiviRimasti(t); strings.Join(dopo, ",") != strings.Join(prima, ",") {
				t.Errorf("cancellati: prima %v, dopo %v", prima, dopo)
			}
		})
	}
	t.Run("ctx finito", func(t *testing.T) {
		r := nuovaRuotaProva(t)
		nuovo, adesso := dieciNotti(t, r)
		ctx, annulla := context.WithCancel(context.Background())
		annulla()
		e, err := r.apri(t).Ruota(ctx, r.d, nuovo, adesso)
		if err == nil || len(e.Tolti) != 0 || len(r.archiviRimasti(t)) != 10 {
			t.Errorf("errore %v, tolti %v", err, e.Tolti)
		}
	})
	// Revisione della 123: un archivio nel futuro **senza riga** (una copia a mano da una macchina con l'orologio
	// avanti) non ferma la rotazione: si lascia, e 16, 17 e 18 si tolgono.
	t.Run("nel futuro senza riga", func(t *testing.T) {
		r := nuovaRuotaProva(t)
		nuovo, adesso := dieciNotti(t, r)
		futuro := r.archivio(t, adesso.Add(48*time.Hour), false, false)
		e, err := r.apri(t).Ruota(context.Background(), r.d, nuovo, adesso)
		if err != nil || len(e.Tolti) != 3 || len(e.Lasciati) != 1 || e.Lasciati[0] != filepath.Base(futuro.File) {
			t.Errorf("%+v %v", e, err)
		}
	})
	// Revisione della 123 (R7): un archivio registrato su un altro dispositivo (un montaggio dentro la chiavetta)
	// si lascia.
	t.Run("altro dispositivo", func(t *testing.T) {
		r := nuovaRuotaProva(t)
		nuovo, adesso := dieciNotti(t, r)
		altro := NomeArchivio(alle(2026, 11, 16, 2, 30))
		vecchio := guardaVoce
		t.Cleanup(func() { guardaVoce = vecchio })
		guardaVoce = func(d *os.File, n string) (voceCartella, error) {
			v, err := vecchio(d, n)
			if n == altro {
				v.id.dev++
			}
			return v, err
		}
		e, err := r.apri(t).Ruota(context.Background(), r.d, nuovo, adesso)
		if err != nil || len(e.Tolti) != 2 || len(e.Lasciati) != 1 || e.Lasciati[0] != altro {
			t.Errorf("%+v %v", e, err)
		}
	})
	// Il controllo funziona: senza dubbi la stessa scena toglie 16, 17 e 18.
	r := nuovaRuotaProva(t)
	nuovo, adesso := dieciNotti(t, r)
	if e, err := r.apri(t).Ruota(context.Background(), r.d, nuovo, adesso); err != nil || len(e.Tolti) != 3 {
		t.Fatalf("senza dubbi: %v %v", e.Tolti, err)
	}
}

// Un errore di cancellazione, o un archivio cambiato fra l'elenco e la cancellazione (un altro file o altri
// byte allo stesso nome), fermano la rotazione: quello già tolto resta tolto, il resto resta.
func TestRuotaSiFerma(t *testing.T) {
	t.Run("errore di cancellazione", func(t *testing.T) {
		r := nuovaRuotaProva(t)
		nuovo, adesso := dieciNotti(t, r)
		vecchio := togliVoce
		t.Cleanup(func() { togliVoce = vecchio })
		n := 0
		togliVoce = func(d *os.File, nome string) error {
			if n++; n == 2 {
				return &fs.PathError{Op: "unlinkat", Path: nome, Err: errors.New("EIO finto")}
			}
			return vecchio(d, nome)
		}
		e, err := r.apri(t).Ruota(context.Background(), r.d, nuovo, adesso)
		if !errors.Is(err, ErrRotazione) || len(e.Tolti) != 1 || len(r.archiviRimasti(t)) != 9 {
			t.Errorf("errore %v, tolti %v, rimasti %v", err, e.Tolti, r.archiviRimasti(t))
		}
	})
	for nome, cambia := range map[string]func(p string) error{
		"altri byte": func(p string) error {
			return os.WriteFile(p, []byte("cambiato durante la rotazione, più lungo"), 0o600)
		},
		"altro file con gli stessi byte": func(p string) error {
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			if err := os.Remove(p); err != nil {
				return err
			}
			return os.WriteFile(p, b, 0o600)
		},
		"collegamento al suo posto": func(p string) error {
			if err := os.Remove(p); err != nil {
				return err
			}
			return os.Symlink("/dev/null", p)
		},
	} {
		t.Run(nome, func(t *testing.T) {
			r := nuovaRuotaProva(t)
			nuovo, adesso := dieciNotti(t, r)
			vecchio := primaDiTogliereArchivio
			t.Cleanup(func() { primaDiTogliereArchivio = vecchio })
			var cambiato string
			primaDiTogliereArchivio = func(n string) {
				if cambiato == "" {
					cambiato = n
					if err := cambia(filepath.Join(r.chiavetta, n)); err != nil {
						t.Fatal(err)
					}
				}
			}
			e, err := r.apri(t).Ruota(context.Background(), r.d, nuovo, adesso)
			if !errors.Is(err, ErrRotazione) || len(e.Tolti) != 0 {
				t.Errorf("errore %v, tolti %v", err, e.Tolti)
			}
			if _, err := os.Lstat(filepath.Join(r.chiavetta, cambiato)); err != nil {
				t.Errorf("il file cambiato è stato tolto: %v", err)
			}
		})
	}
}

// Voce OPS-16, in parte (senza segno «inventario:», vedi TestControllaChiavetta).
// Il giro di gestrip backup -chiavetta con i pezzi veri, sulla chiavetta finta, per dieci notti: Cifra,
// VerificaArchivio, destinazione da registrare («chiavetta»), riga 'ok' nel registro, rotazione. Alla fine
// sulla chiavetta ci sono le dieci notti meno quelle che D-114 non tiene, e nessun .pending.
func TestGiroCompletoSullaChiavetta(t *testing.T) {
	p := nuovaProva(t)
	conChiave(t, p)
	s := nuovaScena(t, "chiavetta")
	// La cartella dei dati della scena è quella della prova.
	dati, err := percorsoVero(p.dir)
	if err != nil {
		t.Fatal(err)
	}
	s.sis.dev[dati] = 1
	o := clock.NuovoFinto(alle(2026, 11, 16, 2, 30))
	p.s.Orologio = o
	ctx := context.Background()
	for range 10 {
		d, err := ApriChiavetta(s.chiavetta, dati)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := d.PulisciPending(); err != nil {
			t.Fatal(err)
		}
		r, err := Cifra(ctx, p.s, d.Cartella)
		if err != nil {
			t.Fatal(err)
		}
		v, err := VerificaArchivio(ctx, p.dir, r.File)
		if err != nil || v.SHA256 != r.SHA256 {
			t.Fatalf("verifica: %v", err)
		}
		dest, err := d.DestinazioneDaRegistrare(r.File)
		if err != nil || dest != db.DestinazioneChiavetta {
			t.Fatalf("destinazione %s %v", dest, err)
		}
		catturato, err := time.Parse(db.FormatoIstante, v.CatturatoIl)
		if err != nil {
			t.Fatal(err)
		}
		if err := db.RegistraVerificaBackup(ctx, p.s.DB, db.VerificaBackup{Riuscita: true, Destinazione: dest, Archivio: r.File,
			SHA256: v.SHA256, Byte: v.ByteCifrati, CatturatoIl: catturato, VerificatoIl: o.Adesso(), Conteggi: v.Conteggi}); err != nil {
			t.Fatal(err)
		}
		if _, err := d.Ruota(ctx, p.s.DB, r, o.Adesso()); err != nil {
			t.Fatal(err)
		}
		if err := d.Chiudi(); err != nil {
			t.Fatal(err)
		}
		o.Sposta(24 * time.Hour)
	}
	r := &ruotaProva{scena: s, d: p.s.DB}
	attesi := "2026-11-25 02:30,2026-11-24 02:30,2026-11-23 02:30,2026-11-22 02:30,2026-11-21 02:30,2026-11-20 02:30,2026-11-19 02:30"
	if v := strings.Join(r.archiviRimasti(t), ","); v != attesi {
		t.Errorf("rimasti %s", v)
	}
	if v := voceDi(t, s.chiavetta); len(v) != 7 {
		t.Errorf("sulla chiavetta %v", v)
	}
}
