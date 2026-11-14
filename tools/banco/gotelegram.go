package main

// Il banco con il canale Telegram acceso (slice 074b, piano della ricevuta 074): solo dalla parte del Go,
// perché lo standalone del banco non ha il bot e la sua immagine non cambia.
//
// Con -dati sintetici-import, banco su aggiunge all'istanza:
//   - il bot finto (botfinto.go), container grgo-<nome>-bot con l'alias bot.invalid, dal binario del
//     banco stesso (quello che sta facendo `su`);
//   - un secondo Go, grgo-<nome>-gotg, su una copia dei dati del Go fatta subito dopo l'import (prima che
//     parta qualunque cosa), con TELEGRAM_API_ENDPOINT=http://bot.invalid:8081, indirizzo_pubblico
//     https://go-telegram.invalid:8443 e le impostazioni seminate con le funzioni della pagina dei
//     provider: token e chat del negozio sintetici, «Notifiche Telegram ai clienti» accese, «Invio
//     Telegram» acceso (D-03; si rifiuta con un messaggio Telegram già in coda, D-27) e il nome del bot;
//   - nel Caddy il sito https://go-telegram.invalid:8443 (stesse facce del Go del confronto) con il suo
//     alias.
//
// Il Go del confronto resta com'era (Telegram spento): gli scenari di sempre non cambiano. Gli scenari con
// "istanza": "go-telegram" (solo passi solo_go e «bot») girano sul secondo Go e sul bot finto
// (esegui.go). banco giu toglie i due container con gli altri; i dati stanno nella cartella dell'istanza.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
	"github.com/L0cutus/gestrip/internal/uscita"
)

const (
	aliasBotFinto       = "bot.invalid"
	portaBotFinto       = "8081"
	indirizzoBotFinto   = "http://" + aliasBotFinto + ":" + portaBotFinto
	hostGoTelegram      = "go-telegram.invalid"
	indirizzoGoTelegram = "https://" + hostGoTelegram + ":8443"
	// tokenBotBanco, chatNegozioBanco e chatClienteBanco sono inventati e hanno la forma che il Go
	// accetta (seconda serratura della 071). Nessun bot vero li conosce.
	tokenBotBanco          = "4242:banco-finto-SINTETICO"
	chatNegozioBanco       = "-4242000"
	chatClienteBanco int64 = 4242001
	nomeBotBanco           = "BancoSinteticoBot"
	// cartellaGoTelegram e fileConfGoTelegram stanno nella cartella dell'istanza.
	cartellaGoTelegram = "go-telegram-data"
	fileConfGoTelegram = "go-telegram.conf"
	// istanzaGoTelegram è il valore di "istanza" degli scenari che girano sul Go con Telegram.
	istanzaGoTelegram = "go-telegram"
)

// auditBanco è chi accende Telegram nella copia: la console, come la semina del primo titolare.
var auditBanco = audit.Contesto{Attore: "console"}

// eseguibileBanco è il binario del banco che il bot finto usa: lo stesso che fa `su` (un gancio per i test).
var eseguibileBanco = os.Executable

// confGoTelegram è il file di configurazione dell'apparecchio del Go con Telegram: l'indirizzo pubblico
// è quello del suo sito nel Caddy, così il pulsante dei messaggi porta alla pagina vera della busta.
func confGoTelegram() string {
	return "# banco 074b: Go con il canale Telegram acceso\nindirizzo_pubblico = " + indirizzoGoTelegram + "\n"
}

// seminaTelegramGo accende Telegram nel database (che deve esserci: l'import l'ha scritto) con le funzioni
// della pagina dei provider, come farebbe il titolare: bot e chat del negozio, notifiche ai clienti, invio
// Telegram (rifiutato con righe Telegram in coda, D-27) e il nome del bot, che la prova di getMe salverebbe.
func seminaTelegramGo(percorso string) error {
	if _, err := os.Lstat(percorso); err != nil {
		return fmt.Errorf("database del Go con Telegram: %w", err)
	}
	d, err := db.Apri(percorso)
	if err != nil {
		return err
	}
	defer func() { _ = d.Chiudi() }()
	ctx := context.Background()
	c := uscita.Nuova(d, clock.Sistema{}, nil)
	rif, err := c.SalvaTelegram(ctx, uscita.ModuloTelegram{"bot_token": tokenBotBanco, "chat_id": chatNegozioBanco}, auditBanco)
	switch {
	case err != nil:
		return err
	case rif != nil:
		return fmt.Errorf("configurazione Telegram rifiutata: %v", rif.Errori)
	}
	if err := c.ImpostaNotificheClienti(ctx, true, auditBanco); err != nil {
		return err
	}
	if err := c.ImpostaTelegram(ctx, true, auditBanco); err != nil {
		return fmt.Errorf("invio Telegram non acceso: %w", err)
	}
	_, err = d.Scrittura.ExecContext(ctx, `INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES (?, ?, ?)
		ON CONFLICT (chiave) DO UPDATE SET valore = excluded.valore, aggiornata_il = excluded.aggiornata_il`,
		uscita.ChiaveNomeBot, nomeBotBanco, clock.Sistema{}.Adesso().UTC().Format(db.FormatoIstante))
	return err
}

// preparaGoTelegram copia i dati del Go dopo l'import (database e media), accende Telegram nella copia e
// scrive la configurazione dell'apparecchio. Va chiamata con il Go fermo, prima di avviarlo.
func preparaGoTelegram(n nomi) error {
	dst := filepath.Join(n.dir, cartellaGoTelegram)
	if err := dentroIstanza(n, dst); err != nil {
		return err
	}
	if _, _, err := copiaAlbero(filepath.Join(n.dir, "go-data"), dst); err != nil {
		return fmt.Errorf("copia dei dati del Go: %w", err)
	}
	if err := seminaTelegramGo(filepath.Join(dst, "gestrip.db")); err != nil {
		return err
	}
	return scriviPrivato(filepath.Join(n.dir, fileConfGoTelegram), confGoTelegram())
}

// comandoBotFinto avvia il bot finto nella rete interna, con l'alias bot.invalid, dal binario del banco
// in sola lettura; nessuna porta sull'host.
func comandoBotFinto(n nomi, banco string) []string {
	return []string{"run", "-d", "--name", n.base + "-bot", "--network", n.rete, "--network-alias", aliasBotFinto, "--label", "grgo=1",
		"--init", "--user", "1000:1000", "--cap-drop", "ALL", "--security-opt", "no-new-privileges:true", "--read-only",
		"--cpus", "1", "--memory", "256m", "--pids-limit", "64",
		"--log-opt", "max-size=10m", "--log-opt", "max-file=2",
		"--mount", "type=bind,src=" + banco + ",dst=/banco/banco,readonly",
		immagineBase, "/banco/banco", "bot-finto", "-ascolta", ":" + portaBotFinto}
}

// comandoGoTelegram avvia il Go con Telegram come il Go del confronto, sui suoi dati, con il bot finto e
// la sua configurazione.
func comandoGoTelegram(n nomi, gestrip string) []string {
	a := []string{"run", "-d", "--name", n.base + "-gotg", "--network", n.rete, "--label", "grgo=1",
		"--init", "--user", "1000:1000", "--cap-drop", "ALL", "--security-opt", "no-new-privileges:true",
		"--read-only", "--tmpfs", "/tmp:uid=1000,gid=1000,mode=1777,size=64m",
		"--log-opt", "max-size=10m", "--log-opt", "max-file=2",
		"--env", "TELEGRAM_API_ENDPOINT=" + indirizzoBotFinto,
		"--mount", "type=bind,src=" + gestrip + ",dst=/gestrip,readonly",
		"--mount", "type=bind,src=" + filepath.Join(n.dir, cartellaGoTelegram) + ",dst=/data",
		"--mount", "type=bind,src=" + filepath.Join(n.dir, fileConfGoTelegram) + ",dst=/conf/apparecchio.conf,readonly",
		"--mount", "type=bind,src=" + filepath.Join(n.dir, "stampa-finta") + ",dst=" + cartellaStampaFinta + ",readonly"}
	a = append(a, limitiContainer...)
	return append(a, immagineBase, "/gestrip", "serve", "-db", "/data/gestrip.db", "-config", "/conf/apparecchio.conf",
		"-interna", "0.0.0.0:47101", "-pubblica", "0.0.0.0:47102", "-senza-volume-cifrato", "-ascolto-non-locale",
		"-comandi-stampa", cartellaStampaFinta)
}

// caddyfileCon è caddyfile più, con conTelegram, il sito del Go con Telegram (le stesse due facce).
func caddyfileCon(n nomi, conTelegram bool) string {
	c := caddyfile(n)
	if conTelegram {
		c += "\n" + sitoGo(indirizzoGoTelegram, n.base+"-gotg")
	}
	return c
}

// telegramDiRuntime dice se banco su ha creato il Go con Telegram (runtime.json, go_telegram e bot): solo
// con gli indirizzi che su scrive.
func telegramDiRuntime(n nomi) (bool, error) {
	b, err := leggiPiccolo(filepath.Join(n.dir, "runtime.json"), 1<<20)
	if err != nil {
		return false, fmt.Errorf("runtime.json dell'istanza (prima banco su -nome …): %w", err)
	}
	var rt struct {
		GoTelegram *string `json:"go_telegram"`
		Bot        *string `json:"bot"`
	}
	if err := json.Unmarshal(b, &rt); err != nil {
		return false, fmt.Errorf("runtime.json dell'istanza: %w", err)
	}
	switch {
	case rt.GoTelegram == nil && rt.Bot == nil:
		return false, nil
	case rt.GoTelegram == nil || rt.Bot == nil || *rt.GoTelegram != indirizzoGoTelegram || *rt.Bot != indirizzoBotFinto:
		return false, errors.New("runtime.json: go_telegram e bot non sono quelli di banco su")
	}
	return true, nil
}

// istanzeDaFlag sono le istanze go-telegram di confronta: tutte e due o nessuna.
func istanzeDaFlag(goTg, bot string, c *http.Client) (istanzeTelegram, error) {
	switch {
	case goTg == "" && bot == "":
		return istanzeTelegram{}, nil
	case goTg == "" || bot == "":
		return istanzeTelegram{}, errors.New("-go-telegram e -bot vanno insieme")
	}
	return istanzeTelegram{Go: istanza{"go-telegram", goTg, c}, Bot: istanza{"bot", bot, c}}, nil
}
