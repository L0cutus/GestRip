package backup

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
	"github.com/L0cutus/gestrip/internal/sftpfinto"
)

// La configurazione della copia remota comune a comando e pagina (slice 129, configura_remoto.go) contro il server
// SFTP finto: campi controllati prima di collegarsi, impronta mai accettata in silenzio, scrittura solo con la prova
// riuscita, tempo massimo di prova e lettura dell'impronta, una prova alla volta. Dati inventati, niente rete.

// configProva è una ConfigurazioneRemota su un database nuovo, con il server finto (cartella /backup) e la chiave
// dell'apparecchio creata e autorizzata.
type configProva struct {
	d   *db.DB
	srv *sftpfinto.Server
	cr  *ConfigurazioneRemota
	pub ssh.PublicKey
	// prepara cambia ogni Remoto dopo il server finto (tempi, connessioni guaste).
	prepara func(*Remoto)
}

func nuovaConfigProva(t *testing.T) *configProva {
	t.Helper()
	p := nuovaProva(t)
	srv := sftpfinto.Nuovo(t)
	if err := os.Mkdir(srv.Locale(cartellaRemota), 0o700); err != nil {
		t.Fatal(err)
	}
	c := &configProva{d: p.s.DB, srv: srv}
	c.cr = NuovaConfigurazioneRemota(p.s.DB, clock.NuovoFinto(alle(2026, 11, 26, 10, 0))).ConPreparazione(func(r *Remoto) {
		r.Componi = srv.Componi
		if c.prepara != nil {
			c.prepara(r)
		}
	})
	pub, creata, err := c.cr.CreaChiave()
	if err != nil || !creata {
		t.Fatalf("chiave: %v %v", creata, err)
	}
	k, _, _, _, err := ssh.ParseAuthorizedKey([]byte(pub))
	if err != nil {
		t.Fatal(err)
	}
	c.pub = k
	srv.Autorizza(k)
	return c
}

func (c *configProva) nuova(t *testing.T, cartella string) ConfigRemota {
	t.Helper()
	n, err := NuovaConfigRemota(" u1.storage.invalid ", 23, " u1-sub1 ", cartella)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func (c *configProva) configurata(t *testing.T) (ConfigRemota, bool) {
	t.Helper()
	cfg, ok, err := c.cr.Leggi(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return cfg, ok
}

// sblocco è un guasto AlLstat che tiene fermo il server sulla cartella dei backup finché non si sblocca (alla fine
// del test, prima del server).
func sblocco(t *testing.T, srv *sftpfinto.Server) (fermo <-chan struct{}, sblocca func()) {
	t.Helper()
	ferma, arrivato := make(chan struct{}), make(chan struct{})
	var una, unaArrivo sync.Once
	sblocca = func() { una.Do(func() { close(ferma) }) }
	t.Cleanup(sblocca)
	srv.Imposta(sftpfinto.Guasti{AlLstat: func(p string) {
		if p == cartellaRemota {
			unaArrivo.Do(func() { close(arrivato) })
			<-ferma
		}
	}})
	return arrivato, sblocca
}

// Voce OPS-16 (destinazione remota), già chiusa dalla 128: niente segno «inventario:».
// I campi scritti si controllano prima di collegarsi, con il campo sbagliato; gli spazi ai lati si tolgono.
func TestNuovaConfigRemota(t *testing.T) {
	for _, c := range []struct {
		host     string
		porta    int
		utente   string
		cartella string
		campo    string
	}{
		{"", 23, "u", "b", CampoHost},
		{"a b", 23, "u", "b", CampoHost},
		{"u@h", 23, "u", "b", CampoHost},
		{"h", 0, "u", "b", CampoPorta},
		{"h", 65536, "u", "b", CampoPorta},
		{"h", 23, "", "b", CampoUtente},
		{"h", 23, "u:x", "b", CampoUtente},
		{"h", 23, "u", "", CampoCartella},
		{"h", 23, "u", "  ", CampoCartella},
		{"h", 23, "u", "a\nb", CampoCartella},
		{"h", 23, "u", strings.Repeat("c", maxCartellaRemota+1), CampoCartella},
	} {
		_, err := NuovaConfigRemota(c.host, c.porta, c.utente, c.cartella)
		var ec *ErrCampoRemoto
		if !errors.As(err, &ec) || ec.Campo != c.campo || !errors.Is(err, ErrConfigRemota) {
			t.Errorf("%+v: %v", c, err)
		}
	}
	n, err := NuovaConfigRemota(" h ", 23, " u ", " backup ")
	if err != nil || n.Host != "h" || n.Utente != "u" || n.Cartella != "backup" || n.Impronta != "" {
		t.Errorf("%+v %v", n, err)
	}
}

// L'impronta del server si mostra senza scrivere niente e si fissa solo riscritta uguale; la configurazione si scrive
// solo con la prova riuscita (chiave accettata, cartella che c'è, non la radice), con la cartella vera del server.
// La prova della configurazione salvata rifiuta un server con un'altra chiave; spenta, non c'è niente da provare.
func TestFissaRemoto(t *testing.T) {
	c := nuovaConfigProva(t)
	ctx := context.Background()
	n := c.nuova(t, "backup")

	tipo, impronta, err := c.cr.ChiaveDelServer(ctx, n)
	if err != nil || tipo != ssh.KeyAlgoED25519 || impronta != c.srv.Impronta() {
		t.Fatalf("chiave del server: %q %q %v", tipo, impronta, err)
	}
	if _, ok := c.configurata(t); ok {
		t.Fatal("mostrare l'impronta ha scritto la configurazione")
	}
	for nome, voluta := range map[string]string{"vuota": "", "senza prefisso": strings.TrimPrefix(impronta, "SHA256:"),
		"corta": impronta[:20]} {
		if _, err := c.cr.Fissa(ctx, n, voluta); !errors.Is(err, ErrImprontaMalScritta) {
			t.Errorf("%s: %v", nome, err)
		}
	}
	altra := "SHA256:" + strings.Repeat("B", 43)
	_, err = c.cr.Fissa(ctx, n, altra)
	var diversa *ErrImprontaDiversa
	if !errors.As(err, &diversa) || diversa.Vista != impronta || diversa.Tipo != tipo || codice(err) != CodiceRemotoImpronta ||
		!errors.Is(err, ErrImprontaServer) {
		t.Fatalf("impronta diversa: %v", err)
	}
	// La chiave dell'apparecchio non è (più) nel pannello.
	c.srv.Autorizza(c.srv.Chiave.PublicKey())
	if _, err := c.cr.Fissa(ctx, n, " "+impronta+" "); codice(err) != CodiceRemotoAccesso {
		t.Fatalf("chiave non autorizzata: %v", err)
	}
	c.srv.Autorizza(c.pub)
	if _, err := c.cr.Fissa(ctx, c.nuova(t, "manca"), impronta); codice(err) != CodiceRemotoCartella {
		t.Fatalf("cartella che non c'è: %v", err)
	}
	var ec *ErrCampoRemoto
	if _, err := c.cr.Fissa(ctx, c.nuova(t, "/"), impronta); !errors.As(err, &ec) || ec.Campo != CampoCartella {
		t.Fatalf("radice: %v", err)
	}
	if _, ok := c.configurata(t); ok {
		t.Fatal("scritta una configurazione con una prova fallita")
	}
	cfg, err := c.cr.Fissa(ctx, n, impronta)
	if err != nil {
		t.Fatal(err)
	}
	salvata, ok := c.configurata(t)
	if !ok || salvata != cfg || cfg.Cartella != cartellaRemota || cfg.Host != "u1.storage.invalid" || cfg.Utente != "u1-sub1" ||
		cfg.Porta != 23 || cfg.Impronta != impronta || cfg.TipoChiaveServer != tipo {
		t.Fatalf("salvata %+v, data %+v", salvata, cfg)
	}
	if vera, err := c.cr.Prova(ctx); err != nil || vera != cartellaRemota {
		t.Fatalf("prova della salvata: %q %v", vera, err)
	}
	// Un altro server (un'altra chiave) allo stesso indirizzo: la prova non si autentica nemmeno.
	altro := sftpfinto.Nuovo(t)
	altro.Autorizza(c.pub)
	if err := os.Mkdir(altro.Locale(cartellaRemota), 0o700); err != nil {
		t.Fatal(err)
	}
	c.prepara = func(r *Remoto) { r.Componi = altro.Componi }
	if _, err := c.cr.Prova(ctx); codice(err) != CodiceRemotoImpronta || !errors.Is(err, ErrImprontaServer) {
		t.Fatalf("impronta cambiata: %v", err)
	}
	c.prepara = nil
	if err := c.cr.Togli(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := c.cr.Prova(ctx); !errors.Is(err, ErrRemotoNonConfigurato) {
		t.Fatalf("prova senza configurazione: %v", err)
	}
}

// Il tempo massimo della prova e della lettura dell'impronta (nota 13: prima senza): un server che resta fermo sulla
// cartella, una connessione che non risponde all'handshake, entro il tempo della prova o quello dell'handshake,
// sempre ErrTempoRemoto con il codice «rete»; lo stop della richiesta è il contesto, senza codice.
func TestProvaRemotaTempoMassimo(t *testing.T) {
	c := nuovaConfigProva(t)
	ctx := context.Background()
	n := c.nuova(t, "backup")
	impronta := c.srv.Impronta()
	arrivato, sblocca := sblocco(t, c.srv)
	c.prepara = func(r *Remoto) { r.TempoProva = 300 * time.Millisecond }
	var err error
	aspettaChiusura(t, 10*time.Second, func() { _, err = c.cr.Fissa(ctx, n, impronta) })
	<-arrivato
	if codice(err) != CodiceRemotoRete || !errors.Is(err, ErrTempoRemoto) {
		t.Fatalf("server fermo sulla cartella: %v (%s)", err, codice(err))
	}
	if _, ok := c.configurata(t); ok {
		t.Fatal("scritta con la prova scaduta")
	}
	sblocca()

	// Una connessione che accetta e non risponde: la lettura dell'impronta scade.
	zitta := func(context.Context, string) (net.Conn, error) {
		a, b := net.Pipe()
		go func() { _, _ = io.Copy(io.Discard, b) }()
		t.Cleanup(func() { _ = b.Close() })
		return a, nil
	}
	for nome, prepara := range map[string]func(*Remoto){
		"tempo della prova":    func(r *Remoto) { r.Componi, r.TempoProva = zitta, 300*time.Millisecond },
		"tempo dell'handshake": func(r *Remoto) { r.Componi, r.TempoHandshake = zitta, 300*time.Millisecond },
	} {
		c.prepara = prepara
		aspettaChiusura(t, 10*time.Second, func() { _, _, err = c.cr.ChiaveDelServer(ctx, n) })
		if codice(err) != CodiceRemotoRete || !errors.Is(err, ErrTempoRemoto) {
			t.Errorf("%s: %v (%s)", nome, err, codice(err))
		}
	}
	// Lo stop (la richiesta del titolare chiusa): il contesto, non un tempo scaduto.
	c.prepara = func(r *Remoto) { r.Componi = zitta }
	cctx, annulla := context.WithTimeout(ctx, 200*time.Millisecond)
	defer annulla()
	aspettaChiusura(t, 10*time.Second, func() { _, _, err = c.cr.ChiaveDelServer(cctx, n) })
	var er *ErrRemoto
	if !errors.Is(err, context.DeadlineExceeded) || errors.As(err, &er) {
		t.Errorf("stop: %v", err)
	}
}

// Porta chiusa e nome che non si trova: codice «rete», con la causa riconoscibile e senza l'indirizzo nel testo.
func TestChiaveDelServerRete(t *testing.T) {
	c := nuovaConfigProva(t)
	n := c.nuova(t, "backup")
	for nome, caso := range map[string]struct {
		err   error
		causa error
	}{
		"porta chiusa": {&net.OpError{Op: "dial", Net: "tcp", Err: os.NewSyscallError("connect", syscall.ECONNREFUSED)}, syscall.ECONNREFUSED},
		"nome":         {&net.OpError{Op: "dial", Net: "tcp", Err: &net.DNSError{Err: "no such host", Name: "u1.storage.invalid"}}, ErrNomeServer},
	} {
		c.prepara = func(r *Remoto) {
			r.Componi = func(context.Context, string) (net.Conn, error) { return nil, caso.err }
		}
		_, _, err := c.cr.ChiaveDelServer(context.Background(), n)
		if codice(err) != CodiceRemotoRete || !errors.Is(err, caso.causa) || strings.Contains(err.Error(), "storage.invalid") {
			t.Errorf("%s: %v", nome, err)
		}
	}
}

// Una prova alla volta: mentre la prima è ferma sul server, la seconda (anche un'altra operazione) risponde subito
// ErrProvaInCorso senza collegarsi; finita la prima, si riprova. La chiave non si crea durante un backup.
func TestConfigurazioneRemotaUnaAllaVolta(t *testing.T) {
	c := nuovaConfigProva(t)
	ctx := context.Background()
	n := c.nuova(t, "backup")
	impronta := c.srv.Impronta()
	arrivato, sblocca := sblocco(t, c.srv)
	fatto := make(chan error, 1)
	go func() {
		_, err := c.cr.Fissa(ctx, n, impronta)
		fatto <- err
	}()
	<-arrivato
	inizio := clock.Parti()
	if _, _, err := c.cr.ChiaveDelServer(ctx, n); !errors.Is(err, ErrProvaInCorso) {
		t.Errorf("seconda impronta: %v", err)
	}
	if _, err := c.cr.Prova(ctx); !errors.Is(err, ErrProvaInCorso) {
		t.Errorf("seconda prova: %v", err)
	}
	if _, err := c.cr.Fissa(ctx, n, impronta); !errors.Is(err, ErrProvaInCorso) {
		t.Errorf("seconda conferma: %v", err)
	}
	if d := inizio.Trascorso(); d > 2*time.Second {
		t.Errorf("la seconda ha aspettato %v", d)
	}
	sblocca()
	if err := <-fatto; err != nil {
		t.Fatalf("prima: %v", err)
	}
	if _, _, err := c.cr.ChiaveDelServer(ctx, n); err != nil {
		t.Errorf("dopo: %v", err)
	}

	l, err := PrendiLucchetto(dirDi(c.d))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Rilascia() }()
	if _, _, err := c.cr.CreaChiave(); !errors.Is(err, ErrBackupInCorso) {
		t.Errorf("chiave durante un backup: %v", err)
	}
}

func dirDi(d *db.DB) string { return strings.TrimSuffix(d.Percorso(), "/gestrip.db") }

// Revisione 129, M1: lo stop di serve interrompe la prova in corso (ferma sul server, con il tempo massimo intero
// davanti): torna subito con l'errore del contesto, senza codice e senza scrivere; dopo lo stop non ne parte un'altra.
func TestConfigurazioneRemotaStopDiServe(t *testing.T) {
	c := nuovaConfigProva(t)
	stop, ferma := context.WithCancel(context.Background())
	defer ferma()
	c.cr.ConStop(stop)
	n := c.nuova(t, "backup")
	arrivato, _ := sblocco(t, c.srv)
	fatto := make(chan error, 1)
	go func() {
		_, err := c.cr.Fissa(context.Background(), n, c.srv.Impronta())
		fatto <- err
	}()
	<-arrivato
	inizio := clock.Parti()
	ferma()
	var err error
	aspettaChiusura(t, 5*time.Second, func() { err = <-fatto })
	var er *ErrRemoto
	if !errors.Is(err, context.Canceled) || errors.As(err, &er) || inizio.Trascorso() > 3*time.Second {
		t.Fatalf("stop: %v dopo %v", err, inizio.Trascorso())
	}
	if _, ok := c.configurata(t); ok {
		t.Fatal("scritta dopo lo stop")
	}
	if _, err := c.cr.Prova(context.Background()); !errors.Is(err, context.Canceled) && !errors.Is(err, ErrRemotoNonConfigurato) {
		t.Errorf("dopo lo stop: %v", err)
	}
}

// Revisione 129, B4: lo spegnimento passa dallo stesso turno della conferma: mentre una conferma è in corso è
// ErrProvaInCorso (niente spento, e la conferma che finisce dopo non riscrive una configurazione spenta).
func TestTogliDuranteLaConferma(t *testing.T) {
	c := nuovaConfigProva(t)
	n := c.nuova(t, "backup")
	arrivato, sblocca := sblocco(t, c.srv)
	fatto := make(chan error, 1)
	go func() {
		_, err := c.cr.Fissa(context.Background(), n, c.srv.Impronta())
		fatto <- err
	}()
	<-arrivato
	if err := c.cr.Togli(context.Background()); !errors.Is(err, ErrProvaInCorso) {
		t.Errorf("togli durante la conferma: %v", err)
	}
	sblocca()
	if err := <-fatto; err != nil {
		t.Fatal(err)
	}
	if err := c.cr.Togli(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.configurata(t); ok {
		t.Error("non spenta")
	}
}

// Revisione 129, B5: un solo tempo massimo per tutta la conferma (lettura della chiave e prova): due collegamenti
// lenti, ognuno sotto il tempo ma insieme sopra, la fanno scadere.
func TestFissaRemotoTempoUnico(t *testing.T) {
	c := nuovaConfigProva(t)
	n := c.nuova(t, "backup")
	c.prepara = func(r *Remoto) {
		r.TempoProva = 600 * time.Millisecond
		srv := r.Componi
		r.Componi = func(ctx context.Context, ind string) (net.Conn, error) {
			if !clock.Aspetta(ctx, 400*time.Millisecond) {
				return nil, ctx.Err()
			}
			return srv(ctx, ind)
		}
	}
	var err error
	aspettaChiusura(t, 10*time.Second, func() { _, err = c.cr.Fissa(context.Background(), n, c.srv.Impronta()) })
	if codice(err) != CodiceRemotoRete || !errors.Is(err, ErrTempoRemoto) {
		t.Fatalf("conferma lenta: %v (%s)", err, codice(err))
	}
	if _, ok := c.configurata(t); ok {
		t.Fatal("scritta")
	}
}
