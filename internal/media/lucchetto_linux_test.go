package media

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Il lucchetto delle cancellazioni (slice 120): vale fra due archivi della stessa cartella come fra due
// processi (gestrip backup accanto a serve), e ogni attesa finisce con il contesto (difetto 5a della
// review della 119: lo stop di serve non resta appeso a un backup).

// tieniLettura tiene SenzaCancellazioni su a finché non si chiude libera; torna quando il lucchetto è
// preso. Il canale dà l'errore di SenzaCancellazioni.
func tieniLettura(t *testing.T, a *Archivio) (libera chan struct{}, fatto chan error) {
	t.Helper()
	libera, fatto = make(chan struct{}), make(chan error, 1)
	preso := make(chan struct{})
	go func() {
		fatto <- a.SenzaCancellazioni(context.Background(), func() error {
			close(preso)
			<-libera
			return nil
		})
	}()
	select {
	case <-preso:
	case err := <-fatto:
		t.Fatalf("SenzaCancellazioni non è partita: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("SenzaCancellazioni non prende il lucchetto")
	}
	return libera, fatto
}

// Un backup di un altro processo (qui un secondo Archivio sulla stessa cartella: un RWMutex diverso, lo
// stesso file del flock) ferma la coda delle cancellazioni di serve finché legge.
func TestCancellazioniAspettanoUnAltroProcesso(t *testing.T) {
	b := bancoPulizia(t)
	k := b.oggetto(t, "foto inventata in coda")
	b.accoda(t, k)
	altro := Nuovo(b.media) // l'archivio del processo «gestrip backup»
	libera, fatto := tieniLettura(t, altro)
	pulita := make(chan error, 1)
	go func() { pulita <- b.p.CancellaInCoda(context.Background()) }()
	select {
	case err := <-pulita:
		t.Fatalf("la coda è passata durante il backup dell'altro processo (%v)", err)
	case <-time.After(300 * time.Millisecond):
	}
	if !presente(t, filepath.Join(b.media, k)) {
		t.Fatal("la foto in coda è sparita durante il backup")
	}
	close(libera)
	if err := <-fatto; err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-pulita:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("finito il backup, la coda non riparte")
	}
	if presente(t, filepath.Join(b.media, k)) {
		t.Error("finito il backup, la foto in coda resta")
	}
	// Il file del lucchetto sta accanto a media/, non dentro (il backup lì accetta solo oggetti), ed è 0600.
	fi, err := os.Lstat(filepath.Join(filepath.Dir(b.media), LucchettoCancellazioni))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("permessi del lucchetto %v, attesi 0600", fi.Mode().Perm())
	}
	if presente(t, filepath.Join(b.media, LucchettoCancellazioni)) {
		t.Error("il lucchetto sta dentro media/")
	}
}

// Due backup insieme (due letture) non si aspettano.
func TestDueBackupInsieme(t *testing.T) {
	b := bancoPulizia(t)
	libera, fatto := tieniLettura(t, Nuovo(b.media))
	defer rilascia(t, libera, fatto)
	ctx, annulla := context.WithTimeout(context.Background(), 5*time.Second)
	defer annulla()
	if err := b.a.SenzaCancellazioni(ctx, func() error { return nil }); err != nil {
		t.Fatalf("il secondo backup aspetta il primo: %v", err)
	}
}

// Lo stop di serve (il contesto della pulizia finito) interrompe l'attesa di un backup, dello stesso
// processo o di un altro, e la coda non si tocca.
func TestAttesaDellaPuliziaSiInterrompe(t *testing.T) {
	for _, c := range []struct {
		nome   string
		stesso bool
	}{{"stesso processo", true}, {"altro processo", false}} {
		t.Run(c.nome, func(t *testing.T) {
			b := bancoPulizia(t)
			k := b.oggetto(t, "foto inventata in coda")
			b.accoda(t, k)
			a := Nuovo(b.media)
			if c.stesso {
				a = b.a
			}
			libera, fatto := tieniLettura(t, a)
			defer rilascia(t, libera, fatto)
			ctx, annulla := context.WithCancel(context.Background())
			pulita := make(chan error, 1)
			go func() { pulita <- b.p.CancellaInCoda(ctx) }()
			time.Sleep(100 * time.Millisecond)
			annulla()
			select {
			case err := <-pulita:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("errore %v, atteso context.Canceled", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("con il contesto finito la pulizia resta appesa al backup")
			}
			if !presente(t, filepath.Join(b.media, k)) {
				t.Error("la foto in coda è sparita")
			}
			if got := b.coda(t); len(got) != 1 {
				t.Errorf("coda %v, attesa intatta", got)
			}
			// Anche la variante per chi tiene già il lucchetto dei salvataggi.
			ctx2, annulla2 := context.WithTimeout(context.Background(), 200*time.Millisecond)
			defer annulla2()
			err := b.a.ConLucchetto(func() error { return b.p.CancellaInCodaConLucchetto(ctx2) })
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Errorf("CancellaInCodaConLucchetto: %v, atteso context.DeadlineExceeded", err)
			}
		})
	}
}

// Un backup che aspetta la coda (che sta cancellando) si interrompe con il suo contesto, senza leggere.
func TestAttesaDelBackupSiInterrompe(t *testing.T) {
	b := bancoPulizia(t)
	libera, preso, fatto := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	altro := Nuovo(b.media) // la coda di serve, in un altro processo
	go func() {
		fatto <- altro.conCancellazioni(context.Background(), true, func() error {
			close(preso)
			<-libera
			return nil
		})
	}()
	<-preso
	defer rilascia(t, libera, fatto)
	ctx, annulla := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer annulla()
	letto := false
	err := b.a.SenzaCancellazioni(ctx, func() error { letto = true; return nil })
	if !errors.Is(err, context.DeadlineExceeded) || letto {
		t.Fatalf("errore %v, letto %v: atteso context.DeadlineExceeded senza leggere", err, letto)
	}
	// Con il contesto già finito non si prova nemmeno.
	ctx3, annulla3 := context.WithCancel(context.Background())
	annulla3()
	if err := Nuovo(t.TempDir()).SenzaCancellazioni(ctx3, func() error { letto = true; return nil }); !errors.Is(err, context.Canceled) || letto {
		t.Fatalf("contesto finito: errore %v, letto %v", err, letto)
	}
}

// rilascia chiude libera e controlla l'esito della lettura tenuta da tieniLettura o dalla coda.
func rilascia(t *testing.T, libera chan struct{}, fatto chan error) {
	t.Helper()
	close(libera)
	if err := <-fatto; err != nil {
		t.Error(err)
	}
}

// Un file del lucchetto che non si apre (qui una cartella al suo posto; sull'apparecchio un file di root
// lasciato da un «gestrip backup» lanciato come root) ferma la coda con ErrLucchetto, e il registro
// dice «lucchetto», non «database» (review della 120).
func TestLucchettoCheNonSiApre(t *testing.T) {
	b := bancoPulizia(t)
	if err := os.Mkdir(filepath.Join(filepath.Dir(b.media), LucchettoCancellazioni), 0o700); err != nil {
		t.Fatal(err)
	}
	err := b.p.CancellaInCoda(context.Background())
	if !errors.Is(err, ErrLucchetto) || causa(err) != "lucchetto" {
		t.Fatalf("errore %v, causa %q", err, causa(err))
	}
}
