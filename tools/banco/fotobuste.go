package main

// Il passo delle foto delle buste (slice 037): dopo il link, nello standalone ancora fermo, una foto
// vera (un JPEG inventato generato dal banco, montato in sola lettura) caricata sulla 1/26 della
// cattura con la funzione che chiama il caricamento della scheda (go-baseline):
// RepairPhotos.attach_many/3, con il contesto della scheda (actor e session_user). Le foto della
// cattura sono byte che non si decodificano (synthetic_capture.py): senza questa foto il banco non
// confronterebbe mai una miniatura vera. Lo standalone la normalizza con Pillow; l'import la porta
// nel Go con i suoi byte, quindi la foto intera si confronta byte per byte e la miniatura (fatta da
// Pillow da una parte e dal Go dall'altra, tema H) per tipo, intestazioni e dimensioni.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"strings"
)

// Quello che il passo deve dire: la foto nuova è la quarta (le tre della cattura prima), in
// posizione 3 della 1/26 (che ne ha già due), JPEG.
const (
	bustaFotoNuova  = int64(1)
	idFotoNuova     = int64(4)
	posFotoNuova    = int64(3)
	totaleFotoBuste = int64(4)
)

const fotoBusteStandalone = `GestripStandalone.LegacyImport.Release.quiet!()
{:ok, _} = Application.ensure_all_started(:gestrip_standalone)
alias GestripStandalone.{Repo, RepairPhotos}
alias GestripStandalone.Accounts.User
alias GestripStandalone.Workshop.{Repair, RepairPhoto}
email = "/run/secrets/admin_email" |> File.read!() |> String.trim_trailing()
risultato =
  with %User{} = titolare <- Repo.get_by(User, email: email),
       audit_scheda = %{actor: email, session_user: {titolare.id, titolare.session_version}},
       %Repair{} = b1 <- Repo.get_by(Repair, bag_number: "1/26"),
       {:ok, [foto]} <- RepairPhotos.attach_many(b1, [%{path: "/catena/foto-busta.jpg", original_filename: "prova-sintetica.jpg"}], audit_scheda) do
    %{esito: "ok", busta: b1.id, foto: foto.id, posizione: foto.position, tipo: foto.mime_type,
      totale_foto: Repo.aggregate(RepairPhoto, :count)}
  else
    errore -> %{esito: "errore", motivo: inspect(errore)}
  end
IO.puts("GR_FOTO:" <> Jason.encode!(risultato))
if risultato.esito != "ok", do: System.halt(1)`

// comandoFotoBuste gira come la vendita: stessa immagine, senza rete, stesse protezioni e stessi
// dati dell'istanza, più la foto in sola lettura (al posto del file temporaneo del caricamento).
func comandoFotoBuste(n nomi, img string, env []string, p percorsiCatena) []string {
	a := append(passoSenzaRete(n, passoFoto), protezioniStandalone(n)...)
	a = append(a, montaggiStandalone(n)...)
	a = append(a, monta(p.fotoBusta, "/catena/foto-busta.jpg", true)...)
	a = append(a, env...)
	return append(a, img, "bin/gestrip_standalone", "eval", fotoBusteStandalone)
}

// esitoFotoBuste è la riga GR_FOTO: del passo delle foto.
type esitoFotoBuste struct {
	Esito      string `json:"esito"`
	Motivo     string `json:"motivo"`
	Busta      int64  `json:"busta"`
	Foto       int64  `json:"foto"`
	Posizione  int64  `json:"posizione"`
	Tipo       string `json:"tipo"`
	TotaleFoto int64  `json:"totale_foto"`
}

// leggiEsitoFotoBuste vuole una sola riga GR_FOTO: riuscita, senza campi in più, con la foto, la
// busta, la posizione, il tipo e il totale attesi.
func leggiEsitoFotoBuste(out string) (esitoFotoBuste, error) {
	righe := righeMarcatore(out, "GR_FOTO:")
	if len(righe) != 1 {
		return esitoFotoBuste{}, fmt.Errorf("foto nello standalone: %d righe GR_FOTO, ne serve una", len(righe))
	}
	var e esitoFotoBuste
	dec := json.NewDecoder(strings.NewReader(righe[0]))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&e); err != nil {
		return esitoFotoBuste{}, fmt.Errorf("foto nello standalone: riga GR_FOTO non valida: %w", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return esitoFotoBuste{}, errors.New("foto nello standalone: dati dopo la riga GR_FOTO")
	}
	switch {
	case e.Esito != "ok":
		return esitoFotoBuste{}, fmt.Errorf("foto nello standalone: esito %q: %s", e.Esito, e.Motivo)
	case e.Busta != bustaFotoNuova || e.Foto != idFotoNuova || e.Posizione != posFotoNuova || e.Tipo != "image/jpeg":
		return esitoFotoBuste{}, fmt.Errorf("foto nello standalone: busta %d foto %d posizione %d tipo %q, attese %d %d %d image/jpeg",
			e.Busta, e.Foto, e.Posizione, e.Tipo, bustaFotoNuova, idFotoNuova, posFotoNuova)
	case e.TotaleFoto != totaleFotoBuste:
		return esitoFotoBuste{}, fmt.Errorf("foto nello standalone: %d foto delle buste in tutto, attese %d", e.TotaleFoto, totaleFotoBuste)
	}
	return e, nil
}

// fotoBustaSintetica è un JPEG inventato di 1000×750 (una sfumatura), generato a ogni giro: nessun
// file binario nel repo. È grande abbastanza da dare una miniatura vera (400×300).
func fotoBustaSintetica() ([]byte, error) {
	img := image.NewRGBA(image.Rect(0, 0, 1000, 750))
	for y := range 750 {
		for x := range 1000 {
			img.Set(x, y, color.RGBA{R: uint8(x / 4), G: uint8(y / 3), B: uint8((x + y) / 7), A: 255})
		}
	}
	var b bytes.Buffer
	err := jpeg.Encode(&b, img, &jpeg.Options{Quality: 90})
	return b.Bytes(), err
}

// fotoBuste fa il passo delle foto e ne dà l'esito controllato.
func fotoBuste(n nomi, img string, env []string, p percorsiCatena) (esitoFotoBuste, error) {
	foto, err := fotoBustaSintetica()
	if err != nil {
		return esitoFotoBuste{}, fmt.Errorf("foto sintetica della busta: %w", err)
	}
	if err := scriviPrivato(p.fotoBusta, string(foto)); err != nil {
		return esitoFotoBuste{}, err
	}
	fmt.Println("catena: foto caricata sulla 1/26 nello standalone…")
	out, errDocker := dockerTiene(comandoFotoBuste(n, img, env, p)...)
	e, err := leggiEsitoFotoBuste(out)
	if err != nil {
		return esitoFotoBuste{}, errors.Join(err, errDocker)
	}
	if errDocker != nil {
		return esitoFotoBuste{}, fmt.Errorf("foto nello standalone: %w", errDocker)
	}
	return e, nil
}
