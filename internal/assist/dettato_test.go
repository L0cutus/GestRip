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

// TrascrizioneConfigurata è Assist.configured?(:stt) (slice 110, il «🎤 Detta a voce»): basta l'indirizzo, ma non di
// soli spazi; un database chiuso è un errore, non «non configurata».
func TestTrascrizioneConfigurata(t *testing.T) {
	i, d := impostazioniDiProva(t)
	ctx := context.Background()
	if ok, err := i.TrascrizioneConfigurata(ctx); ok || err != nil {
		t.Fatalf("vuota: %v %v", ok, err)
	}
	if _, err := d.Scrittura.Exec(`INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES ('assist.stt.endpoint', '  ', 'x'), ('assist.ai.endpoint', ?, 'x'), ('assist.ai.model', 'm', 'x')`, indirizzoLocale); err != nil {
		t.Fatal(err)
	}
	if ok, _ := i.TrascrizioneConfigurata(ctx); ok {
		t.Error("indirizzo di soli spazi (e l'estrazione configurata): configurata")
	}
	if _, err := d.Scrittura.Exec(`UPDATE impostazioni SET valore = ? WHERE chiave = 'assist.stt.endpoint'`, indirizzoLocale); err != nil {
		t.Fatal(err)
	}
	if ok, err := i.TrascrizioneConfigurata(ctx); !ok || err != nil {
		t.Errorf("indirizzo: %v %v", ok, err)
	}
	if err := d.Chiudi(); err != nil {
		t.Fatal(err)
	}
	if ok, err := i.TrascrizioneConfigurata(ctx); ok || err == nil {
		t.Errorf("database chiuso: %v %v", ok, err)
	}
}
