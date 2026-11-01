package main

// Il passo dei numeri e dei testi d'errore (slice 037d, allineamento alla slice 220 dello
// standalone): dopo le foto, nello standalone ancora fermo, le funzioni che chiamano la nuova busta
// e la modifica (go-baseline): Workshop.create_customer/2 e create_repair/3, update_repair/4 con il
// contesto della scheda (actor e session_user). Gli errori si leggono come li mostra la pagina:
// Ecto.Changeset.traverse_errors/2 con GestripStandaloneWeb.CoreComponents.translate_error/1, cioè
// con il catalogo italiano. Tutti i dati sono inventati. In ordine:
//   - «019/26» si salva come 19/26 (la busta 19);
//   - « 0019/26 » è la stessa busta: «Numero busta già esistente.», il modulo mostra 19/26;
//   - «0/26» e «00000/26»: il messaggio del formato, il modulo mostra 0/26;
//   - data d'accettazione e lavorazione vuote; una data impossibile;
//   - «000020/26» (sei cifre) si salva come 20/26 (la busta 20) e porta il contatore a 20;
//   - tre modifiche rifiutate della 19/26: campi obbligatori vuoti, nome e telefono troppo lunghi,
//     data e giorni in più non validi.
//
// L'esito è una sola riga GR_NUMERI:. Il banco la confronta con quello che fa il dominio del Go
// (internal/buste) con gli stessi dati, su un database suo che poi cancella: nello standalone la
// nuova busta e la modifica sono eventi LiveView, che il banco HTTP non può mandare. Le schede delle
// due buste si confrontano poi via HTTP (scenario numeri).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
)

// Le buste del passo (seguono le 1–18 della cattura e dei passi prima), i numeri salvati, il
// prossimo numero proposto per il 2026 e le buste in tutto.
var (
	busteNumeri       = []int64{19, 20}
	numeriNumeri      = []string{"19/26", "20/26"}
	prossimoNumeri    = "21/26"
	totaleBusteNumeri = int64(20)
)

const numeriStandalone = `GestripStandalone.LegacyImport.Release.quiet!()
{:ok, _} = Application.ensure_all_started(:gestrip_standalone)
alias GestripStandalone.{Repo, Workshop}
alias GestripStandalone.Accounts.User
alias GestripStandalone.Workshop.Repair
email = "/run/secrets/admin_email" |> File.read!() |> String.trim_trailing()
audit = %{actor: email}
riletta = fn busta -> Repo.get!(Repair, busta.id) end
campi = fn altri -> Map.merge(%{"accepted_on" => "2026-03-02", "description" => "Lavoro sintetico", "qr_code" => "SINT-QR-0019"}, altri) end
rifiuto = fn
  {:error, %Ecto.Changeset{} = cs} ->
    {:ok, %{errori: Ecto.Changeset.traverse_errors(cs, &GestripStandaloneWeb.CoreComponents.translate_error/1),
      numero: Ecto.Changeset.get_change(cs, :bag_number)}}
  altro -> {:error, {:atteso_un_rifiuto, altro}}
end
risultato =
  with %User{} = titolare <- Repo.get_by(User, email: email),
       audit_scheda = %{actor: email, session_user: {titolare.id, titolare.session_version}},
       {:ok, cliente} <- Workshop.create_customer(%{name: "Cliente Sintetico Numeri", phone: "+390000000019"}, audit),
       {:ok, b19, _token} <- Workshop.create_repair(cliente, campi.(%{"bag_number" => "019/26"}), audit),
       {:ok, doppio} <- rifiuto.(Workshop.create_repair(cliente, campi.(%{"bag_number" => " 0019/26 "}), audit)),
       {:ok, zero} <- rifiuto.(Workshop.create_repair(cliente, campi.(%{"bag_number" => "0/26"}), audit)),
       {:ok, zeri} <- rifiuto.(Workshop.create_repair(cliente, campi.(%{"bag_number" => "00000/26"}), audit)),
       {:ok, vuoti} <- rifiuto.(Workshop.create_repair(cliente, campi.(%{"bag_number" => "21/26", "accepted_on" => "", "description" => ""}), audit)),
       {:ok, data} <- rifiuto.(Workshop.create_repair(cliente, campi.(%{"bag_number" => "21/26", "accepted_on" => "2026-02-30"}), audit)),
       {:ok, b20, _token} <- Workshop.create_repair(cliente, campi.(%{"bag_number" => "000020/26", "qr_code" => "SINT-QR-0020"}), audit),
       {:ok, modifica_vuota} <- rifiuto.(Workshop.update_repair(riletta.(b19), %{"accepted_on" => "", "description" => "",
         "customer_name_snapshot" => "   ", "extra_days" => ""}, audit_scheda, compro_oro: nil)),
       {:ok, modifica_lunghi} <- rifiuto.(Workshop.update_repair(riletta.(b19), %{"customer_name_snapshot" => String.duplicate("n", 161),
         "customer_phone_snapshot" => String.duplicate("3", 41)}, audit_scheda, compro_oro: nil)),
       {:ok, modifica_non_validi} <- rifiuto.(Workshop.update_repair(riletta.(b19), %{"accepted_on" => "2026-02-30", "extra_days" => "tre"},
         audit_scheda, compro_oro: nil)) do
    buste = Enum.map([b19, b20], riletta)
    %{esito: "ok", buste: Enum.map(buste, & &1.id), numeri: Enum.map(buste, & &1.bag_number),
      prossimo: Workshop.peek_next_bag_number(2026),
      rifiuti: %{doppio: doppio, zero: zero, zeri: zeri, vuoti: vuoti, data: data, modifica_vuota: modifica_vuota,
        modifica_lunghi: modifica_lunghi, modifica_non_validi: modifica_non_validi},
      totale_buste: Repo.aggregate(Repair, :count)}
  else
    errore -> %{esito: "errore", motivo: inspect(errore)}
  end
IO.puts("GR_NUMERI:" <> Jason.encode!(risultato))
if risultato.esito != "ok", do: System.halt(1)`

// comandoNumeri gira come gli stati: stessa immagine, senza rete, stesse protezioni e stessi dati
// dell'istanza, e nient'altro.
func comandoNumeri(n nomi, img string, env []string) []string {
	a := append(passoSenzaRete(n, passoNumeri), protezioniStandalone(n)...)
	a = append(a, montaggiStandalone(n)...)
	a = append(a, env...)
	return append(a, img, "bin/gestrip_standalone", "eval", numeriStandalone)
}

// rifiutoNumeri è un rifiuto del passo: gli errori per campo (nomi dello standalone) e il numero che
// il modulo rimostra (nil per la modifica, che non tocca il numero).
type rifiutoNumeri struct {
	Errori map[string][]string `json:"errori"`
	Numero *string             `json:"numero"`
}

// esitoNumeri è la riga GR_NUMERI: del passo dei numeri.
type esitoNumeri struct {
	Esito       string                   `json:"esito"`
	Motivo      string                   `json:"motivo"`
	Buste       []int64                  `json:"buste"`
	Numeri      []string                 `json:"numeri"`
	Prossimo    string                   `json:"prossimo"`
	Rifiuti     map[string]rifiutoNumeri `json:"rifiuti"`
	TotaleBuste int64                    `json:"totale_buste"`
}

// leggiEsitoNumeri vuole una sola riga GR_NUMERI: riuscita, senza campi in più: le due buste con
// gli id e i numeri attesi (19/26 e 20/26: gli zeri tolti), 21/26 come prossimo numero (il contatore
// mosso da «000020/26») e le buste in tutto. I rifiuti li confronta confrontaNumeri.
func leggiEsitoNumeri(out string) (esitoNumeri, error) {
	righe := righeMarcatore(out, "GR_NUMERI:")
	if len(righe) != 1 {
		return esitoNumeri{}, fmt.Errorf("numeri nello standalone: %d righe GR_NUMERI, ne serve una", len(righe))
	}
	var e esitoNumeri
	dec := json.NewDecoder(strings.NewReader(righe[0]))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&e); err != nil {
		return esitoNumeri{}, fmt.Errorf("numeri nello standalone: riga GR_NUMERI non valida: %w", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return esitoNumeri{}, errors.New("numeri nello standalone: dati dopo la riga GR_NUMERI")
	}
	switch {
	case e.Esito != "ok":
		return esitoNumeri{}, fmt.Errorf("numeri nello standalone: esito %q: %s", e.Esito, e.Motivo)
	case !slices.Equal(e.Buste, busteNumeri) || !slices.Equal(e.Numeri, numeriNumeri):
		return esitoNumeri{}, fmt.Errorf("numeri nello standalone: buste %v numeri %v, attesi %v %v", e.Buste, e.Numeri, busteNumeri, numeriNumeri)
	case e.Prossimo != prossimoNumeri:
		return esitoNumeri{}, fmt.Errorf("numeri nello standalone: prossimo numero %q, atteso %q", e.Prossimo, prossimoNumeri)
	case e.TotaleBuste != totaleBusteNumeri:
		return esitoNumeri{}, fmt.Errorf("numeri nello standalone: %d buste in tutto, attese %d", e.TotaleBuste, totaleBusteNumeri)
	}
	return e, nil
}

// campoStandalone dà il nome dello standalone di un campo del dominio Go.
var campoStandalone = map[string]string{
	"numero":            "bag_number",
	"data_accettazione": "accepted_on",
	"descrizione":       "description",
	"nome_cliente":      "customer_name_snapshot",
	"telefono_cliente":  "customer_phone_snapshot",
	"giorni_in_piu":     "extra_days",
}

// rifiutoGo è un rifiuto del dominio Go con i nomi dei campi dello standalone. Un errore che non è
// dei campi, o un campo senza nome nello standalone, è un errore del banco.
func rifiutoGo(err error) (rifiutoNumeri, error) {
	var campi buste.Campi
	if !errors.As(err, &campi) {
		return rifiutoNumeri{}, fmt.Errorf("atteso un rifiuto dei campi, avuto %v", err)
	}
	r := rifiutoNumeri{Errori: map[string][]string{}}
	for k, v := range campi {
		nome, ok := campoStandalone[k]
		if !ok {
			return rifiutoNumeri{}, fmt.Errorf("campo %q senza nome nello standalone", k)
		}
		r.Errori[nome] = v
	}
	var c *buste.CampiRifiutati
	if errors.As(err, &c) {
		n := c.Numero
		r.Numero = &n
	}
	return r, nil
}

// numeriNelGo fa nel dominio Go quello che il passo fa nello standalone, sullo stesso orologio
// dei dati (02/03/2026), in un database nuovo nella cartella dir, che poi cancella. Dà l'esito con
// i numeri, il prossimo numero del 2026 e i rifiuti; buste e totale sono quelli dello standalone
// (il database del Go qui ha solo queste due buste).
func numeriNelGo(dir string) (e esitoNumeri, err error) {
	tmp, err := os.MkdirTemp(dir, "numeri-go-")
	if err != nil {
		return e, err
	}
	defer func() { err = errors.Join(err, os.RemoveAll(tmp)) }()
	ctx := context.Background()
	d, err := db.Apri(filepath.Join(tmp, "gestrip.db"))
	if err != nil {
		return e, err
	}
	defer func() { err = errors.Join(err, d.Chiudi()) }()
	o := clock.NuovoFinto(time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC))
	if _, err := d.Migra(ctx, o); err != nil {
		return e, err
	}
	b := buste.Nuove(d, o)
	chi := audit.Contesto{Attore: "banco@esempio.test"}
	modulo := func(numero string, f func(*buste.Accettazione)) buste.Accettazione {
		a := buste.Accettazione{NomeCliente: "Cliente Sintetico Numeri", TelefonoCliente: "+390000000019", Numero: numero,
			DataAccettazione: "2026-03-02", Descrizione: "Lavoro sintetico", CodiceQR: "SINT-QR-0019"}
		if f != nil {
			f(&a)
		}
		return a
	}
	e.Rifiuti = map[string]rifiutoNumeri{}
	rifiuta := func(nome string, err error) error {
		r, errR := rifiutoGo(err)
		if errR != nil {
			return fmt.Errorf("numeri nel Go, %s: %w", nome, errR)
		}
		e.Rifiuti[nome] = r
		return nil
	}
	b19, err := b.Crea(ctx, modulo("019/26", nil), chi)
	if err != nil {
		return e, fmt.Errorf("numeri nel Go, 019/26: %w", err)
	}
	for _, c := range []struct {
		nome string
		a    buste.Accettazione
	}{
		{"doppio", modulo(" 0019/26 ", nil)},
		{"zero", modulo("0/26", nil)},
		{"zeri", modulo("00000/26", nil)},
		{"vuoti", modulo("21/26", func(a *buste.Accettazione) { a.DataAccettazione, a.Descrizione = "", "" })},
		{"data", modulo("21/26", func(a *buste.Accettazione) { a.DataAccettazione = "2026-02-30" })},
	} {
		_, err := b.Crea(ctx, c.a, chi)
		if err := rifiuta(c.nome, err); err != nil {
			return e, err
		}
	}
	b20, err := b.Crea(ctx, modulo("000020/26", func(a *buste.Accettazione) { a.CodiceQR = "SINT-QR-0020" }), chi)
	if err != nil {
		return e, fmt.Errorf("numeri nel Go, 000020/26: %w", err)
	}
	s := func(v string) *string { return &v }
	for _, c := range []struct {
		nome string
		m    buste.Modifica
	}{
		{"modifica_vuota", buste.Modifica{DataAccettazione: s(""), Descrizione: s(""), NomeCliente: s("   "), GiorniInPiu: s("")}},
		{"modifica_lunghi", buste.Modifica{NomeCliente: s(strings.Repeat("n", 161)), TelefonoCliente: s(strings.Repeat("3", 41))}},
		{"modifica_non_validi", buste.Modifica{DataAccettazione: s("2026-02-30"), GiorniInPiu: s("tre")}},
	} {
		_, err := b.Modifica(ctx, b19.ID, c.m, nil, chi)
		if err := rifiuta(c.nome, err); err != nil {
			return e, err
		}
	}
	e.Esito, e.Buste, e.Numeri, e.TotaleBuste = "ok", busteNumeri, []string{b19.Numero, b20.Numero}, totaleBusteNumeri
	if e.Prossimo, err = b.ProssimoNumeroDellAnno(ctx, 2026); err != nil {
		return e, err
	}
	return e, nil
}

// confrontaNumeri vuole dal Go esattamente l'esito dello standalone: numeri, prossimo numero e ogni
// rifiuto, con i suoi testi e il numero rimostrato.
func confrontaNumeri(sa, gogo esitoNumeri) error {
	if !slices.Equal(sa.Numeri, gogo.Numeri) || sa.Prossimo != gogo.Prossimo {
		return fmt.Errorf("numeri: standalone %v prossimo %q, Go %v prossimo %q", sa.Numeri, sa.Prossimo, gogo.Numeri, gogo.Prossimo)
	}
	nomi := make([]string, 0, len(sa.Rifiuti)+len(gogo.Rifiuti))
	for k := range sa.Rifiuti {
		nomi = append(nomi, k)
	}
	for k := range gogo.Rifiuti {
		nomi = append(nomi, k)
	}
	slices.Sort(nomi)
	var diff []string
	for _, k := range slices.Compact(nomi) {
		a, okA := sa.Rifiuti[k]
		g, okG := gogo.Rifiuti[k]
		if !okA || !okG || !reflect.DeepEqual(a, g) {
			diff = append(diff, fmt.Sprintf("%s: standalone %s, Go %s", k, descriviRifiuto(a, okA), descriviRifiuto(g, okG)))
		}
	}
	if len(diff) > 0 {
		return fmt.Errorf("numeri, rifiuti diversi: %s", strings.Join(diff, "; "))
	}
	return nil
}

func descriviRifiuto(r rifiutoNumeri, ok bool) string {
	if !ok {
		return "(assente)"
	}
	n := "nil"
	if r.Numero != nil {
		n = fmt.Sprintf("%q", *r.Numero)
	}
	return fmt.Sprintf("%v numero %s", r.Errori, n)
}

// numeriBuste fa il passo dei numeri, ne controlla l'esito e lo confronta con il dominio Go.
func numeriBuste(n nomi, img string, env []string) (esitoNumeri, error) {
	fmt.Println("catena: numeri con zeri davanti e testi d'errore nello standalone e nel Go…")
	out, errDocker := dockerTiene(comandoNumeri(n, img, env)...)
	e, err := leggiEsitoNumeri(out)
	if err != nil {
		return esitoNumeri{}, errors.Join(err, errDocker)
	}
	if errDocker != nil {
		return esitoNumeri{}, fmt.Errorf("numeri nello standalone: %w", errDocker)
	}
	gogo, err := numeriNelGo(n.dir)
	if err != nil {
		return esitoNumeri{}, err
	}
	if err := confrontaNumeri(e, gogo); err != nil {
		return esitoNumeri{}, err
	}
	return e, nil
}
