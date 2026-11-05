package main

// Il passo della home (slice 050): dopo i numeri, nello standalone ancora fermo, venti buste nuove
// per la lista della home, fatte con le funzioni che chiamano le pagine (go-baseline):
// Workshop.create_customer/2 e create_repair/3 come la nuova busta; TelegramNotices.transition/3,
// set_repair_waiting/3 e toggle_repair_pin/2 con il contesto della scheda e della lista (actor e
// session_user); RepairMessages.append/3 per un messaggio del cliente (non letto) e uno del negozio
// (letto); Settings.Operator.put/2 per le impostazioni della home (scadenza a 12 giorni, come la
// cattura, e le consegnate nascoste dalla home senza filtri). Tutti i dati sono inventati.
//
// Le scadenze sono relative al giorno in cui gira il passo (oggi in negozio): un giro del banco che
// passa la mezzanotte fra `banco su` ed `esegui` cambia i contatori di tutte e due le app allo stesso
// modo, ma non le attese degli scenari della home.
//
// Le buste (numero interno, codice QR, cosa provano):
//   - 21/26 RIP-2026-0045 consegnata e 22/26 RIP-2026-0045 aperta: le gemelle di un'etichetta (D-64);
//   - 23/26 e 24/26 RIP-2026-0046, tutte e due aperte (la 24 in attesa materiali);
//   - 25/26 RIP-2025-0045: un'etichetta dell'anno prima, «0045/25»;
//   - 26/26 RIP-2026-0047 fissata (📌, in cima alla lista);
//   - 27/26 RIP-2026-0048 con un messaggio del cliente non letto (subito sotto le fissate, 💬);
//   - 28/26 RIP-2026-0049 con un messaggio del negozio (conversazione letta, 🗨);
//   - 29/26 consegna fra 3 giorni (in scadenza, urgente), 30/26 scaduta da 2 (urgente, non in
//     scadenza), 31/26 fra 30 (né l'una né l'altra), 32/26 pronta fra 1 (in scadenza, mai urgente);
//   - 33/26 in attesa del cliente con un motivo da scappare;
//   - 34/26 compro oro in valutazione;
//   - 35/26 senza QR («N. 35/26», «⚠ senza QR»), 36/26 con un codice che non è un'etichetta («N. 36/26»);
//   - 37/26 consegnata (nascosta dalla home senza filtri), 38/26 annullata con la consegna passata
//     (mai urgente);
//   - 9999/25 e 10000/25, del 2025: l'anno e il progressivo letti come numeri; la 10000/25 senza QR.
//
// L'esito è una sola riga GR_HOME: con id, numeri e stati riletti, e le buste in tutto; un errore esce
// con stato 1. Le liste delle due app le confrontano gli scenari home, home-dipendente e
// home-contatori; il numero visibile del Go (D-64) lo dichiara il normalizzatore di confronto.go con
// la tabella numeriVisibiliBanco qui sotto.

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
)

// Le buste del passo: seguono le 1–20 della cattura e dei passi prima.
var (
	busteHome  = []int64{21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32, 33, 34, 35, 36, 37, 38, 39, 40}
	numeriHome = []string{"21/26", "22/26", "23/26", "24/26", "25/26", "26/26", "27/26", "28/26", "29/26", "30/26",
		"31/26", "32/26", "33/26", "34/26", "35/26", "36/26", "37/26", "38/26", "9999/25", "10000/25"}
	statiHome = []string{"consegnata", "in_lavorazione", "in_lavorazione", "in_attesa_materiali", "in_lavorazione",
		"in_lavorazione", "in_lavorazione", "in_lavorazione", "in_lavorazione", "in_lavorazione", "in_lavorazione",
		"pronta", "in_lavorazione", "in_valutazione", "in_lavorazione", "in_lavorazione", "consegnata", "annullata",
		"in_lavorazione", "in_lavorazione"}
	totaleBusteHome = int64(40)
)

// numeriVisibiliBanco è il numero che il Go mostra (D-64) per ogni busta dei dati sintetici del banco,
// scritto a mano dal suo codice QR: «#0045/26» per un'etichetta RIP-AAAA-NNNN, «N. 45/26» per tutto il
// resto (nessun QR, SINT-QR-…, SINT-ALTRO-…). Lo standalone mostra «#<numero interno>»; il
// normalizzatore D-64 di confronto.go mette questo al suo posto prima del confronto. Le buste 1–20
// vengono dalla cattura e dai passi prima (nessuna ha un'etichetta RIP), le altre da questo passo.
var numeriVisibiliBanco = map[string]string{
	"1/26": "N. 1/26", "2/26": "N. 2/26", "3/26": "N. 3/26", "4/26": "N. 4/26", "5/26": "N. 5/26",
	"6/26": "N. 6/26", "7/26": "N. 7/26", "8/26": "N. 8/26", "9/26": "N. 9/26", "10/26": "N. 10/26",
	"11/26": "N. 11/26", "12/26": "N. 12/26", "13/26": "N. 13/26", "14/26": "N. 14/26", "15/26": "N. 15/26",
	"16/26": "N. 16/26", "17/26": "N. 17/26", "18/26": "N. 18/26", "19/26": "N. 19/26", "20/26": "N. 20/26",
	"21/26": "#0045/26", "22/26": "#0045/26", "23/26": "#0046/26", "24/26": "#0046/26", "25/26": "#0045/25",
	"26/26": "#0047/26", "27/26": "#0048/26", "28/26": "#0049/26", "29/26": "#0050/26", "30/26": "#0051/26",
	"31/26": "#0052/26", "32/26": "#0053/26", "33/26": "#0054/26", "34/26": "#0055/26", "35/26": "N. 35/26",
	"36/26": "N. 36/26", "37/26": "#0056/26", "38/26": "#0057/26", "9999/25": "#0012/25", "10000/25": "N. 10000/25",
}

const homeStandalone = `GestripStandalone.LegacyImport.Release.quiet!()
{:ok, _} = Application.ensure_all_started(:gestrip_standalone)
alias GestripStandalone.{Repo, RepairMessages, TelegramNotices, Workshop}
alias GestripStandalone.Accounts.User
alias GestripStandalone.Settings.Operator
alias GestripStandalone.Workshop.Repair
email = "/run/secrets/admin_email" |> File.read!() |> String.trim_trailing()
audit = %{actor: email}
oggi = :calendar.local_time() |> NaiveDateTime.from_erl!() |> NaiveDateTime.to_date()
giorno = fn n -> Date.to_iso8601(Date.add(oggi, n)) end
riletta = fn busta -> Repo.get!(Repair, busta.id) end
nuova = fn numero, telefono, campi ->
  with {:ok, cliente} <- Workshop.create_customer(%{name: "Cliente Sintetico " <> numero, phone: telefono}, audit),
       {:ok, busta, _token} <- Workshop.create_repair(cliente,
         Map.merge(%{"bag_number" => numero, "accepted_on" => "2026-03-02", "description" => "Lavoro sintetico della home"}, campi), audit),
       do: {:ok, busta}
end
stati = fn busta, passi, contesto ->
  Enum.reduce_while(passi, {:ok, busta}, fn nuovo, {:ok, b} ->
    case TelegramNotices.transition(riletta.(b), nuovo, contesto) do
      {:ok, cambiata, _avviso} -> {:cont, {:ok, cambiata}}
      errore -> {:halt, errore}
    end
  end)
end
messaggio = fn busta, campi ->
  case RepairMessages.append(riletta.(busta), campi, audit) do
    {:ok, _messaggio, cambiata} -> {:ok, cambiata}
    errore -> errore
  end
end
risultato =
  with %User{} = titolare <- Repo.get_by(User, email: email),
       audit_scheda = %{actor: email, session_user: {titolare.id, titolare.session_version}},
       {:ok, _} <- Operator.put(%{"due_days" => "12", "show_ready" => "true", "show_delivered" => "false", "gallery_size" => "M"}, audit),
       {:ok, b21} <- nuova.("21/26", "+390000000021", %{"qr_code" => "RIP-2026-0045", "description" => "Collana <oro> & \"perle\""}),
       {:ok, _} <- stati.(b21, ["pronta", "consegnata"], audit_scheda),
       {:ok, b22} <- nuova.("22/26", "+390000000022", %{"qr_code" => "RIP-2026-0045"}),
       {:ok, b23} <- nuova.("23/26", "+390000000023", %{"qr_code" => "RIP-2026-0046"}),
       {:ok, b24} <- nuova.("24/26", "+390000000024", %{"qr_code" => "RIP-2026-0046"}),
       {:ok, _} <- stati.(b24, ["in_attesa_materiali"], audit_scheda),
       {:ok, b25} <- nuova.("25/26", "+390000000025", %{"qr_code" => "RIP-2025-0045"}),
       {:ok, b26} <- nuova.("26/26", "+390000000026", %{"qr_code" => "RIP-2026-0047"}),
       {:ok, _} <- Workshop.toggle_repair_pin(b26, audit_scheda),
       {:ok, b27} <- nuova.("27/26", "+390000000027", %{"qr_code" => "RIP-2026-0048"}),
       {:ok, _} <- messaggio.(b27, %{author: "customer", body: "Domanda sintetica del cliente"}),
       {:ok, b28} <- nuova.("28/26", "+390000000028", %{"qr_code" => "RIP-2026-0049"}),
       {:ok, _} <- messaggio.(b28, %{author: "shop", body: "Risposta sintetica del negozio"}),
       {:ok, b29} <- nuova.("29/26", "+390000000029", %{"qr_code" => "RIP-2026-0050", "expected_on" => giorno.(3)}),
       {:ok, b30} <- nuova.("30/26", "+390000000030", %{"qr_code" => "RIP-2026-0051", "expected_on" => giorno.(-2)}),
       {:ok, b31} <- nuova.("31/26", "+390000000031", %{"qr_code" => "RIP-2026-0052", "expected_on" => giorno.(30)}),
       {:ok, b32} <- nuova.("32/26", "+390000000032", %{"qr_code" => "RIP-2026-0053", "expected_on" => giorno.(1)}),
       {:ok, _} <- stati.(b32, ["pronta"], audit_scheda),
       {:ok, b33} <- nuova.("33/26", "+390000000033", %{"qr_code" => "RIP-2026-0054"}),
       {:ok, _} <- Workshop.set_repair_waiting(riletta.(b33), %{"azione" => "on", "motivo" => "Conferma <prezzo> & \"tempi\""}, audit_scheda),
       {:ok, b34} <- nuova.("34/26", "+390000000034", %{"qr_code" => "RIP-2026-0055", "compro_oro" => "true"}),
       {:ok, b35} <- nuova.("35/26", "+390000000035", %{"qr_code" => ""}),
       {:ok, b36} <- nuova.("36/26", "+390000000036", %{"qr_code" => "SINT-ALTRO-36"}),
       {:ok, b37} <- nuova.("37/26", "+390000000037", %{"qr_code" => "RIP-2026-0056"}),
       {:ok, _} <- stati.(b37, ["pronta", "consegnata"], audit_scheda),
       {:ok, b38} <- nuova.("38/26", "+390000000038", %{"qr_code" => "RIP-2026-0057", "expected_on" => giorno.(-5)}),
       {:ok, _} <- stati.(b38, ["annullata"], audit_scheda),
       {:ok, b39} <- nuova.("9999/25", "+390000000039", %{"qr_code" => "RIP-2025-0012", "accepted_on" => "2025-06-02"}),
       {:ok, b40} <- nuova.("10000/25", "+390000000040", %{"accepted_on" => "2025-06-03"}) do
    buste = Enum.map([b21, b22, b23, b24, b25, b26, b27, b28, b29, b30, b31, b32, b33, b34, b35, b36, b37, b38, b39, b40], riletta)
    %{esito: "ok", buste: Enum.map(buste, & &1.id), numeri: Enum.map(buste, & &1.bag_number),
      stati: Enum.map(buste, & &1.status), totale_buste: Repo.aggregate(Repair, :count)}
  else
    errore -> %{esito: "errore", motivo: inspect(errore)}
  end
IO.puts("GR_HOME:" <> Jason.encode!(risultato))
if risultato.esito != "ok", do: System.halt(1)`

// comandoHome gira come gli stati: stessa immagine, senza rete, stesse protezioni e stessi dati
// dell'istanza, e nient'altro.
func comandoHome(n nomi, img string, env []string) []string {
	a := append(passoSenzaRete(n, passoHome), protezioniStandalone(n)...)
	a = append(a, montaggiStandalone(n)...)
	a = append(a, env...)
	return append(a, img, "bin/gestrip_standalone", "eval", homeStandalone)
}

// esitoHome è la riga GR_HOME: del passo della home.
type esitoHome struct {
	Esito       string   `json:"esito"`
	Motivo      string   `json:"motivo"`
	Buste       []int64  `json:"buste"`
	Numeri      []string `json:"numeri"`
	Stati       []string `json:"stati"`
	TotaleBuste int64    `json:"totale_buste"`
}

// leggiEsitoHome vuole una sola riga GR_HOME: riuscita, senza campi in più, con le venti buste agli
// id, ai numeri e agli stati attesi e le buste in tutto.
func leggiEsitoHome(out string) (esitoHome, error) {
	righe := righeMarcatore(out, "GR_HOME:")
	if len(righe) != 1 {
		return esitoHome{}, fmt.Errorf("home nello standalone: %d righe GR_HOME, ne serve una", len(righe))
	}
	var e esitoHome
	dec := json.NewDecoder(strings.NewReader(righe[0]))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&e); err != nil {
		return esitoHome{}, fmt.Errorf("home nello standalone: riga GR_HOME non valida: %w", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return esitoHome{}, errors.New("home nello standalone: dati dopo la riga GR_HOME")
	}
	switch {
	case e.Esito != "ok":
		return esitoHome{}, fmt.Errorf("home nello standalone: esito %q: %s", e.Esito, e.Motivo)
	case !slices.Equal(e.Buste, busteHome) || !slices.Equal(e.Numeri, numeriHome):
		return esitoHome{}, fmt.Errorf("home nello standalone: buste %v numeri %v, attesi %v %v", e.Buste, e.Numeri, busteHome, numeriHome)
	case !slices.Equal(e.Stati, statiHome):
		return esitoHome{}, fmt.Errorf("home nello standalone: stati %v, attesi %v", e.Stati, statiHome)
	case e.TotaleBuste != totaleBusteHome:
		return esitoHome{}, fmt.Errorf("home nello standalone: %d buste in tutto, attese %d", e.TotaleBuste, totaleBusteHome)
	}
	return e, nil
}

// homeBuste fa il passo della home e ne dà l'esito controllato.
func homeBuste(n nomi, img string, env []string) (esitoHome, error) {
	fmt.Println("catena: buste per la lista della home nello standalone…")
	out, errDocker := dockerTiene(comandoHome(n, img, env)...)
	e, err := leggiEsitoHome(out)
	if err != nil {
		return esitoHome{}, errors.Join(err, errDocker)
	}
	if errDocker != nil {
		return esitoHome{}, fmt.Errorf("home nello standalone: %w", errDocker)
	}
	return e, nil
}
