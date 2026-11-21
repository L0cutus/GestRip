package sidecar

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// TestMain: con «sidecar-finto <modo> <porta> [file]» il binario dei test fa da sidecar (il figlio
// finto). Il modo sta negli argomenti, non nell'ambiente, perché il supervisore dà al figlio solo
// l'ambiente del Comando.
//   - «sano»: /health risponde sempre 200;
//   - «lento»: le prime 3 GET di salute danno 503 (llama-server che carica il modello), poi 200;
//   - «malato»: 200 alle prime 2 GET, poi sempre 503;
//   - «singhiozzo»: 200 e 503 a turno (mai due fallite di fila);
//   - «muto»: ascolta ma risponde sempre 503;
//   - «mai»: esce subito con 3, senza ascoltare;
//   - «sordo»: come «sano», ma ignora SIGTERM;
//   - «nonno»: come «sano», e avvia un nipote («dorme», nello stesso gruppo) di cui scrive il pid in file;
//   - «ambiente»: scrive in file le variabili d'ambiente e la cartella di lavoro, poi come «sano»;
//   - «chiacchierone»: scrive su stdout e stderr il terzo argomento, poi come «sano»;
//   - «appeso»: accetta le connessioni e non risponde mai;
//   - «rinvia»: /health risponde 302 verso /ok, che dà 200;
//   - «ovunque»: come «sano», ma ascolta su tutte le interfacce (0.0.0.0);
//   - «cede»: non ascolta; avvia un processo «sano» in una sessione sua (fuori dal gruppo) che
//     prende la porta, ne scrive il pid in file e dorme;
//   - «dorme»: dorme (il nipote).
//
// Con «sidecar-supervisore <porta> <file>» fa da GestRip: un Supervisore sul figlio finto «sano»,
// che scrive in file il pid del figlio pronto e aspetta (per Pdeathsig).
func TestMain(m *testing.M) {
	if len(os.Args) == 4 && os.Args[1] == "sidecar-supervisore" {
		os.Exit(supervisoreFinto(os.Args[2], os.Args[3]))
	}
	if len(os.Args) >= 4 && os.Args[1] == "sidecar-finto" {
		os.Exit(figlioFinto(os.Args[2], os.Args[3], os.Args[4:]))
	}
	if len(os.Args) >= 3 && os.Args[1] == "sidecar-finto" && os.Args[2] == "dorme" {
		signal.Ignore(syscall.SIGTERM)
		time.Sleep(time.Minute)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func figlioFinto(modo, porta string, resto []string) int {
	if modo == "mai" {
		return 3
	}
	if modo == "sordo" || modo == "nonno" {
		signal.Ignore(syscall.SIGTERM)
	}
	if modo == "ambiente" {
		wd, _ := os.Getwd()
		dati := strings.Join(append([]string{"cartella=" + wd, "variabili=" + strconv.Itoa(len(os.Environ()))}, os.Environ()...), "\n")
		if os.WriteFile(resto[0], []byte(dati), 0o600) != nil {
			return 4
		}
	}
	if modo == "chiacchierone" {
		fmt.Println(resto[0])
		fmt.Fprintln(os.Stderr, resto[0])
	}
	if modo == "appeso" {
		l, err := net.Listen("tcp", "127.0.0.1:"+porta)
		if err != nil {
			return 7
		}
		go func() { time.Sleep(time.Minute); os.Exit(8) }()
		tenute := make(chan net.Conn, 1024) // mai una risposta, mai chiuse
		for {
			c, err := l.Accept()
			if err != nil {
				return 9
			}
			select {
			case tenute <- c:
			default:
			}
		}
	}
	if modo == "cede" {
		c := exec.Command(os.Args[0], "sidecar-finto", "sano", porta)
		c.SysProcAttr = &syscall.SysProcAttr{Setsid: true} // fuori dal gruppo del figlio
		if c.Start() != nil {
			return 5
		}
		if os.WriteFile(resto[0], []byte(strconv.Itoa(c.Process.Pid)), 0o600) != nil {
			return 6
		}
		time.Sleep(time.Minute)
		return 0
	}
	if modo == "nonno" {
		c := exec.Command(os.Args[0], "sidecar-finto", "dorme")
		// Niente Pdeathsig: il nipote deve morire per il SIGKILL al gruppo, non con il figlio. Vive al
		// più un minuto («dorme»).
		if c.Start() != nil {
			return 5
		}
		if os.WriteFile(resto[0], []byte(strconv.Itoa(c.Process.Pid)), 0o600) != nil {
			return 6
		}
	}
	var n atomic.Int64
	mux := http.NewServeMux()
	mux.HandleFunc("/ok", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if modo == "rinvia" {
			http.Redirect(w, r, "/ok", http.StatusFound)
			return
		}
		k := n.Add(1)
		switch {
		case modo == "lento" && k <= 3, modo == "malato" && k > 2, modo == "muto", modo == "singhiozzo" && k%2 == 0:
			w.WriteHeader(http.StatusServiceUnavailable)
		default:
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		}
	})
	host := "127.0.0.1"
	if modo == "ovunque" {
		host = "0.0.0.0"
	}
	l, err := net.Listen("tcp", host+":"+porta)
	if err != nil {
		return 7
	}
	// Il figlio finto vive al più un minuto: un test caduto non lo lascia acceso.
	go func() { time.Sleep(time.Minute); os.Exit(8) }()
	_ = http.Serve(l, mux)
	return 9
}

func supervisoreFinto(porta, file string) int {
	n, err := strconv.Atoi(porta)
	if err != nil {
		return 10
	}
	s, err := Nuovo(Comando{Nome: "prova", Programma: os.Args[0], Argomenti: []string{"sidecar-finto", "sano", SegnapostoPorta}, Porta: n},
		tempiProva, nil)
	if err != nil {
		return 11
	}
	go s.Lavoro(context.Background())
	for range 4000 {
		if st := s.Stato(); st.Fase == FasePronto {
			if os.WriteFile(file, []byte(strconv.Itoa(st.Pid)), 0o600) != nil {
				return 12
			}
			time.Sleep(time.Minute)
			return 0
		}
		time.Sleep(5 * time.Millisecond)
	}
	return 13
}

// portaLiberaPerTest è una porta alta libera su 127.0.0.1 (quella che il sistema dà alla porta 0).
// portaLiberaPerTest è una porta libera a caso fra 20000 e 32000, sotto l'intervallo effimero di Linux
// (32768-60999): la porta 0 darebbe una porta effimera, che sotto carico il sistema può ridare a una
// connessione in uscita prima che il figlio la apra (revisione della 111, M1). Le porte di
// gestrip-switch e llm-switch non ci sono, ma si escludono lo stesso.
func portaLiberaPerTest(t testing.TB) int {
	t.Helper()
	for range 200 {
		p := 20000 + rand.IntN(12000)
		if porteVietate[p] {
			continue
		}
		l, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(p))
		if err != nil {
			continue
		}
		if err := l.Close(); err != nil {
			t.Fatal(err)
		}
		return p
	}
	t.Fatal("nessuna porta libera fra 20000 e 32000")
	return 0
}

// tempiProva: tempi corti e attese di 10, 20, 40 ms.
var tempiProva = Tempi{
	Avvio:         15 * time.Second,
	PassoAvvio:    10 * time.Millisecond,
	Controllo:     10 * time.Millisecond,
	Risposta:      2 * time.Second,
	Malato:        2,
	AttesaMinima:  10 * time.Millisecond,
	AttesaMassima: 40 * time.Millisecond,
	Stabile:       time.Hour,
	Arresto:       5 * time.Second,
}

// finto è il Comando del figlio finto nel modo dato.
func finto(t *testing.T, modo string, altri ...string) Comando {
	t.Helper()
	return Comando{
		Nome:      "prova",
		Programma: os.Args[0],
		Argomenti: append([]string{"sidecar-finto", modo, SegnapostoPorta}, altri...),
		Porta:     portaLiberaPerTest(t),
	}
}

// registroBuffer è un registro slog in memoria, sicuro con più goroutine.
type registroBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (r *registroBuffer) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.b.Write(p)
}

func (r *registroBuffer) String() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.b.String()
}

// inProva è un supervisore che gira in una goroutine; ferma lo chiude e aspetta Lavoro.
type inProva struct {
	s        *Supervisore
	registro *registroBuffer
	annulla  context.CancelFunc
	fatto    chan struct{}
	pids     map[int]bool
	mu       sync.Mutex
	// lasciaStabile: quanto cadutaVoluta lascia vivere il figlio pronto prima di ucciderlo.
	lasciaStabile time.Duration
}

// cadutaVoluta aspetta un figlio pronto nuovo, lo uccide con SIGKILL, aspetta il successivo pronto
// e dà l'attesa scelta dopo questa caduta: conta solo le cadute volute (un tentativo andato a
// vuoto per la porta presa da altri non sposta il confronto; revisione della 111, M1).
func (p *inProva) cadutaVoluta(t *testing.T) time.Duration {
	t.Helper()
	pid := p.nuovoPronto(t)
	if p.lasciaStabile > 0 {
		time.Sleep(p.lasciaStabile)
	}
	n := len(p.s.Stato().Attese)
	if err := syscall.Kill(pid, syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	aspettaChe(t, "il figlio ripartito", func() bool {
		st := p.s.Stato()
		return len(st.Attese) > n && st.Fase == FasePronto && st.Pid != pid
	})
	return p.s.Stato().Attese[n]
}

func parti(t *testing.T, c Comando, tm Tempi) *inProva {
	t.Helper()
	return partiCon(t, c, tm, nil)
}

// partiCon è parti con il registro scritto anche su altro (se non è nil).
func partiCon(t *testing.T, c Comando, tm Tempi, altro io.Writer) *inProva {
	t.Helper()
	reg := &registroBuffer{}
	var w io.Writer = reg
	if altro != nil {
		w = io.MultiWriter(reg, altro)
	}
	s, err := Nuovo(c, tm, slog.New(slog.NewTextHandler(w, nil)))
	if err != nil {
		t.Fatal(err)
	}
	ctx, annulla := context.WithCancel(context.Background())
	p := &inProva{s: s, registro: reg, annulla: annulla, fatto: make(chan struct{}), pids: map[int]bool{}}
	go func() { defer close(p.fatto); s.Lavoro(ctx) }()
	// Sempre, anche con il test fallito: Lavoro fermato e nessun figlio rimasto.
	t.Cleanup(func() { p.ferma(t) })
	return p
}

// ferma chiude Lavoro e controlla che nessun figlio visto resti vivo.
func (p *inProva) ferma(t *testing.T) {
	t.Helper()
	p.annulla()
	select {
	case <-p.fatto:
	case <-time.After(20 * time.Second):
		t.Fatal("Lavoro non torna dopo la fine del contesto")
	}
	if st := p.s.Stato(); st.Fase != FaseSpento || st.Pid != 0 {
		t.Errorf("dopo Lavoro: %+v", st)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for pid := range p.pids {
		aspettaMorto(t, pid)
	}
}

// pid aspetta un figlio pronto diverso da quelli già visti e lo dà.
func (p *inProva) nuovoPronto(t *testing.T) int {
	t.Helper()
	var pid int
	aspettaChe(t, "un figlio nuovo pronto", func() bool {
		st := p.s.Stato()
		p.mu.Lock()
		defer p.mu.Unlock()
		if st.Fase == FasePronto && st.Pid != 0 && !p.pids[st.Pid] {
			pid = st.Pid
			p.pids[pid] = true
			return true
		}
		return false
	})
	return pid
}

// aspettaChe aspetta cond al più 20 s (in passi di 5 ms).
func aspettaChe(t *testing.T, cosa string, cond func() bool) {
	t.Helper()
	fine := time.Now().Add(20 * time.Second)
	for !cond() {
		if time.Now().After(fine) {
			t.Fatalf("aspettavo %s", cosa)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// vivoDavvero: pid c'è e non è uno zombie (uno zombie è già morto, aspetta solo chi lo raccoglie).
func vivoDavvero(pid int) bool {
	stato, _, ok := statDi(pid)
	return ok && stato != 'Z'
}

// gruppoVivo: nel gruppo c'è un processo che non è uno zombie.
func gruppoVivo(gruppo int) bool {
	if errors.Is(syscall.Kill(-gruppo, 0), syscall.ESRCH) {
		return false
	}
	voci, err := os.ReadDir("/proc")
	if err != nil {
		return true
	}
	for _, v := range voci {
		pid, err := strconv.Atoi(v.Name())
		if err != nil {
			continue
		}
		if stato, g, ok := statDi(pid); ok && g == gruppo && stato != 'Z' {
			return true
		}
	}
	return false
}

// aspettaMorto aspetta che pid e il suo gruppo non ci siano più (un nipote ucciso resta zombie
// finché init non lo raccoglie); se no lo uccide a mano e fallisce.
func aspettaMorto(t *testing.T, pid int) {
	t.Helper()
	fine := time.Now().Add(10 * time.Second)
	for vivoDavvero(pid) || gruppoVivo(pid) {
		if time.Now().After(fine) {
			_ = syscall.Kill(-pid, syscall.SIGKILL)
			_ = syscall.Kill(pid, syscall.SIGKILL)
			t.Errorf("pid %d (o il suo gruppo) ancora vivo dopo l'arresto: orfano", pid)
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func salute(t *testing.T, s *Supervisore) int {
	t.Helper()
	r, err := http.Get(s.Indirizzo() + "/health")
	if err != nil {
		t.Fatalf("salute: %v", err)
	}
	_ = r.Body.Close()
	return r.StatusCode
}

func TestPartenzaESalute(t *testing.T) {
	p := parti(t, finto(t, "sano"), tempiProva)
	pid := p.nuovoPronto(t)
	if salute(t, p.s) != 200 {
		t.Error("salute non 200 a sidecar pronto")
	}
	st := p.s.Stato()
	if st.Avvii != 1 || len(st.Attese) != 0 {
		t.Errorf("stato %+v", st)
	}
	// Il figlio ha un gruppo suo.
	if g, err := syscall.Getpgid(pid); err != nil || g != pid {
		t.Errorf("gruppo del figlio = %d, %v; voglio %d", g, err, pid)
	}
	p.ferma(t)
	if !strings.Contains(p.registro.String(), "sidecar: pronto") {
		t.Errorf("registro senza «pronto»:\n%s", p.registro)
	}
}

func TestAvvioLento(t *testing.T) {
	p := parti(t, finto(t, "lento"), tempiProva)
	p.nuovoPronto(t)
	if st := p.s.Stato(); st.Avvii != 1 || len(st.Attese) != 0 {
		t.Errorf("un 503 durante l'avvio non è una caduta: %+v", st)
	}
}

// Un figlio che muore riparte, con attese che raddoppiano fino al tetto.
func TestFiglioCheMuoreRiparte(t *testing.T) {
	p := parti(t, finto(t, "sano"), tempiProva)
	var attese []time.Duration
	for range 4 {
		attese = append(attese, p.cadutaVoluta(t))
	}
	want := []time.Duration{10 * time.Millisecond, 20 * time.Millisecond, 40 * time.Millisecond, 40 * time.Millisecond}
	if !slices.Equal(attese, want) {
		t.Errorf("attese dopo le cadute %v; voglio %v", attese, want)
	}
	if !strings.Contains(p.registro.String(), "segnale killed") {
		t.Errorf("registro senza l'uscita per segnale:\n%s", p.registro)
	}
}

// Un figlio rimasto pronto almeno Stabile riporta l'attesa al minimo.
func TestStabileAzzeraLAttesa(t *testing.T) {
	tm := tempiProva
	tm.Stabile = 30 * time.Millisecond
	p := parti(t, finto(t, "sano"), tm)
	p.lasciaStabile = 60 * time.Millisecond // oltre Stabile: la durata conta solo come minimo
	var attese []time.Duration
	for range 3 {
		attese = append(attese, p.cadutaVoluta(t))
	}
	want := []time.Duration{10 * time.Millisecond, 10 * time.Millisecond, 10 * time.Millisecond}
	if !slices.Equal(attese, want) {
		t.Errorf("attese dopo le cadute %v; voglio %v", attese, want)
	}
}

// Un figlio che esce subito si riprova all'infinito, con le attese che crescono fino al tetto;
// Lavoro intanto non torna (GestRip resta su).
func TestFiglioCheNonParteMai(t *testing.T) {
	p := parti(t, finto(t, "mai"), tempiProva)
	aspettaChe(t, "cinque tentativi", func() bool { return p.s.Stato().Avvii >= 5 })
	st := p.s.Stato()
	want := []time.Duration{10 * time.Millisecond, 20 * time.Millisecond, 40 * time.Millisecond, 40 * time.Millisecond}
	if len(st.Attese) < 4 || !slices.Equal(st.Attese[:4], want) {
		t.Errorf("attese %v; voglio che comincino con %v", st.Attese, want)
	}
	select {
	case <-p.fatto:
		t.Fatal("Lavoro è tornato da solo")
	default:
	}
	if !strings.Contains(p.registro.String(), "il figlio è uscito durante l'avvio") ||
		!strings.Contains(p.registro.String(), "uscita=\"codice 3\"") {
		t.Errorf("registro:\n%s", p.registro)
	}
}

func TestProgrammaCheNonCe(t *testing.T) {
	c := finto(t, "sano")
	c.Programma = filepath.Join(t.TempDir(), "non-c-e")
	p := parti(t, c, tempiProva)
	aspettaChe(t, "tre attese", func() bool { return len(p.s.Stato().Attese) >= 3 })
	if st := p.s.Stato(); st.Avvii != 0 {
		t.Errorf("avvii %d", st.Avvii)
	}
	reg := p.registro.String()
	if !strings.Contains(reg, "errore=\"non trovato\"") || strings.Contains(reg, c.Programma) {
		t.Errorf("registro:\n%s", reg)
	}
}

// Un figlio che non arriva mai a 200 si ferma dopo il tempo d'avvio e si riprova.
func TestFiglioMutoOltreLAvvio(t *testing.T) {
	tm := tempiProva
	tm.Avvio = 100 * time.Millisecond
	p := parti(t, finto(t, "muto"), tm)
	aspettaChe(t, "tre avvii", func() bool { return p.s.Stato().Avvii >= 3 })
	if !strings.Contains(p.registro.String(), "nessuna risposta di salute entro il tempo d'avvio") {
		t.Errorf("registro:\n%s", p.registro)
	}
	if st := p.s.Stato(); st.Pid != 0 {
		p.mu.Lock()
		p.pids[st.Pid] = true
		p.mu.Unlock()
	}
}

// Un figlio vivo che smette di rispondere riparte dopo Malato controlli falliti.
func TestFiglioMalatoRiparte(t *testing.T) {
	p := parti(t, finto(t, "malato"), tempiProva)
	pid := p.nuovoPronto(t)
	p.nuovoPronto(t)
	aspettaMorto(t, pid)
	p.nuovoPronto(t)
	// Un riavvio per salute mancata è una caduta come le altre: le attese raddoppiano.
	if st := p.s.Stato(); !slices.Equal(st.Attese, []time.Duration{10 * time.Millisecond, 20 * time.Millisecond}) {
		t.Errorf("attese %v", st.Attese)
	}
	// Dopo esattamente Malato (2) controlli falliti di fila.
	if !strings.Contains(p.registro.String(), "non risponde alla salute") ||
		!strings.Contains(p.registro.String(), "controlli_falliti=2") || strings.Contains(p.registro.String(), "controlli_falliti=3") {
		t.Errorf("registro:\n%s", p.registro)
	}
}

// Una porta già occupata: il figlio non parte (la salute risponderebbe per l'altro server); quando
// si libera, parte.
func TestPortaOccupata(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	c := finto(t, "sano")
	c.Porta = l.Addr().(*net.TCPAddr).Port
	altro := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) })}
	go func() { _ = altro.Serve(l) }()
	p := parti(t, c, tempiProva)
	aspettaChe(t, "tre attese", func() bool { return len(p.s.Stato().Attese) >= 3 })
	if st := p.s.Stato(); st.Avvii != 0 || st.Fase == FasePronto {
		t.Errorf("con la porta occupata: %+v", st)
	}
	if !strings.Contains(p.registro.String(), "porta occupata") {
		t.Errorf("registro:\n%s", p.registro)
	}
	if err := altro.Close(); err != nil {
		t.Fatal(err)
	}
	p.nuovoPronto(t)
}

// Alla chiusura il figlio che ignora SIGTERM riceve SIGKILL dopo Arresto.
func TestChiusuraSordo(t *testing.T) {
	tm := tempiProva
	tm.Arresto = 200 * time.Millisecond
	p := parti(t, finto(t, "sordo"), tm)
	p.nuovoPronto(t)
	inizio := time.Now()
	p.ferma(t)
	if d := time.Since(inizio); d < 200*time.Millisecond {
		t.Errorf("chiuso in %v: SIGTERM non doveva bastare", d)
	}
	if !strings.Contains(p.registro.String(), "segnale killed") {
		t.Errorf("registro:\n%s", p.registro)
	}
}

// Alla chiusura muore anche il nipote rimasto nel gruppo; e anche quando il figlio cade da solo.
func TestNienteOrfani(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "nipote")
	tm := tempiProva
	tm.Arresto = 200 * time.Millisecond
	p := parti(t, finto(t, "nonno", file), tm)
	pid := p.nuovoPronto(t)
	nipote := leggiPid(t, file)
	if g, _ := syscall.Getpgid(nipote); g != pid {
		t.Fatalf("il nipote è nel gruppo %d, non %d", g, pid)
	}
	// Il figlio cade: il nipote muore con lui.
	if err := syscall.Kill(pid, syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	aspettaMorto(t, nipote)
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	p.nuovoPronto(t)
	nipote2 := leggiPid(t, file)
	p.ferma(t)
	aspettaMorto(t, nipote2)
}

func leggiPid(t *testing.T, file string) int {
	t.Helper()
	var pid int
	aspettaChe(t, "il pid del nipote", func() bool {
		b, err := os.ReadFile(file)
		if err != nil {
			return false
		}
		pid, err = strconv.Atoi(string(b))
		return err == nil
	})
	return pid
}

// Il figlio ha solo l'ambiente del Comando (qui nessuna variabile) e la cartella «/».
func TestAmbienteDelFiglio(t *testing.T) {
	t.Setenv("GESTRIP_PROVA_SEGRETO", "non-deve-passare")
	file := filepath.Join(t.TempDir(), "ambiente")
	p := parti(t, finto(t, "ambiente", file), tempiProva)
	p.nuovoPronto(t)
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(b); got != "cartella=/\nvariabili=0" {
		t.Errorf("figlio: %q", got)
	}
	p.ferma(t)

	c := finto(t, "ambiente", file)
	c.Ambiente = []string{"CUDA_VISIBLE_DEVICES="}
	p2 := parti(t, c, tempiProva)
	p2.nuovoPronto(t)
	b, err = os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(b); got != "cartella=/\nvariabili=1\nCUDA_VISIBLE_DEVICES=" {
		t.Errorf("figlio con ambiente: %q", got)
	}
}

// Nel registro non vanno mai gli argomenti, l'ambiente o quello che il figlio scrive.
func TestRegistroSenzaDati(t *testing.T) {
	dettato := "Mario Inventato 333 0000000 anello da allargare"
	c := finto(t, "chiacchierone", dettato)
	c.Ambiente = []string{"CHIAVE_INVENTATA=sk-inventata-123"}
	p := parti(t, c, tempiProva)
	pid := p.nuovoPronto(t)
	if err := syscall.Kill(pid, syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	p.nuovoPronto(t)
	p.ferma(t)
	reg := p.registro.String()
	for _, vietato := range []string{dettato, "Inventato", "sk-inventata", "CHIAVE_INVENTATA", "sidecar-finto", os.Args[0]} {
		if strings.Contains(reg, vietato) {
			t.Errorf("il registro contiene %q:\n%s", vietato, reg)
		}
	}
	if !strings.Contains(reg, "sidecar=prova") {
		t.Errorf("registro senza il nome del sidecar:\n%s", reg)
	}
}

// Lavoro con il contesto già finito non fa partire niente.
func TestContestoGiaFinito(t *testing.T) {
	s, err := Nuovo(finto(t, "sano"), tempiProva, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, annulla := context.WithCancel(context.Background())
	annulla()
	s.Lavoro(ctx)
	if st := s.Stato(); st.Avvii != 0 || st.Fase != FaseSpento {
		t.Errorf("stato %+v", st)
	}
}

// La chiusura durante l'attesa prima di un riavvio torna subito, senza aspettare l'attesa.
func TestChiusuraDuranteLAttesa(t *testing.T) {
	tm := tempiProva
	tm.AttesaMinima, tm.AttesaMassima = time.Hour, time.Hour
	var chieste []time.Duration
	var mu sync.Mutex
	s, err := Nuovo(finto(t, "mai"), tm, nil)
	if err != nil {
		t.Fatal(err)
	}
	s.ConAttesa(func(ctx context.Context, d time.Duration) bool {
		mu.Lock()
		chieste = append(chieste, d)
		mu.Unlock()
		<-ctx.Done()
		return false
	})
	ctx, annulla := context.WithCancel(context.Background())
	fatto := make(chan struct{})
	go func() { defer close(fatto); s.Lavoro(ctx) }()
	aspettaChe(t, "l'attesa", func() bool { return s.Stato().Fase == FaseAttesa })
	annulla()
	select {
	case <-fatto:
	case <-time.After(10 * time.Second):
		t.Fatal("Lavoro non torna")
	}
	mu.Lock()
	defer mu.Unlock()
	if !slices.Equal(chieste, []time.Duration{time.Hour}) {
		t.Errorf("attese chieste %v", chieste)
	}
}

// Un controllo fallito ogni tanto, mai Malato di fila, non fa ripartire il figlio.
func TestSingoliControlliFalliti(t *testing.T) {
	p := parti(t, finto(t, "singhiozzo"), tempiProva)
	p.nuovoPronto(t)
	time.Sleep(300 * time.Millisecond) // una trentina di controlli: conta solo che siano tanti
	if st := p.s.Stato(); st.Avvii != 1 || st.Fase != FasePronto {
		t.Errorf("con controlli falliti non di fila: %+v", st)
	}
}

// Standard input, output ed errore del figlio sono /dev/null: quello che il server del modello
// scrive (può essere un dettato) non arriva al registro di GestRip (journald).
func TestUscitaDelFiglioPersa(t *testing.T) {
	p := parti(t, finto(t, "chiacchierone", "Anna Inventata"), tempiProva)
	pid := p.nuovoPronto(t)
	for fd := range 3 {
		dove, err := os.Readlink(fmt.Sprintf("/proc/%d/fd/%d", pid, fd))
		if err != nil || dove != os.DevNull {
			t.Errorf("fd %d del figlio: %q %v", fd, dove, err)
		}
	}
}

// Se GestRip muore di colpo (SIGKILL, niente arresto ordinato) il figlio muore con lui (Pdeathsig).
func TestFiglioMuoreConGestRip(t *testing.T) {
	file := filepath.Join(t.TempDir(), "figlio")
	cmd := exec.Command(os.Args[0], "sidecar-supervisore", strconv.Itoa(portaLiberaPerTest(t)), file)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	var pid int
	aspettaChe(t, "il figlio pronto", func() bool {
		b, err := os.ReadFile(file)
		if err != nil {
			return false
		}
		pid, err = strconv.Atoi(string(b))
		return err == nil && pid > 0
	})
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	aspettaMorto(t, pid)
}

// Un figlio che accetta la connessione e non risponde: ogni GET di salute finisce dopo Risposta, il
// tempo d'avvio scade e il figlio riparte (revisione della 111, B1).
func TestFiglioAppesoRiparte(t *testing.T) {
	tm := tempiProva
	tm.Avvio, tm.Risposta = 150*time.Millisecond, 50*time.Millisecond
	p := parti(t, finto(t, "appeso"), tm)
	aspettaChe(t, "tre avvii", func() bool { return p.s.Stato().Avvii >= 3 })
	if st := p.s.Stato(); st.Fase == FasePronto {
		t.Errorf("appeso ma pronto: %+v", st)
	}
	if !strings.Contains(p.registro.String(), "nessuna risposta di salute entro il tempo d'avvio") {
		t.Errorf("registro:\n%s", p.registro)
	}
}

// La chiusura durante una GET di salute appesa non aspetta Risposta: la GET segue il contesto.
func TestChiusuraDuranteUnaSaluteAppesa(t *testing.T) {
	tm := tempiProva
	tm.Avvio, tm.Risposta = time.Minute, time.Minute
	p := parti(t, finto(t, "appeso"), tm)
	aspettaChe(t, "il figlio in avvio", func() bool { return p.s.Stato().Fase == FaseAvvio })
	time.Sleep(50 * time.Millisecond) // la prima GET è partita: conta solo come minimo
	inizio := time.Now()
	p.annulla()
	select {
	case <-p.fatto:
	case <-time.After(15 * time.Second):
		t.Fatal("Lavoro aspetta la GET appesa")
	}
	if d := time.Since(inizio); d > 10*time.Second {
		t.Errorf("chiuso in %v", d)
	}
}

// Un 302 non è salute, nemmeno se porta a un 200 (revisione della 111, B6).
func TestRinvioNonESalute(t *testing.T) {
	tm := tempiProva
	tm.Avvio = 100 * time.Millisecond
	p := parti(t, finto(t, "rinvia"), tm)
	aspettaChe(t, "tre avvii", func() bool { return p.s.Stato().Avvii >= 3 })
	if !strings.Contains(p.registro.String(), "nessuna risposta di salute entro il tempo d'avvio") ||
		strings.Contains(p.registro.String(), "sidecar: pronto") {
		t.Errorf("registro:\n%s", p.registro)
	}
}

// Un figlio che ascolta su tutte le interfacce non è pronto anche se la salute dà 200 (M2).
func TestFiglioCheAscoltaOvunque(t *testing.T) {
	tm := tempiProva
	tm.Avvio = 200 * time.Millisecond
	p := parti(t, finto(t, "ovunque"), tm)
	aspettaChe(t, "due avvii", func() bool { return p.s.Stato().Avvii >= 2 })
	reg := p.registro.String()
	if strings.Contains(reg, "sidecar: pronto") || !strings.Contains(reg, "la salute non viene dal figlio") ||
		!strings.Contains(reg, "in ascolto anche fuori da 127.0.0.1") {
		t.Errorf("registro:\n%s", reg)
	}
}

// Un 200 che arriva da un processo fuori dal gruppo del figlio non vale come «pronto» (M2): qui il
// figlio cede la porta a un processo in una sessione sua.
func TestSaluteDiUnAltroProcesso(t *testing.T) {
	file := filepath.Join(t.TempDir(), "estraneo")
	tm := tempiProva
	tm.Avvio = 300 * time.Millisecond
	p := parti(t, finto(t, "cede", file), tm)
	estraneo := leggiPid(t, file)
	t.Cleanup(func() {
		// Solo se è ancora il nostro processo finto (il pid potrebbe essere stato riusato).
		if b, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", estraneo)); err == nil && bytes.Contains(b, []byte("sidecar-finto")) {
			_ = syscall.Kill(estraneo, syscall.SIGKILL)
		}
	})
	aspettaChe(t, "la salute dell'estraneo e la porta occupata", func() bool {
		r := p.registro.String()
		return strings.Contains(r, "porta occupata") && strings.Contains(r, "la salute non viene dal figlio")
	})
	reg := p.registro.String()
	if strings.Contains(reg, "sidecar: pronto") || !strings.Contains(reg, "socket di un altro processo") {
		t.Errorf("registro:\n%s", reg)
	}
	if st := p.s.Stato(); st.Fase == FasePronto {
		t.Errorf("pronto con la salute di un altro: %+v", st)
	}
}

// Il controllo del proprietario della porta, sul socket di questo stesso processo.
func TestAscoltoDelGruppo(t *testing.T) {
	porta := portaLiberaPerTest(t)
	if ok, motivo := ascoltoDelGruppo(porta, syscall.Getpgrp()); ok || motivo != "nessun socket in ascolto" {
		t.Errorf("porta vuota: %v %q", ok, motivo)
	}
	l, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(porta))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	if ok, motivo := ascoltoDelGruppo(porta, syscall.Getpgrp()); !ok {
		t.Errorf("socket nostro: %q", motivo)
	}
	if ok, motivo := ascoltoDelGruppo(porta, 1); ok || motivo != "socket di un altro processo" {
		t.Errorf("gruppo di init: %v %q", ok, motivo)
	}
	// Anche su ::1 (IPv6, se c'è): non è 127.0.0.1.
	if l6, err := net.Listen("tcp6", "[::1]:"+strconv.Itoa(porta)); err == nil {
		defer func() { _ = l6.Close() }()
		if ok, motivo := ascoltoDelGruppo(porta, syscall.Getpgrp()); ok || motivo != "in ascolto anche fuori da 127.0.0.1" {
			t.Errorf("anche su ::1: %v %q", ok, motivo)
		}
	}
}

// La lettura di /proc/net/tcp su un file inventato: stato, porta, indirizzo.
func TestSocketInAscolto(t *testing.T) {
	file := filepath.Join(t.TempDir(), "tcp")
	testo := "  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode\n" +
		"   0: " + locale4 + ":5BA0 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 111 1 0\n" +
		"   1: 00000000:5BA0 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 222 1 0\n" +
		"   2: " + locale4 + ":5BA0 " + locale4 + ":9C40 01 00000000:00000000 00:00000000 00000000  1000        0 333 1 0\n" +
		"   3: " + locale4 + ":5BA1 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 444 1 0\n"
	if err := os.WriteFile(file, []byte(testo), 0o600); err != nil {
		t.Fatal(err)
	}
	inode, altri, err := socketInAscolto(file, 0x5BA0, true)
	if err != nil || !slices.Equal(inode, []string{"111"}) || altri != 1 {
		t.Errorf("tcp: %v %d %v", inode, altri, err)
	}
	inode, altri, err = socketInAscolto(file, 0x5BA0, false)
	if err != nil || len(inode) != 0 || altri != 2 {
		t.Errorf("tcp6: %v %d %v", inode, altri, err)
	}
}
