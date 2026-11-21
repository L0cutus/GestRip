package sidecar

import (
	"bufio"
	"bytes"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Porte che la corsa vera non usa mai: llm-switch, gestrip-switch e i loro servizi su pc-fisso.
var porteVietate = map[int]bool{43651: true, 43653: true, 43660: true, 43661: true, 43662: true, 43671: true}

// TestSidecarVero è la corsa vera della slice 111, solo a mano (mai in scripts/check né in CI):
// parte solo con GESTRIP_SIDECAR_VERO (programma e argomenti, con {porta}) e
// GESTRIP_SIDECAR_VERO_PORTA (sopra 47000). Il comando esatto, sotto systemd-run con limiti, è
// nella ricevuta docs/slices/111-sidecar.md. Fa: avvio fino alla salute 200, una domanda breve
// inventata a /v1/chat/completions, il figlio ucciso e ripartito, la chiusura senza orfani; scrive
// tempi e memoria (VmRSS, VmHWM) del figlio, mai la risposta del modello.
func TestSidecarVero(t *testing.T) {
	riga := os.Getenv("GESTRIP_SIDECAR_VERO")
	if riga == "" {
		t.Skip("corsa vera solo a mano: GESTRIP_SIDECAR_VERO e GESTRIP_SIDECAR_VERO_PORTA")
	}
	porta, err := strconv.Atoi(os.Getenv("GESTRIP_SIDECAR_VERO_PORTA"))
	if err != nil || porta <= 47000 || porta > 65535 || porteVietate[porta] {
		t.Fatalf("GESTRIP_SIDECAR_VERO_PORTA deve essere una porta sopra 47000, non di gestrip-switch o llm-switch")
	}
	campi := strings.Fields(riga)
	c := Comando{Nome: "estrazione", Programma: campi[0], Argomenti: campi[1:], Porta: porta,
		// Solo CPU: nessuna GPU visibile al figlio, anche se il binario ha il backend CUDA.
		Ambiente: []string{"CUDA_VISIBLE_DEVICES=", "HIP_VISIBLE_DEVICES=", "GGML_VK_VISIBLE_DEVICES="}}
	tm := Tempi{Avvio: 3 * time.Minute, PassoAvvio: 250 * time.Millisecond, Controllo: time.Second,
		AttesaMinima: time.Second, AttesaMassima: 4 * time.Second, Arresto: 10 * time.Second}
	p := partiCon(t, c, tm, os.Stderr)
	inizio := time.Now()
	pid := p.nuovoPronto(t)
	t.Logf("pronto in %v (pid %d)", time.Since(inizio).Round(time.Millisecond), pid)
	memoria(t, pid)

	corpo := `{"model":"x","temperature":0,"stream":false,"max_tokens":64,` +
		`"messages":[{"role":"user","content":"Oggi: 21/11/2026\nDettato: Anna Inventata, anello in oro da allargare di una misura, pronto venerdì"}]}`
	cl := &http.Client{Timeout: 90 * time.Second, Transport: &http.Transport{Proxy: nil, DisableKeepAlives: true}}
	for i := range 2 {
		t0 := time.Now()
		r, err := cl.Post(p.s.Indirizzo()+"/v1/chat/completions", "application/json", strings.NewReader(corpo))
		if err != nil {
			t.Fatalf("domanda %d: %v", i+1, err)
		}
		b, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		_ = r.Body.Close()
		t.Logf("domanda %d: HTTP %d, %d byte, %v", i+1, r.StatusCode, len(b), time.Since(t0).Round(time.Millisecond))
		if r.StatusCode != 200 || !bytes.Contains(b, []byte(`"choices"`)) {
			t.Errorf("domanda %d: risposta non valida (HTTP %d)", i+1, r.StatusCode)
		}
	}
	memoria(t, pid)

	if err := syscall.Kill(pid, syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	t0 := time.Now()
	pid2 := p.nuovoPronto(t)
	t.Logf("ripartito in %v (pid %d), attese %v", time.Since(t0).Round(time.Millisecond), pid2, p.s.Stato().Attese)

	t0 = time.Now()
	p.ferma(t)
	t.Logf("fermato in %v, nessun figlio rimasto", time.Since(t0).Round(time.Millisecond))
	if !strings.Contains(p.registro.String(), "sidecar: pronto") {
		t.Errorf("registro:\n%s", p.registro)
	}
	for _, vietato := range []string{"Inventata", "anello"} {
		if strings.Contains(p.registro.String(), vietato) {
			t.Errorf("il registro contiene il dettato")
		}
	}
	t.Logf("registro del supervisore:\n%s", p.registro)
}

// memoria scrive VmRSS e VmHWM del figlio.
func memoria(t *testing.T, pid int) {
	t.Helper()
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/status")
	if err != nil {
		t.Logf("memoria del figlio: %v", err)
		return
	}
	var righe []string
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		if l := sc.Text(); strings.HasPrefix(l, "VmRSS:") || strings.HasPrefix(l, "VmHWM:") {
			righe = append(righe, strings.Join(strings.Fields(l), " "))
		}
	}
	t.Logf("memoria del figlio: %s", strings.Join(righe, ", "))
}
