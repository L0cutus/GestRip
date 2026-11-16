package buste

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/rivo/uniseg"
)

// ProfiloNegozio è la riga del profilo del negozio come la leggono le pagine pubbliche senza busta:
// il portale ritirato /m/ (CLI-53) e l'informativa /privacy (CLI-54), come
// OnlineSales.get_shop_profile/0 dello standalone. Presente è falso se il negozio non ha ancora un
// profilo; i campi sono nil quando la colonna è NULL e restano com'è scritti (anche vuoti): chi li
// mostra decide, come lo standalone, cosa vale come assente.
type ProfiloNegozio struct {
	Presente                                                      bool
	Nome                                                          string
	RagioneSociale, PartitaIVA, Indirizzo, Citta, Telefono, Email *string
}

// LeggiProfiloNegozio legge il profilo del negozio (ambito «default»).
func (b *Buste) LeggiProfiloNegozio(ctx context.Context) (ProfiloNegozio, error) {
	p := ProfiloNegozio{Presente: true}
	err := b.d.Lettura.QueryRowContext(ctx, `SELECT nome, ragione_sociale, partita_iva, indirizzo, citta, telefono, email
		FROM profilo_negozio WHERE ambito = 'default'`).Scan(&p.Nome, &p.RagioneSociale, &p.PartitaIVA, &p.Indirizzo, &p.Citta, &p.Telefono, &p.Email)
	if errors.Is(err, sql.ErrNoRows) {
		return ProfiloNegozio{}, nil
	}
	if err != nil {
		return ProfiloNegozio{}, err
	}
	return p, nil
}

// Il profilo del negozio nella pagina «Impostazioni del negozio» (slice 087, ADM-02), come
// OnlineSales.put_shop_profile/2 e ShopProfile.changeset/2 dello standalone (go-baseline): la riga
// «default» di shop_profiles si scrive tutta insieme, con la sua riga d'audit, in una transazione.
//
// Il changeset di Ecto si porta com'è, perché i casi limite si vedono nei dati:
//   - cast/3: un campo che non arriva nel modulo non cambia; uno che arriva vuoto o di soli spazi vale nil;
//     è un «cambio» solo se diverso dal valore salvato (nil e testo vuoto sono diversi);
//   - trim_strings: i soli cambi di testo si ripuliscono dagli spazi ai bordi (String.trim/1); un valore uguale
//     a quello salvato non si tocca, nemmeno se ha spazi ai bordi (come lo lascia un import);
//   - validate_required: nome e testo della garanzia, sul valore che resterebbe;
//   - validate_length (in grafemi) e validate_format del codice fiscale: solo sui cambi.
//
// Un campo che arriva ma non è un testo (una lista o una mappa nel modulo) è un errore di cast, «Valore non
// valido.»; nello standalone lo è anche l'input forgiato che crashava il processo LiveView.

// I messaggi del profilo: quelli di Ecto sono i testi del catalogo it dello standalone (slice 220; accanto,
// il msgid inglese); il codice fiscale ha il suo.
const (
	MsgCodiceFiscale = "codice fiscale non valido: 16 caratteri, o 11 cifre per una società"
)

// MsgTroppoLungo è «should be at most %{count} character(s)» in italiano.
func MsgTroppoLungo(n int) string { return "Troppo lungo (massimo " + strconv.Itoa(n) + " caratteri)." }

// CampoProfilo è un campo del profilo: il nome nel modulo (shop_profile[<Chiave>], lo stesso dello
// standalone), la colonna e la lunghezza massima in grafemi (0: nessuna).
type CampoProfilo struct {
	Chiave, Colonna string
	Massimo         int
}

// CampiProfilo sono i diciassette campi di ShopProfile (@fields), nell'ordine dello standalone.
var CampiProfilo = []CampoProfilo{
	{"shop_name", "nome", 255}, {"legal_name", "ragione_sociale", 255}, {"address", "indirizzo", 255}, {"postal_code", "cap", 20},
	{"city", "citta", 120}, {"province", "provincia", 20}, {"email", "email", 255}, {"sales_email", "email_vendite", 255},
	{"phone", "telefono", 80}, {"fiscal_id", "partita_iva", 40}, {"tax_code", "codice_fiscale", 0},
	{"warranty_terms", "condizioni_garanzia", 0}, {"subtitle", "sottotitolo", 255}, {"opening_hours", "orari", 500},
	{"instagram", "instagram", 255}, {"facebook", "facebook", 255}, {"website", "sito", 255},
}

// ModuloProfilo sono i campi arrivati con il modulo, per nome (shop_profile[<chiave>]): un testo, o nil se il
// campo è arrivato in una forma che non è un testo. Un campo che non c'è non è arrivato.
type ModuloProfilo map[string]*string

// ProfiloRifiutato è un salvataggio che il changeset ha rifiutato: gli errori per campo (uno, o più d'uno) e i
// valori da rimettere nel modulo, per ogni campo (input_value/3 del changeset: il cambio, il testo scritto se
// non è un cambio, altrimenti il salvato).
type ProfiloRifiutato struct {
	Errori map[string][]string
	Valori map[string]string
}

func (e *ProfiloRifiutato) Error() string { return "buste: profilo del negozio rifiutato" }

// reCodiceFiscale è @tax_code: sedici lettere e cifre, o undici cifre (società).
var reCodiceFiscale = regexp.MustCompile(`\A(?:[A-Z0-9]{16}|[0-9]{11})\z`)

// maiuscoleDelCodice scioglie le maiuscole che String.upcase/1 scrive con più lettere e che possono finire in un
// codice fiscale (ß → SS, le legature latine); strings.ToUpper conosce solo le maiuscole a una lettera.
var maiuscoleDelCodice = strings.NewReplacer("ß", "SS", "ﬀ", "FF", "ﬁ", "FI", "ﬂ", "FL", "ﬃ", "FFI", "ﬄ", "FFL", "ﬅ", "ST", "ﬆ", "ST")

// normalizzaCodiceFiscale è normalize_tax_code/1: senza spazi (anche Unicode, ~r/\s+/u), in maiuscole, nil se vuoto.
func normalizzaCodiceFiscale(v *string) *string {
	if v == nil {
		return nil
	}
	s := strings.ToUpper(maiuscoleDelCodice.Replace(strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, *v)))
	if s == "" {
		return nil
	}
	return &s
}

func stessoTesto(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// leggiProfiloDelModulo legge la riga «default» (id 0 e valori vuoti senza profilo), con NULL come nil.
func leggiProfiloDelModulo(ctx context.Context, q interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}) (id int64, dati map[string]*string, err error) {
	cols := make([]string, len(CampiProfilo))
	valori := make([]*string, len(CampiProfilo))
	dest := []any{&id}
	for i, c := range CampiProfilo {
		cols[i] = c.Colonna
		dest = append(dest, &valori[i])
	}
	dati = make(map[string]*string, len(CampiProfilo))
	switch err = q.QueryRowContext(ctx, `SELECT id, `+strings.Join(cols, ", ")+` FROM profilo_negozio WHERE ambito = 'default'`).Scan(dest...); {
	case errors.Is(err, sql.ErrNoRows):
		return 0, dati, nil
	case err != nil:
		return 0, nil, err
	}
	for i, c := range CampiProfilo {
		dati[c.Chiave] = valori[i]
	}
	return id, dati, nil
}

// ProfiloPerModulo legge il profilo salvato per il modulo: ogni campo come testo, NULL e senza profilo vuoti.
func (b *Buste) ProfiloPerModulo(ctx context.Context) (map[string]string, error) {
	_, dati, err := leggiProfiloDelModulo(ctx, b.d.Lettura)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(CampiProfilo))
	for _, c := range CampiProfilo {
		out[c.Chiave] = valoreTesto(dati[c.Chiave])
	}
	return out, nil
}

// SalvaProfilo è put_shop_profile/2: crea il profilo la prima volta, altrimenti riscrive i campi cambiati, e scrive
// shop_profile.updated nella stessa transazione (anche alla creazione, come lo standalone). Il changeset rifiutato
// non scrive niente e dà *ProfiloRifiutato; un guasto del database dà l'errore del database.
func (b *Buste) SalvaProfilo(ctx context.Context, m ModuloProfilo, c audit.Contesto) error {
	tx, err := b.d.Scrittura.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }() // dopo il Commit non fa niente
	id, dati, err := leggiProfiloDelModulo(ctx, tx)
	if err != nil {
		return err
	}
	cambi, rifiuto := changesetProfilo(m, dati)
	if rifiuto != nil {
		return rifiuto
	}
	_, adesso := b.adesso()
	switch {
	case id == 0:
		cols, marche, args := []string{"ambito"}, []string{"'default'"}, []any{}
		for _, f := range CampiProfilo {
			cols, marche = append(cols, f.Colonna), append(marche, "?")
			var v any
			if t := cambi[f.Chiave]; t != nil {
				v = *t
			}
			args = append(args, v)
		}
		cols, marche, args = append(cols, "creato_il", "aggiornato_il"), append(marche, "?", "?"), append(args, adesso, adesso)
		res, err := tx.ExecContext(ctx, `INSERT INTO profilo_negozio (`+strings.Join(cols, ", ")+`) VALUES (`+strings.Join(marche, ", ")+`)`, args...)
		if err != nil {
			return err
		}
		if id, err = res.LastInsertId(); err != nil {
			return err
		}
	case len(cambi) > 0:
		// Come repo.update/1: senza cambi niente UPDATE e «aggiornato_il» resta.
		var sets []string
		var args []any
		for _, f := range CampiProfilo {
			if t, ok := cambi[f.Chiave]; ok {
				var v any
				if t != nil {
					v = *t
				}
				sets, args = append(sets, f.Colonna+" = ?"), append(args, v)
			}
		}
		sets, args = append(sets, "aggiornato_il = ?"), append(args, adesso, id)
		if _, err := tx.ExecContext(ctx, `UPDATE profilo_negozio SET `+strings.Join(sets, ", ")+` WHERE id = ?`, args...); err != nil {
			return err
		}
	}
	if err := b.reg.Scrivi(ctx, tx, c, "shop_profile.updated", "shop_profile", id, "ok", nil); err != nil {
		return err
	}
	return tx.Commit()
}

// changesetProfilo è ShopProfile.changeset/2 sul profilo salvato (dati: il testo di ogni campo, nil se NULL o se non
// c'è un profilo): i cambi per campo (nil = NULL) o il rifiuto con gli errori.
func changesetProfilo(m ModuloProfilo, dati map[string]*string) (map[string]*string, *ProfiloRifiutato) {
	errori := map[string][]string{}
	cambi := map[string]*string{}
	for _, f := range CampiProfilo {
		v, arrivato := m[f.Chiave]
		if !arrivato {
			continue
		}
		if v == nil {
			errori[f.Chiave] = append(errori[f.Chiave], MsgNonValido)
			continue
		}
		var nuovo *string
		if !vuoto(*v) {
			nuovo = v
		}
		if !stessoTesto(nuovo, dati[f.Chiave]) {
			cambi[f.Chiave] = nuovo
		}
	}
	// trim_strings: solo i cambi di testo, e un valore ripulito uguale al salvato non è più un cambio (put_change/3).
	for k, v := range cambi {
		if v == nil {
			continue
		}
		t := strings.TrimSpace(*v)
		if stessoTesto(&t, dati[k]) {
			delete(cambi, k)
		} else {
			cambi[k] = &t
		}
	}
	// Il valore che resterebbe, per validate_required/3.
	resta := func(k string) *string {
		if v, ok := cambi[k]; ok {
			return v
		}
		return dati[k]
	}
	for _, k := range []string{"shop_name", "warranty_terms"} {
		if v := resta(k); (v == nil || vuoto(*v)) && len(errori[k]) == 0 {
			errori[k] = append(errori[k], MsgObbligatorio)
		}
	}
	for _, f := range CampiProfilo {
		if v := cambi[f.Chiave]; f.Massimo > 0 && v != nil && uniseg.GraphemeClusterCount(*v) > f.Massimo {
			errori[f.Chiave] = append(errori[f.Chiave], MsgTroppoLungo(f.Massimo))
		}
	}
	// update_change(:tax_code, &normalize_tax_code/1) e validate_format/4.
	if v, ok := cambi["tax_code"]; ok {
		if n := normalizzaCodiceFiscale(v); stessoTesto(n, dati["tax_code"]) {
			delete(cambi, "tax_code")
		} else {
			cambi["tax_code"] = n
			if n != nil && !reCodiceFiscale.MatchString(*n) {
				errori["tax_code"] = append(errori["tax_code"], MsgCodiceFiscale)
			}
		}
	}
	if len(errori) == 0 {
		return cambi, nil
	}
	valori := make(map[string]string, len(CampiProfilo))
	for _, f := range CampiProfilo {
		switch v, cambiato := cambi[f.Chiave]; {
		case cambiato:
			valori[f.Chiave] = valoreTesto(v)
		case m[f.Chiave] != nil:
			valori[f.Chiave] = *m[f.Chiave]
		default:
			valori[f.Chiave] = valoreTesto(dati[f.Chiave])
		}
	}
	return nil, &ProfiloRifiutato{Errori: errori, Valori: valori}
}
