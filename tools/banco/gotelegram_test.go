package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/L0cutus/gestrip/internal/apparecchio"
	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
	"github.com/L0cutus/gestrip/internal/uscita"
)

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

// dbGoDiProva è un database del Go migrato e vuoto, come quello che lascia l'import.
func dbGoDiProva(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "gestrip.db")
	creaDBGo(t, p)
	return p
}

func creaDBGo(t *testing.T, p string) {
	t.Helper()
	d, err := db.Apri(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Migra(context.Background(), clock.Sistema{}); err != nil {
		t.Fatal(err)
	}
	if err := d.Chiudi(); err != nil {
		t.Fatal(err)
	}
}

// La semina accende tutto quello che serve al canale dei clienti (D-03, notifiche, bot usabile, nome
// noto), con il token e le chat sintetiche, passando dalle funzioni della pagina dei provider.
func TestSeminaTelegramGo(t *testing.T) {
	p := dbGoDiProva(t)
	if err := seminaTelegramGo(p); err != nil {
		t.Fatal(err)
	}
	d, err := db.Apri(p)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Chiudi() }()
	ctx := context.Background()
	cfg, err := uscita.LeggiTelegram(ctx, d.Lettura)
	if err != nil || !cfg.Usabile() || cfg.Chat != chatNegozioBanco {
		t.Errorf("configurazione = %+v %v", cfg, err)
	}
	if ok, err := uscita.TelegramAttivo(ctx, d.Lettura); !ok || err != nil {
		t.Errorf("TelegramAttivo = %v %v", ok, err)
	}
	if ok, err := uscita.NotificheClientiAccese(ctx, d.Lettura); !ok || err != nil {
		t.Errorf("NotificheClientiAccese = %v %v", ok, err)
	}
	if nome, err := uscita.NomeBot(ctx, d.Lettura); nome != nomeBotBanco || err != nil {
		t.Errorf("NomeBot = %q %v", nome, err)
	}
	var token string
	if err := d.Lettura.QueryRowContext(ctx, `SELECT valore FROM segreti_provider WHERE nome = 'outgoing.telegram.bot_token'`).Scan(&token); err != nil || token != tokenBotBanco {
		t.Errorf("token = %q %v", token, err)
	}
	// Le righe d'audit dicono chi ha acceso: la console del banco.
	var n int
	if err := d.Lettura.QueryRowContext(ctx, `SELECT count(*) FROM eventi_audit WHERE attore = 'console' AND azione IN
		('outgoing.provider_configured', 'outgoing.channel_enabled', 'outgoing.telegram_customer_notifications')`).Scan(&n); err != nil || n != 3 {
		t.Errorf("righe d'audit = %d %v", n, err)
	}
}

// Senza il database dell'import la semina non ne crea uno vuoto.
func TestSeminaTelegramGoSenzaDatabase(t *testing.T) {
	p := filepath.Join(t.TempDir(), "gestrip.db")
	if err := seminaTelegramGo(p); err == nil {
		t.Error("semina senza database accettata")
	}
	if esiste(p) {
		t.Error("la semina ha creato il database")
	}
}

// Il token e le chat sintetiche passano le serrature del Go, e l'indirizzo pubblico e quello del bot
// sono quelli che serve accetta.
func TestFormeSintetiche(t *testing.T) {
	if !(uscita.ConfigTelegram{Token: uscita.NuovaPassword(tokenBotBanco), Chat: chatNegozioBanco}).Usabile() {
		t.Error("token o chat del negozio non usabili")
	}
	if e, err := uscita.EndpointDaAmbiente(indirizzoBotFinto, true); err != nil || e != indirizzoBotFinto {
		t.Errorf("endpoint = %q %v", e, err)
	}
	p := filepath.Join(t.TempDir(), "apparecchio.conf")
	if err := os.WriteFile(p, []byte(confGoTelegram()), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := apparecchio.Leggi(p)
	if err != nil || c.IndirizzoPubblico != indirizzoGoTelegram || c.IndirizzoInterno != "" {
		t.Errorf("configurazione = %+v %v", c, err)
	}
	for _, s := range []string{aliasBotFinto, hostGoTelegram} {
		if !strings.HasSuffix(s, ".invalid") {
			t.Errorf("%s non è .invalid", s)
		}
	}
}

func TestComandoBotFinto(t *testing.T) {
	n, _ := nomiPer("t9")
	c := comandoBotFinto(n, "/banco/bin/banco")
	s := strings.Join(c, " ")
	for _, x := range []string{"run -d ", "--name grgo-t9-bot", "--network grgo-t9-net", "--network-alias bot.invalid", "--label grgo=1",
		"--user 1000:1000", "--cap-drop ALL", "--read-only", "no-new-privileges:true",
		"type=bind,src=/banco/bin/banco,dst=/banco/banco,readonly", immagineBase + " /banco/banco bot-finto -ascolta :8081"} {
		if !strings.Contains(s, x) {
			t.Errorf("comando del bot senza %q: %s", x, s)
		}
	}
	if c[0] != "run" || strings.Contains(s, "-p ") || strings.Contains(s, "--publish") {
		t.Errorf("comando del bot: %s", s)
	}
}

// protezioni sono gli argomenti di sicurezza e i limiti del Go del confronto (cmdSu): il Go con Telegram
// deve averli tutti, e nessun container del banco pubblica una porta sull'host.
var protezioni = append([]string{"--init", "--user 1000:1000", "--cap-drop ALL", "--security-opt no-new-privileges:true", "--read-only",
	"--tmpfs /tmp:uid=1000,gid=1000,mode=1777,size=64m", "--log-opt max-size=10m", "--log-opt max-file=2"},
	strings.Join(limitiContainer[0:2], " "), strings.Join(limitiContainer[2:4], " "), strings.Join(limitiContainer[4:6], " "))

// contieneArgomenti dice se gli argomenti a hanno la sequenza x («--cap-drop ALL») come argomenti interi.
func contieneArgomenti(a []string, x string) bool {
	return strings.Contains(" "+strings.Join(a, " ")+" ", " "+x+" ")
}

// pubblicaPorte dice se un comando docker pubblica una porta sull'host.
func pubblicaPorte(a []string) bool {
	for _, x := range a {
		if x == "-p" || x == "-P" || x == "--publish" || x == "--publish-all" || strings.HasPrefix(x, "-p=") || strings.HasPrefix(x, "--publish=") {
			return true
		}
	}
	return false
}

func TestComandoGoTelegram(t *testing.T) {
	n, _ := nomiPer("t9")
	c := comandoGoTelegram(n, "/bin/gestrip")
	s := strings.Join(c, " ")
	for _, x := range []string{"run -d ", "--name grgo-t9-gotg", "--network grgo-t9-net", "--label grgo=1", "--env TELEGRAM_API_ENDPOINT=http://bot.invalid:8081",
		"type=bind,src=/bin/gestrip,dst=/gestrip,readonly",
		"type=bind,src=" + filepath.Join(n.dir, "go-telegram-data") + ",dst=/data ",
		"type=bind,src=" + filepath.Join(n.dir, "go-telegram.conf") + ",dst=/conf/apparecchio.conf,readonly",
		"/gestrip serve -db /data/gestrip.db -config /conf/apparecchio.conf -interna 0.0.0.0:47101 -pubblica 0.0.0.0:47102"} {
		if !strings.Contains(s, x) {
			t.Errorf("comando del Go con Telegram senza %q: %s", x, s)
		}
	}
	for _, x := range protezioni {
		if !contieneArgomenti(c, x) {
			t.Errorf("Go con Telegram senza la protezione %q: %s", x, s)
		}
	}
	if pubblicaPorte(c) || pubblicaPorte(comandoBotFinto(n, "/b")) {
		t.Errorf("porta pubblicata sull'host: %s", s)
	}
	// Mai i dati del Go del confronto.
	if strings.Contains(s, filepath.Join(n.dir, "go-data")) {
		t.Errorf("il Go con Telegram monta i dati dell'altro Go: %s", s)
	}
}

func TestCaddyfileConTelegram(t *testing.T) {
	n := nomi{host: "synthetic-x", app: "grgo-x-app", gox: "grgo-x-go", base: "grgo-x"}
	senza := caddyfileCon(n, false)
	if senza != caddyfile(n) || strings.Contains(senza, hostGoTelegram) {
		t.Errorf("senza Telegram:\n%s", senza)
	}
	c := caddyfileCon(n, true)
	i := strings.Index(c, "https://go-telegram.invalid:8443 {")
	if i < 0 || !strings.HasPrefix(c, caddyfile(n)) {
		t.Fatalf("con Telegram:\n%s", c)
	}
	blocco := c[i:]
	for _, x := range []string{"tls internal", "@pubblica path /r/* /stato/* /foto/* /thumb/* /m/* /privacy\n",
		"handle @pubblica {\n        reverse_proxy grgo-x-gotg:47102", "handle {\n        reverse_proxy grgo-x-gotg:47101"} {
		if !strings.Contains(blocco, x) {
			t.Errorf("blocco senza %q:\n%s", x, blocco)
		}
	}
	if strings.Contains(blocco, "grgo-x-go:") {
		t.Errorf("il blocco va al Go del confronto:\n%s", blocco)
	}
}

func TestContainerTelegramDaTogliere(t *testing.T) {
	for _, s := range []string{"-bot", "-gotg"} {
		if !slices.Contains(suffissiContainer(), s) {
			t.Errorf("giu non toglie %s", s)
		}
	}
}

func TestIstanzeDaFlag(t *testing.T) {
	c, _ := clientCon("")
	if tg, err := istanzeDaFlag("", "", c); err != nil || tg.Go.Base != "" || tg.Bot.Base != "" {
		t.Errorf("senza = %+v %v", tg, err)
	}
	for _, x := range [][2]string{{indirizzoGoTelegram, ""}, {"", indirizzoBotFinto}} {
		if _, err := istanzeDaFlag(x[0], x[1], c); err == nil {
			t.Errorf("%v accettato", x)
		}
	}
	tg, err := istanzeDaFlag(indirizzoGoTelegram, indirizzoBotFinto, c)
	if err != nil || tg.Go.Base != indirizzoGoTelegram || tg.Bot.Base != indirizzoBotFinto || tg.Go.Client != c || tg.Bot.Client != c {
		t.Errorf("con = %+v %v", tg, err)
	}
}

// esegui porta a confronta il Go con Telegram e il bot, e li aspetta, solo se l'istanza li ha.
func TestComandiEseguiConTelegram(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GESTRIP_BANCO_RUN", dir)
	n, _ := nomiPer("t0")
	for _, f := range []string{"banco", "dev.json"} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("[]"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "scen"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, con := range []bool{false, true} {
		cmds, err := comandiEseguiCon(n, filepath.Join(dir, "banco"), filepath.Join(dir, "scen"), filepath.Join(dir, "dev.json"), "", datiSinteticiImport, con)
		if err != nil {
			t.Fatal(err)
		}
		attendi, conf := strings.Join(cmds[0], " "), strings.Join(cmds[1], " ")
		for _, x := range []string{indirizzoGoTelegram + "/healthz", indirizzoBotFinto + "/_prova/salute"} {
			if strings.Contains(attendi, x) != con {
				t.Errorf("con=%v attendi %q: %s", con, x, attendi)
			}
		}
		for _, x := range []string{"-go-telegram " + indirizzoGoTelegram, "-bot " + indirizzoBotFinto} {
			if strings.Contains(conf, x) != con {
				t.Errorf("con=%v confronta %q: %s", con, x, conf)
			}
		}
	}
}

// runtime.json dice se l'istanza ha il Go con Telegram; senza runtime.json è un errore.
func TestTelegramDiRuntime(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GESTRIP_BANCO_RUN", dir)
	n, _ := nomiPer("t0")
	if _, err := telegramDiRuntime(n); err == nil {
		t.Error("senza runtime.json")
	}
	if err := os.MkdirAll(n.dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, x := range []struct {
		rt  string
		con bool
		err bool
	}{
		{`{"sintetica":true}`, false, false},
		{`{"sintetica":true,"go_telegram":"` + indirizzoGoTelegram + `","bot":"` + indirizzoBotFinto + `"}`, true, false},
		{`{"sintetica":true,"go_telegram":"https://altro.invalid:8443","bot":"` + indirizzoBotFinto + `"}`, false, true},
		{`{"sintetica":true,"go_telegram":"` + indirizzoGoTelegram + `"}`, false, true},
		{`non json`, false, true},
	} {
		if err := os.WriteFile(filepath.Join(n.dir, "runtime.json"), []byte(x.rt), 0o600); err != nil {
			t.Fatal(err)
		}
		con, err := telegramDiRuntime(n)
		if con != x.con || (err != nil) != x.err {
			t.Errorf("%s = %v %v", x.rt, con, err)
		}
	}
}

// preparaGoTelegram copia i dati del Go dopo l'import (database e media), semina le impostazioni nella
// copia e scrive la configurazione dell'apparecchio; il Go del confronto resta com'era.
func TestPreparaGoTelegram(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GESTRIP_BANCO_RUN", dir)
	n, _ := nomiPer("t0")
	goData := filepath.Join(n.dir, "go-data")
	if err := os.MkdirAll(filepath.Join(goData, "media", "ab"), 0o700); err != nil {
		t.Fatal(err)
	}
	creaDBGo(t, filepath.Join(goData, "gestrip.db"))
	if err := os.WriteFile(filepath.Join(goData, "media", "ab", "foto"), []byte("foto sintetica"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := preparaGoTelegram(n); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(filepath.Join(n.dir, cartellaGoTelegram, "media", "ab", "foto")); err != nil || string(b) != "foto sintetica" {
		t.Errorf("media copiati: %q %v", b, err)
	}
	if b, err := os.ReadFile(filepath.Join(n.dir, fileConfGoTelegram)); err != nil || string(b) != confGoTelegram() {
		t.Errorf("configurazione: %q %v", b, err)
	}
	if fi, err := os.Stat(filepath.Join(n.dir, fileConfGoTelegram)); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("permessi della configurazione: %v %v", fi, err)
	}
	attivo := func(p string) bool {
		d, err := db.Apri(p)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = d.Chiudi() }()
		ok, err := uscita.TelegramAttivo(context.Background(), d.Lettura)
		if err != nil {
			t.Fatal(err)
		}
		return ok
	}
	if !attivo(filepath.Join(n.dir, cartellaGoTelegram, "gestrip.db")) {
		t.Error("la copia non ha Telegram acceso")
	}
	if attivo(filepath.Join(goData, "gestrip.db")) {
		t.Error("il Go del confronto ha Telegram acceso")
	}
	// Una seconda volta non sovrascrive.
	if err := preparaGoTelegram(n); err == nil {
		t.Error("seconda preparazione accettata")
	}
}

// Una riga Telegram già in coda ferma la semina (D-27): il banco non manda al bot finto messaggi
// importati.
func TestSeminaTelegramGoConRigheInCoda(t *testing.T) {
	p := dbGoDiProva(t)
	d, err := db.Apri(p)
	if err != nil {
		t.Fatal(err)
	}
	c := uscita.Nuova(d, clock.Sistema{}, nil)
	if _, err := c.SalvaTelegram(context.Background(), uscita.ModuloTelegram{"bot_token": tokenBotBanco, "chat_id": chatNegozioBanco}, auditBanco); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Scrittura.Exec(`INSERT INTO messaggi_in_uscita (canale, destinatario, indirizzo, testo, prossimo_tentativo_il, accodato_il)
		VALUES ('telegram', 'shop', '-1', 'x', '2026-01-01T00:00:00.000000Z', '2026-01-01T00:00:00.000000Z')`); err != nil {
		t.Fatal(err)
	}
	if err := d.Chiudi(); err != nil {
		t.Fatal(err)
	}
	if err := seminaTelegramGo(p); !errors.Is(err, uscita.ErrTelegramInSospeso) {
		t.Errorf("semina con righe in coda = %v", err)
	}
}
