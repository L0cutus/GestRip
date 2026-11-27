package db

import (
	"bytes"
	"context"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"
)

// La chiave del server si crea alla prima partenza e resta la stessa tra i riavvii (altrimenti
// sessioni e moduli aperti non varrebbero più): sta nella tabella segreti, quindi nel backup.
func TestChiaveServerPersistente(t *testing.T) {
	percorso := filepath.Join(t.TempDir(), "gestrip.db")
	leggi := func() []byte {
		d, err := Apri(percorso)
		if err != nil {
			t.Fatal(err)
		}
		defer chiudi(t, d)
		if _, err := d.Migra(ctx, orologio()); err != nil {
			t.Fatal(err)
		}
		k, err := d.ChiaveServer(ctx, orologio())
		if err != nil {
			t.Fatal(err)
		}
		return k
	}
	k1, k2 := leggi(), leggi()
	if len(k1) != LunghezzaChiaveServer || !bytes.Equal(k1, k2) {
		t.Errorf("chiave cambiata tra due avvii: %x %x", k1, k2)
	}
	d, _ := apriProva(t)
	if _, err := d.Migra(ctx, orologio()); err != nil {
		t.Fatal(err)
	}
	k3, err := d.ChiaveServer(ctx, orologio())
	if err != nil || bytes.Equal(k1, k3) {
		t.Errorf("due database con la stessa chiave: %v", err)
	}
	var creato string
	if err := d.Lettura.QueryRow(`SELECT creato_il FROM segreti WHERE nome = 'chiave_server'`).Scan(&creato); err != nil || creato != "2026-10-30T09:15:07.000000Z" {
		t.Errorf("creato_il = %q, %v", creato, err)
	}
	var n int
	if err := d.Lettura.QueryRow(`SELECT count(*) FROM impostazioni`).Scan(&n); err != nil || n != 0 {
		t.Errorf("la chiave è finita in impostazioni: %d righe, %v", n, err)
	}
}

// Più connessioni che chiedono la chiave insieme su un database nuovo ottengono la stessa
// chiave (la transazione di scrittura è immediata).
func TestChiaveServerConcorrente(t *testing.T) {
	percorso := filepath.Join(t.TempDir(), "gestrip.db")
	d0, err := Apri(percorso)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d0.Migra(ctx, orologio()); err != nil {
		t.Fatal(err)
	}
	chiudi(t, d0)
	var mu sync.Mutex
	var chiavi [][]byte
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d, err := Apri(percorso)
			if err != nil {
				t.Error(err)
				return
			}
			defer chiudi(t, d)
			k, err := d.ChiaveServer(ctx, orologio())
			if err != nil {
				t.Error(err)
				return
			}
			mu.Lock()
			chiavi = append(chiavi, k)
			mu.Unlock()
		}()
	}
	wg.Wait()
	for _, k := range chiavi {
		if !bytes.Equal(k, chiavi[0]) {
			t.Fatal("due connessioni hanno creato due chiavi diverse")
		}
	}
}

// La tabella rifiuta una chiave della lunghezza sbagliata o tutta a zero. Se una riga sbagliata
// ci arriva lo stesso (controlli spenti), l'avvio si ferma e la riga resta com'è: rigenerarla in
// silenzio invaliderebbe sessioni e moduli senza dire perché.
func TestChiaveServerRovinata(t *testing.T) {
	d, _ := apriProva(t)
	if _, err := d.Migra(ctx, orologio()); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Scrittura.Exec(`INSERT INTO segreti VALUES ('altro', ?, '2026-10-30 09:15:07')`, bytes.Repeat([]byte{1}, 32)); err == nil {
		t.Error("istante in forma sbagliata accettato in segreti")
	}
	for nome, valore := range map[string][]byte{"corta": make([]byte, 16), "a zero": make([]byte, 32), "lunga": bytes.Repeat([]byte{1}, 33)} {
		if _, err := d.Scrittura.Exec(`INSERT INTO segreti VALUES ('chiave_server', ?, ?)`, valore, istante); err == nil {
			t.Errorf("%s: accettata dalla tabella", nome)
			if _, err := d.Scrittura.Exec(`DELETE FROM segreti`); err != nil {
				t.Fatal(err)
			}
		}
	}
	conn, err := d.Scrittura.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{`PRAGMA ignore_check_constraints = ON`, `INSERT INTO segreti VALUES ('chiave_server', x'0102', '` + istante + `')`, `PRAGMA ignore_check_constraints = OFF`} {
		if _, err := conn.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := d.ChiaveServer(ctx, orologio()); err == nil {
		t.Error("chiave rovinata accettata")
	}
	var v []byte
	if err := d.Lettura.QueryRow(`SELECT valore FROM segreti WHERE nome = 'chiave_server'`).Scan(&v); err != nil || !bytes.Equal(v, []byte{1, 2}) {
		t.Errorf("la riga rovinata è stata cambiata: %x %v", v, err)
	}
	// La transazione è chiusa: l'unica connessione di scrittura è di nuovo libera.
	breve, annulla := context.WithTimeout(ctx, 2*time.Second)
	defer annulla()
	if _, err := d.Scrittura.ExecContext(breve, `DELETE FROM impostazioni WHERE chiave = 'altro'`); err != nil {
		t.Errorf("scrittura bloccata dopo l'errore: %v", err)
	}
}

// RinnovaChiaveServer cambia la chiave (ripristino isolato, macchina di prova, chiave
// compromessa): da lì sessioni e moduli firmati prima non valgono più.
func TestRinnovaChiaveServer(t *testing.T) {
	d, _ := apriProva(t)
	if _, err := d.Migra(ctx, orologio()); err != nil {
		t.Fatal(err)
	}
	k1, err := d.ChiaveServer(ctx, orologio())
	if err != nil {
		t.Fatal(err)
	}
	k2, err := d.RinnovaChiaveServer(ctx, orologio())
	if err != nil || bytes.Equal(k1, k2) || len(k2) != LunghezzaChiaveServer {
		t.Fatalf("rinnovo: %x %v", k2, err)
	}
	k3, err := d.ChiaveServer(ctx, orologio())
	if err != nil || !bytes.Equal(k2, k3) {
		t.Errorf("dopo il rinnovo ChiaveServer dà %x, atteso %x", k3, k2)
	}
}

// Le tabelle con segreti sono elencate per nome: export e copie le escludono. Ogni tabella con
// «segret» nel nome deve essere nell'elenco.
func TestTabelleSegrete(t *testing.T) {
	d, _ := apriProva(t)
	if _, err := d.Migra(ctx, orologio()); err != nil {
		t.Fatal(err)
	}
	rows, err := d.Lettura.Query(`SELECT name FROM sqlite_schema WHERE type = 'table' AND name LIKE '%segret%'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		if !slices.Contains(TabelleSegrete, n) {
			t.Errorf("tabella %s non elencata in TabelleSegrete", n)
		}
	}
	if !slices.Contains(TabelleSegrete, "segreti") {
		t.Error("segreti non è in TabelleSegrete")
	}
}

// Le sottochiavi per scopo sono diverse tra loro, dalla chiave e stabili.
func TestSottoChiave(t *testing.T) {
	k := bytes.Repeat([]byte{7}, LunghezzaChiaveServer)
	a, b := SottoChiave(k, "csrf"), SottoChiave(k, "sessione")
	if len(a) != 32 || bytes.Equal(a, b) || !bytes.Equal(a, SottoChiave(k, "csrf")) || bytes.Equal(a, k) {
		t.Errorf("sottochiavi: %x %x", a, b)
	}
}

// Le sottochiavi di serve (slice 131, D-118 (a)): senza la chiave del backup tutte dalla chiave del server (come
// prima della 131); dopo un ripristino isolato sessione, CSRF e galleria dalla chiave nuova, email e artigiano da
// quella del backup (i link già mandati valgono), e un altro ripristino non la cambia. (Una chiave rovinata non
// entra: la tabella segreti ha il CHECK sulla lunghezza.)
func TestSottoChiaviDiServe(t *testing.T) {
	d, _ := apriProva(t)
	if _, err := d.Migra(ctx, orologio()); err != nil {
		t.Fatal(err)
	}
	vecchia, err := d.ChiaveServer(ctx, orologio())
	if err != nil {
		t.Fatal(err)
	}
	s, err := d.SottoChiaviDiServe(ctx, orologio())
	if err != nil || s.DalBackup {
		t.Fatalf("prima del ripristino: %v %v", s.DalBackup, err)
	}
	for scopo, k := range map[string][]byte{"sessione": s.Sessione, "csrf": s.CSRF, "galleria": s.Galleria, "email": s.Email, "artigiano": s.Artigiano} {
		if !bytes.Equal(k, SottoChiave(vecchia, scopo)) {
			t.Errorf("prima del ripristino %s non dalla chiave del server", scopo)
		}
	}
	controlla := func(quando string) {
		t.Helper()
		nuova, err := d.ChiaveServer(ctx, orologio())
		if err != nil || bytes.Equal(nuova, vecchia) {
			t.Fatalf("%s: chiave non rinnovata: %v", quando, err)
		}
		s, err := d.SottoChiaviDiServe(ctx, orologio())
		if err != nil || !s.DalBackup {
			t.Fatalf("%s: %v %v", quando, s.DalBackup, err)
		}
		for scopo, k := range map[string][]byte{"sessione": s.Sessione, "csrf": s.CSRF, "galleria": s.Galleria} {
			if !bytes.Equal(k, SottoChiave(nuova, scopo)) || bytes.Equal(k, SottoChiave(vecchia, scopo)) {
				t.Errorf("%s: %s non dalla chiave nuova", quando, scopo)
			}
		}
		for scopo, k := range map[string][]byte{"email": s.Email, "artigiano": s.Artigiano} {
			if !bytes.Equal(k, SottoChiave(vecchia, scopo)) {
				t.Errorf("%s: %s non dalla chiave del backup", quando, scopo)
			}
		}
	}
	if _, err := IsolaRipristino(ctx, d, orologio(), Isolamento{Archivio: "a.tar.age"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	controlla("dopo il ripristino")
	// Un backup della copia ripristinato di nuovo: la chiave del backup resta la prima (INSERT OR IGNORE).
	if _, err := d.Scrittura.Exec(`DELETE FROM impostazioni WHERE chiave = ?`, ChiaveIsolamento); err != nil {
		t.Fatal(err)
	}
	if _, err := IsolaRipristino(ctx, d, orologio(), Isolamento{Archivio: "b.tar.age"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	controlla("dopo il secondo ripristino")
}
