package main

// Il passo degli stati (slice 034): dopo le modifiche, nello standalone ancora fermo, cinque buste
// nuove con stati, riaperture, attese e un lotto chiuso alla consegna, fatte con le funzioni che
// chiamano le pagine (go-baseline): Workshop.create_customer/2 e create_repair/3 come la nuova
// busta; TelegramNotices.transition/3 (l'evento transition della scheda), reopen_repair/3 e
// set_repair_waiting/3 con il contesto della scheda (actor e session_user); ReadyLots.open_lot/2,
// add_bag/3 e send_lot/2 con il contesto delle pagine dei lotti (actor). Tutti i dati sono
// inventati. Le buste, in ordine:
//   - 11/26: in attesa materiali, in attesa del cliente con un motivo da scappare;
//   - 12/26: annullata (riapertura fra gli stati delle riparazioni), in attesa senza motivo
//     (azione con maiuscole e spazi, motivo di soli spazi);
//   - 13/26: compro oro, valutata, perfezionata e chiusa (riapertura fra gli stati del compro oro);
//   - 14/26: dell'oreficeria 2 della cattura, pronta, nel lotto inviato, consegnata: il lotto si
//     chiude alla consegna (ReadyLots.close_after_delivery/2);
//   - 15/26: pronta, consegnata, riaperta in pronta con un motivo da scappare, messa in attesa e
//     tolta.
//
// Poi la scheda della 11/26 si apre 105 volte (Workshop.record_repair_event/3, la riga
// repair.viewed che scrive il mount della scheda): sono più di una pagina del registro (100 righe),
// uguali dalle due parti dopo l'import, e lo scenario registro ci prova la paginazione sul filtro
// action=repair.viewed. Il registro intero non basta: la home dello standalone scrive anche
// repair.index_viewed e quella del Go (T4.2) no, quindi i totali differiscono.
//
// L'esito è una sola riga GR_STATI: con ciò che il passo ha scritto, riletto con le letture dello
// standalone; un errore esce con stato 1. Il cambio di stato, la riapertura e l'attesa nel Go non
// si confrontano via HTTP (nello standalone sono eventi LiveView): li provano i test Go; il banco
// confronta le schede che ne escono.

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
)

// Le buste del passo: seguono le 1–10 della cattura e delle modifiche.
var (
	busteStati     = []int64{11, 12, 13, 14, 15}
	statiStati     = []string{"in_attesa_materiali", "annullata", "chiusa", "consegnata", "pronta"}
	compraOroStati = []bool{false, false, true, false, false}
	attesaStati    = []bool{true, true, false, false, false}
	versioniStati  = []int64{1, 1, 3, 2, 3}
)

// apertureStati sono le aperture della scheda della 11/26 per la paginazione del registro: più delle
// 100 righe di una pagina.
const apertureStati = 105

const statiStandalone = `GestripStandalone.LegacyImport.Release.quiet!()
{:ok, _} = Application.ensure_all_started(:gestrip_standalone)
alias GestripStandalone.{ReadyLots, Repo, TelegramNotices, Workshop}
alias GestripStandalone.Accounts.User
alias GestripStandalone.Workshop.{ReadyLot, Repair}
email = "/run/secrets/admin_email" |> File.read!() |> String.trim_trailing()
audit = %{actor: email}
riletta = fn busta -> Repo.get!(Repair, busta.id) end
nuova = fn nome, telefono, campi ->
  with {:ok, cliente} <- Workshop.create_customer(%{name: nome, phone: telefono}, audit),
       {:ok, busta, _token} <- Workshop.create_repair(cliente,
         Map.merge(%{"accepted_on" => "2026-03-02", "description" => "Lavoro sintetico"}, campi), audit),
       do: {:ok, busta}
end
stato = fn busta, nuovo, contesto ->
  case TelegramNotices.transition(riletta.(busta), nuovo, contesto) do
    {:ok, cambiata, _avviso} -> {:ok, cambiata}
    errore -> errore
  end
end
aperture_attese = 105
risultato =
  with %User{} = titolare <- Repo.get_by(User, email: email),
       audit_scheda = %{actor: email, session_user: {titolare.id, titolare.session_version}},
       {:ok, b11} <- nuova.("Cliente Sintetico 11", "+390000000011", %{"bag_number" => "11/26", "qr_code" => "SINT-QR-0011"}),
       {:ok, _} <- stato.(b11, "in_attesa_materiali", audit_scheda),
       {:ok, _} <- Workshop.set_repair_waiting(riletta.(b11), %{"azione" => "on", "motivo" => "  Quale <pietra>? & \"oro\"  "}, audit_scheda),
       {:ok, b12} <- nuova.("Cliente Sintetico 12", "+390000000012", %{"bag_number" => "12/26", "qr_code" => "SINT-QR-0012"}),
       {:ok, _} <- stato.(b12, "annullata", audit_scheda),
       {:ok, _} <- Workshop.set_repair_waiting(riletta.(b12), %{"azione" => " ON ", "motivo" => "   "}, audit_scheda),
       {:ok, b13} <- nuova.("Cliente Sintetico 13", "+390000000013", %{"bag_number" => "13/26", "qr_code" => "SINT-QR-0013", "compro_oro" => "true"}),
       {:ok, _} <- stato.(b13, "valutata", audit_scheda),
       {:ok, _} <- stato.(b13, "perfezionata", audit_scheda),
       {:ok, _} <- stato.(b13, "chiusa", audit_scheda),
       {:ok, b14} <- nuova.("Cliente Sintetico 14", "+390000000014", %{"bag_number" => "14/26", "qr_code" => "SINT-QR-0014", "oreficeria_id" => "2"}),
       {:ok, _} <- stato.(b14, "pronta", audit_scheda),
       {:ok, lotto} <- ReadyLots.open_lot(2, audit),
       {:ok, _} <- ReadyLots.add_bag(lotto.id, %{"riparazione_id" => Integer.to_string(b14.id)}, audit),
       {:ok, _} <- ReadyLots.send_lot(lotto.id, audit),
       {:ok, _} <- stato.(b14, "consegnata", audit_scheda),
       {:ok, b15} <- nuova.("Cliente Sintetico 15", "+390000000015", %{"bag_number" => "15/26", "qr_code" => "SINT-QR-0015"}),
       {:ok, _} <- stato.(b15, "pronta", audit_scheda),
       {:ok, _} <- stato.(b15, "consegnata", audit_scheda),
       {:ok, _} <- Workshop.reopen_repair(riletta.(b15), %{"status" => "pronta",
         "reopen_reason" => "  Pietra <da> rifissare & \"subito\"  "}, audit_scheda),
       {:ok, _} <- Workshop.set_repair_waiting(riletta.(b15), %{"azione" => "on", "motivo" => "Prima domanda"}, audit_scheda),
       {:ok, _} <- Workshop.set_repair_waiting(riletta.(b15), %{"azione" => "off"}, audit_scheda),
       {:ok, _} <- Enum.reduce_while(1..aperture_attese, {:ok, 0}, fn _, {:ok, n} ->
         case Workshop.record_repair_event(riletta.(b11), "repair.viewed", audit_scheda) do
           {:ok, _} -> {:cont, {:ok, n + 1}}
           errore -> {:halt, errore}
         end
       end) do
    buste = Enum.map([b11, b12, b13, b14, b15], riletta)
    riaperture = Enum.count(Workshop.list_audit_events(), &(&1.action == "repair.reopened"))
    %{esito: "ok", buste: Enum.map(buste, & &1.id), stati: Enum.map(buste, & &1.status),
      compro_oro: Enum.map(buste, & &1.compro_oro), attesa: Enum.map(buste, & &1.attesa_cliente),
      versioni: Enum.map(buste, &Workshop.status_version(&1.id)), riaperture: riaperture,
      lotto_chiuso: not is_nil(Repo.get!(ReadyLot, lotto.id).closed_at), lotti: Repo.aggregate(ReadyLot, :count),
      aperture: Enum.count(Workshop.list_audit_events(), &(&1.action == "repair.viewed" and &1.resource_id == Integer.to_string(b11.id))),
      totale_buste: Repo.aggregate(Repair, :count)}
  else
    errore -> %{esito: "errore", motivo: inspect(errore)}
  end
IO.puts("GR_STATI:" <> Jason.encode!(risultato))
if risultato.esito != "ok", do: System.halt(1)`

// comandoStati gira come le modifiche: stessa immagine, senza rete, stesse protezioni e stessi dati
// dell'istanza, e nient'altro.
func comandoStati(n nomi, img string, env []string) []string {
	a := append(passoSenzaRete(n, passoStati), protezioniStandalone(n)...)
	a = append(a, montaggiStandalone(n)...)
	a = append(a, env...)
	return append(a, img, "bin/gestrip_standalone", "eval", statiStandalone)
}

// esitoStati è la riga GR_STATI: del passo degli stati.
type esitoStati struct {
	Esito       string   `json:"esito"`
	Motivo      string   `json:"motivo"`
	Buste       []int64  `json:"buste"`
	Stati       []string `json:"stati"`
	CompraOro   []bool   `json:"compro_oro"`
	Attesa      []bool   `json:"attesa"`
	Versioni    []int64  `json:"versioni"`
	Riaperture  int64    `json:"riaperture"`
	LottoChiuso bool     `json:"lotto_chiuso"`
	Lotti       int64    `json:"lotti"`
	Aperture    int64    `json:"aperture"`
	TotaleBuste int64    `json:"totale_buste"`
}

// leggiEsitoStati vuole una sola riga GR_STATI: riuscita, senza campi in più: le cinque buste con
// id, stati, tipi, attese e versioni attesi, una sola riapertura, il lotto chiuso dalla consegna
// (almeno un lotto), le aperture della scheda della 11/26 e le buste in tutto (quelle della cattura e delle modifiche più queste).
func leggiEsitoStati(out string) (esitoStati, error) {
	righe := righeMarcatore(out, "GR_STATI:")
	if len(righe) != 1 {
		return esitoStati{}, fmt.Errorf("stati nello standalone: %d righe GR_STATI, ne serve una", len(righe))
	}
	var e esitoStati
	dec := json.NewDecoder(strings.NewReader(righe[0]))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&e); err != nil {
		return esitoStati{}, fmt.Errorf("stati nello standalone: riga GR_STATI non valida: %w", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return esitoStati{}, errors.New("stati nello standalone: dati dopo la riga GR_STATI")
	}
	switch {
	case e.Esito != "ok":
		return esitoStati{}, fmt.Errorf("stati nello standalone: esito %q: %s", e.Esito, e.Motivo)
	case !slices.Equal(e.Buste, busteStati):
		return esitoStati{}, fmt.Errorf("stati nello standalone: buste %v, attese %v", e.Buste, busteStati)
	case !slices.Equal(e.Stati, statiStati) || !slices.Equal(e.CompraOro, compraOroStati):
		return esitoStati{}, fmt.Errorf("stati nello standalone: stati %v compro oro %v, attesi %v %v", e.Stati, e.CompraOro, statiStati, compraOroStati)
	case !slices.Equal(e.Attesa, attesaStati) || !slices.Equal(e.Versioni, versioniStati):
		return esitoStati{}, fmt.Errorf("stati nello standalone: attese %v versioni %v, attese %v %v", e.Attesa, e.Versioni, attesaStati, versioniStati)
	case e.Riaperture != 1 || !e.LottoChiuso || e.Lotti < 1:
		return esitoStati{}, fmt.Errorf("stati nello standalone: %d riaperture, lotto chiuso %v, %d lotti", e.Riaperture, e.LottoChiuso, e.Lotti)
	case e.Aperture != apertureStati:
		return esitoStati{}, fmt.Errorf("stati nello standalone: %d aperture della scheda 11/26, attese %d", e.Aperture, apertureStati)
	case e.TotaleBuste != busteStati[len(busteStati)-1]:
		return esitoStati{}, fmt.Errorf("stati nello standalone: %d buste in tutto, attese %d", e.TotaleBuste, busteStati[len(busteStati)-1])
	}
	return e, nil
}

// statiBuste fa il passo degli stati e ne dà l'esito controllato.
func statiBuste(n nomi, img string, env []string) (esitoStati, error) {
	fmt.Println("catena: stati, riaperture, attese e un lotto chiuso alla consegna nello standalone…")
	out, errDocker := dockerTiene(comandoStati(n, img, env)...)
	e, err := leggiEsitoStati(out)
	if err != nil {
		return esitoStati{}, errors.Join(err, errDocker)
	}
	if errDocker != nil {
		return esitoStati{}, fmt.Errorf("stati nello standalone: %w", errDocker)
	}
	return e, nil
}

// controllaStati vuole nel report dell'import Go i lotti dello standalone letti e scritti, quello
// chiuso alla consegna compreso. Senza il passo (nessun lotto contato) non controlla niente.
func controllaStati(lette map[string]int64, scritte map[string]int64, e esitoStati) error {
	if e.Lotti == 0 {
		return nil
	}
	if lette["ready_lots"] != e.Lotti || scritte["lotti_avviso"] != e.Lotti {
		return fmt.Errorf("report dell'import Go: ready_lots %d righe lette, lotti_avviso %d scritte; nello standalone %d",
			lette["ready_lots"], scritte["lotti_avviso"], e.Lotti)
	}
	return nil
}
