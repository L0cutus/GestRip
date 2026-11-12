package web

import (
	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/clock"
)

// Il riquadro «📧 Avviso email» della scheda (#repair-email-notice di RepairLive, EmailNotice.sheet/1,
// slice 070b): l'indirizzo che la controparte ha chiesto dalla pagina del cliente, il suo consenso, e
// l'email di pronta (CLI-42) con le parole della coda, mai «consegnata». In più, solo Go (D-16): con
// l'email non disponibile la riga che promette l'invio diventa quella che dice che non partirà.

// emailScheda è il riquadro, nil senza indirizzo.
type emailScheda struct {
	Indirizzo     string
	Consenso      string // «In attesa di conferma» o «Confermato»
	PartiraDaSola bool   // #repair-email-ready-pending
	NonPartira    bool   // #repair-email-ready-off (D-16, solo Go)
	Pronta        string // #repair-email-ready, "" senza
}

// emailDellaScheda è il riquadro della busta s: la promessa «partirà da sola» solo per un indirizzo
// confermato della controparte, prima di ogni email di pronta e fuori da «pronta», come lo standalone.
func emailDellaScheda(s buste.Scheda) *emailScheda {
	e := s.Email
	if e == nil {
		return nil
	}
	r := &emailScheda{Indirizzo: e.Indirizzo, Consenso: "In attesa di conferma"}
	if e.Consenso == "confirmed" {
		r.Consenso = "Confermato"
	}
	promessa := e.Consenso == "confirmed" && e.Controparte && e.Pronta == nil && s.Stato != "pronta"
	r.PartiraDaSola, r.NonPartira = promessa && e.Disponibile, promessa && !e.Disponibile
	if e.Pronta != nil {
		r.Pronta = testoEmailPronta(*e.Pronta)
	}
	return r
}

// testoEmailPronta è email_ready_text/1, con l'ora del negozio.
func testoEmailPronta(p buste.EmailPronta) string {
	riparte := ". Riparte al prossimo passaggio a «pronta»."
	switch p.Stato {
	case "queued":
		return "Email di pronta: " + statoInvio("email", p.Stato) + " dal " + clock.Timbro(p.Il)
	case "accepted":
		return "Email di pronta: " + statoInvio("email", p.Stato) + " il " + clock.Timbro(p.Il)
	case "failed":
		return "Email di pronta: " + statoInvio("email", p.Stato) + " il " + clock.Timbro(p.Il) + riparte
	case "discarded":
		return "Email di pronta: tolta dalla coda dal titolare."
	case "sent":
		return "Email di pronta inviata dal gestionale precedente il " + clock.Timbro(p.Il)
	case "not_queued":
		return "Email di pronta non accodata: " + motivoEmailPronta(p.Motivo) + riparte
	}
	return ""
}

// motivoEmailPronta è email_miss/1.
func motivoEmailPronta(motivo string) string {
	switch motivo {
	case "not_configured":
		return "posta SMTP non configurata"
	case "invalid_template":
		return "testo dell'email non valido nelle Impostazioni"
	}
	return "errore"
}
