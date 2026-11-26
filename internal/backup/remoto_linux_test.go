package backup

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
	"github.com/L0cutus/gestrip/internal/sftpfinto"
)

// La copia remota (slice 128) contro il server SFTP finto (internal/sftpfinto: SSH e SFTP veri su una net.Pipe,
// niente rete). La chiavetta è quella finta della 123, il registro è vero. Gli archivi della chiavetta sono byte
// inventati (la copia non li apre: ne ricalcola solo lo SHA-256 mentre li manda); il giro intero con la cifratura
// vera è in TestGiroConCopiaRemota.

const cartellaRemota = "/backup"

// copiaProva è una chiavetta finta con il registro, un server finto con la cartella dei backup e la chiave
// dell'apparecchio già autorizzata.
type copiaProva struct {
	*ruotaProva
	srv *sftpfinto.Server
	rm  *Remoto
}

func nuovaCopiaProva(t *testing.T) *copiaProva {
	t.Helper()
	r := nuovaRuotaProva(t)
	srv := sftpfinto.Nuovo(t)
	if err := os.Mkdir(srv.Locale(cartellaRemota), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, _, err := CreaChiaveSFTP(r.dati); err != nil {
		t.Fatal(err)
	}
	firma, err := LeggiChiaveSFTP(r.dati)
	if err != nil {
		t.Fatal(err)
	}
	srv.Autorizza(firma.PublicKey())
	rm := &Remoto{Dati: r.dati, Componi: srv.Componi, Config: ConfigRemota{Host: "storage.invalid", Porta: 23, Utente: "u1",
		Cartella: cartellaRemota, TipoChiaveServer: ssh.KeyAlgoED25519, Impronta: srv.Impronta()}}
	return &copiaProva{ruotaProva: r, srv: srv, rm: rm}
}

// archivioGrande scrive sulla chiavetta un archivio inventato di n byte catturato a at, registrato come
// «chiavetta» 'ok'.
func (c *copiaProva) archivioGrande(t *testing.T, at time.Time, n int) (Risultato, Verificato) {
	t.Helper()
	b := bytes.Repeat([]byte("archivio "+at.Format(time.RFC3339)+" "), n/30+1)[:n]
	f := filepath.Join(c.chiavetta, NomeArchivio(at))
	if err := os.WriteFile(f, b, 0o600); err != nil {
		t.Fatal(err)
	}
	h := sha256.Sum256(b)
	ris := Risultato{File: f, ByteCifrati: int64(n), SHA256: hex.EncodeToString(h[:])}
	ris.CatturatoIl = at.UTC().Format(db.FormatoIstante)
	c.riga(t, ris, at, true)
	return ris, Verificato{File: f, SHA256: ris.SHA256, ByteCifrati: ris.ByteCifrati, Fotografia: Fotografia{Conteggi: map[string]int64{"buste": 1}}}
}

func (c *copiaProva) copia(t *testing.T, ctx context.Context, ris Risultato, v Verificato, adesso time.Time) (EsitoCopiaRemota, error) {
	t.Helper()
	dest := c.apri(t)
	defer func() { _ = dest.Chiudi() }()
	return c.rm.Copia(ctx, c.d, dest, ris, v, clock.NuovoFinto(adesso))
}

// sulServer sono i nomi nella cartella remota.
func (c *copiaProva) sulServer(t *testing.T) []string {
	t.Helper()
	return voceDi(t, c.srv.Locale(cartellaRemota))
}

func codice(err error) string { return CodiceRemoto(err) }

// Voce OPS-16, in parte (la voce chiude con TestGiroConCopiaRemota).
// La copia riuscita: sul server l'archivio con gli stessi byte, nessun .pending, una riga «sftp» 'ok' nel registro con
// SHA-256 e byte della chiavetta; la riga remota non sblocca niente (risposta F): l'ultimo verificato sulla chiavetta
// resta quello della chiavetta, e un registro con sole righe «sftp» non sblocca le cancellazioni.
func TestCopiaRemotaRiuscita(t *testing.T) {
	c := nuovaCopiaProva(t)
	ctx := context.Background()
	at := alle(2026, 11, 26, 2, 30)
	ris, v := c.archivioGrande(t, at, 200_000)
	e, err := c.copia(t, ctx, ris, v, at.Add(10*time.Minute))
	if err != nil || !e.Registrato || e.Archivio != path.Join(cartellaRemota, NomeArchivio(at)) {
		t.Fatalf("copia: %+v %v", e, err)
	}
	locale, _ := os.ReadFile(ris.File)
	remoto, err := os.ReadFile(c.srv.Locale(e.Archivio))
	if err != nil || !bytes.Equal(locale, remoto) {
		t.Fatalf("byte sul server diversi: %v", err)
	}
	if v := c.sulServer(t); !slices.Equal(v, []string{NomeArchivio(at)}) {
		t.Errorf("sul server %v", v)
	}
	u, ok, err := db.UltimoVerificatoRemoto(ctx, c.d.Lettura)
	if err != nil || !ok || u.Archivio != e.Archivio || u.SHA256 != ris.SHA256 || u.Byte != ris.ByteCifrati {
		t.Errorf("riga sftp %+v %v %v", u, ok, err)
	}
	ch, ok, err := db.UltimoVerificatoSullaChiavetta(ctx, c.d.Lettura)
	if err != nil || !ok || ch.Archivio != ris.File {
		t.Errorf("l'ultimo sulla chiavetta è cambiato: %+v %v %v", ch, ok, err)
	}
}

// Risposta F: un registro con solo righe «sftp» (anche con un file locale vero e lo SHA-256 giusto) non dice mai «c'è
// un backup verificato dopo T», e le righe «sftp» non entrano nella rotazione della chiavetta.
func TestCopiaRemotaNonSblocca(t *testing.T) {
	p := nuovaProva(t)
	ctx := context.Background()
	f := filepath.Join(t.TempDir(), NomeArchivio(alle(2026, 11, 26, 2, 30)))
	b := []byte("archivio inventato")
	if err := os.WriteFile(f, b, 0o600); err != nil {
		t.Fatal(err)
	}
	h := sha256.Sum256(b)
	riga := db.VerificaBackup{Riuscita: true, Destinazione: db.DestinazioneSFTP, Archivio: f, SHA256: hex.EncodeToString(h[:]),
		Byte: int64(len(b)), CatturatoIl: time.Now().Add(-time.Hour), VerificatoIl: time.Now().Add(-time.Minute),
		Conteggi: map[string]int64{"buste": 1}}
	if err := db.RegistraVerificaBackup(ctx, p.s.DB, riga); err != nil {
		t.Fatal(err)
	}
	if ok, err := db.BackupVerificatoDopo(ctx, p.s.DB.Lettura, time.Now().Add(-2*time.Hour)); ok || err != nil {
		t.Errorf("una riga sftp sblocca: %v %v", ok, err)
	}
	if _, ok, err := db.UltimoVerificatoSullaChiavetta(ctx, p.s.DB.Lettura); ok || err != nil {
		t.Errorf("una riga sftp è l'ultimo sulla chiavetta: %v %v", ok, err)
	}
	if v, err := db.ArchiviNellaCartella(ctx, p.s.DB.Lettura, filepath.Dir(f)); len(v) != 0 || err != nil {
		t.Errorf("righe sftp nella rotazione della chiavetta: %+v %v", v, err)
	}
	if v, err := db.ArchiviRemotiNellaCartella(ctx, p.s.DB.Lettura, filepath.Dir(f)); len(v) != 1 || err != nil {
		t.Errorf("righe sftp per la rotazione remota: %+v %v", v, err)
	}
}

// L'impronta del server non si accetta mai in silenzio: una chiave diversa, o di un altro tipo, è «impronta» prima
// dell'autenticazione, e sul server non arriva niente.
func TestCopiaRemotaImprontaSbagliata(t *testing.T) {
	for _, caso := range []struct {
		nome   string
		cambia func(*ConfigRemota)
	}{
		{"altra impronta", func(c *ConfigRemota) { c.Impronta = "SHA256:" + strings.Repeat("A", 43) }},
		{"altro tipo", func(c *ConfigRemota) { c.TipoChiaveServer = ssh.KeyAlgoECDSA256 }},
	} {
		t.Run(caso.nome, func(t *testing.T) {
			c := nuovaCopiaProva(t)
			caso.cambia(&c.rm.Config)
			at := alle(2026, 11, 26, 2, 30)
			ris, v := c.archivioGrande(t, at, 1000)
			e, err := c.copia(t, context.Background(), ris, v, at)
			if codice(err) != CodiceRemotoImpronta || !errors.Is(err, ErrImprontaServer) || e.Registrato {
				t.Fatalf("errore %v (%s), esito %+v", err, codice(err), e)
			}
			if v := c.sulServer(t); len(v) != 0 {
				t.Errorf("sul server %v", v)
			}
		})
	}
}

// La chiave dell'apparecchio rifiutata è «accesso»; senza la chiave è «chiave»; con la configurazione non valida è
// «configurazione». In nessun caso arriva niente.
func TestCopiaRemotaChiaveEConfigurazione(t *testing.T) {
	c := nuovaCopiaProva(t)
	at := alle(2026, 11, 26, 2, 30)
	ris, v := c.archivioGrande(t, at, 1000)
	altra := sftpfinto.Nuovo(t)
	c.srv.Autorizza(altra.Chiave.PublicKey())
	if _, err := c.copia(t, context.Background(), ris, v, at); codice(err) != CodiceRemotoAccesso {
		t.Errorf("chiave rifiutata: %v (%s)", err, codice(err))
	}
	if err := os.Remove(FileChiaveSFTP(c.dati)); err != nil {
		t.Fatal(err)
	}
	if _, err := c.copia(t, context.Background(), ris, v, at); codice(err) != CodiceRemotoChiave || !errors.Is(err, ErrChiaveSFTPAssente) {
		t.Errorf("senza chiave: %v (%s)", err, codice(err))
	}
	c.rm.Config.Cartella = "/"
	if _, err := c.copia(t, context.Background(), ris, v, at); codice(err) != CodiceRemotoConfigurazione {
		t.Errorf("cartella radice: %v (%s)", err, codice(err))
	}
	if v := c.sulServer(t); len(v) != 0 {
		t.Errorf("sul server %v", v)
	}
}

// Il caricamento interrotto (la connessione cade a metà) è «caricamento»: il .pending resta sul server (la connessione
// non c'è più) e nessun nome finale; la copia dopo lo toglie e riesce.
func TestCopiaRemotaCaricamentoInterrotto(t *testing.T) {
	c := nuovaCopiaProva(t)
	at := alle(2026, 11, 26, 2, 30)
	ris, v := c.archivioGrande(t, at, 300_000)
	c.srv.Imposta(sftpfinto.Guasti{CadeDopo: 100_000})
	e, err := c.copia(t, context.Background(), ris, v, at)
	if err == nil || e.Registrato || (codice(err) != CodiceRemotoCaricamento && codice(err) != CodiceRemotoRete) {
		t.Fatalf("interrotto: %v (%s) %+v", err, codice(err), e)
	}
	if v := c.sulServer(t); !slices.Equal(v, []string{NomeArchivio(at) + ".pending"}) {
		t.Fatalf("sul server %v", v)
	}
	c.srv.Imposta(sftpfinto.Guasti{})
	e, err = c.copia(t, context.Background(), ris, v, at.Add(time.Hour))
	if err != nil || !slices.Equal(e.PendingTolti, []string{NomeArchivio(at) + ".pending"}) {
		t.Fatalf("seconda copia: %v %+v", err, e)
	}
	if v := c.sulServer(t); !slices.Equal(v, []string{NomeArchivio(at)}) {
		t.Errorf("sul server %v", v)
	}
}

// Mai sovrascrivere: un file con il nome dell'archivio già sul server ferma la copia («esistente») e resta com'è;
// anche se il server sovrascriverebbe con la rinomina e il file compare durante il caricamento. Il .pending si toglie.
// La pulizia toglie solo i .pending con i nomi esatti.
func TestCopiaRemotaNonSovrascrive(t *testing.T) {
	c := nuovaCopiaProva(t)
	at := alle(2026, 11, 26, 2, 30)
	ris, v := c.archivioGrande(t, at, 50_000)
	finale := c.srv.Locale(path.Join(cartellaRemota, NomeArchivio(at)))
	if err := os.WriteFile(finale, []byte("non nostro"), 0o600); err != nil {
		t.Fatal(err)
	}
	estraneo := c.srv.Locale(path.Join(cartellaRemota, "altro.tar.age.pending"))
	if err := os.WriteFile(estraneo, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := c.copia(t, context.Background(), ris, v, at); codice(err) != CodiceRemotoEsistente {
		t.Fatalf("file già presente: %v (%s)", err, codice(err))
	}
	if b, _ := os.ReadFile(finale); string(b) != "non nostro" {
		t.Fatalf("sovrascritto: %q", b)
	}
	if err := os.Remove(finale); err != nil {
		t.Fatal(err)
	}
	c.srv.Imposta(sftpfinto.Guasti{RinominaSovrascrive: true, AllaPrimaScrittura: func() {
		_ = os.WriteFile(finale, []byte("arrivato durante"), 0o600)
	}})
	if _, err := c.copia(t, context.Background(), ris, v, at); codice(err) != CodiceRemotoEsistente {
		t.Fatalf("comparso durante il caricamento: %v (%s)", err, codice(err))
	}
	if b, _ := os.ReadFile(finale); string(b) != "arrivato durante" {
		t.Fatalf("sovrascritto: %q", b)
	}
	if v := c.sulServer(t); !slices.Equal(v, []string{"altro.tar.age.pending", NomeArchivio(at)}) {
		t.Errorf("sul server %v", v)
	}
	if _, ok, _ := db.UltimoVerificatoRemoto(context.Background(), c.d.Lettura); ok {
		t.Error("registrata una copia non fatta")
	}
}

// Spazio: con lo spazio libero dichiarato e insufficiente la copia non parte («spazio»); con il disco che si riempie a
// metà è «caricamento» e il .pending si toglie (la connessione è viva).
func TestCopiaRemotaSpazioPieno(t *testing.T) {
	c := nuovaCopiaProva(t)
	at := alle(2026, 11, 26, 2, 30)
	ris, v := c.archivioGrande(t, at, 200_000)
	c.srv.Imposta(sftpfinto.Guasti{Liberi: 1 << 20})
	if _, err := c.copia(t, context.Background(), ris, v, at); codice(err) != CodiceRemotoSpazio || !errors.Is(err, ErrSpazioRemoto) {
		t.Fatalf("spazio dichiarato: %v (%s)", err, codice(err))
	}
	if v := c.sulServer(t); len(v) != 0 {
		t.Fatalf("sul server %v", v)
	}
	c.srv.Imposta(sftpfinto.Guasti{SpazioFinito: 50_000})
	if _, err := c.copia(t, context.Background(), ris, v, at); codice(err) != CodiceRemotoCaricamento {
		t.Fatalf("disco pieno: %v (%s)", err, codice(err))
	}
	if v := c.sulServer(t); len(v) != 0 {
		t.Errorf("sul server %v", v)
	}
}

// Il server che smette di rispondere a metà: il sorvegliante chiude dopo TempoSenzaRisposte («rete», ErrTempoRemoto),
// e la copia non resta appesa.
func TestCopiaRemotaServerLento(t *testing.T) {
	c := nuovaCopiaProva(t)
	c.rm.TempoSenzaRisposte = 300 * time.Millisecond
	at := alle(2026, 11, 26, 2, 30)
	ris, v := c.archivioGrande(t, at, 200_000)
	c.srv.Imposta(sftpfinto.Guasti{FermoAllaScrittura: true})
	var err error
	aspettaChiusura(t, 10*time.Second, func() { _, err = c.copia(t, context.Background(), ris, v, at) })
	if codice(err) != CodiceRemotoRete || !errors.Is(err, ErrTempoRemoto) {
		t.Fatalf("server lento: %v (%s)", err, codice(err))
	}
	// Anche la durata massima: un server che risponde, ma la copia dura troppo.
	c2 := nuovaCopiaProva(t)
	c2.rm.TempoMassimo = 200 * time.Millisecond
	ris2, v2 := c2.archivioGrande(t, at, 200_000)
	c2.srv.Imposta(sftpfinto.Guasti{FermoAllaScrittura: true})
	aspettaChiusura(t, 10*time.Second, func() { _, err = c2.copia(t, context.Background(), ris2, v2, at) })
	if codice(err) != CodiceRemotoRete || !errors.Is(err, ErrTempoRemoto) {
		t.Fatalf("durata massima: %v (%s)", err, codice(err))
	}
}

// Lo stop (serve che si ferma) durante il caricamento: la copia finisce subito con l'errore del contesto, senza
// codice e senza righe; il .pending lo toglie la copia dopo.
func TestCopiaRemotaStopDuranteIlCaricamento(t *testing.T) {
	c := nuovaCopiaProva(t)
	at := alle(2026, 11, 26, 2, 30)
	ris, v := c.archivioGrande(t, at, 200_000)
	c.srv.Imposta(sftpfinto.Guasti{FermoAllaScrittura: true})
	ctx, annulla := context.WithCancel(context.Background())
	go func() {
		<-c.srv.ScritturaArrivata()
		annulla()
	}()
	var err error
	aspettaChiusura(t, 10*time.Second, func() { _, err = c.copia(t, ctx, ris, v, at) })
	var er *ErrRemoto
	if !errors.Is(err, context.Canceled) || errors.As(err, &er) {
		t.Fatalf("stop: %v", err)
	}
	if _, ok, _ := db.UltimoVerificatoRemoto(context.Background(), c.d.Lettura); ok {
		t.Error("registrata una copia fermata")
	}
}

// aspettaChiusura aspetta (al più d) che f finisca: i test sullo stop e sui tempi non devono restare appesi.
func aspettaChiusura(t *testing.T, d time.Duration, f func()) {
	t.Helper()
	fatto := make(chan struct{})
	go func() {
		defer close(fatto)
		f()
	}()
	select {
	case <-fatto:
	case <-time.After(d):
		t.Fatalf("ancora appeso dopo %v", d)
	}
}

// remotoRegistrato scrive sul server un archivio finto catturato a at e, se riga, la sua riga «sftp» (ok o no) con gli
// stessi byte.
func (c *copiaProva) remotoRegistrato(t *testing.T, at time.Time, riga, ok bool) string {
	t.Helper()
	p := path.Join(cartellaRemota, NomeArchivio(at))
	b := []byte("copia remota inventata " + at.Format(time.RFC3339))
	if err := os.WriteFile(c.srv.Locale(p), b, 0o600); err != nil {
		t.Fatal(err)
	}
	if riga {
		h := sha256.Sum256(b)
		v := db.VerificaBackup{Riuscita: ok, Destinazione: db.DestinazioneSFTP, Archivio: p, SHA256: hex.EncodeToString(h[:]),
			Byte: int64(len(b)), CatturatoIl: at, VerificatoIl: at.Add(time.Minute), Conteggi: map[string]int64{"buste": 1}}
		if !ok {
			v.Errore, v.Conteggi = "copia finta non riuscita", nil
		}
		if err := db.RegistraVerificaBackup(context.Background(), c.d, v); err != nil {
			t.Fatal(err)
		}
	}
	return path.Base(p)
}

// rimastiSulServer dà i giorni di Roma degli archivi (nome esatto, file normale) sul server.
func (c *copiaProva) rimastiSulServer(t *testing.T) []string {
	t.Helper()
	var v []string
	for _, n := range c.sulServer(t) {
		at, ok := istanteDelNome(reArchivio, n)
		fi, err := os.Lstat(c.srv.Locale(path.Join(cartellaRemota, n)))
		if ok && err == nil && fi.Mode().IsRegular() {
			v = append(v, at.In(clock.Roma).Format("2006-01-02"))
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(v)))
	return v
}

// Voce OPS-28, in parte (la rotazione remota, D-114).
// La rotazione remota: tre mesi di copie ogni notte; restano i 7 + 4 e tutto quello che non è nostro: altri nomi,
// .pending di altri, un collegamento e una cartella con il nome di un archivio, un archivio con il nostro nome che il
// registro non conosce o conosce solo come chiavetta (le righe della chiavetta non contano sul server), uno con
// byte diversi da quelli registrati.
func TestRotazioneRemota(t *testing.T) {
	c := nuovaCopiaProva(t)
	ctx := context.Background()
	dir := c.srv.Locale(cartellaRemota)
	for g := 90; g >= 1; g-- {
		c.remotoRegistrato(t, alle(2026, 11, 26, 2, 30).AddDate(0, 0, -g), true, true)
	}
	senzaRiga := c.remotoRegistrato(t, alle(2026, 7, 1, 2, 30), false, false)
	soloChiavetta := c.remotoRegistrato(t, alle(2026, 7, 2, 2, 30), false, false)
	vc := db.VerificaBackup{Riuscita: true, Destinazione: db.DestinazioneChiavetta, Archivio: path.Join(cartellaRemota, soloChiavetta),
		SHA256: strings.Repeat("a", 64), Byte: int64(len("copia remota inventata ") + 20), CatturatoIl: alle(2026, 7, 2, 2, 30),
		VerificatoIl: alle(2026, 7, 2, 2, 31), Conteggi: map[string]int64{"buste": 1}}
	fi, _ := os.Stat(filepath.Join(dir, soloChiavetta))
	vc.Byte = fi.Size()
	if err := db.RegistraVerificaBackup(ctx, c.d, vc); err != nil {
		t.Fatal(err)
	}
	altriByte := c.remotoRegistrato(t, alle(2026, 7, 3, 2, 30), true, true)
	if err := os.WriteFile(filepath.Join(dir, altriByte), []byte("cambiato a mano, altri byte"), 0o600); err != nil {
		t.Fatal(err)
	}
	estranei := []string{"leggimi.txt", NomeArchivio(alle(2026, 7, 4, 2, 30)) + ".bak"}
	for _, n := range estranei {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(dir, "leggimi.txt"), filepath.Join(dir, NomeArchivio(alle(2026, 7, 5, 2, 30)))); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, NomeArchivio(alle(2026, 7, 6, 2, 30))), 0o700); err != nil {
		t.Fatal(err)
	}
	at := alle(2026, 11, 26, 2, 30)
	ris, v := c.archivioGrande(t, at, 5000)
	e, err := c.copia(t, ctx, ris, v, at.Add(10*time.Minute))
	if err != nil {
		t.Fatalf("copia: %v", err)
	}
	attesi := []string{"2026-11-26", "2026-11-25", "2026-11-24", "2026-11-23", "2026-11-22", "2026-11-21", "2026-11-20",
		"2026-11-15", "2026-11-08", "2026-11-01", "2026-10-25", "2026-07-03", "2026-07-02", "2026-07-01"}
	if got := c.rimastiSulServer(t); !slices.Equal(got, attesi) {
		t.Errorf("rimasti\n%v\nattesi\n%v", got, attesi)
	}
	for _, n := range append(estranei, NomeArchivio(alle(2026, 7, 5, 2, 30)), NomeArchivio(alle(2026, 7, 6, 2, 30))) {
		if _, err := os.Lstat(filepath.Join(dir, n)); err != nil {
			t.Errorf("toccato %s: %v", n, err)
		}
	}
	if len(e.Rotazione.Tolti) != 90-10 {
		t.Errorf("tolti %d", len(e.Rotazione.Tolti))
	}
	for _, p := range c.srv.Tolti() {
		if path.Dir(p) != cartellaRemota {
			t.Errorf("tolto fuori dalla cartella: %s", p)
		}
	}
	for _, n := range []string{senzaRiga, soloChiavetta, altriByte} {
		if !slices.Contains(e.Rotazione.Lasciati, n) {
			t.Errorf("%s non è fra i lasciati %v", n, e.Rotazione.Lasciati)
		}
	}
}

// La rotazione remota non cancella niente con un dubbio: un archivio registrato nel futuro, un'ultima copia nel
// registro che non è quella appena fatta, un file cambiato fra l'elenco e la cancellazione (si ferma lì). La copia
// resta buona e registrata: è «rotazione».
func TestRotazioneRemotaConUnDubbio(t *testing.T) {
	at := alle(2026, 11, 26, 2, 30)
	prepara := func(t *testing.T) (*copiaProva, Risultato, Verificato) {
		c := nuovaCopiaProva(t)
		for g := 30; g >= 1; g-- {
			c.remotoRegistrato(t, at.AddDate(0, 0, -g), true, true)
		}
		ris, v := c.archivioGrande(t, at, 3000)
		return c, ris, v
	}
	t.Run("nel futuro", func(t *testing.T) {
		c, ris, v := prepara(t)
		// Il nome è di dopodomani, la riga ha un'ora di cattura vecchia (così l'ultima copia del registro resta quella
		// nuova e si prova proprio il nome nel futuro).
		n := c.remotoRegistrato(t, at.Add(48*time.Hour), false, false)
		fi, err := os.Stat(c.srv.Locale(path.Join(cartellaRemota, n)))
		if err != nil {
			t.Fatal(err)
		}
		vb := db.VerificaBackup{Riuscita: true, Destinazione: db.DestinazioneSFTP, Archivio: path.Join(cartellaRemota, n),
			SHA256: strings.Repeat("c", 64), Byte: fi.Size(), CatturatoIl: at.AddDate(0, 0, -40), VerificatoIl: at.AddDate(0, 0, -40),
			Conteggi: map[string]int64{"buste": 1}}
		if err := db.RegistraVerificaBackup(context.Background(), c.d, vb); err != nil {
			t.Fatal(err)
		}
		e, err := c.copia(t, context.Background(), ris, v, at.Add(10*time.Minute))
		if codice(err) != CodiceRemotoRotazione || !errors.Is(err, ErrRotazione) || !e.Registrato || len(e.Rotazione.Tolti) != 0 {
			t.Fatalf("%v (%s) %+v", err, codice(err), e)
		}
		if n := len(c.rimastiSulServer(t)); n != 32 {
			t.Errorf("rimasti %d", n)
		}
	})
	t.Run("non è l'ultima", func(t *testing.T) {
		c, ris, v := prepara(t)
		// Una riga sftp catturata dopo la copia nuova (un'altra copia registrata con l'orologio avanti), senza file.
		vb := db.VerificaBackup{Riuscita: true, Destinazione: db.DestinazioneSFTP, Archivio: path.Join(cartellaRemota, "x"),
			SHA256: strings.Repeat("b", 64), Byte: 1, CatturatoIl: at.Add(time.Hour), VerificatoIl: at.Add(time.Hour),
			Conteggi: map[string]int64{"buste": 1}}
		if err := db.RegistraVerificaBackup(context.Background(), c.d, vb); err != nil {
			t.Fatal(err)
		}
		e, err := c.copia(t, context.Background(), ris, v, at.Add(2*time.Hour))
		if codice(err) != CodiceRemotoRotazione || len(e.Rotazione.Tolti) != 0 {
			t.Fatalf("%v (%s) %+v", err, codice(err), e)
		}
		if n := len(c.rimastiSulServer(t)); n != 31 {
			t.Errorf("rimasti %d", n)
		}
	})
	t.Run("cambiato durante", func(t *testing.T) {
		c, ris, v := prepara(t)
		vecchio := c.srv.Locale(path.Join(cartellaRemota, NomeArchivio(at.AddDate(0, 0, -30))))
		liste := 0
		c.srv.Imposta(sftpfinto.Guasti{DopoLista: func() {
			// Il primo elenco è della pulizia dei .pending, il secondo della rotazione.
			if liste++; liste == 2 {
				_ = os.WriteFile(vecchio, []byte("cambiato fra l'elenco e la cancellazione"), 0o600)
			}
		}})
		e, err := c.copia(t, context.Background(), ris, v, at.Add(10*time.Minute))
		if codice(err) != CodiceRemotoRotazione || slices.Contains(e.Rotazione.Tolti, path.Base(vecchio)) {
			t.Fatalf("%v (%s) %+v", err, codice(err), e)
		}
		if _, err := os.Stat(vecchio); err != nil {
			t.Errorf("tolto il file cambiato: %v", err)
		}
	})
}

// La configurazione: Controlla rifiuta i campi sbagliati; la riga si scrive, si rilegge e si toglie; una riga
// illeggibile è configurata con ErrConfigRemota.
func TestConfigRemota(t *testing.T) {
	buona := ConfigRemota{Host: "u1.your-storagebox.de", Porta: 23, Utente: "u1-sub1", Cartella: "/home/backup",
		TipoChiaveServer: ssh.KeyAlgoED25519, Impronta: "SHA256:" + strings.Repeat("A", 43)}
	if err := buona.Controlla(); err != nil {
		t.Fatal(err)
	}
	for _, cambia := range []func(*ConfigRemota){
		func(c *ConfigRemota) { c.Host = "" },
		func(c *ConfigRemota) { c.Host = "a b" },
		func(c *ConfigRemota) { c.Porta = 0 },
		func(c *ConfigRemota) { c.Porta = 70000 },
		func(c *ConfigRemota) { c.Utente = "" },
		func(c *ConfigRemota) { c.Cartella = "backup" },
		func(c *ConfigRemota) { c.Cartella = "/" },
		func(c *ConfigRemota) { c.Cartella = "/a/../b" },
		func(c *ConfigRemota) { c.TipoChiaveServer = "ssh-dss" },
		func(c *ConfigRemota) { c.Impronta = "MD5:aa" },
		func(c *ConfigRemota) { c.Impronta = "SHA256:" + strings.Repeat("A", 42) },
	} {
		c := buona
		cambia(&c)
		if err := c.Controlla(); !errors.Is(err, ErrConfigRemota) {
			t.Errorf("%+v passa", c)
		}
	}
	p := nuovaProva(t)
	ctx := context.Background()
	if _, _, ok, err := LeggiConfigRemota(ctx, p.s.DB.Lettura); ok || err != nil {
		t.Fatalf("senza riga: %v %v", ok, err)
	}
	if err := ScriviConfigRemota(ctx, p.s.DB, buona, alle(2026, 11, 26, 9, 0)); err != nil {
		t.Fatal(err)
	}
	c, quando, ok, err := LeggiConfigRemota(ctx, p.s.DB.Lettura)
	if err != nil || !ok || c != buona || !quando.Equal(alle(2026, 11, 26, 9, 0)) {
		t.Fatalf("riletta %+v %v %v %v", c, quando, ok, err)
	}
	if _, err := p.s.DB.Scrittura.Exec(`UPDATE impostazioni SET valore = '{"host": 1}' WHERE chiave = ?`, ChiaveRemoto); err != nil {
		t.Fatal(err)
	}
	if _, _, ok, err := LeggiConfigRemota(ctx, p.s.DB.Lettura); !ok || !errors.Is(err, ErrConfigRemota) {
		t.Errorf("illeggibile: %v %v", ok, err)
	}
	if err := TogliConfigRemota(ctx, p.s.DB); err != nil {
		t.Fatal(err)
	}
	if _, _, ok, _ := LeggiConfigRemota(ctx, p.s.DB.Lettura); ok {
		t.Error("ancora configurata")
	}
}

// La chiave dell'apparecchio: ed25519 OpenSSH, 0600, la pubblica in forma authorized_keys; una seconda volta non
// cambia; un file con permessi per gli altri non si usa. Entra nell'archivio (risposta B).
func TestChiaveSFTP(t *testing.T) {
	dati := t.TempDir()
	pub, creata, err := CreaChiaveSFTP(dati)
	if err != nil || !creata || !strings.HasPrefix(pub, "ssh-ed25519 ") || !strings.HasSuffix(pub, " gestrip-apparecchio") {
		t.Fatalf("%q %v %v", pub, creata, err)
	}
	fi, err := os.Stat(FileChiaveSFTP(dati))
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("permessi %v %v", fi, err)
	}
	if _, _, _, _, err := ssh.ParseAuthorizedKey([]byte(pub)); err != nil {
		t.Errorf("pubblica illeggibile: %v", err)
	}
	pub2, creata, err := CreaChiaveSFTP(dati)
	if err != nil || creata || pub2 != pub {
		t.Errorf("seconda volta: %q %v %v", pub2, creata, err)
	}
	if v := voceDi(t, filepath.Join(dati, CartellaSegreti)); !slices.Equal(v, []string{NomeChiaveSFTP}) {
		t.Errorf("in secrets/ %v", v)
	}
	if err := os.Chmod(FileChiaveSFTP(dati), 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := LeggiChiaveSFTP(dati); err == nil {
		t.Error("chiave leggibile dal gruppo accettata")
	}
	if _, err := LeggiChiaveSFTP(t.TempDir()); !errors.Is(err, ErrChiaveSFTPAssente) {
		t.Errorf("senza file: %v", err)
	}
	if !slices.Contains(segretiNellArchivio, NomeChiaveSFTP) {
		t.Error("la chiave SFTP non entra nell'archivio")
	}
	if _, err := os.Lstat(filepath.Join(dati, CartellaSegreti, "."+NomeChiaveSFTP)); !errors.Is(err, fs.ErrNotExist) {
		t.Error("temporaneo rimasto")
	}
}

// inventario: OPS-16
// Voce OPS-28, in parte (la voce chiude con il download mensile della 129c, TestProvaRemotaDelMese).
// Il giro notturno intero con la copia remota (cifratura, verifica, registro e lucchetti veri, server finto): dopo il
// backup sulla chiavetta lo stesso archivio va sul server (byte uguali, la chiave SFTP dentro l'archivio verificato),
// riga «ok» in copie_remote. Poi il server cambia chiave: il backup sulla chiavetta riesce lo stesso, l'esecuzione è
// «avviso» (il turno è chiuso), la copia è «fallita» con il codice «impronta», e lo stato ha il problema «remoto» con
// un testo senza host né percorsi, anche nel banner con -senza-lavori-automatici. Un giro buono dopo lo spegne.
func TestGiroConCopiaRemota(t *testing.T) {
	pn := nuovaProvaNotturno(t, alle(2026, 11, 23, 2, 30))
	ctx := context.Background()
	srv := sftpfinto.Nuovo(t)
	if err := os.Mkdir(srv.Locale(cartellaRemota), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, _, err := CreaChiaveSFTP(pn.p.dir); err != nil {
		t.Fatal(err)
	}
	firma, err := LeggiChiaveSFTP(pn.p.dir)
	if err != nil {
		t.Fatal(err)
	}
	srv.Autorizza(firma.PublicKey())
	cfg := ConfigRemota{Host: "u1.storage.invalid", Porta: 23, Utente: "u1-sub1", Cartella: cartellaRemota,
		TipoChiaveServer: ssh.KeyAlgoED25519, Impronta: srv.Impronta()}
	if err := ScriviConfigRemota(ctx, pn.p.s.DB, cfg, alle(2026, 11, 22, 12, 0)); err != nil {
		t.Fatal(err)
	}
	pn.n.preparaRemoto = func(r *Remoto) { r.Componi = srv.Componi }
	e, fatto, err := pn.n.Giro(ctx)
	if err != nil || !fatto || e.Esito != EsitoOK {
		t.Fatalf("giro: %+v %v %v", e, fatto, err)
	}
	locale, _ := os.ReadFile(e.Archivio)
	remoto, err := os.ReadFile(srv.Locale(path.Join(cartellaRemota, filepath.Base(e.Archivio))))
	if err != nil || !bytes.Equal(locale, remoto) {
		t.Fatalf("sul server: %v", err)
	}
	c, ok, err := db.UltimaCopiaRemota(ctx, pn.p.s.DB.Lettura)
	if err != nil || !ok || c.Esito != db.EsitoCopiaOK || c.Avvio != AvvioAutomatico || path.Base(c.Archivio) != filepath.Base(e.Archivio) {
		t.Fatalf("copia: %+v %v %v", c, ok, err)
	}
	s, err := pn.n.Stato(ctx)
	if err != nil || !s.Remoto || len(s.Problemi()) != 0 {
		t.Fatalf("stato: %v %v", codici(s.Problemi()), err)
	}

	// Il server cambia chiave (un altro server al suo posto).
	altro := sftpfinto.Nuovo(t)
	if err := os.Mkdir(altro.Locale(cartellaRemota), 0o700); err != nil {
		t.Fatal(err)
	}
	altro.Autorizza(firma.PublicKey())
	pn.n.preparaRemoto = func(r *Remoto) { r.Componi = altro.Componi }
	pn.o.Imposta(alle(2026, 11, 24, 2, 30))
	e, fatto, err = pn.n.Giro(ctx)
	if !fatto || e.Esito != EsitoAvviso || err == nil || codice(err) != CodiceRemotoImpronta {
		t.Fatalf("giro con l'impronta sbagliata: %+v %v %v (%s)", e, fatto, err, codice(err))
	}
	if u, ok, _ := db.UltimoVerificatoSullaChiavetta(ctx, pn.p.s.DB.Lettura); !ok || u.Archivio != e.Archivio {
		t.Errorf("il backup sulla chiavetta non è l'ultimo verificato: %+v", u)
	}
	if v := voceDi(t, altro.Locale(cartellaRemota)); len(v) != 0 {
		t.Errorf("mandato a un server con un'altra impronta: %v", v)
	}
	c, _, _ = db.UltimaCopiaRemota(ctx, pn.p.s.DB.Lettura)
	if c.Esito != db.EsitoCopiaFallita || c.Codice != CodiceRemotoImpronta || c.Archivio != "" {
		t.Errorf("copia: %+v", c)
	}
	pn.n.ConLavoroSpento()
	s, err = pn.n.Stato(ctx)
	if err != nil || !slices.Equal(codici(s.Allarme()), []string{ProblemaRemoto}) {
		t.Fatalf("allarme %v %v", codici(s.Allarme()), err)
	}
	testo := s.Allarme()[0].Testo
	if !strings.Contains(testo, "impronta diversa") || strings.Contains(testo, "storage.invalid") ||
		strings.Contains(testo, cartellaRemota) || strings.Contains(testo, "u1-sub1") || strings.Contains(testo, "SHA256") {
		t.Errorf("testo %q", testo)
	}
	// Turno chiuso: il lavoro non rifà il backup per la copia remota.
	pn.o.Sposta(time.Hour)
	if _, fatto, err := pn.n.Giro(ctx); fatto || err != nil {
		t.Errorf("rifatto il turno: %v %v", fatto, err)
	}
	// Il server torna quello giusto: la notte dopo la copia riesce e il problema si spegne.
	pn.n.preparaRemoto = func(r *Remoto) { r.Componi = srv.Componi }
	pn.o.Imposta(alle(2026, 11, 25, 2, 30))
	if e, _, err := pn.n.Giro(ctx); err != nil || e.Esito != EsitoOK {
		t.Fatalf("giro buono: %+v %v", e, err)
	}
	if s, err = pn.n.Stato(ctx); err != nil || len(s.Problemi()) != 0 {
		t.Errorf("problemi dopo il giro buono: %v %v", codici(s.Problemi()), err)
	}
	if v := voceDi(t, srv.Locale(cartellaRemota)); len(v) != 2 {
		t.Errorf("sul server %v", v)
	}
}

// Lo stop di serve durante la copia remota: il giro finisce subito (il backup sulla chiavetta è fatto), nessuna riga
// in copie_remote; la riga dello storico è «avviso» (il backup è buono).
func TestGiroStopDuranteLaCopiaRemota(t *testing.T) {
	pn := nuovaProvaNotturno(t, alle(2026, 11, 23, 2, 30))
	srv := sftpfinto.Nuovo(t)
	if err := os.Mkdir(srv.Locale(cartellaRemota), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, _, err := CreaChiaveSFTP(pn.p.dir); err != nil {
		t.Fatal(err)
	}
	firma, _ := LeggiChiaveSFTP(pn.p.dir)
	srv.Autorizza(firma.PublicKey())
	if err := ScriviConfigRemota(context.Background(), pn.p.s.DB, ConfigRemota{Host: "h.invalid", Porta: 22, Utente: "u",
		Cartella: cartellaRemota, TipoChiaveServer: ssh.KeyAlgoED25519, Impronta: srv.Impronta()}, alle(2026, 11, 22, 12, 0)); err != nil {
		t.Fatal(err)
	}
	srv.Imposta(sftpfinto.Guasti{FermoAllaScrittura: true})
	pn.n.preparaRemoto = func(r *Remoto) { r.Componi = srv.Componi }
	ctx, annulla := context.WithCancel(context.Background())
	go func() {
		<-srv.ScritturaArrivata()
		annulla()
	}()
	var e Esecuzione
	var err error
	aspettaChiusura(t, 20*time.Second, func() { e, _, err = pn.n.Giro(ctx) })
	if !errors.Is(err, context.Canceled) || e.Esito != EsitoAvviso {
		t.Fatalf("stop: %+v %v", e, err)
	}
	if _, ok, _ := db.UltimaCopiaRemota(context.Background(), pn.p.s.DB.Lettura); ok {
		t.Error("riga della copia fermata")
	}
	l, err := PrendiLucchetto(pn.p.dir)
	if err != nil {
		t.Fatalf("lucchetto dopo lo stop: %v", err)
	}
	_ = l.Rilascia()
}

// Lo stato della copia remota: configurata e mai riuscita non suona prima di 26 ore dalla configurazione; dopo sì
// («remoto_vecchio»), ma tace con il lavoro spento e quando c'è già «backup vecchio»; la configurazione illeggibile è
// «remoto»; un «avviso» (rotazione remota non riuscita) non è un problema.
func TestProblemiRemoto(t *testing.T) {
	base := statoBuono()
	base.Remoto, base.RemotoDal = true, adessoStato.Add(-2*time.Hour)
	casi := []struct {
		nome   string
		cambia func(*Stato)
		attesi []string
	}{
		{"appena configurata", func(*Stato) {}, nil},
		{"configurata da 26 ore esatte", func(s *Stato) { s.RemotoDal = adessoStato.Add(-EtaMassimaBackup) }, nil},
		{"mai riuscita da più di 26 ore", func(s *Stato) { s.RemotoDal = adessoStato.Add(-EtaMassimaBackup - time.Second) },
			[]string{ProblemaRemotoVecchio}},
		{"riuscita di recente", func(s *Stato) {
			s.RemotoDal = adessoStato.Add(-100 * time.Hour)
			s.CopiaBuona, s.CeCopiaBuona = db.CopiaRemota{IniziatoIl: adessoStato.Add(-8 * time.Hour)}, true
		}, nil},
		{"ultima fallita", func(s *Stato) {
			s.UltimaCopia, s.CeCopia = db.CopiaRemota{Esito: db.EsitoCopiaFallita, Codice: CodiceRemotoRete, IniziatoIl: adessoStato.Add(-6 * time.Hour)}, true
		}, []string{ProblemaRemoto}},
		{"ultima con la rotazione non riuscita", func(s *Stato) {
			s.UltimaCopia, s.CeCopia = db.CopiaRemota{Esito: db.EsitoCopiaAvviso, Codice: CodiceRemotoRotazione}, true
		}, nil},
		{"configurazione illeggibile", func(s *Stato) { s.ErrRemoto = ErrConfigRemota }, []string{ProblemaRemoto}},
		{"con backup vecchio", func(s *Stato) {
			s.RemotoDal = adessoStato.Add(-100 * time.Hour)
			s.UltimoBackup.CatturatoIl = adessoStato.Add(-30 * time.Hour)
		}, []string{ProblemaBackupVecchio}},
		{"non configurata", func(s *Stato) { s.Remoto, s.RemotoDal = false, adessoStato.Add(-100*time.Hour) }, nil},
	}
	for _, c := range casi {
		s := base
		c.cambia(&s)
		if got := codici(s.Problemi()); !slices.Equal(got, c.attesi) {
			t.Errorf("%s: %v, attesi %v", c.nome, got, c.attesi)
		}
		for _, p := range s.Problemi() {
			if strings.ContainsAny(p.Testo, "@\\") || strings.Contains(p.Testo, "sftp") {
				t.Errorf("%s: testo %q", c.nome, p.Testo)
			}
		}
	}
	s := base
	s.RemotoDal, s.LavoroSpento = adessoStato.Add(-100*time.Hour), true
	if got := codici(s.Allarme()); len(got) != 0 {
		t.Errorf("con il lavoro spento: %v", got)
	}
	for _, k := range db.CodiciCopiaRemota {
		if testo := testoRemoto(k, adessoStato); !strings.Contains(testo, "non è riuscita") || strings.Count(testo, "/") != 2 {
			t.Errorf("%s: %q", k, testo)
		}
	}
	for _, k := range []string{ProblemaRemoto, ProblemaRemotoVecchio} {
		if titoloAvviso(k) == titoloAvviso("sconosciuto") {
			t.Errorf("titolo dell'avviso mancante per %s", k)
		}
	}
}

// I controlli dopo il caricamento: un server che perde byte senza dirlo (dimensione del .pending diversa) e un archivio
// della chiavetta cambiato dopo la verifica con gli stessi byte (SHA-256 dei byte mandati diverso) sono «controllo»:
// niente nome finale, niente riga, il .pending tolto.
func TestCopiaRemotaControlli(t *testing.T) {
	c := nuovaCopiaProva(t)
	at := alle(2026, 11, 26, 2, 30)
	ris, v := c.archivioGrande(t, at, 200_000)
	c.srv.Imposta(sftpfinto.Guasti{PerdeDopo: 100_000})
	if _, err := c.copia(t, context.Background(), ris, v, at); codice(err) != CodiceRemotoControllo || !errors.Is(err, ErrControlloRemoto) {
		t.Fatalf("byte persi: %v (%s)", err, codice(err))
	}
	if v := c.sulServer(t); len(v) != 0 {
		t.Fatalf("sul server %v", v)
	}
	c.srv.Imposta(sftpfinto.Guasti{})
	b, err := os.ReadFile(ris.File)
	if err != nil {
		t.Fatal(err)
	}
	b[1000] ^= 0xff
	if err := os.WriteFile(ris.File, b, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := c.copia(t, context.Background(), ris, v, at); codice(err) != CodiceRemotoControllo {
		t.Fatalf("archivio cambiato: %v (%s)", err, codice(err))
	}
	if v := c.sulServer(t); len(v) != 0 {
		t.Errorf("sul server %v", v)
	}
	if _, ok, _ := db.UltimoVerificatoRemoto(context.Background(), c.d.Lettura); ok {
		t.Error("registrata una copia sbagliata")
	}
}

// Revisione della 128 (MEDIA): la chiavetta che si blocca durante il caricamento (la lettura dell'archivio non torna
// più) e lo stop di serve: la copia ritorna subito con l'errore del contesto, perché il sorvegliante chiude anche la
// pipe della lettura, non solo la connessione.
func TestCopiaRemotaStopConChiavettaAppesa(t *testing.T) {
	c := nuovaCopiaProva(t)
	at := alle(2026, 11, 26, 2, 30)
	ris, v := c.archivioGrande(t, at, 200_000)
	partita, sblocca := make(chan struct{}), make(chan struct{})
	t.Cleanup(func() { close(sblocca) })
	vecchio := lettoreArchivioPerTest
	lettoreArchivioPerTest = func(io.Reader) io.Reader { return lettoreAppeso{partita: partita, sblocca: sblocca} }
	t.Cleanup(func() { lettoreArchivioPerTest = vecchio })
	ctx, annulla := context.WithCancel(context.Background())
	go func() {
		<-partita
		annulla()
	}()
	var err error
	aspettaChiusura(t, 10*time.Second, func() { _, err = c.copia(t, ctx, ris, v, at) })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("stop con la chiavetta appesa: %v", err)
	}
}

// lettoreAppeso è una chiavetta appesa: alla prima lettura lo dice e poi non torna finché il test non finisce.
type lettoreAppeso struct{ partita, sblocca chan struct{} }

func (l lettoreAppeso) Read([]byte) (int, error) {
	select {
	case <-l.partita:
	default:
		close(l.partita)
	}
	<-l.sblocca
	return 0, io.ErrUnexpectedEOF
}

// Revisione della 128 (BASSA 2): un server che tronca il file dopo la rinomina. Il file nostro appena rinominato si
// toglie subito («controllo», niente riga 'ok'); se il server non lo lascia togliere, la riga 'fallita' ha i byte
// visti sul server, così la rotazione remota lo riconosce come nostro.
func TestCopiaRemotaTroncataDopoLaRinomina(t *testing.T) {
	c := nuovaCopiaProva(t)
	ctx := context.Background()
	at := alle(2026, 11, 26, 2, 30)
	ris, v := c.archivioGrande(t, at, 100_000)
	c.srv.Imposta(sftpfinto.Guasti{TroncaDopoRinomina: true})
	if _, err := c.copia(t, ctx, ris, v, at); codice(err) != CodiceRemotoControllo {
		t.Fatalf("troncato: %v (%s)", err, codice(err))
	}
	if v := c.sulServer(t); len(v) != 0 {
		t.Fatalf("troncato rimasto sul server: %v", v)
	}
	c.srv.Imposta(sftpfinto.Guasti{TroncaDopoRinomina: true, RifiutaRemove: true})
	if _, err := c.copia(t, ctx, ris, v, at); codice(err) != CodiceRemotoControllo {
		t.Fatalf("troncato non togliibile: %v (%s)", err, codice(err))
	}
	fi, err := os.Stat(c.srv.Locale(path.Join(cartellaRemota, NomeArchivio(at))))
	if err != nil || fi.Size() != 50_000 {
		t.Fatalf("sul server %v %v", fi, err)
	}
	righe, err := db.ArchiviRemotiNellaCartella(ctx, c.d.Lettura, cartellaRemota)
	if err != nil || len(righe) != 1 || righe[0].Riuscita || righe[0].Byte != fi.Size() {
		t.Errorf("riga %+v %v", righe, err)
	}
	if _, ok, _ := db.UltimoVerificatoRemoto(ctx, c.d.Lettura); ok {
		t.Error("una copia troncata registrata come buona")
	}
}

// Revisione della 128 (BASSA 9): il .pending si crea con O_EXCL, quindi uno comparso fra il controllo e l'apertura
// (qui: creato subito dopo il Lstat del .pending, che ha risposto «non c'è») non si sovrascrive; la pulizia toglie solo i .pending che sono file normali
// (una cartella e un collegamento con il nome esatto restano).
func TestCopiaRemotaPendingNonNostri(t *testing.T) {
	c := nuovaCopiaProva(t)
	at := alle(2026, 11, 26, 2, 30)
	ris, v := c.archivioGrande(t, at, 50_000)
	dir := c.srv.Locale(cartellaRemota)
	cartella := filepath.Join(dir, NomeArchivio(at.AddDate(0, 0, -1))+".pending")
	if err := os.Mkdir(cartella, 0o700); err != nil {
		t.Fatal(err)
	}
	collegamento := filepath.Join(dir, NomeArchivio(at.AddDate(0, 0, -2))+".pending")
	if err := os.Symlink(filepath.Join(dir, "altrove"), collegamento); err != nil {
		t.Fatal(err)
	}
	pending := path.Join(cartellaRemota, NomeArchivio(at)+".pending")
	c.srv.Imposta(sftpfinto.Guasti{AlLstat: func(p string) {
		if p == pending {
			if _, err := os.Lstat(c.srv.Locale(p)); err != nil {
				_ = os.WriteFile(c.srv.Locale(p), []byte("di un altro"), 0o600)
			}
		}
	}})
	if _, err := c.copia(t, context.Background(), ris, v, at); codice(err) != CodiceRemotoEsistente {
		t.Fatalf("pending comparso: %v (%s)", err, codice(err))
	}
	if b, _ := os.ReadFile(c.srv.Locale(pending)); string(b) != "di un altro" {
		t.Errorf("pending sovrascritto: %d byte", len(b))
	}
	for _, p := range []string{cartella, collegamento} {
		if _, err := os.Lstat(p); err != nil {
			t.Errorf("tolto %s: %v", filepath.Base(p), err)
		}
	}
}

// Revisione della 128 (BASSA 6): un temporaneo della chiave SFTP rimasto da un «gestrip chiave-sftp» interrotto ferma il
// backup con un messaggio che dice cos'è e cosa fare, come quello della chiave dei backup.
func TestTemporaneoDellaChiaveSFTPRimasto(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "."+NomeChiaveSFTP+"-12345"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := leggiSegreti(dir)
	if !errors.Is(err, ErrFileInatteso) || !strings.Contains(err.Error(), "gestrip chiave-sftp") || !strings.Contains(err.Error(), "toglilo a mano") {
		t.Errorf("errore %v", err)
	}
}
