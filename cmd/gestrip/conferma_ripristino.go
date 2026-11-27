package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/backup"
	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
	"github.com/L0cutus/gestrip/internal/segreti"
)

// gestrip conferma-ripristino (slice 131, OPS-19, D-12, D-118): la conferma «questa è ora la macchina vera» dalla
// console, la stessa della pagina del titolare (backup.ConfermaRipristino). Il nome segue gli altri comandi del
// backup in italiano (verifica-backup, prova-backup, chiave-backup): verbo e cosa. Chiede, in ordine, di aver spento
// la macchina vecchia, poi per ogni messaggio in coda «manda» o «scarta» (Invio = scarta, la proposta della D-118
// (b)), poi la parola. Una risposta non capita si richiede; l'input che finisce o Ctrl-C prima della parola non
// cambiano niente. Il serve che gira resta isolato fino al riavvio: il comando lo dice.

type opzioniConfermaRipristino struct{ DB string }

var errUsoConfermaRipristino = errors.New("uso: gestrip conferma-ripristino [-db FILE]")

// errConfermaInterrotta: l'input è finito prima della parola.
var errConfermaInterrotta = errors.New("conferma interrotta: niente è cambiato, la copia resta isolata")

func opzioniDiConfermaRipristino(args []string) (opzioniConfermaRipristino, error) {
	fs := flag.NewFlagSet("conferma-ripristino", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	o := opzioniConfermaRipristino{}
	fs.StringVar(&o.DB, "db", "/var/lib/gestrip/gestrip.db", "file del database della copia ripristinata")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return opzioniConfermaRipristino{}, errUsoConfermaRipristino
	}
	return o, nil
}

// orologioConferma è l'orologio della conferma (audit, impostazioni rimesse); una variabile per i test.
var orologioConferma clock.Orologio = clock.Sistema{}

// lettoreRighe legge le risposte una riga alla volta; lo stop (Ctrl-C) interrompe l'attesa.
type lettoreRighe struct {
	r *bufio.Reader
}

func (l lettoreRighe) riga(ctx context.Context) (string, error) {
	type letta struct {
		riga string
		err  error
	}
	fatto := make(chan letta, 1)
	go func() {
		riga, err := l.r.ReadString('\n')
		fatto <- letta{riga, err}
	}()
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case x := <-fatto:
		if x.err != nil && (!errors.Is(x.err, io.EOF) || x.riga == "") {
			if errors.Is(x.err, io.EOF) {
				return "", errConfermaInterrotta
			}
			return "", x.err
		}
		return strings.TrimSpace(x.riga), nil
	}
}

// confermaRipristino è gestrip conferma-ripristino: domande su out, risposte da in.
func confermaRipristino(ctx context.Context, o opzioniConfermaRipristino, out io.Writer, in io.Reader) (err error) {
	if err := segreti.ControllaAmbiente(os.LookupEnv); err != nil {
		return err
	}
	if err := databaseEsistente(o.DB); err != nil {
		return err
	}
	d, err := db.Apri(o.DB)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, d.Chiudi()) }()
	st, err := backup.LeggiStatoRipristino(ctx, d.Lettura)
	if err != nil {
		return fmt.Errorf("stato del ripristino: %w", err)
	}
	if !st.Isolata {
		return backup.ErrNonIsolata
	}
	if in == nil {
		in = strings.NewReader("")
	}
	l := lettoreRighe{bufio.NewReader(in)}
	riaccendi := strings.Join(st.DaRiaccendere, ", ")
	if riaccendi == "" {
		riaccendi = "niente (nel backup email, Telegram e copia remota erano spenti)"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "copia isolata ripristinata il %s dal backup %s (dati del %s)\n", st.Isolamento.RipristinatoIl, st.Isolamento.Archivio,
		st.Isolamento.CatturatoIl)
	fmt.Fprintf(&b, "alla conferma si riaccende: %s\n", riaccendi)
	b.WriteString("PRIMA DI CONFERMARE: spegni la macchina vecchia (o staccala dalla rete) e assicurati che non si riaccenda; " +
		"con tutte e due accese i clienti riceverebbero avvisi doppi\n")
	if _, err := io.WriteString(out, b.String()); err != nil {
		return err
	}
	s := backup.SceltaConferma{}
	if len(st.Coda) == 0 {
		if _, err := io.WriteString(out, "nessun messaggio in coda\n"); err != nil {
			return err
		}
	} else {
		if _, err := fmt.Fprintf(out, "%d messaggi in coda: la macchina vecchia potrebbe averli già mandati; nel dubbio scarta\n", len(st.Coda)); err != nil {
			return err
		}
	}
	for _, m := range st.Coda {
		var mb strings.Builder
		fmt.Fprintf(&mb, "\nmessaggio %d: %s a %s (%s), in coda dal %s", m.ID, m.Canale, m.Destinatario, m.Indirizzo, clock.Timbro(m.AccodatoIl))
		if m.NumeroBusta != "" {
			fmt.Fprintf(&mb, ", busta %s", m.NumeroBusta)
		}
		mb.WriteString("\n")
		if m.Oggetto != "" {
			fmt.Fprintf(&mb, "oggetto: %s\n", m.Oggetto)
		}
		mb.WriteString(m.Testo + "\n")
		mb.WriteString("(con «manda», se il canale di questo messaggio è spento, resta in coda finché non lo riaccendi)\n")
		if _, err := io.WriteString(out, mb.String()); err != nil {
			return err
		}
		for {
			if _, err := io.WriteString(out, "manda o scarta? [Invio = scarta, m = manda]: "); err != nil {
				return err
			}
			r, err := l.riga(ctx)
			if err != nil {
				return err
			}
			switch strings.ToLower(r) {
			case "", "s", "scarta":
				s.Scarta = append(s.Scarta, m.ID)
			case "m", "manda":
				s.Manda = append(s.Manda, m.ID)
			default:
				if _, err := io.WriteString(out, "risposta non capita: scrivi m per mandare, oppure premi Invio per scartare\n"); err != nil {
					return err
				}
				continue
			}
			break
		}
	}
	if _, err := fmt.Fprintf(out, "\nper confermare che questa è ora la macchina vera scrivi %s e premi Invio: ", backup.ParolaDiConferma); err != nil {
		return err
	}
	if s.Parola, err = l.riga(ctx); err != nil {
		return err
	}
	es, err := backup.ConfermaRipristino(ctx, d, orologioConferma, s, audit.Contesto{Attore: "console"})
	if err != nil {
		return err
	}
	riaccesi := strings.Join(es.Riaccesi, ", ")
	if riaccesi == "" {
		riaccesi = "niente"
	}
	_, err = fmt.Fprintf(out, "\nRIPRISTINO CONFERMATO: questa è ora la macchina vera\nriacceso: %s\nmessaggi: %d lasciati in coda "+
		"(partono dopo il riavvio), %d scartati\nora riavvia GestRip (gestrip serve): solo il serve nuovo riaccende email, Telegram, "+
		"lavori automatici e backup\nil registro dei backup è vuoto: la prima notte dopo il riavvio si fa un backup nuovo\n",
		riaccesi, es.Mandati, es.Scartati)
	return err
}
