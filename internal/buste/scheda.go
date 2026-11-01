package buste

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/db"
)

// L'apertura della scheda della busta (RIP-S-01, RIP-S-02), come RepairLive.mount/3 dello
// standalone (go-baseline), in sola lettura:
//   - la busta si cerca come Workshop.get_repair/1: un id che non è un intero positivo
//     (GestripStandalone.Id.positive/1) e una busta nel cestino (il filtro globale di
//     Repo.prepare_query/3) sono «non trovata»;
//   - ogni apertura scrive repair.viewed (Workshop.record_repair_event/3); se non si scrive, la
//     busta non si apre;
//   - poi la conversazione e le risposte al preventivo si segnano lette
//     (RepairMessages.mark_read/2, Notifications.mark_quote_read/2), ognuna con la sua riga
//     d'audit e solo se il segno cambia; un segno che non si scrive non ferma l'apertura.
// Il destinatario della busta è Workshop.Recipient.resolve/1.

// ErrBustaNonTrovata: id che non è un intero positivo, busta che non c'è o nel cestino.
var ErrBustaNonTrovata = errors.New("buste: busta non trovata")

// ErrAperturaNonRiuscita: l'apertura non si è potuta registrare (repair.viewed).
var ErrAperturaNonRiuscita = errors.New("buste: apertura della busta non registrata")

// Parte è chi, fuori dal negozio, sta dall'altra parte della busta (Recipient.party/1).
type Parte int

const (
	ParteCliente    Parte = iota // busta diretta: il cliente
	ParteOreficeria              // busta B2B: l'oreficeria, mai il cliente finale
)

// Destinatario è l'unico recapito della busta: nome e telefono senza spazi ai bordi.
type Destinatario struct {
	Parte          Parte
	Nome, Telefono string
}

// Oreficeria è l'oreficeria della busta, anche la riga 1 («il negozio»).
type Oreficeria struct {
	Nome     string
	Telefono *string
}

// Scheda è la busta come la legge la scheda. Le date sono AAAA-MM-GG; nil è NULL.
type Scheda struct {
	ID                             int64
	Numero                         string
	CompraOro                      bool
	DataAccettazione               string
	NomeCliente, TelefonoCliente   string // le copie salvate sulla busta
	Stato                          string
	Descrizione                    string
	NotePubbliche, NoteInterne     *string
	Artigiano                      *string // il nome
	ArtigianoID                    *int64
	Preventivo, PrezzoFinale       Prezzo
	OreficeriaID                   *int64
	Oreficeria                     *Oreficeria
	NomeNegozio                    string // per «Diretta <negozio>» (OnlineSales.shop_name/0)
	Consegna                       Consegna
	DataPronta, DataConsegna       *string
	CreataIl                       time.Time
	CodiceQR                       *string
	GiorniInPiu                    int64 // «Giorni in più» della stima dei tempi
	FuoriBottega                   bool  // presso un artigiano adesso (Repair.out_of_shop?/1)
	PreventivoAccettatoIl          *time.Time
	PreventivoRifiutatoIl          *time.Time
	AttesaCliente                  bool    // in attesa della risposta del cliente (RIP-S-15)
	AttesaDal, AttesaMotivo        *string // come stanno nel database: «dal» è un testo
	ultimoMessaggio, messaggiLetti *string // per il segno «letto», come stanno nel database
	accettato, accettatoLetto      *string
	rifiutato, rifiutatoLetto      *string
}

// B2B dice se la busta è di un'oreficeria esterna (Oreficeria.is_b2b/1: id > 1).
func (s Scheda) B2B() bool { return s.OreficeriaID != nil && *s.OreficeriaID > 1 }

// Destinatario è Recipient.resolve/1: su una busta B2B l'oreficeria, altrimenti il cliente; false
// se quella parte non ha un telefono (vuoto o di soli spazi), senza ripiegare sull'altra.
func (s Scheda) Destinatario() (Destinatario, bool) {
	parte, nome, telefono := ParteCliente, s.NomeCliente, s.TelefonoCliente
	if s.B2B() {
		parte, nome, telefono = ParteOreficeria, s.Oreficeria.Nome, ""
		if s.Oreficeria.Telefono != nil {
			telefono = *s.Oreficeria.Telefono
		}
	}
	telefono = strings.TrimSpace(telefono)
	if telefono == "" {
		return Destinatario{Parte: parte}, false
	}
	return Destinatario{Parte: parte, Nome: strings.TrimSpace(nome), Telefono: telefono}, true
}

// EtichettaCentesimi è Money.label/1 con i centesimi: «€ 12,50».
func EtichettaCentesimi(c int64) string {
	return fmt.Sprintf("€ %d,%02d", c/100, c%100)
}

// idPositivo è Id.positive/1 su un testo: Integer.parse/1 senza resto (segno ammesso, solo cifre
// ASCII), da 1 al massimo di un intero a 64 bit.
func idPositivo(s string) (int64, bool) {
	n, err := strconv.ParseInt(s, 10, 64)
	return n, err == nil && n > 0
}

// istanteEpoca sta al posto di un istante NULL nel confronto di answered_since?/1.
const istanteEpoca = "1970-01-01T00:00:00.000000Z"

// ApriScheda apre la busta id per chi apre (c).
func (b *Buste) ApriScheda(ctx context.Context, id string, c audit.Contesto) (Scheda, error) {
	n, ok := idPositivo(id)
	if !ok {
		return Scheda{}, ErrBustaNonTrovata
	}
	s, err := b.leggiScheda(ctx, n)
	if err != nil {
		return Scheda{}, err
	}
	if err := b.reg.Registra(ctx, c, "repair.viewed", "repair", s.ID, "ok", map[string]any{"bag_number": s.Numero}); err != nil {
		return Scheda{}, fmt.Errorf("%w: %w", ErrAperturaNonRiuscita, err)
	}
	if b.dopoLettura != nil {
		b.dopoLettura()
	}
	// Un segno che non si scrive lascia la busta com'era, come lo standalone (errore ignorato).
	if cambiato, err := b.segnaMessaggiLetti(ctx, s, c); err == nil && cambiato {
		if s, err = b.rileggi(ctx, s); err != nil {
			return Scheda{}, err
		}
	}
	if cambiato, err := b.segnaPreventivoLetto(ctx, s, c); err == nil && cambiato {
		if s, err = b.rileggi(ctx, s); err != nil {
			return Scheda{}, err
		}
	}
	return s, nil
}

// rileggi è la busta dopo un segno «letto»; se nel frattempo è sparita (cestino) resta quella
// di prima, come lo standalone.
func (b *Buste) rileggi(ctx context.Context, s Scheda) (Scheda, error) {
	n, err := b.leggiScheda(ctx, s.ID)
	if errors.Is(err, ErrBustaNonTrovata) {
		return s, nil
	}
	return n, err
}

// leggiScheda legge la busta (fuori dal cestino) con artigiano, oreficeria e nome del negozio.
func (b *Buste) leggiScheda(ctx context.Context, id int64) (Scheda, error) {
	var s Scheda
	var oro int
	var creata string
	var preventivoTesto, prezzoTesto, consegnaTesto, consegnaData, oreficeriaNome sql.NullString
	var oreficeriaTelefono sql.NullString
	var preventivoCent, prezzoCent, oreficeriaID sql.NullInt64
	err := b.d.Lettura.QueryRowContext(ctx, `SELECT b.id, b.numero, b.compro_oro, b.data_accettazione, b.nome_cliente,
		b.telefono_cliente, b.stato, b.descrizione, b.note_pubbliche, b.note_interne, a.nome, b.preventivo_testo,
		b.preventivo_centesimi, b.prezzo_testo, b.prezzo_centesimi, b.oreficeria_id, o.id IS NOT NULL, o.nome, o.telefono,
		b.data_prevista_testo, b.data_prevista, b.data_pronta, b.data_consegna, b.creato_il, b.codice_qr,
		b.ultimo_messaggio_il, b.messaggi_letti_il, b.preventivo_accettato_il, b.preventivo_accettato_letto_il,
		b.preventivo_rifiutato_il, b.preventivo_rifiutato_letto_il, b.giorni_in_piu, b.artigiano_id,
		b.consegnata_artigiano_il IS NOT NULL AND b.rientrata_artigiano_il IS NULL, b.attesa_cliente, b.attesa_dal, b.attesa_motivo
		FROM buste b LEFT JOIN artigiani a ON a.id = b.artigiano_id LEFT JOIN oreficerie o ON o.id = b.oreficeria_id
		WHERE b.id = ? AND b.cancellata_il IS NULL`, id).Scan(
		&s.ID, &s.Numero, &oro, &s.DataAccettazione, &s.NomeCliente, &s.TelefonoCliente, &s.Stato, &s.Descrizione,
		&s.NotePubbliche, &s.NoteInterne, &s.Artigiano, &preventivoTesto, &preventivoCent, &prezzoTesto, &prezzoCent,
		&oreficeriaID, new(bool), &oreficeriaNome, &oreficeriaTelefono, &consegnaTesto, &consegnaData, &s.DataPronta,
		&s.DataConsegna, &creata, &s.CodiceQR, &s.ultimoMessaggio, &s.messaggiLetti, &s.accettato, &s.accettatoLetto,
		&s.rifiutato, &s.rifiutatoLetto, &s.GiorniInPiu, &s.ArtigianoID, &s.FuoriBottega,
		&s.AttesaCliente, &s.AttesaDal, &s.AttesaMotivo)
	if errors.Is(err, sql.ErrNoRows) {
		return Scheda{}, ErrBustaNonTrovata
	}
	if err != nil {
		return Scheda{}, err
	}
	s.CompraOro = oro == 1
	s.Preventivo = Prezzo{Testo: nullaTesto(preventivoTesto), Centesimi: nullaIntero(preventivoCent)}
	s.PrezzoFinale = Prezzo{Testo: nullaTesto(prezzoTesto), Centesimi: nullaIntero(prezzoCent)}
	s.Consegna = Consegna{Testo: nullaTesto(consegnaTesto), Data: nullaTesto(consegnaData)}
	s.OreficeriaID = nullaIntero(oreficeriaID)
	if oreficeriaNome.Valid {
		s.Oreficeria = &Oreficeria{Nome: oreficeriaNome.String, Telefono: nullaTesto(oreficeriaTelefono)}
	}
	if s.B2B() && s.Oreficeria == nil {
		// Recipient.orefice/1 solleva: la chiave esterna lo impedisce, quindi è un dato rotto.
		return Scheda{}, fmt.Errorf("buste: la busta %d punta a un'oreficeria che non c'è", s.ID)
	}
	if s.CreataIl, err = time.Parse(db.FormatoIstante, creata); err != nil {
		return Scheda{}, err
	}
	if s.PreventivoAccettatoIl, err = istante(s.accettato); err != nil {
		return Scheda{}, err
	}
	if s.PreventivoRifiutatoIl, err = istante(s.rifiutato); err != nil {
		return Scheda{}, err
	}
	if s.NomeNegozio, err = b.nomeNegozio(ctx); err != nil {
		return Scheda{}, err
	}
	return s, nil
}

func nullaTesto(v sql.NullString) *string {
	if !v.Valid {
		return nil
	}
	return &v.String
}

func nullaIntero(v sql.NullInt64) *int64 {
	if !v.Valid {
		return nil
	}
	return &v.Int64
}

func istante(s *string) (*time.Time, error) {
	if s == nil {
		return nil, nil
	}
	t, err := time.Parse(db.FormatoIstante, *s)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// nonLetto è unread_messages?/1, unread_approval?/1 e unread_rejection?/1: c'è l'istante e il
// segno manca o è diverso (DateTime.compare/2 != :eq, qui sul testo salvato, che ha una forma
// sola: db.FormatoIstante).
func nonLetto(istante, letto *string) bool {
	return istante != nil && (letto == nil || *istante != *letto)
}

// segnaMessaggiLetti è RepairMessages.mark_read/2: se la conversazione ha un messaggio non letto,
// il segno prende l'istante dell'ultimo messaggio della busta letta, con repair.messages_read
// nella stessa transazione. L'ultima modifica della busta non cambia (update_all).
func (b *Buste) segnaMessaggiLetti(ctx context.Context, s Scheda, c audit.Contesto) (bool, error) {
	if !nonLetto(s.ultimoMessaggio, s.messaggiLetti) {
		return false, nil
	}
	tx, err := b.d.Scrittura.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `UPDATE buste SET messaggi_letti_il = ? WHERE id = ? AND cancellata_il IS NULL`, *s.ultimoMessaggio, s.ID); err != nil {
		return false, err
	}
	if err := b.reg.Scrivi(ctx, tx, c, "repair.messages_read", "repair", s.ID, "ok", map[string]any{"bag_number": s.Numero}); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

// segnaPreventivoLetto è Notifications.mark_quote_read/2: se la busta letta ha una risposta non
// letta, o nel database ne è arrivata una diversa nel frattempo (answered_since?/1), i segni
// dell'accettazione e del rifiuto prendono l'istante della risposta dove mancano o sono diversi;
// se almeno uno cambia, repair.quote_read nella stessa transazione.
func (b *Buste) segnaPreventivoLetto(ctx context.Context, s Scheda, c audit.Contesto) (bool, error) {
	if !nonLetto(s.accettato, s.accettatoLetto) && !nonLetto(s.rifiutato, s.rifiutatoLetto) {
		epoca := func(p *string) string {
			if p == nil {
				return istanteEpoca
			}
			return *p
		}
		var dopo bool
		if err := b.d.Lettura.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM buste WHERE id = ? AND cancellata_il IS NULL AND
			(preventivo_accettato_il != ? OR preventivo_rifiutato_il != ?))`, s.ID, epoca(s.accettato), epoca(s.rifiutato)).Scan(&dopo); err != nil {
			return false, err
		}
		if !dopo {
			return false, nil
		}
	}
	tx, err := b.d.Scrittura.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	var cambiate int64
	for _, q := range []string{
		`UPDATE buste SET preventivo_accettato_letto_il = preventivo_accettato_il WHERE id = ? AND cancellata_il IS NULL AND
			preventivo_accettato_il IS NOT NULL AND (preventivo_accettato_letto_il IS NULL OR preventivo_accettato_letto_il != preventivo_accettato_il)`,
		`UPDATE buste SET preventivo_rifiutato_letto_il = preventivo_rifiutato_il WHERE id = ? AND cancellata_il IS NULL AND
			preventivo_rifiutato_il IS NOT NULL AND (preventivo_rifiutato_letto_il IS NULL OR preventivo_rifiutato_letto_il != preventivo_rifiutato_il)`,
	} {
		r, err := tx.ExecContext(ctx, q, s.ID)
		if err != nil {
			return false, err
		}
		n, err := r.RowsAffected()
		if err != nil {
			return false, err
		}
		cambiate += n
	}
	if cambiate == 0 {
		return false, nil
	}
	if err := b.reg.Scrivi(ctx, tx, c, "repair.quote_read", "repair", s.ID, "ok", map[string]any{"bag_number": s.Numero}); err != nil {
		return false, err
	}
	return true, tx.Commit()
}
