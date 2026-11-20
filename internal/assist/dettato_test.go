package assist

import (
	"context"
	"testing"
)

// EstrazioneConfigurata è Assist.configured?(:ai): un indirizzo senza modello (un import, o una riga scritta a mano) non
// basta, e nemmeno un modello di soli spazi.
func TestEstrazioneConfigurata(t *testing.T) {
	i, d := impostazioniDiProva(t)
	ctx := context.Background()
	if ok, err := i.EstrazioneConfigurata(ctx); ok || err != nil {
		t.Fatalf("vuota: %v %v", ok, err)
	}
	if _, err := d.Scrittura.Exec(`INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES ('assist.ai.endpoint', ?, 'x'), ('assist.ai.model', ' ', 'x')`, indirizzoLocale); err != nil {
		t.Fatal(err)
	}
	if ok, _ := i.EstrazioneConfigurata(ctx); ok {
		t.Error("indirizzo senza modello: configurata")
	}
	if _, err := d.Scrittura.Exec(`UPDATE impostazioni SET valore = 'gestrip-prova' WHERE chiave = 'assist.ai.model'`); err != nil {
		t.Fatal(err)
	}
	if ok, err := i.EstrazioneConfigurata(ctx); !ok || err != nil {
		t.Errorf("indirizzo e modello: %v %v", ok, err)
	}
}
