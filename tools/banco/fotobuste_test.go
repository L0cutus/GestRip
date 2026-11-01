package main

import (
	"strings"
	"testing"

	"github.com/L0cutus/gestrip/internal/imaging/sintetiche"
)

func rigaFoto(json string) string { return "GR_FOTO:" + json + "\n" }

const esitoFotoBuono = `{"esito":"ok","busta":1,"foto":4,"posizione":3,"tipo":"image/jpeg","totale_foto":4}`

// Il passo delle foto gira come la vendita: immagine dello standalone, senza rete, con le
// protezioni e i dati dell'istanza, più la sola foto della busta in sola lettura; banco giu toglie
// il suo container.
func TestComandoFotoBuste(t *testing.T) {
	n, _ := istanzaDiProva(t)
	p := catenaPer(n)
	c := comandoFotoBuste(n, "img:1", []string{"--env", "DATABASE_PATH=/data/gestrip.db"}, p)
	s := strings.Join(c, " ")
	if valoreDopo(c, "--name") != n.base+"-"+passoFoto || valoreDopo(c, "--network") != "none" || indice(c, "--rm") < 0 {
		t.Errorf("nome, rete o --rm: %s", s)
	}
	if !strings.Contains(s, strings.Join(protezioniStandalone(n), " ")) || !strings.Contains(s, "--env DATABASE_PATH=/data/gestrip.db") {
		t.Errorf("protezioni o ambiente: %s", s)
	}
	montaggi := 0
	for _, a := range c {
		if strings.HasPrefix(a, "type=bind,") {
			montaggi++
		}
	}
	if src, ro := montaggio(c, "/catena/foto-busta.jpg"); montaggi != 4 || src != p.fotoBusta || !ro {
		t.Errorf("%d montaggi, foto da %q ro=%v: %s", montaggi, src, ro, s)
	}
	i := indice(c, "img:1")
	if i < 0 || len(c) != i+4 || c[i+1] != "bin/gestrip_standalone" || c[i+2] != "eval" || c[i+3] != fotoBusteStandalone {
		t.Fatalf("comando = %v", c)
	}
	found := false
	for _, x := range suffissiContainer() {
		found = found || x == "-"+passoFoto
	}
	if !found {
		t.Error("banco giu non toglie il container delle foto")
	}
}

// Lo script usa la funzione del caricamento della scheda con il contesto della scheda, sulla 1/26,
// con la foto montata; nessuna scrittura diretta.
func TestScriptFotoComeLaScheda(t *testing.T) {
	for _, voluto := range []string{
		"GestripStandalone.LegacyImport.Release.quiet!()",
		"audit_scheda = %{actor: email, session_user: {titolare.id, titolare.session_version}}",
		`Repo.get_by(Repair, bag_number: "1/26")`,
		`RepairPhotos.attach_many(b1, [%{path: "/catena/foto-busta.jpg", original_filename: "prova-sintetica.jpg"}], audit_scheda)`,
		`IO.puts("GR_FOTO:" <> Jason.encode!(risultato))`,
	} {
		if !strings.Contains(fotoBusteStandalone, voluto) {
			t.Errorf("manca %q", voluto)
		}
	}
	for _, vietato := range []string{"Repo.insert", "Repo.update", "Repo.query", "Repo.delete", "MediaStore.store"} {
		if strings.Contains(fotoBusteStandalone, vietato) {
			t.Errorf("scrittura diretta %q", vietato)
		}
	}
}

// La riga GR_FOTO si legge in modo stretto: una sola, riuscita, senza campi in più, con la foto 4
// in posizione 3 della 1/26, JPEG, e 4 foto in tutto.
func TestLeggiEsitoFotoBuste(t *testing.T) {
	if _, err := leggiEsitoFotoBuste("avvio\n" + rigaFoto(esitoFotoBuono)); err != nil {
		t.Fatal(err)
	}
	cambia := func(vecchio, nuovo string) string {
		if !strings.Contains(esitoFotoBuono, vecchio) {
			t.Fatalf("%q non c'è", vecchio)
		}
		return rigaFoto(strings.Replace(esitoFotoBuono, vecchio, nuovo, 1))
	}
	for nome, out := range map[string]string{
		"nessuna riga":    "fatto\n",
		"due righe":       rigaFoto(esitoFotoBuono) + rigaFoto(esitoFotoBuono),
		"esito errore":    rigaFoto(`{"esito":"errore","motivo":"{:error, :invalid_image}"}`),
		"campo in più":    cambia(`"esito":"ok"`, `"esito":"ok","chiave":"x"`),
		"altra busta":     cambia(`"busta":1`, `"busta":2`),
		"altra foto":      cambia(`"foto":4`, `"foto":5`),
		"altra posizione": cambia(`"posizione":3`, `"posizione":1`),
		"PNG":             cambia(`image/jpeg`, `image/png`),
		"totale diverso":  cambia(`"totale_foto":4`, `"totale_foto":3`),
		"dati in coda":    strings.TrimSuffix(rigaFoto(esitoFotoBuono), "\n") + "{}\n",
	} {
		if _, err := leggiEsitoFotoBuste(out); err == nil {
			t.Errorf("%s: accettato", nome)
		}
	}
}

// La foto della busta è un JPEG vero di 1000×750, stabile da un giro all'altro.
func TestFotoBustaSintetica(t *testing.T) {
	a, err := fotoBustaSintetica()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := fotoBustaSintetica()
	if i, ok := sintetiche.LeggiJPEG(a); !ok || i.Larghezza != 1000 || i.Altezza != 750 || string(a) != string(b) {
		t.Errorf("foto %+v %v", i, ok)
	}
}
