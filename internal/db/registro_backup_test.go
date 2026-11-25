package db

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Gli istanti delle prove del registro: l'import, prima e dopo.
var (
	importProva = time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
	primaProva  = importProva.Add(-time.Hour)
	dopoProva   = importProva.Add(time.Hour)
)

// registroProva è un database migrato con la memoria del ricontrollo vuota e l'orologio del registro vero.
func registroProva(t *testing.T) *DB {
	t.Helper()
	d := utentiMigrati(t)
	memoriaImport.dimentica()
	t.Cleanup(memoriaImport.dimentica)
	return d
}

// archivioProva scrive un archivio finto (byte inventati) in una cartella temporanea e dà percorso,
// SHA-256 e byte.
func archivioProva(t *testing.T, contenuto string) (string, string, int64) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "gestrip-backup-20261120T110000Z.tar.age")
	if err := os.WriteFile(p, []byte(contenuto), 0o600); err != nil {
		t.Fatal(err)
	}
	s := sha256.Sum256([]byte(contenuto))
	return p, hex.EncodeToString(s[:]), int64(len(contenuto))
}

func verificaProva(file, sha string, n int64, catturato time.Time) VerificaBackup {
	return VerificaBackup{Riuscita: true, Destinazione: DestinazioneChiavetta, Archivio: file, SHA256: sha, Byte: n,
		CatturatoIl: catturato, VerificatoIl: catturato.Add(time.Minute), Conteggi: map[string]int64{"buste": 3}}
}

// bloccatoPreparato è quello che fanno i chiamanti: PreparaBloccoImport fuori, poi BloccatoDopoImport; gli
// errori di tutti e due insieme.
func bloccatoPreparato(c context.Context, q Interrogatore) (bool, error) {
	errP := PreparaBloccoImport(c, q)
	b, err := BloccatoDopoImport(c, q)
	return b, errors.Join(errP, err)
}

func registra(t *testing.T, d *DB, v VerificaBackup) {
	t.Helper()
	if err := RegistraVerificaBackup(ctx, d, v); err != nil {
		t.Fatal(err)
	}
}

func segnaImport(t *testing.T, d *DB) {
	t.Helper()
	if _, err := d.Scrittura.Exec(`INSERT INTO import_standalone (id, modalita, sha256_sorgente, byte_sorgente, importato_il)
		VALUES (1, 'sintetico', zeroblob(32), 4096, ?)`, importProva.Format(FormatoIstante)); err != nil {
		t.Fatal(err)
	}
}

// dopoDi chiede BackupVerificatoDopo(importProva) e vuole che un sì non porti mai un errore.
func dopoDi(t *testing.T, d *DB) (bool, error) {
	t.Helper()
	ok, err := BackupVerificatoDopo(ctx, d.Lettura, importProva)
	if ok && err != nil {
		t.Fatalf("sì con un errore: %v", err)
	}
	return ok, err
}

// Voce OPS-17, in parte (senza segno «inventario:»: vedi la ricevuta della 122).
// D-11, risposta F: conta solo un backup verificato catturato strettamente dopo l'istante, con
// l'archivio al suo posto e lo stesso SHA-256. Prima, nello stesso istante o senza righe: no.
func TestBackupVerificatoDopoIstante(t *testing.T) {
	d := registroProva(t)
	if ok, err := dopoDi(t, d); ok || err != nil {
		t.Fatalf("registro vuoto: %v %v", ok, err)
	}
	f, sha, n := archivioProva(t, "archivio di prova catturato prima")
	registra(t, d, verificaProva(f, sha, n, primaProva))
	if ok, err := dopoDi(t, d); ok || err != nil {
		t.Fatalf("backup prima dell'istante: %v %v", ok, err)
	}
	registra(t, d, verificaProva(f, sha, n, importProva))
	if ok, err := dopoDi(t, d); ok || err != nil {
		t.Fatalf("backup nello stesso istante: %v %v", ok, err)
	}
	registra(t, d, verificaProva(f, sha, n, importProva.Add(time.Microsecond)))
	if ok, err := dopoDi(t, d); !ok || err != nil {
		t.Fatalf("backup un microsecondo dopo: %v %v", ok, err)
	}
	// Anche dentro una transazione (come la chiamano conservazione e scadenza dei dettati).
	tx, err := d.Scrittura.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	if ok, err := BackupVerificatoDopo(ctx, tx, importProva); !ok || err != nil {
		t.Fatalf("in una transazione: %v %v", ok, err)
	}
}

// Voce OPS-17, in parte (senza segno «inventario:»: vedi la ricevuta della 122).
// L'archivio dell'ultimo backup verificato deve esserci ancora, identico: tolto è «no» senza errore; con
// altri byte della stessa lunghezza, più corto, più lungo, una cartella o un collegamento (anche a un file
// identico) al suo posto è «no» con ErrArchivioCambiato.
func TestBackupVerificatoArchivioCambiato(t *testing.T) {
	const contenuto = "archivio di prova dopo l'import"
	for nome, caso := range map[string]struct {
		cambia func(t *testing.T, f string)
		errore bool
	}{
		"tolto": {func(t *testing.T, f string) { togli(t, f) }, false},
		"stessa lunghezza, un byte diverso": {func(t *testing.T, f string) {
			scrivi(t, f, "Archivio di prova dopo l'import")
		}, true},
		"più corto": {func(t *testing.T, f string) { scrivi(t, f, contenuto[1:]) }, true},
		"più lungo": {func(t *testing.T, f string) { scrivi(t, f, contenuto+"x") }, true},
		"vuoto":     {func(t *testing.T, f string) { scrivi(t, f, "") }, true},
		"cartella": {func(t *testing.T, f string) {
			togli(t, f)
			if err := os.Mkdir(f, 0o700); err != nil {
				t.Fatal(err)
			}
		}, true},
		"collegamento a una copia identica": {func(t *testing.T, f string) {
			copia := f + ".copia"
			scrivi(t, copia, contenuto)
			togli(t, f)
			if err := os.Symlink(copia, f); err != nil {
				t.Fatal(err)
			}
		}, true},
	} {
		t.Run(nome, func(t *testing.T) {
			d := registroProva(t)
			segnaImport(t, d)
			f, sha, n := archivioProva(t, contenuto)
			registra(t, d, verificaProva(f, sha, n, dopoProva))
			if ok, err := dopoDi(t, d); !ok || err != nil {
				t.Fatalf("prima del cambio: %v %v", ok, err)
			}
			if b, err := bloccatoPreparato(ctx, d.Lettura); b || err != nil {
				t.Fatalf("prima del cambio bloccato: %v %v", b, err)
			}
			// La memoria del ricontrollo non deve nascondere il cambio: ogni caso cambia l'ora di modifica
			// o l'inode, o toglie il file.
			caso.cambia(t, f)
			ok, err := dopoDi(t, d)
			if ok || (err != nil) != caso.errore || (caso.errore && !errors.Is(err, ErrArchivioCambiato)) {
				t.Errorf("BackupVerificatoDopo: %v %v", ok, err)
			}
			b, err := bloccatoPreparato(ctx, d.Lettura)
			if !b || (err != nil) != caso.errore {
				t.Errorf("BloccatoDopoImport: %v %v", b, err)
			}
		})
	}
}

// Voce OPS-17, in parte (senza segno «inventario:»: vedi la ricevuta della 122).
// Un collegamento simbolico al posto dell'archivio, con la stessa lunghezza dell'archivio registrato (Lstat
// dà la lunghezza del percorso a cui punta) e verso una copia identica: «no» con ErrArchivioCambiato.
func TestBackupVerificatoCollegamentoDellaStessaLunghezza(t *testing.T) {
	d := registroProva(t)
	dir := t.TempDir()
	copia := filepath.Join(dir, "copia")
	contenuto := strings.Repeat("a", len(copia))
	scrivi(t, copia, contenuto)
	f := filepath.Join(dir, "gestrip-backup-20261120T110000Z.tar.age")
	scrivi(t, f, contenuto)
	s := sha256.Sum256([]byte(contenuto))
	registra(t, d, verificaProva(f, hex.EncodeToString(s[:]), int64(len(contenuto)), dopoProva))
	if ok, err := dopoDi(t, d); !ok || err != nil {
		t.Fatalf("prima: %v %v", ok, err)
	}
	togli(t, f)
	if err := os.Symlink(copia, f); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Lstat(f); err != nil || fi.Size() != int64(len(contenuto)) {
		t.Fatalf("collegamento di %v byte (%v)", fi.Size(), err)
	}
	if ok, err := dopoDi(t, d); ok || !errors.Is(err, ErrArchivioCambiato) {
		t.Errorf("collegamento: %v %v", ok, err)
	}
}

// Voce OPS-17, in parte (senza segno «inventario:»: vedi la ricevuta della 122).
// Conta solo l'ultima verifica riuscita su una destinazione locale: le verifiche fallite (registrate, con
// il motivo), la copia SFTP e la cartella di gestrip backup -cartella no; un archivio più vecchio ancora buono non salva un ultimo archivio tolto.
func TestBackupVerificatoSoloLUltimoRiuscito(t *testing.T) {
	d := registroProva(t)
	f, sha, n := archivioProva(t, "archivio della verifica fallita")
	v := verificaProva(f, sha, n, dopoProva)
	v.Riuscita, v.Errore, v.Conteggi = false, "backup: verifica: SHA-256 di media/x diverso", nil
	registra(t, d, v)
	var esito, errore string
	if err := d.Lettura.QueryRow(`SELECT esito, errore FROM verifiche_backup`).Scan(&esito, &errore); err != nil ||
		esito != "fallita" || !strings.Contains(errore, "SHA-256") {
		t.Fatalf("verifica fallita registrata come %q %q (%v)", esito, errore, err)
	}
	if ok, err := dopoDi(t, d); ok || err != nil {
		t.Fatalf("verifica fallita contata: %v %v", ok, err)
	}
	s := verificaProva(f, sha, n, dopoProva)
	s.Destinazione = DestinazioneSFTP
	registra(t, d, s)
	if ok, err := dopoDi(t, d); ok || err != nil {
		t.Fatalf("copia SFTP contata da sola: %v %v", ok, err)
	}
	// Risposta F (Stefano): la cartella di gestrip backup -cartella non sblocca, anche se più nuova.
	c := verificaProva(f, sha, n, dopoProva)
	c.Destinazione = DestinazioneCartella
	registra(t, d, c)
	if ok, err := dopoDi(t, d); ok || err != nil {
		t.Fatalf("cartella contata: %v %v", ok, err)
	}
	registra(t, d, verificaProva(f, sha, n, dopoProva))
	if ok, err := dopoDi(t, d); !ok || err != nil {
		t.Fatalf("chiavetta non contata: %v %v", ok, err)
	}
	// Un backup più nuovo il cui archivio è sparito: «no», anche se quello di prima c'è ancora.
	registra(t, d, verificaProva(filepath.Join(t.TempDir(), "sparito.tar.age"), sha, n, dopoProva.Add(time.Hour)))
	if ok, err := dopoDi(t, d); ok || err != nil {
		t.Fatalf("ultimo archivio sparito: %v %v", ok, err)
	}
}

// Voce OPS-17, in parte (senza segno «inventario:»: vedi la ricevuta della 122).
// Istanti che non tornano con l'orologio: cattura o verifica nel futuro (anche di una verifica fallita o di
// una riga SFTP), verifica prima della cattura, istante T zero: sempre «no» con ErrOrologioBackup, e
// BloccatoDopoImport resta bloccato. Un istante T nel futuro è «no» senza errore.
func TestBackupVerificatoOrologio(t *testing.T) {
	futuro := time.Now().Add(48 * time.Hour)
	for nome, cambia := range map[string]func(v *VerificaBackup){
		"cattura nel futuro":           func(v *VerificaBackup) { v.CatturatoIl, v.VerificatoIl = futuro, futuro },
		"verifica nel futuro":          func(v *VerificaBackup) { v.VerificatoIl = futuro },
		"verifica prima della cattura": func(v *VerificaBackup) { v.VerificatoIl = v.CatturatoIl.Add(-time.Second) },
	} {
		t.Run(nome, func(t *testing.T) {
			d := registroProva(t)
			segnaImport(t, d)
			f, sha, n := archivioProva(t, "archivio con l'orologio storto")
			v := verificaProva(f, sha, n, dopoProva)
			cambia(&v)
			registra(t, d, v)
			if ok, err := dopoDi(t, d); ok || !errors.Is(err, ErrOrologioBackup) {
				t.Errorf("BackupVerificatoDopo: %v %v", ok, err)
			}
			if b, err := bloccatoPreparato(ctx, d.Lettura); !b || !errors.Is(err, ErrOrologioBackup) {
				t.Errorf("BloccatoDopoImport: %v %v", b, err)
			}
		})
	}
	t.Run("riga fallita o SFTP nel futuro accanto a una buona", func(t *testing.T) {
		for _, dest := range []string{"fallita", DestinazioneSFTP} {
			d := registroProva(t)
			f, sha, n := archivioProva(t, "archivio buono")
			registra(t, d, verificaProva(f, sha, n, dopoProva))
			v := verificaProva(f, sha, n, futuro)
			if dest == "fallita" {
				v.Riuscita, v.Errore = false, "verifica non passata"
			} else {
				v.Destinazione = dest
			}
			registra(t, d, v)
			if ok, err := dopoDi(t, d); ok || !errors.Is(err, ErrOrologioBackup) {
				t.Errorf("%s: %v %v", dest, ok, err)
			}
		}
	})
	t.Run("istante T nel futuro o zero", func(t *testing.T) {
		d := registroProva(t)
		f, sha, n := archivioProva(t, "archivio buono")
		registra(t, d, verificaProva(f, sha, n, dopoProva))
		if ok, err := BackupVerificatoDopo(ctx, d.Lettura, futuro); ok || err != nil {
			t.Errorf("T nel futuro: %v %v", ok, err)
		}
		if ok, err := BackupVerificatoDopo(ctx, d.Lettura, time.Time{}); ok || !errors.Is(err, ErrOrologioBackup) {
			t.Errorf("T zero: %v %v", ok, err)
		}
	})
	t.Run("orologio del registro tornato indietro", func(t *testing.T) {
		d := registroProva(t)
		f, sha, n := archivioProva(t, "archivio buono")
		registra(t, d, verificaProva(f, sha, n, dopoProva))
		vecchio := adessoRegistro
		t.Cleanup(func() { adessoRegistro = vecchio })
		adessoRegistro = func() time.Time { return dopoProva.Add(30 * time.Second) } // prima della verifica
		if ok, err := dopoDi(t, d); ok || !errors.Is(err, ErrOrologioBackup) {
			t.Errorf("%v %v", ok, err)
		}
	})
}

// Voce OPS-17, in parte (senza segno «inventario:»: vedi la ricevuta della 122).
// Registro illeggibile (tabella tolta, istante rovinato a mano) o lettura interrotta: «no» con l'errore, e
// BloccatoDopoImport resta bloccato. Anche il segno dell'import illeggibile blocca.
func TestBackupVerificatoRegistroGuasto(t *testing.T) {
	t.Run("tabella tolta", func(t *testing.T) {
		d := registroProva(t)
		segnaImport(t, d)
		esegui(t, d, `DROP TABLE verifiche_backup`)
		if ok, err := dopoDi(t, d); ok || err == nil {
			t.Errorf("BackupVerificatoDopo: %v %v", ok, err)
		}
		if b, err := bloccatoPreparato(ctx, d.Lettura); !b || err == nil {
			t.Errorf("BloccatoDopoImport: %v %v", b, err)
		}
	})
	t.Run("istante rovinato", func(t *testing.T) {
		d := registroProva(t)
		segnaImport(t, d)
		f, sha, n := archivioProva(t, "archivio buono")
		registra(t, d, verificaProva(f, sha, n, dopoProva))
		// Il CHECK vuole la forma: la si aggira togliendo e rifacendo la tabella senza CHECK.
		esegui(t, d, `CREATE TABLE copia AS SELECT * FROM verifiche_backup`)
		esegui(t, d, `DROP TABLE verifiche_backup`)
		esegui(t, d, `ALTER TABLE copia RENAME TO verifiche_backup`)
		esegui(t, d, `UPDATE verifiche_backup SET catturato_il = '2026-11-20T11:00:00Z'`)
		if b, err := bloccatoPreparato(ctx, d.Lettura); !b || err == nil {
			t.Errorf("BloccatoDopoImport: %v %v", b, err)
		}
	})
	t.Run("contesto finito durante il ricalcolo", func(t *testing.T) {
		registroProva(t)
		f, sha, n := archivioProva(t, "archivio buono")
		c, ferma := context.WithCancel(ctx)
		ferma()
		r := rigaVerifica{id: 1, archivio: f, sha256: sha, byte: n}
		if ok, err := ricontrolla(c, r, time.Now(), &memoriaImport, riempiMemoria); ok || !errors.Is(err, context.Canceled) {
			t.Errorf("ricontrolla: %v %v", ok, err)
		}
	})
	t.Run("segno dell'import illeggibile", func(t *testing.T) {
		d := registroProva(t)
		esegui(t, d, `DROP TABLE import_standalone`)
		if b, err := bloccatoPreparato(ctx, d.Lettura); !b || err == nil {
			t.Errorf("BloccatoDopoImport: %v %v", b, err)
		}
	})
}

// Voce OPS-17, in parte (senza segno «inventario:»: vedi la ricevuta della 122); ADM-17.
// BloccatoDopoImport: senza import mai bloccato; con l'import bloccato finché non arriva un backup
// verificato catturato dopo l'import, con l'archivio presente e identico; un backup di prima dell'import
// non basta.
func TestBloccatoDopoImportEBackup(t *testing.T) {
	d := registroProva(t)
	if b, err := bloccatoPreparato(ctx, d.Lettura); b || err != nil {
		t.Fatalf("senza import: %v %v", b, err)
	}
	segnaImport(t, d)
	if b, err := bloccatoPreparato(ctx, d.Lettura); !b || err != nil {
		t.Fatalf("import senza backup: %v %v", b, err)
	}
	f, sha, n := archivioProva(t, "archivio di prima dell'import")
	registra(t, d, verificaProva(f, sha, n, primaProva))
	if b, err := bloccatoPreparato(ctx, d.Lettura); !b || err != nil {
		t.Fatalf("backup di prima dell'import: %v %v", b, err)
	}
	g, shaG, nG := archivioProva(t, "archivio di dopo l'import")
	registra(t, d, verificaProva(g, shaG, nG, dopoProva))
	if b, err := bloccatoPreparato(ctx, d.Lettura); b || err != nil {
		t.Fatalf("backup verificato dopo l'import: %v %v", b, err)
	}
}

// Voce OPS-17, in parte (senza segno «inventario:»: vedi la ricevuta della 122).
// BloccatoDopoImport non rilegge mai l'archivio (lo chiamano dentro transazioni e lucchetti): senza la
// memoria di PreparaBloccoImport dice bloccato e il contatore dei ricalcoli non si muove, anche in una
// transazione; PreparaBloccoImport ricalcola una volta, poi la memoria basta.
func TestBloccatoDopoImportNonRicalcolaDentro(t *testing.T) {
	d := registroProva(t)
	segnaImport(t, d)
	f, sha, n := archivioProva(t, "archivio di dopo l'import")
	registra(t, d, verificaProva(f, sha, n, dopoProva))
	prima := ricalcoli.Load()
	tx, err := d.Scrittura.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if b, err := BloccatoDopoImport(ctx, tx); !b || err != nil {
		t.Errorf("senza memoria, in transazione: %v %v", b, err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if ricalcoli.Load() != prima {
		t.Fatal("BloccatoDopoImport ha riletto l'archivio")
	}
	if err := PreparaBloccoImport(ctx, d.Lettura); err != nil {
		t.Fatal(err)
	}
	if ricalcoli.Load() != prima+1 {
		t.Fatalf("PreparaBloccoImport: %d ricalcoli", ricalcoli.Load()-prima)
	}
	for range 3 {
		if err := PreparaBloccoImport(ctx, d.Lettura); err != nil {
			t.Fatal(err)
		}
		if b, err := BloccatoDopoImport(ctx, d.Lettura); b || err != nil {
			t.Fatalf("con la memoria: %v %v", b, err)
		}
	}
	if ricalcoli.Load() != prima+1 {
		t.Errorf("con la memoria fresca: %d ricalcoli", ricalcoli.Load()-prima)
	}
}

// Voce OPS-17, in parte (senza segno «inventario:»: vedi la ricevuta della 122).
// PreparaBloccoImport rinnova la memoria quando le manca meno di AnticipoRicontrollo alla scadenza: un secondo
// prima no, un secondo dopo sì.
func TestPreparaBloccoImportRinnovaInAnticipo(t *testing.T) {
	d := registroProva(t)
	segnaImport(t, d)
	f, sha, n := archivioProva(t, "archivio di dopo l'import")
	registra(t, d, verificaProva(f, sha, n, dopoProva))
	adesso := time.Now()
	vecchio := adessoRegistro
	t.Cleanup(func() { adessoRegistro = vecchio })
	adessoRegistro = func() time.Time { return adesso }
	if err := PreparaBloccoImport(ctx, d.Lettura); err != nil {
		t.Fatal(err)
	}
	inizio, prima := adesso, ricalcoli.Load()
	adesso = inizio.Add(ValiditaRicontrollo - AnticipoRicontrollo - time.Second)
	if err := PreparaBloccoImport(ctx, d.Lettura); err != nil || ricalcoli.Load() != prima {
		t.Errorf("un secondo prima dell'anticipo: %v, %d ricalcoli", err, ricalcoli.Load()-prima)
	}
	adesso = inizio.Add(ValiditaRicontrollo - AnticipoRicontrollo + time.Second)
	if err := PreparaBloccoImport(ctx, d.Lettura); err != nil || ricalcoli.Load() != prima+1 {
		t.Errorf("un secondo dopo l'anticipo: %v, %d ricalcoli", err, ricalcoli.Load()-prima)
	}
}

// Voce OPS-17, in parte (senza segno «inventario:»: vedi la ricevuta della 122).
// Due PreparaBloccoImport insieme (la pulizia e la conservazione all'avvio di serve) fanno un solo ricalcolo:
// il secondo aspetta il primo e usa la sua memoria.
func TestPreparaBloccoImportUnSoloRicalcolo(t *testing.T) {
	d := registroProva(t)
	segnaImport(t, d)
	f, sha, n := archivioProva(t, "archivio di dopo l'import")
	registra(t, d, verificaProva(f, sha, n, dopoProva))
	dentro, via := make(chan struct{}), make(chan struct{})
	duranteRicalcolo = func() {
		select {
		case dentro <- struct{}{}:
		default:
		}
		<-via
	}
	t.Cleanup(func() { duranteRicalcolo = nil })
	prima := ricalcoli.Load()
	errori := make(chan error, 2)
	go func() { errori <- PreparaBloccoImport(ctx, d.Lettura) }()
	<-dentro // il primo sta ricalcolando
	go func() { errori <- PreparaBloccoImport(ctx, d.Lettura) }()
	time.Sleep(100 * time.Millisecond) // il secondo arriva e aspetta
	close(via)
	for range 2 {
		if err := <-errori; err != nil {
			t.Fatal(err)
		}
	}
	if got := ricalcoli.Load() - prima; got != 1 {
		t.Errorf("%d ricalcoli, atteso 1", got)
	}
}

// Voce OPS-17, in parte (senza segno «inventario:»: vedi la ricevuta della 122).
// Seconda revisione: chi aspetta il ricalcolo di un altro rilegge l'ora dopo l'attesa. Il secondo prepara legge
// l'ora prima che il primo finisca (più vecchia della memoria che il primo riempirà); se la usasse dopo l'attesa
// vedrebbe la memoria «nel futuro» e ricalcolerebbe di nuovo. Atteso: un solo ricalcolo.
func TestPreparaBloccoImportOraRilettaDopoLAttesa(t *testing.T) {
	d := registroProva(t)
	segnaImport(t, d)
	f, sha, n := archivioProva(t, "archivio di dopo l'import")
	registra(t, d, verificaProva(f, sha, n, dopoProva))
	base := time.Now()
	var ora atomic.Int64
	ora.Store(base.Add(time.Second).UnixNano())
	vecchio := adessoRegistro
	t.Cleanup(func() { adessoRegistro = vecchio })
	adessoRegistro = func() time.Time { return time.Unix(0, ora.Load()) }
	dentro, via := make(chan struct{}), make(chan struct{})
	duranteRicalcolo = func() {
		select {
		case dentro <- struct{}{}:
		default:
		}
		<-via
	}
	t.Cleanup(func() { duranteRicalcolo = nil })
	prima := ricalcoli.Load()
	errori := make(chan error, 2)
	go func() { errori <- PreparaBloccoImport(ctx, d.Lettura) }()
	<-dentro // il primo ricalcola, con la memoria che avrà l'ora base+1s
	ora.Store(base.UnixNano())
	go func() { errori <- PreparaBloccoImport(ctx, d.Lettura) }() // legge base, poi aspetta
	time.Sleep(100 * time.Millisecond)
	ora.Store(base.Add(2 * time.Second).UnixNano()) // l'ora vera è andata avanti
	close(via)
	for range 2 {
		if err := <-errori; err != nil {
			t.Fatal(err)
		}
	}
	if got := ricalcoli.Load() - prima; got != 1 {
		t.Errorf("%d ricalcoli, atteso 1", got)
	}
}

// Voce OPS-17, in parte (senza segno «inventario:»: vedi la ricevuta della 122).
// Seconda revisione (M3): un rinnovo nella finestra 23-24 h che fallisce (contenuto cambiato di nascosto) svuota la
// memoria: BloccatoDopoImport dice bloccato subito, non fino alla scadenza delle 24 h.
func TestPreparaBloccoImportRinnovoFallitoBlocca(t *testing.T) {
	d := registroProva(t)
	segnaImport(t, d)
	f, sha, n := archivioProva(t, "archivio di dopo l'import")
	registra(t, d, verificaProva(f, sha, n, dopoProva))
	adesso := time.Now()
	vecchio := adessoRegistro
	t.Cleanup(func() { adessoRegistro = vecchio })
	adessoRegistro = func() time.Time { return adesso }
	if err := PreparaBloccoImport(ctx, d.Lettura); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(f)
	if err != nil {
		t.Fatal(err)
	}
	scrivi(t, f, "ARCHIVIO di dopo l'import")
	if err := os.Chtimes(f, fi.ModTime(), fi.ModTime()); err != nil {
		t.Fatal(err)
	}
	adesso = adesso.Add(ValiditaRicontrollo - AnticipoRicontrollo/2) // dentro la finestra di rinnovo
	if b, err := BloccatoDopoImport(ctx, d.Lettura); b || err != nil {
		t.Fatalf("prima del rinnovo la memoria vale ancora: %v %v", b, err)
	}
	if err := PreparaBloccoImport(ctx, d.Lettura); !errors.Is(err, ErrArchivioCambiato) {
		t.Fatalf("rinnovo: %v", err)
	}
	if b, err := BloccatoDopoImport(ctx, d.Lettura); !b || err != nil {
		t.Errorf("dopo un rinnovo fallito: %v %v", b, err)
	}
}

// Voce OPS-17, in parte (senza segno «inventario:»: vedi la ricevuta della 122).
// La memoria vale ValiditaRicontrollo: PreparaBloccoImport la rinnova un'ora prima (AnticipoRicontrollo);
// se nessuno la rinnova, dentro la transazione BloccatoDopoImport dice bloccato senza rileggere. Un contenuto
// cambiato di nascosto (stessa ora di modifica, stesso inode) si vede al rinnovo; BackupVerificatoDopo,
// senza memoria, lo vede subito. Un orologio tornato indietro rispetto alla memoria la rende non valida.
func TestBloccatoDopoImportMemoriaScaduta(t *testing.T) {
	d := registroProva(t)
	segnaImport(t, d)
	f, sha, n := archivioProva(t, "archivio di dopo l'import")
	registra(t, d, verificaProva(f, sha, n, dopoProva))
	adesso := time.Now()
	vecchio := adessoRegistro
	t.Cleanup(func() { adessoRegistro = vecchio })
	adessoRegistro = func() time.Time { return adesso }
	if err := PreparaBloccoImport(ctx, d.Lettura); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(f)
	if err != nil {
		t.Fatal(err)
	}
	scrivi(t, f, "ARCHIVIO di dopo l'import")
	if err := os.Chtimes(f, fi.ModTime(), fi.ModTime()); err != nil {
		t.Fatal(err)
	}
	if ok, err := dopoDi(t, d); ok || !errors.Is(err, ErrArchivioCambiato) {
		t.Errorf("BackupVerificatoDopo senza memoria: %v %v", ok, err)
	}
	prima := ricalcoli.Load()
	adesso = adesso.Add(ValiditaRicontrollo - AnticipoRicontrollo - time.Second)
	if err := PreparaBloccoImport(ctx, d.Lettura); err != nil || ricalcoli.Load() != prima {
		t.Errorf("prima dell'anticipo: %v, %d ricalcoli", err, ricalcoli.Load()-prima)
	}
	adesso = adesso.Add(AnticipoRicontrollo) // a un secondo dalla scadenza
	if b, err := BloccatoDopoImport(ctx, d.Lettura); b || err != nil {
		t.Errorf("dentro la validità: %v %v", b, err)
	}
	adesso = adesso.Add(time.Second)
	if b, err := BloccatoDopoImport(ctx, d.Lettura); !b || err != nil || ricalcoli.Load() != prima {
		t.Errorf("scaduta, dentro: %v %v, %d ricalcoli", b, err, ricalcoli.Load()-prima)
	}
	if err := PreparaBloccoImport(ctx, d.Lettura); !errors.Is(err, ErrArchivioCambiato) {
		t.Errorf("rinnovo con il contenuto cambiato: %v", err)
	}
	if b, _ := BloccatoDopoImport(ctx, d.Lettura); !b {
		t.Error("sbloccato dopo un rinnovo fallito")
	}
	scrivi(t, f, "archivio di dopo l'import")
	if err := PreparaBloccoImport(ctx, d.Lettura); err != nil {
		t.Fatal(err)
	}
	if b, err := BloccatoDopoImport(ctx, d.Lettura); b || err != nil {
		t.Errorf("contenuto rimesso: %v %v", b, err)
	}
	adesso = adesso.Add(-time.Second)
	if b, err := BloccatoDopoImport(ctx, d.Lettura); !b || err != nil {
		t.Errorf("orologio indietro: %v %v", b, err)
	}
}

// Voce OPS-17, in parte (senza segno «inventario:»: vedi la ricevuta della 122).
// Revisione R5/R11: un archivio sostituito da un altro file con gli stessi byte e la stessa ora di modifica
// (come cp -p: inode diverso) non usa la memoria, e nemmeno una riga del registro cambiata (riga nuova per lo
// stesso archivio). Se il file nuovo è uguale si ricalcola e si sblocca; se è diverso si blocca.
func TestBloccatoDopoImportFileSostituito(t *testing.T) {
	d := registroProva(t)
	segnaImport(t, d)
	f, sha, n := archivioProva(t, "archivio di dopo l'import")
	registra(t, d, verificaProva(f, sha, n, dopoProva))
	if err := PreparaBloccoImport(ctx, d.Lettura); err != nil {
		t.Fatal(err)
	}
	sostituisci := func(contenuto string) {
		t.Helper()
		fi, err := os.Stat(f)
		if err != nil {
			t.Fatal(err)
		}
		nuovo := f + ".nuovo"
		scrivi(t, nuovo, contenuto)
		if err := os.Chtimes(nuovo, fi.ModTime(), fi.ModTime()); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(nuovo, f); err != nil {
			t.Fatal(err)
		}
	}
	sostituisci("ARCHIVIO di dopo l'import")
	prima := ricalcoli.Load()
	if b, err := BloccatoDopoImport(ctx, d.Lettura); !b || err != nil || ricalcoli.Load() != prima {
		t.Errorf("file sostituito, dentro: %v %v", b, err)
	}
	if err := PreparaBloccoImport(ctx, d.Lettura); !errors.Is(err, ErrArchivioCambiato) || ricalcoli.Load() != prima+1 {
		t.Errorf("file sostituito, prepara: %v", err)
	}
	sostituisci("archivio di dopo l'import")
	if err := PreparaBloccoImport(ctx, d.Lettura); err != nil {
		t.Fatal(err)
	}
	if b, err := BloccatoDopoImport(ctx, d.Lettura); b || err != nil {
		t.Errorf("file uguale sostituito: %v %v", b, err)
	}
	// Una riga nuova (stesso archivio, cattura dopo): la memoria è della riga vecchia.
	registra(t, d, verificaProva(f, sha, n, dopoProva.Add(time.Minute)))
	if b, err := BloccatoDopoImport(ctx, d.Lettura); !b || err != nil {
		t.Errorf("riga nuova senza rinnovo: %v %v", b, err)
	}
	if err := PreparaBloccoImport(ctx, d.Lettura); err != nil {
		t.Fatal(err)
	}
	if b, err := BloccatoDopoImport(ctx, d.Lettura); b || err != nil {
		t.Errorf("riga nuova dopo il rinnovo: %v %v", b, err)
	}
}

// Voce OPS-17, in parte (senza segno «inventario:»: vedi la ricevuta della 122).
// Revisione R1: un importato_il che passa il GLOB dello schema ma non è un istante (mese 19) blocca, con
// errore, sia in PreparaBloccoImport sia in BloccatoDopoImport.
func TestBloccatoDopoImportIstanteDellImportIlleggibile(t *testing.T) {
	d := registroProva(t)
	esegui(t, d, `INSERT INTO import_standalone (id, modalita, sha256_sorgente, byte_sorgente, importato_il)
		VALUES (1, 'sintetico', zeroblob(32), 4096, '2026-19-01T10:00:00.000000Z')`)
	f, sha, n := archivioProva(t, "archivio di dopo l'import")
	registra(t, d, verificaProva(f, sha, n, dopoProva))
	if err := PreparaBloccoImport(ctx, d.Lettura); err == nil {
		t.Error("prepara senza errore")
	}
	if b, err := BloccatoDopoImport(ctx, d.Lettura); !b || err == nil {
		t.Errorf("BloccatoDopoImport: %v %v", b, err)
	}
}

// Le regole della riga: RegistraVerificaBackup rifiuta i campi sbagliati prima di scrivere; i CHECK dello
// schema rifiutano le stesse cose scritte a mano.
func TestRegistraVerificaBackupRegole(t *testing.T) {
	d := registroProva(t)
	f, sha, n := archivioProva(t, "archivio")
	for nome, cambia := range map[string]func(v *VerificaBackup){
		"destinazione sconosciuta":    func(v *VerificaBackup) { v.Destinazione = "nuvola" },
		"percorso relativo":           func(v *VerificaBackup) { v.Archivio = "backup/x.tar.age" },
		"percorso non pulito":         func(v *VerificaBackup) { v.Archivio = "/tmp/../x.tar.age" },
		"sha maiuscolo":               func(v *VerificaBackup) { v.SHA256 = strings.ToUpper(sha) },
		"sha corto":                   func(v *VerificaBackup) { v.SHA256 = sha[:63] },
		"byte zero":                   func(v *VerificaBackup) { v.Byte = 0 },
		"senza cattura":               func(v *VerificaBackup) { v.CatturatoIl = time.Time{} },
		"senza verifica":              func(v *VerificaBackup) { v.VerificatoIl = time.Time{} },
		"riuscita senza conteggi":     func(v *VerificaBackup) { v.Conteggi = nil },
		"fallita senza motivo":        func(v *VerificaBackup) { v.Riuscita = false },
		"fallita con motivo di spazi": func(v *VerificaBackup) { v.Riuscita, v.Errore = false, "  " },
	} {
		v := verificaProva(f, sha, n, dopoProva)
		cambia(&v)
		if err := RegistraVerificaBackup(ctx, d, v); !errors.Is(err, ErrRegistroBackup) {
			t.Errorf("%s: %v", nome, err)
		}
	}
	v := verificaProva(f, sha, n, dopoProva)
	v.Riuscita, v.Errore = false, strings.Repeat("è", 2000)
	registra(t, d, v)
	var e string
	if err := d.Lettura.QueryRow(`SELECT errore FROM verifiche_backup`).Scan(&e); err != nil || len(e) > maxErroreRegistro || !strings.HasPrefix(e, "è") {
		t.Errorf("motivo lungo: %d byte (%v)", len(e), err)
	}
	const riga = `INSERT INTO verifiche_backup (esito, destinazione, archivio, sha256, byte, catturato_il, verificato_il, conteggi, errore)
		VALUES (?, ?, ?, ?, ?, '2026-11-20T11:00:00.000000Z', '2026-11-20T11:01:00.000000Z', ?, ?)`
	for nome, a := range map[string][]any{
		"esito":             {"forse", "cartella", f, sha, n, "{}", nil},
		"destinazione":      {"ok", "nuvola", f, sha, n, "{}", nil},
		"percorso relativo": {"ok", "cartella", "x.tar.age", sha, n, "{}", nil},
		"sha maiuscolo":     {"ok", "cartella", f, strings.ToUpper(sha), n, "{}", nil},
		"byte":              {"ok", "cartella", f, sha, 0, "{}", nil},
		"conteggi lista":    {"ok", "cartella", f, sha, n, "[]", nil},
		"conteggi non JSON": {"ok", "cartella", f, sha, n, "{", nil},
		"ok con errore":     {"ok", "cartella", f, sha, n, "{}", "x"},
		"fallita senza":     {"fallita", "cartella", f, sha, n, "{}", nil},
		"fallita con vuoto": {"fallita", "cartella", f, sha, n, "{}", ""},
	} {
		if _, err := d.Scrittura.Exec(riga, a...); err == nil {
			t.Errorf("%s accettato dallo schema", nome)
		}
	}
	if _, err := d.Scrittura.Exec(riga, "ok", "cartella", f, sha, n, "{}", nil); err != nil {
		t.Errorf("riga giusta rifiutata: %v", err)
	}
	if _, err := d.Scrittura.Exec(strings.Replace(riga, "'2026-11-20T11:00:00.000000Z'", "'2026-11-20 11:00:00'", 1),
		"ok", "cartella", f, sha, n, "{}", nil); err == nil {
		t.Error("istante nella forma sbagliata accettato")
	}
}

func esegui(t *testing.T, d *DB, q string, a ...any) {
	t.Helper()
	if _, err := d.Scrittura.Exec(q, a...); err != nil {
		t.Fatal(err)
	}
}

func scrivi(t *testing.T, f, s string) {
	t.Helper()
	if err := os.WriteFile(f, []byte(s), 0o600); err != nil {
		t.Fatal(err)
	}
}

func togli(t *testing.T, f string) {
	t.Helper()
	if err := os.Remove(f); err != nil {
		t.Fatal(err)
	}
}
